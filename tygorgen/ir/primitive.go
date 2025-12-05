package ir

// PrimitiveKind identifies the category of a primitive type.
type PrimitiveKind int

const (
	PrimitiveBool  PrimitiveKind = iota
	PrimitiveInt                 // Signed integer (see BitSize)
	PrimitiveUint                // Unsigned integer (see BitSize)
	PrimitiveFloat               // Floating point (see BitSize)
	PrimitiveString
	PrimitiveBytes    // []byte (base64-encoded in JSON)
	PrimitiveTime     // time.Time (RFC 3339 string in JSON)
	PrimitiveDuration // time.Duration (nanoseconds as int64 in JSON)
	PrimitiveAny      // interface{} / any
	PrimitiveEmpty    // struct{} (empty struct, serializes as {})
)

// String returns the string representation of the primitive kind.
func (k PrimitiveKind) String() string {
	switch k {
	case PrimitiveBool:
		return "Bool"
	case PrimitiveInt:
		return "Int"
	case PrimitiveUint:
		return "Uint"
	case PrimitiveFloat:
		return "Float"
	case PrimitiveString:
		return "String"
	case PrimitiveBytes:
		return "Bytes"
	case PrimitiveTime:
		return "Time"
	case PrimitiveDuration:
		return "Duration"
	case PrimitiveAny:
		return "Any"
	case PrimitiveEmpty:
		return "Empty"
	default:
		return "Unknown"
	}
}

// PrimitiveDescriptor represents a built-in primitive type.
type PrimitiveDescriptor struct {
	exprBase
	PrimitiveKind PrimitiveKind

	// BitSize specifies the size for numeric types (PrimitiveInt, PrimitiveUint, PrimitiveFloat).
	// Valid values:
	// - 0: Platform-dependent size (Go's `int`, `uint`)
	// - 8, 16, 32, 64: Explicit bit width
	//
	// Ignored for non-numeric primitive kinds.
	//
	// Generators targeting languages with rich numeric types (Rust, Zod) SHOULD use BitSize
	// to emit precise types or validation. Generators targeting languages with single numeric
	// types (TypeScript, Python) MAY ignore BitSize.
	BitSize int
}

// Kind returns KindPrimitive.
func (d *PrimitiveDescriptor) Kind() DescriptorKind { return KindPrimitive }

// Convenience constructors for common primitives.

// Bool returns a PrimitiveDescriptor for bool.
func Bool() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveBool}
}

// String returns a PrimitiveDescriptor for string.
func String() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveString}
}

// Int returns a PrimitiveDescriptor for int with the given bit size.
// Use 0 for platform-dependent int.
func Int(bitSize int) *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveInt, BitSize: bitSize}
}

// Uint returns a PrimitiveDescriptor for uint with the given bit size.
// Use 0 for platform-dependent uint.
func Uint(bitSize int) *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveUint, BitSize: bitSize}
}

// Float returns a PrimitiveDescriptor for float with the given bit size.
func Float(bitSize int) *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveFloat, BitSize: bitSize}
}

// Bytes returns a PrimitiveDescriptor for []byte.
func Bytes() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveBytes}
}

// Time returns a PrimitiveDescriptor for time.Time.
func Time() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveTime}
}

// Duration returns a PrimitiveDescriptor for time.Duration.
func Duration() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveDuration}
}

// Any returns a PrimitiveDescriptor for any/interface{}.
func Any() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveAny}
}

// Empty returns a PrimitiveDescriptor for struct{}.
func Empty() *PrimitiveDescriptor {
	return &PrimitiveDescriptor{PrimitiveKind: PrimitiveEmpty}
}
