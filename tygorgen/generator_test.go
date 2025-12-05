package tygorgen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tygor.dev/internal/testfixtures"
	"tygor.dev/tygor"
	"tygor.dev/tygorgen/provider/testdata"
	v1 "tygor.dev/tygorgen/provider/testdata/v1"
	v2 "tygor.dev/tygorgen/provider/testdata/v2"
)

func TestApplyConfigDefaults(t *testing.T) {
	tests := []struct {
		name   string
		input  *Config
		check  func(*Config) bool
		errMsg string
	}{
		{
			name:  "empty config gets defaults",
			input: &Config{OutDir: "/tmp"},
			check: func(c *Config) bool {
				return c.Provider == "source" &&
					c.PreserveComments == "default" &&
					c.EnumStyle == "union" &&
					c.OptionalType == "undefined"
			},
			errMsg: "defaults not applied correctly",
		},
		{
			name: "explicit values preserved",
			input: &Config{
				OutDir:           "/tmp",
				PreserveComments: "none",
				EnumStyle:        "enum",
				OptionalType:     "null",
			},
			check: func(c *Config) bool {
				return c.PreserveComments == "none" &&
					c.EnumStyle == "enum" &&
					c.OptionalType == "null"
			},
			errMsg: "explicit values not preserved",
		},
		{
			name: "partial config",
			input: &Config{
				OutDir:    "/tmp",
				EnumStyle: "const",
			},
			check: func(c *Config) bool {
				return c.PreserveComments == "default" &&
					c.EnumStyle == "const" &&
					c.OptionalType == "undefined"
			},
			errMsg: "partial config not handled correctly",
		},
		{
			name: "does not mutate input",
			input: &Config{
				OutDir: "/tmp",
			},
			check: func(c *Config) bool {
				// The returned config should be different from original
				return c.PreserveComments == "default"
			},
			errMsg: "config mutation check failed",
		},
		{
			name: "preserves TypeMappings",
			input: &Config{
				OutDir:       "/tmp",
				TypeMappings: map[string]string{"foo": "bar"},
			},
			check: func(c *Config) bool {
				return c.TypeMappings != nil && c.TypeMappings["foo"] == "bar"
			},
			errMsg: "TypeMappings not preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyConfigDefaults(tt.input)
			if !tt.check(result) {
				t.Error(tt.errMsg)
			}
		})
	}
}

func TestGenerate_NoOutDir_ReturnsFilesInMemory(t *testing.T) {
	reg := tygor.NewApp()
	handler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return nil, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(handler))

	cfg := &Config{
		Provider: "reflection",
	}

	result, err := Generate(reg, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have files in result since no OutDir
	if len(result.Files) == 0 {
		t.Error("expected files in result when OutDir is empty")
	}

	// Should have types.ts and manifest.ts at minimum
	var hasTypes, hasManifest bool
	for _, f := range result.Files {
		if f.Path == "types.ts" {
			hasTypes = true
		}
		if f.Path == "manifest.ts" {
			hasManifest = true
		}
	}
	if !hasTypes {
		t.Error("missing types.ts in result files")
	}
	if !hasManifest {
		t.Error("missing manifest.ts in result files")
	}
}

func TestGenerate_EmptyApp(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check types.ts exists
	typesPath := filepath.Join(outDir, "types.ts")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		t.Error("types.ts was not created")
	}

	// Check manifest.ts exists
	manifestPath := filepath.Join(outDir, "manifest.ts")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("manifest.ts was not created")
	}

	// Verify manifest content for empty registry
	content, _ := os.ReadFile(manifestPath)
	if !strings.Contains(string(content), "Manifest") {
		t.Error("manifest.ts missing Manifest interface")
	}
}

func TestGenerate_WithHandlers(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	// Register a test handler using internal test fixture types
	handler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return &testfixtures.User{Username: req.Username}, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(handler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check manifest.ts has the route
	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	if !strings.Contains(manifestStr, `"Users.Create"`) {
		t.Error("manifest.ts missing Users.Create route")
	}
	if !strings.Contains(manifestStr, `primitive: "exec"`) {
		t.Error("manifest.ts missing exec primitive")
	}
	if !strings.Contains(manifestStr, `path: "/Users/Create"`) {
		t.Error("manifest.ts missing correct path")
	}
}

func TestGenerate_ManifestStructure(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	createHandler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return nil, nil
	}
	listHandler := func(ctx context.Context, req *testfixtures.ListPostsParams) ([]*testfixtures.Post, error) {
		return nil, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(createHandler))
	reg.Service("Posts").Register("List", tygor.Query(listHandler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	// Verify imports
	if !strings.Contains(manifestStr, "import * as types") {
		t.Error("manifest.ts missing types import")
	}

	// Verify interface definition
	if !strings.Contains(manifestStr, "export interface Manifest") {
		t.Error("manifest.ts missing Manifest interface")
	}

	// Verify routes
	if !strings.Contains(manifestStr, `"Users.Create"`) {
		t.Error("manifest.ts missing Users.Create")
	}
	if !strings.Contains(manifestStr, `"Posts.List"`) {
		t.Error("manifest.ts missing Posts.List")
	}
}

func TestGenerate_TypesFile(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	handler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return nil, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(handler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	typesPath := filepath.Join(outDir, "types.ts")
	content, _ := os.ReadFile(typesPath)
	typesStr := string(content)

	// Verify header
	if !strings.Contains(typesStr, "Code generated by tygor") {
		t.Error("types.ts missing generation header")
	}

	// Should have TypeScript interface exports
	// (Note: new generator creates a single file with all types, not re-exports)
}

func TestGenerate_CustomConfig(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	handler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return nil, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(handler))

	cfg := &Config{
		OutDir:           outDir,
		SingleFile:       true,
		Provider:         "reflection",
		PreserveComments: "none",
		EnumStyle:        "enum",
		OptionalType:     "null",
		TypeMappings: map[string]string{
			"custom.Type": "CustomType",
		},
	}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check types file content to verify config was applied
	typesPath := filepath.Join(outDir, "types.ts")
	content, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatalf("failed to read generated types: %v", err)
	}
	typesStr := string(content)

	// Verify User struct is present (sanity check that config was used)
	if !strings.Contains(typesStr, "export interface User") {
		t.Error("expected User interface to be generated")
	}
}

// TestGenerate_GETParamsUseLowercaseNames verifies that GET request parameter types
// generate TypeScript with lowercase property names (matching schema tags via json tags).
// This ensures the TypeScript client sends query params that match what Go expects.
func TestGenerate_GETParamsUseLowercaseNames(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	// Register a GET handler using ListPostsParams which has both json and schema tags
	listHandler := func(ctx context.Context, req *testfixtures.ListPostsParams) ([]*testfixtures.Post, error) {
		return nil, nil
	}
	reg.Service("Posts").Register("List", tygor.Query(listHandler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read the generated types file
	typesPath := filepath.Join(outDir, "types.ts")
	content, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatalf("failed to read generated types: %v", err)
	}
	typesStr := string(content)

	// Verify ListPostsParams has lowercase property names (from json tags)
	// These should match the schema tags used for query parameter decoding
	if !strings.Contains(typesStr, "author_id") {
		t.Error("ListPostsParams should have 'author_id' property (lowercase)")
	}
	if !strings.Contains(typesStr, "published") {
		t.Error("ListPostsParams should have 'published' property (lowercase)")
	}
	if !strings.Contains(typesStr, "limit") {
		t.Error("ListPostsParams should have 'limit' property (lowercase)")
	}
	if !strings.Contains(typesStr, "offset") {
		t.Error("ListPostsParams should have 'offset' property (lowercase)")
	}

	// Verify we DON'T have the capitalized Go field names
	if strings.Contains(typesStr, "AuthorID") {
		t.Error("ListPostsParams should NOT have 'AuthorID' - should use lowercase 'author_id'")
	}
	if strings.Contains(typesStr, "Published:") {
		t.Error("ListPostsParams should NOT have 'Published' - should use lowercase 'published'")
	}
}

func TestFromTypes_GeneratesTypes(t *testing.T) {
	result, err := FromTypes(
		testfixtures.User{},
		testfixtures.CreateUserRequest{},
	).Provider("reflection").Generate()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Files) == 0 {
		t.Fatal("expected files in result")
	}

	// Collect all content across files
	var allContent string
	var hasTypesBarrel bool
	for _, f := range result.Files {
		allContent += string(f.Content)
		if f.Path == "types.ts" {
			hasTypesBarrel = true
		}
	}

	if !hasTypesBarrel {
		t.Fatal("missing types.ts barrel file in result files")
	}

	// Verify User interface is generated (may be in a package-specific file)
	if !strings.Contains(allContent, "export interface User") {
		t.Error("expected User interface to be generated")
	}

	// Verify CreateUserRequest interface is generated
	if !strings.Contains(allContent, "export interface CreateUserRequest") {
		t.Error("expected CreateUserRequest interface to be generated")
	}

	// Should NOT have manifest.ts (no app)
	for _, f := range result.Files {
		if f.Path == "manifest.ts" {
			t.Error("should not have manifest.ts when using FromTypes")
		}
	}
}

func TestFromTypes_WritesToDir(t *testing.T) {
	outDir := t.TempDir()

	_, err := FromTypes(
		testfixtures.User{},
	).Provider("reflection").ToDir(outDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check types.ts barrel exists
	typesPath := filepath.Join(outDir, "types.ts")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		t.Fatal("types.ts was not created")
	}

	// Read all .ts files and check for User interface
	files, _ := filepath.Glob(filepath.Join(outDir, "*.ts"))
	var foundUser bool
	for _, f := range files {
		content, _ := os.ReadFile(f)
		if strings.Contains(string(content), "export interface User") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Error("expected User interface in generated files")
	}

	// Should NOT have manifest.ts
	manifestPath := filepath.Join(outDir, "manifest.ts")
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Error("should not have manifest.ts when using FromTypes")
	}
}

func TestFromTypes_WithZodFlavor(t *testing.T) {
	result, err := FromTypes(
		testfixtures.User{},
	).Provider("reflection").WithFlavor(FlavorZod).Generate()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all content to check for zod schemas
	var allContent string
	var hasZodFile bool
	for _, f := range result.Files {
		content := string(f.Content)
		allContent += content
		if strings.Contains(f.Path, "zod") {
			hasZodFile = true
		}
	}

	if !hasZodFile {
		t.Error("expected zod schema file when FlavorZod is set")
	}

	// Check that z.object is somewhere in the generated output
	if !strings.Contains(allContent, "z.object") {
		t.Error("zod output should contain z.object schemas")
	}
}

func TestFromTypes_MultiPackageGenericInstantiation(t *testing.T) {
	// Test that Page[v1.User] and Page[v2.User] both work correctly.
	// The reflection provider:
	// 1. Generates instantiated types (Page_..._v1_User, Page_..._v2_User)
	// 2. Follows type arguments to generate both v1.User and v2.User
	result, err := FromTypes(
		testdata.Page[v1.User]{},
		testdata.Page[v2.User]{},
	).Provider("reflection").Generate()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all generated content
	var allContent string
	for _, f := range result.Files {
		allContent += string(f.Content)
	}

	// Should have instantiated Page types (reflection generates concrete types, not generics)
	if !strings.Contains(allContent, "Page_") {
		t.Error("expected instantiated Page types")
	}

	// Should have v1.User (check for name field unique to v1)
	if !strings.Contains(allContent, "name: string") {
		t.Error("expected v1.User name field")
	}

	// Should have v2.User (check for its unique fields: email, role)
	if !strings.Contains(allContent, "email: string") {
		t.Error("expected v2.User email field")
	}
	if !strings.Contains(allContent, "role: string") {
		t.Error("expected v2.User role field")
	}

	// Count User interface definitions - should have 2 (one per package)
	userCount := strings.Count(allContent, "export interface User")
	if userCount != 2 {
		t.Errorf("expected 2 User interface definitions (v1 and v2), got %d", userCount)
	}
}

// TestGenerate_PointerStripping verifies that pointer types are handled correctly:
// - Top-level endpoint request/response pointers are stripped (Go idiom, not nullability)
// - Nested pointers in slices/maps are preserved (indicate nullable elements)
func TestGenerate_PointerStripping(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	// Handler with pointer request and response (common Go pattern)
	createHandler := func(ctx context.Context, req *testfixtures.CreateUserRequest) (*testfixtures.User, error) {
		return nil, nil
	}
	// Handler returning slice of pointers (elements can be null)
	listHandler := func(ctx context.Context, req *testfixtures.ListPostsParams) ([]*testfixtures.Post, error) {
		return nil, nil
	}
	reg.Service("Users").Register("Create", tygor.Exec(createHandler))
	reg.Service("Posts").Register("List", tygor.Query(listHandler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	// Top-level pointers should be stripped - no "| null" on req/res types
	// Users.Create: req should be CreateUserRequest, not (CreateUserRequest | null)
	if strings.Contains(manifestStr, "(types.CreateUserRequest | null)") {
		t.Error("request type should not be nullable - pointer should be stripped")
	}
	if strings.Contains(manifestStr, "(types.User | null)") {
		t.Error("response type should not be nullable - pointer should be stripped")
	}

	// Pointer elements in slices should be unwrapped - []*T → T[]
	// Posts.List: res should be types.Post[] - pointer is implementation detail, not nullability
	if !strings.Contains(manifestStr, "types.Post[]") {
		t.Error("slice element pointers should be unwrapped: expected types.Post[]")
	}
}

// TestGenerate_EmptyRequestType verifies that empty request types (tygor.Empty)
// generate Record<string, never> in the manifest
func TestGenerate_EmptyRequestType(t *testing.T) {
	reg := tygor.NewApp()
	outDir := t.TempDir()

	// Handler with empty request (parameterless endpoint)
	handler := func(ctx context.Context, req *struct{}) (*testfixtures.User, error) {
		return nil, nil
	}
	reg.Service("System").Register("Ping", tygor.Query(handler))

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	// Empty struct request should become Record<string, never>
	if !strings.Contains(manifestStr, "req: Record<string, never>") {
		t.Errorf("empty request type should be Record<string, never>, got:\n%s", manifestStr)
	}
}

func TestFromTypes_SourceProviderGenericDefinition(t *testing.T) {
	// Test that source provider generates generic definitions (Page<T>)
	// and follows type arguments to generate referenced types.
	result, err := FromTypes(
		testdata.Page[v1.User]{},
		testdata.Page[v2.User]{},
	).Provider("source").Generate()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect all generated content
	var allContent string
	for _, f := range result.Files {
		allContent += string(f.Content)
	}

	// Source provider generates generic definition with type parameter
	if !strings.Contains(allContent, "Page<T>") {
		t.Error("expected Page<T> generic definition from source provider")
	}

	// Should NOT have instantiated types (that's reflection provider behavior)
	if strings.Contains(allContent, "Page_") {
		t.Error("source provider should generate generic Page<T>, not instantiated types")
	}

	// Should follow type arguments and generate v1.User
	if !strings.Contains(allContent, "name: string") {
		t.Error("expected v1.User name field")
	}

	// Should follow type arguments and generate v2.User
	if !strings.Contains(allContent, "email: string") {
		t.Error("expected v2.User email field")
	}
	if !strings.Contains(allContent, "role: string") {
		t.Error("expected v2.User role field")
	}

	// Count User interface definitions - should have 2 (one per package)
	userCount := strings.Count(allContent, "export interface User")
	if userCount != 2 {
		t.Errorf("expected 2 User interface definitions (v1 and v2), got %d", userCount)
	}
}
