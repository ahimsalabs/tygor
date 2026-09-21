package tygor

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// App is the central router for API handlers.
// It manages route registration, middleware, interceptors, and error handling.
// Use Handler() to get an http.Handler for use with http.ListenAndServe.
type App struct {
	mu                      sync.RWMutex
	routes                  map[string]endpointHandler
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
	handler                 http.Handler
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

// NewApp creates an application and applies options from left to right.
func NewApp(options ...AppOption) *App {
	app := &App{
		routes:             make(map[string]endpointHandler),
		maxRequestBodySize: 1 << 20, // 1MB default
		// streamWriteTimeout uses DefaultStreamWriteTimeout when not explicitly set
	}
	for _, option := range options {
		option.applyApp(app)
	}
	app.handler = app.buildHandler()
	return app
}

// getStreamWriteTimeout returns the effective stream write timeout.
func (a *App) getStreamWriteTimeout() time.Duration {
	if a.streamWriteTimeoutIsSet {
		return a.streamWriteTimeout
	}
	return defaultStreamWriteTimeout
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
//	app := tygor.NewApp(tygor.WithHTTPMiddleware(cors))
//	http.ListenAndServe(":8080", app.Handler())
func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) buildHandler() http.Handler {
	var h http.Handler = http.HandlerFunc(a.serveHTTP)
	// Apply middleware in reverse order so first added is outermost
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		h = a.middlewares[i](h)
	}
	return h
}

// ServeHTTP implements [http.Handler].
func (a *App) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	a.handler.ServeHTTP(w, req)
}

// Service returns a namespace with options applied from left to right.
func (a *App) Service(name string, options ...ServiceOption) *Service {
	service := &Service{
		registry: a,
		name:     name,
	}
	for _, option := range options {
		option.applyService(service)
	}
	return service
}

// serveHTTP handles incoming API requests (internal, called via Handler()).
func (a *App) serveHTTP(w http.ResponseWriter, req *http.Request) {
	recovery := &panicRecoveryState{}
	var rpcCtx *rpcContext
	var endpoint string
	defer func() {
		if rec := recover(); rec != nil {
			if recovery.isOwned() {
				panic(rec)
			}
			logPanic(a.logger, endpoint, "request handler", rec)
			panicErr := NewError(CodeInternal, "internal server error")
			if rpcCtx != nil {
				handleError(rpcCtx, panicErr)
				return
			}
			writePreparedErrorOwned(recovery, w, prepareErrorResponse(a.errorTransformer, a.maskInternalErrors, panicErr), a.logger)
		}
	}()

	path := strings.TrimPrefix(req.URL.Path, "/")
	// Path format: /{service_name}/{method_name}

	// Normalize path
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(recovery, w, NewError(CodeNotFound, "route not found"), a.logger)
		return
	}

	service, method := parts[0], parts[1]

	// We store keys as "Service.Method" internally to match the Manifest format
	// But the URL is /Service/Method.
	key := service + "." + method
	endpoint = key

	a.mu.RLock()
	handler, ok := a.routes[key]
	a.mu.RUnlock()

	if !ok {
		writeError(recovery, w, NewError(CodeNotFound, "route not found"), a.logger)
		return
	}

	// Check HTTP Method based on primitive
	meta := handler.metadata()
	expectedMethod := primitiveToHTTPMethod(meta.Primitive)
	if req.Method != expectedMethod {
		writeError(recovery, w, Errorf(CodeMethodNotAllowed, "method %s not allowed, expected %s", req.Method, expectedMethod), a.logger)
		return
	}

	// Create tygor Context with request metadata and config
	rpcCtx = newContext(req.Context(), w, req, service, method)
	rpcCtx.panicRecovery = recovery
	rpcCtx.errorTransformer = a.errorTransformer
	rpcCtx.maskInternalErrors = a.maskInternalErrors
	rpcCtx.interceptors = a.interceptors
	rpcCtx.logger = a.logger
	rpcCtx.maxRequestBodySize = a.maxRequestBodySize
	rpcCtx.streamWriteTimeout = a.getStreamWriteTimeout()
	rpcCtx.streamHeartbeat = a.getStreamHeartbeat()

	// Execute handler
	handler.serveHTTP(rpcCtx)
}

// Service groups endpoints under one URL and metadata namespace.
// Options passed to [App.Service] become defaults for its endpoints.
type Service struct {
	registry           *App
	name               string
	interceptors       []UnaryInterceptor
	streamInterceptors []StreamInterceptor
	maxRequestBodySize *uint64
	streamWriteTimeout *time.Duration
	streamHeartbeat    *time.Duration
}

// Exec registers a POST handler for a non-streaming API operation. Options are
// applied before the endpoint becomes visible to requests.
func (s *Service) Exec[Req, Res any](name string, fn func(context.Context, Req) (Res, error), options ...ExecOption) {
	config := execConfig{
		interceptors:       slices.Clone(s.interceptors),
		maxRequestBodySize: clonePtr(s.maxRequestBodySize),
	}
	for _, option := range options {
		option.applyExec(&config)
	}
	handler := makeExecHandler(fn, config)
	s.register(name, handler)
}

// Query registers a GET handler for a read operation. Options are applied
// before the endpoint becomes visible to requests.
func (s *Service) Query[Req, Res any](name string, fn func(context.Context, Req) (Res, error), options ...QueryOption) {
	config := queryConfig{interceptors: slices.Clone(s.interceptors)}
	for _, option := range options {
		option.applyQuery(&config)
	}
	handler := makeQueryHandler(fn, config)
	s.register(name, handler)
}

// Stream registers an SSE streaming handler. Options are applied before the
// endpoint becomes visible to requests.
//
// The handler receives a [StreamWriter] to send events to the client. Send and
// SendWithID return an error when the client disconnects, the context ends, or
// a write fails. Disconnect-related errors satisfy errors.Is(err,
// [ErrStreamClosed]); handlers should return when a send fails. Application
// errors after SSE starts are sent as a final event only while the transport
// remains usable.
//
// Example:
//
//	feed.Stream("Subscribe", Subscribe,
//	    tygor.WithStreamHeartbeat(15*time.Second),
//	    tygor.WithUnaryInterceptors(authInterceptor),
//	)
func (s *Service) Stream[Req, Res any](name string, fn func(context.Context, Req, StreamWriter[Res]) error, options ...StreamOption) {
	config := streamConfig{
		unaryInterceptors:  slices.Clone(s.interceptors),
		streamInterceptors: slices.Clone(s.streamInterceptors),
		maxRequestBodySize: clonePtr(s.maxRequestBodySize),
		writeTimeout:       clonePtr(s.streamWriteTimeout),
		heartbeatInterval:  clonePtr(s.streamHeartbeat),
	}
	for _, option := range options {
		option.applyStream(&config)
	}
	handler := makeStreamHandler(fn, config)
	s.register(name, handler)
}

// LiveValue registers a synchronized value as an SSE endpoint. Options are
// applied before the endpoint becomes visible to requests.
func (s *Service) LiveValue[T any](name string, value *LiveValue[T], options ...LiveValueOption) {
	config := liveValueConfig{
		interceptors:      slices.Clone(s.interceptors),
		writeTimeout:      clonePtr(s.streamWriteTimeout),
		heartbeatInterval: clonePtr(s.streamHeartbeat),
	}
	for _, option := range options {
		option.applyLiveValue(&config)
	}
	handler := makeLiveValueHandler(value, config)
	s.register(name, handler)
}

// register registers a handler for the given operation name.
// If a handler is already registered for this service and method, it will be replaced
// and a warning will be logged.
func (s *Service) register(name string, handler endpointHandler) {
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

	s.registry.routes[key] = handler
}
