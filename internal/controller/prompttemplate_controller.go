package controller

import (
	"context"
	"fmt"
	"log/slog"
	"text/template"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
)

// PromptTemplateReconciler reconciles PromptTemplate objects.
//
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=prompttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=prompttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=prompttemplates/finalizers,verbs=update
type PromptTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

// SetupWithManager registers the reconciler.
func (r *PromptTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.PromptTemplate{}).
		Complete(r)
}

// Reconcile validates the PromptTemplate and ensures the referenced
// ModelEndpoint exists. This controller doesn't create any owned resources;
// it only sets status conditions to report on the template's validity.
func (r *PromptTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With("template", req.NamespacedName)

	var pt aiv1alpha1.PromptTemplate
	if err := r.Get(ctx, req.NamespacedName, &pt); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching PromptTemplate: %w", err)
	}

	if !pt.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pt, aiv1alpha1.FinalizerName) {
			controllerutil.RemoveFinalizer(&pt, aiv1alpha1.FinalizerName)
			if err := r.Update(ctx, &pt); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&pt, aiv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(&pt, aiv1alpha1.FinalizerName)
		if err := r.Update(ctx, &pt); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	patch := client.MergeFrom(pt.DeepCopy())
	pt.Status.ObservedGeneration = pt.Generation

	// Validate that the template parses as valid Go text/template syntax.
	templateValid := true
	templateMsg := "template syntax is valid"
	if _, err := template.New("validate").Parse(pt.Spec.Template); err != nil {
		templateValid = false
		templateMsg = fmt.Sprintf("template syntax error: %v", err)
	}

	// Validate that the referenced ModelEndpoint exists.
	endpointReady := false
	endpointMsg := "ModelEndpoint not found"
	endpointReason := "EndpointNotFound"

	var ep aiv1alpha1.ModelEndpoint
	epKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      pt.Spec.ModelEndpointRef.Name,
	}
	if err := r.Get(ctx, epKey, &ep); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("fetching referenced ModelEndpoint: %w", err)
		}
	} else {
		endpointReady = ep.Status.Available
		endpointReason = "EndpointFound"
		if endpointReady {
			endpointMsg = fmt.Sprintf("ModelEndpoint %q is available", ep.Name)
		} else {
			endpointMsg = fmt.Sprintf("ModelEndpoint %q exists but is not available", ep.Name)
			endpointReason = "EndpointUnavailable"
		}
	}

	setCondition(&pt.Status.Conditions, metav1.Condition{
		Type:               "TemplateValid",
		Status:             conditionStatus(templateValid),
		Reason:             "ParseCheck",
		Message:            templateMsg,
		LastTransitionTime: metav1.Now(),
	})

	setCondition(&pt.Status.Conditions, metav1.Condition{
		Type:               aiv1alpha1.ConditionReady,
		Status:             conditionStatus(templateValid && endpointReady),
		Reason:             endpointReason,
		Message:            endpointMsg,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Patch(ctx, &pt, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching status: %w", err)
	}

	log.Info("reconciled", "valid", templateValid, "endpoint_ready", endpointReady)

	if !endpointReady {
		return ctrl.Result{RequeueAfter: requeueTransient}, nil
	}
	return ctrl.Result{RequeueAfter: requeueSteadyState}, nil
}
