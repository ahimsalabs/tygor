package genericstring

type GenericStringEncoded[T ~int] struct {
	Value T `json:"value,string"`
}

type GenericDoublePointerStringEncoded[T ~int] struct {
	Value **T `json:"value,string"`
}

type GenericPointerAlias[T ~int] = *T

type GenericPointerAliasStringEncoded[T ~int] struct {
	Value GenericPointerAlias[T] `json:"value,string"`
}
