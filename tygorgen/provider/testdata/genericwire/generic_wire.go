package genericwire

import (
	"encoding/json"

	"tygor.dev/tygorgen/provider/testdata"
)

type ConstrainedWireBox[T ~int] struct {
	Value T `json:"value"`
}

type ConstrainedCustomMarshalerPayload struct {
	Box ConstrainedWireBox[testdata.AliasJSONValue] `json:"box"`
}

type UnconstrainedWireBox[T any] struct {
	Value T `json:"value"`
}

type UnconstrainedCustomMarshalerPayload struct {
	Box UnconstrainedWireBox[testdata.AliasJSONValue] `json:"box"`
}

type ExactCustomMarshalerBox[T testdata.AliasJSONValue] struct {
	Value T `json:"value"`
}

type ExactCustomMarshalerPayload struct {
	Box ExactCustomMarshalerBox[testdata.AliasJSONValue] `json:"box"`
}

type JSONMarshalerConstraint interface {
	MarshalJSON() ([]byte, error)
}

type MethodConstrainedWireBox[T JSONMarshalerConstraint] struct {
	Value T `json:"value"`
}

type MethodConstrainedCustomMarshalerPayload struct {
	Box MethodConstrainedWireBox[testdata.AliasJSONValue] `json:"box"`
}

type StringWireBox[T ~string] struct {
	Value T `json:"value"`
}

type ConstrainedJSONNumberPayload struct {
	Box StringWireBox[json.Number] `json:"box"`
}

type DependentWireBox[T any, U interface{ *T | ~int }] struct {
	Value U `json:"value"`
}

type AcceptedDependentCustomMarshalerPayload struct {
	Box DependentWireBox[testdata.AliasJSONValue, testdata.AliasJSONValue] `json:"box"`
}

type RejectedDependentCustomMarshalerPayload struct {
	Box DependentWireBox[int, testdata.AliasJSONValue] `json:"box"`
}

type ForwardedDependentWire[T any] struct {
	Box DependentWireBox[T, int] `json:"box"`
}

type CapturedDependentWire[U any] struct {
	Box DependentWireBox[U, testdata.AliasJSONValue] `json:"box"`
}

type PointerAlias[T any] *T

type RenamedPointerAlias[V any] *V

type AliasScopedCustomMarshalerPayload struct {
	First  DependentWireBox[PointerAlias[testdata.AliasJSONValue], testdata.AliasJSONValue]        `json:"first"`
	Second DependentWireBox[RenamedPointerAlias[testdata.AliasJSONValue], testdata.AliasJSONValue] `json:"second"`
}

type RejectedRecursiveWire[T ~int] struct {
	Value T                                               `json:"value"`
	Next  *RejectedRecursiveWire[testdata.AliasJSONValue] `json:"next"`
}

type CompatibleRecursiveWire[T ~int] struct {
	Value T                             `json:"value"`
	Next  *CompatibleRecursiveWire[int] `json:"next"`
}

type ForwardedRecursiveWire[T ~int] struct {
	Value T                          `json:"value"`
	Next  *ForwardedRecursiveWire[T] `json:"next"`
}
