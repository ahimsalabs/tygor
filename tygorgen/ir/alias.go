package ir

// AliasDescriptor represents a type alias or defined type.
//
// Note: Constraint interfaces (e.g., `type Stringish interface { ~string | ~int }`)
// are emitted as AliasDescriptor with Underlying set to a UnionDescriptor. These
// appear in Schema.Types for reference resolution when type parameters use them
// as constraints. Generators typically emit these as type aliases in the target
// language. Since Go forbids using constraint-only interfaces as variable types,
// these are NOT intended to be instantiable types—they exist solely for type
// parameter constraints.
type AliasDescriptor struct {
	// Name is the type identifier.
	Name GoIdentifier

	// TypeParameters contains generic type parameters (source provider only).
	TypeParameters []TypeParameterDescriptor

	// Underlying is the aliased type.
	Underlying TypeDescriptor

	// Documentation for this type.
	Documentation Documentation

	// Source location in Go code.
	Source Source
}

// Kind returns KindAlias.
func (d *AliasDescriptor) Kind() DescriptorKind { return KindAlias }

// TypeName returns the alias's name.
func (d *AliasDescriptor) TypeName() GoIdentifier { return d.Name }

// Doc returns the alias's documentation.
func (d *AliasDescriptor) Doc() Documentation { return d.Documentation }

// Src returns the alias's source location.
func (d *AliasDescriptor) Src() Source { return d.Source }

func (*AliasDescriptor) sealed() {}
