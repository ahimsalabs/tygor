package ir

import (
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

	// Test type parameter with constraint that references missing type
	s.AddService(ServiceDescriptor{
		Name: "Generic",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Process",
				FullName:  "Generic.Process",
				Primitive: "exec",
				Path:      "/Generic/Process",
				Response:  TypeParam("T", Ref("MissingConstraint", "api")),
			},
		},
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
	s := &Schema{}

	// Test type parameter with nil constraint (unconstrained)
	s.AddService(ServiceDescriptor{
		Name: "Generic",
		Endpoints: []EndpointDescriptor{
			{
				Name:      "Process",
				FullName:  "Generic.Process",
				Primitive: "exec",
				Path:      "/Generic/Process",
				Response:  TypeParam("T", nil), // nil constraint is valid
			},
		},
	})

	errors := s.Validate()
	if len(errors) != 0 {
		t.Errorf("Type parameter with nil constraint should have no errors, got %d: %v", len(errors), errors)
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
			{Name: "BoolField", Type: Bool(), StringEncoded: true},
			{Name: "StringField", Type: String(), StringEncoded: true},
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
