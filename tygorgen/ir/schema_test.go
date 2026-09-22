package ir

import (
	"strconv"
	"strings"
	"testing"
)

func TestSchema_AddType(t *testing.T) {
	s := &Schema{}

	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
	})
	s.AddType(&AliasDescriptor{
		Name:       GoIdentifier{Name: "UserID", Package: "api"},
		Underlying: String(),
	})

	if len(s.Types) != 2 {
		t.Errorf("Schema.Types length = %d, want 2", len(s.Types))
	}
}

func TestSchema_AddService(t *testing.T) {
	s := &Schema{}

	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{Name: "Create", FullName: "Users.Create"},
		},
	})
	s.AddService(ServiceDescriptor{
		Name: "Posts",
	})

	if len(s.Services) != 2 {
		t.Errorf("Schema.Services length = %d, want 2", len(s.Services))
	}
}

func TestSchema_AddWarning(t *testing.T) {
	s := &Schema{}

	s.AddWarning(Warning{Code: "W001", Message: "warning 1"})
	s.AddWarning(Warning{Code: "W002", Message: "warning 2"})

	if len(s.Warnings) != 2 {
		t.Errorf("Schema.Warnings length = %d, want 2", len(s.Warnings))
	}
}

func TestSchema_FindType(t *testing.T) {
	s := &Schema{}
	userID := GoIdentifier{Name: "User", Package: "api"}
	postID := GoIdentifier{Name: "Post", Package: "api"}

	s.AddType(&StructDescriptor{Name: userID})
	s.AddType(&StructDescriptor{Name: postID})

	// Find existing type
	found := s.FindType(userID)
	if found == nil {
		t.Fatal("FindType should find User")
	}
	if found.TypeName() != userID {
		t.Errorf("FindType returned wrong type: %v", found.TypeName())
	}

	// Find non-existing type
	notFound := s.FindType(GoIdentifier{Name: "NotExist", Package: "api"})
	if notFound != nil {
		t.Error("FindType should return nil for non-existing type")
	}
}

func TestSchema_FindService(t *testing.T) {
	s := &Schema{}
	s.AddService(ServiceDescriptor{Name: "Users"})
	s.AddService(ServiceDescriptor{Name: "Posts"})

	// Find existing service
	found := s.FindService("Users")
	if found == nil {
		t.Fatal("FindService should find Users")
	}
	if found.Name != "Users" {
		t.Errorf("FindService returned wrong service: %s", found.Name)
	}

	// Find non-existing service
	notFound := s.FindService("NotExist")
	if notFound != nil {
		t.Error("FindService should return nil for non-existing service")
	}
}

func TestSchema_Package(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
			Dir:  "/home/user/go/src/github.com/example/api",
		},
	}

	if s.Package.Path != "github.com/example/api" {
		t.Errorf("Schema.Package.Path = %q", s.Package.Path)
	}
	if s.Package.Name != "api" {
		t.Errorf("Schema.Package.Name = %q", s.Package.Name)
	}
}

func TestSchema_Full(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add types
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
		Fields: []FieldDescriptor{
			{Name: "ID", JSONName: "id", Type: Int(64)},
			{Name: "Name", JSONName: "name", Type: String()},
		},
	})
	s.AddType(&AliasDescriptor{
		Name:       GoIdentifier{Name: "UserID", Package: "api"},
		Underlying: Int(64),
	})
	s.AddType(&EnumDescriptor{
		Name: GoIdentifier{Name: "Status", Package: "api"},
		Members: []EnumMember{
			{Name: "StatusActive", Value: "active"},
			{Name: "StatusInactive", Value: "inactive"},
		},
	})

	// Add services
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Get",
				FullName:  "Users.Get",
				Primitive: "query",
				Path:      "/Users/Get",
				Request:   Ref("GetUserRequest", "api"),
				Response:  Ref("User", "api"),
			},
		},
	})

	// Verify counts
	if len(s.Types) != 3 {
		t.Errorf("Schema.Types length = %d, want 3", len(s.Types))
	}
	if len(s.Services) != 1 {
		t.Errorf("Schema.Services length = %d, want 1", len(s.Services))
	}

	// Verify type kinds
	kinds := make(map[DescriptorKind]int)
	for _, typ := range s.Types {
		kinds[typ.Kind()]++
	}
	if kinds[KindStruct] != 1 {
		t.Errorf("expected 1 struct, got %d", kinds[KindStruct])
	}
	if kinds[KindAlias] != 1 {
		t.Errorf("expected 1 alias, got %d", kinds[KindAlias])
	}
	if kinds[KindEnum] != 1 {
		t.Errorf("expected 1 enum, got %d", kinds[KindEnum])
	}
}

func TestSchema_Validate_ValidSchema(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add types
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
		Fields: []FieldDescriptor{
			{Name: "ID", JSONName: "id", Type: Int(64)},
			{Name: "Name", JSONName: "name", Type: String()},
		},
	})
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "CreateUserRequest", Package: "api"},
		Fields: []FieldDescriptor{
			{Name: "Name", JSONName: "name", Type: String()},
		},
	})

	// Add service with valid endpoints
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Request:   Ref("CreateUserRequest", "api"),
				Response:  Ref("User", "api"),
			},
			{
				Name:      "List",
				FullName:  "Users.List",
				Primitive: "query",
				Path:      "/Users/List",
				Request:   nil, // nil request is valid
				Response:  Slice(Ref("User", "api")),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Valid schema should have no errors, got %d: %v", len(errors), errors)
	}
}

func TestSchema_Validate_MissingTypeReference(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add a service that references a type that doesn't exist
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Request:   Ref("CreateUserRequest", "api"), // Missing type
				Response:  Ref("User", "api"),              // Missing type
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 2 {
		t.Errorf("Expected 2 errors for missing types, got %d: %v", len(errors), errors)
	}

	// Check that both errors are about missing type references
	for _, err := range errors {
		if ve, ok := err.(*ValidationError); ok {
			if ve.Code != "missing_type_reference" {
				t.Errorf("Expected error code 'missing_type_reference', got %q", ve.Code)
			}
		} else {
			t.Errorf("Expected ValidationError, got %T", err)
		}
	}
}

func TestSchema_Validate_DuplicateEndpointName(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add a service with duplicate endpoint names
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Request:   String(),
				Response:  String(),
			},
			{
				Name:      "Create", // Duplicate name in same service
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Request:   String(),
				Response:  String(),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for duplicate endpoint, got %d: %v", len(errors), errors)
	}

	if ve, ok := errors[0].(*ValidationError); ok {
		if ve.Code != "duplicate_endpoint" {
			t.Errorf("Expected error code 'duplicate_endpoint', got %q", ve.Code)
		}
		if ve.Message != "duplicate endpoint name in service Users: Create" {
			t.Errorf("Unexpected error message: %s", ve.Message)
		}
	} else {
		t.Errorf("Expected ValidationError, got %T", errors[0])
	}
}

func TestSchema_Validate_DuplicateEndpointAcrossServices(t *testing.T) {
	// Duplicate endpoint names across different services should be allowed
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Response:  String(),
			},
		},
	})

	s.AddService(ServiceDescriptor{
		Name: "Posts",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create", // Same name, different service - should be OK
				FullName:  "Posts.Create",
				Primitive: "exec",
				Path:      "/Posts/Create",
				Response:  String(),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Duplicate endpoint names across services should be allowed, got %d errors: %v", len(errors), errors)
	}
}

func TestSchema_Validate_InvalidFullName(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Wrong.Name", // Should be "Users.Create"
				Primitive: "exec",
				Path:      "/Users/Create",
				Response:  String(),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for invalid FullName, got %d: %v", len(errors), errors)
	}

	if ve, ok := errors[0].(*ValidationError); ok {
		if ve.Code != "invalid_fullname" {
			t.Errorf("Expected error code 'invalid_fullname', got %q", ve.Code)
		}
	} else {
		t.Errorf("Expected ValidationError, got %T", errors[0])
	}
}

func TestSchema_Validate_InvalidPath(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/api/users/create", // Should be "/Users/Create"
				Response:  String(),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for invalid Path, got %d: %v", len(errors), errors)
	}

	if ve, ok := errors[0].(*ValidationError); ok {
		if ve.Code != "invalid_path" {
			t.Errorf("Expected error code 'invalid_path', got %q", ve.Code)
		}
	} else {
		t.Errorf("Expected ValidationError, got %T", errors[0])
	}
}

func TestSchema_Validate_MultipleErrors(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Schema with multiple validation errors
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Create",
				FullName:  "Wrong.Create", // Invalid FullName
				Primitive: "exec",
				Path:      "/wrong/path",         // Invalid Path
				Request:   Ref("Missing", "api"), // Missing type
				Response:  Ref("User", "api"),    // Missing type
			},
			{
				Name:      "Create", // Duplicate name
				FullName:  "Users.Create",
				Primitive: "exec",
				Path:      "/Users/Create",
				Response:  String(),
			},
		},
	})

	errors := s.Validate()
	// Should have: 2 missing types + 1 invalid fullname + 1 invalid path + 1 duplicate = 5 errors
	if len(errors) != 5 {
		t.Errorf("Expected 5 errors, got %d: %v", len(errors), errors)
	}
}

func TestSchema_Validate_NestedTypeReferences(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add one type
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
	})

	// Test nested references in arrays, maps, pointers
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "GetArray",
				FullName:  "Users.GetArray",
				Primitive: "query",
				Path:      "/Users/GetArray",
				Response:  Slice(Ref("Missing", "api")), // Missing type in array
			},
			{
				Name:      "GetMap",
				FullName:  "Users.GetMap",
				Primitive: "query",
				Path:      "/Users/GetMap",
				Response:  Map(String(), Ref("Missing", "api")), // Missing type in map value
			},
			{
				Name:      "GetPtr",
				FullName:  "Users.GetPtr",
				Primitive: "query",
				Path:      "/Users/GetPtr",
				Response:  Ptr(Ref("Missing", "api")), // Missing type in pointer
			},
			{
				Name:      "GetValid",
				FullName:  "Users.GetValid",
				Primitive: "query",
				Path:      "/Users/GetValid",
				Response:  Slice(Ref("User", "api")), // Valid reference
			},
		},
	})

	errors := s.Validate()
	// Should have 3 errors (one for each missing type reference)
	if len(errors) != 3 {
		t.Errorf("Expected 3 errors for nested missing types, got %d: %v", len(errors), errors)
	}

	for _, err := range errors {
		if ve, ok := err.(*ValidationError); ok {
			if ve.Code != "missing_type_reference" {
				t.Errorf("Expected error code 'missing_type_reference', got %q", ve.Code)
			}
		} else {
			t.Errorf("Expected ValidationError, got %T", err)
		}
	}
}

func TestSchema_Validate_EmptySchema(t *testing.T) {
	s := &Schema{}

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Empty schema should have no errors, got %d: %v", len(errors), errors)
	}
}

func TestSchema_Validate_NoServices(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add types but no services
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Schema with only types should have no errors, got %d: %v", len(errors), errors)
	}
}

func TestSchema_Validate_PrimitiveResponse(t *testing.T) {
	s := &Schema{}

	// Endpoint with primitive response (no type reference)
	s.AddService(ServiceDescriptor{
		Name: "Math",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Add",
				FullName:  "Math.Add",
				Primitive: "exec",
				Path:      "/Math/Add",
				Request:   Slice(Int(64)),
				Response:  Int(64),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Endpoints with primitive types should have no errors, got %d: %v", len(errors), errors)
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{
		Code:    "test_code",
		Message: "test message",
	}

	if ve.Error() != "test message" {
		t.Errorf("ValidationError.Error() = %q, want %q", ve.Error(), "test message")
	}
}

func TestSchema_Validate_UnionTypeReferences(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Add one valid type
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "api"},
	})

	// Test union with missing type reference
	s.AddService(ServiceDescriptor{
		Name: "Users",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Get",
				FullName:  "Users.Get",
				Primitive: "query",
				Path:      "/Users/Get",
				Response:  Union(Ref("User", "api"), Ref("Missing", "api")),
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for missing type in union, got %d: %v", len(errors), errors)
	}

	if ve, ok := errors[0].(*ValidationError); ok {
		if ve.Code != "missing_type_reference" {
			t.Errorf("Expected error code 'missing_type_reference', got %q", ve.Code)
		}
	} else {
		t.Errorf("Expected ValidationError, got %T", errors[0])
	}
}

func TestSchema_Validate_TypeParameterConstraint(t *testing.T) {
	s := &Schema{
		Package: PackageInfo{
			Path: "github.com/example/api",
			Name: "api",
		},
	}

	// Test a declared type parameter with a constraint that references a missing type.
	s.AddType(&AliasDescriptor{
		Name: GoIdentifier{Name: "Constrained", Package: "api"},
		TypeParameters: []TypeParameterDescriptor{
			{ParamName: "T", Constraint: Ref("MissingConstraint", "api")},
		},
		Underlying: TypeParam("T", nil),
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for missing type in constraint, got %d: %v", len(errors), errors)
	}

	if ve, ok := errors[0].(*ValidationError); ok {
		if ve.Code != "missing_type_reference" {
			t.Errorf("Expected error code 'missing_type_reference', got %q", ve.Code)
		}
	} else {
		t.Errorf("Expected ValidationError, got %T", errors[0])
	}
}

func TestSchema_Validate_TypeParameterNoConstraint(t *testing.T) {
	s := &Schema{Types: []TypeDescriptor{&AliasDescriptor{
		Name:           GoIdentifier{Name: "Identity", Package: "api"},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		Underlying:     TypeParam("T", nil),
	}}}

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Type parameter with nil constraint should have no errors, got %d: %v", len(errors), errors)
	}
}

func TestSchema_Validate_TypeParameterScope(t *testing.T) {
	box := GoIdentifier{Name: "Box", Package: "test"}
	validBox := &StructDescriptor{
		Name:           box,
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		Fields:         []FieldDescriptor{{Name: "Value", Type: TypeParam("T", nil)}},
	}
	tests := []struct {
		name   string
		schema *Schema
		code   string
	}{
		{
			name: "undeclared struct field",
			schema: &Schema{Types: []TypeDescriptor{&StructDescriptor{
				Name:   GoIdentifier{Name: "Broken", Package: "test"},
				Fields: []FieldDescriptor{{Name: "Value", Type: TypeParam("T", nil)}},
			}}},
			code: "undeclared_type_parameter",
		},
		{
			name: "undeclared reference argument",
			schema: &Schema{Types: []TypeDescriptor{
				validBox,
				&AliasDescriptor{Name: GoIdentifier{Name: "Broken", Package: "test"}, Underlying: RefWithArgs(box.Name, box.Package, TypeParam("U", nil))},
			}},
			code: "undeclared_type_parameter",
		},
		{
			name: "endpoint parameter",
			schema: &Schema{Services: []ServiceDescriptor{{Name: "Generic", Endpoints: []EndpointDescriptor{{
				Name: "Get", FullName: "Generic.Get", Path: "/Generic/Get", Response: TypeParam("T", nil),
			}}}}},
			code: "undeclared_type_parameter",
		},
		{
			name: "empty declaration",
			schema: &Schema{Types: []TypeDescriptor{&AliasDescriptor{
				Name: GoIdentifier{Name: "Broken", Package: "test"}, TypeParameters: []TypeParameterDescriptor{{}}, Underlying: String(),
			}}},
			code: "empty_type_parameter",
		},
		{
			name: "duplicate declaration",
			schema: &Schema{Types: []TypeDescriptor{&StructDescriptor{
				Name: GoIdentifier{Name: "Broken", Package: "test"}, TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}, {ParamName: "T"}},
			}}},
			code: "duplicate_type_parameter",
		},
		{
			name: "scope isolation",
			schema: &Schema{Types: []TypeDescriptor{
				validBox,
				&AliasDescriptor{Name: GoIdentifier{Name: "Broken", Package: "test"}, Underlying: TypeParam("T", nil)},
			}},
			code: "undeclared_type_parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidationCode(t, tt.schema.Validate(), tt.code)
		})
	}
}

func TestSchema_Validate_DuplicateType(t *testing.T) {
	s := &Schema{}

	// Add the same type twice
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "test/pkg"},
	})
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "test/pkg"},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Fatalf("Expected 1 error for duplicate type, got %d: %v", len(errors), errors)
	}
	if !strings.Contains(errors[0].Error(), "duplicate type name") {
		t.Errorf("Expected duplicate type error, got: %v", errors[0])
	}
}

func TestSchema_Validate_DuplicateTypeDifferentPackages(t *testing.T) {
	s := &Schema{}

	// Same name but different packages should be allowed
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "pkg/a"},
	})
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "User", Package: "pkg/b"},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Same type name in different packages should be allowed, got errors: %v", errors)
	}
}

func TestSchema_Validate_StringEncodedOnValidTypes(t *testing.T) {
	s := &Schema{}

	// StringEncoded on valid types should pass
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "Valid", Package: "test"},
		Fields: []FieldDescriptor{
			{Name: "IntField", Type: Int(64), StringEncoded: true},
			{Name: "UintField", Type: Uint(64), StringEncoded: true},
			{Name: "FloatField", Type: Float(64), StringEncoded: true},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("StringEncoded on valid types should pass, got errors: %v", errors)
	}
}

func TestSchema_Validate_StringEncodedOnInvalidTypes(t *testing.T) {
	s := &Schema{}

	// StringEncoded on invalid types should fail
	s.AddType(&StructDescriptor{Name: GoIdentifier{Name: "Other", Package: "test"}})
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "Invalid", Package: "test"},
		Fields: []FieldDescriptor{
			{Name: "StructField", Type: Ref("Other", "test"), StringEncoded: true},
		},
	})

	errors := s.Validate()
	if len(errors) != 1 {
		t.Fatalf("Expected 1 error for StringEncoded on struct type, got %d: %v", len(errors), errors)
	}
	if !strings.Contains(errors[0].Error(), "StringEncoded") || !strings.Contains(errors[0].Error(), "StructField") {
		t.Errorf("Expected StringEncoded error for StructField, got: %v", errors[0])
	}
}

func TestSchema_Validate_StringEncodedOnPointerToValid(t *testing.T) {
	s := &Schema{}

	// StringEncoded on pointer to valid type should pass
	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "Valid", Package: "test"},
		Fields: []FieldDescriptor{
			{Name: "PtrInt", Type: Ptr(Int(64)), StringEncoded: true},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("StringEncoded on pointer to int should pass, got errors: %v", errors)
	}
}

func TestSchema_StringEncodingAppliesThroughPointers(t *testing.T) {
	scalarID := GoIdentifier{Name: "Scalar", Package: "test"}
	pointerID := GoIdentifier{Name: "DefinedPointer", Package: "test"}
	pointerAliasID := GoIdentifier{Name: "DefinedPointerAlias", Package: "test"}
	s := &Schema{Types: []TypeDescriptor{
		&AliasDescriptor{Name: scalarID, Underlying: Int(64)},
		&AliasDescriptor{Name: pointerID, Underlying: Ptr(Int(64))},
		&AliasDescriptor{Name: pointerAliasID, Underlying: Ref(pointerID.Name, pointerID.Package)},
	}}

	tests := []struct {
		name string
		typ  TypeDescriptor
		want bool
	}{
		{name: "direct", typ: Int(64), want: true},
		{name: "duration", typ: Duration(), want: false},
		{name: "single pointer", typ: Ptr(Int(64)), want: true},
		{name: "double pointer", typ: Ptr(Ptr(Int(64))), want: true},
		{name: "named scalar", typ: Ref(scalarID.Name, scalarID.Package), want: true},
		{name: "pointer to named scalar", typ: Ptr(Ref(scalarID.Name, scalarID.Package)), want: true},
		{name: "defined pointer", typ: Ref(pointerID.Name, pointerID.Package), want: true},
		{name: "alias to defined pointer", typ: Ref(pointerAliasID.Name, pointerAliasID.Package), want: true},
		{name: "pointer to defined pointer", typ: Ptr(Ref(pointerID.Name, pointerID.Package)), want: true},
		{name: "pointer to alias to defined pointer", typ: Ptr(Ref(pointerAliasID.Name, pointerAliasID.Package)), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.StringEncodingApplies(tt.typ); got != tt.want {
				t.Fatalf("StringEncodingApplies() = %v, want %v", got, tt.want)
			}
		})
	}

	s.AddType(&StructDescriptor{
		Name: GoIdentifier{Name: "ValidDefinedPointer", Package: "test"},
		Fields: []FieldDescriptor{{
			Name: "Value", Type: Ref(pointerAliasID.Name, pointerAliasID.Package), StringEncoded: true,
		}},
	})
	if errors := s.Validate(); len(errors) != 0 {
		t.Fatalf("StringEncoded on a defined pointer should validate, got errors: %v", errors)
	}
}

func TestSchema_V2FieldPresenceAndNullability(t *testing.T) {
	three := 3
	zero := 0
	schema := &Schema{}
	tests := []struct {
		name     string
		field    FieldDescriptor
		optional bool
		nullable bool
	}{
		{name: "int omitempty remains required", field: FieldDescriptor{Type: Int(0), OmitEmpty: true}},
		{name: "int omitzero", field: FieldDescriptor{Type: Int(0), OmitZero: true}, optional: true},
		{name: "slice is non-null", field: FieldDescriptor{Type: Slice(String())}},
		{name: "slice omitempty", field: FieldDescriptor{Type: Slice(String()), OmitEmpty: true}, optional: true},
		{name: "pointer is nullable", field: FieldDescriptor{Type: Ptr(Int(0))}, nullable: true},
		{name: "pointer omitempty", field: FieldDescriptor{Type: Ptr(Int(0)), OmitEmpty: true}, optional: true},
		{name: "double pointer omitzero", field: FieldDescriptor{Type: Ptr(Ptr(Int(0))), OmitZero: true}, optional: true, nullable: true},
		{name: "fixed bytes omitempty remains required", field: FieldDescriptor{Type: &PrimitiveDescriptor{PrimitiveKind: PrimitiveBytes, ByteArrayLength: &three}, OmitEmpty: true}},
		{name: "empty fixed bytes omitempty", field: FieldDescriptor{Type: &PrimitiveDescriptor{PrimitiveKind: PrimitiveBytes, ByteArrayLength: &zero}, OmitEmpty: true}, optional: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := schema.FieldOptional(tt.field); got != tt.optional {
				t.Errorf("FieldOptional() = %v, want %v", got, tt.optional)
			}
			if got := schema.FieldNullableWhenPresent(tt.field); got != tt.nullable {
				t.Errorf("FieldNullableWhenPresent() = %v, want %v", got, tt.nullable)
			}
		})
	}
}

func TestSchema_HasAppliedGenericPointerOnlyAliasCycle(t *testing.T) {
	id := GoIdentifier{Name: "P", Package: "example.com/p"}
	typeParam := TypeParameterDescriptor{ParamName: "T"}
	alias := &AliasDescriptor{
		Name:           id,
		TypeParameters: []TypeParameterDescriptor{typeParam},
		Underlying:     Ptr(RefWithArgs(id.Name, id.Package, &TypeParameterDescriptor{ParamName: "T"})),
	}
	schema := &Schema{Types: []TypeDescriptor{alias}}

	if !schema.HasPointerOnlyAliasCycle(alias) {
		t.Fatal("applied generic pointer-only alias cycle was not detected")
	}
}

func TestSchema_HasIndirectGenericPointerOnlyAliasCycle(t *testing.T) {
	pkg := "example.com/p"
	typeParam := func(name string) *TypeParameterDescriptor {
		return &TypeParameterDescriptor{ParamName: name}
	}
	pointerAlias := &AliasDescriptor{
		Name:           GoIdentifier{Name: "Ptr", Package: pkg},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		Underlying:     Ptr(typeParam("T")),
	}
	recursiveAlias := &AliasDescriptor{
		Name: GoIdentifier{Name: "P", Package: pkg},
		Underlying: Ptr(RefWithArgs(
			pointerAlias.Name.Name,
			pointerAlias.Name.Package,
			Ref("P", pkg),
		)),
	}
	schema := &Schema{Types: []TypeDescriptor{pointerAlias, recursiveAlias}}

	if !schema.HasPointerOnlyAliasCycle(recursiveAlias) {
		t.Fatal("pointer-only cycle through an applied generic alias was not detected")
	}

	nestedRecursiveAlias := &AliasDescriptor{
		Name: GoIdentifier{Name: "Nested", Package: pkg},
		Underlying: Ptr(RefWithArgs(pointerAlias.Name.Name, pkg,
			RefWithArgs(pointerAlias.Name.Name, pkg, Ref("Nested", pkg)))),
	}
	sameNameWrapper := &AliasDescriptor{
		Name:           GoIdentifier{Name: "Same", Package: pkg},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		Underlying:     Ptr(RefWithArgs(pointerAlias.Name.Name, pkg, typeParam("T"))),
	}
	renamedWrapper := &AliasDescriptor{
		Name:           GoIdentifier{Name: "Renamed", Package: pkg},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "U"}},
		Underlying:     Ptr(RefWithArgs(pointerAlias.Name.Name, pkg, typeParam("U"))),
	}
	productiveAlias := &AliasDescriptor{
		Name: GoIdentifier{Name: "Productive", Package: pkg},
		Underlying: Ptr(RefWithArgs(pointerAlias.Name.Name, pkg,
			Slice(Ref("Productive", pkg)))),
	}
	schema.Types = append(schema.Types, nestedRecursiveAlias, sameNameWrapper, renamedWrapper, productiveAlias)

	if !schema.HasPointerOnlyAliasCycle(nestedRecursiveAlias) {
		t.Fatal("nested applied generic pointer-only cycle was not detected")
	}
	for _, alias := range []*AliasDescriptor{sameNameWrapper, renamedWrapper, productiveAlias} {
		if schema.HasPointerOnlyAliasCycle(alias) {
			t.Fatalf("productive or finite alias %s was classified as a pointer-only cycle", alias.Name.Name)
		}
	}
}

func TestSchema_GenericPointerAliasHeadResolutionIsSymbolicAndFinite(t *testing.T) {
	pkg := "example.com/p"
	typeParam := func(name string) *TypeParameterDescriptor {
		return &TypeParameterDescriptor{ParamName: name}
	}

	expanding := &AliasDescriptor{
		Name:           GoIdentifier{Name: "B", Package: pkg},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}, {ParamName: "U"}},
	}
	expanding.Underlying = Ptr(RefWithArgs(expanding.Name.Name, pkg,
		RefWithArgs(expanding.Name.Name, pkg, typeParam("T"), typeParam("T")),
		RefWithArgs(expanding.Name.Name, pkg, typeParam("U"), typeParam("U")),
	))
	expandingRoot := &AliasDescriptor{
		Name: GoIdentifier{Name: "A", Package: pkg},
		Underlying: Ptr(RefWithArgs(expanding.Name.Name, pkg,
			Int(64), Int(64))),
	}
	schema := &Schema{Types: []TypeDescriptor{expandingRoot, expanding}}
	if !schema.HasPointerOnlyAliasCycle(expandingRoot) {
		t.Fatal("expanding generic pointer-only alias cycle was not detected")
	}

	finite := &AliasDescriptor{
		Name:           GoIdentifier{Name: "F0", Package: pkg},
		TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		Underlying:     Ptr(typeParam("T")),
	}
	schema.Types = append(schema.Types, finite)
	for i := 1; i <= 5; i++ {
		previous := finite
		finite = &AliasDescriptor{
			Name:           GoIdentifier{Name: "F" + strconv.Itoa(i), Package: pkg},
			TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
			Underlying: Ptr(RefWithArgs(previous.Name.Name, pkg,
				RefWithArgs(previous.Name.Name, pkg, typeParam("T")))),
		}
		schema.Types = append(schema.Types, finite)
	}
	if schema.HasPointerOnlyAliasCycle(finite) {
		t.Fatal("finite nested generic pointer wrappers were classified as a cycle")
	}
}

func TestSchema_Validate_StringEncodedNamedScalars(t *testing.T) {
	id := GoIdentifier{Name: "ID", Package: "test"}
	chainedID := GoIdentifier{Name: "ChainedID", Package: "test"}
	status := GoIdentifier{Name: "Status", Package: "test"}
	s := &Schema{Types: []TypeDescriptor{
		&AliasDescriptor{Name: id, Underlying: Int(64)},
		&AliasDescriptor{Name: chainedID, Underlying: Ref(id.Name, id.Package)},
		&EnumDescriptor{Name: status, Members: []EnumMember{{Name: "Ready", Value: int64(1)}}},
		&StructDescriptor{
			Name: GoIdentifier{Name: "Valid", Package: "test"},
			Fields: []FieldDescriptor{
				{Name: "ID", Type: Ref(id.Name, id.Package), StringEncoded: true},
				{Name: "ChainedID", Type: Ptr(Ref(chainedID.Name, chainedID.Package)), StringEncoded: true},
				{Name: "Status", Type: Ref(status.Name, status.Package), StringEncoded: true},
			},
		},
	}}

	if errors := s.Validate(); len(errors) != 0 {
		t.Errorf("StringEncoded on named wire scalars should pass, got errors: %v", errors)
	}
}

func TestSchema_Validate_StringEncodedUnknownEnumAndTypeCycle(t *testing.T) {
	empty := GoIdentifier{Name: "Empty", Package: "test"}
	cycle := &PtrDescriptor{}
	cycle.Element = cycle
	s := &Schema{Types: []TypeDescriptor{
		&EnumDescriptor{Name: empty},
		&StructDescriptor{
			Name: GoIdentifier{Name: "Invalid", Package: "test"},
			Fields: []FieldDescriptor{
				{Name: "Empty", Type: Ref(empty.Name, empty.Package), StringEncoded: true},
				{Name: "Cycle", Type: cycle},
			},
		},
	}}

	errors := s.Validate()
	assertValidationCode(t, errors, "invalid_string_encoded")
	assertValidationCode(t, errors, "cyclic_type_expression")
}

func TestSchema_Validate_MalformedDescriptorsDoNotPanic(t *testing.T) {
	var nilPrimitive *PrimitiveDescriptor
	var nilStruct *StructDescriptor

	tests := []struct {
		name   string
		schema *Schema
		code   string
	}{
		{name: "nil schema", schema: nil, code: "nil_schema"},
		{name: "nil top-level", schema: &Schema{Types: []TypeDescriptor{nil}}, code: "nil_top_level_type"},
		{name: "typed nil top-level", schema: &Schema{Types: []TypeDescriptor{nilStruct}}, code: "nil_top_level_type"},
		{name: "expression at top-level", schema: &Schema{Types: []TypeDescriptor{String()}}, code: "invalid_top_level_type"},
		{name: "unnamed declaration", schema: &Schema{Types: []TypeDescriptor{&StructDescriptor{}}}, code: "missing_type_name"},
		{
			name: "nil field type",
			schema: &Schema{Types: []TypeDescriptor{&StructDescriptor{
				Name:   GoIdentifier{Name: "Broken", Package: "test"},
				Fields: []FieldDescriptor{{Name: "Value"}},
			}}},
			code: "nil_type_descriptor",
		},
		{
			name: "typed nil field type",
			schema: &Schema{Types: []TypeDescriptor{&StructDescriptor{
				Name:   GoIdentifier{Name: "Broken", Package: "test"},
				Fields: []FieldDescriptor{{Name: "Value", Type: nilPrimitive}},
			}}},
			code: "nil_type_descriptor",
		},
		{
			name: "nil endpoint response",
			schema: &Schema{Services: []ServiceDescriptor{{Name: "Broken", Endpoints: []EndpointDescriptor{{
				Name: "Get", FullName: "Broken.Get", Path: "/Broken/Get",
			}}}}},
			code: "nil_type_descriptor",
		},
		{
			name: "typed nil endpoint response",
			schema: &Schema{Services: []ServiceDescriptor{{Name: "Broken", Endpoints: []EndpointDescriptor{{
				Name: "Get", FullName: "Broken.Get", Path: "/Broken/Get", Response: nilPrimitive,
			}}}}},
			code: "nil_type_descriptor",
		},
		{
			name:   "nil alias underlying",
			schema: &Schema{Types: []TypeDescriptor{&AliasDescriptor{Name: GoIdentifier{Name: "Broken", Package: "test"}}}},
			code:   "nil_type_descriptor",
		},
		{
			name:   "nil array element",
			schema: malformedAlias(&ArrayDescriptor{}),
			code:   "nil_type_descriptor",
		},
		{
			name:   "nil map key",
			schema: malformedAlias(&MapDescriptor{Value: String()}),
			code:   "nil_type_descriptor",
		},
		{
			name:   "nil map value",
			schema: malformedAlias(&MapDescriptor{Key: String()}),
			code:   "nil_type_descriptor",
		},
		{
			name:   "nil pointer element",
			schema: malformedAlias(&PtrDescriptor{}),
			code:   "nil_type_descriptor",
		},
		{
			name:   "nil union member",
			schema: malformedAlias(&UnionDescriptor{Types: []TypeDescriptor{nil}}),
			code:   "nil_type_descriptor",
		},
		{
			name: "nil reference argument",
			schema: &Schema{Types: []TypeDescriptor{
				&StructDescriptor{
					Name:           GoIdentifier{Name: "Box", Package: "test"},
					TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
				},
				&AliasDescriptor{
					Name: GoIdentifier{Name: "Broken", Package: "test"},
					Underlying: &ReferenceDescriptor{
						Target:        GoIdentifier{Name: "Box", Package: "test"},
						TypeArguments: []TypeDescriptor{nil},
					},
				},
			}},
			code: "nil_type_descriptor",
		},
		{
			name:   "negative array length",
			schema: malformedAlias(&ArrayDescriptor{Element: String(), Length: -1}),
			code:   "invalid_array_length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidationCode(t, tt.schema.Validate(), tt.code)
		})
	}
}

func TestSchema_Validate_ReferenceTypeArguments(t *testing.T) {
	box := GoIdentifier{Name: "Box", Package: "test"}
	plain := GoIdentifier{Name: "Plain", Package: "test"}
	schema := &Schema{Types: []TypeDescriptor{
		&StructDescriptor{
			Name:           box,
			TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}},
		},
		&StructDescriptor{Name: plain},
		&StructDescriptor{
			Name: GoIdentifier{Name: "Uses", Package: "test"},
			Fields: []FieldDescriptor{
				{Name: "Applied", Type: RefWithArgs(box.Name, box.Package, String())},
				{Name: "MissingArguments", Type: Ref(box.Name, box.Package)},
				{Name: "TooMany", Type: RefWithArgs(box.Name, box.Package, String(), Int(64))},
				{Name: "Unexpected", Type: RefWithArgs(plain.Name, plain.Package, String())},
				{Name: "NestedMissing", Type: RefWithArgs(box.Name, box.Package, Ref("Missing", "test"))},
			},
		},
	}}

	errors := schema.Validate()
	if got := countValidationCode(errors, "invalid_type_argument_count"); got != 3 {
		t.Errorf("invalid_type_argument_count errors = %d, want 3: %v", got, errors)
	}
	if got := countValidationCode(errors, "missing_type_reference"); got != 1 {
		t.Errorf("missing_type_reference errors = %d, want 1: %v", got, errors)
	}
	if len(errors) != 4 {
		t.Errorf("validation errors = %d, want 4: %v", len(errors), errors)
	}
}

func TestSchema_Validate_GenericExtendsRequiresApplication(t *testing.T) {
	base := GoIdentifier{Name: "Base", Package: "test"}
	schema := &Schema{Types: []TypeDescriptor{
		&StructDescriptor{Name: base, TypeParameters: []TypeParameterDescriptor{{ParamName: "T"}}},
		&StructDescriptor{Name: GoIdentifier{Name: "Derived", Package: "test"}, Extends: []GoIdentifier{base}},
	}}
	assertValidationCode(t, schema.Validate(), "generic_extends_reference")
}

func malformedAlias(underlying TypeDescriptor) *Schema {
	return &Schema{Types: []TypeDescriptor{&AliasDescriptor{
		Name:       GoIdentifier{Name: "Broken", Package: "test"},
		Underlying: underlying,
	}}}
}

func assertValidationCode(t *testing.T, errors []error, code string) {
	t.Helper()
	if countValidationCode(errors, code) == 0 {
		t.Fatalf("validation errors %v do not contain code %q", errors, code)
	}
}

func countValidationCode(errors []error, code string) int {
	count := 0
	for _, err := range errors {
		if validationError, ok := err.(*ValidationError); ok && validationError.Code == code {
			count++
		}
	}
	return count
}
