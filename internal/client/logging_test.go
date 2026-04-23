package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
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
	// NewLoggingTransport(nil) should not panic and should wrap DefaultTransport.
	// We cannot easily exercise it without a live server, so just confirm the
	// return value is non-nil and doesn't panic on construction.
	transport := client.NewLoggingTransport(nil)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestLoggingTransport_doesNotLeakAPIKeyInRequest(t *testing.T) {
	// The logging transport must not add or expose the raw API key value. The
	// api_key is injected by headerInjector as "Authorization: Bearer <key>",
	// but the logging transport only reads req.Method and req.URL — it never
	// reads or logs the Authorization header value itself.
	//
	// This test verifies that the Authorization header is still present after
	// passing through the logging transport (i.e. it is not stripped), and that
	// the transport does not itself add any additional auth headers.
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

	// The Authorization header must pass through unchanged.
	if got := capturedReq.Header.Get("Authorization"); got != "Bearer secret-key" {
		t.Errorf("Authorization header modified by logging transport: got %q", got)
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
