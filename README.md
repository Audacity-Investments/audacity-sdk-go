# audacity-sdk-go

Go client for the [Audacity Investments](https://portal.audacityinvestments.com) API with an
**Amazon Bedrock Converse–shaped surface** — swap `bedrockruntime` for `audacityruntime`, change
the constructor, and keep the rest of your code.

- Module: `github.com/Audacity-Investments/audacity-sdk-go`
- Go 1.22+, **stdlib only** (`net/http`, `encoding/json`, `bufio`)
- Version: `0.1.0`

---

## Installation

```sh
go get github.com/Audacity-Investments/audacity-sdk-go
```

---

## Configuration

| Priority | Source |
|----------|--------|
| 1 | Explicit `Options` field |
| 2 | `AUDACITY_API_KEY` / `AUDACITY_BASE_URL` environment variables |
| 3 | Defaults: `baseURL=https://portal.audacityinvestments.com`, `timeout=120s`, `maxRetries=2` |

A missing API key causes `Converse`/`ConverseStream` to return `*types.MissingAPIKeyError`
immediately, before any network call.

| Variable | Purpose |
|----------|---------|
| `AUDACITY_API_KEY` | API key (`audacity_api_…`) |
| `AUDACITY_BASE_URL` | Override the default endpoint |

---

## Quickstart — Converse (non-streaming)

This mirrors the aws-sdk-go-v2 Bedrock `Converse` example.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Audacity-Investments/audacity-sdk-go"
    "github.com/Audacity-Investments/audacity-sdk-go/audacityruntime"
    "github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

func main() {
    // Reads AUDACITY_API_KEY from the environment.
    client := audacityruntime.New(audacityruntime.Options{})

    resp, err := client.Converse(context.Background(), &audacityruntime.ConverseInput{
        ModelId: audacity.String("gpt-5.4-mini"),
        Messages: []types.Message{{
            Role:    types.ConversationRoleUser,
            Content: []types.ContentBlock{
                &types.ContentBlockMemberText{Value: "Hello, world!"},
            },
        }},
        InferenceConfig: &types.InferenceConfiguration{
            MaxTokens:   audacity.Int32(500),
            Temperature: audacity.Float32(0.2),
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    out := resp.Output.(*types.ConverseOutputMemberMessage)
    text := out.Value.Content[0].(*types.ContentBlockMemberText).Value
    fmt.Println(text)

    fmt.Printf("stop=%s  input=%d  output=%d  latency=%dms\n",
        resp.StopReason,
        resp.Usage.InputTokens,
        resp.Usage.OutputTokens,
        resp.Metrics.LatencyMs,
    )
}
```

---

## Streaming example

```go
streamResp, err := client.ConverseStream(ctx, &audacityruntime.ConverseStreamInput{
    ModelId: audacity.String("gpt-5.4-mini"),
    Messages: []types.Message{{
        Role:    types.ConversationRoleUser,
        Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Tell me a story"}},
    }},
})
if err != nil {
    log.Fatal(err)
}

stream := streamResp.GetStream()
defer stream.Close()

for event := range stream.Events() {
    switch e := event.(type) {
    case *types.ConverseStreamOutputMemberContentBlockDelta:
        if td, ok := e.Value.Delta.(*types.ContentBlockDeltaMemberText); ok {
            fmt.Print(td.Value)
        }
    case *types.ConverseStreamOutputMemberMessageStop:
        fmt.Printf("\n[stop: %s]\n", e.Value.StopReason)
    case *types.ConverseStreamOutputMemberMetadata:
        fmt.Printf("[tokens in=%d out=%d latency=%dms]\n",
            e.Value.Usage.InputTokens,
            e.Value.Usage.OutputTokens,
            e.Value.Metrics.LatencyMs,
        )
    }
}
if err := stream.Err(); err != nil {
    log.Fatal(err)
}
```

---

## Migrating from `bedrockruntime`

```diff
 import (
-    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
-    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
+    "github.com/Audacity-Investments/audacity-sdk-go/audacityruntime"
+    "github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
 )

-client := bedrockruntime.NewFromConfig(cfg)
+client := audacityruntime.New(audacityruntime.Options{})  // reads AUDACITY_API_KEY

 resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
-    ModelId: aws.String("anthropic.claude-3-5-sonnet-20241022-v2:0"),
+    ModelId: audacity.String("gpt-5.4-mini"),
     Messages: []types.Message{{ … }},
 })
```

The input/output shapes, union-type members, and `errors.As` patterns are identical to
`aws-sdk-go-v2`.

---

## Error handling

All server-derived errors embed `types.APIError` and are reachable via `errors.As`:

```go
import (
    "errors"
    "github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

_, err := client.Converse(ctx, input)
switch {
case err == nil:
    // success
case errors.Is(err, &types.MissingAPIKeyError{}):
    log.Fatal("set AUDACITY_API_KEY")
default:
    var throttle *types.ThrottlingException
    var quota *types.ServiceQuotaExceededException
    var accessDenied *types.AccessDeniedException

    switch {
    case errors.As(err, &throttle):
        fmt.Printf("rate limited (retry-after=%v)\n", throttle.RetryAfterSeconds)
    case errors.As(err, &quota):
        fmt.Println("budget exhausted — will not retry")
    case errors.As(err, &accessDenied):
        fmt.Println("check your API key")
    default:
        log.Fatal(err)
    }
}
```

### Exception taxonomy

| Type | Retryable | Typical cause |
|------|-----------|---------------|
| `ValidationException` | no | Bad request |
| `AccessDeniedException` | no | Auth / authz failure |
| `ResourceNotFoundException` | no | Model not found |
| `ServiceQuotaExceededException` | no | Budget / quota exhausted |
| `ThrottlingException` | **yes** | Rate limited |
| `ModelTimeoutException` | **yes** | Model timeout |
| `ModelErrorException` | no | Model-level error |
| `ModelStreamErrorException` | no | Stream interrupted |
| `ServiceUnavailableException` | **yes** | 502/503/504 |
| `InternalServerException` | **yes** | 500 |
| `MissingAPIKeyError` | — | No key configured |
| `SdkError` | (network: yes) | Network / decode failure |

The SDK retries transient failures automatically (up to `Options.MaxRetries` additional
attempts, default 2) using jittered exponential backoff.  `BUDGET_EXCEEDED` (429) and
all 4xx non-408/429 errors are never retried.

---

## Tool use

```go
resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId: audacity.String("gpt-5.4-mini"),
    Messages: []types.Message{{
        Role:    types.ConversationRoleUser,
        Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Weather in London?"}},
    }},
    ToolConfig: &types.ToolConfiguration{
        Tools: []types.Tool{{
            ToolSpec: &types.ToolSpecification{
                Name:        "get_weather",
                Description: audacity.String("Returns current weather"),
                InputSchema: &types.ToolInputSchema{
                    Json: map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                            "city": map[string]interface{}{"type": "string"},
                        },
                        "required": []string{"city"},
                    },
                },
            },
        }},
        ToolChoice: &types.ToolChoiceMemberAuto{},
    },
})
// resp.StopReason == "tool_use"
// resp.Output.(*types.ConverseOutputMemberMessage).Value.Content[0].(*types.ContentBlockMemberToolUse)
```

---

## Images (vision models)

Bedrock-style image content blocks are supported in user messages. Pass raw
bytes (base64-encoded for you) or a URL (Audacity extension):

```go
imageBytes, err := os.ReadFile("chart.png")
if err != nil {
    log.Fatal(err)
}

resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId: audacity.String("gpt-5.5"),
    Messages: []types.Message{{
        Role: types.ConversationRoleUser,
        Content: []types.ContentBlock{
            &types.ContentBlockMemberText{Value: "What does this chart show?"},
            &types.ContentBlockMemberImage{Value: types.ImageBlock{
                Format: types.ImageFormatPng,
                Source: &types.ImageSourceMemberBytes{Value: imageBytes},
            }},
        },
    }},
})

// Or reference a hosted image directly (not available in Bedrock):
// Source: &types.ImageSourceMemberUrl{Value: "https://example.com/photo.jpg"}
```

`Format` is one of `ImageFormatPng`, `ImageFormatJpeg`, `ImageFormatGif`,
`ImageFormatWebp`. Use a vision-capable model.

---

## Options reference

```go
client := audacityruntime.New(audacityruntime.Options{
    APIKey:     "audacity_api_…",         // falls back to AUDACITY_API_KEY
    BaseURL:    "https://…",              // falls back to AUDACITY_BASE_URL, then default
    HTTPClient: &http.Client{…},          // custom transport / TLS config
    MaxRetries: 3,                        // additional attempts (0 = default 2 → 3 total;
                                          // audacityruntime.NoRetries disables retries)
    Timeout:    60 * time.Second,         // per-attempt timeout (0 = default 120s;
                                          // audacityruntime.NoTimeout disables it)
})
```

The timeout bounds each attempt's connection + headers, and — for `Converse` only —
the full response body. **Streams are never cut off by it**: for `ConverseStream` the
timeout applies only until response headers arrive, so long generations can run
indefinitely. To abort a stream, cancel the request `context` or call `stream.Close()`
— either unblocks the reader goroutine and releases the connection.

If you provide a custom `HTTPClient`, leave its `Timeout` unset — an `http.Client`
timeout bounds the entire response body and will kill long streams mid-generation.

---

## License

Copyright Audacity Investments. All rights reserved.
