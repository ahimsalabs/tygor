package testdata

import (
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
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

type MarshalerBytes = []byte
type MarshalerError = error

type AliasJSONValue int

const AliasJSONValueOne AliasJSONValue = 1

func (AliasJSONValue) MarshalJSON() (MarshalerBytes, error) {
	return MarshalerBytes(`"json-value"`), nil
}

type ValidatedAliasJSON string

func (value ValidatedAliasJSON) MarshalJSON() (MarshalerBytes, error) {
	return json.Marshal(string(value))
}

type CustomMarshalerValidation struct {
	Address ValidatedAliasJSON `json:"address" validate:"email"`
}

type AliasJSONPointer int

func (*AliasJSONPointer) MarshalJSON() (MarshalerBytes, error) {
	return MarshalerBytes(`"json-pointer"`), nil
}

type AliasTextValue int

func (AliasTextValue) MarshalText() (MarshalerBytes, error) {
	return MarshalerBytes("text-value"), nil
}

type AliasTextPointer int

func (*AliasTextPointer) MarshalText() (MarshalerBytes, error) {
	return MarshalerBytes("text-pointer"), nil
}

type V2JSONValue int

func (V2JSONValue) MarshalJSONTo(*jsontext.Encoder) error {
	return errors.ErrUnsupported
}

type AppendedTextValue int

func (AppendedTextValue) AppendText(dst []byte) ([]byte, error) {
	return append(dst, "appended"...), nil
}

type AliasResultTextMapKey struct {
	ID string
}

func (k AliasResultTextMapKey) MarshalText() (MarshalerBytes, MarshalerError) {
	return MarshalerBytes(k.ID), nil
}

type AliasResultMarshalers struct {
	JSONValue    AliasJSONValue    `json:"json_value"`
	JSONPointer  AliasJSONPointer  `json:"json_pointer"`
	TextValue    AliasTextValue    `json:"text_value"`
	TextPointer  *AliasTextPointer `json:"text_pointer"`
	V2JSON       V2JSONValue       `json:"v2_json"`
	AppendedText AppendedTextValue `json:"appended_text"`
}

type AliasResultTextMap struct {
	Values map[AliasResultTextMapKey]int `json:"values"`
}

type MarshaledOctet uint8

func (*MarshaledOctet) MarshalJSON() ([]byte, error) {
	return []byte(`"octet"`), nil
}

type CustomElementByteSlice struct {
	Data []MarshaledOctet `json:"data"`
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
