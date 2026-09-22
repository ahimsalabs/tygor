package tygorgen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tygor.dev/internal/testfixtures"
	"tygor.dev/tygor"
	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider/testdata"
	"tygor.dev/tygorgen/provider/testdata/genericbytes"
	"tygor.dev/tygorgen/provider/testdata/shadow"
	v1 "tygor.dev/tygorgen/provider/testdata/v1"
	v2 "tygor.dev/tygorgen/provider/testdata/v2"
)

type namedEmptyRequest struct{}
type namedStrings []string
type reflectedRecursivePointerAlias *reflectedRecursivePointerAlias

func TestReflectTypeToIRRefPreservesWireShapeAndGenericIdentity(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[string](), true, false)
		primitive, ok := descriptor.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveString {
			t.Fatalf("string endpoint = %#v, want string primitive", descriptor)
		}
	})

	t.Run("pointer response", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[*v1.User](), true, false)
		pointer, ok := descriptor.(*ir.PtrDescriptor)
		if !ok {
			t.Fatalf("pointer response = %T, want *ir.PtrDescriptor", descriptor)
		}
		ref, ok := pointer.Element.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "User" || ref.Target.Package != "tygor.dev/tygorgen/provider/testdata/v1" {
			t.Fatalf("pointer element = %#v, want v1.User reference", pointer.Element)
		}
	})

	t.Run("outer pointer to recursive pointer alias", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[*reflectedRecursivePointerAlias](), true, false)
		pointer, ok := descriptor.(*ir.PtrDescriptor)
		if !ok {
			t.Fatalf("recursive pointer endpoint = %#v, want pointer", descriptor)
		}
		ref, ok := pointer.Element.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "reflectedRecursivePointerAlias" {
			t.Fatalf("recursive pointer endpoint element = %#v, want named alias reference", pointer.Element)
		}
	})

	t.Run("container", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[[]*v1.User](), true, false)
		array, ok := descriptor.(*ir.ArrayDescriptor)
		if !ok || array.Length != 0 {
			t.Fatalf("slice endpoint = %#v, want slice descriptor", descriptor)
		}
		if _, ok := array.Element.(*ir.PtrDescriptor); !ok {
			t.Fatalf("slice element = %T, want nullable pointer", array.Element)
		}
	})

	t.Run("defined byte elements", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[[]testdata.Octet](), true, false)
		primitive, ok := descriptor.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveBytes {
			t.Fatalf("[]Octet endpoint = %#v, want bytes primitive", descriptor)
		}

		descriptor = reflectTypeToIRRef(reflect.TypeFor[[]testdata.MarshaledOctet](), true, false)
		array, ok := descriptor.(*ir.ArrayDescriptor)
		if !ok || array.Length != 0 {
			t.Fatalf("[]MarshaledOctet endpoint = %#v, want slice descriptor", descriptor)
		}
	})

	t.Run("named container identity", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[namedStrings](), true, false)
		ref, ok := descriptor.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "namedStrings" || ref.Target.Package != "tygor.dev/tygorgen" {
			t.Fatalf("named container = %#v, want named reference", descriptor)
		}
	})

	t.Run("special wire scalars", func(t *testing.T) {
		number := reflectTypeToIRRef(reflect.TypeFor[json.Number](), true, false)
		primitive, ok := number.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveFloat || primitive.BitSize != 64 {
			t.Fatalf("json.Number endpoint = %#v, want float64 wire value", number)
		}
		raw := reflectTypeToIRRef(reflect.TypeFor[json.RawMessage](), true, false)
		primitive, ok = raw.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveAny {
			t.Fatalf("json.RawMessage endpoint = %#v, want arbitrary JSON", raw)
		}
		numberMap := reflectTypeToIRRef(reflect.TypeFor[map[json.Number]string](), true, false)
		mapDescriptor, ok := numberMap.(*ir.MapDescriptor)
		if !ok {
			t.Fatalf("map[json.Number]string endpoint = %T, want map descriptor", numberMap)
		}
		key, ok := mapDescriptor.Key.(*ir.PrimitiveDescriptor)
		if !ok || key.PrimitiveKind != ir.PrimitiveString {
			t.Fatalf("json.Number map key = %#v, want string wire key", mapDescriptor.Key)
		}
	})

	t.Run("source generic application", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[testdata.Page[v1.User]](), true, false)
		ref, ok := descriptor.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "Page" || len(ref.TypeArguments) != 1 {
			t.Fatalf("source generic = %#v, want Page with one argument", descriptor)
		}
		arg, ok := ref.TypeArguments[0].(*ir.ReferenceDescriptor)
		if !ok || arg.Target.Name != "User" || arg.Target.Package != "tygor.dev/tygorgen/provider/testdata/v1" {
			t.Fatalf("source generic argument = %#v, want v1.User", ref.TypeArguments[0])
		}
	})

	t.Run("reflection generic application", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[testdata.Page[v1.User]](), true, true)
		ref, ok := descriptor.(*ir.ReferenceDescriptor)
		if !ok || !strings.HasPrefix(ref.Target.Name, "Page_") || len(ref.TypeArguments) != 0 {
			t.Fatalf("reflection generic = %#v, want monomorphized reference", descriptor)
		}
	})

	t.Run("named empty declaration", func(t *testing.T) {
		descriptor := reflectTypeToIRRef(reflect.TypeFor[namedEmptyRequest](), false, false)
		ref, ok := descriptor.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "namedEmptyRequest" || ref.Target.Package != "tygor.dev/tygorgen" {
			t.Fatalf("named empty request = %#v, want named reference", descriptor)
		}
		if isUnnamedEmptyStructType(reflect.TypeFor[namedEmptyRequest]()) {
			t.Fatal("named empty request must remain a provider root")
		}
		if !isUnnamedEmptyStructType(reflect.TypeFor[struct{}]()) {
			t.Fatal("anonymous empty request must not become a top-level schema type")
		}
	})
}

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
					c.OptionalType == "default"
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
					c.OptionalType == "default"
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
	reg.Service("Users").Exec("Create", handler)

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

func TestGenerate_SourceGenericDefinedBytesUseJSONWireType(t *testing.T) {
	type byteResponse = testdata.Response[[]testdata.Octet]
	type customResponse = testdata.Response[[]testdata.MarshaledOctet]
	type nestedResponse = testdata.Response[map[string][][]testdata.Octet]
	type phantomResponse = testdata.Phantom[[]testdata.Octet]

	app := tygor.NewApp()
	app.Service("Bytes").Exec("Echo", func(context.Context, byteResponse) (byteResponse, error) {
		return byteResponse{}, nil
	})
	app.Service("Bytes").Exec("Custom", func(context.Context, customResponse) (customResponse, error) {
		return customResponse{}, nil
	})
	app.Service("Bytes").Exec("Nested", func(context.Context, nestedResponse) (nestedResponse, error) {
		return nestedResponse{}, nil
	})
	app.Service("Bytes").Exec("Phantom", func(context.Context, phantomResponse) (phantomResponse, error) {
		return phantomResponse{}, nil
	})

	dir := t.TempDir()
	result, err := Generate(app, &Config{
		OutDir:     dir,
		Provider:   "source",
		SingleFile: true,
		Flavors:    []Flavor{FlavorZod, FlavorZodMini},
	})
	if err != nil {
		t.Fatal(err)
	}

	service := result.Schema.FindService("Bytes")
	if service == nil || len(service.Endpoints) != 4 {
		t.Fatalf("Bytes service = %#v, want four endpoints", service)
	}
	endpoints := make(map[string]ir.EndpointDescriptor, len(service.Endpoints))
	for _, endpoint := range service.Endpoints {
		endpoints[endpoint.Name] = endpoint
	}
	assertArgument := func(endpointName string, check func(ir.TypeDescriptor) bool, want string) {
		t.Helper()
		endpoint := endpoints[endpointName]
		for name, descriptor := range map[string]ir.TypeDescriptor{
			"request":  endpoint.Request,
			"response": endpoint.Response,
		} {
			ref, ok := descriptor.(*ir.ReferenceDescriptor)
			if !ok || len(ref.TypeArguments) != 1 {
				t.Fatalf("%s %s descriptor = %#v, want one type argument", endpointName, name, descriptor)
			}
			if !check(ref.TypeArguments[0]) {
				t.Fatalf("%s %s argument = %#v, want %s", endpointName, name, ref.TypeArguments[0], want)
			}
		}
	}
	isBytes := func(descriptor ir.TypeDescriptor) bool {
		primitive, ok := descriptor.(*ir.PrimitiveDescriptor)
		return ok && primitive.PrimitiveKind == ir.PrimitiveBytes
	}
	assertArgument("Echo", isBytes, "bytes primitive")
	assertArgument("Phantom", isBytes, "bytes primitive even though the generic field is unused")
	assertArgument("Custom", func(descriptor ir.TypeDescriptor) bool {
		array, ok := descriptor.(*ir.ArrayDescriptor)
		if !ok || array.Length != 0 {
			return false
		}
		primitive, ok := array.Element.(*ir.PrimitiveDescriptor)
		return ok && primitive.PrimitiveKind == ir.PrimitiveAny
	}, "slice of custom-marshaled values")
	assertArgument("Nested", func(descriptor ir.TypeDescriptor) bool {
		mapping, ok := descriptor.(*ir.MapDescriptor)
		if !ok {
			return false
		}
		array, ok := mapping.Value.(*ir.ArrayDescriptor)
		return ok && array.Length == 0 && isBytes(array.Element)
	}, "map of byte-slice arrays")

	bytePayload, err := json.Marshal(byteResponse{Data: []testdata.Octet{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(bytePayload), `{"data":"AQI="}`; got != want {
		t.Fatalf("encoding/json payload = %s, want %s", got, want)
	}
}

func TestGenerate_SourceGenericPointerArgumentsResolvePackages(t *testing.T) {
	type pointerResponse = testdata.Response[[]*v1.User]
	type nestedPointerResponse = testdata.Response[[]*map[string]v1.User]

	app := tygor.NewApp()
	app.Service("Pointers").Exec("Slice", func(context.Context, pointerResponse) (pointerResponse, error) {
		return pointerResponse{}, nil
	})
	app.Service("Pointers").Exec("Nested", func(context.Context, nestedPointerResponse) (nestedPointerResponse, error) {
		return nestedPointerResponse{}, nil
	})
	result, err := Generate(app, &Config{Provider: "source", SingleFile: true})
	if err != nil {
		t.Fatal(err)
	}
	service := result.Schema.FindService("Pointers")
	if service == nil || len(service.Endpoints) != 2 {
		t.Fatalf("Pointers service = %#v, want two endpoints", service)
	}
	endpoints := make(map[string]ir.EndpointDescriptor, len(service.Endpoints))
	for _, endpoint := range service.Endpoints {
		endpoints[endpoint.Name] = endpoint
	}
	responseArgument := func(endpointName string) ir.TypeDescriptor {
		t.Helper()
		ref, ok := endpoints[endpointName].Response.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Name != "Response" || len(ref.TypeArguments) != 1 {
			t.Fatalf("%s response = %#v, want Response with one argument", endpointName, endpoints[endpointName].Response)
		}
		return ref.TypeArguments[0]
	}
	slice, ok := responseArgument("Slice").(*ir.ArrayDescriptor)
	if !ok || slice.Length != 0 {
		t.Fatalf("Slice argument = %#v, want slice", responseArgument("Slice"))
	}
	pointer, ok := slice.Element.(*ir.PtrDescriptor)
	if !ok {
		t.Fatalf("Slice element = %#v, want pointer", slice.Element)
	}
	user, ok := pointer.Element.(*ir.ReferenceDescriptor)
	if !ok || user.Target.Package != "tygor.dev/tygorgen/provider/testdata/v1" || user.Target.Name != "User" {
		t.Fatalf("Slice pointer target = %#v, want v1.User", pointer.Element)
	}
	nested, ok := responseArgument("Nested").(*ir.ArrayDescriptor)
	if !ok || nested.Length != 0 {
		t.Fatalf("Nested argument = %#v, want slice", responseArgument("Nested"))
	}
	nestedPointer, ok := nested.Element.(*ir.PtrDescriptor)
	if !ok {
		t.Fatalf("Nested element = %#v, want pointer", nested.Element)
	}
	if _, ok := nestedPointer.Element.(*ir.MapDescriptor); !ok {
		t.Fatalf("Nested pointer target = %#v, want map", nestedPointer.Element)
	}
}

func TestGenerate_SourceDefinedByteSliceDoesNotExtractUnrelatedPackageTypes(t *testing.T) {
	app := tygor.NewApp()
	app.Service("Bytes").Exec("Raw", func(context.Context, struct{}) ([]genericbytes.Octet, error) {
		return []genericbytes.Octet{1, 2}, nil
	})
	result, err := Generate(app, &Config{Provider: "source", SingleFile: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Schema.Types) != 0 {
		t.Fatalf("scalar byte endpoint extracted unrelated types: %#v", result.Schema.Types)
	}
	service := result.Schema.FindService("Bytes")
	if service == nil || len(service.Endpoints) != 1 {
		t.Fatalf("Bytes service = %#v, want one endpoint", service)
	}
	primitive, ok := service.Endpoints[0].Response.(*ir.PrimitiveDescriptor)
	if !ok || primitive.PrimitiveKind != ir.PrimitiveBytes {
		t.Fatalf("defined byte response = %#v, want bytes primitive", service.Endpoints[0].Response)
	}
	payload, err := json.Marshal([]genericbytes.Octet{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), `"AQI="`; got != want {
		t.Fatalf("encoding/json payload = %s, want %s", got, want)
	}
}

func TestGenerate_SourceNamedTypeCanShadowPredeclaredIdentifier(t *testing.T) {
	app := tygor.NewApp()
	app.Service("Shadow").Exec("Echo", shadow.EchoHandler())
	result, err := Generate(app, &Config{Provider: "source", SingleFile: true})
	if err != nil {
		t.Fatal(err)
	}
	service := result.Schema.FindService("Shadow")
	if service == nil || len(service.Endpoints) != 1 {
		t.Fatalf("Shadow service = %#v, want one endpoint", service)
	}
	for name, descriptor := range map[string]ir.TypeDescriptor{
		"request":  service.Endpoints[0].Request,
		"response": service.Endpoints[0].Response,
	} {
		ref, ok := descriptor.(*ir.ReferenceDescriptor)
		if !ok || ref.Target.Package != "tygor.dev/tygorgen/provider/testdata/shadow" || ref.Target.Name != "int" {
			t.Fatalf("%s descriptor = %#v, want shadow.int reference", name, descriptor)
		}
		alias, ok := result.Schema.FindType(ref.Target).(*ir.AliasDescriptor)
		if !ok {
			t.Fatalf("%s target = %#v, want alias", name, result.Schema.FindType(ref.Target))
		}
		primitive, ok := alias.Underlying.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveString {
			t.Fatalf("shadow.int underlying = %#v, want string", alias.Underlying)
		}
	}
	payload, err := shadow.JSONValue()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), `"value"`; got != want {
		t.Fatalf("encoding/json payload = %s, want %s", got, want)
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
	reg.Service("Users").Exec("Create", handler)

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
	reg.Service("Users").Exec("Create", createHandler)
	reg.Service("Posts").Query("List", listHandler)

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	// Verify imports
	if !strings.Contains(manifestStr, "import type * as types") {
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
	reg.Service("Users").Exec("Create", handler)

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
	reg.Service("Users").Exec("Create", handler)

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
	reg.Service("Posts").Query("List", listHandler)

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

func TestFromTypes_CustomMarshalerValidatorRejectsUnknownWireSchema(t *testing.T) {
	for _, flavor := range []Flavor{FlavorZod, FlavorZodMini} {
		t.Run(flavor.String(), func(t *testing.T) {
			result, err := FromTypes(testdata.CustomMarshalerValidation{}).
				Provider("source").
				WithFlavor(flavor).
				Generate()
			if err == nil {
				t.Fatalf("Generate() = %#v, want unsupported validator error", result)
			}
			for _, context := range []string{"CustomMarshalerValidation", "field Address", `validator "email"`, "unknown wire schema"} {
				if !strings.Contains(err.Error(), context) {
					t.Fatalf("Generate() error = %v, want context %q", err, context)
				}
			}
		})
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

}

// TestGenerate_PointerNullability verifies the endpoint wire contract:
// request pointers remain an implementation detail, while response pointers and
// nested pointer elements preserve JSON nullability.
func TestGenerate_PointerNullability(t *testing.T) {
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
	reg.Service("Users").Exec("Create", createHandler)
	reg.Service("Posts").Query("List", listHandler)

	cfg := &Config{OutDir: outDir, SingleFile: true, Provider: "reflection"}

	_, err := Generate(reg, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	content, _ := os.ReadFile(manifestPath)
	manifestStr := string(content)

	// Request pointers are stripped.
	if strings.Contains(manifestStr, "(types.CreateUserRequest | null)") {
		t.Error("request type should not be nullable - pointer should be stripped")
	}
	if !strings.Contains(manifestStr, "res: (types.User | null)") {
		t.Error("pointer response should be nullable")
	}

	if !strings.Contains(manifestStr, "res: ((types.Post | null)[] | null)") {
		t.Error("slice and pointer elements should preserve nullability")
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
	reg.Service("System").Query("Ping", handler)

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

}
