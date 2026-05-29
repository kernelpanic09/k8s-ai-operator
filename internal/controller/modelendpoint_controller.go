package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
	"github.com/kernelpanic09/k8s-ai-operator/internal/bedrock"
	"github.com/kernelpanic09/k8s-ai-operator/internal/metrics"
)

const (
	// proxyPort is the port the operator's proxy HTTP server listens on.
	// This matches the PROXY_PORT env var in the manager Deployment.
	proxyPort = 8090

	// requeueSteadyState is the requeue interval when the endpoint is healthy.
	// Periodic requeue keeps the status and Bedrock availability check fresh.
	requeueSteadyState = 5 * time.Minute

	// requeueTransient is used when a non-fatal error occurred and we expect
	// it to resolve soon (rate limit, Bedrock hiccup, etc.).
	requeueTransient = 10 * time.Second
)

// BedrockHealthChecker is the subset of bedrock.Client the reconciler uses.
// Extracted as an interface so tests can inject a fake without spinning up AWS.
type BedrockHealthChecker interface {
	HealthCheck(ctx context.Context, key bedrock.EndpointKey, modelId string) bedrock.HealthResult
	InvalidateCache(key bedrock.EndpointKey)
}

// ModelEndpointReconciler reconciles ModelEndpoint objects.
//
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=modelendpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=modelendpoints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=modelendpoints/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
type ModelEndpointReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	BedrockClient BedrockHealthChecker
	Logger        *slog.Logger
	// ProxyNamespace is the namespace where the operator pod runs.
	ProxyNamespace string
	// ProxyServiceName is the name of the operator's own Service.
	ProxyServiceName string
}

// SetupWithManager registers the reconciler and sets up watches.
func (r *ModelEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.ModelEndpoint{}).
		// Watch Services that we own so we reconcile if someone deletes one.
		Owns(&corev1.Service{}).
		Complete(r)
}

// Reconcile is the main reconciliation loop.
func (r *ModelEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With("endpoint", req.NamespacedName)

	var ep aiv1alpha1.ModelEndpoint
	if err := r.Get(ctx, req.NamespacedName, &ep); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching ModelEndpoint: %w", err)
	}

	// Handle deletion: remove rate limiter state, clean up metrics cardinality.
	if !ep.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, log, &ep)
	}

	// Ensure the finalizer is present before doing any work.
	if !controllerutil.ContainsFinalizer(&ep, aiv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(&ep, aiv1alpha1.FinalizerName)
		if err := r.Update(ctx, &ep); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		// Re-read after update to avoid stale resource version.
		return ctrl.Result{Requeue: true}, nil
	}

	// Check whether Bedrock can reach the model. This is the core health signal.
	bedrockKey := bedrock.EndpointKey{
		RoleArn: ep.Spec.IRSARoleArn,
		Region:  ep.Spec.Region,
	}
	health := r.BedrockClient.HealthCheck(ctx, bedrockKey, ep.Spec.ModelId)

	// Reconcile the proxy Service in the endpoint's namespace.
	svcName, err := r.reconcileService(ctx, &ep)
	if err != nil {
		log.Error("reconciling Service", "error", err)
		metrics.ReconcileErrorsTotal.WithLabelValues("modelendpoint", "service_reconcile").Inc()
		return ctrl.Result{RequeueAfter: requeueTransient}, fmt.Errorf("reconciling service: %w", err)
	}

	// Update Prometheus gauge for availability alerting.
	availableVal := 0.0
	if health.Available {
		availableVal = 1.0
	}
	metrics.EndpointAvailable.WithLabelValues(ep.Namespace, ep.Name, ep.Spec.ModelId).Set(availableVal)

	// Read current cost/invocation counters from Prometheus for status update.
	// We do a best-effort scrape of the counter values here. In a full
	// implementation this would use the Prometheus HTTP API or an in-memory
	// accumulator. For now we carry forward whatever was set last.
	patch := client.MergeFrom(ep.DeepCopy())

	ep.Status.Available = health.Available
	ep.Status.ServiceName = svcName
	ep.Status.ObservedGeneration = ep.Generation

	setCondition(&ep.Status.Conditions, metav1.Condition{
		Type:               aiv1alpha1.ConditionBedrockReachable,
		Status:             conditionStatus(health.Available),
		Reason:             health.Reason,
		Message:            health.Message,
		LastTransitionTime: metav1.Now(),
	})

	setCondition(&ep.Status.Conditions, metav1.Condition{
		Type:               aiv1alpha1.ConditionReady,
		Status:             conditionStatus(health.Available),
		Reason:             health.Reason,
		Message:            health.Message,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Patch(ctx, &ep, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching status: %w", err)
	}

	if !health.Available {
		log.Warn("bedrock unavailable, will retry", "reason", health.Reason)
		return ctrl.Result{RequeueAfter: requeueTransient}, nil
	}

	log.Info("reconciled", "service", svcName, "available", health.Available)
	return ctrl.Result{RequeueAfter: requeueSteadyState}, nil
}

func (r *ModelEndpointReconciler) handleDeletion(
	ctx context.Context,
	log *slog.Logger,
	ep *aiv1alpha1.ModelEndpoint,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ep, aiv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	log.Info("deleting ModelEndpoint, cleaning up")

	// Delete the Prometheus metric series for this endpoint to avoid stale data.
	// This is best-effort; the metrics will expire on the next scrape cycle anyway.
	metrics.EndpointAvailable.DeleteLabelValues(ep.Namespace, ep.Name, ep.Spec.ModelId)

	// Invalidate the cached Bedrock client for this endpoint's credentials.
	r.BedrockClient.InvalidateCache(bedrock.EndpointKey{
		RoleArn: ep.Spec.IRSARoleArn,
		Region:  ep.Spec.Region,
	})

	controllerutil.RemoveFinalizer(ep, aiv1alpha1.FinalizerName)
	if err := r.Update(ctx, ep); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileService creates or updates the proxy Service in the endpoint's namespace.
// The Service is an ExternalName service pointing at the operator's proxy so that
// requests to svc.namespace.svc.cluster.local get routed to the operator's own
// HTTP server, which handles auth, rate limiting, and Bedrock forwarding.
//
// We use ExternalName (rather than putting the proxy's ClusterIP directly) because
// it allows the Service to exist in any namespace without needing to know the
// operator's ClusterIP at CRD creation time.
func (r *ModelEndpointReconciler) reconcileService(
	ctx context.Context,
	ep *aiv1alpha1.ModelEndpoint,
) (string, error) {
	svcName := "model-" + ep.Name

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ep.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":  "k8s-ai-operator",
				"ai.kernelpanic09.io/endpoint":  ep.Name,
				"ai.kernelpanic09.io/model-id":  sanitizeLabel(ep.Spec.ModelId),
			},
		},
	}

	if err := controllerutil.SetControllerReference(ep, desired, r.Scheme); err != nil {
		return "", fmt.Errorf("setting owner reference: %w", err)
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		// ExternalName points at the operator's own Service.
		// Callers in the same cluster can reach the proxy at:
		//   http://model-<endpoint-name>.<namespace>.svc.cluster.local:8090/invoke/<namespace>/<name>
		desired.Spec = corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: fmt.Sprintf("%s.%s.svc.cluster.local", r.ProxyServiceName, r.ProxyNamespace),
			Ports: []corev1.ServicePort{
				{
					Name:       "proxy",
					Port:       proxyPort,
					TargetPort: intstr.FromInt(proxyPort),
				},
			},
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("CreateOrUpdate service %s/%s: %w", ep.Namespace, svcName, err)
	}

	return svcName, nil
}

// setCondition upserts a condition into the slice. It only updates
// LastTransitionTime if the Status field changed, following K8s convention.
func setCondition(conditions *[]metav1.Condition, newCond metav1.Condition) {
	for i, c := range *conditions {
		if c.Type != newCond.Type {
			continue
		}
		if c.Status == newCond.Status {
			// Status unchanged; preserve the original transition time.
			newCond.LastTransitionTime = c.LastTransitionTime
		}
		(*conditions)[i] = newCond
		return
	}
	*conditions = append(*conditions, newCond)
}

func conditionStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// sanitizeLabel strips characters that aren't valid in K8s label values.
// Model IDs like "anthropic.claude-haiku-4-5-20251001-v1:0" contain colons.
func sanitizeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	// Label values must be <= 63 chars.
	if len(out) > 63 {
		out = out[:63]
	}
	return string(out)
}

// FormatCost formats a float64 cost to 2 decimal places for status fields.
// Used by the status update path when reading accumulated cost from metrics.
func FormatCost(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
