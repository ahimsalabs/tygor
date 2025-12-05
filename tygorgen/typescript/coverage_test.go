package typescript

import (
	"context"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

// Tests to improve coverage

func TestTypeScriptGenerator_Generate_EnumObject(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Pending", Value: "pending"},
					{Name: "Approved", Value: "approved"},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			Custom: map[string]any{
				"EnumStyle": "object",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	wants := []string{
		"export const Status = {",
		`Pending: "pending"`,
		`Approved: "approved"`,
		"} as const;",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_EnumIntValues(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Priority", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Low", Value: int64(1)},
					{Name: "Medium", Value: int64(2)},
					{Name: "High", Value: int64(3)},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			Custom: map[string]any{
				"EnumStyle": "union",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	want := "export type Priority = 1 | 2 | 3;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_EnumFloatValues(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Rating", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Half", Value: float64(0.5)},
					{Name: "Full", Value: float64(1.0)},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	want := "export type Rating = 0.5 | 1;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_Union(t *testing.T) {
	// Test union types in type parameter constraints
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.AliasDescriptor{
				Name: ir.GoIdentifier{Name: "StringOrInt", Package: "test"},
				Underlying: &ir.UnionDescriptor{
					Types: []ir.TypeDescriptor{
						ir.String(),
						ir.Int(0),
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	want := "export type StringOrInt = string | number /* int */;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_GenericConstraint(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				TypeParameters: []ir.TypeParameterDescriptor{
					{
						ParamName: "T",
						Constraint: &ir.UnionDescriptor{
							Types: []ir.TypeDescriptor{
								ir.String(),
								ir.Int(0),
							},
						},
					},
				},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Value",
						JSONName: "value",
						Type:     &ir.TypeParameterDescriptor{ParamName: "T"},
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	wants := []string{
		"export interface Container<T extends string | number /* int */> {",
		"  value: T;",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_GenericConstraintError(t *testing.T) {
	// Test that invalid constraints propagate errors instead of being silently dropped
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				TypeParameters: []ir.TypeParameterDescriptor{
					{
						ParamName: "T",
						// StructDescriptor is not valid as a constraint type expression
						Constraint: &ir.StructDescriptor{
							Name: ir.GoIdentifier{Name: "Invalid", Package: "test"},
						},
					},
				},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Value",
						JSONName: "value",
						Type:     &ir.TypeParameterDescriptor{ParamName: "T"},
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err == nil {
		t.Fatal("Generate() should have returned an error for invalid constraint")
	}

	if !strings.Contains(err.Error(), "constraint") {
		t.Errorf("error should mention 'constraint', got: %v", err)
	}
}

func TestTypeScriptGenerator_Generate_PropertyNameField(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "FirstName",
						JSONName: "first_name",
						Type:     ir.String(),
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile:         true,
			PropertyNameSource: "field",
			FieldCase:          "camel",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Should use Go field name (FirstName) converted to camel case
	want := "firstName: string;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_MapWithNamedKeyType(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.AliasDescriptor{
				Name:       ir.GoIdentifier{Name: "UserID", Package: "test"},
				Underlying: ir.String(),
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Users",
						JSONName: "users",
						Type: ir.Map(
							ir.Ref("UserID", "test"),
							ir.String(),
						),
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Map with named key type should preserve the key type
	want := "users: Record<UserID, string> | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_UseInterfaceWithExtends(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Base", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Derived", Package: "test"},
				Extends: []ir.GoIdentifier{
					{Name: "Base", Package: "test"},
				},
				Fields: []ir.FieldDescriptor{
					{Name: "Name", JSONName: "name", Type: ir.String()},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			Custom: map[string]any{
				"UseInterface": false,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// With UseInterface=false and Extends, should use type with intersection
	wants := []string{
		"export type Derived = Base & {",
		"  name: string;",
		"};",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_ReadonlyArrays(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Items",
						JSONName: "items",
						Type:     ir.Slice(ir.String()),
						Optional: true,
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			Custom: map[string]any{
				"UseReadonlyArrays": true,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Per §4.9: slices are always nullable, optional is independent
	want := "items?: readonly string[] | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_JSDocMultiline(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Documentation: ir.Documentation{
					Summary: "User represents a user.",
					Body:    "User represents a user.\n\nThis is a multi-line\ndocumentation block.",
				},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile:   true,
			EmitComments: true,
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	wants := []string{
		"/**",
		" * User represents a user.",
		" * This is a multi-line",
		" * documentation block.",
		" */",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_LineEndings(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			LineEnding: "crlf",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := memSink.Get("types.ts")

	// Should contain CRLF line endings
	if !strings.Contains(string(content), "\r\n") {
		t.Errorf("output should contain CRLF line endings")
	}
}

func TestTypeScriptGenerator_Generate_PointerToSlice(t *testing.T) {
	// Test the rare case of *[]T with omitempty (optional + nullable)
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Items",
						JSONName: "items",
						Type:     ir.Ptr(ir.Slice(ir.String())),
						Optional: true, // *[]T with omitempty
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Should be optional and nullable
	want := "items?: string[] | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_PointerToMap(t *testing.T) {
	// Test the rare case of *map[K]V with omitempty
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Metadata",
						JSONName: "metadata",
						Type:     ir.Ptr(ir.Map(ir.String(), ir.String())),
						Optional: true,
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Should be optional and nullable
	want := "metadata?: Record<string, string> | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_EmptySchema(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	result, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.TypesGenerated != 0 {
		t.Errorf("TypesGenerated = %d, want 0", result.TypesGenerated)
	}

	content := string(memSink.Get("types.ts"))
	if !strings.Contains(content, "// Code generated by tygor") {
		t.Errorf("output should contain header comment")
	}
}

func TestTypeScriptGenerator_Generate_NoSink(t *testing.T) {
	schema := &ir.Schema{}
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: nil,
		Config: GeneratorConfig{
			SingleFile: true},
	})

	if err == nil {
		t.Fatal("Generate() should return error when sink is nil")
	}

	if !strings.Contains(err.Error(), "sink is required") {
		t.Errorf("error should mention sink, got: %v", err)
	}
}

func TestTypeScriptGenerator_Generate_SliceOfPointers(t *testing.T) {
	// Test that []*T generates T[] (pointer unwrapped) by default.
	// Go's []*T semantically means "a slice of T values" - the pointer is an
	// implementation detail for efficiency/mutability, not expressing optionality.
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Task", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "TaskList", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Tasks",
						JSONName: "tasks",
						Type:     ir.Slice(ir.Ptr(ir.Ref("Task", "test"))), // []*Task
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Should be Task[] not (Task | null)[]
	if strings.Contains(content, "(Task | null)[]") {
		t.Error("slice of pointers should NOT generate (T | null)[] by default, got nullable elements")
	}

	want := "tasks: Task[] | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_SliceOfPointers_NullableElements(t *testing.T) {
	// Test that []*T generates (T | null)[] when NullableSliceElements=true.
	// This is for users who genuinely want to express nullable array elements.
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Task", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "TaskList", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Tasks",
						JSONName: "tasks",
						Type:     ir.Slice(ir.Ptr(ir.Ref("Task", "test"))), // []*Task
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	_, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile: true,
			Custom: map[string]any{
				"NullableSliceElements": true,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Should be (Task | null)[] when NullableSliceElements is true
	want := "tasks: (Task | null)[] | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}
