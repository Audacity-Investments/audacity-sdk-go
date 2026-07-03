package audacityruntime

// http.go — low-level HTTP execution helpers used by Converse and ConverseStream.
//
// Timeout semantics (spec §1): the configured timeout bounds each attempt's
// connection + request write + response headers, and — for Converse only —
// the full response body read.  The SSE body of a stream is never bounded.

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
		return nil, &types.SdkError{Message: "HTTP request failed", Err: err, Retryable: true}
	}
	return resp, nil
}

// sleepBackoff waits out the jittered backoff before a retry, honouring
// context cancellation.  attempt is 1-based for the first retry (spec §4).
func sleepBackoff(ctx context.Context, attempt int, lastErr error) error {
	delay := backoffDuration(attempt, retryAfterFromErr(lastErr))
	select {
	case <-ctx.Done():
		return &types.SdkError{
			Message: "context cancelled during retry backoff",
			Err:     ctx.Err(),
		}
	case <-time.After(delay):
		return nil
	}
}

// doWithRetry drives the shared retry skeleton — attempt loop, backoff,
// caller-context abort, §4 retryability branch — around an attempt func that
// performs one full request and returns its classified error.
func (c *Client) doWithRetry(ctx context.Context, attempt func(context.Context) error) error {
	var lastErr error

	for n := 0; n <= c.options.MaxRetries; n++ {
		if n > 0 {
			if err := sleepBackoff(ctx, n, lastErr); err != nil {
				return err
			}
		}

		err := attempt(ctx)
		if err == nil {
			return nil
		}
		// A cancelled caller context aborts immediately — a per-attempt
		// timeout (attempt context only) stays retryable per §4.
		if ctx.Err() != nil {
			return &types.SdkError{Message: "request aborted by caller context", Err: err}
		}
		if isRetryableError(err) {
			lastErr = err
			continue
		}
		return err
	}

	return lastErr
}

// doConverseWithRetry executes a non-streaming request with the configured
// retry policy and returns the fully-read response body plus the elapsed
// wall-clock latency.  The per-attempt timeout covers the full body read.
func (c *Client) doConverseWithRetry(ctx context.Context, body []byte) ([]byte, int64, error) {
	start := time.Now()
	var respBody []byte
	err := c.doWithRetry(ctx, func(ctx context.Context) error {
		var err error
		respBody, err = c.converseAttempt(ctx, body)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return respBody, time.Since(start).Milliseconds(), nil
}

// converseAttempt performs one non-streaming attempt, bounded end-to-end
// (headers + body) by the configured timeout.
func (c *Client) converseAttempt(ctx context.Context, body []byte) ([]byte, error) {
	actx := ctx
	var cancel context.CancelFunc = func() {}
	if c.options.Timeout > 0 {
		actx, cancel = context.WithTimeout(ctx, c.options.Timeout)
	}
	defer cancel()

	resp, err := c.doRequest(actx, body, "application/json")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseErrorBody(respBody, resp.StatusCode, resp.Header)
	}

	respBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		// Network failure during body read — retryable per §4.
		return nil, &types.SdkError{Message: "failed to read response body", Err: readErr, Retryable: true}
	}
	return respBody, nil
}

// streamAttempt performs one streaming attempt.  The configured timeout
// bounds only the wait for response headers; the SSE body must stay readable
// indefinitely, so on success the live response is returned together with the
// stream's context and its cancel func, which the caller MUST eventually
// invoke to release resources.
func (c *Client) streamAttempt(ctx context.Context, body []byte) (*http.Response, context.Context, context.CancelFunc, error) {
	sctx, cancel := context.WithCancel(ctx)
	var headerTimer *time.Timer
	if c.options.Timeout > 0 {
		headerTimer = time.AfterFunc(c.options.Timeout, cancel)
	}

	resp, err := c.doRequest(sctx, body, "text/event-stream")
	if headerTimer != nil {
		headerTimer.Stop()
	}
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, nil, nil, parseErrorBody(respBody, resp.StatusCode, resp.Header)
	}

	// HTTP 200: hand the live response to the caller — no more retries.
	return resp, sctx, cancel, nil
}

// doConverseStreamWithRetry executes a streaming request, retrying only until
// HTTP status + headers are received.
func (c *Client) doConverseStreamWithRetry(ctx context.Context, body []byte) (*http.Response, context.Context, context.CancelFunc, time.Time, error) {
	start := time.Now()
	var (
		resp   *http.Response
		sctx   context.Context
		cancel context.CancelFunc
	)
	err := c.doWithRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, sctx, cancel, err = c.streamAttempt(ctx, body)
		return err
	})
	if err != nil {
		return nil, nil, nil, start, err
	}
	return resp, sctx, cancel, start, nil
}
