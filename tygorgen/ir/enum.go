package ir

// EnumDescriptor represents an enumeration.
// NOTE: Reflection provider cannot produce EnumDescriptor (cannot enumerate const values).
// This descriptor is only available from source-based providers.
type EnumDescriptor struct {
	// Name is the type identifier.
	Name GoIdentifier

	// Members contains all enum variants.
	Members []EnumMember

	// Documentation for this type.
	Documentation Documentation

	// Source location in Go code.
	Source Source
}

// Kind returns KindEnum.
func (d *EnumDescriptor) Kind() DescriptorKind { return KindEnum }

// TypeName returns the enum's name.
func (d *EnumDescriptor) TypeName() GoIdentifier { return d.Name }

// Doc returns the enum's documentation.
func (d *EnumDescriptor) Doc() Documentation { return d.Documentation }

// Src returns the enum's source location.
func (d *EnumDescriptor) Src() Source { return d.Source }

func (*EnumDescriptor) sealed() {}

// EnumMember represents a single enum variant.
type EnumMember struct {
	// Name is the constant name.
	Name string

	// Value is the constant value. Providers convert Go constant values
	// to one of exactly three types: string, int64, or float64.
	// Generators can rely on type assertions to these concrete types.
	Value any

	// Documentation for this member.
	Documentation Documentation
}
