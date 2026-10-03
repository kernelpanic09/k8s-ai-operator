package controller

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
)

func TestBuildContentPolicy(t *testing.T) {
	spec := &aiv1alpha1.ContentPolicyConfig{
		Filters: []aiv1alpha1.ContentFilter{
			{
				Type: "SEXUAL",
				ContentFilterConfig: aiv1alpha1.ContentFilterConfig{
					InputStrength:  "HIGH",
					OutputStrength: "MEDIUM",
				},
			},
			{
				Type: "HATE",
				ContentFilterConfig: aiv1alpha1.ContentFilterConfig{
					InputStrength:  "LOW",
					OutputStrength: "NONE",
				},
			},
		},
	}

	got := buildContentPolicy(spec)

	if len(got.FiltersConfig) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(got.FiltersConfig))
	}
	if got.FiltersConfig[0].Type != types.GuardrailContentFilterTypeSexual {
		t.Errorf("filter 0 type = %v, want SEXUAL", got.FiltersConfig[0].Type)
	}
	if got.FiltersConfig[0].InputStrength != types.GuardrailFilterStrengthHigh {
		t.Errorf("filter 0 input strength = %v, want HIGH", got.FiltersConfig[0].InputStrength)
	}
	if got.FiltersConfig[1].Type != types.GuardrailContentFilterTypeHate {
		t.Errorf("filter 1 type = %v, want HATE", got.FiltersConfig[1].Type)
	}
	if got.FiltersConfig[1].OutputStrength != types.GuardrailFilterStrengthNone {
		t.Errorf("filter 1 output strength = %v, want NONE", got.FiltersConfig[1].OutputStrength)
	}
}

func TestBuildContentPolicyEmpty(t *testing.T) {
	got := buildContentPolicy(&aiv1alpha1.ContentPolicyConfig{})
	if len(got.FiltersConfig) != 0 {
		t.Errorf("expected no filters for empty spec, got %d", len(got.FiltersConfig))
	}
}

func TestBuildTopicPolicy(t *testing.T) {
	spec := &aiv1alpha1.TopicPolicyConfig{
		Topics: []aiv1alpha1.DeniedTopic{
			{
				Name:       "legal-advice",
				Definition: "Requests for legal advice or representation.",
				Examples:   []string{"Can I sue my landlord?"},
			},
		},
	}

	got := buildTopicPolicy(spec)

	if len(got.TopicsConfig) != 1 {
		t.Fatalf("expected 1 topic, got %d", len(got.TopicsConfig))
	}
	topic := got.TopicsConfig[0]
	if aws.ToString(topic.Name) != "legal-advice" {
		t.Errorf("name = %q, want %q", aws.ToString(topic.Name), "legal-advice")
	}
	if aws.ToString(topic.Definition) != "Requests for legal advice or representation." {
		t.Errorf("definition = %q, want the spec definition", aws.ToString(topic.Definition))
	}
	if topic.Type != types.GuardrailTopicTypeDeny {
		t.Errorf("type = %v, want DENY (only supported topic type)", topic.Type)
	}
	if len(topic.Examples) != 1 || topic.Examples[0] != "Can I sue my landlord?" {
		t.Errorf("examples = %v, want [\"Can I sue my landlord?\"]", topic.Examples)
	}
}

func TestBuildWordPolicy(t *testing.T) {
	spec := &aiv1alpha1.WordPolicyConfig{
		Words:            []string{"foo", "bar"},
		ManagedWordLists: []string{"PROFANITY"},
	}

	got := buildWordPolicy(spec)

	if len(got.WordsConfig) != 2 {
		t.Fatalf("expected 2 words, got %d", len(got.WordsConfig))
	}
	if aws.ToString(got.WordsConfig[0].Text) != "foo" || aws.ToString(got.WordsConfig[1].Text) != "bar" {
		t.Errorf("words = %+v, want [foo bar] in order", got.WordsConfig)
	}
	if len(got.ManagedWordListsConfig) != 1 {
		t.Fatalf("expected 1 managed word list, got %d", len(got.ManagedWordListsConfig))
	}
	if got.ManagedWordListsConfig[0].Type != types.GuardrailManagedWordsTypeProfanity {
		t.Errorf("managed word list type = %v, want PROFANITY", got.ManagedWordListsConfig[0].Type)
	}
}

func TestBuildWordPolicyEmpty(t *testing.T) {
	got := buildWordPolicy(&aiv1alpha1.WordPolicyConfig{})
	if len(got.WordsConfig) != 0 || len(got.ManagedWordListsConfig) != 0 {
		t.Errorf("expected no words or managed lists for empty spec, got %+v", got)
	}
}

func TestBuildPIIPolicy(t *testing.T) {
	spec := &aiv1alpha1.SensitiveInformationPolicyConfig{
		PIIEntities: []aiv1alpha1.PIIEntityConfig{
			{Type: aiv1alpha1.PIIEntityType("EMAIL"), Action: aiv1alpha1.PIIActionBlock},
			{Type: aiv1alpha1.PIIEntityType("PHONE"), Action: aiv1alpha1.PIIActionAnonymize},
		},
	}

	got := buildPIIPolicy(spec)

	if len(got.PiiEntitiesConfig) != 2 {
		t.Fatalf("expected 2 PII entities, got %d", len(got.PiiEntitiesConfig))
	}
	if got.PiiEntitiesConfig[0].Type != types.GuardrailPiiEntityTypeEmail {
		t.Errorf("entity 0 type = %v, want EMAIL", got.PiiEntitiesConfig[0].Type)
	}
	if got.PiiEntitiesConfig[0].Action != types.GuardrailSensitiveInformationActionBlock {
		t.Errorf("entity 0 action = %v, want BLOCK", got.PiiEntitiesConfig[0].Action)
	}
	if got.PiiEntitiesConfig[1].Type != types.GuardrailPiiEntityTypePhone {
		t.Errorf("entity 1 type = %v, want PHONE", got.PiiEntitiesConfig[1].Type)
	}
	if got.PiiEntitiesConfig[1].Action != types.GuardrailSensitiveInformationActionAnonymize {
		t.Errorf("entity 1 action = %v, want ANONYMIZE", got.PiiEntitiesConfig[1].Action)
	}
}
