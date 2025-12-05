package middleware

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"tygor.dev/tygor"
)

// loggingTestContext implements tygor.Context for testing.
type loggingTestContext struct {
	context.Context
	service string
	name    string
}

func (c *loggingTestContext) Service() string                 { return c.service }
func (c *loggingTestContext) EndpointID() string              { return c.service + "." + c.name }
func (c *loggingTestContext) HTTPRequest() *http.Request      { return nil }
func (c *loggingTestContext) HTTPWriter() http.ResponseWriter { return nil }

func newLoggingTestContext(parent context.Context, service, method string) tygor.Context {
	return &loggingTestContext{
		Context: parent,
		service: service,
		name:    method,
	}
}

func TestLoggingInterceptor_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	result, err := interceptor(ctx, "request", handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result != "response" {
		t.Errorf("expected response, got %v", result)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "request started") {
		t.Error("expected 'request started' in log output")
	}
	if !strings.Contains(logOutput, "request completed") {
		t.Error("expected 'request completed' in log output")
	}
	if !strings.Contains(logOutput, "TestService.TestMethod") {
		t.Error("expected endpoint ID in log output")
	}
}

func TestLoggingInterceptor_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	testErr := errors.New("test error")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, testErr
	}

	result, err := interceptor(ctx, "request", handler)

	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "request started") {
		t.Error("expected 'request started' in log output")
	}
	if !strings.Contains(logOutput, "request failed") {
		t.Error("expected 'request failed' in log output")
	}
	if !strings.Contains(logOutput, "test error") {
		t.Error("expected error message in log output")
	}
}

func TestLoggingInterceptor_NilLogger(t *testing.T) {
	// Should not panic with nil logger, should use default
	interceptor := LoggingInterceptor(nil)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	result, err := interceptor(ctx, "request", handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result != "response" {
		t.Errorf("expected response, got %v", result)
	}
}

func TestLoggingInterceptor_LogsDuration(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	_, err := interceptor(ctx, "request", handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "duration") {
		t.Error("expected 'duration' in log output")
	}
}

func TestLoggingInterceptor_PropagatesContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	type ctxKey string
	key := ctxKey("test-key")
	baseCtx := context.WithValue(context.Background(), key, "test-value")
	ctx := newLoggingTestContext(baseCtx, "TestService", "TestMethod")

	handler := func(ctx context.Context, req any) (any, error) {
		val := ctx.Value(key)
		if val != "test-value" {
			t.Error("expected context value to be propagated")
		}
		return "response", nil
	}

	_, err := interceptor(ctx, "request", handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoggingInterceptor_EndpointIDInLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	tests := []struct {
		service    string
		name       string
		endpointID string
	}{
		{"Users", "Create", "Users.Create"},
		{"Posts", "List", "Posts.List"},
		{"Comments", "Delete", "Comments.Delete"},
	}

	for _, tt := range tests {
		t.Run(tt.endpointID, func(t *testing.T) {
			buf.Reset()

			ctx := newLoggingTestContext(context.Background(), tt.service, tt.name)

			handler := func(ctx context.Context, req any) (any, error) {
				return nil, nil
			}

			_, _ = interceptor(ctx, nil, handler)

			logOutput := buf.String()
			if !strings.Contains(logOutput, tt.endpointID) {
				t.Errorf("expected endpoint ID %s in log output", tt.endpointID)
			}
		})
	}
}

func TestLoggingInterceptor_ErrorDetails(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	customErr := tygor.NewError(tygor.CodeNotFound, "resource not found")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, customErr
	}

	_, err := interceptor(ctx, "request", handler)

	if err != customErr {
		t.Errorf("expected custom error, got %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "request failed") {
		t.Error("expected 'request failed' in log output")
	}
	// Error should be logged with details
	if !strings.Contains(logOutput, "not_found") || !strings.Contains(logOutput, "resource not found") {
		t.Error("expected error details in log output")
	}
}

func TestLoggingInterceptor_PassthroughRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	interceptor := LoggingInterceptor(logger)

	ctx := newLoggingTestContext(context.Background(), "TestService", "TestMethod")

	type testReq struct {
		Key string
	}
	expectedReq := testReq{Key: "value"}
	handler := func(ctx context.Context, req any) (any, error) {
		if req != expectedReq {
			t.Error("expected request to be passed through")
		}
		return "response", nil
	}

	_, err := interceptor(ctx, expectedReq, handler)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
