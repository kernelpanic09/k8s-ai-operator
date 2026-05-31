// Package webhook implements admission webhooks for the operator's CRDs.
// The validating webhook catches configuration errors at apply-time rather
// than surfacing them as confusing status conditions after the fact.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
)

// validRegions is the set of AWS regions where Bedrock is available.
// Updated as AWS expands Bedrock's footprint; rejecting unknown regions early
// prevents the operator from making impossible API calls.
var validRegions = map[string]bool{
	"us-east-1":      true,
	"us-east-2":      true,
	"us-west-2":      true,
	"eu-west-1":      true,
	"eu-west-2":      true,
	"eu-west-3":      true,
	"eu-central-1":   true,
	"eu-north-1":     true,
	"ap-southeast-1": true,
	"ap-southeast-2": true,
	"ap-northeast-1": true,
	"ap-northeast-2": true,
	"ap-south-1":     true,
	"ca-central-1":   true,
	"sa-east-1":      true,
	"us-gov-west-1":  true,
}

// knownModelPrefixes are the vendor prefixes for models available on Bedrock.
// We don't enumerate every model ID here (they change too frequently), but we
// reject obvious typos by checking the prefix.
var knownModelPrefixes = []string{
	"anthropic.",
	"amazon.",
	"meta.",
	"mistral.",
	"cohere.",
	"ai21.",
	"stability.",
	"writer.",
}

// irsaArnPattern matches IAM role ARNs in the form arn:aws:iam::<account>:role/<name>.
var irsaArnPattern = regexp.MustCompile(`^arn:aws[a-z-]*:iam::\d{12}:role/.+$`)

// ModelEndpointValidator is the validating admission handler for ModelEndpoint.
//
// +kubebuilder:webhook:path=/validate-ai-kernelpanic09-io-v1alpha1-modelendpoint,mutating=false,failurePolicy=fail,sideEffects=None,groups=ai.kernelpanic09.io,resources=modelendpoints,verbs=create;update,versions=v1alpha1,name=vmodelendpoint.kb.io,admissionReviewVersions=v1
type ModelEndpointValidator struct {
	decoder admission.Decoder
}

// SetupWithManager registers the webhook with the manager.
func (v *ModelEndpointValidator) SetupWithManager(mgr ctrl.Manager) error {
	// As of controller-runtime v0.15 the framework no longer injects a decoder
	// via InjectDecoder; construct one from the manager's scheme instead.
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register(
		"/validate-ai-kernelpanic09-io-v1alpha1-modelendpoint",
		&admission.Webhook{Handler: v},
	)
	return nil
}

// Handle validates a ModelEndpoint admission request.
func (v *ModelEndpointValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	ep := &aiv1alpha1.ModelEndpoint{}
	if err := v.decoder.DecodeRaw(req.Object, ep); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decoding ModelEndpoint: %w", err))
	}

	if err := validateModelEndpoint(ep); err != nil {
		return admission.Denied(err.Error())
	}

	return admission.Allowed("")
}

func validateModelEndpoint(ep *aiv1alpha1.ModelEndpoint) error {
	if err := validateRegion(ep.Spec.Region); err != nil {
		return err
	}

	if err := validateModelId(ep.Spec.ModelId); err != nil {
		return err
	}

	if !irsaArnPattern.MatchString(ep.Spec.IRSARoleArn) {
		return fmt.Errorf("spec.irsaRoleArn %q does not look like a valid IAM role ARN; expected arn:aws:iam::<account>:role/<name>", ep.Spec.IRSARoleArn)
	}

	if ep.Spec.CostBudget != nil {
		if err := validateBudget(ep.Spec.CostBudget); err != nil {
			return err
		}
	}

	if ep.Spec.RateLimit != nil {
		if ep.Spec.RateLimit.RequestsPerMinute < 1 || ep.Spec.RateLimit.RequestsPerMinute > 3600 {
			return fmt.Errorf("spec.rateLimit.requestsPerMinute must be between 1 and 3600, got %d",
				ep.Spec.RateLimit.RequestsPerMinute)
		}
	}

	return nil
}

func validateRegion(region string) error {
	if !validRegions[region] {
		return fmt.Errorf("spec.region %q is not a known Bedrock-enabled AWS region", region)
	}
	return nil
}

func validateModelId(modelId string) error {
	for _, prefix := range knownModelPrefixes {
		if len(modelId) > len(prefix) && modelId[:len(prefix)] == prefix {
			return nil
		}
	}
	return fmt.Errorf("spec.modelId %q does not start with a known Bedrock vendor prefix (anthropic., amazon., meta., etc.)", modelId)
}

func validateBudget(budget *aiv1alpha1.BudgetSpec) error {
	if budget.Daily == "" && budget.Monthly == "" {
		return fmt.Errorf("spec.costBudget is present but both daily and monthly are empty; set at least one")
	}
	return nil
}

// ModelEndpointDefaulter is a mutating webhook that fills in sensible defaults
// for optional fields before the object is persisted.
//
// +kubebuilder:webhook:path=/mutate-ai-kernelpanic09-io-v1alpha1-modelendpoint,mutating=true,failurePolicy=fail,sideEffects=None,groups=ai.kernelpanic09.io,resources=modelendpoints,verbs=create,versions=v1alpha1,name=mmodelendpoint.kb.io,admissionReviewVersions=v1
type ModelEndpointDefaulter struct {
	decoder admission.Decoder
	scheme  *runtime.Scheme
}

// NewModelEndpointDefaulter constructs a defaulter. The decoder is built from
// the same scheme; controller-runtime v0.15+ no longer injects one.
func NewModelEndpointDefaulter(scheme *runtime.Scheme) *ModelEndpointDefaulter {
	return &ModelEndpointDefaulter{
		scheme:  scheme,
		decoder: admission.NewDecoder(scheme),
	}
}

func (d *ModelEndpointDefaulter) SetupWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(
		"/mutate-ai-kernelpanic09-io-v1alpha1-modelendpoint",
		&admission.Webhook{Handler: d},
	)
	return nil
}

func (d *ModelEndpointDefaulter) Handle(ctx context.Context, req admission.Request) admission.Response {
	ep := &aiv1alpha1.ModelEndpoint{}
	if err := d.decoder.DecodeRaw(req.Object, ep); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if ep.Spec.MaxTokens == nil {
		defaultMax := int32(4096)
		ep.Spec.MaxTokens = &defaultMax
	}

	if ep.Spec.RateLimit == nil {
		ep.Spec.RateLimit = &aiv1alpha1.RateLimitSpec{RequestsPerMinute: 60}
	}

	patched, err := json.Marshal(ep)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, patched)
}
