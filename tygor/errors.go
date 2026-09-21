package tygor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ErrorCode represents a machine-readable error code.
type ErrorCode string

const (
	CodeInvalidArgument   ErrorCode = "invalid_argument"
	CodeUnauthenticated   ErrorCode = "unauthenticated"
	CodePermissionDenied  ErrorCode = "permission_denied"
	CodeNotFound          ErrorCode = "not_found"
	CodeMethodNotAllowed  ErrorCode = "method_not_allowed"
	CodeConflict          ErrorCode = "conflict"
	CodeAlreadyExists     ErrorCode = "already_exists" // Alias for conflict, used when resource already exists
	CodeGone              ErrorCode = "gone"
	CodeResourceExhausted ErrorCode = "resource_exhausted"
	CodeCanceled          ErrorCode = "canceled"
	CodeInternal          ErrorCode = "internal"
	CodeNotImplemented    ErrorCode = "not_implemented"
	CodeUnavailable       ErrorCode = "unavailable"
	CodeDeadlineExceeded  ErrorCode = "deadline_exceeded"
)

// Error is the standard JSON error envelope.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewError creates a new service error.
func NewError(code ErrorCode, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Errorf creates a new service error with a formatted message.
func Errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WithDetail returns a new Error with the key-value pair added to details.
func (e *Error) WithDetail(key string, value any) *Error {
	details := make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		details[k] = v
	}
	details[key] = value
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Details: details,
	}
}

// WithDetails returns a new Error with the provided map merged into details.
// For multiple details, this is more efficient than chaining WithDetail calls.
func (e *Error) WithDetails(details map[string]any) *Error {
	if len(details) == 0 {
		return e
	}
	merged := make(map[string]any, len(e.Details)+len(details))
	for k, v := range e.Details {
		merged[k] = v
	}
	for k, v := range details {
		merged[k] = v
	}
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Details: merged,
	}
}

// ErrorTransformer is a function that maps an application error to a service error.
// If it returns nil, the default transformer logic should be applied.
type ErrorTransformer func(error) *Error

const internalErrorResponseJSON = "{\"error\":{\"code\":\"internal\",\"message\":\"internal server error\"}}\n"

type preparedErrorResponse struct {
	status       int
	code         ErrorCode
	message      string
	data         []byte
	usedFallback bool
	fallbackErr  error
}

func internalPreparedErrorResponse(cause error) preparedErrorResponse {
	return preparedErrorResponse{
		status:       http.StatusInternalServerError,
		code:         CodeInternal,
		message:      "internal server error",
		data:         []byte(internalErrorResponseJSON),
		usedFallback: true,
		fallbackErr:  cause,
	}
}

// prepareErrorResponse contains custom error policy and serialization in one
// panic boundary. Its fallback is constant and does not invoke either again.
func prepareErrorResponse(transformer ErrorTransformer, maskInternal bool, err error) (prepared preparedErrorResponse) {
	prepared = internalPreparedErrorResponse(err)
	defer func() {
		if recovered := recover(); recovered != nil {
			prepared = internalPreparedErrorResponse(fmt.Errorf("panic in error policy: %v", recovered))
		}
	}()

	svcErr := transformError(transformer, maskInternal, err)
	data, marshalErr := marshalErrorResponse(svcErr)
	if marshalErr != nil {
		prepared.fallbackErr = fmt.Errorf("marshal transformed error: %w", marshalErr)
		return prepared
	}
	return preparedErrorResponse{
		status:  svcErr.Code.HTTPStatus(),
		code:    svcErr.Code,
		message: svcErr.Message,
		data:    data,
	}
}

func prepareServiceErrorResponse(svcErr *Error) (prepared preparedErrorResponse) {
	prepared = internalPreparedErrorResponse(nil)
	defer func() {
		if recovered := recover(); recovered != nil {
			prepared = internalPreparedErrorResponse(fmt.Errorf("panic marshaling service error: %v", recovered))
		}
	}()

	data, marshalErr := marshalErrorResponse(svcErr)
	if marshalErr != nil {
		prepared.fallbackErr = fmt.Errorf("marshal service error: %w", marshalErr)
		return prepared
	}
	return preparedErrorResponse{
		status:  svcErr.Code.HTTPStatus(),
		code:    svcErr.Code,
		message: svcErr.Message,
		data:    data,
	}
}

func transformError(transformer ErrorTransformer, maskInternal bool, err error) *Error {
	var transformed *Error
	if transformer != nil {
		transformed = transformer(err)
	}
	if transformed == nil {
		transformed = DefaultErrorTransformer(err)
	}
	if transformed == nil {
		return NewError(CodeInternal, "internal server error")
	}

	copy := *transformed
	if maskInternal && copy.Code == CodeInternal {
		copy.Message = "internal server error"
	}
	return &copy
}

func logPanic(logger *slog.Logger, endpoint, location string, rec any) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("PANIC recovered",
		slog.String("endpoint", endpoint),
		slog.String("location", location),
		slog.Any("panic", rec),
		slog.String("stack", string(debug.Stack())))
}

// DefaultErrorTransformer maps standard Go errors to service errors.
func DefaultErrorTransformer(err error) *Error {
	if err == nil {
		return nil
	}

	var svcErr *Error
	if errors.As(err, &svcErr) {
		return svcErr
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(CodeDeadlineExceeded, "request timeout")
	}

	if errors.Is(err, context.Canceled) {
		return NewError(CodeCanceled, "context canceled")
	}

	if errors.Is(err, ErrWriteTimeout) {
		return NewError(CodeDeadlineExceeded, "write timeout")
	}

	if errors.Is(err, ErrStreamClosed) {
		return NewError(CodeCanceled, "stream closed")
	}

	var valErrs validator.ValidationErrors
	if errors.As(err, &valErrs) {
		details := make(map[string]any)
		messages := make([]string, 0, len(valErrs))
		for _, ve := range valErrs {
			msg := formatValidationError(ve)
			details[ve.Field()] = msg
			messages = append(messages, ve.Field()+": "+msg)
		}
		return &Error{
			Code:    CodeInvalidArgument,
			Message: strings.Join(messages, "; "),
			Details: details,
		}
	}

	// Handle multi-errors (errors.Join)
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		errs := u.Unwrap()
		if len(errs) > 0 {
			// Use the first error to determine code? Or just use Unknown/Internal?
			// For now, let's try to map the first error, but keep all messages.
			firstMapped := DefaultErrorTransformer(errs[0])
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			return &Error{
				Code:    firstMapped.Code,
				Message: strings.Join(msgs, "; "),
				Details: firstMapped.Details,
			}
		}
	}

	// Fallback to internal error.
	// In a real production system, we might want to log the original error
	// and return a generic "internal server error" message to avoid leaking details.
	// For this implementation, we'll just return the error message.
	return NewError(CodeInternal, err.Error())
}

// HTTPStatus maps an ErrorCode to an HTTP status code.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case CodeConflict, CodeAlreadyExists:
		return http.StatusConflict
	case CodeGone:
		return http.StatusGone
	case CodeResourceExhausted:
		return http.StatusTooManyRequests
	case CodeCanceled:
		return 499 // Client Closed Request (Nginx standard)
	case CodeInternal:
		return http.StatusInternalServerError
	case CodeNotImplemented:
		return http.StatusNotImplemented
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// formatValidationError converts a validator.FieldError to a human-readable message.
func formatValidationError(ve validator.FieldError) string {
	switch ve.Tag() {
	case "required":
		return "required"
	case "min":
		return fmt.Sprintf("must be at least %s characters", ve.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", ve.Param())
	case "len":
		return fmt.Sprintf("must be exactly %s characters", ve.Param())
	case "eq":
		return fmt.Sprintf("must equal %s", ve.Param())
	case "ne":
		return fmt.Sprintf("must not equal %s", ve.Param())
	case "gt":
		return fmt.Sprintf("must be greater than %s", ve.Param())
	case "gte":
		return fmt.Sprintf("must be at least %s", ve.Param())
	case "lt":
		return fmt.Sprintf("must be less than %s", ve.Param())
	case "lte":
		return fmt.Sprintf("must be at most %s", ve.Param())
	case "email":
		return "must be a valid email address"
	case "url":
		return "must be a valid URL"
	case "uuid":
		return "must be a valid UUID"
	case "oneof":
		return fmt.Sprintf("must be one of: %s", ve.Param())
	default:
		if ve.Param() != "" {
			return fmt.Sprintf("failed %s=%s validation", ve.Tag(), ve.Param())
		}
		return fmt.Sprintf("failed %s validation", ve.Tag())
	}
}

func writeError(recovery *panicRecoveryState, w http.ResponseWriter, svcErr *Error, logger *slog.Logger) {
	writePreparedErrorOwned(recovery, w, prepareServiceErrorResponse(svcErr), logger)
}

func writePreparedErrorOwned(recovery *panicRecoveryState, w http.ResponseWriter, prepared preparedErrorResponse, logger *slog.Logger) {
	recovery.own(func() {
		writePreparedError(w, prepared, logger)
	})
}

func writePreparedError(w http.ResponseWriter, prepared preparedErrorResponse, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if prepared.usedFallback {
		logger.Error("error policy failed; using internal fallback", slog.Any("error", prepared.fallbackErr))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(prepared.status)
	if n, writeErr := w.Write(prepared.data); writeErr != nil || n != len(prepared.data) {
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		logger.Error("failed to encode error response",
			slog.String("code", string(prepared.code)),
			slog.String("message", prepared.message),
			slog.Any("error", writeErr))
	}
}
