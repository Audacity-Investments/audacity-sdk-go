package audacityruntime

// http.go — low-level HTTP execution helpers used by Converse and ConverseStream.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// doRequest executes a single HTTP POST to the completions endpoint and returns
// the raw *http.Response (caller is responsible for closing the body).
func (c *Client) doRequest(ctx context.Context, body []byte, accept string) (*http.Response, error) {
	url := c.options.BaseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &types.SdkError{Message: "failed to create HTTP request", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.options.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &types.SdkError{Message: "HTTP request failed", Err: err}
	}
	return resp, nil
}

// doWithRetry executes a request with the configured retry policy: it owns the
// attempt loop, backoff (honouring Retry-After), context cancellation, retry on
// network errors, and parse + retryability classification of non-200 responses.
// On success it returns the unread HTTP 200 *http.Response (caller closes the
// body) and the wall-clock start time of the first attempt.
func (c *Client) doWithRetry(ctx context.Context, body []byte, stream bool) (*http.Response, time.Time, error) {
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}

	var lastErr error
	start := time.Now()

	for attempt := 0; attempt <= c.options.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDuration(attempt-1, retryAfterFromErr(lastErr))
			select {
			case <-ctx.Done():
				return nil, start, &types.SdkError{
					Message: "context cancelled during retry backoff",
					Err:     ctx.Err(),
				}
			case <-time.After(delay):
			}
		}

		resp, err := c.doRequest(ctx, body, accept)
		if err != nil {
			lastErr = err
			continue // network error — always retry
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			apiErr := parseErrorBody(respBody, resp.StatusCode, resp.Header)
			if isRetryableError(apiErr) {
				lastErr = apiErr
				continue
			}
			return nil, start, apiErr
		}

		// HTTP 200: hand the response to the caller — no more retries.
		return resp, start, nil
	}

	return nil, start, lastErr
}

// doConverseWithRetry executes a non-streaming request with the configured retry
// policy and returns the fully-read response body plus the elapsed wall-clock latency.
func (c *Client) doConverseWithRetry(ctx context.Context, body []byte) ([]byte, int64, error) {
	resp, start, err := c.doWithRetry(ctx, body, false)
	if err != nil {
		return nil, 0, err
	}

	respBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, 0, &types.SdkError{Message: "failed to read response body", Err: readErr}
	}

	return respBody, time.Since(start).Milliseconds(), nil
}

// doConverseStreamWithRetry executes a streaming request, retrying only until
// HTTP status + headers are received.  Once HTTP 200 is confirmed it returns
// the raw *http.Response for the caller to consume via SSE reading.
func (c *Client) doConverseStreamWithRetry(ctx context.Context, body []byte) (*http.Response, time.Time, error) {
	return c.doWithRetry(ctx, body, true)
}
