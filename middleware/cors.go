package middleware

import (
	"net/http"

	"github.com/ahimsalabs/cors"
)

// CORSAllowAll returns a permissive CORS middleware for development.
// For production, use github.com/ahimsalabs/cors directly.
func CORSAllowAll() func(http.Handler) http.Handler {
	return cors.AnyOrigin().Handler()
}
