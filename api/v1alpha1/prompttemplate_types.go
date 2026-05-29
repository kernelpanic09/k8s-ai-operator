package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemplateVariableType enumerates supported variable types for PromptTemplates.
// +kubebuilder:validation:Enum=string;integer;boolean
type TemplateVariableType string

const (
	TemplateVariableTypeString  TemplateVariableType = "string"
	TemplateVariableTypeInteger TemplateVariableType = "integer"
	TemplateVariableTypeBoolean TemplateVariableType = "boolean"
)

// TemplateVariable defines a single interpolation variable in a PromptTemplate.
type TemplateVariable struct {
	// Name is the variable name as it appears in the template, e.g. "Diff" for
	// a template referencing {{.Diff}}.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_]*$`
	Name string `json:"name"`

	// Type determines how the value is validated and serialized.
	// +kubebuilder:default=string
	Type TemplateVariableType `json:"type"`

	// Required marks this variable as mandatory at render time.
	// +optional
	Required bool `json:"required,omitempty"`

	// Default is used when the variable is not supplied at render time.
	// Ignored when Required is true.
	// +optional
	Default string `json:"default,omitempty"`

	// Description is surfaced in the CRD reference docs and OpenAPI schema.
	// +optional
	Description string `json:"description,omitempty"`
}

// PromptTemplateSpec describes the template content and its rendering context.
type PromptTemplateSpec struct {
	// Template is the Go text/template body. Variables are referenced as
	// {{.VarName}}. The rendered output is sent as the user message to Bedrock.
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// Variables declares the interpolation variables the template uses.
	// The operator validates that all Required variables are present before
	// forwarding a render request to Bedrock.
	// +optional
	// +listType=map
	// +listMapKey=name
	Variables []TemplateVariable `json:"variables,omitempty"`

	// ModelEndpointRef names the ModelEndpoint in the same namespace that
	// renders of this template will route through.
	ModelEndpointRef ModelEndpointRef `json:"modelEndpointRef"`

	// SystemPrompt is prepended as the system message before the rendered
	// template. Optional; many models benefit from a strong system prompt.
	// +optional
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// MaxTokens overrides the ModelEndpoint's maxTokens for renders of this
	// template. Must be <= the endpoint's configured limit.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=131072
	// +optional
	MaxTokens *int32 `json:"maxTokens,omitempty"`
}

// PromptTemplateStatus is written by the controller.
type PromptTemplateStatus struct {
	// Conditions holds standard condition types.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RenderCount is the lifetime count of successful renders through this template.
	// +optional
	RenderCount int64 `json:"renderCount,omitempty"`

	// ObservedGeneration mirrors metadata.generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories=ai,shortName=pt
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.modelEndpointRef.name`
// +kubebuilder:printcolumn:name="Variables",type=integer,JSONPath=`.spec.variables[*]`,priority=1
// +kubebuilder:printcolumn:name="Renders",type=integer,JSONPath=`.status.renderCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PromptTemplate is a versioned, parameterized prompt that routes renders
// through a named ModelEndpoint. Templates use Go's text/template syntax.
// Render requests hit the ModelEndpoint proxy with variables interpolated,
// meaning all rate limiting, cost tracking, and guardrails apply automatically.
type PromptTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PromptTemplateSpec   `json:"spec,omitempty"`
	Status PromptTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PromptTemplateList contains a list of PromptTemplate.
type PromptTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PromptTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PromptTemplate{}, &PromptTemplateList{})
}
