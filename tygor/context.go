package tygor

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"tygor.dev/internal/tgrcontext"
)

// Context provides type-safe access to request metadata and HTTP primitives.
// It embeds context.Context, so it can be used anywhere a context.Context is expected.
//
// Interceptors receive Context directly for convenient access to request metadata.
// Handlers receive context.Context but can use FromContext to get the Context if needed.
//
// For testing interceptors, implement this interface with your own type:
//
//	type testContext struct {
//	    context.Context
//	    service, method string
//	}
//	func (c *testContext) Service() string                 { return c.service }
//	func (c *testContext) EndpointID() string              { return c.service + "." + c.method }
//	func (c *testContext) HTTPRequest() *http.Request      { return nil }
//	func (c *testContext) HTTPWriter() http.ResponseWriter { return nil }
type Context interface {
	context.Context

	// Service returns the name of the service being called.
	Service() string

	// EndpointID returns the full identifier for the endpoint being called (e.g., "Users.Create").
	EndpointID() string

	// HTTPRequest returns the underlying HTTP request.
	HTTPRequest() *http.Request

	// HTTPWriter returns the underlying HTTP response writer.
	// Use with caution in handlers - prefer returning errors to writing directly.
	// This is useful for setting response headers.
	HTTPWriter() http.ResponseWriter
}

type panicRecoveryState struct {
	owned bool
}

// own gives fn responsibility for any panic that escapes it. Ownership is
// restored only after a normal return so panic unwinding leaves it latched for
// the outer request recovery. This also preserves an enclosing owner.
func (s *panicRecoveryState) own(fn func()) {
	previous := s.owned
	s.owned = true
	fn()
	s.owned = previous
}

func (s *panicRecoveryState) claim() {
	s.owned = true
}

func (s *panicRecoveryState) isOwned() bool {
	return s.owned
}

// rpcContext is the framework's implementation of Context.
type rpcContext struct {
	context.Context
	service string
	name    string
	request *http.Request
	writer  http.ResponseWriter

	// Internal fields for handler execution (not exposed via public methods)
	errorTransformer   ErrorTransformer
	maskInternalErrors bool
	interceptors       []UnaryInterceptor
	logger             *slog.Logger
	maxRequestBodySize uint64
	streamWriteTimeout time.Duration
	streamHeartbeat    time.Duration
	panicRecovery      *panicRecoveryState
}

func (c *rpcContext) Service() string                 { return c.service }
func (c *rpcContext) EndpointID() string              { return c.service + "." + c.name }
func (c *rpcContext) HTTPRequest() *http.Request      { return c.request }
func (c *rpcContext) HTTPWriter() http.ResponseWriter { return c.writer }

// derivedContext preserves standard context derivations while retaining Tygor's
// request metadata. Interceptors commonly pass context.WithValue/WithTimeout
// results to the next handler; those contexts no longer implement Context.
type derivedContext struct {
	context.Context
	metadata Context
}

func (c *derivedContext) Service() string                 { return c.metadata.Service() }
func (c *derivedContext) EndpointID() string              { return c.metadata.EndpointID() }
func (c *derivedContext) HTTPWriter() http.ResponseWriter { return c.metadata.HTTPWriter() }
func (c *derivedContext) HTTPRequest() *http.Request {
	req := c.metadata.HTTPRequest()
	if req == nil {
		return nil
	}
	return req.WithContext(c.Context)
}

// FromContext extracts the Context from a context.Context.
// Returns the Context and true if found, or nil and false if not in a tygor handler context.
//
// This is useful in handlers that receive context.Context but need access to request metadata:
//
//	func (s *MyService) GetThing(ctx context.Context, req *GetThingRequest) (*GetThingResponse, error) {
//	    tc, ok := tygor.FromContext(ctx)
//	    if ok {
//	        log.Printf("handling %s", tc.EndpointID())
//	    }
//	    // ...
//	}
func FromContext(ctx context.Context) (Context, bool) {
	if tc, ok := ctx.(Context); ok {
		return tc, true
	}

	v := ctx.Value(tgrcontext.ContextKey)
	if v == nil {
		return nil, false
	}

	// Try our type first (production path)
	if tc, ok := v.(*rpcContext); ok {
		return &derivedContext{Context: ctx, metadata: tc}, true
	}

	// Try the internal type (test utilities path)
	if rc, ok := v.(*tgrcontext.Context); ok {
		metadata := &rpcContext{
			Context: rc.Context,
			service: rc.Service,
			name:    rc.Name,
			request: rc.Request,
			writer:  rc.Writer,
		}
		return &derivedContext{Context: ctx, metadata: metadata}, true
	}

	// Try any Context implementation (user test types)
	if tc, ok := v.(Context); ok {
		return &derivedContext{Context: ctx, metadata: tc}, true
	}

	return nil, false
}

func contextWithMetadata(ctx context.Context, fallback Context) Context {
	if tc, ok := FromContext(ctx); ok {
		return tc
	}
	return &derivedContext{Context: ctx, metadata: fallback}
}

// newContext creates a new rpcContext with all fields.
func newContext(parent context.Context, w http.ResponseWriter, r *http.Request, service, name string) *rpcContext {
	ctx := &rpcContext{
		service:       service,
		name:          name,
		request:       r,
		writer:        w,
		panicRecovery: &panicRecoveryState{},
	}
	// Store self using the shared key so FromContext works
	ctx.Context = context.WithValue(parent, tgrcontext.ContextKey, ctx)
	return ctx
}
