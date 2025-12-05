package ir

import "testing"

func TestAliasDescriptor_Kind(t *testing.T) {
	a := &AliasDescriptor{}
	if a.Kind() != KindAlias {
		t.Errorf("AliasDescriptor.Kind() = %v, want KindAlias", a.Kind())
	}
}

func TestAliasDescriptor_TypeName(t *testing.T) {
	name := GoIdentifier{Name: "UserID", Package: "github.com/example/api"}
	a := &AliasDescriptor{Name: name}

	if a.TypeName() != name {
		t.Errorf("AliasDescriptor.TypeName() = %v, want %v", a.TypeName(), name)
	}
}

func TestAliasDescriptor_Doc(t *testing.T) {
	doc := Documentation{Summary: "A user identifier"}
	a := &AliasDescriptor{Documentation: doc}

	if a.Doc() != doc {
		t.Errorf("AliasDescriptor.Doc() = %v, want %v", a.Doc(), doc)
	}
}

func TestAliasDescriptor_Src(t *testing.T) {
	src := Source{File: "types.go", Line: 15, Column: 1}
	a := &AliasDescriptor{Source: src}

	if a.Src() != src {
		t.Errorf("AliasDescriptor.Src() = %v, want %v", a.Src(), src)
	}
}

func TestAliasDescriptor_SimpleAlias(t *testing.T) {
	// type UserID = string
	a := &AliasDescriptor{
		Name:       GoIdentifier{Name: "UserID", Package: "api"},
		Underlying: String(),
	}

	if a.Underlying.Kind() != KindPrimitive {
		t.Errorf("expected primitive underlying type, got %v", a.Underlying.Kind())
	}
}

func TestAliasDescriptor_GenericAlias(t *testing.T) {
	// type Result[T any] = struct { Data T }
	a := &AliasDescriptor{
		Name: GoIdentifier{Name: "Result", Package: "api"},
		TypeParameters: []TypeParameterDescriptor{
			{ParamName: "T", Constraint: nil}, // any constraint
		},
		Underlying: Ref("ResultStruct", "api"),
	}

	if len(a.TypeParameters) != 1 {
		t.Errorf("expected 1 type parameter, got %d", len(a.TypeParameters))
	}
	if a.TypeParameters[0].ParamName != "T" {
		t.Errorf("expected type parameter T, got %s", a.TypeParameters[0].ParamName)
	}
}

func TestAliasDescriptor_ConstraintInterface(t *testing.T) {
	// type Stringish interface { ~string | ~[]byte }
	a := &AliasDescriptor{
		Name: GoIdentifier{Name: "Stringish", Package: "api"},
		Underlying: Union(
			String(),
			Bytes(),
		),
	}

	if a.Underlying.Kind() != KindUnion {
		t.Errorf("expected union underlying type, got %v", a.Underlying.Kind())
	}
	union := a.Underlying.(*UnionDescriptor)
	if len(union.Types) != 2 {
		t.Errorf("expected 2 union members, got %d", len(union.Types))
	}
}
