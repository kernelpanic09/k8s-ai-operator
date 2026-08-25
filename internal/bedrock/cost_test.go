package bedrock

import (
	"strings"
	"testing"
)

func TestPricingForModel(t *testing.T) {
	tests := []struct {
		name    string
		modelId string
		wantOk  bool
		wantIn  float64
		wantOut float64
	}{
		{
			name:    "known model returns pricing",
			modelId: "meta.llama3-70b-instruct-v1:0",
			wantOk:  true,
			wantIn:  2.65,
			wantOut: 3.50,
		},
		{
			name:    "embedding model has zero output cost",
			modelId: "cohere.embed-multilingual-v3",
			wantOk:  true,
			wantIn:  0.10,
			wantOut: 0,
		},
		{
			name:    "unknown model returns false",
			modelId: "vendor.nonexistent-v0:0",
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := PricingForModel(tt.modelId)
			if ok != tt.wantOk {
				t.Fatalf("PricingForModel(%q) ok = %v; want %v", tt.modelId, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if p.InputPer1M != tt.wantIn {
				t.Errorf("InputPer1M = %v; want %v", p.InputPer1M, tt.wantIn)
			}
			if p.OutputPer1M != tt.wantOut {
				t.Errorf("OutputPer1M = %v; want %v", p.OutputPer1M, tt.wantOut)
			}
		})
	}
}

func TestErrUnknownModelError(t *testing.T) {
	const id = "acme.test-model-v1:0"
	msg := ErrUnknownModel{ModelId: id}.Error()
	if !strings.Contains(msg, id) {
		t.Errorf("error message %q does not contain model ID %q", msg, id)
	}
	if !strings.Contains(msg, "cost tracking disabled") {
		t.Errorf("error message %q missing expected phrase 'cost tracking disabled'", msg)
	}
}
