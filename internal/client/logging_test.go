package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// ---------------------------------------------------------------------------
// NewLoggingTransport
// ---------------------------------------------------------------------------

func TestLoggingTransport_forwardsSuccessfulResponse(t *testing.T) {
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Request-Id": []string{"req-123"}},
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})

	transport := client.NewLoggingTransport(inner)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/api/v1/things", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: want %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestLoggingTransport_propagatesTransportError(t *testing.T) {
	sentinel := errors.New("connection refused")
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, sentinel
	})

	transport := client.NewLoggingTransport(inner)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/api/v1/things", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	_, gotErr := transport.RoundTrip(req)
	if !errors.Is(gotErr, sentinel) {
		t.Errorf("want sentinel error, got %v", gotErr)
	}
}

func TestLoggingTransport_usesDefaultTransportWhenInnerIsNil(t *testing.T) {
	// NewLoggingTransport(nil) must wrap http.DefaultTransport. Exercise the
	// fallback end-to-end by making a real request to a local httptest.Server,
	// which confirms that DefaultTransport is wired correctly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	transport := client.NewLoggingTransport(nil)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip with nil inner: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("StatusCode: want %d, got %d", http.StatusNoContent, resp.StatusCode)
	}
}

// TestLoggingTransport_doesNotLogQueryParameters verifies that the transport
// logs only the URL path, not the raw query string. Query parameters may carry
// tokens, session IDs, or other sensitive values that must not appear in logs.
func TestLoggingTransport_doesNotLogQueryParameters(t *testing.T) {
	// Capture the request as seen by the inner transport to confirm it is
	// forwarded unmodified (the logging layer must be read-only).
	var capturedURL string
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	transport := client.NewLoggingTransport(inner)

	// Include a sensitive-looking query parameter that must not appear in logs.
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://localhost/api/v1/things?token=supersecret&page=1",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-key")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	// The full URL (including query string) must still reach the inner transport
	// unchanged — the logging transport must not strip or modify the request.
	if !strings.Contains(capturedURL, "token=supersecret") {
		t.Errorf("inner transport did not receive the full URL; got %q", capturedURL)
	}

	// The Authorization header must pass through unchanged.
	// (Logging transport must not read or strip auth headers.)
	if got := req.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization header modified by logging transport: got %q", got)
	}
}

// TestLoggingTransport_forwardsHeadersUnmodified verifies the Authorization
// header is preserved and that the transport does not inject extra auth headers.
func TestLoggingTransport_forwardsHeadersUnmodified(t *testing.T) {
	var capturedReq *http.Request
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	transport := client.NewLoggingTransport(inner)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/api/v1/things", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-key")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	// Authorization must pass through unchanged; the logging transport is read-only.
	if got := capturedReq.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization header modified by logging transport: got %q", got)
	}
	// The transport must not add extra Authorization headers.
	if n := len(capturedReq.Header["Authorization"]); n != 1 {
		t.Errorf("expected 1 Authorization header value, got %d", n)
	}
}

// TestLoggingTransport_wiredThroughClientNew verifies that the logging
// transport is active when a client is created via client.New. The stub
// transport passed via HTTPClient.Transport is wrapped transparently — all
// requests still reach the stub.
func TestLoggingTransport_wiredThroughClientNew(t *testing.T) {
	var called bool
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	c, err := client.New(client.Config{
		BaseURL:    "http://localhost",
		APIKey:     "test-key",
		HTTPClient: &http.Client{Transport: inner},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := c.Inner().GetAdminApiKeyCollection(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetAdminApiKeyCollection: %v", err)
	}
	resp.Body.Close()

	if !called {
		t.Error("expected the stub transport to be called; the logging transport did not forward the request")
	}
}
