package ir

import "testing"

func TestEnumDescriptor_Kind(t *testing.T) {
	e := &EnumDescriptor{}
	if e.Kind() != KindEnum {
		t.Errorf("EnumDescriptor.Kind() = %v, want KindEnum", e.Kind())
	}
}

func TestEnumDescriptor_TypeName(t *testing.T) {
	name := GoIdentifier{Name: "Status", Package: "github.com/example/api"}
	e := &EnumDescriptor{Name: name}

	if e.TypeName() != name {
		t.Errorf("EnumDescriptor.TypeName() = %v, want %v", e.TypeName(), name)
	}
}

func TestEnumDescriptor_Doc(t *testing.T) {
	doc := Documentation{Summary: "Status of a request"}
	e := &EnumDescriptor{Documentation: doc}

	if e.Doc() != doc {
		t.Errorf("EnumDescriptor.Doc() = %v, want %v", e.Doc(), doc)
	}
}

func TestEnumDescriptor_Src(t *testing.T) {
	src := Source{File: "status.go", Line: 8, Column: 1}
	e := &EnumDescriptor{Source: src}

	if e.Src() != src {
		t.Errorf("EnumDescriptor.Src() = %v, want %v", e.Src(), src)
	}
}

func TestEnumDescriptor_StringEnum(t *testing.T) {
	// type Status string
	// const (
	//     StatusPending  Status = "pending"
	//     StatusApproved Status = "approved"
	// )
	e := &EnumDescriptor{
		Name: GoIdentifier{Name: "Status", Package: "api"},
		Members: []EnumMember{
			{Name: "StatusPending", Value: "pending"},
			{Name: "StatusApproved", Value: "approved"},
			{Name: "StatusRejected", Value: "rejected"},
		},
	}

	if len(e.Members) != 3 {
		t.Errorf("expected 3 members, got %d", len(e.Members))
	}

	// Check string values
	for _, m := range e.Members {
		if _, ok := m.Value.(string); !ok {
			t.Errorf("expected string value for %s, got %T", m.Name, m.Value)
		}
	}
}

func TestEnumDescriptor_IntEnum(t *testing.T) {
	// type Priority int
	// const (
	//     PriorityLow Priority = iota
	//     PriorityMedium
	//     PriorityHigh
	// )
	e := &EnumDescriptor{
		Name: GoIdentifier{Name: "Priority", Package: "api"},
		Members: []EnumMember{
			{Name: "PriorityLow", Value: int64(0)},
			{Name: "PriorityMedium", Value: int64(1)},
			{Name: "PriorityHigh", Value: int64(2)},
		},
	}

	if len(e.Members) != 3 {
		t.Errorf("expected 3 members, got %d", len(e.Members))
	}

	// Check int64 values
	for i, m := range e.Members {
		v, ok := m.Value.(int64)
		if !ok {
			t.Errorf("expected int64 value for %s, got %T", m.Name, m.Value)
		}
		if v != int64(i) {
			t.Errorf("expected value %d for %s, got %d", i, m.Name, v)
		}
	}
}

func TestEnumMember_Documentation(t *testing.T) {
	m := EnumMember{
		Name:          "StatusPending",
		Value:         "pending",
		Documentation: Documentation{Summary: "Request is pending review"},
	}

	if m.Documentation.Summary != "Request is pending review" {
		t.Errorf("EnumMember.Documentation.Summary = %q", m.Documentation.Summary)
	}
}
