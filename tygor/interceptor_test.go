package tygor

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// interceptorTestContext implements Context for testing interceptors.
type interceptorTestContext struct {
	context.Context
	service string
	name    string
}

func (c *interceptorTestContext) Service() string                 { return c.service }
func (c *interceptorTestContext) EndpointID() string              { return c.service + "." + c.name }
func (c *interceptorTestContext) HTTPRequest() *http.Request      { return nil }
func (c *interceptorTestContext) HTTPWriter() http.ResponseWriter { return nil }

func newInterceptorTestContext(parent context.Context, service, method string) Context {
	return &interceptorTestContext{
		Context: parent,
		service: service,
		name:    method,
	}
}

func TestChainInterceptors_Empty(t *testing.T) {
	chain := chainInterceptors([]UnaryInterceptor{})
	if chain != nil {
		t.Error("expected nil chain for empty interceptors")
	}
}

func TestChainInterceptors_Single(t *testing.T) {
	called := false
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		called = true
		return handler(ctx, req)
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor})
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		return "result", nil
	}

	result, err := chain(ctx, "request", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("expected 'result', got %v", result)
	}
	if !called {
		t.Error("expected interceptor to be called")
	}
}

func TestChainInterceptors_Multiple(t *testing.T) {
	var order []string

	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		order = append(order, "before-1")
		res, err := handler(ctx, req)
		order = append(order, "after-1")
		return res, err
	}

	interceptor2 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		order = append(order, "before-2")
		res, err := handler(ctx, req)
		order = append(order, "after-2")
		return res, err
	}

	interceptor3 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		order = append(order, "before-3")
		res, err := handler(ctx, req)
		order = append(order, "after-3")
		return res, err
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor1, interceptor2, interceptor3})
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		order = append(order, "handler")
		return "result", nil
	}

	result, err := chain(ctx, "request", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("expected 'result', got %v", result)
	}

	expectedOrder := []string{"before-1", "before-2", "before-3", "handler", "after-3", "after-2", "after-1"}
	if len(order) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d", len(expectedOrder), len(order))
	}
	for i, expected := range expectedOrder {
		if order[i] != expected {
			t.Errorf("at position %d: expected %s, got %s", i, expected, order[i])
		}
	}
}

func TestChainInterceptors_ErrorPropagation(t *testing.T) {
	testErr := errors.New("test error")

	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	interceptor2 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// This interceptor returns an error
		return nil, testErr
	}

	interceptor3 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor1, interceptor2, interceptor3})

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when interceptor returns error")
		return nil, nil
	}

	result, err := chain(ctx, "request", handler)
	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestChainInterceptors_ModifyRequest(t *testing.T) {
	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// Modify request
		return handler(ctx, "modified")
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor1})

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		if req != "modified" {
			t.Errorf("expected modified request, got %v", req)
		}
		return req, nil
	}

	result, err := chain(ctx, "original", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "modified" {
		t.Errorf("expected 'modified', got %v", result)
	}
}

func TestChainInterceptors_ModifyResponse(t *testing.T) {
	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		res, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}
		// Modify response
		return res.(string) + "-modified", nil
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor1})

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		return "original", nil
	}

	result, err := chain(ctx, "request", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "original-modified" {
		t.Errorf("expected 'original-modified', got %v", result)
	}
}

func TestChainInterceptors_ContextPropagation(t *testing.T) {
	type ctxKey string
	key := ctxKey("test-key")

	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// Add value to context
		newCtx := context.WithValue(ctx, key, "test-value")
		return handler(newCtx, req)
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor1})

	ctx := newInterceptorTestContext(context.Background(), "Test", "Method")
	handler := func(ctx context.Context, req any) (any, error) {
		val := ctx.Value(key)
		if val != "test-value" {
			t.Errorf("expected 'test-value' in context, got %v", val)
		}
		return "success", nil
	}

	res, err := chain(ctx, "request", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res != "success" {
		t.Errorf("expected 'success', got %v", res)
	}
}

func TestChainInterceptors_ContextAccessible(t *testing.T) {
	expectedService := "TestService"
	expectedName := "TestMethod"
	expectedEndpointID := expectedService + "." + expectedName

	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		if ctx.EndpointID() != expectedEndpointID {
			t.Errorf("expected endpoint %s, got %s", expectedEndpointID, ctx.EndpointID())
		}
		return handler(ctx, req)
	}

	chain := chainInterceptors([]UnaryInterceptor{interceptor})

	ctx := newInterceptorTestContext(context.Background(), expectedService, expectedName)
	handler := func(ctx context.Context, req any) (any, error) {
		return "success", nil
	}

	res, err := chain(ctx, "request", handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res != "success" {
		t.Errorf("expected 'success', got %v", res)
	}
}
