package ir

import "fmt"

// ArrayDescriptor represents an ordered collection (slice or fixed-length array).
//
// Nullability: Go slices (Length == 0) can be nil, which serializes to JSON null.
// This is NOT represented with PtrDescriptor; instead, generators derive nullability
// from context:
// - If Optional=false: field: T[] | null (always present, can be null)
// - If Optional=true: field?: T[] (optional, never null when present)
// See §4.9 for the complete decision tree.
//
// Note: [N]byte fixed arrays serialize as JSON arrays of numbers, NOT base64.
// Only []byte slices are base64-encoded (represented as PrimitiveBytes).
type ArrayDescriptor struct {
	exprBase

	// Element is the array element type.
	Element TypeDescriptor

	// Length is 0 for slices ([]T), or >0 for fixed-length arrays ([N]T).
	// Generators MAY emit tuples for fixed arrays in languages that support them.
	Length int
}

// Kind returns KindArray.
func (d *ArrayDescriptor) Kind() DescriptorKind { return KindArray }

// Slice returns an ArrayDescriptor for a slice type.
// Panics if element is nil.
func Slice(element TypeDescriptor) *ArrayDescriptor {
	if element == nil {
		panic("ir.Slice: element cannot be nil")
	}
	return &ArrayDescriptor{Element: element, Length: 0}
}

// Array returns an ArrayDescriptor for a fixed-length array.
// Panics if element is nil or length is negative.
func Array(element TypeDescriptor, length int) *ArrayDescriptor {
	if element == nil {
		panic("ir.Array: element cannot be nil")
	}
	if length < 0 {
		panic("ir.Array: length cannot be negative")
	}
	return &ArrayDescriptor{Element: element, Length: length}
}

// MapDescriptor represents a key-value mapping.
//
// Nullability: Go maps can be nil, which serializes to JSON null.
// This is NOT represented with PtrDescriptor; instead, generators derive nullability
// from context:
// - If Optional=false: field: Record<K,V> | null (always present, can be null)
// - If Optional=true: field?: Record<K,V> (optional, never null when present)
// See §4.9 for the complete decision tree.
type MapDescriptor struct {
	exprBase

	// Key is the map key type.
	Key TypeDescriptor

	// Value is the map value type.
	Value TypeDescriptor
}

// Kind returns KindMap.
func (d *MapDescriptor) Kind() DescriptorKind { return KindMap }

// Map returns a MapDescriptor for a map type.
// Panics if key or value is nil.
func Map(key, value TypeDescriptor) *MapDescriptor {
	if key == nil {
		panic("ir.Map: key cannot be nil")
	}
	if value == nil {
		panic("ir.Map: value cannot be nil")
	}
	return &MapDescriptor{Key: key, Value: value}
}

// ReferenceDescriptor represents a reference to a named type.
type ReferenceDescriptor struct {
	exprBase

	// Target is the referenced type's identifier.
	Target GoIdentifier
}

// Kind returns KindReference.
func (d *ReferenceDescriptor) Kind() DescriptorKind { return KindReference }

// Ref returns a ReferenceDescriptor for a named type.
func Ref(name string, pkg string) *ReferenceDescriptor {
	return &ReferenceDescriptor{Target: GoIdentifier{Name: name, Package: pkg}}
}

// PtrDescriptor represents a Go pointer type (*T).
// The TypeScript output depends on field context (see §4.9):
// - If Optional=false: field: T | null (always present, can be null)
// - If Optional=true: field?: T (optional, never null when present)
type PtrDescriptor struct {
	exprBase

	// Element is the pointed-to type.
	Element TypeDescriptor
}

// Kind returns KindPtr.
func (d *PtrDescriptor) Kind() DescriptorKind { return KindPtr }

// Ptr returns a PtrDescriptor for a pointer type.
// Panics if element is nil.
func Ptr(element TypeDescriptor) *PtrDescriptor {
	if element == nil {
		panic("ir.Ptr: element cannot be nil")
	}
	return &PtrDescriptor{Element: element}
}

// UnionDescriptor represents a union of types (T1 | T2 | ...).
//
// SCOPE: UnionDescriptor currently appears ONLY within TypeParameterDescriptor.Constraint
// to represent Go type constraint unions (e.g., `~string | ~int`). It does NOT appear
// as field types or in other contexts.
//
// Note: Go's `~T` (approximate type) syntax is not preserved in the IR. Both `~string`
// and `string` in a constraint produce the same PrimitiveDescriptor. The tilde only
// affects Go compile-time type checking, not JSON serialization behavior.
type UnionDescriptor struct {
	exprBase

	// Types contains the union members. Must have at least 1 element.
	// Single-element unions are valid (e.g., [T ~string] has one union term).
	Types []TypeDescriptor
}

// Kind returns KindUnion.
func (d *UnionDescriptor) Kind() DescriptorKind { return KindUnion }

// Union returns a UnionDescriptor for a union of types.
// Panics if types is empty or contains nil elements.
func Union(types ...TypeDescriptor) *UnionDescriptor {
	if len(types) == 0 {
		panic("ir.Union: must have at least 1 type")
	}
	for i, t := range types {
		if t == nil {
			panic(fmt.Sprintf("ir.Union: type at index %d is nil", i))
		}
	}
	return &UnionDescriptor{Types: types}
}

// TypeParameterDescriptor represents a generic type parameter.
// This descriptor is only produced by the source provider; the reflection
// provider sees instantiated types and emits concrete types instead.
//
// Note: TypeParameterDescriptor appears in two contexts:
//   - Declaration: In StructDescriptor.TypeParameters or AliasDescriptor.TypeParameters,
//     where Name and Constraint define the type parameter.
//   - Usage: As a field type (FieldDescriptor.Type), where only Name is used to
//     reference back to the declaration. In usage context, Constraint is ignored.
type TypeParameterDescriptor struct {
	exprBase

	// Name is the type parameter name (e.g., "T", "K", "V").
	ParamName string

	// Constraint is the type set constraint, represented as a TypeDescriptor.
	// nil means unconstrained (equivalent to `any`).
	//
	// Common constraint patterns and their IR representation:
	// - [T any]              -> Constraint: nil
	// - [T comparable]       -> Constraint: nil (see note below)
	// - [T ~string]          -> Constraint: &UnionDescriptor{Types: [PrimitiveString]}
	// - [T ~string | ~int]   -> Constraint: &UnionDescriptor{Types: [PrimitiveString, PrimitiveInt]}
	// - [T MyConstraint]     -> Constraint: &ReferenceDescriptor{Target: "MyConstraint"}
	//
	// Note on `comparable`: The `comparable` constraint is a Go compile-time concept
	// that does not affect JSON serialization. It is NOT preserved in the IR.
	Constraint TypeDescriptor
}

// Kind returns KindTypeParameter.
func (d *TypeParameterDescriptor) Kind() DescriptorKind { return KindTypeParameter }

// TypeParam returns a TypeParameterDescriptor for a type parameter.
func TypeParam(name string, constraint TypeDescriptor) *TypeParameterDescriptor {
	return &TypeParameterDescriptor{ParamName: name, Constraint: constraint}
}
