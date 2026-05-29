// Package bedrock wraps the AWS SDK v2 Bedrock clients with the operator's
// specific requirements: IRSA-based role assumption, per-endpoint client
// caching, and a unified interface that the controllers and proxy use.
package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// InvokeInput is what callers pass to Invoke. It maps loosely to the Bedrock
// Converse API so the proxy doesn't need to know about model-specific payloads.
type InvokeInput struct {
	ModelId      string
	SystemPrompt string
	Messages     []Message
	MaxTokens    int32
	Temperature  float64
}

// Message is a single turn in a conversation.
type Message struct {
	// Role is "user" or "assistant".
	Role    string `json:"role"`
	Content string `json:"content"`
}

// InvokeOutput holds the response from Bedrock plus token counts for billing.
type InvokeOutput struct {
	Content      string
	InputTokens  int64
	OutputTokens int64
	StopReason   string
}

// converseRequest mirrors the Bedrock Converse API request body.
// We assemble this ourselves rather than using the SDK type so the proxy can
// pass it through without tying callers to any one SDK version.
type converseRequest struct {
	ModelId         string             `json:"modelId"`
	Messages        []converseMessage  `json:"messages"`
	System          []systemContent    `json:"system,omitempty"`
	InferenceConfig *inferenceConfig   `json:"inferenceConfig,omitempty"`
}

type converseMessage struct {
	Role    string           `json:"role"`
	Content []messageContent `json:"content"`
}

type messageContent struct {
	Text string `json:"text"`
}

type systemContent struct {
	Text string `json:"text"`
}

type inferenceConfig struct {
	MaxTokens *int32  `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

// converseResponse is what Bedrock returns from InvokeModel when we send a
// Converse-shaped payload. This covers Claude and other models that support it.
type converseResponse struct {
	Output struct {
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	} `json:"output"`
	Usage struct {
		InputTokens  int64 `json:"inputTokens"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"usage"`
	StopReason string `json:"stopReason"`
}

// EndpointKey uniquely identifies the combination of AWS credentials and region
// we need a client for. One client per key is cached.
type EndpointKey struct {
	RoleArn string
	Region  string
}

// Client manages per-endpoint AWS clients with IRSA role assumption.
type Client struct {
	// baseConfig is the in-cluster pod identity config (IRSA ambient credentials).
	baseConfig aws.Config

	mu      sync.RWMutex
	runtime map[EndpointKey]*bedrockruntime.Client
	control map[EndpointKey]*awsbedrock.Client
}

// NewClient builds a Client using the ambient AWS credentials available in the
// pod (typically IRSA via the projected service account token).
func NewClient(ctx context.Context) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading ambient AWS config: %w", err)
	}

	return &Client{
		baseConfig: cfg,
		runtime:    make(map[EndpointKey]*bedrockruntime.Client),
		control:    make(map[EndpointKey]*awsbedrock.Client),
	}, nil
}

// runtimeClient returns a cached bedrockruntime.Client for the given role/region,
// creating one on first access. The role is assumed from the pod's ambient
// credentials, which must have sts:AssumeRole on the target role.
func (c *Client) runtimeClient(ctx context.Context, key EndpointKey) (*bedrockruntime.Client, error) {
	c.mu.RLock()
	if cl, ok := c.runtime[key]; ok {
		c.mu.RUnlock()
		return cl, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if cl, ok := c.runtime[key]; ok {
		return cl, nil
	}

	assumedCfg, err := c.assumeRoleConfig(ctx, key)
	if err != nil {
		return nil, err
	}

	cl := bedrockruntime.NewFromConfig(assumedCfg)
	c.runtime[key] = cl
	return cl, nil
}

// controlClient returns a cached awsbedrock.Client (control plane, not runtime).
func (c *Client) controlClient(ctx context.Context, key EndpointKey) (*awsbedrock.Client, error) {
	c.mu.RLock()
	if cl, ok := c.control[key]; ok {
		c.mu.RUnlock()
		return cl, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if cl, ok := c.control[key]; ok {
		return cl, nil
	}

	assumedCfg, err := c.assumeRoleConfig(ctx, key)
	if err != nil {
		return nil, err
	}

	cl := awsbedrock.NewFromConfig(assumedCfg)
	c.control[key] = cl
	return cl, nil
}

func (c *Client) assumeRoleConfig(ctx context.Context, key EndpointKey) (aws.Config, error) {
	stsClient := sts.NewFromConfig(c.baseConfig)
	creds := stscreds.NewAssumeRoleProvider(stsClient, key.RoleArn, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = "k8s-ai-operator"
	})

	assumedCfg := c.baseConfig.Copy()
	assumedCfg.Region = key.Region
	assumedCfg.Credentials = aws.NewCredentialsCache(creds)
	return assumedCfg, nil
}

// Invoke sends a Converse-shaped request to Bedrock and returns the response.
// It handles JSON marshaling/unmarshaling so callers work with typed structs.
func (c *Client) Invoke(ctx context.Context, key EndpointKey, input InvokeInput) (*InvokeOutput, error) {
	rt, err := c.runtimeClient(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("getting runtime client for %s/%s: %w", key.RoleArn, key.Region, err)
	}

	req := converseRequest{
		ModelId:  input.ModelId,
		Messages: make([]converseMessage, 0, len(input.Messages)),
	}

	for _, m := range input.Messages {
		req.Messages = append(req.Messages, converseMessage{
			Role:    m.Role,
			Content: []messageContent{{Text: m.Content}},
		})
	}

	if input.SystemPrompt != "" {
		req.System = []systemContent{{Text: input.SystemPrompt}}
	}

	if input.MaxTokens > 0 || input.Temperature > 0 {
		req.InferenceConfig = &inferenceConfig{}
		if input.MaxTokens > 0 {
			req.InferenceConfig.MaxTokens = &input.MaxTokens
		}
		if input.Temperature > 0 {
			req.InferenceConfig.Temperature = &input.Temperature
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling converse request: %w", err)
	}

	resp, err := rt.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(input.ModelId),
		Body:        body,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("invoking model %s: %w", input.ModelId, err)
	}

	var out converseResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Errorf("unmarshaling bedrock response: %w", err)
	}

	text := ""
	if len(out.Output.Message.Content) > 0 {
		text = out.Output.Message.Content[0].Text
	}

	return &InvokeOutput{
		Content:      text,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		StopReason:   out.StopReason,
	}, nil
}

// InvalidateCache removes cached clients for a given key. Call this when an
// endpoint's roleArn or region changes so the next reconcile picks up fresh
// credentials.
func (c *Client) InvalidateCache(key EndpointKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.runtime, key)
	delete(c.control, key)
}
