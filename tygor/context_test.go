package tygor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testContext implements Context for testing interceptors.
type testContext struct {
	context.Context
	service string
	name    string
	request *http.Request
	writer  http.ResponseWriter
}

func (c *testContext) Service() string                 { return c.service }
func (c *testContext) EndpointID() string              { return c.service + "." + c.name }
func (c *testContext) HTTPRequest() *http.Request      { return c.request }
func (c *testContext) HTTPWriter() http.ResponseWriter { return c.writer }

// newSimpleTestContext creates a minimal test context with the given service and method.
// Use this for testing the Context interface methods - it doesn't have HTTP primitives.
func newSimpleTestContext(parent context.Context, service, method string) Context {
	return &testContext{
		Context: parent,
		service: service,
		name:    method,
	}
}

func TestContext_Service(t *testing.T) {
	ctx := newSimpleTestContext(context.Background(), "TestService", "TestMethod")

	if ctx.Service() != "TestService" {
		t.Errorf("expected service 'TestService', got %s", ctx.Service())
	}
}

func TestContext_EndpointID(t *testing.T) {
	ctx := newSimpleTestContext(context.Background(), "TestService", "TestMethod")

	if ctx.EndpointID() != "TestService.TestMethod" {
		t.Errorf("expected endpoint 'TestService.TestMethod', got %s", ctx.EndpointID())
	}
}

func TestContext_HTTPRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newContext(context.Background(), w, req, "TestService", "TestMethod")

	if ctx.HTTPRequest() != req {
		t.Error("expected HTTPRequest to return the request")
	}
}

func TestContext_HTTPWriter(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newContext(context.Background(), w, req, "TestService", "TestMethod")

	if ctx.HTTPWriter() != w {
		t.Error("expected HTTPWriter to return the response writer")
	}
}

func TestContext_HTTPWriter_SetHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newContext(context.Background(), w, req, "TestService", "TestMethod")

	ctx.HTTPWriter().Header().Set("X-Custom-Header", "custom-value")

	if w.Header().Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected header to be set, got %s", w.Header().Get("X-Custom-Header"))
	}
}

func TestFromContext_Found(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newContext(context.Background(), w, req, "TestService", "TestMethod")

	tc, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected FromContext to return true")
	}
	if tc.EndpointID() != "TestService.TestMethod" {
		t.Errorf("expected endpoint 'TestService.TestMethod', got %s", tc.EndpointID())
	}
}

func TestFromContext_NotFound(t *testing.T) {
	ctx := context.Background()
	tc, ok := FromContext(ctx)
	if ok {
		t.Error("expected FromContext to return false")
	}
	if tc != nil {
		t.Error("expected nil context")
	}
}

func TestFromContext_AfterWithValue(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newContext(context.Background(), w, req, "TestService", "TestMethod")

	// Wrap the context with a new value - FromContext should still work
	type ctxKey string
	wrappedCtx := context.WithValue(ctx, ctxKey("key"), "value")

	tc, ok := FromContext(wrappedCtx)
	if !ok {
		t.Fatal("expected FromContext to return true after WithValue")
	}
	if tc.Service() != "TestService" {
		t.Errorf("expected service 'TestService', got %s", tc.Service())
	}
}

func TestContext_Basic(t *testing.T) {
	ctx := newSimpleTestContext(context.Background(), "TestService", "TestMethod")

	if ctx.EndpointID() != "TestService.TestMethod" {
		t.Errorf("expected endpoint 'TestService.TestMethod', got %s", ctx.EndpointID())
	}

	// HTTPRequest and HTTPWriter should be nil when created via newSimpleTestContext
	if ctx.HTTPRequest() != nil {
		t.Error("expected HTTPRequest to be nil")
	}
	if ctx.HTTPWriter() != nil {
		t.Error("expected HTTPWriter to be nil")
	}
}

func TestContext_ImplementsContextInterface(t *testing.T) {
	ctx := newSimpleTestContext(context.Background(), "TestService", "TestMethod")

	// Should be usable as context.Context
	var _ context.Context = ctx

	// Should be able to call context.Context methods
	if ctx.Done() != nil {
		t.Error("expected Done() to return nil for background context")
	}
	if ctx.Err() != nil {
		t.Error("expected Err() to return nil")
	}
}

func TestContext_ValuePropagation(t *testing.T) {
	type ctxKey string
	key := ctxKey("test-key")

	parent := context.WithValue(context.Background(), key, "test-value")
	ctx := newSimpleTestContext(parent, "TestService", "TestMethod")

	val := ctx.Value(key)
	if val != "test-value" {
		t.Errorf("expected 'test-value', got %v", val)
	}
}
