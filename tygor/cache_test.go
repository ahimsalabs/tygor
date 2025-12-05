package tygor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type CacheTestRequest struct {
	Query string `schema:"query"`
}

type CacheTestResponse struct {
	Message string `json:"message"`
}

// TestGetHandler_CacheControl_Simple verifies simple max-age cache directive (RFC 9111 Section 5.2.2.1)
// RFC 9111: max-age specifies the maximum time a resource is considered fresh
func TestGetHandler_CacheControl_Simple(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{MaxAge: 5 * time.Minute})

	cacheHeader := handler.getCacheControlHeader()
	expected := "private, max-age=300"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}

	// Verify header is set in HTTP response
	req := httptest.NewRequest("GET", "/test?query=hello", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	if w.Header().Get("Cache-Control") != expected {
		t.Errorf("expected Cache-Control header %q, got %q", expected, w.Header().Get("Cache-Control"))
	}
}

// TestGetHandler_CacheControl_Public verifies public cache directive (RFC 9111 Section 5.2.2.9)
// RFC 9111: public indicates the response may be cached by any cache, including CDNs
func TestGetHandler_CacheControl_Public(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge: 5 * time.Minute,
		Public: true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=300"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_Private verifies private cache directive (RFC 9111 Section 5.2.2.7)
// RFC 9111: private indicates the response is specific to a single user (default behavior)
func TestGetHandler_CacheControl_Private(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge: 5 * time.Minute,
		Public: false, // Explicit private
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "private, max-age=300"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_SMaxAge verifies s-maxage for shared caches (RFC 9111 Section 5.2.2.10)
// RFC 9111: s-maxage overrides max-age for shared caches (CDNs), ignored by private caches
func TestGetHandler_CacheControl_SMaxAge(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:  1 * time.Minute,
		SMaxAge: 10 * time.Minute,
		Public:  true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=60, s-maxage=600"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_StaleWhileRevalidate verifies stale-while-revalidate directive (RFC 5861)
// RFC 5861: Allows serving stale content while revalidating in the background
// Example: MaxAge=300s, StaleWhileRevalidate=60s means serve fresh for 5min,
// then serve stale for up to 1min more while fetching fresh data in background
func TestGetHandler_CacheControl_StaleWhileRevalidate(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:               5 * time.Minute,
		StaleWhileRevalidate: 1 * time.Minute,
		Public:               true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=300, stale-while-revalidate=60"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_StaleIfError verifies stale-if-error directive (RFC 5861)
// RFC 5861: Allows serving stale content if the origin server is unavailable (5xx errors)
// Example: StaleIfError=86400 allows serving day-old stale content if origin returns 5xx
func TestGetHandler_CacheControl_StaleIfError(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:       5 * time.Minute,
		StaleIfError: 24 * time.Hour,
		Public:       true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=300, stale-if-error=86400"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_MustRevalidate verifies must-revalidate directive (RFC 9111 Section 5.2.2.2)
// RFC 9111: Requires caches to revalidate stale responses before serving them
// Prevents serving stale content even if client would accept it
func TestGetHandler_CacheControl_MustRevalidate(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:         5 * time.Minute,
		MustRevalidate: true,
		Public:         true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=300, must-revalidate"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_Immutable verifies immutable directive (RFC 8246)
// RFC 8246: Indicates the response will never change during its freshness lifetime
// Browsers won't send conditional requests for immutable resources within MaxAge period
// Useful for content-addressed assets like "bundle.abc123.js"
func TestGetHandler_CacheControl_Immutable(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:    365 * 24 * time.Hour, // 1 year
		Immutable: true,
		Public:    true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=31536000, immutable"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_AllDirectives verifies all directives can be combined
// Tests that multiple RFC 9111 and RFC 5861 directives work together correctly
func TestGetHandler_CacheControl_AllDirectives(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:               5 * time.Minute,
		SMaxAge:              10 * time.Minute,
		StaleWhileRevalidate: 1 * time.Minute,
		StaleIfError:         1 * time.Hour,
		Public:               true,
		MustRevalidate:       true,
		Immutable:            true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=300, s-maxage=600, stale-while-revalidate=60, stale-if-error=3600, must-revalidate, immutable"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_NoCache verifies no cache when config is nil
func TestGetHandler_CacheControl_NoCache(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn) // No Cache() or CacheControl() called

	cacheHeader := handler.getCacheControlHeader()
	if cacheHeader != "" {
		t.Errorf("expected no Cache-Control header, got %q", cacheHeader)
	}

	// Verify no header in HTTP response
	req := httptest.NewRequest("GET", "/test?query=hello", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	if w.Header().Get("Cache-Control") != "" {
		t.Errorf("expected no Cache-Control header, got %q", w.Header().Get("Cache-Control"))
	}
}

// TestHandler_POST_NoCache verifies POST handlers don't set cache headers
func TestHandler_POST_NoCache(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "created"}, nil
	}

	handler := Exec(fn) // POST handler

	// Verify no Cache-Control header in HTTP response
	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	if w.Header().Get("Cache-Control") != "" {
		t.Errorf("expected no Cache-Control header for POST, got %q", w.Header().Get("Cache-Control"))
	}
}

// TestGetHandler_CacheControl_CDNPattern verifies common CDN caching pattern
// Pattern: Short browser cache, long CDN cache with stale-while-revalidate
func TestGetHandler_CacheControl_CDNPattern(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:               1 * time.Minute, // Browser: 1 minute
		SMaxAge:              1 * time.Hour,   // CDN: 1 hour
		StaleWhileRevalidate: 5 * time.Minute, // Background revalidation window
		Public:               true,
	})

	cacheHeader := handler.getCacheControlHeader()
	expected := "public, max-age=60, s-maxage=3600, stale-while-revalidate=300"
	if cacheHeader != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, cacheHeader)
	}
}

// TestGetHandler_CacheControl_ReflectsCacheTTL verifies cacheConfig reflects MaxAge
func TestGetHandler_CacheControl_ReflectsCacheTTL(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "test"}, nil
	}

	ttl := 10 * time.Minute
	handler := Query(fn).CacheControl(CacheConfig{MaxAge: ttl})

	if handler.cacheConfig.MaxAge != ttl {
		t.Errorf("expected cacheConfig.MaxAge = %v, got %v", ttl, handler.cacheConfig.MaxAge)
	}
}

// TestGetHandler_CacheControl_EndToEnd verifies cache headers in complete HTTP response
func TestGetHandler_CacheControl_EndToEnd(t *testing.T) {
	fn := func(ctx context.Context, req CacheTestRequest) (CacheTestResponse, error) {
		return CacheTestResponse{Message: "cached response"}, nil
	}

	handler := Query(fn).CacheControl(CacheConfig{
		MaxAge:               5 * time.Minute,
		StaleWhileRevalidate: 1 * time.Minute,
		Public:               true,
	})

	req := httptest.NewRequest("GET", "/test?query=hello", nil)
	w := httptest.NewRecorder()
	ctx := newRpcTestContext(w, req, testContextConfig{})

	handler.serveHTTP(ctx)

	// Verify status
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify Cache-Control header
	expected := "public, max-age=300, stale-while-revalidate=60"
	if w.Header().Get("Cache-Control") != expected {
		t.Errorf("expected Cache-Control: %q, got %q", expected, w.Header().Get("Cache-Control"))
	}

	// Verify Content-Type
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type: application/json, got %q", w.Header().Get("Content-Type"))
	}
}
