package ir

import "testing"

func TestPrimitiveKind_String(t *testing.T) {
	tests := []struct {
		kind PrimitiveKind
		want string
	}{
		{PrimitiveBool, "Bool"},
		{PrimitiveInt, "Int"},
		{PrimitiveUint, "Uint"},
		{PrimitiveFloat, "Float"},
		{PrimitiveString, "String"},
		{PrimitiveBytes, "Bytes"},
		{PrimitiveTime, "Time"},
		{PrimitiveDuration, "Duration"},
		{PrimitiveAny, "Any"},
		{PrimitiveEmpty, "Empty"},
		{PrimitiveKind(999), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("PrimitiveKind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrimitiveDescriptor_Kind(t *testing.T) {
	p := &PrimitiveDescriptor{PrimitiveKind: PrimitiveString}
	if p.Kind() != KindPrimitive {
		t.Errorf("PrimitiveDescriptor.Kind() = %v, want KindPrimitive", p.Kind())
	}
}

func TestPrimitiveDescriptor_ExprBase(t *testing.T) {
	p := &PrimitiveDescriptor{PrimitiveKind: PrimitiveInt, BitSize: 64}

	if !p.TypeName().IsZero() {
		t.Error("PrimitiveDescriptor.TypeName() should return zero value")
	}
	if !p.Doc().IsZero() {
		t.Error("PrimitiveDescriptor.Doc() should return zero value")
	}
	if !p.Src().IsZero() {
		t.Error("PrimitiveDescriptor.Src() should return zero value")
	}
}

func TestPrimitiveConstructors(t *testing.T) {
	tests := []struct {
		name    string
		fn      func() *PrimitiveDescriptor
		want    PrimitiveKind
		bitSize int
	}{
		{"Bool", Bool, PrimitiveBool, 0},
		{"String", String, PrimitiveString, 0},
		{"Bytes", Bytes, PrimitiveBytes, 0},
		{"Time", Time, PrimitiveTime, 0},
		{"Duration", Duration, PrimitiveDuration, 0},
		{"Any", Any, PrimitiveAny, 0},
		{"Empty", Empty, PrimitiveEmpty, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.fn()
			if p.PrimitiveKind != tt.want {
				t.Errorf("got PrimitiveKind %v, want %v", p.PrimitiveKind, tt.want)
			}
			if p.BitSize != tt.bitSize {
				t.Errorf("got BitSize %d, want %d", p.BitSize, tt.bitSize)
			}
		})
	}
}

func TestIntConstructor(t *testing.T) {
	tests := []struct {
		bitSize int
	}{
		{0}, {8}, {16}, {32}, {64},
	}
	for _, tt := range tests {
		p := Int(tt.bitSize)
		if p.PrimitiveKind != PrimitiveInt {
			t.Errorf("Int(%d).PrimitiveKind = %v, want PrimitiveInt", tt.bitSize, p.PrimitiveKind)
		}
		if p.BitSize != tt.bitSize {
			t.Errorf("Int(%d).BitSize = %d, want %d", tt.bitSize, p.BitSize, tt.bitSize)
		}
	}
}

func TestUintConstructor(t *testing.T) {
	tests := []struct {
		bitSize int
	}{
		{0}, {8}, {16}, {32}, {64},
	}
	for _, tt := range tests {
		p := Uint(tt.bitSize)
		if p.PrimitiveKind != PrimitiveUint {
			t.Errorf("Uint(%d).PrimitiveKind = %v, want PrimitiveUint", tt.bitSize, p.PrimitiveKind)
		}
		if p.BitSize != tt.bitSize {
			t.Errorf("Uint(%d).BitSize = %d, want %d", tt.bitSize, p.BitSize, tt.bitSize)
		}
	}
}

func TestFloatConstructor(t *testing.T) {
	tests := []struct {
		bitSize int
	}{
		{32}, {64},
	}
	for _, tt := range tests {
		p := Float(tt.bitSize)
		if p.PrimitiveKind != PrimitiveFloat {
			t.Errorf("Float(%d).PrimitiveKind = %v, want PrimitiveFloat", tt.bitSize, p.PrimitiveKind)
		}
		if p.BitSize != tt.bitSize {
			t.Errorf("Float(%d).BitSize = %d, want %d", tt.bitSize, p.BitSize, tt.bitSize)
		}
	}
}
