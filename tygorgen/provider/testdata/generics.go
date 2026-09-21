package testdata

import (
	"fmt"
)

// Response is a generic response wrapper.
type Response[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error,omitempty"`
}

// Phantom keeps its type argument out of its fields so endpoint conversion
// cannot infer the concrete argument from the instantiated struct shape.
type Phantom[T any] struct {
	OK bool `json:"ok"`
}

// Container has a constrained type parameter.
type Container[T comparable] struct {
	Value T `json:"value"`
}

// Pair holds two values of potentially different types.
type Pair[K, V any] struct {
	Key   K `json:"key"`
	Value V `json:"value"`
}

// Constrained uses a union constraint.
type Stringish interface {
	~string | ~int
}

type Wrapper[T Stringish] struct {
	Item T `json:"item"`
}

// StringerBox preserves a named method-only constraint.
type StringerBox[T fmt.Stringer] struct {
	Item T `json:"item"`
}

// RecursiveList and RecursiveMap exercise recursive aliases.
type RecursiveList []RecursiveList
type RecursiveMap map[string]RecursiveMap

// MultiConstraint has multiple type parameters with constraints.
type MultiConstraint[K comparable, V any] struct {
	Key   K `json:"key"`
	Value V `json:"value"`
}

// DependentContainer constrains S using an earlier byte-excluding E parameter.
type DependentContainer[E ~string, S ~[]E] struct {
	Values S `json:"values"`
}

type GenericEmailBox[T ~string] struct {
	Value T `json:"value" validate:"email"`
}

type GenericEmailPointer[T ~string] *T

type GenericEmailPointerHolder struct {
	Email GenericEmailPointer[string] `json:"email" validate:"email"`
}

// RecursivePointerAlias is a legal pointer-only recursive alias.
type RecursivePointerAlias *RecursivePointerAlias

type RecursivePointerAliasHolder struct {
	Value RecursivePointerAlias `json:"value"`
}

type RecursiveGenericPointerAlias[T any] *RecursiveGenericPointerAlias[T]

type RecursiveGenericPointerAliasHolder struct {
	Value RecursiveGenericPointerAlias[int] `json:"value"`
}

type GenericPointerAlias[T any] *T
type GenericPointerWrapper[T any] *GenericPointerAlias[T]
type RenamedGenericPointerWrapper[U any] *GenericPointerAlias[U]
type DeepGenericPointerWrapper[T any] *GenericPointerAlias[GenericPointerAlias[GenericPointerAlias[T]]]
type IndirectRecursivePointerAlias *GenericPointerAlias[IndirectRecursivePointerAlias]
type NestedIndirectRecursivePointerAlias *GenericPointerAlias[GenericPointerAlias[NestedIndirectRecursivePointerAlias]]
type ProductiveIndirectPointerAlias *GenericPointerAlias[[]ProductiveIndirectPointerAlias]
type ProductivePointerMapAlias *map[string]ProductivePointerMapAlias
type ProductiveDoublePointerMapAlias **map[string]ProductiveDoublePointerMapAlias
type ProductiveGenericPointerMapAlias[T any] *map[string]ProductiveGenericPointerMapAlias[T]
type ProductiveGenericHiddenMapAlias *GenericPointerAlias[map[string][]ProductiveGenericHiddenMapAlias]
type ProductiveValidatedPointerSliceAlias *[]ProductiveValidatedPointerSliceAlias
type UnsupportedGenericHiddenMapAlias *GenericPointerAlias[map[string]UnsupportedGenericHiddenMapAlias]
type GenericMapAlias[T any] map[string]T
type UnsupportedAppliedGenericMapAlias *GenericMapAlias[UnsupportedAppliedGenericMapAlias]

type IndirectRecursivePointerAliasHolder struct {
	Value IndirectRecursivePointerAlias `json:"value"`
}

type ProductiveValidatedPointerSliceAliasHolder struct {
	Values ProductiveValidatedPointerSliceAlias `json:"values" validate:"len=1"`
}

// RecursiveGeneric demonstrates recursive generic types.
type TreeNode[T any] struct {
	Value    T             `json:"value"`
	Children []TreeNode[T] `json:"children,omitempty"`
}

// Page is a generic paginated response.
type Page[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
}
