package testdata

// Response is a generic response wrapper.
type Response[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error,omitempty"`
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

// MultiConstraint has multiple type parameters with constraints.
type MultiConstraint[K comparable, V any] struct {
	Key   K `json:"key"`
	Value V `json:"value"`
}

// RecursiveGeneric demonstrates recursive generic types.
type TreeNode[T any] struct {
	Value    T             `json:"value"`
	Children []TreeNode[T] `json:"children,omitempty"`
}

// Page is a generic paginated response for testing multi-package instantiation.
type Page[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
}
