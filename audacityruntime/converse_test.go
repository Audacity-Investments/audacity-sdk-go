package audacityruntime_test

// converse_test.go — conformance checklist items 1, 3 (non-streaming), 4, 7, 9, 10.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Audacity-Investments/audacity-sdk-go"
	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime"
	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// ─────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────

func newTestClient(t *testing.T, handler http.HandlerFunc) (*audacityruntime.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := audacityruntime.New(audacityruntime.Options{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		MaxRetries: 0,
	})
	return client, srv
}

func jsonResponse(t *testing.T, w http.ResponseWriter, status int, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 1 — Converse happy path
// ─────────────────────────────────────────────────────────────

func TestConverseHappyPath(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Validate headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "audacity-sdk-go/") {
			t.Errorf("User-Agent = %q, want audacity-sdk-go/…", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		// Validate request body includes the model field
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		if req["model"] != "gpt-5.4-mini" {
			t.Errorf("model = %v, want gpt-5.4-mini", req["model"])
		}
		// stream must not be set for non-streaming
		if _, ok := req["stream"]; ok {
			t.Errorf("expected no stream field in Converse request")
		}

		jsonResponse(t, w, 200, map[string]interface{}{
			"id":    "chatcmpl-1",
			"model": "gpt-5.4-mini",
			"choices": []map[string]interface{}{{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello there!",
				},
			}},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 3,
				"total_tokens":      13,
			},
		})
	}

	client, _ := newTestClient(t, handler)
	resp, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
		ModelId: audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{{
			Role:    types.ConversationRoleUser,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Hi"}},
		}},
		InferenceConfig: &types.InferenceConfiguration{MaxTokens: audacity.Int32(500)},
	})
	if err != nil {
		t.Fatalf("Converse returned error: %v", err)
	}

	// Output
	msgOut, ok := resp.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		t.Fatalf("Output is not *ConverseOutputMemberMessage, got %T", resp.Output)
	}
	if len(msgOut.Value.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	textBlock, ok := msgOut.Value.Content[0].(*types.ContentBlockMemberText)
	if !ok {
		t.Fatalf("content[0] is not text, got %T", msgOut.Value.Content[0])
	}
	if textBlock.Value != "Hello there!" {
		t.Errorf("text = %q, want Hello there!", textBlock.Value)
	}

	// StopReason
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}

	// Usage
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 13 {
		t.Errorf("Usage = %+v, want {10 3 13}", resp.Usage)
	}

	// Latency is measured client-side — just check it's positive
	if resp.Metrics == nil || resp.Metrics.LatencyMs < 0 {
		t.Errorf("Metrics = %v, want non-nil and non-negative latency", resp.Metrics)
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 3 (non-streaming) — tool round-trip
// ─────────────────────────────────────────────────────────────

func TestConverseToolRoundTrip(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parse request: %v", err)
		}

		// Verify tools were serialised correctly
		tools, ok := req["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Errorf("expected tools array in request, got %v", req["tools"])
		} else {
			tool := tools[0].(map[string]interface{})
			fn := tool["function"].(map[string]interface{})
			if fn["name"] != "get_weather" {
				t.Errorf("tool name = %v, want get_weather", fn["name"])
			}
		}

		// Verify tool_choice
		if req["tool_choice"] != "auto" {
			t.Errorf("tool_choice = %v, want auto", req["tool_choice"])
		}

		// Return a tool_calls response
		jsonResponse(t, w, 200, map[string]interface{}{
			"choices": []map[string]interface{}{{
				"index":         0,
				"finish_reason": "tool_calls",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]interface{}{{
						"id":   "call_abc",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "get_weather",
							"arguments": `{"city":"London"}`,
						},
					}},
				},
			}},
			"usage": map[string]interface{}{
				"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30,
			},
		})
	}

	client, _ := newTestClient(t, handler)
	resp, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
		ModelId: audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{{
			Role:    types.ConversationRoleUser,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Weather in London?"}},
		}},
		ToolConfig: &types.ToolConfiguration{
			Tools: []types.Tool{{
				ToolSpec: &types.ToolSpecification{
					Name:        "get_weather",
					Description: audacity.String("Get weather"),
					InputSchema: &types.ToolInputSchema{
						Json: map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
						},
					},
				},
			}},
			ToolChoice: &types.ToolChoiceMemberAuto{},
		},
	})
	if err != nil {
		t.Fatalf("Converse error: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}

	msgOut := resp.Output.(*types.ConverseOutputMemberMessage)
	if len(msgOut.Value.Content) == 0 {
		t.Fatal("expected content blocks")
	}
	tuBlock, ok := msgOut.Value.Content[0].(*types.ContentBlockMemberToolUse)
	if !ok {
		t.Fatalf("content[0] is %T, want *ContentBlockMemberToolUse", msgOut.Value.Content[0])
	}
	if tuBlock.Value.ToolUseId != "call_abc" {
		t.Errorf("ToolUseId = %q, want call_abc", tuBlock.Value.ToolUseId)
	}
	if tuBlock.Value.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", tuBlock.Value.Name)
	}
	inputMap, ok := tuBlock.Value.Input.(map[string]interface{})
	if !ok {
		t.Fatalf("Input is %T, want map", tuBlock.Value.Input)
	}
	if inputMap["city"] != "London" {
		t.Errorf("input.city = %v, want London", inputMap["city"])
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 4 — toolResult user turn → role:tool message
// ─────────────────────────────────────────────────────────────

func TestToolResultSerialization(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("parse request: %v", err)
		}

		messages, _ := req["messages"].([]interface{})
		// Expected: system (none), user text → messages[0] = user "Weather?"
		// assistant tool_call → messages[1]
		// tool result → messages[2] (role:tool)
		// user text (none in this case — only toolResult) → no extra user message
		var toolMsg map[string]interface{}
		for _, m := range messages {
			mm := m.(map[string]interface{})
			if mm["role"] == "tool" {
				toolMsg = mm
				break
			}
		}
		if toolMsg == nil {
			t.Error("expected a role:tool message, got none")
			t.Logf("messages: %+v", messages)
		} else {
			if toolMsg["tool_call_id"] != "call_xyz" {
				t.Errorf("tool_call_id = %v, want call_xyz", toolMsg["tool_call_id"])
			}
			if !strings.Contains(toolMsg["content"].(string), "sunny") {
				t.Errorf("tool content = %v, want to contain 'sunny'", toolMsg["content"])
			}
		}

		jsonResponse(t, w, 200, map[string]interface{}{
			"choices": []map[string]interface{}{{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "It is sunny in London.",
				},
			}},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}

	client, _ := newTestClient(t, handler)
	_, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
		ModelId: audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{
			{
				Role:    types.ConversationRoleUser,
				Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Weather?"}},
			},
			{
				Role: types.ConversationRoleAssistant,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
						ToolUseId: "call_xyz",
						Name:      "get_weather",
						Input:     map[string]interface{}{"city": "London"},
					}},
				},
			},
			{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolResult{Value: types.ToolResultBlock{
						ToolUseId: "call_xyz",
						Content: []types.ToolResultContentBlock{
							&types.ToolResultContentMemberText{Value: "It is sunny"},
						},
					}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Converse error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 7 — defensive unwrap (envelope regression)
// ─────────────────────────────────────────────────────────────

func TestDefensiveUnwrap(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Server returns a {success, data:{...}} envelope
		jsonResponse(t, w, 200, map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"id":    "chatcmpl-wrapped",
				"model": "gpt-5.4-mini",
				"choices": []map[string]interface{}{{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Unwrapped correctly",
					},
				}},
				"usage": map[string]interface{}{
					"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7,
				},
			},
		})
	}

	client, _ := newTestClient(t, handler)
	resp, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Converse error: %v", err)
	}
	msgOut := resp.Output.(*types.ConverseOutputMemberMessage)
	text := msgOut.Value.Content[0].(*types.ContentBlockMemberText).Value
	if text != "Unwrapped correctly" {
		t.Errorf("text = %q, want Unwrapped correctly", text)
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 9 — missing API key
// ─────────────────────────────────────────────────────────────

func TestMissingAPIKey(t *testing.T) {
	// Ensure AUDACITY_API_KEY env var doesn't satisfy the key requirement.
	t.Setenv("AUDACITY_API_KEY", "")
	client := audacityruntime.New(audacityruntime.Options{
		APIKey:  "",
		BaseURL: "http://127.0.0.1:0",
	})
	_, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "hi"}}}},
	})
	var missingKey *types.MissingAPIKeyError
	if !errors.As(err, &missingKey) {
		t.Errorf("expected MissingAPIKeyError, got %T: %v", err, err)
	}
}

// ─────────────────────────────────────────────────────────────
// Checklist item 10 — User-Agent header
// ─────────────────────────────────────────────────────────────

func TestUserAgent(t *testing.T) {
	var gotUA string
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		jsonResponse(t, w, 200, map[string]interface{}{
			"choices": []map[string]interface{}{{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]interface{}{"role": "assistant", "content": "ok"},
			}},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}
	client, _ := newTestClient(t, handler)
	client.Converse(context.Background(), &audacityruntime.ConverseInput{ //nolint:errcheck
		ModelId:  audacity.String("gpt-5.4-mini"),
		Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "hi"}}}},
	})
	if !strings.HasPrefix(gotUA, "audacity-sdk-go/") {
		t.Errorf("User-Agent = %q, want prefix audacity-sdk-go/", gotUA)
	}
}

// ─────────────────────────────────────────────────────────────
// Additional — system message serialisation
// ─────────────────────────────────────────────────────────────

func TestSystemMessages(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req) //nolint:errcheck
		messages := req["messages"].([]interface{})
		first := messages[0].(map[string]interface{})
		if first["role"] != "system" {
			t.Errorf("first message role = %v, want system", first["role"])
		}
		if !strings.Contains(first["content"].(string), "helpful") {
			t.Errorf("system content = %v, want to contain 'helpful'", first["content"])
		}

		jsonResponse(t, w, 200, map[string]interface{}{
			"choices": []map[string]interface{}{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]interface{}{"role": "assistant", "content": "Sure"},
			}},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 1, "total_tokens": 6},
		})
	}
	client, _ := newTestClient(t, handler)
	client.Converse(context.Background(), &audacityruntime.ConverseInput{ //nolint:errcheck
		ModelId: audacity.String("gpt-5.4-mini"),
		System:  []types.SystemContentBlock{{Text: "You are helpful"}, {Text: "Be concise"}},
		Messages: []types.Message{{
			Role:    types.ConversationRoleUser,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "hi"}},
		}},
	})
}

// ─────────────────────────────────────────────────────────────
// Additional — additionalModelRequestFields shallow merge
// ─────────────────────────────────────────────────────────────

func TestAdditionalModelRequestFields(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req) //nolint:errcheck
		if req["custom_field"] != "custom_value" {
			t.Errorf("custom_field = %v, want custom_value", req["custom_field"])
		}
		jsonResponse(t, w, 200, map[string]interface{}{
			"choices": []map[string]interface{}{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]interface{}{"role": "assistant", "content": "ok"},
			}},
			"usage": map[string]interface{}{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}
	client, _ := newTestClient(t, handler)
	client.Converse(context.Background(), &audacityruntime.ConverseInput{ //nolint:errcheck
		ModelId:                      audacity.String("gpt-5.4-mini"),
		Messages:                     []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "hi"}}}},
		AdditionalModelRequestFields: map[string]interface{}{"custom_field": "custom_value"},
	})
}
