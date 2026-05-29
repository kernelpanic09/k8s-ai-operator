package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContentFilterConfig maps to Bedrock's content policy configuration.
type ContentFilterConfig struct {
	// InputStrength controls how aggressively input content is filtered.
	// +kubebuilder:validation:Enum=NONE;LOW;MEDIUM;HIGH
	// +kubebuilder:default=MEDIUM
	InputStrength string `json:"inputStrength"`

	// OutputStrength controls filtering of model-generated content.
	// +kubebuilder:validation:Enum=NONE;LOW;MEDIUM;HIGH
	// +kubebuilder:default=MEDIUM
	OutputStrength string `json:"outputStrength"`
}

// ContentPolicyConfig mirrors Bedrock's ContentPolicyConfig structure.
type ContentPolicyConfig struct {
	// Filters defines per-category thresholds.
	// Supported categories: SEXUAL, VIOLENCE, HATE, INSULTS, MISCONDUCT,
	// PROMPT_ATTACK.
	// +optional
	// +listType=map
	// +listMapKey=type
	Filters []ContentFilter `json:"filters,omitempty"`
}

// ContentFilter is one entry in a ContentPolicyConfig.
type ContentFilter struct {
	// Type is the Bedrock content filter category name.
	// +kubebuilder:validation:Enum=SEXUAL;VIOLENCE;HATE;INSULTS;MISCONDUCT;PROMPT_ATTACK
	Type string `json:"type"`

	ContentFilterConfig `json:",inline"`
}

// PIIEntityType enumerates entity types for PII detection.
// +kubebuilder:validation:Enum=ADDRESS;AGE;AWS_ACCESS_KEY;AWS_SECRET_KEY;CA_HEALTH_NUMBER;CA_SOCIAL_INSURANCE_NUMBER;CREDIT_DEBIT_CARD_CVV;CREDIT_DEBIT_CARD_EXPIRY;CREDIT_DEBIT_CARD_NUMBER;DRIVER_ID;EMAIL;INTERNATIONAL_BANK_ACCOUNT_NUMBER;IP_ADDRESS;LICENSE_PLATE;MAC_ADDRESS;NAME;PASSWORD;PHONE;PIN;SWIFT_CODE;UK_NATIONAL_HEALTH_SERVICE_NUMBER;UK_NATIONAL_INSURANCE_NUMBER;UK_UNIQUE_TAXPAYER_REFERENCE_NUMBER;URL;USERNAME;US_BANK_ACCOUNT_NUMBER;US_BANK_ROUTING_NUMBER;US_INDIVIDUAL_TAX_IDENTIFICATION_NUMBER;US_PASSPORT_NUMBER;US_SOCIAL_SECURITY_NUMBER;VEHICLE_IDENTIFICATION_NUMBER
type PIIEntityType string

// PIIAction determines what happens when a PII entity is detected.
// +kubebuilder:validation:Enum=BLOCK;ANONYMIZE
type PIIAction string

const (
	PIIActionBlock     PIIAction = "BLOCK"
	PIIActionAnonymize PIIAction = "ANONYMIZE"
)

// PIIEntityConfig maps one PII entity type to an action.
type PIIEntityConfig struct {
	// Type is the PII entity category.
	Type PIIEntityType `json:"type"`

	// Action is applied when an entity of this type is detected.
	Action PIIAction `json:"action"`
}

// SensitiveInformationPolicyConfig configures PII detection and redaction.
type SensitiveInformationPolicyConfig struct {
	// PIIEntities is the list of PII categories to detect.
	// +optional
	// +listType=map
	// +listMapKey=type
	PIIEntities []PIIEntityConfig `json:"piiEntities,omitempty"`
}

// TopicPolicyConfig defines topics the guardrail should refuse to engage with.
type TopicPolicyConfig struct {
	// Topics is the list of denied topic definitions.
	// +optional
	Topics []DeniedTopic `json:"topics,omitempty"`
}

// DeniedTopic defines one topic the guardrail will block.
type DeniedTopic struct {
	// Name is a human-readable label for this topic.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Definition describes the topic so Bedrock can classify it.
	// +kubebuilder:validation:MinLength=1
	Definition string `json:"definition"`

	// Examples are optional sample phrases that illustrate the topic.
	// More examples improve classification accuracy.
	// +optional
	Examples []string `json:"examples,omitempty"`
}

// WordPolicyConfig blocks or filters specific words and phrases.
type WordPolicyConfig struct {
	// Words is a list of exact words to block.
	// +optional
	Words []string `json:"words,omitempty"`

	// ManagedWordListsConfig references Bedrock-managed word lists
	// (e.g. PROFANITY).
	// +optional
	// +kubebuilder:validation:Enum=PROFANITY
	ManagedWordLists []string `json:"managedWordLists,omitempty"`
}

// GuardrailPolicySpec describes the desired Bedrock Guardrail configuration.
type GuardrailPolicySpec struct {
	// Description is a human-readable summary shown in the Bedrock console
	// and in kubectl get guardrailpolicies output.
	// +kubebuilder:validation:MaxLength=200
	// +optional
	Description string `json:"description,omitempty"`

	// BlockedInputMessage is the message returned to callers when an input
	// is blocked. Defaults to a generic denial message if unset.
	// +kubebuilder:validation:MaxLength=500
	// +optional
	BlockedInputMessage string `json:"blockedInputMessage,omitempty"`

	// BlockedOutputsMessage is the message returned when model output is
	// blocked by the guardrail.
	// +kubebuilder:validation:MaxLength=500
	// +optional
	BlockedOutputsMessage string `json:"blockedOutputsMessage,omitempty"`

	// ContentPolicy configures per-category content filters.
	// +optional
	ContentPolicy *ContentPolicyConfig `json:"contentPolicy,omitempty"`

	// SensitiveInformationPolicy controls PII detection and handling.
	// +optional
	SensitiveInformationPolicy *SensitiveInformationPolicyConfig `json:"sensitiveInformationPolicy,omitempty"`

	// TopicPolicy defines topics the guardrail will refuse to engage with.
	// +optional
	TopicPolicy *TopicPolicyConfig `json:"topicPolicy,omitempty"`

	// WordPolicy blocks specific words or Bedrock-managed word lists.
	// +optional
	WordPolicy *WordPolicyConfig `json:"wordPolicy,omitempty"`
}

// GuardrailPolicyStatus is written by the controller after syncing to Bedrock.
type GuardrailPolicyStatus struct {
	// GuardrailId is the ID assigned by Bedrock after the guardrail is created.
	// This is referenced in Bedrock API calls.
	// +optional
	GuardrailId string `json:"guardrailId,omitempty"`

	// GuardrailVersion is the Bedrock-assigned version string.
	// +optional
	GuardrailVersion string `json:"guardrailVersion,omitempty"`

	// Conditions holds standard condition types.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration mirrors metadata.generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=ai,shortName=gp
// +kubebuilder:printcolumn:name="GuardrailId",type=string,JSONPath=`.status.guardrailId`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.guardrailVersion`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="GuardrailSynced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GuardrailPolicy is a cluster-scoped resource that mirrors a set of content
// moderation rules to Bedrock's Guardrails API. Once synced, ModelEndpoints
// can reference it by name; the operator passes the guardrailId and version in
// every Bedrock invocation through those endpoints.
type GuardrailPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailPolicySpec   `json:"spec,omitempty"`
	Status GuardrailPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuardrailPolicyList contains a list of GuardrailPolicy.
type GuardrailPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailPolicy{}, &GuardrailPolicyList{})
}
