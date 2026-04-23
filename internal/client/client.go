// Package client provides a high-level Monotaur API client that wraps the
// oapi-codegen-generated client with JSON:API envelope helpers, ETag round-trip
// support, and typed error decoding.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/monotaur/terraform-provider-monotaur/internal/api"
)

// ContentType is the media type required by the Monotaur API.
const ContentType = "application/vnd.api+json; ext=openapi"

// ErrNotModified is returned by read operations when the server responds with
// 304 Not Modified (i.e. the ETag matched and the cached copy is still valid).
var ErrNotModified = errors.New("monotaur: 304 not modified")

// APIError represents one or more JSON:API error objects returned by the
// server. It implements the error interface so callers can use errors.As to
// inspect individual ErrorObject values.
type APIError struct {
	// StatusCode is the HTTP status code from the response.
	StatusCode int
	// Document is the decoded errorResponseDocument from the response body.
	Document api.ErrorResponseDocument
}

// Error returns a human-readable summary of all errors in the document.
func (e *APIError) Error() string {
	if len(e.Document.Errors) == 0 {
		return fmt.Sprintf("monotaur: HTTP %d with empty error document", e.StatusCode)
	}
	msgs := make([]string, 0, len(e.Document.Errors))
	for _, obj := range e.Document.Errors {
		var parts []string
		if obj.Status != nil && *obj.Status != "" {
			parts = append(parts, *obj.Status)
		}
		if obj.Title != nil && *obj.Title != "" {
			parts = append(parts, *obj.Title)
		}
		if obj.Detail != nil && *obj.Detail != "" {
			parts = append(parts, *obj.Detail)
		}
		if len(parts) == 0 {
			parts = append(parts, fmt.Sprintf("HTTP %d", e.StatusCode))
		}
		msgs = append(msgs, strings.Join(parts, ": "))
	}
	return "monotaur: " + strings.Join(msgs, "; ")
}

// Errors returns the individual JSON:API error objects from the document.
func (e *APIError) Errors() []api.ErrorObject {
	return e.Document.Errors
}

// Config holds the configuration for a Monotaur API client.
type Config struct {
	// BaseURL is the scheme+host (and optional path prefix) of the Monotaur API,
	// e.g. "https://api.example.com".
	BaseURL string
	// APIKey is the bearer token used to authenticate requests.
	APIKey string
	// HTTPClient is the underlying HTTP client. When nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// Client is a high-level Monotaur API client. It wraps the generated
// api.ClientInterface and adds:
//
//   - Automatic Content-Type / Accept header injection
//   - JSON:API document envelope marshal/unmarshal helpers
//   - ETag round-trip helpers for read paths
//   - Typed error decoding from errorResponseDocument
type Client struct {
	inner api.ClientInterface
}

// New creates a new Client from the provided Config. It registers a request
// editor that injects the required Content-Type and Authorization headers on
// every outgoing request, and wraps the underlying HTTP transport with
// structured tflog logging.
//
// BaseURL and APIKey are both required; an error is returned if either is empty.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("monotaur client: BaseURL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("monotaur client: APIKey is required")
	}

	// Determine the base transport: honour an explicitly provided HTTPClient's
	// Transport so tests can inject a stub, then wrap it with logging.
	var baseTransport http.RoundTripper
	if cfg.HTTPClient != nil {
		baseTransport = cfg.HTTPClient.Transport // may be nil → DefaultTransport
	}
	loggingRT := NewLoggingTransport(baseTransport)

	httpClient := &http.Client{Transport: loggingRT}
	if cfg.HTTPClient != nil {
		// Copy timeouts / jar from the caller-supplied client but replace the transport.
		httpClient.Timeout = cfg.HTTPClient.Timeout
		httpClient.Jar = cfg.HTTPClient.Jar
		httpClient.CheckRedirect = cfg.HTTPClient.CheckRedirect
	}

	inner, err := api.NewClient(
		cfg.BaseURL,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(headerInjector(cfg.APIKey)),
	)
	if err != nil {
		return nil, fmt.Errorf("monotaur client: %w", err)
	}

	return &Client{inner: inner}, nil
}

// headerInjector returns a RequestEditorFn that sets the Content-Type, Accept,
// and (when non-empty) Authorization headers required by the Monotaur API.
func headerInjector(apiKey string) api.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Content-Type", ContentType)
		req.Header.Set("Accept", ContentType)
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		return nil
	}
}

// Inner returns the underlying generated api.ClientInterface. This gives
// callers direct access to every generated operation.
func (c *Client) Inner() api.ClientInterface {
	return c.inner
}

// ---------------------------------------------------------------------------
// JSON:API envelope helpers
// ---------------------------------------------------------------------------

// Document is the generic JSON:API primary-data document envelope used when
// reading or writing a single resource:
//
//	{"data": {"type": "...", "id": "...", "attributes": {...}, "relationships": {...}}}
type Document[T any] struct {
	Data T `json:"data"`
}

// CollectionDocument is the generic JSON:API primary-data document envelope
// used for collection responses:
//
//	{"data": [{"type": "...", "id": "...", "attributes": {...}}]}
type CollectionDocument[T any] struct {
	Data []T `json:"data"`
}

// MarshalDocument wraps resource in a JSON:API document envelope and returns
// the JSON-encoded bytes. Use this when building request bodies.
func MarshalDocument[T any](resource T) ([]byte, error) {
	b, err := json.Marshal(Document[T]{Data: resource})
	if err != nil {
		return nil, fmt.Errorf("monotaur: marshal document: %w", err)
	}
	return b, nil
}

// UnmarshalDocument decodes a JSON:API document envelope from r and returns
// the inner data object. The reader is consumed but not closed.
func UnmarshalDocument[T any](r io.Reader) (T, error) {
	var doc Document[T]
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		var zero T
		return zero, fmt.Errorf("monotaur: unmarshal document: %w", err)
	}
	return doc.Data, nil
}

// UnmarshalCollectionDocument decodes a JSON:API collection document envelope
// from r and returns the slice of data objects.
func UnmarshalCollectionDocument[T any](r io.Reader) ([]T, error) {
	var doc CollectionDocument[T]
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("monotaur: unmarshal collection document: %w", err)
	}
	return doc.Data, nil
}

// ---------------------------------------------------------------------------
// Error decoding helpers
// ---------------------------------------------------------------------------

// DecodeError reads the response body and decodes it as a JSON:API
// errorResponseDocument, returning an *APIError. If the body cannot be decoded
// as an errorResponseDocument the raw body text is preserved in a fallback error.
//
// The caller is responsible for closing resp.Body.
func DecodeError(resp *http.Response) error {
	if resp.StatusCode == http.StatusNotModified {
		return ErrNotModified
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("monotaur: HTTP %d: read error body: %w", resp.StatusCode, err)
	}

	var doc api.ErrorResponseDocument
	if jsonErr := json.Unmarshal(body, &doc); jsonErr != nil {
		// Body is not valid JSON:API — surface raw body as context.
		preview := string(body)
		if len(preview) > 256 {
			preview = preview[:256] + "…"
		}
		return fmt.Errorf("monotaur: HTTP %d: %s", resp.StatusCode, preview)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Document:   doc,
	}
}

// CheckResponse returns nil if resp.StatusCode is in the 2xx range. It returns
// ErrNotModified for 304, and calls DecodeError for all other non-2xx codes.
//
// Example usage:
//
//	resp, err := c.Inner().GetComponent(ctx, id, params)
//	if err != nil {
//	    return err
//	}
//	defer resp.Body.Close()
//	if err := client.CheckResponse(resp); err != nil {
//	    return err
//	}
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusNotModified {
		return ErrNotModified
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return DecodeError(resp)
}

// ---------------------------------------------------------------------------
// ETag helpers
// ---------------------------------------------------------------------------

// ETagFromResponse extracts the ETag header value from a response. Returns an
// empty string if the header is absent.
func ETagFromResponse(resp *http.Response) string {
	return resp.Header.Get("ETag")
}

// StringPtr returns a pointer to s. It is a convenience helper for setting
// optional string fields — in particular the IfNoneMatch field on generated
// *Params structs:
//
//	params.IfNoneMatch = client.StringPtr(etag)
func StringPtr(s string) *string {
	return &s
}
