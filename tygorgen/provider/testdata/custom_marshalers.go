package testdata

import (
	"encoding/json"
)

// CustomJSONType implements json.Marshaler
type CustomJSONType struct {
	value string
}

func (c CustomJSONType) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.value)
}

// CustomTextType implements encoding.TextMarshaler
type CustomTextType struct {
	value string
}

func (c CustomTextType) MarshalText() ([]byte, error) {
	return []byte(c.value), nil
}

// TypeWithCustomMarshaler uses a custom marshaler
type TypeWithCustomMarshaler struct {
	Custom CustomJSONType `json:"custom"`
	Text   CustomTextType `json:"text"`
}

// MapWithTextMarshalerKey uses a TextMarshaler as a map key
type MapWithTextMarshalerKey struct {
	Data map[CustomTextType]string `json:"data"`
}
