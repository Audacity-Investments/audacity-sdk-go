package audacityruntime

// mapping.go — normative §3 request/response translation between the
// Bedrock-shaped public API and the OpenAI wire format.

import (
	"encoding/json"
	"strings"

	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// ─────────────────────────────────────────────────────────────
// Internal OpenAI wire types
// ─────────────────────────────────────────────────────────────

type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OAI response/chunk structs

type oaiResponse struct {
	Choices []oaiChoice  `json:"choices"`
	Usage   *oaiUsage    `json:"usage,omitempty"`
	Data    *oaiResponse `json:"data,omitempty"` // defensive unwrap
}

type oaiChoice struct {
	Index        int     `json:"index"`
	Message      oaiMsg  `json:"message"`
	FinishReason *string `json:"finish_reason"`
}

type oaiMsg struct {
	Role      string        `json:"role"`
	Content   *string       `json:"content"` // nullable
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
}

// Stream-specific chunk types

type oaiChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
	Error   json.RawMessage   `json:"error,omitempty"`
}

type oaiStreamChoice struct {
	Index        int            `json:"index"`
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiStreamDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   *string             `json:"content,omitempty"`
	ToolCalls []oaiStreamToolCall `json:"tool_calls,omitempty"`
}

type oaiStreamToolCall struct {
	Index    *int              `json:"index,omitempty"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function oaiStreamFunction `json:"function"`
}

type oaiStreamFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ─────────────────────────────────────────────────────────────
// Bedrock → OpenAI request mapping  (spec §3 "Converse input → OpenAI request")
// ─────────────────────────────────────────────────────────────

// buildRequestBody translates a Converse-shaped input (ConverseStreamInput is
// converted by the caller — the two types are identical) into the OpenAI wire body.
func buildRequestBody(input *ConverseInput, stream bool) ([]byte, error) {
	modelId := ""
	if input.ModelId != nil {
		modelId = *input.ModelId
	}

	// Build the body as a map so additionalModelRequestFields can be shallow-merged last.
	body := map[string]interface{}{
		"model":    modelId,
		"messages": buildMessages(input),
	}

	// §3 rule 4 — inferenceConfig
	if ic := input.InferenceConfig; ic != nil {
		if ic.MaxTokens != nil {
			body["max_tokens"] = *ic.MaxTokens
		}
		if ic.Temperature != nil {
			body["temperature"] = *ic.Temperature
		}
		if ic.TopP != nil {
			body["top_p"] = *ic.TopP
		}
		if len(ic.StopSequences) > 0 {
			body["stop"] = ic.StopSequences
		}
	}

	// §3 rule 5 — toolConfig
	if tc := input.ToolConfig; tc != nil && len(tc.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(tc.Tools))
		for _, t := range tc.Tools {
			if t.ToolSpec == nil {
				continue
			}
			fn := map[string]interface{}{"name": t.ToolSpec.Name}
			if t.ToolSpec.Description != nil {
				fn["description"] = *t.ToolSpec.Description
			}
			if t.ToolSpec.InputSchema != nil {
				fn["parameters"] = t.ToolSpec.InputSchema.Json
			}
			tools = append(tools, map[string]interface{}{
				"type":     "function",
				"function": fn,
			})
		}
		body["tools"] = tools

		if tc.ToolChoice != nil {
			body["tool_choice"] = buildToolChoice(tc.ToolChoice)
		}
	}

	// §3 rule 7 — stream flag
	if stream {
		body["stream"] = true
	}

	// §3 rule 6 — additionalModelRequestFields shallow-merged last
	for k, v := range input.AdditionalModelRequestFields {
		body[k] = v
	}

	return json.Marshal(body)
}

func buildMessages(input *ConverseInput) []interface{} {
	var out []interface{}

	// §3 rule 2 — system blocks
	if len(input.System) > 0 {
		parts := make([]string, 0, len(input.System))
		for _, s := range input.System {
			parts = append(parts, s.Text)
		}
		out = append(out, map[string]interface{}{
			"role":    "system",
			"content": strings.Join(parts, "\n\n"),
		})
	}

	// §3 rule 3 — conversation messages
	for _, msg := range input.Messages {
		switch msg.Role {
		case types.ConversationRoleUser:
			// tool-result blocks first, then text block
			for _, block := range msg.Content {
				tr, ok := block.(*types.ContentBlockMemberToolResult)
				if !ok {
					continue
				}
				out = append(out, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tr.Value.ToolUseId,
					"content":      joinToolResultContent(tr.Value.Content),
				})
			}
			var textParts []string
			for _, block := range msg.Content {
				if tb, ok := block.(*types.ContentBlockMemberText); ok {
					textParts = append(textParts, tb.Value)
				}
			}
			if len(textParts) > 0 {
				out = append(out, map[string]interface{}{
					"role":    "user",
					"content": strings.Join(textParts, "\n"),
				})
			}

		case types.ConversationRoleAssistant:
			var textParts []string
			var toolCalls []map[string]interface{}
			for _, block := range msg.Content {
				switch b := block.(type) {
				case *types.ContentBlockMemberText:
					textParts = append(textParts, b.Value)
				case *types.ContentBlockMemberToolUse:
					args, _ := json.Marshal(b.Value.Input)
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   b.Value.ToolUseId,
						"type": "function",
						"function": map[string]interface{}{
							"name":      b.Value.Name,
							"arguments": string(args),
						},
					})
				}
			}
			// content is the joined text, or null if no text blocks
			var content interface{}
			if len(textParts) > 0 {
				content = strings.Join(textParts, "\n")
			}
			m := map[string]interface{}{
				"role":    "assistant",
				"content": content,
			}
			if len(toolCalls) > 0 {
				m["tool_calls"] = toolCalls
			}
			out = append(out, m)
		}
	}
	return out
}

func joinToolResultContent(content []types.ToolResultContentBlock) string {
	var sb strings.Builder
	for _, c := range content {
		switch v := c.(type) {
		case *types.ToolResultContentMemberText:
			sb.WriteString(v.Value)
		case *types.ToolResultContentMemberJson:
			b, _ := json.Marshal(v.Value)
			sb.Write(b)
		}
	}
	return sb.String()
}

func buildToolChoice(tc types.ToolChoice) interface{} {
	if tool, ok := tc.(*types.ToolChoiceMemberTool); ok {
		return map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": tool.Value.Name,
			},
		}
	}
	switch tc.(type) {
	case *types.ToolChoiceMemberAny:
		return "required"
	default: // *types.ToolChoiceMemberAuto and anything unrecognised
		return "auto"
	}
}

// ─────────────────────────────────────────────────────────────
// OpenAI response → Bedrock output mapping  (spec §3 "OpenAI response → Converse output")
// ─────────────────────────────────────────────────────────────

func parseConverseResponse(body []byte, latencyMs int64) (*ConverseOutput, error) {
	var resp oaiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &types.SdkError{Message: "failed to decode response", Err: err}
	}

	// Defensive unwrap rule (§1): if no choices but data.choices exists, use data.
	if len(resp.Choices) == 0 && resp.Data != nil && len(resp.Data.Choices) > 0 {
		resp = *resp.Data
	}

	if len(resp.Choices) == 0 {
		return nil, &types.SdkError{Message: "response contained no choices"}
	}

	choice := resp.Choices[0]
	var contentBlocks []types.ContentBlock

	if choice.Message.Content != nil && *choice.Message.Content != "" {
		contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{Value: *choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		var input interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = tc.Function.Arguments // raw string fallback
		}
		contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolUse{
			Value: types.ToolUseBlock{
				ToolUseId: tc.ID,
				Name:      tc.Function.Name,
				Input:     input,
			},
		})
	}

	msg := types.Message{
		Role:    types.ConversationRoleAssistant,
		Content: contentBlocks,
	}

	usage := &types.TokenUsage{}
	if resp.Usage != nil {
		usage.InputTokens = resp.Usage.PromptTokens
		usage.OutputTokens = resp.Usage.CompletionTokens
		usage.TotalTokens = resp.Usage.TotalTokens
	}

	return &ConverseOutput{
		Output:     &types.ConverseOutputMemberMessage{Value: msg},
		StopReason: mapFinishReason(choice.FinishReason),
		Usage:      usage,
		Metrics:    &types.ConverseMetrics{LatencyMs: latencyMs},
	}, nil
}

func mapFinishReason(reason *string) string {
	if reason == nil {
		return "end_turn"
	}
	switch *reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "content_filtered"
	default:
		return "end_turn"
	}
}
