// Package internal contains types for code generation.
// These types are exported for use by tygorgen but are not intended for direct use.
package internal

import "reflect"

// MethodMetadata holds runtime metadata for a registered service method.
type MethodMetadata struct {
	Name      string
	Primitive string // "query", "exec", "stream"
	Request   reflect.Type
	Response  reflect.Type
}

// RouteMap maps route names to their metadata.
type RouteMap map[string]*MethodMetadata
