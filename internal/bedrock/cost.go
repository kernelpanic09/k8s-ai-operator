package bedrock

import (
	"fmt"
	"math"
)

// ModelPricing holds the per-million-token rates for a Bedrock model.
// All prices are in USD. Rates are on-demand; provisioned throughput pricing
// differs and is not modeled here.
type ModelPricing struct {
	InputPer1M  float64
	OutputPer1M float64
}

// Pricing maps Bedrock model IDs to their on-demand token costs.
// Cross-region inference profiles share the underlying model's pricing.
// Prices current as of May 2026; check AWS Bedrock pricing page for updates.
//
// https://aws.amazon.com/bedrock/pricing/
var Pricing = map[string]ModelPricing{
	// Claude 4 family
	"anthropic.claude-opus-4-20250514-v1:0":     {InputPer1M: 15.00, OutputPer1M: 75.00},
	"anthropic.claude-sonnet-4-20250514-v1:0":   {InputPer1M: 3.00, OutputPer1M: 15.00},
	"anthropic.claude-sonnet-4-6-20250514-v1:0": {InputPer1M: 3.00, OutputPer1M: 15.00},

	// Claude 3.7 family
	"anthropic.claude-3-7-sonnet-20250219-v1:0": {InputPer1M: 3.00, OutputPer1M: 15.00},

	// Claude 3.5 family
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {InputPer1M: 0.80, OutputPer1M: 4.00},

	// Claude 3 family
	"anthropic.claude-3-opus-20240229-v1:0":   {InputPer1M: 15.00, OutputPer1M: 75.00},
	"anthropic.claude-3-sonnet-20240229-v1:0": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"anthropic.claude-3-haiku-20240307-v1:0":  {InputPer1M: 0.25, OutputPer1M: 1.25},

	// Claude Haiku 4.5 (as referenced in the CRD sample)
	"anthropic.claude-haiku-4-5-20251001-v1:0": {InputPer1M: 0.80, OutputPer1M: 4.00},

	// Meta Llama 3 family
	"meta.llama3-8b-instruct-v1:0":     {InputPer1M: 0.30, OutputPer1M: 0.60},
	"meta.llama3-70b-instruct-v1:0":    {InputPer1M: 2.65, OutputPer1M: 3.50},
	"meta.llama3-1-8b-instruct-v1:0":   {InputPer1M: 0.22, OutputPer1M: 0.22},
	"meta.llama3-1-70b-instruct-v1:0":  {InputPer1M: 0.72, OutputPer1M: 0.72},
	"meta.llama3-1-405b-instruct-v1:0": {InputPer1M: 5.32, OutputPer1M: 16.00},
	"meta.llama3-2-11b-instruct-v1:0":  {InputPer1M: 0.16, OutputPer1M: 0.16},
	"meta.llama3-2-90b-instruct-v1:0":  {InputPer1M: 2.00, OutputPer1M: 2.00},
	"meta.llama3-2-1b-instruct-v1:0":   {InputPer1M: 0.10, OutputPer1M: 0.10},
	"meta.llama3-2-3b-instruct-v1:0":   {InputPer1M: 0.15, OutputPer1M: 0.15},
	"meta.llama3-3-70b-instruct-v1:0":  {InputPer1M: 0.72, OutputPer1M: 0.72},

	// Amazon Titan family
	"amazon.titan-text-lite-v1":      {InputPer1M: 0.30, OutputPer1M: 0.40},
	"amazon.titan-text-express-v1":   {InputPer1M: 0.80, OutputPer1M: 1.60},
	"amazon.titan-text-premier-v1:0": {InputPer1M: 0.50, OutputPer1M: 1.50},

	// Amazon Nova family
	"amazon.nova-micro-v1:0":   {InputPer1M: 0.035, OutputPer1M: 0.14},
	"amazon.nova-lite-v1:0":    {InputPer1M: 0.06, OutputPer1M: 0.24},
	"amazon.nova-pro-v1:0":     {InputPer1M: 0.80, OutputPer1M: 3.20},
	"amazon.nova-premier-v1:0": {InputPer1M: 2.50, OutputPer1M: 12.50},

	// Mistral family
	"mistral.mistral-7b-instruct-v0:2":   {InputPer1M: 0.15, OutputPer1M: 0.20},
	"mistral.mixtral-8x7b-instruct-v0:1": {InputPer1M: 0.45, OutputPer1M: 0.70},
	"mistral.mistral-large-2402-v1:0":    {InputPer1M: 4.00, OutputPer1M: 12.00},
	"mistral.mistral-large-2407-v1:0":    {InputPer1M: 3.00, OutputPer1M: 9.00},
	"mistral.mistral-small-2402-v1:0":    {InputPer1M: 1.00, OutputPer1M: 3.00},

	// Cohere family
	"cohere.command-r-v1:0":         {InputPer1M: 0.50, OutputPer1M: 1.50},
	"cohere.command-r-plus-v1:0":    {InputPer1M: 3.00, OutputPer1M: 15.00},
	"cohere.command-text-v14":       {InputPer1M: 1.50, OutputPer1M: 2.00},
	"cohere.command-light-text-v14": {InputPer1M: 0.30, OutputPer1M: 0.60},
	"cohere.embed-english-v3":       {InputPer1M: 0.10, OutputPer1M: 0},
	"cohere.embed-multilingual-v3":  {InputPer1M: 0.10, OutputPer1M: 0},

	// AI21 family
	"ai21.jamba-1-5-mini-v1:0":  {InputPer1M: 0.20, OutputPer1M: 0.40},
	"ai21.jamba-1-5-large-v1:0": {InputPer1M: 2.00, OutputPer1M: 8.00},
}

// ErrUnknownModel is returned when a model ID isn't in the pricing table.
// Callers should treat this as non-fatal; cost tracking will be skipped
// rather than blocking the invocation.
type ErrUnknownModel struct {
	ModelId string
}

func (e ErrUnknownModel) Error() string {
	return fmt.Sprintf("no pricing data for model %q; cost tracking disabled for this invocation", e.ModelId)
}

// CalculateCost returns the USD cost for a Bedrock invocation.
// inputTokens and outputTokens come from the Bedrock response's usage field.
// The result is rounded to 6 decimal places (sub-cent precision).
func CalculateCost(modelId string, inputTokens, outputTokens int64) (float64, error) {
	p, ok := Pricing[modelId]
	if !ok {
		return 0, ErrUnknownModel{ModelId: modelId}
	}

	inputCost := float64(inputTokens) / 1_000_000.0 * p.InputPer1M
	outputCost := float64(outputTokens) / 1_000_000.0 * p.OutputPer1M
	total := inputCost + outputCost

	// Round to 6 decimal places to avoid floating-point noise accumulating
	// across many small invocations.
	return math.Round(total*1_000_000) / 1_000_000, nil
}

// KnownModel reports whether a model ID has pricing data.
func KnownModel(modelId string) bool {
	_, ok := Pricing[modelId]
	return ok
}

// PricingForModel returns the pricing entry for a model, if known.
func PricingForModel(modelId string) (ModelPricing, bool) {
	p, ok := Pricing[modelId]
	return p, ok
}
