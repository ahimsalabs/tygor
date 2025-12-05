package tygor

import (
	"context"
)

// HandlerFunc represents the next handler in an interceptor chain.
// It is passed to [UnaryInterceptor] functions to invoke the next interceptor
// or the final handler.
type HandlerFunc func(ctx context.Context, req any) (res any, err error)

// UnaryInterceptor is a hook that wraps handler execution for unary (non-streaming) calls.
//
// Interceptors receive Context for type-safe access to request metadata:
//
//	func loggingInterceptor(ctx tygor.Context, req any, handler tygor.HandlerFunc) (any, error) {
//	    start := time.Now()
//	    res, err := handler(ctx, req)
//	    log.Printf("%s took %v", ctx.EndpointID(), time.Since(start))
//	    return res, err
//	}
//
// The handler parameter is the next handler in the chain. Interceptors can:
//   - Inspect/modify the request before calling handler
//   - Inspect/modify the response after calling handler
//   - Short-circuit by returning an error without calling handler
//   - Add values to context using context.WithValue
//
// req/res are pointers to the request/response structs.
type UnaryInterceptor func(ctx Context, req any, handler HandlerFunc) (res any, err error)

// chainInterceptors combines multiple interceptors into a single one.
// The first interceptor in the slice is the outer-most one (runs first).
func chainInterceptors(interceptors []UnaryInterceptor) UnaryInterceptor {
	if len(interceptors) == 0 {
		return nil
	}
	if len(interceptors) == 1 {
		return interceptors[0]
	}
	return func(ctx Context, req any, handler HandlerFunc) (any, error) {
		// Chain: i[0] -> i[1] -> ... -> handler
		// We recursively build the chain
		var chain HandlerFunc = handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chain
			chain = func(ctx context.Context, req any) (any, error) {
				// Convert context.Context back to Context
				// This works because the interceptor passes ctx (which is Context) to handler
				tygorCtx, ok := ctx.(Context)
				if !ok {
					// If someone wrapped the context, extract our Context from it
					tygorCtx, _ = FromContext(ctx)
				}
				return current(tygorCtx, req, next)
			}
		}
		return chain(ctx, req)
	}
}
