package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowAll(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := CORSAllowAll()(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()

	corsHandler.ServeHTTP(w, req)

	// ahimsalabs/cors echoes the requesting origin
	if w.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin http://example.com, got %s", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
