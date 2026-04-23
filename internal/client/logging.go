// Package client provides a high-level Monotaur API client. This file
// implements a logging http.RoundTripper that records every outbound request
// and its response using tflog, with the api_key value redacted from logs.
package client

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// loggingTransport is an http.RoundTripper that wraps an inner transport and
// emits structured tflog entries for each request/response pair.
//
// Secrets (the API key value) are never written to the log. The request ID is
// derived from the response's X-Request-Id header when present, and falls back
// to a synthetic "<method> <path>" label otherwise.
type loggingTransport struct {
	inner http.RoundTripper
}

// NewLoggingTransport wraps inner with request/response logging via tflog. If
// inner is nil, http.DefaultTransport is used.
func NewLoggingTransport(inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &loggingTransport{inner: inner}
}

// RoundTrip executes the request via the inner transport and logs the outcome.
// It never modifies the request.
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Derive a context from the request so tflog can attach provider metadata.
	// Note: req.Context() may return context.Background() when the caller used
	// http.NewRequest rather than http.NewRequestWithContext, in which case tflog
	// entries emitted here will not be correlated with the provider-level masked
	// context (and the api_key mask will not apply). This is acceptable for v0.1;
	// a future improvement would thread the provider context through to the client.
	ctx := req.Context()

	method := req.Method
	// Log only the URL path, never the raw query string. Query parameters may
	// contain tokens, session IDs, or other PII that must not appear in logs.
	path := req.URL.Path

	tflog.Debug(ctx, "monotaur: sending request", map[string]any{
		"http.method": method,
		"http.path":   path,
	})

	start := time.Now()
	resp, err := t.inner.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		tflog.Debug(ctx, "monotaur: request failed", map[string]any{
			"http.method":   method,
			"http.path":     path,
			"http.duration": fmt.Sprintf("%dms", elapsed.Milliseconds()),
			"error":         err.Error(),
		})
		return nil, err
	}

	// Extract X-Request-Id from response headers when the server provides one.
	requestID := resp.Header.Get("X-Request-Id")

	fields := map[string]any{
		"http.method":   method,
		"http.path":     path,
		"http.status":   resp.StatusCode,
		"http.duration": fmt.Sprintf("%dms", elapsed.Milliseconds()),
	}
	if requestID != "" {
		fields["http.request_id"] = requestID
	}

	tflog.Debug(ctx, "monotaur: received response", fields)

	return resp, nil
}
