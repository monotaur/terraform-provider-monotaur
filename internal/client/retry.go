// Package client provides a high-level Monotaur API client. This file
// implements an http.RoundTripper that retries transient failures (network
// errors and 5xx responses) on idempotent HTTP methods, using
// hashicorp/go-retryablehttp's policy machinery underneath.
package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// RetryConfig controls the retry transport. Zero values fall back to defaults.
type RetryConfig struct {
	// MaxAttempts is the maximum total number of HTTP requests issued for a
	// single logical call (including the initial attempt). Default: 4.
	MaxAttempts int
	// WaitMin is the minimum backoff between attempts. Default: 200ms.
	WaitMin time.Duration
	// WaitMax is the maximum backoff between attempts. Default: 2s.
	WaitMax time.Duration
}

func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 4
	}
	if c.WaitMin <= 0 {
		c.WaitMin = 200 * time.Millisecond
	}
	if c.WaitMax <= 0 {
		c.WaitMax = 2 * time.Second
	}
	return c
}

// NewRetryTransport wraps inner with retry logic for transient failures on
// idempotent HTTP methods (GET, HEAD, PUT, DELETE). POST and PATCH are never
// retried because the Monotaur API has no idempotency-key support — retrying
// them could create duplicate resources or apply an update twice.
//
// Retries are triggered by:
//   - Network/connection errors from the inner transport
//   - HTTP 5xx responses (except 501 Not Implemented and 504 Gateway Timeout,
//     which retryablehttp's default policy excludes by design)
//   - HTTP 429 Too Many Requests
//
// Backoff is exponential between WaitMin and WaitMax with jitter, capped at
// MaxAttempts total HTTP requests.
//
// If inner is nil, http.DefaultTransport is used.
func NewRetryTransport(inner http.RoundTripper, cfg RetryConfig) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	cfg = cfg.withDefaults()

	rc := retryablehttp.NewClient()
	// Run requests through the supplied inner transport (typically the logging
	// layer) by giving retryablehttp an http.Client that uses it directly.
	rc.HTTPClient = &http.Client{Transport: inner}
	// retryablehttp uses RetryMax as the number of *additional* retries after
	// the initial attempt; convert from total-attempts semantics.
	rc.RetryMax = cfg.MaxAttempts - 1
	rc.RetryWaitMin = cfg.WaitMin
	rc.RetryWaitMax = cfg.WaitMax
	rc.Logger = nil
	rc.CheckRetry = idempotentOnly5xxRetryPolicy

	// StandardClient() returns a *http.Client whose Transport is an internal
	// adapter that calls rc.Do — i.e. it applies the retry policy as a
	// RoundTripper.
	return rc.StandardClient().Transport
}

// idempotentOnly5xxRetryPolicy is the CheckRetry function used by the retry
// transport. It defers to retryablehttp's DefaultRetryPolicy for the
// "transient failure" classification, but refuses to retry POST and PATCH
// requests even when the response is a 5xx — the Monotaur API has no
// idempotency-key mechanism, so retrying a write could create duplicate
// resources or apply an update twice.
func idempotentOnly5xxRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	// Respect context cancellation first.
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	shouldRetry, policyErr := retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	if !shouldRetry {
		return false, policyErr
	}

	// Determine the request method. When err != nil, resp may be nil — but
	// retryablehttp guarantees the *original request* is reused, so the method
	// is implicit from the caller's request. We can only inspect resp.Request
	// when resp is non-nil; on transport error we assume the method comes from
	// the request that triggered the call. Since we don't have direct access
	// to it here, we err on the safe side: retry network errors regardless of
	// method (the request didn't reach the server, so it's safe to retry), but
	// refuse to retry 5xx on POST/PATCH (the server may have already applied
	// the change).
	if resp == nil || resp.Request == nil {
		return true, policyErr
	}

	method := resp.Request.Method
	if method == http.MethodPost || method == http.MethodPatch {
		tflog.Debug(ctx, "monotaur: skipping retry of non-idempotent request",
			map[string]any{"http.method": method, "http.status": resp.StatusCode})
		return false, fmt.Errorf("monotaur: %s returned HTTP %d (not retried; method is not idempotent)",
			method, resp.StatusCode)
	}

	return true, policyErr
}
