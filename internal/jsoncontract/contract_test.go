package jsoncontract

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeJoinOrderAndExplicitFalse(t *testing.T) {
	options := json.JoinOptions(
		json.StringifyNumbers(true),
		json.JoinOptions(json.FormatNilSliceAsNull(true), json.StringifyNumbers(false)),
		json.FormatNilSliceAsNull(false),
		jsontext.WithIndent("  "),
	)
	got := Normalize(options)
	if got.Encode.StringifyNumbers || got.Decode.StringifyNumbers {
		t.Fatal("later StringifyNumbers(false) did not override true")
	}
	if got.Encode.FormatNilSliceAsNull {
		t.Fatal("later FormatNilSliceAsNull(false) did not override true")
	}
	if !got.Encode.Multiline || got.Encode.Indent != "  " {
		t.Fatalf("WithIndent facts = multiline %t, indent %q", got.Encode.Multiline, got.Encode.Indent)
	}
}

func TestNormalizeMaterializesFormattingDefaultsAndOverrides(t *testing.T) {
	multiline := Normalize(jsontext.Multiline(true))
	if !multiline.Encode.Multiline || multiline.Encode.Indent != "\t" || !multiline.Encode.SpaceAfterColon {
		t.Fatalf("Multiline defaults = %#v, want tab indent and colon spacing", multiline.Encode)
	}

	indented := Normalize(json.JoinOptions(jsontext.WithIndent("  "), jsontext.SpaceAfterColon(false)))
	if !indented.Encode.Multiline || indented.Encode.Indent != "  " || indented.Encode.SpaceAfterColon {
		t.Fatalf("explicit formatting override = %#v", indented.Encode)
	}
}

func TestNormalizeInventoriesLegacyCompatibilityOptions(t *testing.T) {
	contract := Normalize(json.JoinOptions(
		jsonv1.FormatDurationAsNano(true),
		jsonv1.UnmarshalArrayFromAnyLength(true),
	))
	want := []string{"FormatDurationAsNano", "UnmarshalArrayFromAnyLength"}
	if got := contract.LegacyOptions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LegacyOptions() = %v, want %v", got, want)
	}

	contract = Normalize(json.JoinOptions(
		jsonv1.DefaultOptionsV1(),
		jsonv1.FormatDurationAsNano(false),
	))
	if contract.Compatibility.FormatDurationAsNano {
		t.Fatal("later explicit false did not override DefaultOptionsV1")
	}
	if len(contract.LegacyOptions()) == 0 {
		t.Fatal("DefaultOptionsV1 was not expanded into compatibility facts")
	}
}

func TestNormalizeAbsentEqualsFalse(t *testing.T) {
	absent := Normalize(nil)
	explicit := Normalize(json.JoinOptions(
		json.StringifyNumbers(false),
		json.FormatNilSliceAsNull(false),
		json.MatchCaseInsensitiveNames(false),
		jsontext.AllowInvalidUTF8(false),
	))
	if absent != explicit {
		t.Fatalf("absent options differ from explicit false:\nabsent:  %#v\nexplicit: %#v", absent, explicit)
	}
}

func TestNormalizeOpaqueCodecReplacementAndClearing(t *testing.T) {
	marshalers := json.MarshalFunc(func(int) ([]byte, error) { return []byte("0"), nil })
	unmarshalers := json.UnmarshalFunc(func([]byte, *int) error { return nil })
	set := Normalize(json.JoinOptions(json.WithMarshalers(marshalers), json.WithUnmarshalers(unmarshalers)))
	if !set.Encode.HasMarshalers || !set.Decode.HasUnmarshalers {
		t.Fatalf("opaque codec presence not recorded: %#v", set)
	}
	if _, err := set.EncodeSemanticKey(); !errors.Is(err, ErrOpaqueCodec) {
		t.Fatalf("EncodeSemanticKey error = %v, want ErrOpaqueCodec", err)
	}

	cleared := Normalize(json.JoinOptions(
		json.WithMarshalers(marshalers), json.WithMarshalers(nil),
		json.WithUnmarshalers(unmarshalers), json.WithUnmarshalers(nil),
	))
	if cleared.Encode.HasMarshalers || cleared.Decode.HasUnmarshalers {
		t.Fatalf("nil codec did not clear earlier codec: %#v", cleared)
	}
	if _, err := cleared.EncodeSemanticKey(); err != nil {
		t.Fatalf("cleared EncodeSemanticKey: %v", err)
	}
}

func TestSemanticKeysIgnoreFormatting(t *testing.T) {
	plain := Normalize(nil)
	formatted := Normalize(json.JoinOptions(
		json.Deterministic(true),
		jsontext.EscapeForHTML(true),
		jsontext.WithIndent("  "),
	))
	want, err := plain.EncodeSemanticKey()
	if err != nil {
		t.Fatal(err)
	}
	got, err := formatted.EncodeSemanticKey()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("formatting changed semantic key:\n got %q\nwant %q", got, want)
	}
}
