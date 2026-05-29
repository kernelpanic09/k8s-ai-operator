package bedrock

import (
	"context"
	"fmt"

	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
)

// CheckModelAvailability returns nil if the model is available in the given
// region under the assumed role, and a descriptive error otherwise.
//
// We use GetFoundationModel (control plane, not runtime) so this doesn't
// incur any inference cost. The call also validates that the IRSA role has
// sufficient permissions before any traffic reaches the endpoint.
func (c *Client) CheckModelAvailability(ctx context.Context, key EndpointKey, modelId string) error {
	ctrl, err := c.controlClient(ctx, key)
	if err != nil {
		return fmt.Errorf("building control-plane client: %w", err)
	}

	_, err = ctrl.GetFoundationModel(ctx, &awsbedrock.GetFoundationModelInput{
		ModelIdentifier: &modelId,
	})
	if err != nil {
		return fmt.Errorf("model %q not available in %s: %w", modelId, key.Region, err)
	}

	return nil
}

// HealthResult is returned by HealthCheck and includes enough detail for status
// conditions without requiring callers to inspect raw AWS errors.
type HealthResult struct {
	Available bool
	Reason    string
	Message   string
}

// HealthCheck runs CheckModelAvailability and wraps the result in a
// HealthResult suitable for setting K8s status conditions.
func (c *Client) HealthCheck(ctx context.Context, key EndpointKey, modelId string) HealthResult {
	err := c.CheckModelAvailability(ctx, key, modelId)
	if err != nil {
		return HealthResult{
			Available: false,
			Reason:    "BedrockUnavailable",
			Message:   err.Error(),
		}
	}

	return HealthResult{
		Available: true,
		Reason:    "ModelAvailable",
		Message:   fmt.Sprintf("model %q is available in %s", modelId, key.Region),
	}
}
