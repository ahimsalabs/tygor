package tygor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tygor.dev/internal/tygortest"
)

type panicJSONDetail struct{}

func (panicJSONDetail) MarshalJSON() ([]byte, error) {
	panic("private detail marshaler panic")
}

type panicErrorResponseWriter struct {
	header      http.Header
	writes      int
	statusCalls int
}

func (w *panicErrorResponseWriter) Header() http.Header { return w.header }

func (w *panicErrorResponseWriter) WriteHeader(int) { w.statusCalls++ }

func (w *panicErrorResponseWriter) Write([]byte) (int, error) {
	w.writes++
	panic("transport panic")
}

type frameworkPanicWriter struct {
	header      http.Header
	panicAt     string
	panicValue  any
	headerCalls int
	statusCalls int
	writes      int
}

func (w *frameworkPanicWriter) Header() http.Header {
	w.headerCalls++
	if w.panicAt == "header" {
		panic(w.panicValue)
	}
	return w.header
}

func (w *frameworkPanicWriter) WriteHeader(int) {
	w.statusCalls++
	if w.panicAt == "write header" {
		panic(w.panicValue)
	}
}

func (w *frameworkPanicWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.panicAt == "write" {
		panic(w.panicValue)
	}
	return len(data), nil
}

func serveAndRecover(handler http.Handler, writer http.ResponseWriter, req *http.Request) (recovered any) {
	defer func() { recovered = recover() }()
	handler.ServeHTTP(writer, req)
	return nil
}

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

	reg := NewApp(WithErrorTransformer(transformer))
	if reg.errorTransformer == nil {
		t.Error("expected error transformer to be set")
	}
}

func TestApp_WithMaskInternalErrors(t *testing.T) {
	reg := NewApp(WithMaskInternalErrors())
	if !reg.maskInternalErrors {
		t.Error("expected maskInternalErrors to be true")
	}
}

func TestApp_WithUnaryInterceptor(t *testing.T) {
	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	reg := NewApp(WithUnaryInterceptors(interceptor))
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

	reg := NewApp(WithHTTPMiddleware(middleware))
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

	reg := NewApp(WithHTTPMiddleware(middleware))

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}
	reg.Service("Test").Exec("Method", fn)

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

func TestApp_MiddlewareConstructedOnce(t *testing.T) {
	constructed := 0
	middleware := func(next http.Handler) http.Handler {
		constructed++
		return next
	}
	app := NewApp(WithHTTPMiddleware(middleware))

	if constructed != 1 {
		t.Fatalf("middleware constructed %d times, want 1", constructed)
	}
	app.Handler()
	app.Handler()
	if constructed != 1 {
		t.Fatalf("middleware reconstructed by Handler: got %d constructions", constructed)
	}
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

	reg.Service("Test").Exec("Method", fn)

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
	called := false
	reg.Service("Test").Exec("Method", func(ctx context.Context, req Empty) (Empty, error) {
		called = true
		return nil, nil
	})

	tests := []struct {
		name string
		path string
	}{
		{"no slash", "/NoSlash"},
		{"root", "/"},
		{"missing service", "//Method"},
		{"missing method", "/Test/"},
		{"trailing slash", "/Test/Method/"},
		{"extra component", "/Test/Method/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			w := httptest.NewRecorder()

			reg.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", w.Code)
			}
			if called {
				t.Fatal("registered handler ran for an invalid path")
			}
		})
	}
}

func TestApp_Handler_MethodMismatch(t *testing.T) {
	reg := NewApp()

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	reg.Service("Test").Exec("Method", fn)

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

	transformerCalled := false
	reg := NewApp(
		WithLogger(logger),
		WithErrorTransformer(func(err error) *Error {
			transformerCalled = true
			return NewError(CodeInternal, "private transformed panic")
		}),
		WithMaskInternalErrors(),
	)

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		panic("private test panic")
	}

	reg.Service("Test").Exec("Method", fn)

	reqBody := `{"name":"John","email":"john@example.com"}`
	req := httptest.NewRequest("POST", "/Test/Method", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusInternalServerError)
	errResp := tygortest.AssertJSONError(t, w, string(CodeInternal))
	if errResp.Message != "internal server error" {
		t.Fatalf("panic message = %q, want masked internal error", errResp.Message)
	}
	if strings.Contains(w.Body.String(), "private test panic") {
		t.Fatalf("panic value leaked to response: %s", w.Body.String())
	}
	if !transformerCalled {
		t.Fatal("panic did not use the configured error transformer")
	}

	// Verify panic was logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "PANIC recovered") {
		t.Errorf("expected panic log, got: %s", logOutput)
	}
}

func TestApp_ErrorPolicyPanicFallsBackOverHTTP(t *testing.T) {
	const fallback = "{\"error\":{\"code\":\"internal\",\"message\":\"internal server error\"}}\n"
	tests := []struct {
		name        string
		handler     func(context.Context, TestRequest) (TestResponse, error)
		transformer func(*atomic.Int32) ErrorTransformer
	}{
		{
			name: "returned error with panicking transformer",
			handler: func(context.Context, TestRequest) (TestResponse, error) {
				return TestResponse{}, NewError(CodeUnavailable, "private returned error")
			},
			transformer: func(calls *atomic.Int32) ErrorTransformer {
				return func(error) *Error {
					calls.Add(1)
					panic("private transformer panic")
				}
			},
		},
		{
			name: "handler panic with panicking transformer",
			handler: func(context.Context, TestRequest) (TestResponse, error) {
				panic("private handler panic")
			},
			transformer: func(calls *atomic.Int32) ErrorTransformer {
				return func(error) *Error {
					calls.Add(1)
					panic("private transformer panic")
				}
			},
		},
		{
			name: "error envelope serialization panic",
			handler: func(context.Context, TestRequest) (TestResponse, error) {
				return TestResponse{}, NewError(CodeUnavailable, "private returned error")
			},
			transformer: func(calls *atomic.Int32) ErrorTransformer {
				return func(error) *Error {
					calls.Add(1)
					return NewError(CodeUnavailable, "private transformed error").WithDetail("value", panicJSONDetail{})
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			app := NewApp(WithErrorTransformer(tt.transformer(&calls)))
			app.Service("Test").Exec("Method", tt.handler)
			server := httptest.NewServer(app.Handler())
			t.Cleanup(server.Close)

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Post(server.URL+"/Test/Method", "application/json", strings.NewReader(`{"name":"John","email":"john@example.com"}`))
			if err != nil {
				t.Fatalf("POST error = %v, want sanitized HTTP response", err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %q, want 500 application/json", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if string(body) != fallback {
				t.Fatalf("body = %q, want constant fallback %q", body, fallback)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("transformer calls = %d, want exactly 1", got)
			}
		})
	}
}

func TestApp_ErrorTransportPanicDoesNotReenterPolicy(t *testing.T) {
	var calls atomic.Int32
	app := NewApp(WithErrorTransformer(func(error) *Error {
		calls.Add(1)
		return NewError(CodeUnavailable, "safe transformed error")
	}))
	app.Service("Test").Exec("Method", func(context.Context, TestRequest) (TestResponse, error) {
		return TestResponse{}, NewError(CodeUnavailable, "private returned error")
	})
	req := httptest.NewRequest(http.MethodPost, "/Test/Method", strings.NewReader(`{"name":"John","email":"john@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := &panicErrorResponseWriter{header: make(http.Header)}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		app.Handler().ServeHTTP(w, req)
	}()

	if recovered != "transport panic" {
		t.Fatalf("recovered = %v, want original transport panic", recovered)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("transformer calls = %d, want exactly 1", got)
	}
	if w.statusCalls != 1 || w.writes != 1 {
		t.Fatalf("transport attempts = status:%d write:%d, want 1/1", w.statusCalls, w.writes)
	}
}

func TestApp_RouteErrorTransportPanicDoesNotRetry(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		panicAt    string
		wantHeader int
		wantStatus int
		wantWrites int
		configure  func(*App)
	}{
		{name: "invalid path header", method: http.MethodPost, path: "/invalid", panicAt: "header", wantHeader: 1},
		{name: "missing route write header", method: http.MethodPost, path: "/missing/route", panicAt: "write header", wantHeader: 1, wantStatus: 1},
		{
			name:       "wrong method write",
			method:     http.MethodGet,
			path:       "/Test/Method",
			panicAt:    "write",
			wantHeader: 1,
			wantStatus: 1,
			wantWrites: 1,
			configure: func(app *App) {
				app.Service("Test").Exec("Method", func(context.Context, Empty) (Empty, error) {
					return nil, nil
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicValue := &struct{}{}
			var transformerCalls atomic.Int32
			app := NewApp(WithErrorTransformer(func(error) *Error {
				transformerCalls.Add(1)
				return NewError(CodeInternal, "transformed")
			}))
			if tt.configure != nil {
				tt.configure(app)
			}
			writer := &frameworkPanicWriter{
				header:     make(http.Header),
				panicAt:    tt.panicAt,
				panicValue: panicValue,
			}
			req := httptest.NewRequest(tt.method, tt.path, nil)

			recovered := serveAndRecover(app.Handler(), writer, req)

			if recovered != panicValue {
				t.Fatalf("recovered = %v, want original transport panic", recovered)
			}
			if got := transformerCalls.Load(); got != 0 {
				t.Fatalf("transformer calls = %d, want 0", got)
			}
			if writer.headerCalls != tt.wantHeader || writer.statusCalls != tt.wantStatus || writer.writes != tt.wantWrites {
				t.Fatalf("transport attempts = header:%d status:%d write:%d, want %d/%d/%d", writer.headerCalls, writer.statusCalls, writer.writes, tt.wantHeader, tt.wantStatus, tt.wantWrites)
			}
		})
	}
}

func TestApp_SuccessTransportPanicDoesNotRetry(t *testing.T) {
	tests := []struct {
		name       string
		panicAt    string
		wantHeader int
		wantWrites int
	}{
		{name: "header", panicAt: "header", wantHeader: 1},
		{name: "write", panicAt: "write", wantHeader: 1, wantWrites: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicValue := &struct{}{}
			var transformerCalls atomic.Int32
			app := NewApp(WithErrorTransformer(func(error) *Error {
				transformerCalls.Add(1)
				return NewError(CodeInternal, "transformed")
			}))
			app.Service("Test").Exec("Method", func(context.Context, Empty) (TestResponse, error) {
				return TestResponse{Message: "ok"}, nil
			})
			writer := &frameworkPanicWriter{
				header:     make(http.Header),
				panicAt:    tt.panicAt,
				panicValue: panicValue,
			}
			req := httptest.NewRequest(http.MethodPost, "/Test/Method", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			recovered := serveAndRecover(app.Handler(), writer, req)

			if recovered != panicValue {
				t.Fatalf("recovered = %v, want original transport panic", recovered)
			}
			if got := transformerCalls.Load(); got != 0 {
				t.Fatalf("transformer calls = %d, want 0", got)
			}
			if writer.headerCalls != tt.wantHeader || writer.statusCalls != 0 || writer.writes != tt.wantWrites {
				t.Fatalf("transport attempts = header:%d status:%d write:%d, want %d/0/%d", writer.headerCalls, writer.statusCalls, writer.writes, tt.wantHeader, tt.wantWrites)
			}
		})
	}
}

func TestApp_GlobalInterceptor(t *testing.T) {
	interceptorCalled := false

	reg := NewApp(WithUnaryInterceptors(func(ctx Context, req any, handler HandlerFunc) (any, error) {
		interceptorCalled = true
		if ctx.EndpointID() != "Test.Method" {
			t.Errorf("unexpected endpoint: %s", ctx.EndpointID())
		}
		return handler(ctx, req)
	}))

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test").Exec("Method", fn)

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

	service := reg.Service("Test", WithUnaryInterceptors(interceptor))

	if len(service.interceptors) != 1 {
		t.Errorf("expected 1 service interceptor, got %d", len(service.interceptors))
	}
}

func TestService_ExecRegistersHandler(t *testing.T) {
	reg := NewApp()
	service := reg.Service("Test")

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	service.Exec("Method", fn)

	reg.mu.RLock()
	defer reg.mu.RUnlock()

	if _, ok := reg.routes["Test.Method"]; !ok {
		t.Error("expected route to be registered")
	}
}

func TestService_TypedRegistrationMethods(t *testing.T) {
	type execRequest struct{ Value string }
	type execResponse struct{ Value int }
	type queryRequest struct{ Value int }
	type queryResponse struct{ Value string }
	type streamRequest struct{ Value bool }
	type streamResponse struct{ Value float64 }

	app := NewApp()
	service := app.Service("Test")
	service.Exec("Exec", func(context.Context, execRequest) (execResponse, error) {
		return execResponse{}, nil
	})
	service.Query("Query", func(context.Context, queryRequest) (queryResponse, error) {
		return queryResponse{}, nil
	}, WithCacheControl(CacheConfig{MaxAge: time.Minute}))
	service.Stream("Stream", func(context.Context, streamRequest, StreamWriter[streamResponse]) error {
		return nil
	})

	app.mu.RLock()
	queryHandler, ok := app.routes["Test.Query"].(*queryHandler[queryRequest, queryResponse])
	app.mu.RUnlock()
	if !ok {
		t.Fatal("query route did not contain the typed query handler")
	}
	if queryHandler.cacheConfig == nil || queryHandler.cacheConfig.MaxAge != time.Minute {
		t.Fatal("typed registration method did not apply endpoint options")
	}

	routes := app.Routes()
	tests := []struct {
		name      string
		primitive string
		request   any
		response  any
	}{
		{name: "Test.Exec", primitive: "exec", request: execRequest{}, response: execResponse{}},
		{name: "Test.Query", primitive: "query", request: queryRequest{}, response: queryResponse{}},
		{name: "Test.Stream", primitive: "stream", request: streamRequest{}, response: streamResponse{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata, ok := routes[tt.name]
			if !ok {
				t.Fatalf("route %q was not registered", tt.name)
			}
			if metadata.Primitive != tt.primitive {
				t.Errorf("primitive = %q, want %q", metadata.Primitive, tt.primitive)
			}
			if metadata.Request != reflect.TypeOf(tt.request) {
				t.Errorf("request type = %v, want %T", metadata.Request, tt.request)
			}
			if metadata.Response != reflect.TypeOf(tt.response) {
				t.Errorf("response type = %v, want %T", metadata.Response, tt.response)
			}
		})
	}
}

func TestApp_DuplicateRouteRegistration(t *testing.T) {
	// Use a test logger to verify duplicate registration warning
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	reg := NewApp(WithLogger(logger))

	fn1 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "first"}, nil
	}

	fn2 := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "second"}, nil
	}

	// Register the same route twice
	reg.Service("Test").Exec("Method", fn1)
	reg.Service("Test").Exec("Method", fn2)

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

	reg := NewApp(WithUnaryInterceptors(globalInterceptor))

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		callOrder = append(callOrder, "fn")
		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test", WithUnaryInterceptors(serviceInterceptor)).
		Exec("Method", fn, WithUnaryInterceptors(handlerInterceptor))

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

	reg.Service("Test").Exec("Method", fn)

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

	reg.Service("Service1").Exec("Method1", fn1)
	reg.Service("Service2").Exec("Method2", fn2)

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

	reg := NewApp(WithHTTPMiddleware(mw1, mw2))

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		callOrder = append(callOrder, "handler")
		return TestResponse{Message: "ok"}, nil
	}

	reg.Service("Test").Exec("Method", fn)

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

func TestQueryHandler_Metadata(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := newQueryHandler(fn)

	meta := handler.metadata()
	if meta.Primitive != "query" {
		t.Errorf("expected Primitive query, got %s", meta.Primitive)
	}
}

func TestApp_WithMaskInternalErrors_Integration(t *testing.T) {
	reg := NewApp(WithMaskInternalErrors())

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, NewError(CodeInternal, "sensitive internal error")
	}

	reg.Service("Test").Exec("Method", fn)

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
