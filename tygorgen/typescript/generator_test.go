package typescript

import (
	"context"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

func TestTypeScriptGenerator_Name(t *testing.T) {
	gen := &TypeScriptGenerator{}
	if got := gen.Name(); got != "typescript" {
		t.Errorf("Name() = %q, want %q", got, "typescript")
	}
}

func TestTypeScriptGenerator_Generate_BasicStruct(t *testing.T) {
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "example.com/test",
			Name: "test",
		},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "example.com/test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "ID",
						JSONName: "id",
						Type:     ir.String(),
						Optional: false,
					},
					{
						Name:     "Email",
						JSONName: "email",
						Type:     ir.String(),
						Optional: false,
					},
					{
						Name:     "Age",
						JSONName: "age",
						Type:     ir.Ptr(ir.Int(0)),
						Optional: true,
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	result, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile:      true,
			EmitComments:    false,
			TrailingNewline: true,
			LineEnding:      "lf",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.TypesGenerated != 1 {
		t.Errorf("TypesGenerated = %d, want 1", result.TypesGenerated)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Check for expected output
	// Note: age is *int with omitempty, so it's both optional (?:) and nullable (| null)
	// per §4.9 - optional and nullable are independent properties
	want := []string{
		"export interface User {",
		"  id: string;",
		"  email: string;",
		"  age?: number /* int */ | null;",
		"}",
	}

	for _, w := range want {
		if !strings.Contains(content, w) {
			t.Errorf("output missing expected string %q", w)
		}
	}
}

func TestTypeScriptGenerator_Generate_NullableField(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Response", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Data",
						JSONName: "data",
						Type:     ir.Ptr(ir.String()),
						Optional: false, // pointer without omitempty
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

	// Should have nullable field (not optional)
	if !strings.Contains(content, "data: string | null;") {
		t.Errorf("output should contain 'data: string | null;', got:\n%s", content)
	}
}

func TestTypeScriptGenerator_Generate_SliceField(t *testing.T) {
	// Per §4.9: slices are always nullable (can be nil), and optional is independent
	tests := []struct {
		name     string
		optional bool
		want     string
	}{
		{
			name:     "non-optional slice",
			optional: false,
			want:     "items: string[] | null;",
		},
		{
			name:     "optional slice",
			optional: true,
			want:     "items?: string[] | null;", // both optional AND nullable
		},
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
								Type:     ir.Slice(ir.String()),
								Optional: tt.optional,
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
			if !strings.Contains(content, tt.want) {
				t.Errorf("output should contain %q, got:\n%s", tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_MapField(t *testing.T) {
	// Per §4.9: maps are always nullable (can be nil), and optional is independent
	tests := []struct {
		name     string
		optional bool
		want     string
	}{
		{
			name:     "non-optional map",
			optional: false,
			want:     "metadata: Record<string, string> | null;",
		},
		{
			name:     "optional map",
			optional: true,
			want:     "metadata?: Record<string, string> | null;", // both optional AND nullable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "Container", Package: "test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:     "Metadata",
								JSONName: "metadata",
								Type:     ir.Map(ir.String(), ir.String()),
								Optional: tt.optional,
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
			if !strings.Contains(content, tt.want) {
				t.Errorf("output should contain %q, got:\n%s", tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		irType   ir.TypeDescriptor
		wantType string
	}{
		{"bool", ir.Bool(), "boolean"},
		{"string", ir.String(), "string"},
		{"int", ir.Int(0), "number /* int */"},
		{"int8", ir.Int(8), "number /* int8 */"},
		{"int32", ir.Int(32), "number /* int32 */"},
		{"int64", ir.Int(64), "number /* int64 */"},
		{"uint", ir.Uint(0), "number /* uint */"},
		{"float32", ir.Float(32), "number /* float32 */"},
		{"float64", ir.Float(64), "number /* float64 */"},
		{"bytes", ir.Bytes(), "string /* base64 */"},
		{"time", ir.Time(), "string /* RFC3339 */"},
		{"duration", ir.Duration(), "number /* nanoseconds */"},
		{"any", ir.Any(), "unknown"},
		{"empty", ir.Empty(), "Record<string, never>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "TestType", Package: "test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:     "Field",
								JSONName: "field",
								Type:     tt.irType,
								Optional: false,
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
			want := "field: " + tt.wantType + ";"
			if !strings.Contains(content, want) {
				t.Errorf("output should contain %q, got:\n%s", want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_Alias(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.AliasDescriptor{
				Name:       ir.GoIdentifier{Name: "UserID", Package: "test"},
				Underlying: ir.String(),
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
	want := "export type UserID = string;"
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_EnumUnion(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "StatusPending", Value: "pending"},
					{Name: "StatusApproved", Value: "approved"},
					{Name: "StatusRejected", Value: "rejected"},
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

	want := `export type Status = "pending" | "approved" | "rejected";`
	if !strings.Contains(content, want) {
		t.Errorf("output should contain %q, got:\n%s", want, content)
	}
}

func TestTypeScriptGenerator_Generate_EnumEnum(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "StatusPending", Value: "pending"},
					{Name: "StatusApproved", Value: "approved"},
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
		"export enum Status {",
		`StatusPending = "pending"`,
		`StatusApproved = "approved"`,
		"}",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_ReservedWords(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "interface", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Type",
						JSONName: "type",
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
			SingleFile: true},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Type name should be escaped
	if !strings.Contains(content, "export interface interface_ {") {
		t.Errorf("reserved word 'interface' not escaped in type name")
	}

	// Field name "type" is a reserved word, should be quoted
	if !strings.Contains(content, `"type": string;`) {
		t.Errorf("reserved word 'type' in field name should be quoted")
	}
}

func TestTypeScriptGenerator_Generate_Generics(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Response", Package: "test"},
				TypeParameters: []ir.TypeParameterDescriptor{
					{ParamName: "T", Constraint: nil},
				},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "Data",
						JSONName: "data",
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
		"export interface Response<T> {",
		"  data: T;",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q, got:\n%s", want, content)
		}
	}
}

func TestTypeScriptGenerator_Generate_Extends(t *testing.T) {
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
				"UseInterface": true,
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memSink.Get("types.ts"))
	t.Logf("Generated:\n%s", content)

	// Note: UseInterface with Extends should still use type (intersection)
	// because interfaces can't properly handle some edge cases
	if !strings.Contains(content, "Derived") {
		t.Errorf("output missing Derived type")
	}
}

func TestTypeScriptGenerator_Generate_Manifest(t *testing.T) {
	userType := &ir.StructDescriptor{
		Name:   ir.GoIdentifier{Name: "User", Package: "test"},
		Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
	}

	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{userType},
		Services: []ir.ServiceDescriptor{
			{
				Name: "Users",
				Endpoints: []ir.EndpointDescriptor{
					{
						Name:      "Get",
						FullName:  "Users.Get",
						Primitive: "query",
						Path:      "/Users/Get",
						Request:   ir.Ref("GetRequest", "test"),
						Response:  ir.Ref("User", "test"),
					},
					{
						Name:      "List",
						FullName:  "Users.List",
						Primitive: "query",
						Path:      "/Users/List",
						Request:   nil,
						Response:  ir.Slice(ir.Ref("User", "test")),
					},
				},
			},
		},
	}

	// Add GetRequest type
	schema.Types = append(schema.Types, &ir.StructDescriptor{
		Name:   ir.GoIdentifier{Name: "GetRequest", Package: "test"},
		Fields: []ir.FieldDescriptor{{Name: "ID", JSONName: "id", Type: ir.String()}},
	})

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

	manifestContent := string(memSink.Get("manifest.ts"))
	t.Logf("Generated manifest:\n%s", manifestContent)

	wants := []string{
		"export interface Manifest {",
		`"Users.Get": {`,
		"req: types.GetRequest;",
		"res: types.User;",
		`"Users.List": {`,
		"req: Record<string, never>;",
		"res: types.User[];",
	}

	for _, want := range wants {
		if !strings.Contains(manifestContent, want) {
			t.Errorf("manifest should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_Documentation(t *testing.T) {
	deprecatedMsg := "Use NewUser instead"
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Documentation: ir.Documentation{
					Summary: "User represents a user account.",
					Body:    "User represents a user account.\n\nThis is a detailed description.",
				},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "ID",
						JSONName: "id",
						Type:     ir.String(),
						Documentation: ir.Documentation{
							Summary:    "ID is the unique identifier.",
							Body:       "ID is the unique identifier.",
							Deprecated: &deprecatedMsg,
						},
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
		"User represents a user account",
		"@deprecated Use NewUser instead",
	}

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestTypeScriptGenerator_Generate_Configuration(t *testing.T) {
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "TestType", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "FieldName", JSONName: "field_name", Type: ir.String()},
				},
			},
		},
	}

	tests := []struct {
		name   string
		config GeneratorConfig
		want   string
	}{
		{
			name: "type prefix/suffix",
			config: GeneratorConfig{
				SingleFile: true,
				TypePrefix: "API",
				TypeSuffix: "DTO",
			},
			want: "export interface APITestTypeDTO {",
		},
		{
			name: "field case camel",
			config: GeneratorConfig{
				SingleFile: true,
				FieldCase:  "camel",
			},
			want: "fieldName: string;",
		},
		{
			name: "type case snake",
			config: GeneratorConfig{
				SingleFile: true,
				TypeCase:   "snake",
			},
			want: "export interface test_type {",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memSink := sink.NewMemorySink()
			gen := &TypeScriptGenerator{}

			_, err := gen.Generate(context.Background(), schema, GenerateOptions{
				Sink:   memSink,
				Config: tt.config,
			})

			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			content := string(memSink.Get("types.ts"))
			if !strings.Contains(content, tt.want) {
				t.Errorf("output should contain %q, got:\n%s", tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_FixedArray(t *testing.T) {
	tests := []struct {
		name   string
		length int
		want   string
	}{
		{
			name:   "small fixed array as tuple",
			length: 3,
			want:   "items: [string, string, string] | null;",
		},
		{
			name:   "large fixed array",
			length: 20,
			want:   "items: string[] | null;",
		},
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
			if !strings.Contains(content, tt.want) {
				t.Errorf("output should contain %q, got:\n%s", tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_UnknownType(t *testing.T) {
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

	tests := []struct {
		name        string
		unknownType string
		want        string
	}{
		{"unknown", "unknown", "data: unknown;"},
		{"any", "any", "data: any;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memSink := sink.NewMemorySink()
			gen := &TypeScriptGenerator{}

			_, err := gen.Generate(context.Background(), schema, GenerateOptions{
				Sink: memSink,
				Config: GeneratorConfig{
					SingleFile: true,
					Custom: map[string]any{
						"UnknownType": tt.unknownType,
					},
				},
			})

			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			content := string(memSink.Get("types.ts"))
			if !strings.Contains(content, tt.want) {
				t.Errorf("output should contain %q, got:\n%s", tt.want, content)
			}
		})
	}
}

func TestTypeScriptGenerator_Generate_MultiFile(t *testing.T) {
	// Types from two different packages
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "example.com/main",
			Name: "main",
		},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "example.com/api/v1"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.Int(0)},
					{Name: "Name", JSONName: "name", Type: ir.String()},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "example.com/api/v2"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.Int(0)},
					{Name: "Name", JSONName: "name", Type: ir.String()},
					{Name: "Email", JSONName: "email", Type: ir.String()},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	result, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink:   memSink,
		Config: GeneratorConfig{
			// SingleFile: false (default) - should generate multiple files
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Should have 3 files: types_example_com_api_v1.ts, types_example_com_api_v2.ts, types.ts (barrel)
	// No manifest since schema.Services is nil (types-only generation)
	if len(result.Files) != 3 {
		t.Errorf("expected 3 files (2 packages + barrel), got %d", len(result.Files))
		for _, f := range result.Files {
			t.Logf("  %s", f.Path)
		}
	}

	// Check barrel file exists and re-exports
	barrel := string(memSink.Get("types.ts"))
	if !strings.Contains(barrel, "export * from") {
		t.Errorf("barrel file should contain re-exports, got:\n%s", barrel)
	}

	// Check v1 file
	v1File := string(memSink.Get("types_example_com_api_v1.ts"))
	if !strings.Contains(v1File, "export interface User {") {
		t.Errorf("v1 file should contain User interface, got:\n%s", v1File)
	}
	if strings.Contains(v1File, "email") {
		t.Errorf("v1 User should not have email field")
	}

	// Check v2 file
	v2File := string(memSink.Get("types_example_com_api_v2.ts"))
	if !strings.Contains(v2File, "export interface User {") {
		t.Errorf("v2 file should contain User interface, got:\n%s", v2File)
	}
	if !strings.Contains(v2File, "email") {
		t.Errorf("v2 User should have email field")
	}
}
