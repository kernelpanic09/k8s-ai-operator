package controller

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
	"github.com/kernelpanic09/k8s-ai-operator/internal/metrics"
)

// GuardrailCreator is the interface over Bedrock control-plane guardrail calls.
// Extracted here so unit tests can inject a fake without touching the AWS SDK.
type GuardrailCreator interface {
	CreateGuardrail(ctx context.Context, input *awsbedrock.CreateGuardrailInput, opts ...func(*awsbedrock.Options)) (*awsbedrock.CreateGuardrailOutput, error)
	UpdateGuardrail(ctx context.Context, input *awsbedrock.UpdateGuardrailInput, opts ...func(*awsbedrock.Options)) (*awsbedrock.UpdateGuardrailOutput, error)
	DeleteGuardrail(ctx context.Context, input *awsbedrock.DeleteGuardrailInput, opts ...func(*awsbedrock.Options)) (*awsbedrock.DeleteGuardrailOutput, error)
}

// GuardrailPolicyReconciler reconciles GuardrailPolicy objects.
//
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=guardrailpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=guardrailpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.kernelpanic09.io,resources=guardrailpolicies/finalizers,verbs=update
type GuardrailPolicyReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Logger  *slog.Logger
	Bedrock GuardrailCreator
}

// SetupWithManager registers the reconciler.
func (r *GuardrailPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.GuardrailPolicy{}).
		Complete(r)
}

// Reconcile syncs the GuardrailPolicy spec to the Bedrock Guardrails API.
// On create: call CreateGuardrail and store the returned ID in status.
// On update: call UpdateGuardrail with the existing ID.
// On delete: call DeleteGuardrail before removing the finalizer.
func (r *GuardrailPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With("guardrail", req.Name)

	var gp aiv1alpha1.GuardrailPolicy
	if err := r.Get(ctx, req.NamespacedName, &gp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching GuardrailPolicy: %w", err)
	}

	if !gp.DeletionTimestamp.IsZero() {
		return r.handleGuardrailDeletion(ctx, log, &gp)
	}

	if !controllerutil.ContainsFinalizer(&gp, aiv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(&gp, aiv1alpha1.FinalizerName)
		if err := r.Update(ctx, &gp); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	patch := client.MergeFrom(gp.DeepCopy())
	gp.Status.ObservedGeneration = gp.Generation

	var (
		guardrailId      string
		guardrailVersion string
		syncErr          error
	)

	if gp.Status.GuardrailId == "" {
		guardrailId, guardrailVersion, syncErr = r.createGuardrail(ctx, &gp)
	} else {
		guardrailId = gp.Status.GuardrailId
		guardrailVersion, syncErr = r.updateGuardrail(ctx, &gp)
	}

	if syncErr != nil {
		log.Error("syncing guardrail to Bedrock", "error", syncErr)
		metrics.ReconcileErrorsTotal.WithLabelValues("guardrailpolicy", "bedrock_sync").Inc()

		setCondition(&gp.Status.Conditions, metav1.Condition{
			Type:               aiv1alpha1.ConditionGuardrailSynced,
			Status:             metav1.ConditionFalse,
			Reason:             "SyncFailed",
			Message:            syncErr.Error(),
			LastTransitionTime: metav1.Now(),
		})

		if err := r.Status().Patch(ctx, &gp, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("patching status: %w", err)
		}

		return ctrl.Result{RequeueAfter: requeueTransient}, nil
	}

	gp.Status.GuardrailId = guardrailId
	gp.Status.GuardrailVersion = guardrailVersion

	setCondition(&gp.Status.Conditions, metav1.Condition{
		Type:               aiv1alpha1.ConditionGuardrailSynced,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            fmt.Sprintf("guardrail %s version %s synced to Bedrock", guardrailId, guardrailVersion),
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Patch(ctx, &gp, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching status: %w", err)
	}

	log.Info("guardrail synced", "id", guardrailId, "version", guardrailVersion)
	return ctrl.Result{RequeueAfter: requeueSteadyState}, nil
}

func (r *GuardrailPolicyReconciler) createGuardrail(
	ctx context.Context,
	gp *aiv1alpha1.GuardrailPolicy,
) (id, version string, err error) {
	input := &awsbedrock.CreateGuardrailInput{
		Name:        aws.String(gp.Name),
		Description: aws.String(gp.Spec.Description),
	}

	if gp.Spec.BlockedInputMessage != "" {
		input.BlockedInputMessaging = aws.String(gp.Spec.BlockedInputMessage)
	}
	if gp.Spec.BlockedOutputsMessage != "" {
		input.BlockedOutputsMessaging = aws.String(gp.Spec.BlockedOutputsMessage)
	}

	if gp.Spec.ContentPolicy != nil {
		input.ContentPolicyConfig = buildContentPolicy(gp.Spec.ContentPolicy)
	}

	if gp.Spec.TopicPolicy != nil {
		input.TopicPolicyConfig = buildTopicPolicy(gp.Spec.TopicPolicy)
	}

	if gp.Spec.WordPolicy != nil {
		input.WordPolicyConfig = buildWordPolicy(gp.Spec.WordPolicy)
	}

	if gp.Spec.SensitiveInformationPolicy != nil {
		input.SensitiveInformationPolicyConfig = buildPIIPolicy(gp.Spec.SensitiveInformationPolicy)
	}

	out, err := r.Bedrock.CreateGuardrail(ctx, input)
	if err != nil {
		return "", "", fmt.Errorf("CreateGuardrail: %w", err)
	}

	return aws.ToString(out.GuardrailId), aws.ToString(out.Version), nil
}

func (r *GuardrailPolicyReconciler) updateGuardrail(
	ctx context.Context,
	gp *aiv1alpha1.GuardrailPolicy,
) (version string, err error) {
	input := &awsbedrock.UpdateGuardrailInput{
		GuardrailIdentifier: aws.String(gp.Status.GuardrailId),
		Name:                aws.String(gp.Name),
		Description:         aws.String(gp.Spec.Description),
	}

	if gp.Spec.BlockedInputMessage != "" {
		input.BlockedInputMessaging = aws.String(gp.Spec.BlockedInputMessage)
	}
	if gp.Spec.BlockedOutputsMessage != "" {
		input.BlockedOutputsMessaging = aws.String(gp.Spec.BlockedOutputsMessage)
	}

	if gp.Spec.ContentPolicy != nil {
		input.ContentPolicyConfig = buildContentPolicy(gp.Spec.ContentPolicy)
	}

	if gp.Spec.TopicPolicy != nil {
		input.TopicPolicyConfig = buildTopicPolicy(gp.Spec.TopicPolicy)
	}

	if gp.Spec.WordPolicy != nil {
		input.WordPolicyConfig = buildWordPolicy(gp.Spec.WordPolicy)
	}

	if gp.Spec.SensitiveInformationPolicy != nil {
		input.SensitiveInformationPolicyConfig = buildPIIPolicy(gp.Spec.SensitiveInformationPolicy)
	}

	out, err := r.Bedrock.UpdateGuardrail(ctx, input)
	if err != nil {
		return "", fmt.Errorf("UpdateGuardrail %s: %w", gp.Status.GuardrailId, err)
	}

	return aws.ToString(out.GuardrailVersion), nil
}

func (r *GuardrailPolicyReconciler) handleGuardrailDeletion(
	ctx context.Context,
	log *slog.Logger,
	gp *aiv1alpha1.GuardrailPolicy,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(gp, aiv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	if gp.Status.GuardrailId != "" {
		log.Info("deleting Bedrock guardrail", "id", gp.Status.GuardrailId)
		_, err := r.Bedrock.DeleteGuardrail(ctx, &awsbedrock.DeleteGuardrailInput{
			GuardrailIdentifier: aws.String(gp.Status.GuardrailId),
		})
		if err != nil {
			// Log but don't block deletion; the guardrail may already be gone.
			log.Warn("DeleteGuardrail returned error (proceeding with finalizer removal)", "error", err)
		}
	}

	controllerutil.RemoveFinalizer(gp, aiv1alpha1.FinalizerName)
	if err := r.Update(ctx, gp); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// --- spec translation helpers ---

func buildContentPolicy(spec *aiv1alpha1.ContentPolicyConfig) *types.GuardrailContentPolicyConfig {
	cfg := &types.GuardrailContentPolicyConfig{}
	for _, f := range spec.Filters {
		cfg.FiltersConfig = append(cfg.FiltersConfig, types.GuardrailContentFilterConfig{
			Type:           types.GuardrailContentFilterType(f.Type),
			InputStrength:  types.GuardrailFilterStrength(f.InputStrength),
			OutputStrength: types.GuardrailFilterStrength(f.OutputStrength),
		})
	}
	return cfg
}

func buildTopicPolicy(spec *aiv1alpha1.TopicPolicyConfig) *types.GuardrailTopicPolicyConfig {
	cfg := &types.GuardrailTopicPolicyConfig{}
	for _, t := range spec.Topics {
		topic := types.GuardrailTopicConfig{
			Name:       aws.String(t.Name),
			Definition: aws.String(t.Definition),
			Type:       types.GuardrailTopicTypeDeny,
		}
		for _, ex := range t.Examples {
			topic.Examples = append(topic.Examples, ex)
		}
		cfg.TopicsConfig = append(cfg.TopicsConfig, topic)
	}
	return cfg
}

func buildWordPolicy(spec *aiv1alpha1.WordPolicyConfig) *types.GuardrailWordPolicyConfig {
	cfg := &types.GuardrailWordPolicyConfig{}
	for _, w := range spec.Words {
		cfg.WordsConfig = append(cfg.WordsConfig, types.GuardrailWordConfig{
			Text: aws.String(w),
		})
	}
	for _, ml := range spec.ManagedWordLists {
		cfg.ManagedWordListsConfig = append(cfg.ManagedWordListsConfig, types.GuardrailManagedWordsConfig{
			Type: types.GuardrailManagedWordsType(ml),
		})
	}
	return cfg
}

func buildPIIPolicy(spec *aiv1alpha1.SensitiveInformationPolicyConfig) *types.GuardrailSensitiveInformationPolicyConfig {
	cfg := &types.GuardrailSensitiveInformationPolicyConfig{}
	for _, p := range spec.PIIEntities {
		cfg.PiiEntitiesConfig = append(cfg.PiiEntitiesConfig, types.GuardrailPiiEntityConfig{
			Type:   types.GuardrailPiiEntityType(p.Type),
			Action: types.GuardrailSensitiveInformationAction(p.Action),
		})
	}
	return cfg
}
