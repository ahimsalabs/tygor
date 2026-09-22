package jsonv2invalid

import "time"

type Duration struct {
	Value time.Duration `json:"value"`
}

type BoolString struct {
	Value bool `json:"value,string"`
}

type Format struct {
	Value []byte `json:"value,format:base64"`
}

type Embed struct {
	Value struct{ X int } `json:",embed"`
}

type Embedded struct {
	Value string `json:"value"`
}

type OptionsOnlyEmbedding struct {
	Embedded `json:",omitempty"`
}

type EmbeddedString string

type EmbeddedInterface interface {
	Marker()
}

type ScalarEmbedding struct {
	EmbeddedString
	EmbeddedInterface
}

type DirectNameConflict struct {
	First  string `json:"same"`
	Second string `json:"same"`
}
