package typescript

import (
	"bytes"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
)

// TestEmitter_EmitType tests the main EmitType entrypoint with various type descriptors
func TestEmitter_EmitType(t *testing.T) {
	tests := []struct {
		name     string
		typ      ir.TypeDescriptor
		config   GeneratorConfig
		tsConfig TypeScriptConfig
		want     []string
		notWant  []string
	}{
		{
			name: "basic struct with export",
			typ: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EmitExport: true, UseInterface: true},
			want:     []string{"export interface User {", "  id: string;", "}"},
		},
		{
			name: "struct with declare keyword",
			typ: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EmitExport: true, EmitDeclare: true, UseInterface: true},
			want:     []string{"export declare interface User {"},
		},
		{
			name: "struct as type alias (UseInterface=false)",
			typ: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.String()},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EmitExport: true, UseInterface: false},
			want:     []string{"export type User = {", "  id: string;", "};"},
			notWant:  []string{"interface"},
		},
		{
			name: "enum as union",
			typ: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
					{Name: "Inactive", Value: "inactive"},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EnumStyle: "union", EmitExport: true},
			want:     []string{`export type Status = "active" | "inactive";`},
		},
		{
			name: "enum as const enum",
			typ: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
					{Name: "Inactive", Value: "inactive"},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EnumStyle: "const_enum", EmitExport: true},
			want:     []string{"export const enum Status {", `Active = "active"`, `Inactive = "inactive"`},
		},
		{
			name: "enum as object",
			typ: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EnumStyle: "object", EmitExport: true},
			want:     []string{"export const Status = {", `Active: "active"`, "} as const;"},
		},
		{
			name: "alias type",
			typ: &ir.AliasDescriptor{
				Name:       ir.GoIdentifier{Name: "UserID", Package: "test"},
				Underlying: ir.String(),
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{EmitExport: true},
			want:     []string{"export type UserID = string;"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:    schema,
				config:    tt.config,
				tsConfig:  tt.tsConfig,
				indent:    "",
				indentStr: "  ",
			}

			var buf bytes.Buffer
			warnings, err := emitter.EmitType(&buf, tt.typ)
			if err != nil {
				t.Fatalf("EmitType() error = %v", err)
			}

			output := buf.String()
			t.Logf("Generated:\n%s", output)

			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q", want)
				}
			}

			for _, notWant := range tt.notWant {
				if strings.Contains(output, notWant) {
					t.Errorf("output should NOT contain %q", notWant)
				}
			}

			if len(warnings) > 0 {
				t.Logf("Warnings: %v", warnings)
			}
		})
	}
}

// TestEmitter_EmitStruct tests struct emission with various field configurations
func TestEmitter_EmitStruct(t *testing.T) {
	tests := []struct {
		name     string
		struc    *ir.StructDescriptor
		config   GeneratorConfig
		tsConfig TypeScriptConfig
		want     []string
		notWant  []string
	}{
		{
			name: "struct with optional field",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Name", JSONName: "name", Type: ir.String(), Optional: true},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{"name?: string;"},
		},
		{
			name: "struct with nullable pointer field",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Age", JSONName: "age", Type: ir.Ptr(ir.Int(0)), Optional: false},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true, EmitTypeHints: false},
			want:     []string{"age: number | null;"},
		},
		{
			name: "struct with optional nullable pointer field",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Age", JSONName: "age", Type: ir.Ptr(ir.Int(0)), Optional: true},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true, EmitTypeHints: false},
			want:     []string{"age?: number | null;"},
		},
		{
			name: "struct with OptionalType=null",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Name", JSONName: "name", Type: ir.String(), Optional: true},
					{Name: "Age", JSONName: "age", Type: ir.Ptr(ir.Int(0)), Optional: false},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true, OptionalType: "null", EmitTypeHints: false},
			want:     []string{"name: string | null;", "age: number | null;"},
			notWant:  []string{"?"},
		},
		{
			name: "struct with OptionalType=undefined",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Name", JSONName: "name", Type: ir.String(), Optional: true},
					{Name: "Age", JSONName: "age", Type: ir.Ptr(ir.Int(0)), Optional: false},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true, OptionalType: "undefined", EmitTypeHints: false},
			want:     []string{"name?: string;", "age?: number;"},
			notWant:  []string{"| null"},
		},
		{
			name: "struct with skipped field",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Public", JSONName: "public", Type: ir.String()},
					{Name: "Private", JSONName: "-", Type: ir.String(), Skip: true},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{`"public": string;`},
			notWant:  []string{"private", "Private", "public:"},
		},
		{
			name: "struct with embedded/extends types",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Admin", Package: "test"},
				Extends: []ir.GoIdentifier{
					{Name: "User", Package: "test"},
					{Name: "Auditable", Package: "test"},
				},
				Fields: []ir.FieldDescriptor{
					{Name: "Role", JSONName: "role", Type: ir.String()},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{"type Admin = User & Auditable & {", "  role: string;", "};"},
			notWant:  []string{"interface"},
		},
		{
			name: "struct with generic type parameter",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Response", Package: "test"},
				TypeParameters: []ir.TypeParameterDescriptor{
					{ParamName: "T"},
					{ParamName: "E"},
				},
				Fields: []ir.FieldDescriptor{
					{Name: "Data", JSONName: "data", Type: &ir.TypeParameterDescriptor{ParamName: "T"}},
					{Name: "Error", JSONName: "error", Type: &ir.TypeParameterDescriptor{ParamName: "E"}},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{"interface Response<T, E> {", "  data: T;", "  error: E;"},
		},
		{
			name: "struct with field needing quotes",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Data", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Field", JSONName: "my-field", Type: ir.String()},
					{Name: "Type", JSONName: "type", Type: ir.String()}, // reserved word
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{`"my-field": string;`, `"type": string;`},
		},
		{
			name: "struct with double pointer field",
			struc: &ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Data", Package: "test"},
				Fields: []ir.FieldDescriptor{
					{Name: "Value", JSONName: "value", Type: ir.Ptr(ir.Ptr(ir.String()))},
				},
			},
			config:   GeneratorConfig{},
			tsConfig: TypeScriptConfig{UseInterface: true},
			want:     []string{"value: string | null;"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:    schema,
				config:    tt.config,
				tsConfig:  tt.tsConfig,
				indent:    "",
				indentStr: "  ",
			}

			var buf bytes.Buffer
			_, err := emitter.emitStruct(&buf, tt.struc)
			if err != nil {
				t.Fatalf("emitStruct() error = %v", err)
			}

			output := buf.String()
			t.Logf("Generated:\n%s", output)

			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q", want)
				}
			}

			for _, notWant := range tt.notWant {
				if strings.Contains(output, notWant) {
					t.Errorf("output should NOT contain %q", notWant)
				}
			}
		})
	}
}

// TestEmitter_EmitTypeExpr tests type expression emission
func TestEmitter_EmitTypeExpr(t *testing.T) {
	tests := []struct {
		name     string
		typ      ir.TypeDescriptor
		tsConfig TypeScriptConfig
		want     string
	}{
		{
			name:     "primitive bool",
			typ:      ir.Bool(),
			tsConfig: TypeScriptConfig{},
			want:     "boolean",
		},
		{
			name:     "primitive string",
			typ:      ir.String(),
			tsConfig: TypeScriptConfig{},
			want:     "string",
		},
		{
			name:     "primitive int with hint",
			typ:      ir.Int(64),
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "number /* int64 */",
		},
		{
			name:     "primitive int without hint",
			typ:      ir.Int(64),
			tsConfig: TypeScriptConfig{EmitTypeHints: false},
			want:     "number",
		},
		{
			name:     "primitive bytes",
			typ:      ir.Bytes(),
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "string /* base64 */",
		},
		{
			name:     "primitive time",
			typ:      ir.Time(),
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "string /* RFC3339 */",
		},
		{
			name:     "primitive duration",
			typ:      ir.Duration(),
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "number /* nanoseconds */",
		},
		{
			name:     "primitive any as unknown",
			typ:      ir.Any(),
			tsConfig: TypeScriptConfig{UnknownType: "unknown"},
			want:     "unknown",
		},
		{
			name:     "primitive any as any",
			typ:      ir.Any(),
			tsConfig: TypeScriptConfig{UnknownType: "any"},
			want:     "any",
		},
		{
			name:     "primitive empty",
			typ:      ir.Empty(),
			tsConfig: TypeScriptConfig{},
			want:     "Record<string, never>",
		},
		{
			name:     "array of strings",
			typ:      ir.Slice(ir.String()),
			tsConfig: TypeScriptConfig{},
			want:     "string[]",
		},
		{
			name:     "readonly array",
			typ:      ir.Slice(ir.String()),
			tsConfig: TypeScriptConfig{UseReadonlyArrays: true},
			want:     "readonly string[]",
		},
		{
			name:     "fixed array small (tuple)",
			typ:      ir.Array(ir.String(), 3),
			tsConfig: TypeScriptConfig{},
			want:     "[string, string, string]",
		},
		{
			name:     "fixed array large",
			typ:      ir.Array(ir.String(), 100),
			tsConfig: TypeScriptConfig{},
			want:     "string[]",
		},
		{
			name:     "map with string key",
			typ:      ir.Map(ir.String(), ir.Int(0)),
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "Record<string, number /* int */>",
		},
		{
			name:     "map with named key type",
			typ:      ir.Map(ir.Ref("UserID", "test"), ir.String()),
			tsConfig: TypeScriptConfig{},
			want:     "Record<UserID, string>",
		},
		{
			name:     "reference type",
			typ:      ir.Ref("User", "test"),
			tsConfig: TypeScriptConfig{},
			want:     "User",
		},
		{
			name:     "pointer type",
			typ:      ir.Ptr(ir.String()),
			tsConfig: TypeScriptConfig{},
			want:     "(string | null)",
		},
		{
			name:     "nested pointer",
			typ:      ir.Ptr(ir.Ptr(ir.String())),
			tsConfig: TypeScriptConfig{},
			want:     "(string | null)",
		},
		{
			name:     "union type",
			typ:      &ir.UnionDescriptor{Types: []ir.TypeDescriptor{ir.String(), ir.Int(0)}},
			tsConfig: TypeScriptConfig{EmitTypeHints: true},
			want:     "string | number /* int */",
		},
		{
			name:     "type parameter",
			typ:      &ir.TypeParameterDescriptor{ParamName: "T"},
			tsConfig: TypeScriptConfig{},
			want:     "T",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:    schema,
				config:    GeneratorConfig{},
				tsConfig:  tt.tsConfig,
				indent:    "",
				indentStr: "  ",
			}

			got, err := emitter.EmitTypeExpr(tt.typ)
			if err != nil {
				t.Fatalf("EmitTypeExpr() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("EmitTypeExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEmitter_EmitJSDoc tests JSDoc comment emission
func TestEmitter_EmitJSDoc(t *testing.T) {
	tests := []struct {
		name string
		doc  ir.Documentation
		want []string
	}{
		{
			name: "single line doc",
			doc:  ir.Documentation{Body: "Simple description"},
			want: []string{"/** Simple description */"},
		},
		{
			name: "multi-line doc",
			doc: ir.Documentation{
				Body: "First line\nSecond line\nThird line",
			},
			want: []string{"/**", " * First line", " * Second line", " * Third line", " */"},
		},
		{
			name: "doc with deprecation",
			doc: ir.Documentation{
				Body:       "Deprecated function",
				Deprecated: ptrString("Use NewFunc instead"),
			},
			want: []string{"/**", " * Deprecated function", " * @deprecated Use NewFunc instead", " */"},
		},
		{
			name: "doc with empty deprecation",
			doc: ir.Documentation{
				Body:       "Deprecated",
				Deprecated: ptrString(""),
			},
			want: []string{"/**", " * Deprecated", " * @deprecated", " */"},
		},
		{
			name: "zero doc",
			doc:  ir.Documentation{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{
				config:    GeneratorConfig{EmitComments: true},
				indentStr: "  ",
			}

			var buf bytes.Buffer
			emitter.emitJSDoc(&buf, tt.doc)
			output := buf.String()

			if len(tt.want) == 0 {
				if output != "" {
					t.Errorf("emitJSDoc() should produce empty output for zero doc, got %q", output)
				}
				return
			}

			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, got:\n%s", want, output)
				}
			}
		})
	}
}

// TestEmitter_EmitEnum tests enum emission in various styles
func TestEmitter_EmitEnum(t *testing.T) {
	tests := []struct {
		name     string
		enum     *ir.EnumDescriptor
		tsConfig TypeScriptConfig
		want     []string
	}{
		{
			name: "string enum as union",
			enum: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
					{Name: "Inactive", Value: "inactive"},
				},
			},
			tsConfig: TypeScriptConfig{EnumStyle: "union"},
			want:     []string{`type Status = "active" | "inactive";`},
		},
		{
			name: "int enum as union",
			enum: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Priority", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Low", Value: int64(1)},
					{Name: "High", Value: int64(10)},
				},
			},
			tsConfig: TypeScriptConfig{EnumStyle: "union"},
			want:     []string{"type Priority = 1 | 10;"},
		},
		{
			name: "float enum as union",
			enum: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Rate", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Half", Value: float64(0.5)},
					{Name: "Full", Value: float64(1.0)},
				},
			},
			tsConfig: TypeScriptConfig{EnumStyle: "union"},
			want:     []string{"type Rate = 0.5 | 1;"},
		},
		{
			name: "enum as enum",
			enum: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{Name: "Active", Value: "active"},
				},
			},
			tsConfig: TypeScriptConfig{EnumStyle: "enum"},
			want:     []string{"enum Status {", `Active = "active"`},
		},
		{
			name: "enum with documentation",
			enum: &ir.EnumDescriptor{
				Name: ir.GoIdentifier{Name: "Status", Package: "test"},
				Members: []ir.EnumMember{
					{
						Name:  "Active",
						Value: "active",
						Documentation: ir.Documentation{
							Body: "Active status",
						},
					},
				},
			},
			tsConfig: TypeScriptConfig{EnumStyle: "enum"},
			want:     []string{"/** Active status */", `Active = "active"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:    schema,
				config:    GeneratorConfig{EmitComments: true},
				tsConfig:  tt.tsConfig,
				indent:    "",
				indentStr: "  ",
			}

			var buf bytes.Buffer
			_, err := emitter.emitEnum(&buf, tt.enum)
			if err != nil {
				t.Fatalf("emitEnum() error = %v", err)
			}

			output := buf.String()
			t.Logf("Generated:\n%s", output)

			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q", want)
				}
			}
		})
	}
}

// TestEmitter_QualifyTypeName tests package qualification logic
func TestEmitter_QualifyTypeName(t *testing.T) {
	tests := []struct {
		name               string
		id                 ir.GoIdentifier
		mainPackage        string
		stripPackagePrefix string
		want               string
	}{
		{
			name:               "main package type not qualified",
			id:                 ir.GoIdentifier{Name: "User", Package: "example.com/api"},
			mainPackage:        "example.com/api",
			stripPackagePrefix: "example.com/",
			want:               "User",
		},
		{
			name:               "external package qualified",
			id:                 ir.GoIdentifier{Name: "User", Package: "example.com/api/v1"},
			mainPackage:        "example.com/api",
			stripPackagePrefix: "example.com/",
			want:               "api_v1_User",
		},
		{
			name:               "no strip prefix - no qualification",
			id:                 ir.GoIdentifier{Name: "User", Package: "example.com/api/v1"},
			mainPackage:        "example.com/api",
			stripPackagePrefix: "",
			want:               "User",
		},
		{
			name:               "strip prefix not matched - uses full path",
			id:                 ir.GoIdentifier{Name: "User", Package: "other.com/api/v1"},
			mainPackage:        "example.com/api",
			stripPackagePrefix: "example.com/",
			want:               "other_com_api_v1_User",
		},
		{
			name:               "empty package",
			id:                 ir.GoIdentifier{Name: "string", Package: ""},
			mainPackage:        "example.com/api",
			stripPackagePrefix: "example.com/",
			want:               "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: tt.mainPackage, Name: "api"},
			}
			emitter := &Emitter{
				schema: schema,
				config: GeneratorConfig{
					StripPackagePrefix: tt.stripPackagePrefix,
				},
				tsConfig:  TypeScriptConfig{},
				indent:    "",
				indentStr: "  ",
			}

			got := emitter.qualifyTypeName(tt.id)
			if got != tt.want {
				t.Errorf("qualifyTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEmitter_LargeIntegerWarning tests warning generation for large integers
func TestEmitter_LargeIntegerWarning(t *testing.T) {
	tests := []struct {
		name        string
		field       ir.FieldDescriptor
		wantWarning bool
	}{
		{
			name: "int64 without string tag - should warn",
			field: ir.FieldDescriptor{
				Name:          "ID",
				Type:          ir.Int(64),
				StringEncoded: false,
			},
			wantWarning: true,
		},
		{
			name: "uint64 without string tag - should warn",
			field: ir.FieldDescriptor{
				Name:          "ID",
				Type:          ir.Uint(64),
				StringEncoded: false,
			},
			wantWarning: true,
		},
		{
			name: "int64 with string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "ID",
				Type:          ir.Int(64),
				StringEncoded: true,
			},
			wantWarning: false,
		},
		{
			name: "*int64 without string tag - should warn",
			field: ir.FieldDescriptor{
				Name:          "ID",
				Type:          ir.Ptr(ir.Int(64)),
				StringEncoded: false,
			},
			wantWarning: true,
		},
		{
			name: "int32 - no warning",
			field: ir.FieldDescriptor{
				Name:          "Count",
				Type:          ir.Int(32),
				StringEncoded: false,
			},
			wantWarning: false,
		},
		{
			name: "string - no warning",
			field: ir.FieldDescriptor{
				Name: "Name",
				Type: ir.String(),
			},
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{
				schema:    &ir.Schema{},
				config:    GeneratorConfig{},
				tsConfig:  TypeScriptConfig{},
				indent:    "",
				indentStr: "  ",
			}

			warning := emitter.checkLargeIntegerWarning(tt.field, "TestType")

			if tt.wantWarning && warning == nil {
				t.Error("expected warning but got none")
			}
			if !tt.wantWarning && warning != nil {
				t.Errorf("expected no warning but got: %v", warning)
			}

			if warning != nil {
				t.Logf("Warning: %s", warning.Message)
				if !strings.Contains(warning.Message, "precision") {
					t.Error("warning should mention precision")
				}
			}
		})
	}
}

// TestEmitter_TypeMappings tests custom type mappings
func TestEmitter_TypeMappings(t *testing.T) {
	schema := &ir.Schema{
		Package: ir.PackageInfo{Path: "test", Name: "test"},
	}

	emitter := &Emitter{
		schema: schema,
		config: GeneratorConfig{
			TypeMappings: map[string]string{
				"time.Time": "DateTime",
			},
		},
		tsConfig:  TypeScriptConfig{EmitTypeHints: false},
		indent:    "",
		indentStr: "  ",
	}

	// Test that time.Time maps to DateTime
	got, err := emitter.EmitTypeExpr(ir.Time())
	if err != nil {
		t.Fatalf("EmitTypeExpr() error = %v", err)
	}

	if got != "DateTime" {
		t.Errorf("EmitTypeExpr(time.Time) = %q, want %q", got, "DateTime")
	}

	// Test duration mapping
	emitter.config.TypeMappings["time.Duration"] = "Duration"
	got, err = emitter.EmitTypeExpr(ir.Duration())
	if err != nil {
		t.Fatalf("EmitTypeExpr() error = %v", err)
	}

	if got != "Duration" {
		t.Errorf("EmitTypeExpr(time.Duration) = %q, want %q", got, "Duration")
	}
}

// TestEmitter_EmitTypeParameters tests type parameter emission
func TestEmitter_EmitTypeParameters(t *testing.T) {
	tests := []struct {
		name    string
		params  []ir.TypeParameterDescriptor
		want    string
		wantErr bool
	}{
		{
			name:   "no parameters",
			params: []ir.TypeParameterDescriptor{},
			want:   "",
		},
		{
			name: "single unconstrained parameter",
			params: []ir.TypeParameterDescriptor{
				{ParamName: "T"},
			},
			want: "<T>",
		},
		{
			name: "multiple unconstrained parameters",
			params: []ir.TypeParameterDescriptor{
				{ParamName: "T"},
				{ParamName: "E"},
			},
			want: "<T, E>",
		},
		{
			name: "parameter with primitive constraint",
			params: []ir.TypeParameterDescriptor{
				{
					ParamName:  "T",
					Constraint: ir.String(),
				},
			},
			want: "<T extends string>",
		},
		{
			name: "parameter with union constraint",
			params: []ir.TypeParameterDescriptor{
				{
					ParamName: "T",
					Constraint: &ir.UnionDescriptor{
						Types: []ir.TypeDescriptor{ir.String(), ir.Int(0)},
					},
				},
			},
			want: "<T extends string | number /* int */>",
		},
		{
			name: "parameter with reference constraint",
			params: []ir.TypeParameterDescriptor{
				{
					ParamName:  "T",
					Constraint: ir.Ref("Serializable", "test"),
				},
			},
			want: "<T extends Serializable>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:    schema,
				config:    GeneratorConfig{},
				tsConfig:  TypeScriptConfig{EmitTypeHints: true},
				indent:    "",
				indentStr: "  ",
			}

			got, err := emitter.emitTypeParameters(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("emitTypeParameters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("emitTypeParameters() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEmitter_GetPropertyName tests field name resolution
func TestEmitter_GetPropertyName(t *testing.T) {
	tests := []struct {
		name               string
		field              ir.FieldDescriptor
		propertyNameSource string
		fieldCase          string
		want               string
	}{
		{
			name: "default - use JSON tag",
			field: ir.FieldDescriptor{
				Name:     "FirstName",
				JSONName: "first_name",
			},
			propertyNameSource: "",
			fieldCase:          "",
			want:               "first_name",
		},
		{
			name: "tag:json with case transform",
			field: ir.FieldDescriptor{
				Name:     "FirstName",
				JSONName: "first_name",
			},
			propertyNameSource: "tag:json",
			fieldCase:          "camel",
			want:               "firstName",
		},
		{
			name: "field name with case transform",
			field: ir.FieldDescriptor{
				Name:     "FirstName",
				JSONName: "first_name",
			},
			propertyNameSource: "field",
			fieldCase:          "snake",
			want:               "first_name",
		},
		{
			name: "empty JSON name falls back to field name",
			field: ir.FieldDescriptor{
				Name:     "MyField",
				JSONName: "",
			},
			propertyNameSource: "",
			fieldCase:          "camel",
			want:               "myField",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{
				config: GeneratorConfig{
					PropertyNameSource: tt.propertyNameSource,
					FieldCase:          tt.fieldCase,
				},
			}

			got := emitter.getPropertyName(tt.field)
			if got != tt.want {
				t.Errorf("getPropertyName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEmitter_UnsupportedType tests error handling for unsupported types
func TestEmitter_UnsupportedType(t *testing.T) {
	schema := &ir.Schema{
		Package: ir.PackageInfo{Path: "test", Name: "test"},
	}

	emitter := &Emitter{
		schema:    schema,
		config:    GeneratorConfig{},
		tsConfig:  TypeScriptConfig{},
		indent:    "",
		indentStr: "  ",
	}

	// Create an invalid top-level type (using a primitive as top-level)
	var buf bytes.Buffer
	_, err := emitter.EmitType(&buf, ir.String())
	if err == nil {
		t.Fatal("EmitType() should return error for primitive as top-level type")
	}

	if !strings.Contains(err.Error(), "unsupported top-level type kind") {
		t.Errorf("error should mention unsupported type kind, got: %v", err)
	}
}

// TestEmitter_SliceElements tests slice element nullability
func TestEmitter_SliceElements(t *testing.T) {
	tests := []struct {
		name                  string
		sliceType             ir.TypeDescriptor
		nullableSliceElements bool
		want                  string
	}{
		{
			name:                  "slice of pointers - unwrap by default",
			sliceType:             ir.Slice(ir.Ptr(ir.Ref("User", "test"))),
			nullableSliceElements: false,
			want:                  "User[]",
		},
		{
			name:                  "slice of pointers - preserve with config",
			sliceType:             ir.Slice(ir.Ptr(ir.Ref("User", "test"))),
			nullableSliceElements: true,
			want:                  "(User | null)[]",
		},
		{
			name:                  "slice of double pointers - unwrap by default",
			sliceType:             ir.Slice(ir.Ptr(ir.Ptr(ir.String()))),
			nullableSliceElements: false,
			want:                  "string[]",
		},
		{
			name:                  "slice of non-pointers - unaffected",
			sliceType:             ir.Slice(ir.String()),
			nullableSliceElements: false,
			want:                  "string[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &ir.Schema{
				Package: ir.PackageInfo{Path: "test", Name: "test"},
			}
			emitter := &Emitter{
				schema:   schema,
				config:   GeneratorConfig{},
				tsConfig: TypeScriptConfig{NullableSliceElements: tt.nullableSliceElements},
			}

			got, err := emitter.EmitTypeExpr(tt.sliceType)
			if err != nil {
				t.Fatalf("EmitTypeExpr() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("EmitTypeExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ptrString is a helper to create string pointers for tests
func ptrString(s string) *string {
	return &s
}
