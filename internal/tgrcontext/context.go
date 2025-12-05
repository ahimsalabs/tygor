// Package tgrcontext provides the shared context key for tygor.
// This allows internal test utilities to create compatible contexts
// without importing the main tygor package (avoiding import cycles).
package tgrcontext

import (
	"context"
	"net/http"
)

// ContextKey is the key used to store tygor.Context in context.Context.
// This is exported so internal packages can create compatible contexts.
var ContextKey = &struct{ name string }{"tygor"}

// Context mirrors tygor.Context for use in internal packages.
// The tygor package defines its own Context type with methods.
type Context struct {
	context.Context
	Service string
	Name    string
	Request *http.Request
	Writer  http.ResponseWriter
}

// NewContext creates a context with service metadata.
// The resulting context is compatible with tygor.FromContext.
func NewContext(parent context.Context, w http.ResponseWriter, r *http.Request, service, name string) *Context {
	ctx := &Context{
		Service: service,
		Name:    name,
		Request: r,
		Writer:  w,
	}
	ctx.Context = context.WithValue(parent, ContextKey, ctx)
	return ctx
}
