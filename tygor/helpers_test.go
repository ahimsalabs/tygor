package tygor

import (
	"log/slog"
	"net/http"
	"net/http/httptest"

	"tygor.dev/internal/tygortest"
)

// TestRequestBuilder wraps tygortest.RequestBuilder for use in tygor package tests.
type TestRequestBuilder struct {
	*tygortest.RequestBuilder
}

// NewTestRequest creates a new test request builder for tygor handlers.
func NewTestRequest() *TestRequestBuilder {
	return &TestRequestBuilder{
		RequestBuilder: tygortest.NewRequest(tygortest.ContextSetup()),
	}
}

// POST sets the HTTP method to POST and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) POST(path string) *TestRequestBuilder {
	tr.RequestBuilder.POST(path)
	return tr
}

// GET sets the HTTP method to GET and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) GET(path string) *TestRequestBuilder {
	tr.RequestBuilder.GET(path)
	return tr
}

// WithJSON sets the request body as JSON and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) WithJSON(v any) *TestRequestBuilder {
	tr.RequestBuilder.WithJSON(v)
	return tr
}

// WithBody sets the raw request body and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) WithBody(body string) *TestRequestBuilder {
	tr.RequestBuilder.WithBody(body)
	return tr
}

// WithHeader adds a header to the request and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) WithHeader(key, value string) *TestRequestBuilder {
	tr.RequestBuilder.WithHeader(key, value)
	return tr
}

// WithQuery adds a query parameter and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) WithQuery(key, value string) *TestRequestBuilder {
	tr.RequestBuilder.WithQuery(key, value)
	return tr
}

// WithServiceInfo sets the service and method for context and returns the TestRequestBuilder for chaining.
func (tr *TestRequestBuilder) WithServiceInfo(service, method string) *TestRequestBuilder {
	tr.RequestBuilder.WithServiceInfo(service, method)
	return tr
}

// Build creates the HTTP request with tygor context.
func (tr *TestRequestBuilder) Build() (*http.Request, *httptest.ResponseRecorder) {
	return tr.RequestBuilder.Build()
}

// testContextConfig holds configuration for creating test contexts.
type testContextConfig struct {
	errorTransformer   ErrorTransformer
	maskInternalErrors bool
	interceptors       []UnaryInterceptor
	logger             *slog.Logger
	maxRequestBodySize uint64
}

// ServeHandler builds the request and serves it to a tygor handler.
// For testing, it accepts a testContextConfig to configure the context.
func (tr *TestRequestBuilder) ServeHandler(handler Endpoint, config testContextConfig) *httptest.ResponseRecorder {
	req, w := tr.Build()
	h := handler.(endpointHandler)

	// Create internal context with config
	ctx := newContext(req.Context(), w, req, "TestService", "TestMethod")
	ctx.errorTransformer = config.errorTransformer
	ctx.maskInternalErrors = config.maskInternalErrors
	ctx.interceptors = config.interceptors
	ctx.logger = config.logger
	ctx.maxRequestBodySize = config.maxRequestBodySize

	h.serveHTTP(ctx)
	return w
}

// newRpcTestContext creates an rpcContext for testing with the given request/response and config.
func newRpcTestContext(w http.ResponseWriter, r *http.Request, config testContextConfig) *rpcContext {
	ctx := newContext(r.Context(), w, r, "TestService", "TestMethod")
	ctx.errorTransformer = config.errorTransformer
	ctx.maskInternalErrors = config.maskInternalErrors
	ctx.interceptors = config.interceptors
	ctx.logger = config.logger
	ctx.maxRequestBodySize = config.maxRequestBodySize
	return ctx
}
