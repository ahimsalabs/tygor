package tygortest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tygor.dev/internal/tygortest"
	"tygor.dev/tygor"
)

// Example types for testing
type ExampleRequest struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
}

type ExampleResponse struct {
	Message string `json:"message"`
	ID      int    `json:"id"`
}

type GetParams struct {
	Query string `schema:"query"`
	Limit int    `schema:"limit"`
}

// Example handler function
func exampleHandler(ctx context.Context, req *ExampleRequest) (*ExampleResponse, error) {
	return &ExampleResponse{
		Message: "Hello, " + req.Name,
		ID:      123,
	}, nil
}

// TestWithApp demonstrates testing handlers through the App
func TestWithApp(t *testing.T) {
	app := tygor.NewApp()
	app.Service("Example").Register("Create", tygor.Exec(exampleHandler))

	req := httptest.NewRequest("POST", "/Example/Create", strings.NewReader(`{"name":"Alice","email":"alice@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, &ExampleResponse{
		Message: "Hello, Alice",
		ID:      123,
	})
}

// TestWithApp_Validation demonstrates validation error handling
func TestWithApp_Validation(t *testing.T) {
	app := tygor.NewApp()
	app.Service("Example").Register("Create", tygor.Exec(exampleHandler))

	req := httptest.NewRequest("POST", "/Example/Create", strings.NewReader(`{"name":"Alice","email":"invalid-email"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusBadRequest)
	errResp := tygortest.AssertJSONError(t, w, string(tygor.CodeInvalidArgument))

	wantMsg := "Email: must be a valid email address"
	if errResp.Message != wantMsg {
		t.Errorf("expected message %q, got %q", wantMsg, errResp.Message)
	}
}

// TestWithApp_GET demonstrates GET request with query parameters
func TestWithApp_GET(t *testing.T) {
	getHandler := func(ctx context.Context, req *GetParams) (*ExampleResponse, error) {
		return &ExampleResponse{
			Message: "Search: " + req.Query,
			ID:      req.Limit,
		}, nil
	}

	app := tygor.NewApp()
	app.Service("Search").Register("Query", tygor.Query(getHandler))

	req := httptest.NewRequest("GET", "/Search/Query?query=golang&limit=10", nil)
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusOK)
	tygortest.AssertJSONResponse(t, w, &ExampleResponse{
		Message: "Search: golang",
		ID:      10,
	})
}

// TestWithApp_CustomHeader demonstrates custom headers
func TestWithApp_CustomHeader(t *testing.T) {
	authHandler := func(ctx context.Context, req *ExampleRequest) (*ExampleResponse, error) {
		tc, ok := tygor.FromContext(ctx)
		if !ok {
			return nil, tygor.NewError(tygor.CodeInternal, "missing tygor context")
		}
		token := tc.HTTPRequest().Header.Get("X-API-Key")
		if token != "secret" {
			return nil, tygor.NewError(tygor.CodeUnauthenticated, "invalid api key")
		}
		return &ExampleResponse{Message: "authenticated"}, nil
	}

	app := tygor.NewApp()
	app.Service("Auth").Register("Check", tygor.Exec(authHandler))

	req := httptest.NewRequest("POST", "/Auth/Check", strings.NewReader(`{"name":"Alice","email":"alice@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	tygortest.AssertStatus(t, w, http.StatusOK)
}

// TestWithApp_CacheControl demonstrates cache header assertions
func TestWithApp_CacheControl(t *testing.T) {
	getHandler := func(ctx context.Context, req *GetParams) (*ExampleResponse, error) {
		return &ExampleResponse{Message: "cached response"}, nil
	}

	app := tygor.NewApp()
	app.Service("Cache").Register("Get", tygor.Query(getHandler).CacheControl(tygor.CacheConfig{MaxAge: 60 * time.Second}))

	req := httptest.NewRequest("GET", "/Cache/Get?query=test&limit=10", nil)
	w := httptest.NewRecorder()

	app.Handler().ServeHTTP(w, req)

	tygortest.AssertHeader(t, w, "Cache-Control", "private, max-age=60")
}
