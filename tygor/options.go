package tygor

import (
	json "encoding/json/v2"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

// AppOption configures an [App] before it is used.
type AppOption interface {
	applyApp(*App)
}

// ServiceOption configures defaults for every endpoint in a service.
type ServiceOption interface {
	applyService(*Service)
}

// ExecOption configures an exec endpoint before it is registered.
type ExecOption interface {
	applyExec(*execConfig)
}

// QueryOption configures a query endpoint before it is registered.
type QueryOption interface {
	applyQuery(*queryConfig)
}

// StreamOption configures a stream endpoint before it is registered.
type StreamOption interface {
	applyStream(*streamConfig)
}

// LiveValueOption configures a live value endpoint before it is registered.
type LiveValueOption interface {
	applyLiveValue(*liveValueConfig)
}

// JSONOption configures encoding/json/v2 behavior at any server scope.
// Options at narrower scopes override options at broader scopes according to
// [json.JoinOptions].
type JSONOption interface {
	AppOption
	ServiceOption
	ExecOption
	QueryOption
	StreamOption
	LiveValueOption
}

// UnaryInterceptorOption can configure unary interceptors at every server scope.
type UnaryInterceptorOption interface {
	AppOption
	ServiceOption
	ExecOption
	QueryOption
	StreamOption
	LiveValueOption
}

// RequestBodySizeOption can configure request body limits at every applicable scope.
type RequestBodySizeOption interface {
	AppOption
	ServiceOption
	ExecOption
	StreamOption
}

// StreamTimingOption can configure SSE timing at every applicable scope.
type StreamTimingOption interface {
	AppOption
	ServiceOption
	StreamOption
	LiveValueOption
}

// StreamInterceptorOption can configure stream interceptors for a service or endpoint.
type StreamInterceptorOption interface {
	ServiceOption
	StreamOption
}

// ValidationOption can disable validation for request-bearing endpoints.
type ValidationOption interface {
	ExecOption
	QueryOption
	StreamOption
}

type execConfig struct {
	interceptors       []UnaryInterceptor
	skipValidation     bool
	maxRequestBodySize *uint64
	jsonOptions        json.Options
}

type queryConfig struct {
	interceptors      []UnaryInterceptor
	skipValidation    bool
	cacheConfig       *CacheConfig
	strictQueryParams bool
	jsonOptions       json.Options
}

type streamConfig struct {
	unaryInterceptors  []UnaryInterceptor
	streamInterceptors []StreamInterceptor
	skipValidation     bool
	maxRequestBodySize *uint64
	writeTimeout       *time.Duration
	heartbeatInterval  *time.Duration
	jsonOptions        json.Options
}

type liveValueConfig struct {
	interceptors      []UnaryInterceptor
	writeTimeout      *time.Duration
	heartbeatInterval *time.Duration
	jsonOptions       json.Options
}

type jsonOption struct{ options json.Options }

// WithJSONOptions composes encoding/json/v2 options at the current scope.
// Repeated calls are joined in application order, so later values win.
func WithJSONOptions(options ...json.Options) JSONOption {
	return jsonOption{options: json.JoinOptions(options...)}
}

func (o jsonOption) applyApp(app *App) {
	app.jsonOptions = json.JoinOptions(app.jsonOptions, o.options)
}
func (o jsonOption) applyService(service *Service) {
	service.jsonOptions = json.JoinOptions(service.jsonOptions, o.options)
}
func (o jsonOption) applyExec(config *execConfig) {
	config.jsonOptions = json.JoinOptions(config.jsonOptions, o.options)
}
func (o jsonOption) applyQuery(config *queryConfig) {
	config.jsonOptions = json.JoinOptions(config.jsonOptions, o.options)
}
func (o jsonOption) applyStream(config *streamConfig) {
	config.jsonOptions = json.JoinOptions(config.jsonOptions, o.options)
}
func (o jsonOption) applyLiveValue(config *liveValueConfig) {
	config.jsonOptions = json.JoinOptions(config.jsonOptions, o.options)
}

type unaryInterceptorsOption struct {
	interceptors []UnaryInterceptor
}

// WithUnaryInterceptors appends interceptors in the order provided. App
// interceptors run before service interceptors, which run before endpoint
// interceptors. For stream and live value endpoints, unary interceptors run
// during setup only: interceptor-derived contexts and requests do not govern
// the stream lifetime, and repeated calls to next do not replay the producer.
func WithUnaryInterceptors(interceptors ...UnaryInterceptor) UnaryInterceptorOption {
	return unaryInterceptorsOption{interceptors: slices.Clone(interceptors)}
}

func (o unaryInterceptorsOption) applyApp(app *App) {
	app.interceptors = append(app.interceptors, o.interceptors...)
}

func (o unaryInterceptorsOption) applyService(service *Service) {
	service.interceptors = append(service.interceptors, o.interceptors...)
}

func (o unaryInterceptorsOption) applyExec(config *execConfig) {
	config.interceptors = append(config.interceptors, o.interceptors...)
}

func (o unaryInterceptorsOption) applyQuery(config *queryConfig) {
	config.interceptors = append(config.interceptors, o.interceptors...)
}

func (o unaryInterceptorsOption) applyStream(config *streamConfig) {
	config.unaryInterceptors = append(config.unaryInterceptors, o.interceptors...)
}

func (o unaryInterceptorsOption) applyLiveValue(config *liveValueConfig) {
	config.interceptors = append(config.interceptors, o.interceptors...)
}

type streamInterceptorsOption struct {
	interceptors []StreamInterceptor
}

// WithStreamInterceptors appends event-stream interceptors in the order provided.
func WithStreamInterceptors(interceptors ...StreamInterceptor) StreamInterceptorOption {
	return streamInterceptorsOption{interceptors: slices.Clone(interceptors)}
}

func (o streamInterceptorsOption) applyService(service *Service) {
	service.streamInterceptors = append(service.streamInterceptors, o.interceptors...)
}

func (o streamInterceptorsOption) applyStream(config *streamConfig) {
	config.streamInterceptors = append(config.streamInterceptors, o.interceptors...)
}

type errorTransformerOption struct {
	transformer ErrorTransformer
}

// WithErrorTransformer configures application error transformation.
func WithErrorTransformer(transformer ErrorTransformer) AppOption {
	return errorTransformerOption{transformer: transformer}
}

func (o errorTransformerOption) applyApp(app *App) {
	app.errorTransformer = o.transformer
}

type maskInternalErrorsOption struct{}

// WithMaskInternalErrors prevents internal error messages from being sent to clients.
func WithMaskInternalErrors() AppOption {
	return maskInternalErrorsOption{}
}

func (maskInternalErrorsOption) applyApp(app *App) {
	app.maskInternalErrors = true
}

type httpMiddlewareOption struct {
	middlewares []func(http.Handler) http.Handler
}

// WithHTTPMiddleware appends standard HTTP middleware in the order provided.
func WithHTTPMiddleware(middlewares ...func(http.Handler) http.Handler) AppOption {
	return httpMiddlewareOption{middlewares: slices.Clone(middlewares)}
}

func (o httpMiddlewareOption) applyApp(app *App) {
	app.middlewares = append(app.middlewares, o.middlewares...)
}

type loggerOption struct {
	logger *slog.Logger
}

// WithLogger configures the application logger.
func WithLogger(logger *slog.Logger) AppOption {
	return loggerOption{logger: logger}
}

func (o loggerOption) applyApp(app *App) {
	app.logger = o.logger
}

type maxRequestBodySizeOption struct {
	size uint64
}

// WithMaxRequestBodySize sets the request body limit. A value of zero disables the limit.
func WithMaxRequestBodySize(size uint64) RequestBodySizeOption {
	return maxRequestBodySizeOption{size: size}
}

func (o maxRequestBodySizeOption) applyApp(app *App) {
	app.maxRequestBodySize = o.size
}

func (o maxRequestBodySizeOption) applyService(service *Service) {
	service.maxRequestBodySize = ptr(o.size)
}

func (o maxRequestBodySizeOption) applyExec(config *execConfig) {
	config.maxRequestBodySize = ptr(o.size)
}

func (o maxRequestBodySizeOption) applyStream(config *streamConfig) {
	config.maxRequestBodySize = ptr(o.size)
}

type streamWriteTimeoutOption struct {
	timeout time.Duration
}

// WithStreamWriteTimeout sets the SSE write timeout. A value of zero disables it.
func WithStreamWriteTimeout(timeout time.Duration) StreamTimingOption {
	return streamWriteTimeoutOption{timeout: timeout}
}

func (o streamWriteTimeoutOption) applyApp(app *App) {
	app.streamWriteTimeout = o.timeout
	app.streamWriteTimeoutIsSet = true
}

func (o streamWriteTimeoutOption) applyService(service *Service) {
	service.streamWriteTimeout = ptr(o.timeout)
}

func (o streamWriteTimeoutOption) applyStream(config *streamConfig) {
	config.writeTimeout = ptr(o.timeout)
}

func (o streamWriteTimeoutOption) applyLiveValue(config *liveValueConfig) {
	config.writeTimeout = ptr(o.timeout)
}

type streamHeartbeatOption struct {
	interval time.Duration
}

// WithStreamHeartbeat sets the SSE heartbeat interval. A value of zero disables it.
func WithStreamHeartbeat(interval time.Duration) StreamTimingOption {
	return streamHeartbeatOption{interval: interval}
}

func (o streamHeartbeatOption) applyApp(app *App) {
	app.streamHeartbeat = o.interval
	app.streamHeartbeatIsSet = true
}

func (o streamHeartbeatOption) applyService(service *Service) {
	service.streamHeartbeat = ptr(o.interval)
}

func (o streamHeartbeatOption) applyStream(config *streamConfig) {
	config.heartbeatInterval = ptr(o.interval)
}

func (o streamHeartbeatOption) applyLiveValue(config *liveValueConfig) {
	config.heartbeatInterval = ptr(o.interval)
}

type cacheControlOption struct {
	config CacheConfig
}

// WithCacheControl configures HTTP caching for a query endpoint.
func WithCacheControl(config CacheConfig) QueryOption {
	return cacheControlOption{config: config}
}

func (o cacheControlOption) applyQuery(config *queryConfig) {
	config.cacheConfig = ptr(o.config)
}

type strictQueryParamsOption struct{}

// WithStrictQueryParams rejects unknown query parameters.
func WithStrictQueryParams() QueryOption {
	return strictQueryParamsOption{}
}

func (strictQueryParamsOption) applyQuery(config *queryConfig) {
	config.strictQueryParams = true
}

type withoutValidationOption struct{}

// WithoutValidation disables request validation for an endpoint.
func WithoutValidation() ValidationOption {
	return withoutValidationOption{}
}

func (withoutValidationOption) applyExec(config *execConfig) {
	config.skipValidation = true
}

func (withoutValidationOption) applyQuery(config *queryConfig) {
	config.skipValidation = true
}

func (withoutValidationOption) applyStream(config *streamConfig) {
	config.skipValidation = true
}

func ptr[T any](value T) *T {
	return &value
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return ptr(*value)
}
