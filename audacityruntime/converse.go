package audacityruntime

import (
	"context"

	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// ConverseInput is the input to the Converse operation.
type ConverseInput struct {
	// ModelId is the model identifier (e.g. "gpt-5.4-mini").  Required.
	ModelId *string

	// Messages is the ordered conversation history.  Required; min 1 element.
	Messages []types.Message

	// System is zero or more system-prompt content blocks.
	System []types.SystemContentBlock

	// InferenceConfig overrides inference parameters.
	InferenceConfig *types.InferenceConfiguration

	// ToolConfig provides tool definitions and the tool-choice policy.
	ToolConfig *types.ToolConfiguration

	// AdditionalModelRequestFields is shallow-merged into the request body last,
	// allowing passthrough of model-specific parameters.
	AdditionalModelRequestFields map[string]interface{}
}

// ConverseOutput is the output of the Converse operation.
type ConverseOutput struct {
	// Output contains the assistant's response (currently always *types.ConverseOutputMemberMessage).
	Output types.ConverseOutput

	// StopReason is one of: "end_turn", "max_tokens", "tool_use",
	// "stop_sequence", "content_filtered".
	StopReason string

	// Usage reports prompt/completion/total token counts.
	Usage *types.TokenUsage

	// Metrics reports the end-to-end client-side latency.
	Metrics *types.ConverseMetrics
}

// Converse sends a non-streaming request to the Audacity API and returns the
// assistant's complete response.
//
// Returns types.MissingAPIKeyError if no API key has been configured.
// Retries transient failures up to Options.MaxRetries additional times.
func (c *Client) Converse(ctx context.Context, input *ConverseInput) (*ConverseOutput, error) {
	if c.options.APIKey == "" {
		return nil, &types.MissingAPIKeyError{}
	}

	body, err := buildRequestBody(input, false)
	if err != nil {
		return nil, &types.SdkError{Message: "failed to build request body", Err: err}
	}

	respBody, latencyMs, err := c.doConverseWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}

	return parseConverseResponse(respBody, latencyMs)
}
