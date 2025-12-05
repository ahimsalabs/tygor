package ir

import "testing"

func TestArrayDescriptor_Kind(t *testing.T) {
	a := &ArrayDescriptor{}
	if a.Kind() != KindArray {
		t.Errorf("ArrayDescriptor.Kind() = %v, want KindArray", a.Kind())
	}
}

func TestArrayDescriptor_ExprBase(t *testing.T) {
	a := &ArrayDescriptor{Element: String()}
	if !a.TypeName().IsZero() {
		t.Error("ArrayDescriptor.TypeName() should return zero value")
	}
	if !a.Doc().IsZero() {
		t.Error("ArrayDescriptor.Doc() should return zero value")
	}
	if !a.Src().IsZero() {
		t.Error("ArrayDescriptor.Src() should return zero value")
	}
}

func TestSliceConstructor(t *testing.T) {
	s := Slice(String())
	if s.Element.Kind() != KindPrimitive {
		t.Errorf("Slice element kind = %v, want KindPrimitive", s.Element.Kind())
	}
	if s.Length != 0 {
		t.Errorf("Slice.Length = %d, want 0", s.Length)
	}
}

func TestArrayConstructor(t *testing.T) {
	a := Array(Int(32), 10)
	if a.Element.Kind() != KindPrimitive {
		t.Errorf("Array element kind = %v, want KindPrimitive", a.Element.Kind())
	}
	if a.Length != 10 {
		t.Errorf("Array.Length = %d, want 10", a.Length)
	}
}

func TestMapDescriptor_Kind(t *testing.T) {
	m := &MapDescriptor{}
	if m.Kind() != KindMap {
		t.Errorf("MapDescriptor.Kind() = %v, want KindMap", m.Kind())
	}
}

func TestMapDescriptor_ExprBase(t *testing.T) {
	m := &MapDescriptor{Key: String(), Value: Int(64)}
	if !m.TypeName().IsZero() {
		t.Error("MapDescriptor.TypeName() should return zero value")
	}
}

func TestMapConstructor(t *testing.T) {
	m := Map(String(), Ref("User", "api"))
	if m.Key.Kind() != KindPrimitive {
		t.Errorf("Map key kind = %v, want KindPrimitive", m.Key.Kind())
	}
	if m.Value.Kind() != KindReference {
		t.Errorf("Map value kind = %v, want KindReference", m.Value.Kind())
	}
}

func TestReferenceDescriptor_Kind(t *testing.T) {
	r := &ReferenceDescriptor{}
	if r.Kind() != KindReference {
		t.Errorf("ReferenceDescriptor.Kind() = %v, want KindReference", r.Kind())
	}
}

func TestReferenceDescriptor_ExprBase(t *testing.T) {
	r := &ReferenceDescriptor{Target: GoIdentifier{Name: "Foo", Package: "pkg"}}
	if !r.TypeName().IsZero() {
		t.Error("ReferenceDescriptor.TypeName() should return zero value")
	}
}

func TestRefConstructor(t *testing.T) {
	r := Ref("User", "github.com/example/api")
	if r.Target.Name != "User" {
		t.Errorf("Ref.Target.Name = %q, want User", r.Target.Name)
	}
	if r.Target.Package != "github.com/example/api" {
		t.Errorf("Ref.Target.Package = %q, want github.com/example/api", r.Target.Package)
	}
}

func TestPtrDescriptor_Kind(t *testing.T) {
	p := &PtrDescriptor{}
	if p.Kind() != KindPtr {
		t.Errorf("PtrDescriptor.Kind() = %v, want KindPtr", p.Kind())
	}
}

func TestPtrDescriptor_ExprBase(t *testing.T) {
	p := &PtrDescriptor{Element: String()}
	if !p.TypeName().IsZero() {
		t.Error("PtrDescriptor.TypeName() should return zero value")
	}
}

func TestPtrConstructor(t *testing.T) {
	p := Ptr(String())
	if p.Element.Kind() != KindPrimitive {
		t.Errorf("Ptr element kind = %v, want KindPrimitive", p.Element.Kind())
	}
}

func TestPtrDescriptor_Nested(t *testing.T) {
	// **string
	p := Ptr(Ptr(String()))
	if p.Kind() != KindPtr {
		t.Errorf("outer ptr kind = %v, want KindPtr", p.Kind())
	}
	inner := p.Element.(*PtrDescriptor)
	if inner.Kind() != KindPtr {
		t.Errorf("inner ptr kind = %v, want KindPtr", inner.Kind())
	}
	if inner.Element.Kind() != KindPrimitive {
		t.Errorf("innermost kind = %v, want KindPrimitive", inner.Element.Kind())
	}
}

func TestUnionDescriptor_Kind(t *testing.T) {
	u := &UnionDescriptor{}
	if u.Kind() != KindUnion {
		t.Errorf("UnionDescriptor.Kind() = %v, want KindUnion", u.Kind())
	}
}

func TestUnionDescriptor_ExprBase(t *testing.T) {
	u := &UnionDescriptor{Types: []TypeDescriptor{String()}}
	if !u.TypeName().IsZero() {
		t.Error("UnionDescriptor.TypeName() should return zero value")
	}
}

func TestUnionConstructor(t *testing.T) {
	u := Union(String(), Int(64), Bytes())
	if len(u.Types) != 3 {
		t.Errorf("Union.Types length = %d, want 3", len(u.Types))
	}
}

func TestUnionDescriptor_SingleElement(t *testing.T) {
	// Single-element unions are valid (e.g., [T ~string])
	u := Union(String())
	if len(u.Types) != 1 {
		t.Errorf("Union.Types length = %d, want 1", len(u.Types))
	}
}

func TestTypeParameterDescriptor_Kind(t *testing.T) {
	tp := &TypeParameterDescriptor{}
	if tp.Kind() != KindTypeParameter {
		t.Errorf("TypeParameterDescriptor.Kind() = %v, want KindTypeParameter", tp.Kind())
	}
}

func TestTypeParameterDescriptor_ExprBase(t *testing.T) {
	tp := &TypeParameterDescriptor{ParamName: "T"}
	if !tp.TypeName().IsZero() {
		t.Error("TypeParameterDescriptor.TypeName() should return zero value")
	}
}

func TestTypeParamConstructor(t *testing.T) {
	// Unconstrained: [T any]
	tp := TypeParam("T", nil)
	if tp.ParamName != "T" {
		t.Errorf("TypeParam.ParamName = %q, want T", tp.ParamName)
	}
	if tp.Constraint != nil {
		t.Error("TypeParam.Constraint should be nil for unconstrained")
	}
}

func TestTypeParameterDescriptor_WithConstraint(t *testing.T) {
	// [T ~string | ~int]
	constraint := Union(String(), Int(0))
	tp := TypeParam("T", constraint)

	if tp.ParamName != "T" {
		t.Errorf("TypeParam.ParamName = %q, want T", tp.ParamName)
	}
	if tp.Constraint == nil {
		t.Error("TypeParam.Constraint should not be nil")
	}
	if tp.Constraint.Kind() != KindUnion {
		t.Errorf("TypeParam.Constraint kind = %v, want KindUnion", tp.Constraint.Kind())
	}
}

func TestTypeParameterDescriptor_ReferenceConstraint(t *testing.T) {
	// [T MyConstraint]
	tp := TypeParam("T", Ref("MyConstraint", "api"))

	if tp.Constraint.Kind() != KindReference {
		t.Errorf("TypeParam.Constraint kind = %v, want KindReference", tp.Constraint.Kind())
	}
}
