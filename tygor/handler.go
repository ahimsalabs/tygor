package tygor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
	"tygor.dev/internal"
)

// Ensure Context interface satisfies context.Context at compile time.
var _ context.Context = (Context)(nil)

var (
	validate            = validator.New()
	schemaDecoder       = schema.NewDecoder() // lenient: ignores unknown keys
	strictSchemaDecoder = schema.NewDecoder() // strict: errors on unknown keys
)

func init() {
	schemaDecoder.IgnoreUnknownKeys(true)
	strictSchemaDecoder.IgnoreUnknownKeys(false)
}

// Endpoint is the interface for handlers that can be registered with [Service.Register].
//
// Implementations:
//   - [*ExecHandler] - for POST requests (created with [Exec])
//   - [*QueryHandler] - for GET requests (created with [Query])
//   - [*StreamHandler] - for SSE streaming (created with [Stream])
//   - [*AtomHandler] - for synchronized state (created with [Atom.Handler])
type Endpoint interface {
	// Metadata returns route metadata for code generation.
	// The return type is internal; this method is for use by tygorgen only.
	Metadata() *internal.MethodMetadata
}

// endpointHandler is the internal interface used by the framework to serve requests.
type endpointHandler interface {
	Endpoint
	serveHTTP(ctx *rpcContext)
	metadata() *internal.MethodMetadata
}

// handlerBase contains common configuration shared by ExecHandler and QueryHandler.
type handlerBase[Req any, Res any] struct {
	fn             func(context.Context, Req) (Res, error)
	interceptors   []UnaryInterceptor
	skipValidation bool
}

// ExecHandler implements Endpoint for POST requests (state-changing operations).
//
// Request Type Guidelines:
//   - Use struct or pointer types
//   - Request is decoded from JSON body
//
// Example:
//
//	func CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) { ... }
//	Exec(CreateUser)
//
//	func UpdatePost(ctx context.Context, req *UpdatePostRequest) (*Post, error) { ... }
//	Exec(UpdatePost).WithUnaryInterceptor(requireAuth)
type ExecHandler[Req any, Res any] struct {
	handlerBase[Req, Res]
	maxRequestBodySize *uint64 // nil means use registry default
}

// Exec creates a new POST handler from a generic function for non-streaming API calls.
//
// The handler function signature is func(context.Context, Req) (Res, error).
// Requests are decoded from JSON body.
//
// For GET requests (cacheable reads), use Query instead.
func Exec[Req any, Res any](fn func(context.Context, Req) (Res, error)) *ExecHandler[Req, Res] {
	return &ExecHandler[Req, Res]{
		handlerBase: handlerBase[Req, Res]{
			fn: fn,
		},
	}
}

// QueryHandler implements Endpoint for GET requests (cacheable read operations).
//
// Request Type Guidelines:
//   - Use struct types for simple cases, pointer types when you need optional fields
//   - Request parameters are decoded from URL query string
//
// Struct vs Pointer Types:
//   - Struct types (e.g., ListParams): Query parameters are decoded directly into the struct
//   - Pointer types (e.g., *ListParams): A new instance is created and query parameters are decoded into it
//
// Example:
//
//	func ListPosts(ctx context.Context, req ListPostsParams) ([]*Post, error) { ... }
//	Query(ListPosts).CacheControl(tygor.CacheConfig{
//	    MaxAge: 5 * time.Minute,
//	    Public: true,
//	})
type QueryHandler[Req any, Res any] struct {
	handlerBase[Req, Res]
	cacheConfig       *CacheConfig
	strictQueryParams bool
}

// CacheConfig defines HTTP cache directives for GET requests.
// See RFC 9111 (HTTP Caching) for detailed semantics.
//
// Common patterns:
//   - Simple caching: CacheConfig{MaxAge: 5*time.Minute}
//   - Public CDN caching: CacheConfig{MaxAge: 5*time.Minute, Public: true}
//   - Stale-while-revalidate: CacheConfig{MaxAge: 1*time.Minute, StaleWhileRevalidate: 5*time.Minute}
//   - Immutable assets: CacheConfig{MaxAge: 365*24*time.Hour, Immutable: true}
type CacheConfig struct {
	// MaxAge specifies the maximum time a resource is considered fresh (RFC 9111 Section 5.2.2.1).
	// After this time, caches must revalidate before serving the cached response.
	MaxAge time.Duration

	// SMaxAge is like MaxAge but only applies to shared caches like CDNs (RFC 9111 Section 5.2.2.10).
	// Overrides MaxAge for shared caches. Private caches ignore this directive.
	SMaxAge time.Duration

	// StaleWhileRevalidate allows serving stale content while revalidating in the background (RFC 5861).
	// Example: MaxAge=60s, StaleWhileRevalidate=300s means serve from cache for 60s,
	// then serve stale content for up to 300s more while fetching fresh data in background.
	StaleWhileRevalidate time.Duration

	// StaleIfError allows serving stale content if the origin server is unavailable (RFC 5861).
	// Example: StaleIfError=86400 allows serving day-old stale content if origin returns 5xx errors.
	StaleIfError time.Duration

	// Public indicates the response may be cached by any cache, including CDNs (RFC 9111 Section 5.2.2.9).
	// Default is false (private), meaning only the user's browser cache may store it.
	// Set to true for responses that are safe to cache publicly.
	Public bool

	// MustRevalidate requires caches to revalidate stale responses with the origin before serving (RFC 9111 Section 5.2.2.2).
	// Prevents serving stale content. Useful when stale data could cause problems.
	MustRevalidate bool

	// Immutable indicates the response will never change during its freshness lifetime (RFC 8246).
	// Browsers won't send conditional requests for immutable resources within MaxAge period.
	// Useful for content-addressed assets like "bundle.abc123.js".
	Immutable bool
}

// Query creates a new GET handler from a generic function for cacheable read operations.
//
// The handler function signature is func(context.Context, Req) (Res, error).
// Requests are decoded from URL query parameters.
//
// Use CacheControl() to configure HTTP caching behavior.
func Query[Req any, Res any](fn func(context.Context, Req) (Res, error)) *QueryHandler[Req, Res] {
	return &QueryHandler[Req, Res]{
		handlerBase: handlerBase[Req, Res]{
			fn: fn,
		},
	}
}

// CacheControl sets detailed HTTP cache directives for the handler.
// See CacheConfig documentation and RFC 9111 for directive semantics.
//
// Example:
//
//	Query(ListPosts).CacheControl(tygor.CacheConfig{
//	    MaxAge:               5 * time.Minute,
//	    StaleWhileRevalidate: 1 * time.Minute,
//	    Public:               true,
//	})
//	// Sets: Cache-Control: public, max-age=300, stale-while-revalidate=60
func (h *QueryHandler[Req, Res]) CacheControl(cfg CacheConfig) *QueryHandler[Req, Res] {
	h.cacheConfig = &cfg
	return h
}

// WithStrictQueryParams enables strict query parameter validation for GET requests.
// By default, unknown query parameters are ignored (lenient mode).
// When enabled, requests with unknown query parameters will return an error.
// This helps catch typos and enforces exact parameter expectations.
func (h *QueryHandler[Req, Res]) WithStrictQueryParams() *QueryHandler[Req, Res] {
	h.strictQueryParams = true
	return h
}

// WithMaxRequestBodySize sets the maximum request body size for this handler.
// This overrides the registry-level default. A value of 0 means no limit.
func (h *ExecHandler[Req, Res]) WithMaxRequestBodySize(size uint64) *ExecHandler[Req, Res] {
	h.maxRequestBodySize = &size
	return h
}

// WithUnaryInterceptor adds an interceptor to this handler.
// Handler interceptors execute after global and service interceptors.
// See App.WithUnaryInterceptor for the complete execution order.
func (h *ExecHandler[Req, Res]) WithUnaryInterceptor(i UnaryInterceptor) *ExecHandler[Req, Res] {
	h.interceptors = append(h.interceptors, i)
	return h
}

// WithUnaryInterceptor adds an interceptor to this handler.
// Handler interceptors execute after global and service interceptors.
// See App.WithUnaryInterceptor for the complete execution order.
func (h *QueryHandler[Req, Res]) WithUnaryInterceptor(i UnaryInterceptor) *QueryHandler[Req, Res] {
	h.interceptors = append(h.interceptors, i)
	return h
}

// WithSkipValidation disables validation for this handler.
// By default, all handlers validate requests using the validator package.
// Use this when you need to handle validation manually or when the request
// type has no validation tags.
func (h *ExecHandler[Req, Res]) WithSkipValidation() *ExecHandler[Req, Res] {
	h.skipValidation = true
	return h
}

// WithSkipValidation disables validation for this handler.
// By default, all handlers validate requests using the validator package.
// Use this when you need to handle validation manually or when the request
// type has no validation tags.
func (h *QueryHandler[Req, Res]) WithSkipValidation() *QueryHandler[Req, Res] {
	h.skipValidation = true
	return h
}

// Metadata implements [Endpoint].
func (h *ExecHandler[Req, Res]) Metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "exec",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// Metadata implements [Endpoint].
func (h *QueryHandler[Req, Res]) Metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "query",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// metadata returns the runtime metadata for the exec handler.
func (h *ExecHandler[Req, Res]) metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "exec",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// metadata returns the runtime metadata for the query handler.
func (h *QueryHandler[Req, Res]) metadata() *internal.MethodMetadata {
	var req Req
	var res Res
	return &internal.MethodMetadata{
		Primitive: "query",
		Request:   reflect.TypeOf(req),
		Response:  reflect.TypeOf(res),
	}
}

// getCacheControlHeader builds the Cache-Control header value from the cache config.
// Returns empty string if no cache config is set.
func (h *QueryHandler[Req, Res]) getCacheControlHeader() string {
	if h.cacheConfig == nil {
		return ""
	}

	cfg := h.cacheConfig
	var parts []string

	// Visibility directive
	if cfg.Public {
		parts = append(parts, "public")
	} else {
		parts = append(parts, "private")
	}

	// max-age (required if any caching is configured)
	if cfg.MaxAge > 0 {
		parts = append(parts, fmt.Sprintf("max-age=%d", int(cfg.MaxAge.Seconds())))
	}

	// s-maxage (shared cache specific)
	if cfg.SMaxAge > 0 {
		parts = append(parts, fmt.Sprintf("s-maxage=%d", int(cfg.SMaxAge.Seconds())))
	}

	// stale-while-revalidate (RFC 5861)
	if cfg.StaleWhileRevalidate > 0 {
		parts = append(parts, fmt.Sprintf("stale-while-revalidate=%d", int(cfg.StaleWhileRevalidate.Seconds())))
	}

	// stale-if-error (RFC 5861)
	if cfg.StaleIfError > 0 {
		parts = append(parts, fmt.Sprintf("stale-if-error=%d", int(cfg.StaleIfError.Seconds())))
	}

	// must-revalidate
	if cfg.MustRevalidate {
		parts = append(parts, "must-revalidate")
	}

	// immutable (RFC 8246)
	if cfg.Immutable {
		parts = append(parts, "immutable")
	}

	if len(parts) == 0 {
		return ""
	}

	// Join with ", "
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += ", " + parts[i]
	}
	return result
}

// serveHTTP implements the API handler for GET requests with caching support.
func (h *QueryHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
	decoder := func() (Req, error) {
		var req Req
		// Select decoder based on strictness setting
		decoder := schemaDecoder
		if h.strictQueryParams {
			decoder = strictSchemaDecoder
		}

		reqType := reflect.TypeOf(req)
		if reqType.Kind() == reflect.Pointer {
			// Instantiate the element
			val := reflect.New(reqType.Elem())
			// val is *Elem.
			// Decode into it
			if err := decoder.Decode(val.Interface(), ctx.request.URL.Query()); err != nil {
				return req, Errorf(CodeInvalidArgument, "failed to decode query: %v", err)
			}
			req = val.Interface().(Req)
		} else {
			// Req is a struct. &req is *Req.
			if err := decoder.Decode(&req, ctx.request.URL.Query()); err != nil {
				return req, Errorf(CodeInvalidArgument, "failed to decode query: %v", err)
			}
		}
		return req, nil
	}
	h.serve(ctx, h.getCacheControlHeader(), decoder)
}

// serveHTTP implements the API handler for POST requests.
func (h *ExecHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
	decoder := func() (Req, error) {
		var req Req
		if ctx.request.Body != nil {
			// Determine effective body size limit
			effectiveLimit := ctx.maxRequestBodySize
			if h.maxRequestBodySize != nil {
				effectiveLimit = *h.maxRequestBodySize
			}

			// Apply body size limit if > 0
			// 0 means unlimited for backwards compatibility
			if effectiveLimit > 0 {
				ctx.request.Body = http.MaxBytesReader(ctx.writer, ctx.request.Body, int64(effectiveLimit))
			}

			if err := json.NewDecoder(ctx.request.Body).Decode(&req); err != nil {
				// Empty body (EOF) is OK - treat as empty request ({})
				if !errors.Is(err, io.EOF) {
					return req, Errorf(CodeInvalidArgument, "failed to decode body: %v", err)
				}
			}
		}
		return req, nil
	}
	h.serve(ctx, "", decoder)
}

// serve implements the generic glue code for both ExecHandler and QueryHandler.
func (h *handlerBase[Req, Res]) serve(ctx *rpcContext, cacheControl string, decodeFunc func() (Req, error)) {
	// 1. Combine Interceptors
	// ctx.interceptors contains: Global + Service interceptors
	// We append Handler-level interceptors
	allInterceptors := make([]UnaryInterceptor, 0, len(ctx.interceptors)+len(h.interceptors))
	allInterceptors = append(allInterceptors, ctx.interceptors...)
	allInterceptors = append(allInterceptors, h.interceptors...)

	chain := chainInterceptors(allInterceptors)

	// 2. Decode Request
	req, decodeErr := func() (Req, error) {
		req, err := decodeFunc()
		if err != nil {
			return req, err
		}

		if !h.skipValidation {
			// Skip validation for tygor.Empty (nil is its valid zero value).
			// For other types, nil would be caught by validate.Struct anyway.
			_, isEmptyType := any(req).(Empty)
			if !isEmptyType {
				if err := validate.Struct(req); err != nil {
					return req, err
				}
			}
		}
		return req, nil
	}()

	if decodeErr != nil {
		handleError(ctx, decodeErr)
		return
	}

	// 3. Execute Chain
	// The chain eventually calls the user function.

	finalHandler := func(c context.Context, reqAny any) (any, error) {
		// Type assertion should be safe here because we only pass 'req' (type Req) into the chain.
		reqTyped, ok := reqAny.(Req)
		if !ok {
			return nil, Errorf(CodeInternal, "interceptor modified request type incorrectly")
		}
		return h.fn(c, reqTyped)
	}

	var res any
	var err error

	if chain != nil {
		res, err = chain(ctx, req, finalHandler)
	} else {
		res, err = finalHandler(ctx, req)
	}

	if err != nil {
		handleError(ctx, err)
		return
	}

	// 4. Write Response
	ctx.writer.Header().Set("Content-Type", "application/json")
	if cacheControl != "" {
		ctx.writer.Header().Set("Cache-Control", cacheControl)
	}

	if err := encodeResponse(ctx.writer, res); err != nil {
		// Response may be partially written, nothing we can do. Log for debugging.
		logger := ctx.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("failed to encode response",
			slog.String("endpoint", ctx.EndpointID()),
			slog.Any("error", err))
	}
}

func handleError(ctx *rpcContext, err error) {
	var svcErr *Error
	if ctx.errorTransformer != nil {
		svcErr = ctx.errorTransformer(err)
	}
	if svcErr == nil {
		svcErr = DefaultErrorTransformer(err)
	}
	if ctx.maskInternalErrors && svcErr.Code == CodeInternal {
		svcErr.Message = "internal server error"
	}
	writeError(ctx.writer, svcErr, ctx.logger)
}
