package ir

// StructDescriptor represents a structured object type (Go struct).
type StructDescriptor struct {
	// Name is the type identifier.
	Name GoIdentifier

	// TypeParameters contains generic type parameters (source provider only).
	TypeParameters []TypeParameterDescriptor

	// Fields contains all struct fields.
	Fields []FieldDescriptor

	// Extends contains embedded types without json tags (inheritance).
	// These are types whose fields are flattened into this struct's JSON.
	Extends []GoIdentifier

	// Documentation for this type.
	Documentation Documentation

	// Source location in Go code.
	Source Source
}

// Kind returns KindStruct.
func (d *StructDescriptor) Kind() DescriptorKind { return KindStruct }

// TypeName returns the struct's name.
func (d *StructDescriptor) TypeName() GoIdentifier { return d.Name }

// Doc returns the struct's documentation.
func (d *StructDescriptor) Doc() Documentation { return d.Documentation }

// Src returns the struct's source location.
func (d *StructDescriptor) Src() Source { return d.Source }

func (*StructDescriptor) sealed() {}

// FieldDescriptor represents a single field within a struct.
type FieldDescriptor struct {
	// Name is the Go field name.
	Name string

	// Type is the field's type descriptor.
	Type TypeDescriptor

	// JSONName is the serialized property name (from json tag).
	// Falls back to Name if json tag is absent.
	JSONName string

	// Optional indicates the field can be absent from JSON output.
	// This is true when json:",omitempty" or json:",omitzero" is set.
	//
	// For type generation, omitempty and omitzero have identical effects:
	// both make a field optional (field?: T in TypeScript). The behavioral
	// differences (omitempty omits empty collections while omitzero keeps them;
	// omitzero omits zero structs while omitempty keeps them) are runtime
	// concerns that don't affect the generated type signature.
	Optional bool

	// StringEncoded indicates json:",string" was set.
	// When true, the field is encoded as a JSON string on the wire.
	// Only valid for string, integer, floating-point, or boolean types.
	StringEncoded bool

	// Skip indicates json:"-" was set.
	// When true, the field should not appear in generated output.
	Skip bool

	// ValidateTag is the raw value from the `validate` struct tag.
	// Empty string if no validate tag is present.
	//
	// Example: "required,min=3,email" or "omitempty,gt=0,lte=100"
	ValidateTag string

	// RawTags preserves all struct tags for generator-specific handling.
	// Keys are tag names (e.g., "json", "validate", "db").
	RawTags map[string]string

	// Documentation for this field.
	Documentation Documentation
}
