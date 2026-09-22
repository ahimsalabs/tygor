// Package jsoncontract reduces encoding/json/v2 options to stable,
// serializable facts about their encoding and decoding behavior.
package jsoncontract

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
)

// ErrOpaqueCodec reports that a semantic key cannot describe user-provided
// marshal or unmarshal callbacks.
var ErrOpaqueCodec = errors.New("json contract contains opaque codec callbacks")

// Contract is the normalized, directional view of a set of JSON options.
// Callback values are deliberately represented only by their presence.
type Contract struct {
	Encode        Encode        `json:"encode"`
	Decode        Decode        `json:"decode"`
	Compatibility Compatibility `json:"compatibility"`
}

// Encode contains options that affect marshaling. Formatting facts are
// retained for inspection, but are not part of SemanticKey.
type Encode struct {
	StringifyNumbers          bool   `json:"stringifyNumbers"`
	FormatNilSliceAsNull      bool   `json:"formatNilSliceAsNull"`
	FormatNilMapAsNull        bool   `json:"formatNilMapAsNull"`
	OmitZeroStructFields      bool   `json:"omitZeroStructFields"`
	MatchCaseInsensitiveNames bool   `json:"matchCaseInsensitiveNames"`
	AllowDuplicateNames       bool   `json:"allowDuplicateNames"`
	AllowInvalidUTF8          bool   `json:"allowInvalidUTF8"`
	EscapeForHTML             bool   `json:"escapeForHTML"`
	EscapeForJS               bool   `json:"escapeForJS"`
	PreserveRawStrings        bool   `json:"preserveRawStrings"`
	CanonicalizeRawInts       bool   `json:"canonicalizeRawInts"`
	CanonicalizeRawFloats     bool   `json:"canonicalizeRawFloats"`
	ReorderRawObjects         bool   `json:"reorderRawObjects"`
	Deterministic             bool   `json:"deterministic"`
	SpaceAfterColon           bool   `json:"spaceAfterColon"`
	SpaceAfterComma           bool   `json:"spaceAfterComma"`
	Multiline                 bool   `json:"multiline"`
	Indent                    string `json:"indent"`
	IndentPrefix              string `json:"indentPrefix"`
	HasMarshalers             bool   `json:"hasMarshalers"`
}

// Decode contains options that affect unmarshaling.
type Decode struct {
	StringifyNumbers          bool `json:"stringifyNumbers"`
	MatchCaseInsensitiveNames bool `json:"matchCaseInsensitiveNames"`
	RejectUnknownMembers      bool `json:"rejectUnknownMembers"`
	AllowDuplicateNames       bool `json:"allowDuplicateNames"`
	AllowInvalidUTF8          bool `json:"allowInvalidUTF8"`
	HasUnmarshalers           bool `json:"hasUnmarshalers"`
}

// Compatibility records encoding/json v1 compatibility options. Tygor's
// generated contract is native-v2-only, so callers can identify and reject
// these options rather than silently generating a v2 schema for v1 behavior.
type Compatibility struct {
	CallMethodsWithLegacySemantics  bool `json:"callMethodsWithLegacySemantics"`
	FormatByteArrayAsArray          bool `json:"formatByteArrayAsArray"`
	FormatBytesWithLegacySemantics  bool `json:"formatBytesWithLegacySemantics"`
	FormatDurationAsNano            bool `json:"formatDurationAsNano"`
	MatchCaseSensitiveDelimiter     bool `json:"matchCaseSensitiveDelimiter"`
	MergeWithLegacySemantics        bool `json:"mergeWithLegacySemantics"`
	OmitEmptyWithLegacySemantics    bool `json:"omitEmptyWithLegacySemantics"`
	ParseBytesWithLooseRFC4648      bool `json:"parseBytesWithLooseRFC4648"`
	ParseTimeWithLooseRFC3339       bool `json:"parseTimeWithLooseRFC3339"`
	ReportErrorsWithLegacySemantics bool `json:"reportErrorsWithLegacySemantics"`
	StringifyWithLegacySemantics    bool `json:"stringifyWithLegacySemantics"`
	UnmarshalArrayFromAnyLength     bool `json:"unmarshalArrayFromAnyLength"`
}

// Normalize applies v2 defaults and returns option facts. Joining with the
// defaults makes an absent boolean equivalent to an explicit false value.
func Normalize(options json.Options) Contract {
	options = json.JoinOptions(json.DefaultOptionsV2(), options)
	// Encoder.Options materializes implied formatting defaults such as the tab
	// indent and space-after-colon enabled by Multiline.
	options = jsontext.NewEncoder(io.Discard, options).Options()
	boolOption := func(setter func(bool) json.Options) bool {
		value, _ := json.GetOption(options, setter)
		return value
	}
	stringOption := func(setter func(string) json.Options) string {
		value, _ := json.GetOption(options, setter)
		return value
	}
	marshalers, _ := json.GetOption(options, json.WithMarshalers)
	unmarshalers, _ := json.GetOption(options, json.WithUnmarshalers)

	sharedStringify := boolOption(json.StringifyNumbers)
	sharedCase := boolOption(json.MatchCaseInsensitiveNames)
	sharedDuplicates := boolOption(jsontext.AllowDuplicateNames)
	sharedUTF8 := boolOption(jsontext.AllowInvalidUTF8)
	return Contract{
		Encode: Encode{
			StringifyNumbers:          sharedStringify,
			FormatNilSliceAsNull:      boolOption(json.FormatNilSliceAsNull),
			FormatNilMapAsNull:        boolOption(json.FormatNilMapAsNull),
			OmitZeroStructFields:      boolOption(json.OmitZeroStructFields),
			MatchCaseInsensitiveNames: sharedCase,
			AllowDuplicateNames:       sharedDuplicates,
			AllowInvalidUTF8:          sharedUTF8,
			EscapeForHTML:             boolOption(jsontext.EscapeForHTML),
			EscapeForJS:               boolOption(jsontext.EscapeForJS),
			PreserveRawStrings:        boolOption(jsontext.PreserveRawStrings),
			CanonicalizeRawInts:       boolOption(jsontext.CanonicalizeRawInts),
			CanonicalizeRawFloats:     boolOption(jsontext.CanonicalizeRawFloats),
			ReorderRawObjects:         boolOption(jsontext.ReorderRawObjects),
			Deterministic:             boolOption(json.Deterministic),
			SpaceAfterColon:           boolOption(jsontext.SpaceAfterColon),
			SpaceAfterComma:           boolOption(jsontext.SpaceAfterComma),
			Multiline:                 boolOption(jsontext.Multiline),
			Indent:                    stringOption(jsontext.WithIndent),
			IndentPrefix:              stringOption(jsontext.WithIndentPrefix),
			HasMarshalers:             marshalers != nil,
		},
		Decode: Decode{
			StringifyNumbers:          sharedStringify,
			MatchCaseInsensitiveNames: sharedCase,
			RejectUnknownMembers:      boolOption(json.RejectUnknownMembers),
			AllowDuplicateNames:       sharedDuplicates,
			AllowInvalidUTF8:          sharedUTF8,
			HasUnmarshalers:           unmarshalers != nil,
		},
		Compatibility: Compatibility{
			CallMethodsWithLegacySemantics:  boolOption(jsonv1.CallMethodsWithLegacySemantics),
			FormatByteArrayAsArray:          boolOption(jsonv1.FormatByteArrayAsArray),
			FormatBytesWithLegacySemantics:  boolOption(jsonv1.FormatBytesWithLegacySemantics),
			FormatDurationAsNano:            boolOption(jsonv1.FormatDurationAsNano),
			MatchCaseSensitiveDelimiter:     boolOption(jsonv1.MatchCaseSensitiveDelimiter),
			MergeWithLegacySemantics:        boolOption(jsonv1.MergeWithLegacySemantics),
			OmitEmptyWithLegacySemantics:    boolOption(jsonv1.OmitEmptyWithLegacySemantics),
			ParseBytesWithLooseRFC4648:      boolOption(jsonv1.ParseBytesWithLooseRFC4648),
			ParseTimeWithLooseRFC3339:       boolOption(jsonv1.ParseTimeWithLooseRFC3339),
			ReportErrorsWithLegacySemantics: boolOption(jsonv1.ReportErrorsWithLegacySemantics),
			StringifyWithLegacySemantics:    boolOption(jsonv1.StringifyWithLegacySemantics),
			UnmarshalArrayFromAnyLength:     boolOption(jsonv1.UnmarshalArrayFromAnyLength),
		},
	}
}

// LegacyOptions returns enabled encoding/json v1 compatibility options.
func (c Contract) LegacyOptions() []string {
	compatibility := c.Compatibility
	checks := []struct {
		name    string
		enabled bool
	}{
		{"CallMethodsWithLegacySemantics", compatibility.CallMethodsWithLegacySemantics},
		{"FormatByteArrayAsArray", compatibility.FormatByteArrayAsArray},
		{"FormatBytesWithLegacySemantics", compatibility.FormatBytesWithLegacySemantics},
		{"FormatDurationAsNano", compatibility.FormatDurationAsNano},
		{"MatchCaseSensitiveDelimiter", compatibility.MatchCaseSensitiveDelimiter},
		{"MergeWithLegacySemantics", compatibility.MergeWithLegacySemantics},
		{"OmitEmptyWithLegacySemantics", compatibility.OmitEmptyWithLegacySemantics},
		{"ParseBytesWithLooseRFC4648", compatibility.ParseBytesWithLooseRFC4648},
		{"ParseTimeWithLooseRFC3339", compatibility.ParseTimeWithLooseRFC3339},
		{"ReportErrorsWithLegacySemantics", compatibility.ReportErrorsWithLegacySemantics},
		{"StringifyWithLegacySemantics", compatibility.StringifyWithLegacySemantics},
		{"UnmarshalArrayFromAnyLength", compatibility.UnmarshalArrayFromAnyLength},
	}
	var enabled []string
	for _, check := range checks {
		if check.enabled {
			enabled = append(enabled, check.name)
		}
	}
	return enabled
}

// EncodeSemanticKey returns a stable key containing only built-in options
// that can alter the encoded schema. It rejects opaque custom marshalers.
func (c Contract) EncodeSemanticKey() (string, error) {
	if c.Encode.HasMarshalers {
		return "", fmt.Errorf("encode: %w", ErrOpaqueCodec)
	}
	e := c.Encode
	return fmt.Sprintf("json/v2/encode:v1;stringify=%t;nilSliceNull=%t;nilMapNull=%t;omitZero=%t;caseInsensitive=%t",
		e.StringifyNumbers, e.FormatNilSliceAsNull, e.FormatNilMapAsNull,
		e.OmitZeroStructFields, e.MatchCaseInsensitiveNames), nil
}

// DecodeSemanticKey returns a stable key containing only built-in options
// that alter accepted JSON values. It rejects opaque custom unmarshalers.
func (c Contract) DecodeSemanticKey() (string, error) {
	if c.Decode.HasUnmarshalers {
		return "", fmt.Errorf("decode: %w", ErrOpaqueCodec)
	}
	d := c.Decode
	return fmt.Sprintf("json/v2/decode:v1;stringify=%t;caseInsensitive=%t;rejectUnknown=%t;allowDuplicates=%t;allowInvalidUTF8=%t",
		d.StringifyNumbers, d.MatchCaseInsensitiveNames, d.RejectUnknownMembers,
		d.AllowDuplicateNames, d.AllowInvalidUTF8), nil
}
