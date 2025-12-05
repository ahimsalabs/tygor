package ir

import "testing"

func TestDescriptorKind_String(t *testing.T) {
	tests := []struct {
		kind DescriptorKind
		want string
	}{
		{KindStruct, "Struct"},
		{KindAlias, "Alias"},
		{KindEnum, "Enum"},
		{KindPrimitive, "Primitive"},
		{KindArray, "Array"},
		{KindMap, "Map"},
		{KindReference, "Reference"},
		{KindPtr, "Ptr"},
		{KindUnion, "Union"},
		{KindTypeParameter, "TypeParameter"},
		{DescriptorKind(999), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("DescriptorKind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExprBase_ZeroValues(t *testing.T) {
	// Test that exprBase returns zero values
	var base exprBase

	if !base.TypeName().IsZero() {
		t.Error("exprBase.TypeName() should return zero value")
	}
	if !base.Doc().IsZero() {
		t.Error("exprBase.Doc() should return zero value")
	}
	if !base.Src().IsZero() {
		t.Error("exprBase.Src() should return zero value")
	}
}
