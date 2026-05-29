package v1alpha1

// BudgetSpec defines cost limits for a ModelEndpoint.
type BudgetSpec struct {
	// Daily is the maximum USD spend allowed per calendar day.
	// The operator gates new requests once this threshold is breached,
	// returning HTTP 429 until the next UTC midnight reset.
	// +kubebuilder:validation:Pattern=`^\d+(\.\d{1,2})?$`
	// +optional
	Daily string `json:"daily,omitempty"`

	// Monthly is the maximum USD spend allowed in the current calendar month.
	// +kubebuilder:validation:Pattern=`^\d+(\.\d{1,2})?$`
	// +optional
	Monthly string `json:"monthly,omitempty"`
}

// RateLimitSpec controls request throughput for a ModelEndpoint.
type RateLimitSpec struct {
	// RequestsPerMinute is a token-bucket rate applied per endpoint.
	// Requests that exceed this rate receive HTTP 429 with a Retry-After header.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	RequestsPerMinute int32 `json:"requestsPerMinute"`
}

// ModelEndpointRef is a local-namespace reference to a ModelEndpoint.
type ModelEndpointRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ConditionType names used across all resources in this group.
const (
	// ConditionReady indicates the resource is configured and operational.
	ConditionReady = "Ready"

	// ConditionBedrockReachable indicates the operator successfully contacted
	// the Bedrock control plane for this model in its configured region.
	ConditionBedrockReachable = "BedrockReachable"

	// ConditionBudgetBreached indicates daily or monthly spend has exceeded
	// the configured limit. New invocations are gated until the period resets.
	ConditionBudgetBreached = "BudgetBreached"

	// ConditionGuardrailSynced indicates the GuardrailPolicy has been
	// reconciled to the Bedrock API and its guardrailId/version are current.
	ConditionGuardrailSynced = "GuardrailSynced"
)

// FinalizerName is the finalizer this operator adds to resources it manages.
// It gates deletion until the operator has cleaned up in-cluster state
// (rate limiter buckets, metric label cardinality, etc.).
const FinalizerName = "ai.kernelpanic09.io/finalizer"
