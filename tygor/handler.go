package tygor

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
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
	// Use "json" tags for query parameter names instead of "schema" tags.
	// This ensures TypeScript clients send the same property names (from json tags)
	// that work for both POST bodies and GET query params.
	schemaDecoder.SetAliasTag("json")
	strictSchemaDecoder.SetAliasTag("json")

	schemaDecoder.IgnoreUnknownKeys(true)
	strictSchemaDecoder.IgnoreUnknownKeys(false)
}

// endpointHandler is the internal interface used by the framework to serve requests.
type endpointHandler interface {
	serveHTTP(ctx *rpcContext)
	metadata() *internal.MethodMetadata
}

// handlerBase contains common configuration shared by exec and query handlers.
type handlerBase[Req any, Res any] struct {
	fn             func(context.Context, Req) (Res, error)
	interceptors   []UnaryInterceptor
	skipValidation bool
	jsonOptions    json.Options
}

type execHandler[Req any, Res any] struct {
	handlerBase[Req, Res]
	maxRequestBodySize *uint64 // nil means use registry default
}

func newExecHandler[Req any, Res any](fn func(context.Context, Req) (Res, error), options ...ExecOption) *execHandler[Req, Res] {
	config := execConfig{jsonOptions: json.DefaultOptionsV2()}
	for _, option := range options {
		option.applyExec(&config)
	}
	return makeExecHandler(fn, config)
}

func makeExecHandler[Req any, Res any](fn func(context.Context, Req) (Res, error), config execConfig) *execHandler[Req, Res] {
	return &execHandler[Req, Res]{
		handlerBase: handlerBase[Req, Res]{
			fn:             fn,
			interceptors:   config.interceptors,
			skipValidation: config.skipValidation,
			jsonOptions:    config.jsonOptions,
		},
		maxRequestBodySize: config.maxRequestBodySize,
	}
}

type queryHandler[Req any, Res any] struct {
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

func newQueryHandler[Req any, Res any](fn func(context.Context, Req) (Res, error), options ...QueryOption) *queryHandler[Req, Res] {
	config := queryConfig{jsonOptions: json.DefaultOptionsV2()}
	for _, option := range options {
		option.applyQuery(&config)
	}
	return makeQueryHandler(fn, config)
}

func makeQueryHandler[Req any, Res any](fn func(context.Context, Req) (Res, error), config queryConfig) *queryHandler[Req, Res] {
	return &queryHandler[Req, Res]{
		handlerBase: handlerBase[Req, Res]{
			fn:             fn,
			interceptors:   config.interceptors,
			skipValidation: config.skipValidation,
			jsonOptions:    config.jsonOptions,
		},
		cacheConfig:       config.cacheConfig,
		strictQueryParams: config.strictQueryParams,
	}
}

// metadata returns the runtime metadata for the exec handler.
func (h *execHandler[Req, Res]) metadata() *internal.MethodMetadata {
	return &internal.MethodMetadata{
		Primitive:   "exec",
		Request:     reflect.TypeFor[Req](),
		Response:    reflect.TypeFor[Res](),
		JSONOptions: h.jsonOptions,
	}
}

// metadata returns the runtime metadata for the query handler.
func (h *queryHandler[Req, Res]) metadata() *internal.MethodMetadata {
	return &internal.MethodMetadata{
		Primitive:   "query",
		Request:     reflect.TypeFor[Req](),
		Response:    reflect.TypeFor[Res](),
		JSONOptions: h.jsonOptions,
	}
}

// getCacheControlHeader builds the Cache-Control header value from the cache config.
// Returns empty string if no cache config is set.
func (h *queryHandler[Req, Res]) getCacheControlHeader() string {
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
func (h *queryHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
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
func (h *execHandler[Req, Res]) serveHTTP(ctx *rpcContext) {
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

			if err := decodeJSONBody(ctx.request.Body, &req, h.jsonOptions); err != nil {
				return req, Errorf(CodeInvalidArgument, "failed to decode body: %v", err)
			}
		}
		return req, nil
	}
	h.serve(ctx, "", decoder)
}

// decodeJSONBody decodes at most one JSON value. An empty body leaves dst at
// its zero value; after a value, only JSON whitespace is allowed.
func decodeJSONBody(body io.Reader, dst any, options ...json.Options) error {
	decoder := jsontext.NewDecoder(body, options...)
	value, err := decoder.ReadValue()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(value, dst, options...); err != nil {
		return err
	}

	if _, err := decoder.ReadValue(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("request body must contain exactly one JSON value")
}

func validateRequest(req any) error {
	// tygor.Empty is a nil *struct{} by design.
	if _, isEmptyType := req.(Empty); isEmptyType {
		return nil
	}

	value := reflect.ValueOf(req)
	if !value.IsValid() || value.Kind() == reflect.Pointer && value.IsNil() {
		return NewError(CodeInvalidArgument, "request body must not be null")
	}
	return validate.Struct(req)
}

// serve implements the generic glue code for both exec and query handlers.
func (h *handlerBase[Req, Res]) serve(ctx *rpcContext, cacheControl string, decodeFunc func() (Req, error)) {
	// 1. Combine Interceptors
	// The request context contains app interceptors; the immutable handler
	// contains service and endpoint interceptors.
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
			if err := validateRequest(req); err != nil {
				return req, err
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

	// 4. Marshal the complete response before committing success.
	data, marshalErr := marshalResponse(res, h.jsonOptions)
	if marshalErr != nil {
		logger := ctx.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("failed to marshal response",
			slog.String("endpoint", ctx.EndpointID()),
			slog.Any("error", marshalErr))
		handleError(ctx, fmt.Errorf("marshal response: %w", marshalErr))
		return
	}

	// 5. Write Response
	var n int
	var writeErr error
	ctx.panicRecovery.own(func() {
		ctx.writer.Header().Set("Content-Type", "application/json")
		if cacheControl != "" {
			ctx.writer.Header().Set("Cache-Control", cacheControl)
		}
		n, writeErr = ctx.writer.Write(data)
	})
	if writeErr != nil || n != len(data) {
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		logger := ctx.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("failed to write response",
			slog.String("endpoint", ctx.EndpointID()),
			slog.Any("error", writeErr))
	}
}

func handleError(ctx *rpcContext, err error) {
	prepared := prepareErrorResponse(ctx.errorTransformer, ctx.maskInternalErrors, err)
	writePreparedErrorOwned(ctx.panicRecovery, ctx.writer, prepared, ctx.logger)
}
