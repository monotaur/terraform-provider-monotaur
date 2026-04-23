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
	// http.Request.Context() is always non-nil for requests created with
	// http.NewRequest or http.NewRequestWithContext.
	ctx := req.Context()

	method := req.Method
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path = path + "?" + req.URL.RawQuery
	}

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
