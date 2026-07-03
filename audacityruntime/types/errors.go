package types

import "fmt"

// APIError is embedded in every server-derived exception and carries the
// common diagnostic fields.
type APIError struct {
	Message           string
	StatusCode        int
	ErrorCode         string
	RequestID         *string
	RetryAfterSeconds *int
	RawBody           string

	// Retryable is stamped when the error is classified (spec §4 table:
	// Throttling/ModelTimeout/ServiceUnavailable/InternalServer are retryable;
	// ServiceQuotaExceeded — including 429 + BUDGET_EXCEEDED — and all others
	// are not).  The retry loop reads this flag instead of re-deriving it.
	Retryable bool
}

func (e *APIError) Error() string {
	if e.RequestID != nil {
		return fmt.Sprintf("%s (status=%d, code=%s, requestId=%s)",
			e.Message, e.StatusCode, e.ErrorCode, *e.RequestID)
	}
	return fmt.Sprintf("%s (status=%d, code=%s)", e.Message, e.StatusCode, e.ErrorCode)
}

// ValidationException is raised for malformed or logically invalid requests (HTTP 400).
type ValidationException struct{ APIError }

func (e *ValidationException) Unwrap() error { return &e.APIError }

// AccessDeniedException is raised for authentication or authorisation failures (HTTP 401/403).
type AccessDeniedException struct{ APIError }

func (e *AccessDeniedException) Unwrap() error { return &e.APIError }

// ResourceNotFoundException is raised when the requested resource does not exist (HTTP 404).
type ResourceNotFoundException struct{ APIError }

func (e *ResourceNotFoundException) Unwrap() error { return &e.APIError }

// ServiceQuotaExceededException is raised when a usage budget or quota is exhausted (HTTP 402).
type ServiceQuotaExceededException struct{ APIError }

func (e *ServiceQuotaExceededException) Unwrap() error { return &e.APIError }

// ThrottlingException is raised when the request is rate-limited (HTTP 429).
type ThrottlingException struct{ APIError }

func (e *ThrottlingException) Unwrap() error { return &e.APIError }

// ModelTimeoutException is raised when the model exceeds its time budget (HTTP 408).
type ModelTimeoutException struct{ APIError }

func (e *ModelTimeoutException) Unwrap() error { return &e.APIError }

// ModelErrorException is raised for non-retryable model-level errors.
type ModelErrorException struct{ APIError }

func (e *ModelErrorException) Unwrap() error { return &e.APIError }

// ModelStreamErrorException is raised when a streaming connection fails mid-stream.
// It unwraps to ModelErrorException for errors.As compatibility.
type ModelStreamErrorException struct{ APIError }

// Unwrap allows errors.As to match *ModelErrorException in the chain.
func (e *ModelStreamErrorException) Unwrap() error {
	return &ModelErrorException{APIError: e.APIError}
}

// ServiceUnavailableException is raised when the upstream service is temporarily unavailable
// (HTTP 502/503/504).
type ServiceUnavailableException struct{ APIError }

func (e *ServiceUnavailableException) Unwrap() error { return &e.APIError }

// InternalServerException is raised for unclassified server-side failures (HTTP 500).
type InternalServerException struct{ APIError }

func (e *InternalServerException) Unwrap() error { return &e.APIError }

// MissingAPIKeyError is returned immediately (before any network call) when no API
// key is available via the explicit option or the AUDACITY_API_KEY environment variable.
type MissingAPIKeyError struct{}

func (*MissingAPIKeyError) Error() string {
	return "missing API key: set AUDACITY_API_KEY environment variable or provide APIKey in Options"
}

// SdkError wraps network-level or response-decode failures.
type SdkError struct {
	Message string
	Err     error
}

func (e *SdkError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *SdkError) Unwrap() error { return e.Err }
