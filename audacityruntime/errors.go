package audacityruntime

// errors.go — error-body parsing (shapes A and B) and the code→exception mapping table
// from spec §4.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Audacity-Investments/audacity-sdk-go/audacityruntime/types"
)

// ─────────────────────────────────────────────────────────────
// Error-body JSON shapes
// ─────────────────────────────────────────────────────────────

// oaiErrorPayload is the OpenAI-style inner error object.  It appears in shape A
// bodies and inline SSE error chunks; the latter may also carry a shape-B inner
// object, whose request_id must be preserved (spec §4).
type oaiErrorPayload struct {
	Message   string  `json:"message"`
	Type      string  `json:"type"`
	Code      *string `json:"code"`
	RequestID string  `json:"request_id"`
}

// shapeA is the OpenAI-style error envelope.
type shapeA struct {
	Error *oaiErrorPayload `json:"error"`
}

// shapeB is the LiteLLM-proxy envelope.
type shapeB struct {
	Success *bool `json:"success"`
	Error   *struct {
		Code      string      `json:"code"`
		Message   string      `json:"message"`
		RequestID string      `json:"request_id"`
		Details   interface{} `json:"details"`
	} `json:"error"`
}

// parseErrorBody converts a non-200 HTTP response into a typed exception.
// It tries shape A then shape B; falls back to HTTP-status mapping.
func parseErrorBody(body []byte, statusCode int, header http.Header) error {
	rawBody := string(body)
	retryAfter := parseRetryAfter(header)

	// Try to parse as JSON
	if len(body) > 0 {
		// Shape B checked FIRST: { "success": false, "error": { "code": …, "request_id": … } }
		// (takes priority because it carries request_id that we must not lose)
		var b shapeB
		if json.Unmarshal(body, &b) == nil && b.Success != nil && !*b.Success && b.Error != nil {
			rid := b.Error.RequestID
			base := types.APIError{
				Message:           b.Error.Message,
				StatusCode:        statusCode,
				ErrorCode:         b.Error.Code,
				RetryAfterSeconds: retryAfter,
				RawBody:           rawBody,
			}
			if rid != "" {
				base.RequestID = &rid
			}
			return mapCodeToException(b.Error.Code, statusCode, base)
		}

		// Shape A: { "error": { "message": …, "type": …, "code": … } }
		var a shapeA
		if json.Unmarshal(body, &a) == nil && a.Error != nil {
			return exceptionFromOAIError(a.Error, statusCode, retryAfter, rawBody)
		}
	}

	// Fallback: treat the raw body as the message and use HTTP status.
	base := types.APIError{
		Message:           rawBody,
		StatusCode:        statusCode,
		ErrorCode:         "",
		RetryAfterSeconds: retryAfter,
		RawBody:           rawBody,
	}
	return mapStatusToException(statusCode, base)
}

// exceptionFromOAIError converts an OpenAI-style inner error object into a typed
// exception, applying the code-then-type precedence rule and preserving any
// request_id (present on shape-B inner objects delivered mid-stream).
func exceptionFromOAIError(p *oaiErrorPayload, statusCode int, retryAfter *int, rawBody string) error {
	rawCode := p.Type
	if p.Code != nil && *p.Code != "" {
		rawCode = *p.Code
	}
	base := types.APIError{
		Message:           p.Message,
		StatusCode:        statusCode,
		ErrorCode:         rawCode,
		RetryAfterSeconds: retryAfter,
		RawBody:           rawBody,
	}
	if p.RequestID != "" {
		rid := p.RequestID
		base.RequestID = &rid
	}
	return mapCodeToException(rawCode, statusCode, base)
}

// parseRetryAfter extracts the integer seconds from a Retry-After header, or nil.
func parseRetryAfter(h http.Header) *int {
	v := h.Get("Retry-After")
	if v == "" {
		return nil
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return nil
	}
	return &secs
}

// ─────────────────────────────────────────────────────────────
// Code → exception mapping table  (spec §4)
// ─────────────────────────────────────────────────────────────

func mapCodeToException(rawCode string, statusCode int, base types.APIError) error {
	code := strings.ToLower(rawCode)

	switch code {
	case "invalid_api_key", "api_key_required", "authentication_error",
		"authorization_error", "model_not_allowed":
		return &types.AccessDeniedException{APIError: base}

	case "usage_cap_exceeded", "usage_cap_error", "budget_exceeded":
		return &types.ServiceQuotaExceededException{APIError: base}

	case "rate_limit_exceeded", "rate_limit_error":
		base.Retryable = true
		return &types.ThrottlingException{APIError: base}

	case "invalid_request_error", "validation_error":
		return &types.ValidationException{APIError: base}

	case "model_not_found":
		return &types.ResourceNotFoundException{APIError: base}

	case "timeout_error":
		base.Retryable = true
		return &types.ModelTimeoutException{APIError: base}

	case "stream_error":
		return &types.ModelStreamErrorException{APIError: base}

	case "upstream_error":
		if statusCode >= 500 {
			base.Retryable = true
			return &types.ServiceUnavailableException{APIError: base}
		}
		return &types.ModelErrorException{APIError: base}
	}

	// No recognised code — fall back to HTTP status.
	return mapStatusToException(statusCode, base)
}

// mapStatusToException maps an HTTP status code to the appropriate typed exception.
func mapStatusToException(statusCode int, base types.APIError) error {
	switch statusCode {
	case 400:
		return &types.ValidationException{APIError: base}
	case 401, 403:
		return &types.AccessDeniedException{APIError: base}
	case 402:
		return &types.ServiceQuotaExceededException{APIError: base}
	case 404:
		return &types.ResourceNotFoundException{APIError: base}
	case 408:
		base.Retryable = true
		return &types.ModelTimeoutException{APIError: base}
	case 429:
		base.Retryable = true
		return &types.ThrottlingException{APIError: base}
	case 500:
		base.Retryable = true
		return &types.InternalServerException{APIError: base}
	case 502, 503, 504:
		base.Retryable = true
		return &types.ServiceUnavailableException{APIError: base}
	default:
		if statusCode >= 400 && statusCode < 500 {
			return &types.ValidationException{APIError: base}
		}
		return &types.InternalServerException{APIError: base}
	}
}

// parseStreamError parses an inline {"error": ...} payload from an SSE chunk.
func parseStreamError(raw json.RawMessage) error {
	var payload oaiErrorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return &types.SdkError{Message: "stream error (unparseable)", Err: err}
	}
	return exceptionFromOAIError(&payload, 0, nil, string(raw))
}
