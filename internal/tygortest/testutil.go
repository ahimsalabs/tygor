// Package tygortest provides testing helpers for HTTP handlers and tygor service handlers.
// This package is designed to be import-cycle safe and can be used from any package
// within the tygor module.
package tygortest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RequestBuilder helps construct test HTTP requests with fluent API.
type RequestBuilder struct {
	httpMethod   string
	path         string
	body         []byte
	headers      map[string]string
	queryParams  map[string]string
	service      string
	method       string
	contextSetup ContextSetupFunc
}

// NewRequest creates a new request builder.
// Optionally accepts a ContextSetupFunc to configure the request context.
func NewRequest(contextSetup ...ContextSetupFunc) *RequestBuilder {
	var setup ContextSetupFunc
	if len(contextSetup) > 0 {
		setup = contextSetup[0]
	}
	return &RequestBuilder{
		httpMethod:   "GET",
		path:         "/",
		headers:      make(map[string]string),
		queryParams:  make(map[string]string),
		service:      "TestService",
		method:       "TestMethod",
		contextSetup: setup,
	}
}

// GET sets the HTTP method to GET.
func (b *RequestBuilder) GET(path string) *RequestBuilder {
	b.httpMethod = "GET"
	b.path = path
	return b
}

// POST sets the HTTP method to POST.
func (b *RequestBuilder) POST(path string) *RequestBuilder {
	b.httpMethod = "POST"
	b.path = path
	return b
}

// WithJSON sets the request body as JSON.
func (b *RequestBuilder) WithJSON(v any) *RequestBuilder {
	data, _ := json.Marshal(v)
	b.body = data
	b.headers["Content-Type"] = "application/json"
	return b
}

// WithBody sets the raw request body.
func (b *RequestBuilder) WithBody(body string) *RequestBuilder {
	b.body = []byte(body)
	return b
}

// WithHeader adds a header to the request.
func (b *RequestBuilder) WithHeader(key, value string) *RequestBuilder {
	b.headers[key] = value
	return b
}

// WithQuery adds a query parameter.
func (b *RequestBuilder) WithQuery(key, value string) *RequestBuilder {
	b.queryParams[key] = value
	return b
}

// WithServiceInfo sets the service and method for context.
func (b *RequestBuilder) WithServiceInfo(service, method string) *RequestBuilder {
	b.service = service
	b.method = method
	return b
}

// ContextSetupFunc is a function that sets up the request context.
// It receives the current context, response writer, and request, and returns a new context.
type ContextSetupFunc func(ctx context.Context, w http.ResponseWriter, r *http.Request, service, method string) context.Context

// Build creates the HTTP request and ResponseRecorder.
// Uses the contextSetup provided to NewRequest().
func (b *RequestBuilder) Build() (*http.Request, *httptest.ResponseRecorder) {
	path := b.path
	if len(b.queryParams) > 0 {
		params := []string{}
		for k, v := range b.queryParams {
			params = append(params, k+"="+v)
		}
		path += "?" + strings.Join(params, "&")
	}

	var bodyReader *bytes.Reader
	if len(b.body) > 0 {
		bodyReader = bytes.NewReader(b.body)
	}

	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequest(b.httpMethod, path, bodyReader)
	} else {
		req = httptest.NewRequest(b.httpMethod, path, nil)
	}

	for k, v := range b.headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()

	// Set up service context if provided
	if b.contextSetup != nil {
		ctx := b.contextSetup(req.Context(), w, req, b.service, b.method)
		req = req.WithContext(ctx)
	}

	return req, w
}

// AssertStatus checks that the response has the expected status code.
func AssertStatus(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) {
	t.Helper()
	if w.Code != expectedStatus {
		t.Errorf("expected status %d, got %d\nBody: %s", expectedStatus, w.Code, w.Body.String())
	}
}

// responseEnvelope is the expected response format with result/error discrimination.
type responseEnvelope struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorResponse  `json:"error,omitempty"`
}

// AssertJSONResponse decodes the response body and compares it with expected value.
// Expects the response to be wrapped in {"result": ...} envelope.
func AssertJSONResponse(t *testing.T, w *httptest.ResponseRecorder, expected any) {
	t.Helper()

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected Content-Type to contain application/json, got %s", contentType)
	}

	// Decode the envelope
	var envelope responseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response envelope: %v\nBody: %s", err, w.Body.String())
	}

	if envelope.Error != nil {
		t.Fatalf("expected success response but got error: %s: %s", envelope.Error.Code, envelope.Error.Message)
	}

	// Compare the result field with expected
	expectedJSON, _ := json.Marshal(expected)

	var expectedData, actualData any
	json.Unmarshal(expectedJSON, &expectedData)
	json.Unmarshal(envelope.Result, &actualData)

	expectedStr, _ := json.MarshalIndent(expectedData, "", "  ")
	actualStr, _ := json.MarshalIndent(actualData, "", "  ")

	if string(expectedStr) != string(actualStr) {
		t.Errorf("response mismatch:\nExpected:\n%s\nActual:\n%s", expectedStr, actualStr)
	}
}

// ErrorResponse represents a generic error response with code and message.
type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// AssertJSONError checks that the response contains an error with the expected code.
// Expects the error to be wrapped in {"error": {...}} envelope.
func AssertJSONError(t *testing.T, w *httptest.ResponseRecorder, expectedCode string) *ErrorResponse {
	t.Helper()

	var envelope responseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode error response envelope: %v\nBody: %s", err, w.Body.String())
	}

	if envelope.Error == nil {
		t.Fatalf("expected error response but got result: %s", string(envelope.Result))
	}

	if envelope.Error.Code != expectedCode {
		t.Errorf("expected error code %s, got %s (message: %s)", expectedCode, envelope.Error.Code, envelope.Error.Message)
	}

	return envelope.Error
}

// AssertHeader checks that a response header has the expected value.
func AssertHeader(t *testing.T, w *httptest.ResponseRecorder, key, expectedValue string) {
	t.Helper()
	actual := w.Header().Get(key)
	if actual != expectedValue {
		t.Errorf("expected header %s=%s, got %s", key, expectedValue, actual)
	}
}

// DecodeJSON decodes the response body into the provided value.
func DecodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode response: %v\nBody: %s", err, w.Body.String())
	}
}
