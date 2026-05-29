package bedrock

import (
	"testing"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name         string
		modelId      string
		inputTokens  int64
		outputTokens int64
		wantCost     float64
		wantErr      bool
	}{
		{
			name:         "haiku small invocation",
			modelId:      "anthropic.claude-haiku-4-5-20251001-v1:0",
			inputTokens:  1000,
			outputTokens: 200,
			// 1000/1M * $0.80 + 200/1M * $4.00 = $0.0008 + $0.0008 = $0.0016
			wantCost: 0.001600,
		},
		{
			name:         "sonnet million tokens",
			modelId:      "anthropic.claude-sonnet-4-6-20250514-v1:0",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			// $3.00 + $15.00 = $18.00
			wantCost: 18.00,
		},
		{
			name:         "opus expensive invocation",
			modelId:      "anthropic.claude-opus-4-20250514-v1:0",
			inputTokens:  50_000,
			outputTokens: 10_000,
			// 50000/1M * $15 + 10000/1M * $75 = $0.75 + $0.75 = $1.50
			wantCost: 1.50,
		},
		{
			name:         "nova micro tiny call",
			modelId:      "amazon.nova-micro-v1:0",
			inputTokens:  100,
			outputTokens: 50,
			// 100/1M * $0.035 + 50/1M * $0.14 = 0.0000035 + 0.000007 = 0.0000105
			wantCost: 0.0000105,
		},
		{
			name:         "embedding model zero output cost",
			modelId:      "cohere.embed-english-v3",
			inputTokens:  5000,
			outputTokens: 0,
			// 5000/1M * $0.10 = $0.0005
			wantCost: 0.000500,
		},
		{
			name:         "zero tokens",
			modelId:      "amazon.nova-lite-v1:0",
			inputTokens:  0,
			outputTokens: 0,
			wantCost:     0,
		},
		{
			name:         "unknown model returns error",
			modelId:      "unknown.model-v9:0",
			inputTokens:  100,
			outputTokens: 100,
			wantErr:      true,
		},
		{
			name:         "llama large model",
			modelId:      "meta.llama3-1-405b-instruct-v1:0",
			inputTokens:  10_000,
			outputTokens: 5_000,
			// 10000/1M * $5.32 + 5000/1M * $16.00 = $0.0532 + $0.080 = $0.1332
			wantCost: 0.1332,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateCost(tt.modelId, tt.inputTokens, tt.outputTokens)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CalculateCost(%q, %d, %d) error = %v, wantErr %v",
					tt.modelId, tt.inputTokens, tt.outputTokens, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			// Allow for floating-point rounding within 1 micro-dollar.
			const epsilon = 0.000001
			if diff := got - tt.wantCost; diff > epsilon || diff < -epsilon {
				t.Errorf("CalculateCost(%q, %d, %d) = %f, want %f (diff %f)",
					tt.modelId, tt.inputTokens, tt.outputTokens, got, tt.wantCost, diff)
			}
		})
	}
}

func TestCalculateCost_ErrorType(t *testing.T) {
	_, err := CalculateCost("does.not-exist-v1:0", 100, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var unknown ErrUnknownModel
	switch e := err.(type) {
	case ErrUnknownModel:
		unknown = e
	default:
		t.Fatalf("expected ErrUnknownModel, got %T: %v", err, err)
	}

	if unknown.ModelId != "does.not-exist-v1:0" {
		t.Errorf("ErrUnknownModel.ModelId = %q, want %q", unknown.ModelId, "does.not-exist-v1:0")
	}
}

func TestKnownModel(t *testing.T) {
	if !KnownModel("anthropic.claude-3-haiku-20240307-v1:0") {
		t.Error("expected claude haiku to be known")
	}
	if KnownModel("fake.model-v0:0") {
		t.Error("expected fake model to be unknown")
	}
}

func TestPricingCoverage(t *testing.T) {
	// Sanity check that all pricing entries have positive input cost.
	// Output cost can be zero (embeddings).
	for modelId, p := range Pricing {
		if p.InputPer1M < 0 {
			t.Errorf("model %q has negative InputPer1M: %f", modelId, p.InputPer1M)
		}
		if p.OutputPer1M < 0 {
			t.Errorf("model %q has negative OutputPer1M: %f", modelId, p.OutputPer1M)
		}
	}
}
