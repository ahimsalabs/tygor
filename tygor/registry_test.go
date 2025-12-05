package tygor

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tygor.dev/internal/tygortest"
)

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.routes == nil {
		t.Error("expected routes map to be initialized")
	}
}

func TestApp_WithErrorTransformer(t *testing.T) {
	transformer := func(err error) *Error {
		return NewError(CodeInternal, "transformed")
	}

	reg := NewApp().WithErrorTransformer(transformer)
	if reg.errorTransformer == nil {
		t.Error("expected error transformer to be set")
	}
}

func TestApp_WithMaskInternalErrors(t *testing.T) {
	reg := NewApp().WithMaskInternalErrors()
	if !reg.maskInternalErrors {
		t.Error("expected maskInternalErrors to be true")
	}
}

func TestApp_WithUnaryInterceptor(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	reg := NewApp().WithUnaryInterceptor(interceptor)
	if len(reg.interceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(reg.interceptors))
	}
}

func TestApp_WithMiddleware(t *testing.T) {
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	reg := NewApp().WithMiddleware(middleware)
	if len(reg.middlewares) != 1 {
		t.Errorf("expected 1 middleware, got %d", len(reg.middlewares))
	}
}

func TestApp_Handler(t *testing.T) {
	middlewareCalled := false
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, r)
		})
	}

	reg := NewApp().WithMiddleware(middleware)

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}
	reg.Service("Test").Register("Method", Exec(fn))

	handler := reg.Handler()
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !middlewareCalled {
		t.Error("expected middleware to be called")
	}
	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ok"})
}

func TestApp_Service(t *testing.T) {
	reg := NewApp()
	service := reg.Service("TestService")

	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.name != "TestService" {
		t.Errorf("expected service name 'TestService', got %s", service.name)
	}
	if service.registry != reg {
		t.Error("expected service to reference parent registry")
	}
}

func TestApp_Handler_Success(t *testing.T) {
	reg := NewApp()

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name, ID: 123}, nil
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John", ID: 123})
}

func TestApp_Handler_NotFound(t *testing.T) {
	reg := NewApp()

	req := httptest.NewRequest("POST", "/NonExistent/Method", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusNotFound)
	tygortest.AssertJSONError(t, w, string(CodeNotFound))
}

func TestApp_Handler_InvalidPath(t *testing.T) {
	reg := NewApp()

	tests := []struct {
		name string
		path string
	}{
		{"no slash", "/NoSlash"},
		{"root", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			w := httptest.NewRecorder()

			reg.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", w.Code)
			}
		})
	}
}

func TestApp_Handler_MethodMismatch(t *testing.T) {
	reg := NewApp()

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	reg.Service("Test").Register("Method", Exec(fn))

	// Try GET when handler expects POST
	req := httptest.NewRequest("GET", "/Test/Method", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusMethodNotAllowed)
	tygortest.AssertJSONError(t, w, string(CodeMethodNotAllowed))
}

func TestApp_Handler_WithPanic(t *testing.T) {
	// Use a test logger to verify panic logging
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	reg := NewApp().WithLogger(logger)

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		panic("test panic")
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusInternalServerError)
	tygortest.AssertJSONError(t, w, string(CodeInternal))

	// Verify panic was logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "PANIC recovered") {
		t.Errorf("expected panic log, got: %s", logOutput)
	}
}

func TestApp_GlobalInterceptor(t *testing.T) {
	interceptorCalled := false

	reg := NewApp().WithUnaryInterceptor(func(ctx Context, req any, handler HandlerFunc) (any, error) {
		interceptorCalled = true
		if ctx.EndpointID() != "Test.Method" {
			t.Errorf("unexpected endpoint: %s", ctx.EndpointID())
		}
		return handler(ctx, req)
	})

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	if !interceptorCalled {
		t.Error("expected global interceptor to be called")
	}
	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ok"})
}

func TestService_WithUnaryInterceptor(t *testing.T) {
	reg := NewApp()

	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	service := reg.Service("Test").WithUnaryInterceptor(interceptor)

	if len(service.interceptors) != 1 {
		t.Errorf("expected 1 service interceptor, got %d", len(service.interceptors))
	}
}

func TestService_Register(t *testing.T) {
	reg := NewApp()
	service := reg.Service("Test")

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Exec(fn)
	service.Register("Method", handler)

	reg.mu.RLock()
	defer reg.mu.RUnlock()

	if _, ok := reg.routes["Test.Method"]; !ok {
		t.Error("expected route to be registered")
	}
}

func TestApp_DuplicateRouteRegistration(t *testing.T) {
	// Use a test logger to verify duplicate registration warning
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	reg := NewApp().WithLogger(logger)

	fn1 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "first"}, nil
	}

	fn2 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "second"}, nil
	}

	// Register the same route twice
	reg.Service("Test").Register("Method", Exec(fn1))
	reg.Service("Test").Register("Method", Exec(fn2))

	// Verify warning was logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "duplicate route registration") {
		t.Errorf("expected duplicate registration warning, got: %s", logOutput)
	}

	// Verify second handler is used
	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "second"})
}

func TestService_InterceptorOrder(t *testing.T) {
	var callOrder []string

	globalInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "global")
		return handler(ctx, req)
	}

	serviceInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "service")
		return handler(ctx, req)
	}

	handlerInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "handler")
		return handler(ctx, req)
	}

	reg := NewApp().WithUnaryInterceptor(globalInterceptor)

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		callOrder = append(callOrder, "fn")
		return TestResponse{Message: "ok"}, nil
	}

	handler := Exec(fn).WithUnaryInterceptor(handlerInterceptor)
	reg.Service("Test").WithUnaryInterceptor(serviceInterceptor).Register("Method", handler)

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	// Expected order: global -> service -> handler -> fn
	expectedOrder := []string{"global", "service", "handler", "fn"}
	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedOrder), len(callOrder), callOrder)
	}
	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("at position %d: expected %s, got %s", i, expected, callOrder[i])
		}
	}
}

func TestApp_ContextPropagation(t *testing.T) {
	reg := NewApp()

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		// Verify context has request, writer, and service info via FromContext
		tc, ok := FromContext(ctx)
		if !ok {
			t.Fatal("expected tygor context")
		}

		if tc.HTTPRequest() == nil {
			t.Error("expected request in context")
		}

		if tc.EndpointID() != "Test.Method" {
			t.Errorf("expected endpoint 'Test.Method', got %s", tc.EndpointID())
		}

		// Test setting header via HTTPWriter
		tc.HTTPWriter().Header().Set("X-Custom", "value")

		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	if w.Header().Get("X-Custom") != "value" {
		t.Errorf("expected custom header to be set, got %s", w.Header().Get("X-Custom"))
	}
}

func TestApp_MultipleServices(t *testing.T) {
	reg := NewApp()

	fn1 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "service1"}, nil
	}

	fn2 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "service2"}, nil
	}

	reg.Service("Service1").Register("Method1", Exec(fn1))
	reg.Service("Service2").Register("Method2", Exec(fn2))

	tests := []struct {
		path            string
		expectedMessage string
	}{
		{"/Service1/Method1", "service1"},
		{"/Service2/Method2", "service2"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			reqBody := `{"name":"John","email":"john@example.com"}`
			req := httptest.NewRequest("POST", tt.path, strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			reg.Handler().ServeHTTP(w, req)

			tygortest.AssertStatus(t, w, http.StatusOK)
			tygortest.AssertJSONResponse(t, w, TestResponse{Message: tt.expectedMessage})
		})
	}
}

func TestApp_MiddlewareOrder(t *testing.T) {
	var callOrder []string

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "mw1-before")
			next.ServeHTTP(w, r)
			callOrder = append(callOrder, "mw1-after")
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "mw2-before")
			next.ServeHTTP(w, r)
			callOrder = append(callOrder, "mw2-after")
		})
	}

	reg := NewApp().WithMiddleware(mw1).WithMiddleware(mw2)

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		callOrder = append(callOrder, "handler")
		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler := reg.Handler()
	handler.ServeHTTP(w, req)

	// First added middleware is outermost: mw1 -> mw2 -> handler
	expectedOrder := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedOrder), len(callOrder), callOrder)
	}
	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("at position %d: expected %s, got %s", i, expected, callOrder[i])
		}
	}
}

func TestServiceWrappedHandler_Metadata(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Query(fn)

	wrapped := &serviceWrappedHandler{
		inner:        handler,
		interceptors: []UnaryInterceptor{},
	}

	meta := wrapped.metadata()
	if meta.Primitive != "query" {
		t.Errorf("expected Primitive query, got %s", meta.Primitive)
	}
}

func TestApp_WithMaskInternalErrors_Integration(t *testing.T) {
	reg := NewApp().WithMaskInternalErrors()

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, NewError(CodeInternal, "sensitive internal error")
	}

	reg.Service("Test").Register("Method", Exec(fn))

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	var envelope struct {
		Error *Error `json:"error"`
	}
	json.NewDecoder(w.Body).Decode(&envelope)

	if envelope.Error.Message == "sensitive internal error" {
		t.Error("expected internal error to be masked")
	}
	if envelope.Error.Message != "internal server error" {
		t.Errorf("expected generic error message, got %s", envelope.Error.Message)
	}
}
