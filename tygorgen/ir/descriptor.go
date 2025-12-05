package ir

// DescriptorKind identifies the category of a type descriptor.
type DescriptorKind int

const (
	// Named type descriptors (appear in Schema.Types)
	KindStruct DescriptorKind = iota // Object type with named fields (Go struct)
	KindAlias                        // Type alias (type X = Y) or defined type
	KindEnum                         // Enumeration of constants

	// Expression type descriptors (appear nested in fields/types)
	KindPrimitive     // Built-in primitive type
	KindArray         // Ordered collection ([]T or [N]T)
	KindMap           // Key-value mapping (map[K]V)
	KindReference     // Reference to another type
	KindPtr           // Pointer wrapper (*T)
	KindUnion         // Union of types (T1 | T2 | ...)
	KindTypeParameter // Generic type parameter (T, K, V, etc.)
)

// String returns the string representation of the descriptor kind.
func (k DescriptorKind) String() string {
	switch k {
	case KindStruct:
		return "Struct"
	case KindAlias:
		return "Alias"
	case KindEnum:
		return "Enum"
	case KindPrimitive:
		return "Primitive"
	case KindArray:
		return "Array"
	case KindMap:
		return "Map"
	case KindReference:
		return "Reference"
	case KindPtr:
		return "Ptr"
	case KindUnion:
		return "Union"
	case KindTypeParameter:
		return "TypeParameter"
	default:
		return "Unknown"
	}
}

// TypeDescriptor is the base interface for all type descriptors.
type TypeDescriptor interface {
	// Kind returns the descriptor kind for type switching.
	Kind() DescriptorKind

	// TypeName returns the canonical name of this type.
	// Returns zero value for expression types (primitives, arrays, etc).
	TypeName() GoIdentifier

	// Doc returns associated documentation comments.
	// Returns zero value for expression types.
	Doc() Documentation

	// Src returns the original Go source location.
	// Returns zero value for expression types.
	Src() Source

	// Ensure only types in this package can implement TypeDescriptor.
	sealed()
}

// exprBase provides zero-value implementations of TypeDescriptor methods
// for expression type descriptors that don't have names, docs, or source.
type exprBase struct{}

func (exprBase) TypeName() GoIdentifier { return GoIdentifier{} }
func (exprBase) Doc() Documentation     { return Documentation{} }
func (exprBase) Src() Source            { return Source{} }
func (exprBase) sealed()                {}
