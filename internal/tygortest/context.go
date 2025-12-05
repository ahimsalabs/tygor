package tygortest

import (
	"context"
	"net/http"

	"tygor.dev/internal/tgrcontext"
)

// NewTestContext creates a context with service metadata for testing.
// This is useful when testing handlers directly without going through the App.
//
// Example:
//
//	req := httptest.NewRequest("POST", "/test", body)
//	w := httptest.NewRecorder()
//	ctx := tygortest.NewTestContext(req.Context(), w, req, "MyService", "MyMethod")
//	req = req.WithContext(ctx)
func NewTestContext(ctx context.Context, w http.ResponseWriter, r *http.Request, service, method string) context.Context {
	return tgrcontext.NewContext(ctx, w, r, service, method)
}

// ContextSetup returns a context setup function for use with NewRequest().
// This provides a convenient way to set up tygor service context when testing.
//
// Example usage:
//
//	req, w := tygortest.NewRequest(tygortest.ContextSetup()).
//	    POST("/test").
//	    WithJSON(&MyRequest{...}).
//	    Build()
//
//	handler.ServeHTTP(w, req, tygor.HandlerConfig{})
func ContextSetup() ContextSetupFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, service, method string) context.Context {
		return NewTestContext(ctx, w, r, service, method)
	}
}
