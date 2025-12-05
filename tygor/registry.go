package tygor

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"tygor.dev/internal"
)

// App is the central router for API handlers.
// It manages route registration, middleware, interceptors, and error handling.
// Use Handler() to get an http.Handler for use with http.ListenAndServe.
type App struct {
	mu                      sync.RWMutex
	routes                  map[string]Endpoint
	errorTransformer        ErrorTransformer
	maskInternalErrors      bool
	interceptors            []UnaryInterceptor
	middlewares             []func(http.Handler) http.Handler
	logger                  *slog.Logger
	maxRequestBodySize      uint64
	streamWriteTimeout      time.Duration
	streamWriteTimeoutIsSet bool // distinguishes zero (disabled) from unset (use default)
	streamHeartbeat         time.Duration
	streamHeartbeatIsSet    bool // distinguishes zero (disabled) from unset (use default)
}

const (
	// defaultStreamWriteTimeout is the default timeout for writing SSE events.
	// If a write takes longer than this, the stream is closed to prevent
	// goroutine leaks from stuck or slow clients.
	defaultStreamWriteTimeout = 30 * time.Second

	// defaultStreamHeartbeat is the default interval for sending SSE heartbeat comments.
	// This keeps connections alive through proxies that have idle timeouts (typically 60s).
	defaultStreamHeartbeat = 30 * time.Second
)

// primitiveToHTTPMethod maps tygor primitives to HTTP methods.
func primitiveToHTTPMethod(primitive string) string {
	switch primitive {
	case "query":
		return "GET"
	case "exec", "stream":
		return "POST"
	default:
		return "POST" // safe default
	}
}

func NewApp() *App {
	return &App{
		routes:             make(map[string]Endpoint),
		maxRequestBodySize: 1 << 20, // 1MB default
		// streamWriteTimeout uses DefaultStreamWriteTimeout when not explicitly set
	}
}

// WithErrorTransformer adds a custom error transformer.
// It returns the app for chaining.
func (a *App) WithErrorTransformer(fn ErrorTransformer) *App {
	a.errorTransformer = fn
	return a
}

// WithMaskInternalErrors enables masking of internal error messages.
// This is useful in production to avoid leaking sensitive information.
// The original error is still available to interceptors and logging.
func (a *App) WithMaskInternalErrors() *App {
	a.maskInternalErrors = true
	return a
}

// WithUnaryInterceptor adds a global interceptor.
// Global interceptors are executed before service-level and handler-level interceptors.
//
// Interceptor execution order:
//  1. Global interceptors (added via App.WithUnaryInterceptor)
//  2. Service interceptors (added via Service.WithUnaryInterceptor)
//  3. Handler interceptors (added via Handler.WithUnaryInterceptor)
//  4. Handler function
//
// Within each level, interceptors execute in the order they were added.
func (a *App) WithUnaryInterceptor(i UnaryInterceptor) *App {
	a.interceptors = append(a.interceptors, i)
	return a
}

// WithMiddleware adds an HTTP middleware to wrap the app.
// Middleware is applied in the order added (first added is outermost).
func (a *App) WithMiddleware(mw func(http.Handler) http.Handler) *App {
	a.middlewares = append(a.middlewares, mw)
	return a
}

// WithLogger sets a custom logger for the app.
// If not set, slog.Default() will be used.
func (a *App) WithLogger(logger *slog.Logger) *App {
	a.logger = logger
	return a
}

// WithMaxRequestBodySize sets the default maximum request body size for all handlers.
// Individual handlers can override this with Handler.WithMaxRequestBodySize.
// A value of 0 means no limit. Default is 1MB (1 << 20).
func (a *App) WithMaxRequestBodySize(size uint64) *App {
	a.maxRequestBodySize = size
	return a
}

// WithStreamWriteTimeout sets the default timeout for writing SSE events.
// If a single event write takes longer than this, the stream is closed.
// Individual handlers can override this with StreamHandler.WithWriteTimeout.
//
// Default is 30 seconds. Use 0 to disable (not recommended - risks goroutine leaks).
func (a *App) WithStreamWriteTimeout(d time.Duration) *App {
	a.streamWriteTimeout = d
	a.streamWriteTimeoutIsSet = true
	return a
}

// getStreamWriteTimeout returns the effective stream write timeout.
func (a *App) getStreamWriteTimeout() time.Duration {
	if a.streamWriteTimeoutIsSet {
		return a.streamWriteTimeout
	}
	return defaultStreamWriteTimeout
}

// WithStreamHeartbeat sets the default interval for sending SSE heartbeat comments.
// Heartbeats keep connections alive through proxies with idle timeouts.
// Individual handlers can override this with StreamHandler.WithHeartbeat.
//
// Default is 30 seconds. Use 0 to disable heartbeats.
func (a *App) WithStreamHeartbeat(d time.Duration) *App {
	a.streamHeartbeat = d
	a.streamHeartbeatIsSet = true
	return a
}

// getStreamHeartbeat returns the effective stream heartbeat interval.
func (a *App) getStreamHeartbeat() time.Duration {
	if a.streamHeartbeatIsSet {
		return a.streamHeartbeat
	}
	return defaultStreamHeartbeat
}

// Handler returns an http.Handler for use with http.ListenAndServe or other
// HTTP servers. The returned handler includes all configured middleware.
//
// Example:
//
//	app := tygor.NewApp().WithMiddleware(cors)
//	http.ListenAndServe(":8080", app.Handler())
func (a *App) Handler() http.Handler {
	var h http.Handler = http.HandlerFunc(a.serveHTTP)
	// Apply middleware in reverse order so first added is outermost
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		h = a.middlewares[i](h)
	}
	return h
}

// Service returns a Service namespace.
func (a *App) Service(name string) *Service {
	return &Service{
		registry: a,
		name:     name,
	}
}

// serveHTTP handles incoming API requests (internal, called via Handler()).
func (a *App) serveHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			stack := debug.Stack()
			logger := a.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("PANIC recovered",
				slog.Any("panic", rec),
				slog.String("stack", string(stack)))
			writeError(w, NewError(CodeInternal, fmt.Sprintf("internal server error (panic): %v", rec)), a.logger)
		}
	}()

	path := strings.TrimPrefix(req.URL.Path, "/")
	// Path format: /{service_name}/{method_name}

	// Normalize path
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		writeError(w, NewError(CodeNotFound, "route not found"), a.logger)
		return
	}

	service, method := parts[0], parts[1]

	// We store keys as "Service.Method" internally to match the Manifest format
	// But the URL is /Service/Method.
	key := service + "." + method

	a.mu.RLock()
	handler, ok := a.routes[key]
	a.mu.RUnlock()

	if !ok {
		writeError(w, NewError(CodeNotFound, "route not found"), a.logger)
		return
	}

	// Type assert to internal handler interface
	h, ok := handler.(endpointHandler)
	if !ok {
		writeError(w, NewError(CodeInternal, "invalid handler type"), a.logger)
		return
	}

	// Check HTTP Method based on primitive
	meta := h.metadata()
	expectedMethod := primitiveToHTTPMethod(meta.Primitive)
	if req.Method != expectedMethod {
		writeError(w, Errorf(CodeMethodNotAllowed, "method %s not allowed, expected %s", req.Method, expectedMethod), a.logger)
		return
	}

	// Create tygor Context with request metadata and config
	ctx := newContext(req.Context(), w, req, service, method)
	ctx.errorTransformer = a.errorTransformer
	ctx.maskInternalErrors = a.maskInternalErrors
	ctx.interceptors = a.interceptors
	ctx.logger = a.logger
	ctx.maxRequestBodySize = a.maxRequestBodySize
	ctx.streamWriteTimeout = a.getStreamWriteTimeout()
	ctx.streamHeartbeat = a.getStreamHeartbeat()

	// Execute handler
	h.serveHTTP(ctx)
}

type Service struct {
	registry     *App
	name         string
	interceptors []UnaryInterceptor
}

// WithUnaryInterceptor adds an interceptor to this service.
// Service interceptors execute after global interceptors but before handler interceptors.
// See App.WithUnaryInterceptor for the complete execution order.
func (s *Service) WithUnaryInterceptor(i UnaryInterceptor) *Service {
	s.interceptors = append(s.interceptors, i)
	return s
}

// Register registers a handler for the given operation name.
// If a handler is already registered for this service and method, it will be replaced
// and a warning will be logged.
func (s *Service) Register(name string, handler Endpoint) {
	// Type assert to internal handler interface
	h, ok := handler.(endpointHandler)
	if !ok {
		panic("tygor: handler must be created with Exec(), Query(), or Stream()")
	}

	key := s.name + "." + name
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()

	// Check for duplicate registration
	if _, exists := s.registry.routes[key]; exists {
		logger := s.registry.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("duplicate route registration",
			slog.String("service", s.name),
			slog.String("method", name),
			slog.String("route", key))
	}

	// Wrap the handler to include service interceptors
	wrappedHandler := &serviceWrappedHandler{
		inner:        h,
		interceptors: s.interceptors,
	}

	s.registry.routes[key] = wrappedHandler
}

type serviceWrappedHandler struct {
	inner        endpointHandler
	interceptors []UnaryInterceptor
}

func (h *serviceWrappedHandler) serveHTTP(ctx *rpcContext) {
	// Combine: Global (ctx.interceptors) + Service (h.interceptors)
	combined := make([]UnaryInterceptor, 0, len(ctx.interceptors)+len(h.interceptors))
	combined = append(combined, ctx.interceptors...)
	combined = append(combined, h.interceptors...)

	// Update context with combined interceptors
	ctx.interceptors = combined

	h.inner.serveHTTP(ctx)
}

func (h *serviceWrappedHandler) metadata() *internal.MethodMetadata {
	return h.inner.metadata()
}

func (h *serviceWrappedHandler) Metadata() *internal.MethodMetadata {
	return h.inner.Metadata()
}
