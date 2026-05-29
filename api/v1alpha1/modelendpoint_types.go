package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModelEndpointSpec defines the desired state of a ModelEndpoint.
type ModelEndpointSpec struct {
	// ModelId is the Bedrock model identifier, e.g.
	// "anthropic.claude-haiku-4-5-20251001-v1:0".
	// Cross-region inference profiles are supported; use the profile ARN form.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	ModelId string `json:"modelId"`

	// Region is the AWS region where Bedrock invocations are sent.
	// Must be a region where the model is available.
	// +kubebuilder:validation:Pattern=`^[a-z]{2}-[a-z]+-\d+$`
	Region string `json:"region"`

	// IRSARoleArn is the ARN of the IAM role the operator assumes (via IRSA)
	// when calling Bedrock on behalf of this endpoint. The role must grant
	// bedrock:InvokeModel for the configured modelId.
	// +kubebuilder:validation:Pattern=`^arn:aws:iam::\d{12}:role/.+$`
	IRSARoleArn string `json:"irsaRoleArn"`

	// MaxTokens caps the max_tokens_to_sample / max_tokens parameter sent to
	// Bedrock. Individual callers can request fewer but not more.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=131072
	// +optional
	MaxTokens *int32 `json:"maxTokens,omitempty"`

	// CostBudget sets hard spending limits. When a limit is reached, the proxy
	// returns 429 until the period resets (daily at UTC midnight; monthly on the
	// first of each month).
	// +optional
	CostBudget *BudgetSpec `json:"costBudget,omitempty"`

	// RateLimit controls request throughput via a token bucket per endpoint.
	// +optional
	RateLimit *RateLimitSpec `json:"rateLimit,omitempty"`

	// GuardrailPolicyRef references a cluster-scoped GuardrailPolicy to apply
	// to every invocation through this endpoint.
	// +optional
	GuardrailPolicyRef *string `json:"guardrailPolicyRef,omitempty"`
}

// ModelEndpointStatus is written by the controller after each reconciliation.
type ModelEndpointStatus struct {
	// Available is true when Bedrock is reachable and no budget has been breached.
	Available bool `json:"available"`

	// Conditions follows the standard K8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// CostThisMonth is the accumulated USD spend for the current calendar month,
	// formatted as a decimal string (e.g. "47.23").
	// +optional
	CostThisMonth string `json:"costThisMonth,omitempty"`

	// CostToday is the accumulated USD spend for the current UTC calendar day.
	// +optional
	CostToday string `json:"costToday,omitempty"`

	// InvocationsToday is the count of Bedrock invocations through this endpoint
	// since UTC midnight.
	// +optional
	InvocationsToday int64 `json:"invocationsToday,omitempty"`

	// TotalInputTokens is the lifetime count of input tokens processed.
	// +optional
	TotalInputTokens int64 `json:"totalInputTokens,omitempty"`

	// TotalOutputTokens is the lifetime count of output tokens generated.
	// +optional
	TotalOutputTokens int64 `json:"totalOutputTokens,omitempty"`

	// ServiceName is the name of the proxy Service created by the operator
	// in the same namespace as the ModelEndpoint.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// ObservedGeneration is the .metadata.generation this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories=ai,shortName=me
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.modelId`
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="Available",type=boolean,JSONPath=`.status.available`
// +kubebuilder:printcolumn:name="Cost/Month",type=string,JSONPath=`.status.costThisMonth`
// +kubebuilder:printcolumn:name="Invocations",type=integer,JSONPath=`.status.invocationsToday`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ModelEndpoint is the control-plane representation of an AWS Bedrock model.
// Creating one causes the operator to spin up a proxy Service in the same
// namespace; workloads POST JSON to that Service and get Bedrock responses back,
// with rate limiting, cost tracking, and guardrails applied transparently.
type ModelEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelEndpointSpec   `json:"spec,omitempty"`
	Status ModelEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelEndpointList contains a list of ModelEndpoint.
type ModelEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelEndpoint{}, &ModelEndpointList{})
}
