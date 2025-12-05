package tygor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tygor.dev/internal/tygortest"
)

type TestRequest struct {
	Name  string `json:"name" validate:"required,min=3"`
	Email string `json:"email" validate:"required,email"`
}

type TestResponse struct {
	Message string `json:"message"`
	ID      int    `json:"id"`
}

func TestExec(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok", ID: 1}, nil
	}

	handler := Exec(fn)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.metadata().Primitive != "exec" {
		t.Errorf("expected Primitive exec, got %s", handler.metadata().Primitive)
	}
}

func TestHandler_Method(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Query(fn)
	if handler.metadata().Primitive != "query" {
		t.Errorf("expected Primitive query, got %s", handler.metadata().Primitive)
	}
}

func TestHandler_CacheControl(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	ttl := 5 * time.Minute
	handler := Query(fn).CacheControl(CacheConfig{MaxAge: ttl})
	if handler.cacheConfig == nil {
		t.Fatal("expected cacheConfig to be set")
	}
	if handler.cacheConfig.MaxAge != ttl {
		t.Errorf("expected cache TTL %v, got %v", ttl, handler.cacheConfig.MaxAge)
	}
}

func TestHandler_WithUnaryInterceptor(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		return handler(ctx, req)
	}

	handler := Exec(fn).WithUnaryInterceptor(interceptor)
	if len(handler.interceptors) != 1 {
		t.Errorf("expected 1 interceptor, got %d", len(handler.interceptors))
	}
}

func TestHandler_Metadata(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{MaxAge: 1 * time.Minute})
	meta := handler.metadata()

	if meta.Primitive != "query" {
		t.Errorf("expected Primitive query, got %s", meta.Primitive)
	}
	if handler.cacheConfig.MaxAge != 1*time.Minute {
		t.Errorf("expected cache MaxAge 1m, got %v", handler.cacheConfig.MaxAge)
	}
	if meta.Request == nil {
		t.Error("expected Request type to be set")
	}
	if meta.Response == nil {
		t.Error("expected Response type to be set")
	}
}

func TestHandler_ServeHTTP_POST_Success(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name, ID: 123}, nil
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John", ID: 123})
}

func TestHandler_ServeHTTP_POST_ValidationError(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Exec(fn)

	// Invalid email and name too short
	w := NewTestRequest().
		POST("/test").
		WithJSON(map[string]string{"name": "Jo", "email": "invalid"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

func TestHandler_ServeHTTP_POST_InvalidJSON(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, nil
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithBody(`{invalid json}`).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

func TestHandler_ServeHTTP_GET_WithQueryParams(t *testing.T) {
	type GetRequest struct {
		Name  string `schema:"name"`
		Email string `schema:"email"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Query(fn)

	w := NewTestRequest().
		GET("/test").
		WithQuery("name", "John").
		WithQuery("email", "john@example.com").
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John"})
}

func TestHandler_ServeHTTP_HandlerError(t *testing.T) {
	testErr := NewError(CodeNotFound, "resource not found")
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, testErr
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusNotFound)
	errResp := tygortest.AssertJSONError(t, w, string(CodeNotFound))

	if errResp.Message != "resource not found" {
		t.Errorf("expected message 'resource not found', got %s", errResp.Message)
	}
}

func TestHandler_ServeHTTP_WithCacheControl(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{MaxAge: 60 * time.Second})

	w := NewTestRequest().
		GET("/test").
		WithQuery("name", "John").
		WithQuery("email", "john@example.com").
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertHeader(t, w, "Cache-Control", "private, max-age=60")
}

func TestHandler_ServeHTTP_WithUnaryInterceptor(t *testing.T) {
	interceptorCalled := false

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		interceptorCalled = true
		return handler(ctx, req)
	}

	handler := Exec(fn).WithUnaryInterceptor(interceptor)

	NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	if !interceptorCalled {
		t.Error("expected interceptor to be called")
	}
}

func TestHandler_ServeHTTP_MaskInternalErrors(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, errors.New("internal database error")
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{
			maskInternalErrors: true,
		})

	errResp := tygortest.AssertJSONError(t, w, string(CodeInternal))

	if errResp.Message == "internal database error" {
		t.Error("expected internal error message to be masked")
	}
	if errResp.Message != "internal server error" {
		t.Errorf("expected message 'internal server error', got %s", errResp.Message)
	}
}

func TestHandler_ServeHTTP_CustomErrorTransformer(t *testing.T) {
	customErr := errors.New("custom error")
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{}, customErr
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{
			errorTransformer: func(err error) *Error {
				if err == customErr {
					return NewError(CodeUnavailable, "custom mapped error")
				}
				return nil
			},
		})

	errResp := tygortest.AssertJSONError(t, w, string(CodeUnavailable))

	if errResp.Message != "custom mapped error" {
		t.Errorf("expected message 'custom mapped error', got %s", errResp.Message)
	}
}

func TestHandler_ServeHTTP_EmptyBody(t *testing.T) {
	type EmptyRequest struct{}

	fn := func(ctx context.Context, req EmptyRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	handler := Exec(fn)

	// Send empty JSON object instead of nil body
	w := NewTestRequest().
		POST("/test").
		WithBody("{}").
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ok"})
}

func TestHandler_ServeHTTP_PointerRequest(t *testing.T) {
	fn := func(ctx context.Context, req *TestRequest) (TestResponse, error) {
		if req == nil {
			return TestResponse{}, errors.New("nil request")
		}
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John"})
}

func TestHandler_ServeHTTP_InterceptorModifiesRequest(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: req.Name}, nil
	}

	interceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// Modify the request
		r := req.(TestRequest)
		r.Name = "Modified"
		return handler(ctx, r)
	}

	handler := Exec(fn).WithUnaryInterceptor(interceptor)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "Original", Email: "test@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "Modified"})
}

// errorEnvelopeTest is used for decoding error responses in tests.
type errorEnvelopeTest struct {
	Error *Error `json:"error"`
}

func TestHandleError(t *testing.T) {
	t.Run("with custom error transformer", func(t *testing.T) {
		testErr := errors.New("test error")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/test", nil)

		ctx := newRpcTestContext(w, req, testContextConfig{
			errorTransformer: func(err error) *Error {
				if err == testErr {
					return NewError(CodeUnavailable, "transformed")
				}
				return nil
			},
		})

		handleError(ctx, testErr)

		var envelope errorEnvelopeTest
		json.NewDecoder(w.Body).Decode(&envelope)

		if envelope.Error.Code != CodeUnavailable {
			t.Errorf("expected code %s, got %s", CodeUnavailable, envelope.Error.Code)
		}
		if envelope.Error.Message != "transformed" {
			t.Errorf("expected message 'transformed', got %s", envelope.Error.Message)
		}
	})

	t.Run("fallback to default transformer", func(t *testing.T) {
		testErr := errors.New("test error")
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/test", nil)

		ctx := newRpcTestContext(w, req, testContextConfig{
			errorTransformer: func(err error) *Error {
				// Return nil to use default transformer
				return nil
			},
		})

		handleError(ctx, testErr)

		var envelope errorEnvelopeTest
		json.NewDecoder(w.Body).Decode(&envelope)

		if envelope.Error.Code != CodeInternal {
			t.Errorf("expected code %s, got %s", CodeInternal, envelope.Error.Code)
		}
	})
}

func TestHandler_ChainedInterceptors(t *testing.T) {
	var callOrder []string

	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		callOrder = append(callOrder, "handler")
		return TestResponse{Message: "ok"}, nil
	}

	interceptor1 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "interceptor1-before")
		res, err := handler(ctx, req)
		callOrder = append(callOrder, "interceptor1-after")
		return res, err
	}

	interceptor2 := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "interceptor2-before")
		res, err := handler(ctx, req)
		callOrder = append(callOrder, "interceptor2-after")
		return res, err
	}

	handler := Exec(fn).WithUnaryInterceptor(interceptor1).WithUnaryInterceptor(interceptor2)

	// Add config interceptor as well
	configInterceptor := func(ctx Context, req any, handler HandlerFunc) (any, error) {
		callOrder = append(callOrder, "config-before")
		res, err := handler(ctx, req)
		callOrder = append(callOrder, "config-after")
		return res, err
	}

	NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{
			interceptors: []UnaryInterceptor{configInterceptor},
		})

	// Expected order: config -> interceptor1 -> interceptor2 -> handler -> interceptor2 -> interceptor1 -> config
	expectedOrder := []string{
		"config-before",
		"interceptor1-before",
		"interceptor2-before",
		"handler",
		"interceptor2-after",
		"interceptor1-after",
		"config-after",
	}

	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedOrder), len(callOrder), callOrder)
	}

	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("at position %d: expected %s, got %s", i, expected, callOrder[i])
		}
	}
}

func TestHandler_ServeHTTP_GET_PointerRequest(t *testing.T) {
	type GetRequest struct {
		Name string `schema:"name"`
	}

	fn := func(ctx context.Context, req *GetRequest) (TestResponse, error) {
		if req == nil {
			return TestResponse{}, errors.New("nil request")
		}
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Query(fn)

	w := NewTestRequest().
		GET("/test").
		WithQuery("name", "John").
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John"})
}

func TestHandler_WithSkipValidation(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		// Handler should receive the invalid request
		return TestResponse{Message: "ok"}, nil
	}

	handler := Exec(fn).WithSkipValidation()

	// Send invalid request (name too short, invalid email)
	w := NewTestRequest().
		POST("/test").
		WithJSON(map[string]string{"name": "Jo", "email": "invalid"}).
		ServeHandler(handler, testContextConfig{})

	// Should succeed because validation is skipped
	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ok"})
}

func TestHandler_ServeHTTP_GET_ArrayParams(t *testing.T) {
	type GetRequest struct {
		IDs []string `schema:"ids"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		message := fmt.Sprintf("ids: %v", strings.Join(req.IDs, ","))
		return TestResponse{Message: message}, nil
	}

	handler := Query(fn)

	req := httptest.NewRequest("GET", "/test?ids=1&ids=2&ids=3", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ids: 1,2,3"})
}

func TestHandler_ServeHTTP_GET_IntArrayParams(t *testing.T) {
	type GetRequest struct {
		Numbers []int `schema:"num"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		sum := 0
		for _, n := range req.Numbers {
			sum += n
		}
		return TestResponse{ID: sum}, nil
	}

	handler := Query(fn)

	req := httptest.NewRequest("GET", "/test?num=10&num=20&num=30", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{ID: 60})
}

func TestHandler_ServeHTTP_GET_SpecialCharacters(t *testing.T) {
	type GetRequest struct {
		Query string `schema:"q"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		return TestResponse{Message: req.Query}, nil
	}

	handler := Query(fn)

	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"spaces", "/test?q=hello+world", "hello world"},
		{"special chars", "/test?q=a%26b%3Dc", "a&b=c"},
		{"unicode", "/test?q=%E2%9C%93", "✓"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.query, nil)
			w := httptest.NewRecorder()
			ctx := newRpcTestContext(w, req, testContextConfig{})

			handler.serveHTTP(ctx)

			tygortest.AssertStatus(t, w, http.StatusOK)
			tygortest.AssertJSONResponse(t, w, TestResponse{Message: tt.expected})
		})
	}
}

func TestHandler_ServeHTTP_EmptyStructResponse(t *testing.T) {
	type EmptyResponse struct{}

	fn := func(ctx context.Context, req TestRequest) (EmptyResponse, error) {
		return EmptyResponse{}, nil
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	// Empty struct should encode as {"result": {}}
	tygortest.AssertJSONResponse(t, w, EmptyResponse{})
}

func TestHandler_ServeHTTP_NilPointerResponse(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (*TestResponse, error) {
		return nil, nil
	}

	handler := Exec(fn)

	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{})

	tygortest.AssertStatus(t, w, http.StatusOK)
	// Nil pointer should encode as {"result": null}
	var envelope struct {
		Result *TestResponse `json:"result"`
	}
	tygortest.DecodeJSON(t, w, &envelope)
	if envelope.Result != nil {
		t.Errorf("expected result to be nil, got %v", envelope.Result)
	}
}

func TestHandler_ServeHTTP_ResponseEncodingError(t *testing.T) {
	// This test simulates a response encoding error by returning a channel
	// which cannot be JSON encoded
	fn := func(ctx context.Context, req TestRequest) (chan int, error) {
		ch := make(chan int)
		return ch, nil
	}

	handler := Exec(fn)

	// Use a test logger to verify error logging
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{
			logger: logger,
		})

	// Verify error was logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to encode response") {
		t.Errorf("expected error log, got: %s", logOutput)
	}
}

func TestHandler_ServeHTTP_GET_StrictQueryParams_RejectsUnknown(t *testing.T) {
	type GetRequest struct {
		Name string `schema:"name"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Query(fn)
	handler.WithStrictQueryParams()

	// Send a request with an unknown parameter "unknown"
	w := NewTestRequest().
		GET("/test").
		WithQuery("name", "John").
		WithQuery("unknown", "value"). // This should cause an error with strict mode
		ServeHandler(handler, testContextConfig{})

	// With strict query params, unknown params should return an error
	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

func TestHandler_ServeHTTP_GET_StrictQueryParams_AllowsKnown(t *testing.T) {
	type GetRequest struct {
		Name  string `schema:"name"`
		Email string `schema:"email"`
	}

	fn := func(ctx context.Context, req GetRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Query(fn)
	handler.WithStrictQueryParams()

	// Send a request with only known parameters
	w := NewTestRequest().
		GET("/test").
		WithQuery("name", "John").
		WithQuery("email", "john@example.com").
		ServeHandler(handler, testContextConfig{})

	// Should succeed - all params are known
	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John"})
}

func TestHandler_ServeHTTP_MaxRequestBodySize_ExceedsLimit(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	handler := Exec(fn)

	// Create a large request body (100 bytes) with limit of 50 bytes
	largeReq := TestRequest{
		Name:  "ThisIsAReallyLongNameThatExceedsTheLimitWhenEncodedAsJSON",
		Email: "test@example.com",
	}

	w := NewTestRequest().
		POST("/test").
		WithJSON(largeReq).
		ServeHandler(handler, testContextConfig{
			maxRequestBodySize: 50, // 50 bytes limit
		})

	// Should return invalid_argument error when body exceeds limit
	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

func TestHandler_ServeHTTP_MaxRequestBodySize_WithinLimit(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "hello " + req.Name}, nil
	}

	handler := Exec(fn)

	// Small request that fits within limit
	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "John", Email: "john@example.com"}).
		ServeHandler(handler, testContextConfig{
			maxRequestBodySize: 1000, // 1KB limit
		})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello John"})
}

func TestHandler_ServeHTTP_MaxRequestBodySize_HandlerOverride(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	// Handler sets a very small limit (10 bytes)
	handler := Exec(fn).WithMaxRequestBodySize(10)

	// Even a small request should fail with the handler override
	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{Name: "Jo", Email: "a@b.c"}).
		ServeHandler(handler, testContextConfig{
			maxRequestBodySize: 10000, // App default is 10KB, but handler overrides to 10 bytes
		})

	// Should return invalid_argument error
	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

func TestHandler_ServeHTTP_MaxRequestBodySize_Unlimited(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	// Handler sets unlimited (0 means no limit)
	handler := Exec(fn).WithMaxRequestBodySize(0)

	// Large request should succeed with unlimited setting
	largeReq := TestRequest{
		Name:  "ThisIsAReallyLongNameThatWouldExceedMostLimitsButShouldSucceedWithUnlimitedSetting",
		Email: "verylongemailaddress@verylongdomainname.example.com",
	}

	w := NewTestRequest().
		POST("/test").
		WithJSON(largeReq).
		ServeHandler(handler, testContextConfig{
			maxRequestBodySize: 50, // App default is 50 bytes, but handler overrides to unlimited
		})

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, TestResponse{Message: "ok"})
}

func TestHandler_ServeHTTP_MaxRequestBodySize_DefaultLimit(t *testing.T) {
	fn := func(ctx context.Context, req TestRequest) (TestResponse, error) {
		return TestResponse{Message: "ok"}, nil
	}

	handler := Exec(fn)

	// Use a very small registry default
	w := NewTestRequest().
		POST("/test").
		WithJSON(TestRequest{
			Name:  "ThisIsAReallyLongNameThatExceedsTheDefaultLimit",
			Email: "test@example.com",
		}).
		ServeHandler(handler, testContextConfig{
			maxRequestBodySize: 20, // Very small default
		})

	// Should return invalid_argument error
	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	tygortest.AssertJSONError(t, w, string(CodeInvalidArgument))
}

// TestHandler_GET_TagBehavior tests which struct tags work for GET query param decoding.
// gorilla/schema uses "schema" tags by default, NOT "json" tags.
// IMPORTANT: gorilla/schema uses case-insensitive matching for field names.
func TestHandler_GET_TagBehavior(t *testing.T) {
	t.Run("schema_tags_work", func(t *testing.T) {
		type Req struct {
			Value string `schema:"myvalue"`
		}
		fn := func(ctx context.Context, req Req) (TestResponse, error) {
			return TestResponse{Message: req.Value}, nil
		}
		w := NewTestRequest().
			GET("/test").
			WithQuery("myvalue", "hello").
			ServeHandler(Query(fn), testContextConfig{})

		tygortest.AssertStatus(t, w, http.StatusOK)
		tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello"})
	})

	t.Run("json_tags_are_ignored", func(t *testing.T) {
		type Req struct {
			Value string `json:"myjsonvalue"`
		}
		fn := func(ctx context.Context, req Req) (TestResponse, error) {
			return TestResponse{Message: req.Value}, nil
		}
		// Using "myjsonvalue" which is the json tag - this should NOT work
		w := NewTestRequest().
			GET("/test").
			WithQuery("myjsonvalue", "hello").
			ServeHandler(Query(fn), testContextConfig{})

		tygortest.AssertStatus(t, w, http.StatusOK)
		// Value should be empty because json tag is ignored by gorilla/schema
		tygortest.AssertJSONResponse(t, w, TestResponse{Message: ""})
	})

	t.Run("field_name_works_as_default", func(t *testing.T) {
		type Req struct {
			Value string `json:"myjsonvalue"` // json tag ignored
		}
		fn := func(ctx context.Context, req Req) (TestResponse, error) {
			return TestResponse{Message: req.Value}, nil
		}
		// Using "Value" (field name) - this should work
		w := NewTestRequest().
			GET("/test").
			WithQuery("Value", "hello").
			ServeHandler(Query(fn), testContextConfig{})

		tygortest.AssertStatus(t, w, http.StatusOK)
		tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello"})
	})

	t.Run("field_name_is_case_insensitive", func(t *testing.T) {
		type Req struct {
			Value string // no tags at all
		}
		fn := func(ctx context.Context, req Req) (TestResponse, error) {
			return TestResponse{Message: req.Value}, nil
		}
		// Using lowercase "value" for field name "Value" - works due to case-insensitive matching
		w := NewTestRequest().
			GET("/test").
			WithQuery("value", "hello").
			ServeHandler(Query(fn), testContextConfig{})

		tygortest.AssertStatus(t, w, http.StatusOK)
		tygortest.AssertJSONResponse(t, w, TestResponse{Message: "hello"})
	})
}
