package audacityruntime

// converse_stream.go — ConverseStream operation, SSE reader, and the §3 state machine.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// ────────────────────────────────────────────────────────────────────────────
// Public types
// ────────────────────────────────────────────────────────────────────────────

// ConverseStreamInput is the input to the ConverseStream operation.
// It has the same fields as ConverseInput.
type ConverseStreamInput struct {
	// ModelId is the model identifier.  Required.
	ModelId *string

	// Messages is the ordered conversation history.  Required; min 1 element.
	Messages []types.Message

	// System is zero or more system-prompt content blocks.
	System []types.SystemContentBlock

	// InferenceConfig overrides inference parameters.
	InferenceConfig *types.InferenceConfiguration

	// ToolConfig provides tool definitions and the tool-choice policy.
	ToolConfig *types.ToolConfiguration

	// AdditionalModelRequestFields is shallow-merged into the request body last.
	AdditionalModelRequestFields map[string]interface{}
}

// ConverseStreamOutput is returned by ConverseStream.  Call GetStream() to obtain
// the event channel.
type ConverseStreamOutput struct {
	stream *ConverseStreamEventStream
}

// GetStream returns the ConverseStreamEventStream for consuming events.
func (o *ConverseStreamOutput) GetStream() *ConverseStreamEventStream {
	return o.stream
}

// ConverseStreamEventStream provides aws-sdk-go-v2–style access to the stream:
//
//	stream := resp.GetStream()
//	for event := range stream.Events() { … }
//	if err := stream.Err(); err != nil { … }
type ConverseStreamEventStream struct {
	events chan types.ConverseStreamOutput

	mu   sync.Mutex
	err  error
	once sync.Once
	done chan struct{}
	resp *http.Response
}

// Events returns a read-only channel that receives stream events in order.
// The channel is closed when the stream ends (normally or with an error).
func (s *ConverseStreamEventStream) Events() <-chan types.ConverseStreamOutput {
	return s.events
}

// Err returns any error that occurred during streaming.  Call this after
// the Events() channel has been drained (i.e. after a range loop exits).
func (s *ConverseStreamEventStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops the stream and releases the underlying HTTP response body.
// Safe to call more than once.
func (s *ConverseStreamEventStream) Close() error {
	s.once.Do(func() {
		close(s.done)
	})
	return s.resp.Body.Close()
}

// ────────────────────────────────────────────────────────────────────────────
// ConverseStream operation
// ────────────────────────────────────────────────────────────────────────────

// ConverseStream sends a streaming request and returns a ConverseStreamOutput
// whose embedded event stream delivers typed SSE events.
//
// Retries are applied only while waiting for the HTTP response headers; once
// the first SSE byte arrives no further retry is possible.
func (c *Client) ConverseStream(ctx context.Context, input *ConverseStreamInput) (*ConverseStreamOutput, error) {
	if c.options.APIKey == "" {
		return nil, &types.MissingAPIKeyError{}
	}

	// ConverseStreamInput and ConverseInput have identical underlying types,
	// so the conversion is legal and lets both operations share one builder.
	body, err := buildRequestBody((*ConverseInput)(input), true)
	if err != nil {
		return nil, &types.SdkError{Message: "failed to build request body", Err: err}
	}

	resp, startTime, err := c.doConverseStreamWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}

	stream := &ConverseStreamEventStream{
		events: make(chan types.ConverseStreamOutput, 64),
		done:   make(chan struct{}),
		resp:   resp,
	}

	go stream.pump(startTime)

	return &ConverseStreamOutput{stream: stream}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// SSE reader + §3 state machine
// ────────────────────────────────────────────────────────────────────────────

var (
	sseDataPrefix = []byte("data:")
	sseDone       = []byte("[DONE]")
)

// pump runs in a goroutine: reads SSE lines from resp.Body, drives the state
// machine, and sends typed events on the channel.
func (s *ConverseStreamEventStream) pump(startTime time.Time) {
	defer close(s.events)

	scanner := bufio.NewScanner(s.resp.Body)
	// Small initial buffer; allow growth for events with big tool-input JSON.
	scanner.Buffer(make([]byte, 64<<10), 1<<20)

	sm := &streamSM{
		blocks: make(map[int]blockEntry),
	}

	for scanner.Scan() {
		// Honour Close() — discard remaining events without setting an error.
		select {
		case <-s.done:
			return
		default:
		}

		line := scanner.Bytes()

		// SSE parsing rules: skip blank lines and comment lines.
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue // ignore other SSE field types (event:, id:, retry:)
		}

		payload := line[len(sseDataPrefix):]
		if len(payload) > 0 && payload[0] == ' ' {
			payload = payload[1:] // strip optional single space
		}

		if bytes.Equal(payload, sseDone) {
			// Flush pending metadata as the final event.
			if ev := sm.finish(time.Since(startTime).Milliseconds()); ev != nil {
				s.send(ev)
			}
			return
		}

		var chunk oaiChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			s.setErr(&types.SdkError{Message: "failed to parse SSE chunk", Err: err})
			return
		}

		if err := sm.process(chunk, s); err != nil {
			s.setErr(err)
			return
		}
	}

	// scanner.Scan() returned false before [DONE] — either EOF or a read error.
	select {
	case <-s.done:
		return // intentional Close(); no error.
	default:
	}

	if scanErr := scanner.Err(); scanErr != nil {
		s.setErr(&types.ModelStreamErrorException{APIError: types.APIError{
			Message:   "stream read error: " + scanErr.Error(),
			ErrorCode: "STREAM_ERROR",
		}})
		return
	}

	// EOF without [DONE] = unexpected connection drop.
	s.setErr(&types.ModelStreamErrorException{APIError: types.APIError{
		Message:   "stream ended without [DONE]",
		ErrorCode: "STREAM_ERROR",
	}})
}

// send delivers an event, or returns immediately if Close() was called.
func (s *ConverseStreamEventStream) send(ev types.ConverseStreamOutput) {
	select {
	case s.events <- ev:
	case <-s.done:
	}
}

func (s *ConverseStreamEventStream) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// ────────────────────────────────────────────────────────────────────────────
// State machine  (spec §3 "OpenAI chunks → ConverseStream events")
// ────────────────────────────────────────────────────────────────────────────

type blockEntry struct {
	contentBlockIndex int32
	closed            bool
}

type streamSM struct {
	messageStarted bool
	// blocks keyed by the tool-call index (int); text block uses key -1.
	blocks       map[int]blockEntry
	nextIndex    int32
	pendingUsage *oaiUsage
}

const textBlockKey = -1

func (sm *streamSM) process(chunk oaiChunk, s *ConverseStreamEventStream) error {
	// Step 1 — inline stream error
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		return parseStreamError(chunk.Error)
	}

	var choice *oaiStreamChoice
	if len(chunk.Choices) > 0 {
		choice = &chunk.Choices[0]
	}

	// Step 2 — messageStart (once, on first chunk that carries a choice)
	if !sm.messageStarted && choice != nil {
		s.send(&types.ConverseStreamOutputMemberMessageStart{
			Value: types.MessageStartEvent{Role: "assistant"},
		})
		sm.messageStarted = true
	}

	if choice != nil {
		delta := choice.Delta

		// Step 3 — text delta
		if delta.Content != nil && *delta.Content != "" {
			entry, registered := sm.blocks[textBlockKey]
			if !registered {
				idx := sm.nextIndex
				sm.nextIndex++
				entry = blockEntry{contentBlockIndex: idx}
				sm.blocks[textBlockKey] = entry
				// Spec: no contentBlockStart event for text blocks.
			}
			s.send(&types.ConverseStreamOutputMemberContentBlockDelta{
				Value: types.ContentBlockDeltaEvent{
					ContentBlockIndex: entry.contentBlockIndex,
					Delta:             &types.ContentBlockDeltaMemberText{Value: *delta.Content},
				},
			})
		}

		// Step 4 — tool-call deltas
		for j, tc := range delta.ToolCalls {
			key := j
			if tc.Index != nil {
				key = *tc.Index
			}

			entry, registered := sm.blocks[key]
			if !registered {
				idx := sm.nextIndex
				sm.nextIndex++
				entry = blockEntry{contentBlockIndex: idx}
				sm.blocks[key] = entry

				s.send(&types.ConverseStreamOutputMemberContentBlockStart{
					Value: types.ContentBlockStartEvent{
						ContentBlockIndex: idx,
						Start: &types.ContentBlockStartMemberToolUse{
							Value: types.ToolUseStart{
								ToolUseId: tc.ID,
								Name:      tc.Function.Name,
							},
						},
					},
				})
			}

			if tc.Function.Arguments != "" {
				s.send(&types.ConverseStreamOutputMemberContentBlockDelta{
					Value: types.ContentBlockDeltaEvent{
						ContentBlockIndex: entry.contentBlockIndex,
						Delta: &types.ContentBlockDeltaMemberToolUse{
							Value: types.ToolUseInputDelta{Input: tc.Function.Arguments},
						},
					},
				})
			}
		}

		// Step 5 — finish_reason → emit contentBlockStop* + messageStop
		if choice.FinishReason != nil {
			stopReason := mapFinishReason(choice.FinishReason)

			// Collect open blocks sorted by ascending contentBlockIndex.
			type kv struct {
				key   int
				index int32
			}
			var open []kv
			for k, b := range sm.blocks {
				if !b.closed {
					open = append(open, kv{k, b.contentBlockIndex})
				}
			}
			sort.Slice(open, func(i, j int) bool { return open[i].index < open[j].index })

			for _, o := range open {
				s.send(&types.ConverseStreamOutputMemberContentBlockStop{
					Value: types.ContentBlockStopEvent{ContentBlockIndex: o.index},
				})
				e := sm.blocks[o.key]
				e.closed = true
				sm.blocks[o.key] = e
			}

			s.send(&types.ConverseStreamOutputMemberMessageStop{
				Value: types.MessageStopEvent{StopReason: stopReason},
			})
		}
	}

	// Step 6 — usage chunk (often post-finish, before [DONE])
	if chunk.Usage != nil {
		sm.pendingUsage = chunk.Usage
	}

	return nil
}

// finish builds the final metadata event from the pending usage chunk (spec §3
// step 7 — flushed when [DONE] arrives), or returns nil if none was received.
func (sm *streamSM) finish(latencyMs int64) types.ConverseStreamOutput {
	if sm.pendingUsage == nil {
		return nil
	}
	return &types.ConverseStreamOutputMemberMetadata{
		Value: types.ConverseStreamMetadataEvent{
			Usage: &types.TokenUsage{
				InputTokens:  sm.pendingUsage.PromptTokens,
				OutputTokens: sm.pendingUsage.CompletionTokens,
				TotalTokens:  sm.pendingUsage.TotalTokens,
			},
			Metrics: &types.ConverseMetrics{LatencyMs: latencyMs},
		},
	}
}
