// Package audacityruntime provides the Audacity API client with an Amazon Bedrock
// Converse–shaped surface, translating Bedrock-style inputs/outputs to the
// Audacity OpenAI-compatible endpoint.
package audacityruntime

import (
	"net/http"
	"os"
	"time"
)

const (
	defaultBaseURL    = "https://portal.audacityinvestments.com"
	defaultTimeout    = 120 * time.Second
	defaultMaxRetries = 2
	sdkVersion        = "0.1.0"
	userAgent         = "audacity-sdk-go/" + sdkVersion
)

// Options configures a Client.
//
// Resolution order for each field:
//  1. Explicit value in Options.
//  2. Environment variable (AUDACITY_API_KEY / AUDACITY_BASE_URL).
//  3. Built-in default.
type Options struct {
	// APIKey is the Audacity API key (e.g. "audacity_api_…").
	// Falls back to AUDACITY_API_KEY.
	APIKey string

	// BaseURL overrides the default API endpoint.
	// Falls back to AUDACITY_BASE_URL, then https://portal.audacityinvestments.com.
	BaseURL string

	// HTTPClient replaces the default http.Client.
	// When nil, a client with the configured Timeout is created.
	HTTPClient *http.Client

	// MaxRetries is the number of additional attempts after the first.
	// Default: 2 (up to 3 total attempts).
	MaxRetries int

	// Timeout is the per-request timeout applied when HTTPClient is nil.
	// Default: 120s.  Set to 0 for no timeout (useful for long streams).
	Timeout time.Duration
}

// Client is the Audacity runtime API client.
type Client struct {
	options    Options
	httpClient *http.Client
}

// New creates a Client from the provided Options, applying environment-variable
// and default fallbacks.
//
// The client does not validate the API key at construction time; a missing key
// will produce a types.MissingAPIKeyError on the first operation.
func New(opts Options) *Client {
	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("AUDACITY_API_KEY")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = os.Getenv("AUDACITY_BASE_URL")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultMaxRetries
	}
	if opts.Timeout == 0 {
		opts.Timeout = defaultTimeout
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: opts.Timeout}
	}

	return &Client{
		options:    opts,
		httpClient: httpClient,
	}
}
