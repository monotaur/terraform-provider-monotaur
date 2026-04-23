package client_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monotaur/terraform-provider-monotaur/internal/api"
	"github.com/monotaur/terraform-provider-monotaur/internal/client"
)

// ---------------------------------------------------------------------------
// JSON:API envelope helpers
// ---------------------------------------------------------------------------

// simpleResource is a stand-in for any JSON:API resource data object.
type simpleResource struct {
	Type       string         `json:"type"`
	ID         string         `json:"id,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

func TestMarshalDocument_wrapsResourceInDataEnvelope(t *testing.T) {
	res := simpleResource{
		Type: "components",
		ID:   "42",
		Attributes: map[string]any{
			"name": "web-frontend",
		},
	}

	b, err := client.MarshalDocument(res)
	if err != nil {
		t.Fatalf("MarshalDocument returned unexpected error: %v", err)
	}

	got := string(b)
	if !strings.Contains(got, `"data"`) {
		t.Errorf("expected output to contain %q, got: %s", `"data"`, got)
	}
	if !strings.Contains(got, `"components"`) {
		t.Errorf("expected output to contain resource type %q, got: %s", "components", got)
	}
	if !strings.Contains(got, `"web-frontend"`) {
		t.Errorf("expected output to contain attribute value %q, got: %s", "web-frontend", got)
	}
}

func TestMarshalDocument_roundTrip(t *testing.T) {
	original := simpleResource{
		Type: "monitors",
		ID:   "7",
		Attributes: map[string]any{
			"name": "ping-check",
		},
	}

	b, err := client.MarshalDocument(original)
	if err != nil {
		t.Fatalf("MarshalDocument: %v", err)
	}

	decoded, err := client.UnmarshalDocument[simpleResource](bytes.NewReader(b))
	if err != nil {
		t.Fatalf("UnmarshalDocument: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type: want %q, got %q", original.Type, decoded.Type)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID: want %q, got %q", original.ID, decoded.ID)
	}
}

func TestUnmarshalDocument_decodesEnvelope(t *testing.T) {
	const fixture = `{"data":{"type":"components","id":"1","attributes":{"name":"api"}}}`

	res, err := client.UnmarshalDocument[simpleResource](strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("UnmarshalDocument returned unexpected error: %v", err)
	}

	if res.Type != "components" {
		t.Errorf("Type: want %q, got %q", "components", res.Type)
	}
	if res.ID != "1" {
		t.Errorf("ID: want %q, got %q", "1", res.ID)
	}
}

func TestUnmarshalDocument_returnsErrorOnInvalidJSON(t *testing.T) {
	_, err := client.UnmarshalDocument[simpleResource](strings.NewReader(`not-json`))
	if err == nil {
		t.Fatal("expected error from UnmarshalDocument, got nil")
	}
}

func TestUnmarshalCollectionDocument_decodesSlice(t *testing.T) {
	const fixture = `{"data":[
		{"type":"components","id":"1","attributes":{"name":"api"}},
		{"type":"components","id":"2","attributes":{"name":"worker"}}
	]}`

	items, err := client.UnmarshalCollectionDocument[simpleResource](strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("UnmarshalCollectionDocument: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].ID != "1" {
		t.Errorf("items[0].ID: want %q, got %q", "1", items[0].ID)
	}
	if items[1].ID != "2" {
		t.Errorf("items[1].ID: want %q, got %q", "2", items[1].ID)
	}
}

func TestUnmarshalCollectionDocument_returnsEmptySliceWhenDataIsEmpty(t *testing.T) {
	const fixture = `{"data":[]}`
	items, err := client.UnmarshalCollectionDocument[simpleResource](strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("want 0 items, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Error decoding helpers
// ---------------------------------------------------------------------------

// newResponseWithBody builds a minimal *http.Response for testing.
func newResponseWithBody(statusCode int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const errorDocumentFixture = `{
  "errors": [
    {
      "status": "422",
      "title":  "Value is too short",
      "detail": "Attribute 'name' is too short.",
      "source": {
        "pointer": "/data/attributes/name"
      }
    }
  ],
  "links": {
    "self": "/api/v1/components"
  }
}`

func TestDecodeError_returnsAPIErrorForJSONAPIBody(t *testing.T) {
	resp := newResponseWithBody(http.StatusUnprocessableEntity, errorDocumentFixture, nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}

	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode: want %d, got %d", http.StatusUnprocessableEntity, apiErr.StatusCode)
	}

	errs := apiErr.Errors()
	if len(errs) != 1 {
		t.Fatalf("want 1 error object, got %d", len(errs))
	}

	obj := errs[0]
	if obj.Status == nil || *obj.Status != "422" {
		t.Errorf("error status: want %q, got %v", "422", obj.Status)
	}
	if obj.Title == nil || *obj.Title != "Value is too short" {
		t.Errorf("error title: want %q, got %v", "Value is too short", obj.Title)
	}
	if obj.Detail == nil || *obj.Detail != "Attribute 'name' is too short." {
		t.Errorf("error detail: want %q, got %v", "Attribute 'name' is too short.", obj.Detail)
	}
	if obj.Source == nil || obj.Source.Pointer == nil || *obj.Source.Pointer != "/data/attributes/name" {
		t.Errorf("error source pointer: want %q, got %v", "/data/attributes/name", obj.Source)
	}
}

func TestDecodeError_errorsMessageContainsStatusAndTitle(t *testing.T) {
	resp := newResponseWithBody(http.StatusUnprocessableEntity, errorDocumentFixture, nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	msg := err.Error()

	if !strings.Contains(msg, "422") {
		t.Errorf("error message should contain status %q, got: %s", "422", msg)
	}
	if !strings.Contains(msg, "Value is too short") {
		t.Errorf("error message should contain title, got: %s", msg)
	}
}

func TestDecodeError_returnsNotModifiedForHTTP304(t *testing.T) {
	resp := newResponseWithBody(http.StatusNotModified, "", nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	if !errors.Is(err, client.ErrNotModified) {
		t.Errorf("want ErrNotModified, got %v", err)
	}
}

func TestDecodeError_fallsBackToRawBodyForNonJSONAPIResponse(t *testing.T) {
	resp := newResponseWithBody(http.StatusInternalServerError, "internal server error", nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error message should not mention unmarshal for raw fallback, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error message should contain status code, got: %s", err.Error())
	}
}

func TestDecodeError_handlesEmptyErrorsArray(t *testing.T) {
	const fixture = `{"errors":[],"links":{"self":"/api/v1/components"}}`
	resp := newResponseWithBody(http.StatusBadRequest, fixture, nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T", err)
	}
	if !strings.Contains(apiErr.Error(), "400") {
		t.Errorf("error message for empty errors array should mention status, got: %s", apiErr.Error())
	}
}

const multiErrorFixture = `{
  "errors": [
    {
      "status": "400",
      "title": "Missing field",
      "detail": "name is required"
    },
    {
      "status": "400",
      "title": "Invalid value",
      "detail": "type must be 'components'"
    }
  ],
  "links": {}
}`

func TestDecodeError_handlesMultipleErrors(t *testing.T) {
	resp := newResponseWithBody(http.StatusBadRequest, multiErrorFixture, nil)
	defer resp.Body.Close()

	err := client.DecodeError(resp)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *client.APIError, got %T: %v", err, err)
	}

	if len(apiErr.Errors()) != 2 {
		t.Fatalf("want 2 error objects, got %d", len(apiErr.Errors()))
	}
	msg := apiErr.Error()
	if !strings.Contains(msg, "Missing field") || !strings.Contains(msg, "Invalid value") {
		t.Errorf("error message should contain both error titles, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// CheckResponse
// ---------------------------------------------------------------------------

func TestCheckResponse_returnsNilFor2xx(t *testing.T) {
	for _, code := range []int{200, 201, 204} {
		resp := newResponseWithBody(code, "", nil)
		resp.Body.Close()
		if err := client.CheckResponse(resp); err != nil {
			t.Errorf("CheckResponse(%d): want nil, got %v", code, err)
		}
	}
}

func TestCheckResponse_returnsErrNotModifiedFor304(t *testing.T) {
	resp := newResponseWithBody(http.StatusNotModified, "", nil)
	resp.Body.Close()
	if err := client.CheckResponse(resp); !errors.Is(err, client.ErrNotModified) {
		t.Errorf("want ErrNotModified, got %v", err)
	}
}

func TestCheckResponse_returnsAPIErrorFor4xx(t *testing.T) {
	resp := newResponseWithBody(http.StatusNotFound, errorDocumentFixture, nil)
	defer resp.Body.Close()

	err := client.CheckResponse(resp)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("expected *client.APIError, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// ETag helpers
// ---------------------------------------------------------------------------

func TestETagFromResponse_returnsHeaderValue(t *testing.T) {
	resp := newResponseWithBody(http.StatusOK, "", map[string]string{
		"ETag": `"abc123"`,
	})
	resp.Body.Close()

	got := client.ETagFromResponse(resp)
	if got != `"abc123"` {
		t.Errorf("want %q, got %q", `"abc123"`, got)
	}
}

func TestETagFromResponse_returnsEmptyStringWhenAbsent(t *testing.T) {
	resp := newResponseWithBody(http.StatusOK, "", nil)
	resp.Body.Close()

	if got := client.ETagFromResponse(resp); got != "" {
		t.Errorf("want empty string, got %q", got)
	}
}

func TestStringPtr_returnsPointerToString(t *testing.T) {
	s := "hello"
	p := client.StringPtr(s)
	if p == nil {
		t.Fatal("want non-nil pointer")
	}
	if *p != s {
		t.Errorf("want %q, got %q", s, *p)
	}
}

// TestStringPtr_usedAsIfNoneMatch demonstrates the ETag round-trip pattern
// using the generated params struct.
func TestStringPtr_usedAsIfNoneMatch(t *testing.T) {
	etag := `"v1-abc"`
	params := api.GetAdminApiKeyCollectionParams{
		IfNoneMatch: client.StringPtr(etag),
	}
	if params.IfNoneMatch == nil || *params.IfNoneMatch != etag {
		t.Errorf("IfNoneMatch: want %q, got %v", etag, params.IfNoneMatch)
	}
}

// ---------------------------------------------------------------------------
// New / Config validation
// ---------------------------------------------------------------------------

func TestNew_returnsErrorForEmptyBaseURL(t *testing.T) {
	_, err := client.New(client.Config{})
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
	if !strings.Contains(err.Error(), "BaseURL") {
		t.Errorf("error should mention BaseURL, got: %v", err)
	}
}

func TestNew_setsContentTypeHeaderOnRequests(t *testing.T) {
	var capturedHeader http.Header
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedHeader = req.Header.Clone()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
		}, nil
	})

	c, err := client.New(client.Config{
		BaseURL:    "http://localhost",
		APIKey:     "test-key",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Exercise any generated method that results in an outgoing request.
	resp, err := c.Inner().GetAdminApiKeyCollection(context.Background(), &api.GetAdminApiKeyCollectionParams{})
	if err != nil {
		t.Fatalf("GetAdminApiKeyCollection: %v", err)
	}
	resp.Body.Close()

	if got := capturedHeader.Get("Content-Type"); got != client.ContentType {
		t.Errorf("Content-Type: want %q, got %q", client.ContentType, got)
	}
	if got := capturedHeader.Get("Accept"); got != client.ContentType {
		t.Errorf("Accept: want %q, got %q", client.ContentType, got)
	}
	if got := capturedHeader.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization: want %q, got %q", "Bearer test-key", got)
	}
}

func TestNew_doesNotSetAuthorizationWhenAPIKeyEmpty(t *testing.T) {
	var capturedHeader http.Header
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedHeader = req.Header.Clone()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
		}, nil
	})

	c, err := client.New(client.Config{
		BaseURL:    "http://localhost",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := c.Inner().GetAdminApiKeyCollection(context.Background(), &api.GetAdminApiKeyCollectionParams{})
	if err != nil {
		t.Fatalf("GetAdminApiKeyCollection: %v", err)
	}
	resp.Body.Close()

	if got := capturedHeader.Get("Authorization"); got != "" {
		t.Errorf("Authorization: want empty string, got %q", got)
	}
}

// roundTripFunc is an http.RoundTripper that delegates to a function —
// convenient for capturing outbound request headers in tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
