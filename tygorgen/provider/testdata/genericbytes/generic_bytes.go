package genericbytes

// GenericBytes cannot have one reusable JSON contract because uint8
// instantiations encode as base64 while other element types encode as arrays.
type GenericBytes[T ~uint8] []T

type Payload struct {
	Data GenericBytes[uint8] `json:"data"`
}

type AnyList[T any] []T

type Octet uint8

func (Octet) Marker() {}

type MethodOnly interface {
	Marker()
}

type MethodBytes[T MethodOnly] []T

type MethodPayload struct {
	Data MethodBytes[Octet] `json:"data"`
}

type StructPayload[T ~uint8] struct {
	Data []T `json:"data"`
}

type ConcreteStructPayload struct {
	Value StructPayload[uint8] `json:"value"`
}

type NestedPayload[T ~uint8] struct {
	Data map[string][]T `json:"data"`
}

type AnyStruct[T any] struct {
	Data []T `json:"data"`
}

type ConcreteAnyStructPayload struct {
	Value AnyStruct[uint8] `json:"value"`
}

type MixedContainer[E any, S ~[]E | ~int] struct {
	Values S `json:"values"`
}

type MixedResponse struct {
	Value MixedContainer[uint8, []uint8] `json:"value"`
}

type SafeMixedContainer[E ~string, S ~[]E | ~int] struct {
	Values S `json:"values"`
}

type StringOnly interface {
	~uint8 | ~string
	~string
}

type StringList[T StringOnly] []T

type CustomSlice[T any] []T

func (CustomSlice[T]) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}
