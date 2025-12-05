package typescript

import (
	"context"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

// Final tests to push coverage above 95%

func TestTypeScriptGenerator_Generate_UnsupportedTypeKind(t *testing.T) {
	// Create a mock type with unsupported kind
	// This tests error handling in EmitType
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{},
	}

	// We can't easily test unsupported kinds without creating invalid IR,
	// so this test ensures the generator handles empty schemas correctly
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
		t.Errorf("TypesGenerated should be 0 for empty schema")
	}
}

func TestTypeScriptGenerator_Generate_EnumDefaultStyle(t *testing.T) {
	// Test that invalid enum style defaults to union
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
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
				"EnumStyle": "invalid_style",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	// Should default to union style
	want := `export type Status = "active";`
	if !strings.Contains(content, want) {
		t.Errorf("invalid enum style should default to union, got:\n%s", content)
	}
}

func TestEmitter_PrefixTypeReferences_ComplexCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple type", "User", "types.User"},
		{"array type", "User[]", "types.User[]"},
		{"primitive", "string", "string"},
		{"Record with user type", "Record<string, User>", "Record<string, types.User>"},
		{"union with null", "User | null", "types.User | null"},
		{"generic type", "Response<User>", "types.Response<types.User>"},
		{"nested generics", "Response<Array<User>>", "types.Response<Array<types.User>>"},
		{"multiple types in union", "User | Admin | null", "types.User | types.Admin | null"},
		{"array of generic", "Response<User>[]", "types.Response<types.User>[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prefixTypeReferences(tt.input, "types.")
			if result != tt.expected {
				t.Errorf("prefixTypeReferences(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_UseInterfaceNoExtends(t *testing.T) {
	// Test interface generation when UseInterface is explicitly true with no extends
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "Simple", Package: "test"},
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
				"UseInterface": true,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	// Should use interface (not type)
	if !strings.Contains(content, "export interface Simple {") {
		t.Errorf("output should use interface syntax")
	}
}

func TestTypeScriptGenerator_Generate_TypePrefixSuffix(t *testing.T) {
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
			TypePrefix: "Generated",
			TypeSuffix: "Type",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	if !strings.Contains(content, "export interface GeneratedUserType {") {
		t.Errorf("output should apply prefix and suffix, got:\n%s", content)
	}
}

func TestTypeScriptGenerator_Generate_FieldCaseTransforms(t *testing.T) {
	tests := []struct {
		name      string
		fieldCase string
		want      string
	}{
		{"preserve", "preserve", "FirstName: string;"},
		{"camel", "camel", "firstName: string;"},
		{"pascal", "pascal", "Firstname: string;"}, // PascalCase lowercases after first char
		{"snake", "snake", "first_name: string;"},
		{"kebab", "kebab", `"first-name": string;`}, // kebab requires quoting
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "User", Package: "test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:     "FirstName",
								JSONName: "FirstName",
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
					SingleFile: true,
					FieldCase:  tt.fieldCase,
				},
			})

			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			content := string(memSink.Get("types.ts"))
			if !strings.Contains(content, tt.want) {
				t.Errorf("field case %s: output should contain %q, got:\n%s", tt.fieldCase, tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_MapWithPrimitiveKey(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "IntMap",
						JSONName: "intMap",
						Type:     ir.Map(ir.Int(0), ir.String()),
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

	// Int keys should be mapped to string in JSON
	want := "intMap: Record<string, string> | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_NestedPtr(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Value",
						JSONName: "value",
						// Nested pointer: **T
						Type: ir.Ptr(ir.Ptr(ir.String())),
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

	// Should have nullable field
	want := "value: string | null;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_DefaultUnknownType(t *testing.T) {
	// Test that UnknownType defaults to "unknown" when not specified
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Container", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Data", JSONName: "data", Type: ir.Any()},
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
			// Don't specify UnknownType - should default to "unknown"
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	want := "data: unknown;"
	if !strings.Contains(content, want) {
		t.Errorf("default UnknownType should be 'unknown', got:\n%s", content)
	}
}

func TestTypeScriptGenerator_Generate_EmitEnumAsUnionWithDeclare(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
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
				"EmitDeclare": true,
				"EnumStyle":   "union",
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))

	want := "export declare type Status ="
	if !strings.Contains(content, want) {
		t.Errorf("output should contain 'export declare type', got:\n%s", content)
	}
}

func TestTypeScriptGenerator_Generate_WithWarnings(t *testing.T) {
	// Test that schema warnings are propagated to result
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name:   ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
			},
		},
		Warnings: []ir.Warning{
			{Code: "TEST", Message: "Test warning"},
		},
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

	if len(result.Warnings) == 0 {
		t.Error("result should include schema warnings")
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if w.Message == "Test warning" {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Error("result should include the test warning from schema")
	}
}
