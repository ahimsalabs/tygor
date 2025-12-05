package typescript

import (
	"context"
	"strings"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

func TestQualifyTypeName(t *testing.T) {
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "github.com/myorg/myrepo/api",
			Name: "api",
		},
	}

	tests := []struct {
		name               string
		stripPackagePrefix string
		id                 ir.GoIdentifier
		want               string
	}{
		{
			name:               "main package not qualified",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api"},
			want:               "User",
		},
		{
			name:               "empty package not qualified",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "User", Package: ""},
			want:               "User",
		},
		{
			name:               "sibling package qualified with stripped prefix",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/models"},
			want:               "models_User",
		},
		{
			name:               "nested package qualified",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v1"},
			want:               "api_v1_User",
		},
		{
			name:               "external package uses full path",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "Config", Package: "github.com/other/lib"},
			want:               "github_com_other_lib_Config",
		},
		{
			name:               "no prefix configured - no qualification",
			stripPackagePrefix: "",
			id:                 ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/models"},
			want:               "User",
		},
		{
			name:               "same package name different path - distinguished",
			stripPackagePrefix: "github.com/myorg/myrepo/",
			id:                 ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/v1/types"},
			want:               "v1_types_User",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{
				schema: schema,
				config: GeneratorConfig{
					StripPackagePrefix: tt.stripPackagePrefix,
				},
			}
			got := emitter.qualifyTypeName(tt.id)
			if got != tt.want {
				t.Errorf("qualifyTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQualifyTypeName_SameNameDifferentPackages(t *testing.T) {
	// Simulate two packages with the same type name "User"
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "github.com/myorg/myrepo/api",
			Name: "api",
		},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v1"},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v2"},
			},
		},
	}

	emitter := &Emitter{
		schema: schema,
		config: GeneratorConfig{
			StripPackagePrefix: "github.com/myorg/myrepo/",
		},
	}

	v1Name := emitter.qualifyTypeName(ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v1"})
	v2Name := emitter.qualifyTypeName(ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v2"})

	if v1Name == v2Name {
		t.Errorf("same-name types from different packages should have different qualified names: v1=%q, v2=%q", v1Name, v2Name)
	}

	if v1Name != "api_v1_User" {
		t.Errorf("v1 User should be 'api_v1_User', got %q", v1Name)
	}
	if v2Name != "api_v2_User" {
		t.Errorf("v2 User should be 'api_v2_User', got %q", v2Name)
	}
}

func TestGenerator_PackageQualification_Integration(t *testing.T) {
	// Create a schema with types from multiple packages
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "github.com/myorg/myrepo/api",
			Name: "api",
		},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "Request", Package: "github.com/myorg/myrepo/api"},
				Fields: []ir.FieldDescriptor{
					{
						Name:     "V1User",
						JSONName: "v1_user",
						Type:     ir.Ref("User", "github.com/myorg/myrepo/api/v1"),
					},
					{
						Name:     "V2User",
						JSONName: "v2_user",
						Type:     ir.Ref("User", "github.com/myorg/myrepo/api/v2"),
					},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v1"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.Int(64)},
				},
			},
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api/v2"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.Int(64)},
					{Name: "Email", JSONName: "email", Type: ir.String()},
				},
			},
		},
	}

	gen := &TypeScriptGenerator{}
	memorySink := sink.NewMemorySink()

	opts := GenerateOptions{
		Sink: memorySink,
		Config: GeneratorConfig{
			SingleFile:         true,
			StripPackagePrefix: "github.com/myorg/myrepo/",
		},
	}

	result, err := gen.Generate(context.Background(), schema, opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.TypesGenerated != 3 {
		t.Errorf("TypesGenerated = %d, want 3", result.TypesGenerated)
	}

	content := string(memorySink.Get("types.ts"))

	// Check that main package type is not qualified
	if !strings.Contains(content, "export interface Request {") {
		t.Error("main package type 'Request' should not be qualified")
	}

	// Check that external package types are qualified
	if !strings.Contains(content, "export interface api_v1_User {") {
		t.Errorf("v1 User should be qualified as 'api_v1_User'\n%s", content)
	}
	if !strings.Contains(content, "export interface api_v2_User {") {
		t.Errorf("v2 User should be qualified as 'api_v2_User'\n%s", content)
	}

	// Check that references use qualified names
	if !strings.Contains(content, "v1_user: api_v1_User") {
		t.Errorf("reference to v1 User should use qualified name\n%s", content)
	}
	if !strings.Contains(content, "v2_user: api_v2_User") {
		t.Errorf("reference to v2 User should use qualified name\n%s", content)
	}
}

func TestGenerator_PackageQualification_BackwardCompat(t *testing.T) {
	// Without StripPackagePrefix, types should not be qualified (backward compat)
	schema := &ir.Schema{
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "github.com/myorg/myrepo/api"},
				Fields: []ir.FieldDescriptor{
					{Name: "ID", JSONName: "id", Type: ir.Int(64)},
				},
			},
		},
	}

	gen := &TypeScriptGenerator{}
	memorySink := sink.NewMemorySink()

	opts := GenerateOptions{
		Sink: memorySink,
		Config: GeneratorConfig{
			SingleFile: true,
			// No StripPackagePrefix
		},
	}

	_, err := gen.Generate(context.Background(), schema, opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content := string(memorySink.Get("types.ts"))

	// Type should NOT be qualified
	if !strings.Contains(content, "export interface User {") {
		t.Errorf("without StripPackagePrefix, types should not be qualified\n%s", content)
	}
	if strings.Contains(content, "github_com") {
		t.Errorf("without StripPackagePrefix, types should not be qualified with package path\n%s", content)
	}
}
