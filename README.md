# audacity-sdk-go

Go client for the [Audacity Investments](https://portal.audacityinvestments.com) API with an
**Amazon Bedrock Converse–shaped surface** — swap `bedrockruntime` for `audacityruntime`, change
the constructor, and keep the rest of your code.

- Module: `github.com/Audacity-Investments/audacity-sdk-go`
- Go 1.22+, **stdlib only** (`net/http`, `encoding/json`, `bufio`)
- Version: `0.3.0`

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

## Video input

Bedrock-style video content blocks are supported in user messages. Pass raw
bytes; they are base64-encoded for you:

```go
videoBytes, err := os.ReadFile("demo.mp4")
if err != nil {
    log.Fatal(err)
}

resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId: audacity.String("gemini-2.5-flash"),
    Messages: []types.Message{{
        Role: types.ConversationRoleUser,
        Content: []types.ContentBlock{
            &types.ContentBlockMemberText{Value: "What happens in this video?"},
            &types.ContentBlockMemberVideo{Value: types.VideoBlock{
                Format: types.VideoFormatMp4,
                Source: &types.VideoSourceMemberBytes{Value: videoBytes},
            }},
        },
    }},
})
```

`Format` is one of `VideoFormatMp4`, `VideoFormatMov`, `VideoFormatMkv`,
`VideoFormatWebm`, `VideoFormatFlv`, `VideoFormatMpeg`, `VideoFormatMpg`,
`VideoFormatWmv`, `VideoFormatThreeGp`.

Video is **Gemini-only** at the gateway: `gemini-2.5-flash`, `gemini-2.5-pro`,
and `gemini-3-flash-preview` accept video input — every other model rejects it
with an HTTP 400. Inline video rides the request body, so keep raw video
≤ ~20 MB (base64 inflates it by ~33% against the gateway's body cap).

### Media resolution (cheaper video tokens)

Set `MediaResolution` on the request to control how video/image input is
tokenized — `types.MediaResolutionLow` cuts video token cost roughly **4x**
(Gemini models; other models ignore the field). Accepted values:
`MediaResolutionLow`, `MediaResolutionMedium`, `MediaResolutionHigh`,
`MediaResolutionUltraHigh`. When unset, the field is omitted and the model's
default applies.

```go
resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId:         audacity.String("gemini-2.5-flash"),
    MediaResolution: types.MediaResolutionLow, // ~4x cheaper video tokens
    Messages: []types.Message{{
        Role: types.ConversationRoleUser,
        Content: []types.ContentBlock{
            &types.ContentBlockMemberText{Value: "What happens in this video?"},
            &types.ContentBlockMemberVideo{Value: types.VideoBlock{
                Format: types.VideoFormatMp4,
                Source: &types.VideoSourceMemberBytes{Value: videoBytes},
            }},
        },
    }},
})
```

The same field exists on `ConverseStreamInput`.

### Large videos: upload first, then reference by URI

For videos over ~20 MB (up to **1 GB**), upload once with `UploadFile` and
reference the returned `audacity://files/…` URI — the analogue of Bedrock
Converse's `s3Location` video source:

```go
videoBytes, err := os.ReadFile("large-demo.mp4")
if err != nil {
    log.Fatal(err)
}

upload, err := client.UploadFile(ctx, &audacityruntime.UploadFileInput{
    Data:        videoBytes,
    ContentType: "video/mp4",
})
if err != nil {
    log.Fatal(err)
}

resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId: audacity.String("gemini-2.5-flash"),
    Messages: []types.Message{{
        Role: types.ConversationRoleUser,
        Content: []types.ContentBlock{
            &types.ContentBlockMemberText{Value: "Summarise this video."},
            &types.ContentBlockMemberVideo{Value: types.VideoBlock{
                Format: types.VideoFormatMp4,
                Source: &types.VideoSourceMemberURI{Value: upload.Uri},
            }},
        },
    }},
})
```

`UploadFile` performs the two-step §6 flow for you: a `POST /v1/files` for a
presigned upload ticket (same auth, error mapping, and retry policy as
`Converse`), then a **resumable upload** of the bytes (GCS resumable-session
protocol): the file is sent in 8 MiB chunks, and on a network failure or
transient server error (5xx/429) the SDK queries the session for the last
confirmed byte and **automatically resumes from there** — up to 5 recovery
attempts, with the budget resetting every time a chunk is confirmed. Other
4xx errors fail immediately. Notes:

- `ContentType` must be one of the video MIME types (`video/mp4`,
  `video/mov`, `video/webm`, …); `Data` is capped at **1 GB**.
- Uploaded files are transient inference inputs: they auto-delete after
  **~24 hours**, so upload shortly before use and re-upload for later
  sessions.
- The presigned upload URL itself expires after **~15 minutes**
  (`UploadFileOutput.ExpiresAt`); `UploadFile` uses it immediately, so this
  only matters if you inspect the ticket yourself.
- Files are namespaced per API key's client — a URI from one client is a
  400 `file not found` for another.

---

## Image generation

Generate images from a text prompt with `GenerateImage`:

```go
out, err := client.GenerateImage(ctx, &audacityruntime.GenerateImageInput{
    Model:          audacity.String("gpt-image-1"),
    Prompt:         audacity.String("A watercolor painting of a fox in a snowy forest"),
    Size:           audacity.String("1024x1024"),
    ResponseFormat: audacity.String("b64_json"), // or "url" (default) for a signed link
})
if err != nil {
    log.Fatal(err)
}

img, err := base64.StdEncoding.DecodeString(out.Data[0].B64Json)
if err != nil {
    log.Fatal(err)
}
os.WriteFile("fox.png", img, 0o644)
// With ResponseFormat "url": out.Data[0].Url (signed link, expires ~24 h)
```

Optional fields: `N` (1–10 images), `Size` (`"WxH"`, model-dependent),
`Quality` (e.g. `"standard"`, `"hd"`), and `User`. The output carries
`Created`, `Data` (each entry has `Url` or `B64Json`, plus `RevisedPrompt`
when the provider rewrites your prompt) and optional `Usage` token counts.
Errors map to the same typed errors as `Converse` (401 →
`*types.AccessDeniedException`, 429 → `*types.ThrottlingException`, spend cap
→ `*types.ServiceQuotaExceededException`), usable with `errors.As`.

---

## Prompt caching

Place a Bedrock-style cache-point block after the stable prefix you want the
provider to cache (system prompt, large documents). Everything up to the cache
point is cached provider-side on Claude models; OpenAI/Gemini models cache
automatically and ignore the marker. At most 4 cache points per request.

```go
resp, err := client.Converse(ctx, &audacityruntime.ConverseInput{
    ModelId: audacity.String("claude-sonnet-4-5"),
    System: []types.SystemContentBlock{
        {Text: longSystemPrompt},
        {CachePoint: &types.CachePointBlock{Type: types.CachePointTypeDefault}},
    },
    Messages: []types.Message{{
        Role: types.ConversationRoleUser,
        Content: []types.ContentBlock{
            &types.ContentBlockMemberText{Value: bigReferenceDocument},
            &types.ContentBlockMemberCachePoint{
                Value: types.CachePointBlock{Type: types.CachePointTypeDefault},
            },
            &types.ContentBlockMemberText{Value: "Summarise the key risks."},
        },
    }},
})

// Cache activity is reported in usage (Bedrock names):
fmt.Println(resp.Usage.CacheReadInputTokens)  // tokens served from cache
fmt.Println(resp.Usage.CacheWriteInputTokens) // tokens written to cache
```

A cache point with nothing before it in the same message is silently ignored.

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
