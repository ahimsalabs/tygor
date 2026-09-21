package testdata

import "encoding/json"

type ConflictA struct {
	Clash string `json:"clash"`
	A     string `json:"a"`
}

type ConflictB struct {
	Clash string `json:"clash"`
	B     string `json:"b"`
}

type EqualDepthConflict struct {
	ConflictA
	ConflictB
	Own string `json:"own"`
}

type TaggedCandidate struct {
	Tagged string `json:"Same"`
}

type UntaggedCandidate struct {
	Same string
}

type TaggedDominance struct {
	TaggedCandidate
	UntaggedCandidate
}

type PointerBase struct {
	FromPointer int `json:"from_pointer"`
}

type PointerEmbedding struct {
	*PointerBase
}

type OptionsOnlyEmbedding struct {
	ConflictA `json:",omitempty"`
}

type TaggedEmbedding struct {
	ConflictA `json:"nested"`
}

type EmbeddedString string

type EmbeddedInterface interface {
	Marker()
}

type ScalarEmbedding struct {
	EmbeddedString
	EmbeddedInterface
}

type EscapedTag struct {
	Escaped string `json:"foo\x2cbar,omitempty"`
}

type MarshaledEnum int

const MarshaledEnumOne MarshaledEnum = 1

func (*MarshaledEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal("custom")
}

type GenericBox[T any] struct {
	Value T `json:"value"`
}

type GenericUse struct {
	Box GenericBox[string] `json:"box"`
}

type StringEncodedID int64

type StringEncodedStatus int64

const StringEncodedReady StringEncodedStatus = 1

type StringEncodedNamedScalars struct {
	ID     StringEncodedID     `json:"id,string"`
	Status StringEncodedStatus `json:"status,string"`
}
