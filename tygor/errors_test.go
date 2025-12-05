package tygor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"tygor.dev/internal/tygortest"
)

func TestNewError(t *testing.T) {
	err := NewError(CodeNotFound, "resource not found")
	if err.Code != CodeNotFound {
		t.Errorf("expected code %s, got %s", CodeNotFound, err.Code)
	}
	if err.Message != "resource not found" {
		t.Errorf("expected message 'resource not found', got %s", err.Message)
	}
}

func TestErrorf(t *testing.T) {
	err := Errorf(CodeInvalidArgument, "invalid field: %s", "email")
	if err.Code != CodeInvalidArgument {
		t.Errorf("expected code %s, got %s", CodeInvalidArgument, err.Code)
	}
	if err.Message != "invalid field: email" {
		t.Errorf("expected formatted message, got %s", err.Message)
	}
}

func TestErrorError(t *testing.T) {
	err := NewError(CodeInternal, "something went wrong")
	expected := "internal: something went wrong"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestErrorWithDetail(t *testing.T) {
	err := NewError(CodeNotFound, "resource not found").
		WithDetail("resource_id", 123).
		WithDetail("resource_type", "user")

	if err.Code != CodeNotFound {
		t.Errorf("expected code %s, got %s", CodeNotFound, err.Code)
	}
	if err.Details == nil {
		t.Fatal("expected details to be set")
	}
	if err.Details["resource_id"] != 123 {
		t.Errorf("expected resource_id=123, got %v", err.Details["resource_id"])
	}
	if err.Details["resource_type"] != "user" {
		t.Errorf("expected resource_type=user, got %v", err.Details["resource_type"])
	}
}

func TestErrorWithDetails(t *testing.T) {
	details := map[string]any{"field": "email", "reason": "invalid format"}
	err := NewError(CodeInvalidArgument, "validation failed").WithDetails(details)

	if err.Code != CodeInvalidArgument {
		t.Errorf("expected code %s, got %s", CodeInvalidArgument, err.Code)
	}
	if err.Details == nil {
		t.Fatal("expected details to be set")
	}
	if err.Details["field"] != "email" {
		t.Errorf("expected field=email, got %v", err.Details["field"])
	}
	if err.Details["reason"] != "invalid format" {
		t.Errorf("expected reason='invalid format', got %v", err.Details["reason"])
	}
}

func TestErrorWithDetails_Nil(t *testing.T) {
	err := NewError(CodeInternal, "error").WithDetails(nil)
	if err.Details != nil {
		t.Error("expected nil details when passing nil")
	}
}

func TestErrorWithDetails_Merge(t *testing.T) {
	err := NewError(CodeInvalidArgument, "error").
		WithDetail("existing", "value").
		WithDetails(map[string]any{"new": "value"})

	if err.Details["existing"] != "value" {
		t.Error("expected existing detail to be preserved")
	}
	if err.Details["new"] != "value" {
		t.Error("expected new detail to be added")
	}
}

func TestErrorWithDetail_Immutable(t *testing.T) {
	err1 := NewError(CodeNotFound, "not found").WithDetail("a", 1)
	err2 := err1.WithDetail("b", 2)

	// err1 and err2 should be different pointers
	if err1 == err2 {
		t.Error("expected WithDetail to return a new Error")
	}

	// err1 should not have the "b" detail
	if _, ok := err1.Details["b"]; ok {
		t.Error("err1 should not be mutated by err2.WithDetail")
	}

	// err2 should have both details
	if err2.Details["a"] != 1 || err2.Details["b"] != 2 {
		t.Error("err2 should have both details")
	}
}

func TestErrorWithDetails_Immutable(t *testing.T) {
	err1 := NewError(CodeNotFound, "not found").WithDetails(map[string]any{"a": 1})
	err2 := err1.WithDetails(map[string]any{"b": 2})

	// err1 and err2 should be different pointers
	if err1 == err2 {
		t.Error("expected WithDetails to return a new Error")
	}

	// err1 should not have the "b" detail
	if _, ok := err1.Details["b"]; ok {
		t.Error("err1 should not be mutated by err2.WithDetails")
	}
}

func TestDefaultErrorTransformer(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		wantCode ErrorCode
		wantMsg  string
	}{
		{
			name:     "nil error",
			input:    nil,
			wantCode: "",
			wantMsg:  "",
		},
		{
			name:     "RPC error passthrough",
			input:    NewError(CodeNotFound, "not found"),
			wantCode: CodeNotFound,
			wantMsg:  "not found",
		},
		{
			name:     "context deadline exceeded",
			input:    context.DeadlineExceeded,
			wantCode: CodeDeadlineExceeded,
			wantMsg:  "request timeout",
		},
		{
			name:     "context canceled",
			input:    context.Canceled,
			wantCode: CodeCanceled,
			wantMsg:  "context canceled",
		},
		{
			name:     "generic error",
			input:    errors.New("something failed"),
			wantCode: CodeInternal,
			wantMsg:  "something failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultErrorTransformer(tt.input)
			if tt.input == nil {
				if result != nil {
					t.Errorf("expected nil for nil input, got %v", result)
				}
				return
			}
			if result.Code != tt.wantCode {
				t.Errorf("expected code %s, got %s", tt.wantCode, result.Code)
			}
			if result.Message != tt.wantMsg {
				t.Errorf("expected message %q, got %q", tt.wantMsg, result.Message)
			}
		})
	}
}

func TestDefaultErrorTransformer_ValidationErrors(t *testing.T) {
	type TestStruct struct {
		Email string `validate:"required,email"`
		Age   int    `validate:"gte=0,lte=120"`
	}

	validate := validator.New()
	s := TestStruct{Email: "invalid", Age: -1}
	err := validate.Struct(s)

	result := DefaultErrorTransformer(err)
	if result.Code != CodeInvalidArgument {
		t.Errorf("expected code %s, got %s", CodeInvalidArgument, result.Code)
	}
	if result.Details == nil {
		t.Fatal("expected details to be non-nil")
	}
	if result.Details["Email"] != "must be a valid email address" {
		t.Errorf("expected Email error, got %v", result.Details["Email"])
	}
	if result.Details["Age"] != "must be at least 0" {
		t.Errorf("expected Age error, got %v", result.Details["Age"])
	}
	// Message combines field errors
	wantMsg := "Email: must be a valid email address; Age: must be at least 0"
	if result.Message != wantMsg {
		t.Errorf("expected message %q, got %q", wantMsg, result.Message)
	}
}

func TestDefaultErrorTransformer_MultiError(t *testing.T) {
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	multiErr := errors.Join(err1, err2)

	result := DefaultErrorTransformer(multiErr)
	if result.Code != CodeInternal {
		t.Errorf("expected code from first error %s, got %s", CodeInternal, result.Code)
	}
	// Message should contain both errors
	if result.Message != "error 1; error 2" {
		t.Errorf("expected combined message, got %q", result.Message)
	}
}

func TestErrorCode_HTTPStatus(t *testing.T) {
	tests := []struct {
		code       ErrorCode
		wantStatus int
	}{
		{CodeInvalidArgument, http.StatusBadRequest},
		{CodeUnauthenticated, http.StatusUnauthorized},
		{CodePermissionDenied, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeMethodNotAllowed, http.StatusMethodNotAllowed},
		{CodeConflict, http.StatusConflict},
		{CodeAlreadyExists, http.StatusConflict},
		{CodeGone, http.StatusGone},
		{CodeResourceExhausted, http.StatusTooManyRequests},
		{CodeCanceled, 499},
		{CodeInternal, http.StatusInternalServerError},
		{CodeNotImplemented, http.StatusNotImplemented},
		{CodeUnavailable, http.StatusServiceUnavailable},
		{CodeDeadlineExceeded, http.StatusGatewayTimeout},
		{ErrorCode("unknown"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			status := tt.code.HTTPStatus()
			if status != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, status)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	svcErr := NewError(CodeNotFound, "resource not found")
	w := httptest.NewRecorder()

	writeError(w, svcErr, nil)

	tygortest.AssertStatus(t, w, http.StatusNotFound)
	tygortest.AssertHeader(t, w, "Content-Type", "application/json")
	tygortest.AssertJSONError(t, w, string(CodeNotFound))
}

type failingWriter struct {
	headerWritten bool
}

func (fw *failingWriter) Header() http.Header {
	return http.Header{}
}

func (fw *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (fw *failingWriter) WriteHeader(statusCode int) {
	fw.headerWritten = true
}

func TestWriteError_EncodingFailure(t *testing.T) {
	svcErr := NewError(CodeInternal, "test error")
	w := &failingWriter{}

	// Use a test logger to verify error logging
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	writeError(w, svcErr, logger)

	// Verify error was logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed to encode error response") {
		t.Errorf("expected error log, got: %s", logOutput)
	}

	if !w.headerWritten {
		t.Error("expected WriteHeader to be called")
	}
}
