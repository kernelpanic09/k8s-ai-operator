package bedrock

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
)

// GuardrailClient wraps the Bedrock control-plane client with guardrail-specific
// operations. It uses a fixed (roleArn, region) pair, unlike the main Client
// which caches one client per endpoint. GuardrailPolicies are cluster-scoped,
// so there's typically one GuardrailClient per operator deployment.
type GuardrailClient struct {
	inner *awsbedrock.Client
}

// NewGuardrailClient builds a GuardrailClient. If key.RoleArn is empty, the
// operator's ambient IRSA credentials are used directly (no role assumption).
// The returned client is not cached; callers hold it for the operator's lifetime.
func (c *Client) NewGuardrailClient(ctx context.Context, key EndpointKey) (*GuardrailClient, error) {
	var inner *awsbedrock.Client
	if key.RoleArn == "" {
		// Use the ambient credentials directly.
		cfg := c.baseConfig // aws.Config is a value type; copying is safe.
		if key.Region != "" {
			cfg.Region = key.Region
		}
		inner = awsbedrock.NewFromConfig(cfg)
	} else {
		var err error
		inner, err = c.controlClient(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("building guardrail client for %s/%s: %w", key.RoleArn, key.Region, err)
		}
	}
	return &GuardrailClient{inner: inner}, nil
}

func (g *GuardrailClient) CreateGuardrail(
	ctx context.Context,
	input *awsbedrock.CreateGuardrailInput,
	opts ...func(*awsbedrock.Options),
) (*awsbedrock.CreateGuardrailOutput, error) {
	return g.inner.CreateGuardrail(ctx, input, opts...)
}

func (g *GuardrailClient) UpdateGuardrail(
	ctx context.Context,
	input *awsbedrock.UpdateGuardrailInput,
	opts ...func(*awsbedrock.Options),
) (*awsbedrock.UpdateGuardrailOutput, error) {
	return g.inner.UpdateGuardrail(ctx, input, opts...)
}

func (g *GuardrailClient) DeleteGuardrail(
	ctx context.Context,
	input *awsbedrock.DeleteGuardrailInput,
	opts ...func(*awsbedrock.Options),
) (*awsbedrock.DeleteGuardrailOutput, error) {
	return g.inner.DeleteGuardrail(ctx, input, opts...)
}

// defaultGuardrailBlockedMessage is used when GuardrailPolicySpec.BlockedInputMessage is empty.
const defaultGuardrailBlockedMessage = "Your request was blocked by the content policy."

// DefaultBlockedInput returns the blocked input message, falling back to the default.
func DefaultBlockedInput(msg string) *string {
	if msg == "" {
		return aws.String(defaultGuardrailBlockedMessage)
	}
	return aws.String(msg)
}
