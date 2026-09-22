package ir

import "fmt"

// ArrayDescriptor represents an ordered collection (slice or fixed-length array).
//
// Under default encoding/json/v2 semantics, nil slices encode as [] rather than
// null. Exact []byte and [N]byte types use PrimitiveBytes instead.
type ArrayDescriptor struct {
	exprBase

	// Element is the array element type.
	Element TypeDescriptor

	// Length is 0 for slices and [0]T arrays, or positive for [N]T.
	// Generators MAY emit tuples for fixed arrays in languages that support them.
	Length int

	// IsArray explicitly marks a fixed array, including [0]T. For compatibility,
	// descriptors with Length > 0 are arrays even when this field is false.
	IsArray bool
}

// Kind returns KindArray.
func (d *ArrayDescriptor) Kind() DescriptorKind { return KindArray }

// IsSlice reports whether the descriptor represents a slice. A missing marker
// with Length 0 retains the historical slice interpretation.
func (d *ArrayDescriptor) IsSlice() bool { return d.Length == 0 && !d.IsArray }

// Slice returns an ArrayDescriptor for a slice type.
// Panics if element is nil.
func Slice(element TypeDescriptor) *ArrayDescriptor {
	if element == nil {
		panic("ir.Slice: element cannot be nil")
	}
	return &ArrayDescriptor{Element: element}
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
	return &ArrayDescriptor{Element: element, Length: length, IsArray: true}
}

// MapDescriptor represents a key-value mapping.
//
// Under default encoding/json/v2 semantics, nil maps encode as {} rather than null.
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

	// TypeArguments contains the arguments for a generic application. Arguments
	// may include type parameters declared by the containing type. It is empty
	// only for references to non-generic declarations.
	TypeArguments []TypeDescriptor
}

// Kind returns KindReference.
func (d *ReferenceDescriptor) Kind() DescriptorKind { return KindReference }

// Ref returns a ReferenceDescriptor for a named type.
func Ref(name string, pkg string) *ReferenceDescriptor {
	return &ReferenceDescriptor{Target: GoIdentifier{Name: name, Package: pkg}}
}

// RefWithArgs returns a ReferenceDescriptor for an applied generic type.
// Panics if any type argument is nil.
func RefWithArgs(name string, pkg string, args ...TypeDescriptor) *ReferenceDescriptor {
	for i, arg := range args {
		if arg == nil {
			panic(fmt.Sprintf("ir.RefWithArgs: type argument at index %d is nil", i))
		}
	}
	return &ReferenceDescriptor{
		Target:        GoIdentifier{Name: name, Package: pkg},
		TypeArguments: append([]TypeDescriptor(nil), args...),
	}
}

// PtrDescriptor represents a Go pointer type (*T).
// The generated field type depends on omission context: a pointer without an
// omission tag is nullable, while an omitted nil pointer is non-null when present.
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
