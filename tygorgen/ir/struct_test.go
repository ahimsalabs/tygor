package ir

import "testing"

func TestStructDescriptor_Kind(t *testing.T) {
	s := &StructDescriptor{}
	if s.Kind() != KindStruct {
		t.Errorf("StructDescriptor.Kind() = %v, want KindStruct", s.Kind())
	}
}

func TestStructDescriptor_TypeName(t *testing.T) {
	name := GoIdentifier{Name: "User", Package: "github.com/example/api"}
	s := &StructDescriptor{Name: name}

	if s.TypeName() != name {
		t.Errorf("StructDescriptor.TypeName() = %v, want %v", s.TypeName(), name)
	}
}

func TestStructDescriptor_Doc(t *testing.T) {
	doc := Documentation{Summary: "A user", Body: "A user in the system"}
	s := &StructDescriptor{Documentation: doc}

	if s.Doc() != doc {
		t.Errorf("StructDescriptor.Doc() = %v, want %v", s.Doc(), doc)
	}
}

func TestStructDescriptor_Src(t *testing.T) {
	src := Source{File: "user.go", Line: 10, Column: 1}
	s := &StructDescriptor{Source: src}

	if s.Src() != src {
		t.Errorf("StructDescriptor.Src() = %v, want %v", s.Src(), src)
	}
}

func TestStructDescriptor_Full(t *testing.T) {
	s := &StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
		TypeParameters: []TypeParameterDescriptor{
			{ParamName: "T"},
		},
		Fields: []FieldDescriptor{
			{
				Name:     "ID",
				JSONName: "id",
				Type:     Int(64),
			},
			{
				Name:          "Name",
				JSONName:      "name",
				Type:          String(),
				Optional:      true,
				StringEncoded: false,
				ValidateTag:   "required,min=1",
				RawTags:       map[string]string{"json": "name,omitempty"},
			},
		},
		Extends: []GoIdentifier{
			{Name: "BaseModel", Package: "api"},
		},
		Documentation: Documentation{Summary: "User model"},
		Source:        Source{File: "user.go", Line: 5},
	}

	if len(s.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(s.Fields))
	}
	if len(s.Extends) != 1 {
		t.Errorf("expected 1 embedded type, got %d", len(s.Extends))
	}
	if len(s.TypeParameters) != 1 {
		t.Errorf("expected 1 type parameter, got %d", len(s.TypeParameters))
	}
}

func TestFieldDescriptor(t *testing.T) {
	f := FieldDescriptor{
		Name:          "Email",
		Type:          String(),
		JSONName:      "email",
		Optional:      false,
		StringEncoded: false,
		Skip:          false,
		ValidateTag:   "required,email",
		RawTags: map[string]string{
			"json":     "email",
			"validate": "required,email",
		},
		Documentation: Documentation{Summary: "User's email address"},
	}

	if f.Name != "Email" {
		t.Errorf("FieldDescriptor.Name = %q, want Email", f.Name)
	}
	if f.JSONName != "email" {
		t.Errorf("FieldDescriptor.JSONName = %q, want email", f.JSONName)
	}
	if f.Optional {
		t.Error("FieldDescriptor.Optional should be false")
	}
	if f.Skip {
		t.Error("FieldDescriptor.Skip should be false")
	}
	if f.ValidateTag != "required,email" {
		t.Errorf("FieldDescriptor.ValidateTag = %q, want required,email", f.ValidateTag)
	}
}

func TestFieldDescriptor_Skip(t *testing.T) {
	f := FieldDescriptor{
		Name:     "Password",
		JSONName: "-",
		Type:     String(),
		Skip:     true,
	}

	if !f.Skip {
		t.Error("FieldDescriptor.Skip should be true")
	}
}

func TestFieldDescriptor_StringEncoded(t *testing.T) {
	f := FieldDescriptor{
		Name:          "BigNumber",
		JSONName:      "big_number",
		Type:          Int(64),
		StringEncoded: true,
	}

	if !f.StringEncoded {
		t.Error("FieldDescriptor.StringEncoded should be true")
	}
}
