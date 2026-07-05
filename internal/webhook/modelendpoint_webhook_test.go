package webhook

import (
	"strings"
	"testing"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
)

func TestValidateRegion(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		wantErr bool
	}{
		{name: "us-east-1 is valid", region: "us-east-1"},
		{name: "eu-central-1 is valid", region: "eu-central-1"},
		{name: "gov region is valid", region: "us-gov-west-1"},
		{name: "unknown region rejected", region: "us-west-1", wantErr: true},
		{name: "empty region rejected", region: "", wantErr: true},
		{name: "typo region rejected", region: "us-east-9", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegion(tt.region)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRegion(%q) error = %v, wantErr %v", tt.region, err, tt.wantErr)
			}
		})
	}
}

func TestValidateModelId(t *testing.T) {
	tests := []struct {
		name    string
		modelId string
		wantErr bool
	}{
		{name: "anthropic model accepted", modelId: "anthropic.claude-haiku-4-5-20251001-v1:0"},
		{name: "amazon model accepted", modelId: "amazon.nova-micro-v1:0"},
		{name: "meta model accepted", modelId: "meta.llama3-1-8b-instruct-v1:0"},
		{name: "unknown vendor rejected", modelId: "openai.gpt-4o", wantErr: true},
		{name: "empty rejected", modelId: "", wantErr: true},
		{name: "bare prefix with no model rejected", modelId: "anthropic.", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelId(tt.modelId)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateModelId(%q) error = %v, wantErr %v", tt.modelId, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBudget(t *testing.T) {
	tests := []struct {
		name    string
		budget  *aiv1alpha1.BudgetSpec
		wantErr bool
	}{
		{name: "daily only is valid", budget: &aiv1alpha1.BudgetSpec{Daily: "10.00"}},
		{name: "monthly only is valid", budget: &aiv1alpha1.BudgetSpec{Monthly: "250.00"}},
		{name: "both set is valid", budget: &aiv1alpha1.BudgetSpec{Daily: "10.00", Monthly: "250.00"}},
		{name: "both empty rejected", budget: &aiv1alpha1.BudgetSpec{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBudget(tt.budget)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBudget(%+v) error = %v, wantErr %v", tt.budget, err, tt.wantErr)
			}
		})
	}
}

// validSpec returns a ModelEndpoint that passes every validation check.
// Individual test cases mutate one field to exercise a single failure mode.
func validSpec() *aiv1alpha1.ModelEndpoint {
	return &aiv1alpha1.ModelEndpoint{
		Spec: aiv1alpha1.ModelEndpointSpec{
			ModelId:     "anthropic.claude-haiku-4-5-20251001-v1:0",
			Region:      "us-east-1",
			IRSARoleArn: "arn:aws:iam::123456789012:role/bedrock-invoke",
		},
	}
}

func TestValidateModelEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(ep *aiv1alpha1.ModelEndpoint)
		wantErr bool
	}{
		{
			name:   "fully valid endpoint",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {},
		},
		{
			name:    "invalid region",
			mutate:  func(ep *aiv1alpha1.ModelEndpoint) { ep.Spec.Region = "atlantis-1" },
			wantErr: true,
		},
		{
			name:    "invalid model id",
			mutate:  func(ep *aiv1alpha1.ModelEndpoint) { ep.Spec.ModelId = "acme.super-model" },
			wantErr: true,
		},
		{
			name:    "malformed IRSA ARN",
			mutate:  func(ep *aiv1alpha1.ModelEndpoint) { ep.Spec.IRSARoleArn = "not-an-arn" },
			wantErr: true,
		},
		{
			name: "gov-partition ARN accepted",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {
				ep.Spec.IRSARoleArn = "arn:aws-us-gov:iam::123456789012:role/bedrock-invoke"
				ep.Spec.Region = "us-gov-west-1"
			},
		},
		{
			name: "budget present but empty",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {
				ep.Spec.CostBudget = &aiv1alpha1.BudgetSpec{}
			},
			wantErr: true,
		},
		{
			name: "rate limit below minimum",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {
				ep.Spec.RateLimit = &aiv1alpha1.RateLimitSpec{RequestsPerMinute: 0}
			},
			wantErr: true,
		},
		{
			name: "rate limit above maximum",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {
				ep.Spec.RateLimit = &aiv1alpha1.RateLimitSpec{RequestsPerMinute: 5000}
			},
			wantErr: true,
		},
		{
			name: "rate limit within bounds",
			mutate: func(ep *aiv1alpha1.ModelEndpoint) {
				ep.Spec.RateLimit = &aiv1alpha1.RateLimitSpec{RequestsPerMinute: 120}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := validSpec()
			tt.mutate(ep)
			err := validateModelEndpoint(ep)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateModelEndpoint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateModelEndpoint_ErrorMentionsField ensures denial messages point the
// user at the offending field, since that text surfaces directly in kubectl output.
func TestValidateModelEndpoint_ErrorMentionsField(t *testing.T) {
	ep := validSpec()
	ep.Spec.Region = "atlantis-1"

	err := validateModelEndpoint(ep)
	if err == nil {
		t.Fatal("expected error for invalid region, got nil")
	}
	if !strings.Contains(err.Error(), "spec.region") {
		t.Errorf("error %q should reference spec.region", err.Error())
	}
}
