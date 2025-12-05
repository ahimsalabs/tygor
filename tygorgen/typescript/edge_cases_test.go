package typescript

import (
	"context"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

// Additional edge case tests to reach 95% coverage

func TestTypeScriptGenerator_Generate_EmitDeclare(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
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
				"EmitDeclare": true,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	if !strings.Contains(content, "export declare interface User") {
		t.Errorf("output should contain 'export declare interface User'")
	}
}

func TestTypeScriptGenerator_Generate_NoExport(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
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
				"EmitExport": false,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	if strings.Contains(content, "export interface User") {
		t.Errorf("output should not contain 'export' keyword")
	}
	if !strings.Contains(content, "interface User") {
		t.Errorf("output should still contain 'interface User'")
	}
}

func TestTypeScriptGenerator_Generate_MultipleExtends(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "Base1", Package: "test"},
				Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
			},
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "Base2", Package: "test"},
				Fields: []ir.FieldDescriptor{{Name: "Name", JSONName: "name", Type: ir.String()}},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Derived", Package: "test"},
				Extends: []ir.GoIdentifier{
					{Name: "Base1", Package: "test"},
					{Name: "Base2", Package: "test"},
				},
				Fields: []ir.FieldDescriptor{{Name: "Email", JSONName: "email", Type: ir.String()}},
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

	// Should use type with multiple intersections
	if !strings.Contains(content, "Base1 & Base2 & {") {
		t.Errorf("output should contain multiple extends")
	}
}

func TestTypeScriptGenerator_Generate_SkipField(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
					{Name: "Internal", JSONName: "-", Type: ir.String(), Skip: true},
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

	if strings.Contains(content, "Internal") {
		t.Errorf("output should not contain skipped field")
	}
}

func TestTypeScriptGenerator_Generate_QuotedFieldName(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Data", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Field", JSONName: "field-name", Type: ir.String()},
					{Name: "Numeric", JSONName: "123", Type: ir.String()},
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

	// Field names with special characters or starting with digits should be quoted
	wants := []string{
		`"field-name": string;`,
		`"123": string;`,
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_AliasWithGenerics(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.AliasDescriptor{
				Name: ir.GoIdentifier{Name: "Maybe", Package: "test"},
				TypeParameters: []ir.TypeParameterDescriptor{
					{ParamName: "T", Constraint: nil},
				},
				Underlying: ir.Ptr(&ir.TypeParameterDescriptor{ParamName: "T"}),
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

	// The underlying type is *T (pointer to type parameter), so it should emit T | null
	// because pointers can be null in JSON serialization
	want := "export type Maybe<T> = (T | null);"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_PrimitiveAllTypes(t *testing.T) {
	// Test all primitive types including edge cases
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "AllPrimitives", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Empty", JSONName: "empty", Type: ir.Empty()},
					{Name: "Any", JSONName: "any", Type: ir.Any()},
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
				"UnknownType": "any",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	wants := []string{
		"empty: Record<string, never>;",
		"any: any;",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_ConstEnum(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
					{Name: "Inactive", Value: "inactive"},
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
				"EnumStyle": "const_enum",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	wants := []string{
		"export const enum Status {",
		`Active = "active"`,
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_EnumWithDocumentation(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{
						Name:  "Active",
						Value: "active",
						Documentation: ir.Documentation{
							Summary: "Active status",
							Body:    "Active status",
						},
					},
					{Name: "Inactive", Value: "inactive"},
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
			Custom: map[string]any{
				"EnumStyle": "enum",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	wants := []string{
		"/** Active status */",
		`Active = "active"`,
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_PropertyNameWithTagPrefix(t *testing.T) {
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
			PropertyNameSource: "tag:json",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	// Should use JSON tag name
	want := "first_name: string;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_ArrayFixedLengthEdgeCase(t *testing.T) {
	// Test exact boundary at 10 (where we switch from tuple to array)
	tests := []struct {
		name    string
		length  int
		isTuple bool
	}{
		{"exactly 10 elements", 10, true},
		{"11 elements", 11, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "Container", Package: "test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:     "Items",
								JSONName: "items",
								Type:     ir.Array(ir.String(), tt.length),
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

			if tt.isTuple {
				// Should be a tuple
				if !strings.Contains(content, "[string,") {
					t.Errorf("output should contain tuple syntax, got:\n%s", content)
				}
			} else {
				// Should be an array
				if !strings.Contains(content, "string[]") {
					t.Errorf("output should contain array syntax, got:\n%s", content)
				}
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_ReferenceToAlias(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.AliasDescriptor{
				Name:       ir.GoIdentifier{Name: "UserID", Package: "test"},
				Underlying: ir.String(),
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "ID",
						JSONName: "id",
						Type:     ir.Ref("UserID", "test"),
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

	wants := []string{
		"export type UserID = string;",
		"id: UserID;",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}
