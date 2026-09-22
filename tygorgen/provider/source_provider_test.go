package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider/testdata"
	"tygor.dev/tygorgen/provider/testdata/genericbytes"
	"tygor.dev/tygorgen/provider/testdata/genericstring"
)

// rootTypes converts string names to RootType slice for test convenience.
func rootTypes(names ...string) []RootType {
	result := make([]RootType, len(names))
	for i, name := range names {
		result[i] = RootType{Name: name}
	}
	return result
}

func TestSourceProvider_BasicTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("User"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	if schema == nil {
		t.Fatal("schema is nil")
	}

	// Find User type
	userType := findType(schema, "User")
	if userType == nil {
		t.Fatal("User type not found")
	}

	userStruct, ok := userType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("User is not a StructDescriptor, got %T", userType)
	}

	// Check documentation
	if userStruct.Documentation.Summary == "" {
		t.Error("User should have documentation summary")
	}

	// Check fields
	expectedFields := map[string]struct {
		jsonName      string
		optional      bool
		primitiveKind ir.PrimitiveKind
	}{
		"ID":        {"id", false, ir.PrimitiveString},
		"Name":      {"name", false, ir.PrimitiveString},
		"Email":     {"email", true, ir.PrimitiveString},
		"CreatedAt": {"created_at", false, ir.PrimitiveTime},
	}

	for fieldName, expected := range expectedFields {
		field := findFieldByName(userStruct.Fields, fieldName)
		if field == nil {
			t.Errorf("Field %s not found", fieldName)
			continue
		}

		if field.JSONName != expected.jsonName {
			t.Errorf("Field %s: expected JSON name %q, got %q", fieldName, expected.jsonName, field.JSONName)
		}

		if field.Optional != expected.optional {
			t.Errorf("Field %s: expected Optional=%v, got %v", fieldName, expected.optional, field.Optional)
		}
	}

	// Check Age field (pointer type)
	ageField := findFieldByName(userStruct.Fields, "Age")
	if ageField == nil {
		t.Fatal("Age field not found")
	}
	if ageField.Type.Kind() != ir.KindPtr {
		t.Errorf("Age field should be a pointer type, got %v", ageField.Type.Kind())
	}
}

func TestSourceProvider_EnumTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("Status", "Priority"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Check Status enum (string-based)
	statusType := findType(schema, "Status")
	if statusType == nil {
		t.Fatal("Status type not found")
	}

	statusEnum, ok := statusType.(*ir.EnumDescriptor)
	if !ok {
		t.Fatalf("Status is not an EnumDescriptor, got %T", statusType)
	}

	if len(statusEnum.Members) != 3 {
		t.Errorf("Status should have 3 members, got %d", len(statusEnum.Members))
	}

	// Check member values
	expectedValues := map[string]string{
		"StatusActive":   "active",
		"StatusInactive": "inactive",
		"StatusPending":  "pending",
	}

	for _, member := range statusEnum.Members {
		expectedValue, exists := expectedValues[member.Name]
		if !exists {
			t.Errorf("Unexpected enum member: %s", member.Name)
			continue
		}

		if strVal, ok := member.Value.(string); !ok || strVal != expectedValue {
			t.Errorf("Member %s: expected value %q, got %v", member.Name, expectedValue, member.Value)
		}
	}

	// Check Priority enum (int-based)
	priorityType := findType(schema, "Priority")
	if priorityType == nil {
		t.Fatal("Priority type not found")
	}

	priorityEnum, ok := priorityType.(*ir.EnumDescriptor)
	if !ok {
		t.Fatalf("Priority is not an EnumDescriptor, got %T", priorityType)
	}

	if len(priorityEnum.Members) != 3 {
		t.Errorf("Priority should have 3 members, got %d", len(priorityEnum.Members))
	}
}

func TestSourceProvider_EnumExcludesUnexportedConstants(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("Status"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	statusType := findType(schema, "Status")
	if statusType == nil {
		t.Fatal("Status type not found")
	}

	statusEnum, ok := statusType.(*ir.EnumDescriptor)
	if !ok {
		t.Fatalf("Status is not an EnumDescriptor, got %T", statusType)
	}

	// Verify unexported constants are excluded
	// testdata/basic.go has statusInternal which is unexported
	for _, member := range statusEnum.Members {
		if member.Name == "statusInternal" {
			t.Error("Unexported constant 'statusInternal' should not be included in enum members")
		}
	}

	// Verify only exported constants are included
	if len(statusEnum.Members) != 3 {
		t.Errorf("Status should have exactly 3 exported members, got %d", len(statusEnum.Members))
	}
}

func TestSourceProvider_SliceAndArrayTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("SliceAndArrayTypes"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	sliceType := findType(schema, "SliceAndArrayTypes")
	if sliceType == nil {
		t.Fatal("SliceAndArrayTypes not found")
	}

	sliceStruct, ok := sliceType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("SliceAndArrayTypes is not a StructDescriptor, got %T", sliceType)
	}

	// Check Slice field
	sliceField := findFieldByName(sliceStruct.Fields, "Slice")
	if sliceField == nil {
		t.Fatal("Slice field not found")
	}
	if sliceField.Type.Kind() != ir.KindArray {
		t.Errorf("Slice field should be KindArray, got %v", sliceField.Type.Kind())
	}
	arrayDesc := sliceField.Type.(*ir.ArrayDescriptor)
	if arrayDesc.Length != 0 {
		t.Errorf("Slice should have Length=0, got %d", arrayDesc.Length)
	}

	// Check Array field (fixed-length)
	arrayField := findFieldByName(sliceStruct.Fields, "Array")
	if arrayField == nil {
		t.Fatal("Array field not found")
	}
	if arrayField.Type.Kind() != ir.KindArray {
		t.Errorf("Array field should be KindArray, got %v", arrayField.Type.Kind())
	}
	fixedArrayDesc := arrayField.Type.(*ir.ArrayDescriptor)
	if fixedArrayDesc.Length != 3 {
		t.Errorf("Array should have Length=3, got %d", fixedArrayDesc.Length)
	}

	// Check ByteSlice field (should be PrimitiveBytes)
	byteSliceField := findFieldByName(sliceStruct.Fields, "ByteSlice")
	if byteSliceField == nil {
		t.Fatal("ByteSlice field not found")
	}
	if byteSliceField.Type.Kind() != ir.KindPrimitive {
		t.Errorf("ByteSlice should be KindPrimitive, got %v", byteSliceField.Type.Kind())
	}
	primDesc := byteSliceField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveBytes {
		t.Errorf("ByteSlice should be PrimitiveBytes, got %v", primDesc.PrimitiveKind)
	}

	// Check ByteArray field (should be array of bytes, NOT PrimitiveBytes)
	byteArrayField := findFieldByName(sliceStruct.Fields, "ByteArray")
	if byteArrayField == nil {
		t.Fatal("ByteArray field not found")
	}
	if byteArrayField.Type.Kind() != ir.KindArray {
		t.Errorf("ByteArray should be KindArray, got %v", byteArrayField.Type.Kind())
	}
}

func TestSourceProvider_MapTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("MapTypes"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	mapType := findType(schema, "MapTypes")
	if mapType == nil {
		t.Fatal("MapTypes not found")
	}

	mapStruct, ok := mapType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("MapTypes is not a StructDescriptor, got %T", mapType)
	}

	// Check StringMap field
	stringMapField := findFieldByName(mapStruct.Fields, "StringMap")
	if stringMapField == nil {
		t.Fatal("StringMap field not found")
	}
	if stringMapField.Type.Kind() != ir.KindMap {
		t.Errorf("StringMap should be KindMap, got %v", stringMapField.Type.Kind())
	}

	// Check IntMap field (int keys should be valid)
	intMapField := findFieldByName(mapStruct.Fields, "IntMap")
	if intMapField == nil {
		t.Fatal("IntMap field not found")
	}
	if intMapField.Type.Kind() != ir.KindMap {
		t.Errorf("IntMap should be KindMap, got %v", intMapField.Type.Kind())
	}
}

func TestSourceProvider_PointerTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("PointerTypes"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	ptrType := findType(schema, "PointerTypes")
	if ptrType == nil {
		t.Fatal("PointerTypes not found")
	}

	ptrStruct, ok := ptrType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("PointerTypes is not a StructDescriptor, got %T", ptrType)
	}

	// Check RequiredPtr (pointer without omitempty)
	requiredPtrField := findFieldByName(ptrStruct.Fields, "RequiredPtr")
	if requiredPtrField == nil {
		t.Fatal("RequiredPtr field not found")
	}
	if requiredPtrField.Type.Kind() != ir.KindPtr {
		t.Errorf("RequiredPtr should be KindPtr, got %v", requiredPtrField.Type.Kind())
	}
	if requiredPtrField.Optional {
		t.Error("RequiredPtr should not be optional")
	}

	// Check OptionalPtr (pointer with omitempty)
	optionalPtrField := findFieldByName(ptrStruct.Fields, "OptionalPtr")
	if optionalPtrField == nil {
		t.Fatal("OptionalPtr field not found")
	}
	if !optionalPtrField.Optional {
		t.Error("OptionalPtr should be optional")
	}

	// Check DoublePtr (pointer to pointer)
	doublePtrField := findFieldByName(ptrStruct.Fields, "DoublePtr")
	if doublePtrField == nil {
		t.Fatal("DoublePtr field not found")
	}
	if doublePtrField.Type.Kind() != ir.KindPtr {
		t.Errorf("DoublePtr should be KindPtr, got %v", doublePtrField.Type.Kind())
	}
	// The element should also be a pointer
	ptrDesc := doublePtrField.Type.(*ir.PtrDescriptor)
	if ptrDesc.Element.Kind() != ir.KindPtr {
		t.Errorf("DoublePtr element should be KindPtr, got %v", ptrDesc.Element.Kind())
	}
}

func TestSourceProvider_EmbeddedTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("EmbeddedTypes", "NamedEmbedding"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Embedded fields are flattened to the effective encoding/json shape.
	embeddedType := findType(schema, "EmbeddedTypes")
	if embeddedType == nil {
		t.Fatal("EmbeddedTypes not found")
	}

	embeddedStruct, ok := embeddedType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("EmbeddedTypes is not a StructDescriptor, got %T", embeddedType)
	}

	if len(embeddedStruct.Extends) != 0 {
		t.Errorf("EmbeddedTypes should not use Extends, got %d entries", len(embeddedStruct.Extends))
	}
	if baseField := findFieldByName(embeddedStruct.Fields, "BaseField"); baseField == nil {
		t.Error("promoted BaseField not found in EmbeddedTypes")
	}

	// Should have OwnField
	ownField := findFieldByName(embeddedStruct.Fields, "OwnField")
	if ownField == nil {
		t.Error("OwnField not found in EmbeddedTypes")
	}

	// Check NamedEmbedding (nested, not inheritance)
	namedType := findType(schema, "NamedEmbedding")
	if namedType == nil {
		t.Fatal("NamedEmbedding not found")
	}

	namedStruct, ok := namedType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("NamedEmbedding is not a StructDescriptor, got %T", namedType)
	}

	// Should NOT have anything in Extends
	if len(namedStruct.Extends) != 0 {
		t.Errorf("NamedEmbedding should not extend any types, got %d", len(namedStruct.Extends))
	}

	// Should have Base as a regular field
	baseField := findFieldByName(namedStruct.Fields, "Base")
	if baseField == nil {
		t.Error("Base field not found in NamedEmbedding")
	} else {
		if baseField.JSONName != "base" {
			t.Errorf("Base field should have JSON name 'base', got %q", baseField.JSONName)
		}
	}
}

func TestSourceProvider_TaggedFields(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("TaggedFields"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	taggedType := findType(schema, "TaggedFields")
	if taggedType == nil {
		t.Fatal("TaggedFields not found")
	}

	taggedStruct, ok := taggedType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("TaggedFields is not a StructDescriptor, got %T", taggedType)
	}

	// Skipped field should not appear
	skippedField := findFieldByName(taggedStruct.Fields, "Skipped")
	if skippedField != nil {
		t.Error("Skipped field should not be in Fields")
	}

	// StringEncoded field
	stringEncodedField := findFieldByName(taggedStruct.Fields, "StringEncoded")
	if stringEncodedField == nil {
		t.Fatal("StringEncoded field not found")
	}
	if !stringEncodedField.StringEncoded {
		t.Error("StringEncoded field should have StringEncoded=true")
	}

	// Validated field
	validatedField := findFieldByName(taggedStruct.Fields, "Validated")
	if validatedField == nil {
		t.Fatal("Validated field not found")
	}
	if validatedField.ValidateTag != "required,email" {
		t.Errorf("Validated field should have ValidateTag='required,email', got %q", validatedField.ValidateTag)
	}

	// OmitZero field
	omitZeroField := findFieldByName(taggedStruct.Fields, "OmitZero")
	if omitZeroField == nil {
		t.Fatal("OmitZero field not found")
	}
	if !omitZeroField.Optional {
		t.Error("OmitZero field should be Optional")
	}

	// CustomTags field
	customTagsField := findFieldByName(taggedStruct.Fields, "CustomTags")
	if customTagsField == nil {
		t.Fatal("CustomTags field not found")
	}
	if customTagsField.RawTags["db"] != "custom_db" {
		t.Errorf("CustomTags field should have db tag 'custom_db', got %q", customTagsField.RawTags["db"])
	}
	if customTagsField.RawTags["xml"] != "CustomXML" {
		t.Errorf("CustomTags field should have xml tag 'CustomXML', got %q", customTagsField.RawTags["xml"])
	}
}

func TestSourceProvider_DeprecatedType(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("OldStruct"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	oldType := findType(schema, "OldStruct")
	if oldType == nil {
		t.Fatal("OldStruct not found")
	}

	oldStruct, ok := oldType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("OldStruct is not a StructDescriptor, got %T", oldType)
	}

	if oldStruct.Documentation.Deprecated == nil {
		t.Error("OldStruct should be marked as deprecated")
	} else {
		if *oldStruct.Documentation.Deprecated == "" {
			t.Error("Deprecated message should not be empty")
		}
	}
}

func TestSourceProvider_AllExportedTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{"tygor.dev/tygorgen/provider/testdata"},
		// RootTypes is empty - should extract all exported types
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have many types
	if len(schema.Types) < 10 {
		t.Errorf("Expected at least 10 types, got %d", len(schema.Types))
	}

	// Check that various types exist
	expectedTypes := []string{"User", "Status", "Priority", "SimpleStruct", "MapTypes"}
	for _, typeName := range expectedTypes {
		if findType(schema, typeName) == nil {
			t.Errorf("Expected type %s not found", typeName)
		}
	}
}

func TestSourceProvider_NoPackages(t *testing.T) {
	provider := &SourceProvider{}
	_, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{},
	})

	if err == nil {
		t.Error("Expected error when no packages specified")
	}
}

func TestSourceProvider_NonexistentPackage(t *testing.T) {
	provider := &SourceProvider{}
	_, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{"github.com/nonexistent/package"},
	})

	if err == nil {
		t.Error("Expected error for nonexistent package")
	}
}

func TestSourceProvider_InterfaceTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("InterfaceField"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	interfaceType := findType(schema, "InterfaceField")
	if interfaceType == nil {
		t.Fatal("InterfaceField not found")
	}

	interfaceStruct, ok := interfaceType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("InterfaceField is not a StructDescriptor, got %T", interfaceType)
	}

	// Any field should be PrimitiveAny
	anyField := findFieldByName(interfaceStruct.Fields, "Any")
	if anyField == nil {
		t.Fatal("Any field not found")
	}
	if anyField.Type.Kind() != ir.KindPrimitive {
		t.Errorf("Any field should be KindPrimitive, got %v", anyField.Type.Kind())
	}
	primDesc := anyField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveAny {
		t.Errorf("Any field should be PrimitiveAny, got %v", primDesc.PrimitiveKind)
	}
}

// Helper functions

func findType(schema *ir.Schema, name string) ir.TypeDescriptor {
	for _, t := range schema.Types {
		if t.TypeName().Name == name {
			return t
		}
	}
	return nil
}

func findFieldByName(fields []ir.FieldDescriptor, name string) *ir.FieldDescriptor {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

func TestSourceProvider_FieldDocumentation(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("User"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	userType := findType(schema, "User")
	if userType == nil {
		t.Fatal("User type not found")
	}

	structDesc, ok := userType.(*ir.StructDescriptor)
	if !ok {
		t.Fatal("User should be a struct")
	}

	// Check field documentation
	tests := []struct {
		fieldName   string
		wantSummary string
	}{
		{"ID", "ID is the unique identifier"},
		{"Name", "Name is the user's display name"},
		{"Email", "Email is optional"},
		{"Age", "Age may be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			field := findFieldByName(structDesc.Fields, tt.fieldName)
			if field == nil {
				t.Fatalf("Field %s not found", tt.fieldName)
			}
			if field.Documentation.Summary != tt.wantSummary {
				t.Errorf("Field %s doc summary = %q, want %q", tt.fieldName, field.Documentation.Summary, tt.wantSummary)
			}
		})
	}
}

func TestSourceProvider_EnumMemberDocumentation(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("Status"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	statusType := findType(schema, "Status")
	if statusType == nil {
		t.Fatal("Status type not found")
	}

	enumDesc, ok := statusType.(*ir.EnumDescriptor)
	if !ok {
		t.Fatal("Status should be an enum")
	}

	// Check enum member documentation
	tests := []struct {
		memberName  string
		wantSummary string
	}{
		{"StatusActive", "StatusActive means the user is active"},
		{"StatusInactive", "StatusInactive means the user is inactive"},
		{"StatusPending", "StatusPending means awaiting approval"},
	}

	for _, tt := range tests {
		t.Run(tt.memberName, func(t *testing.T) {
			var found *ir.EnumMember
			for i := range enumDesc.Members {
				if enumDesc.Members[i].Name == tt.memberName {
					found = &enumDesc.Members[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("Enum member %s not found", tt.memberName)
			}
			if found.Documentation.Summary != tt.wantSummary {
				t.Errorf("Enum member %s doc summary = %q, want %q", tt.memberName, found.Documentation.Summary, tt.wantSummary)
			}
		})
	}
}

func TestSourceProvider_UnionConstraint(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("Wrapper"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	wrapperType := findType(schema, "Wrapper")
	if wrapperType == nil {
		t.Fatal("Wrapper type not found")
	}

	structDesc, ok := wrapperType.(*ir.StructDescriptor)
	if !ok {
		t.Fatal("Wrapper should be a struct")
	}

	// Wrapper should have one type parameter with a union constraint
	if len(structDesc.TypeParameters) != 1 {
		t.Fatalf("expected 1 type parameter, got %d", len(structDesc.TypeParameters))
	}

	tp := structDesc.TypeParameters[0]
	if tp.ParamName != "T" {
		t.Errorf("expected type parameter name T, got %s", tp.ParamName)
	}

	// The constraint should be a union of string and int (from ~string | ~int)
	if tp.Constraint == nil {
		t.Fatal("expected constraint to be non-nil for union constraint")
	}

	unionDesc, ok := tp.Constraint.(*ir.UnionDescriptor)
	if !ok {
		t.Fatalf("expected UnionDescriptor, got %T", tp.Constraint)
	}

	if len(unionDesc.Types) != 2 {
		t.Fatalf("expected 2 union types, got %d", len(unionDesc.Types))
	}

	// Check that we have string and int primitives
	var hasString, hasInt bool
	for _, ut := range unionDesc.Types {
		if prim, ok := ut.(*ir.PrimitiveDescriptor); ok {
			switch prim.PrimitiveKind {
			case ir.PrimitiveString:
				hasString = true
			case ir.PrimitiveInt:
				hasInt = true
			}
		}
	}

	if !hasString {
		t.Error("union constraint should include string")
	}
	if !hasInt {
		t.Error("union constraint should include int")
	}
}

func TestSourceProvider_NamedMethodConstraintIsExtracted(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("StringerBox"),
	})
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	box, ok := findType(schema, "StringerBox").(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("StringerBox type = %T, want *ir.StructDescriptor", findType(schema, "StringerBox"))
	}
	if len(box.TypeParameters) != 1 {
		t.Fatalf("StringerBox type parameters = %d, want 1", len(box.TypeParameters))
	}
	constraint, ok := box.TypeParameters[0].Constraint.(*ir.ReferenceDescriptor)
	if !ok {
		t.Fatalf("StringerBox constraint = %T, want *ir.ReferenceDescriptor", box.TypeParameters[0].Constraint)
	}
	if constraint.Target != (ir.GoIdentifier{Name: "Stringer", Package: "fmt"}) {
		t.Fatalf("StringerBox constraint target = %+v, want fmt.Stringer", constraint.Target)
	}
	if schema.FindType(constraint.Target) == nil {
		t.Fatal("fmt.Stringer declaration was not extracted")
	}
	if validationErrors := schema.Validate(); len(validationErrors) > 0 {
		t.Fatalf("schema.Validate() error = %v", validationErrors[0])
	}
}

func TestSourceProvider_RecursiveAliasesValidate(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("RecursiveList", "RecursiveMap"),
	})
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}
	if validationErrors := schema.Validate(); len(validationErrors) > 0 {
		t.Fatalf("schema.Validate() error = %v", validationErrors[0])
	}
}

func TestSourceProvider_CustomMarshalerWarning(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("CustomJSONType", "CustomTextType"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Both CustomJSONType and CustomTextType should generate warnings
	foundWarnings := make(map[string]string)

	for _, warning := range schema.Warnings {
		if warning.Code == "CUSTOM_MARSHALER" {
			foundWarnings[warning.TypeName] = warning.Message
		}
	}

	// Verify we got warnings for both types
	for _, typeName := range []string{"CustomJSONType", "CustomTextType"} {
		msg, found := foundWarnings[typeName]
		if !found {
			t.Errorf("Expected CUSTOM_MARSHALER warning for %s, but none was found", typeName)
			continue
		}

		// Verify the message says "unknown" (not "any") - this is the correct terminology
		// since TypeScript output defaults to 'unknown'
		if !strings.Contains(msg, "unknown") {
			t.Errorf("Warning for %s should mention 'unknown', got: %s", typeName, msg)
		}
		if strings.Contains(msg, "'any'") {
			t.Errorf("Warning for %s should not mention 'any', got: %s", typeName, msg)
		}
	}

	// Verify the types are mapped to PrimitiveAny in IR
	jsonType := findType(schema, "CustomJSONType")
	if jsonType == nil {
		t.Fatal("CustomJSONType not found")
	}

	aliasDesc, ok := jsonType.(*ir.AliasDescriptor)
	if !ok {
		t.Fatalf("CustomJSONType should be an AliasDescriptor, got %T", jsonType)
	}

	primDesc, ok := aliasDesc.Underlying.(*ir.PrimitiveDescriptor)
	if !ok || primDesc.PrimitiveKind != ir.PrimitiveAny {
		t.Errorf("CustomJSONType should be mapped to PrimitiveAny")
	}
}

func TestSourceProvider_JSONSpecialTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("JSONSpecialTypes"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	jsonSpecialType := findType(schema, "JSONSpecialTypes")
	if jsonSpecialType == nil {
		t.Fatal("JSONSpecialTypes not found")
	}

	structDesc, ok := jsonSpecialType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("JSONSpecialTypes should be a StructDescriptor, got %T", jsonSpecialType)
	}

	// Test cases for each field
	testCases := []struct {
		fieldName     string
		jsonName      string
		optional      bool
		expectedKind  ir.PrimitiveKind
		expectedBits  int
		expectedIsPtr bool
	}{
		// json.Number emits numeric JSON tokens.
		{"Number", "number", false, ir.PrimitiveFloat, 64, false},
		{"OptionalNumber", "optional_number", true, ir.PrimitiveFloat, 64, false},
		{"NumberAsString", "number_as_string", false, ir.PrimitiveFloat, 64, false},
		{"NumberAlias", "number_alias", false, ir.PrimitiveFloat, 64, false},

		// json.RawMessage should map to PrimitiveAny
		{"RawMessage", "raw_message", false, ir.PrimitiveAny, 0, false},
		{"OptionalRaw", "optional_raw", true, ir.PrimitiveAny, 0, false},

		// Pointers to json.Number should be Ptr(PrimitiveFloat).
		{"NumberPtr", "number_ptr", false, ir.PrimitiveFloat, 64, true},

		// Pointers to json.RawMessage should be Ptr(PrimitiveAny)
		{"RawPtr", "raw_ptr", true, ir.PrimitiveAny, 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.fieldName, func(t *testing.T) {
			field := findFieldByName(structDesc.Fields, tc.fieldName)
			if field == nil {
				t.Fatalf("Field %s not found", tc.fieldName)
			}

			// Verify JSON name
			if field.JSONName != tc.jsonName {
				t.Errorf("Field %s: expected JSON name %q, got %q", tc.fieldName, tc.jsonName, field.JSONName)
			}

			// Verify optional flag
			if field.Optional != tc.optional {
				t.Errorf("Field %s: expected Optional=%v, got %v", tc.fieldName, tc.optional, field.Optional)
			}

			// Check if it's a pointer type
			fieldType := field.Type
			if tc.expectedIsPtr {
				ptrDesc, ok := fieldType.(*ir.PtrDescriptor)
				if !ok {
					t.Fatalf("Field %s: expected pointer type, got %T", tc.fieldName, fieldType)
				}
				fieldType = ptrDesc.Element
			}

			// Verify the underlying type is the correct primitive
			primDesc, ok := fieldType.(*ir.PrimitiveDescriptor)
			if !ok {
				t.Fatalf("Field %s: expected PrimitiveDescriptor, got %T", tc.fieldName, fieldType)
			}

			if primDesc.PrimitiveKind != tc.expectedKind {
				t.Errorf("Field %s: expected PrimitiveKind %v, got %v", tc.fieldName, tc.expectedKind, primDesc.PrimitiveKind)
			}
			if primDesc.BitSize != tc.expectedBits {
				t.Errorf("Field %s: expected BitSize %d, got %d", tc.fieldName, tc.expectedBits, primDesc.BitSize)
			}
		})
	}

	numberAsString := findFieldByName(structDesc.Fields, "NumberAsString")
	if numberAsString == nil || !numberAsString.StringEncoded {
		t.Error("NumberAsString should retain json string encoding metadata")
	}

	numberMap := findFieldByName(structDesc.Fields, "NumberMap")
	if numberMap == nil {
		t.Fatal("NumberMap not found")
	}
	mapDesc, ok := numberMap.Type.(*ir.MapDescriptor)
	if !ok {
		t.Fatalf("NumberMap should be a MapDescriptor, got %T", numberMap.Type)
	}
	mapKey, ok := mapDesc.Key.(*ir.PrimitiveDescriptor)
	if !ok || mapKey.PrimitiveKind != ir.PrimitiveString {
		t.Errorf("NumberMap key should be PrimitiveString, got %#v", mapDesc.Key)
	}

	definedNumber := findFieldByName(structDesc.Fields, "DefinedNumber")
	if definedNumber == nil {
		t.Fatal("DefinedNumber not found")
	}
	definedRef, ok := definedNumber.Type.(*ir.ReferenceDescriptor)
	if !ok || definedRef.Target.Name != "DefinedJSONNumber" {
		t.Fatalf("DefinedNumber should reference DefinedJSONNumber, got %#v", definedNumber.Type)
	}
	definedAlias, ok := findType(schema, "DefinedJSONNumber").(*ir.AliasDescriptor)
	if !ok {
		t.Fatal("DefinedJSONNumber should be an AliasDescriptor")
	}
	definedUnderlying, ok := definedAlias.Underlying.(*ir.PrimitiveDescriptor)
	if !ok || definedUnderlying.PrimitiveKind != ir.PrimitiveString {
		t.Errorf("DefinedJSONNumber should remain string-backed, got %#v", definedAlias.Underlying)
	}
}

func TestSourceProvider_GenericContainerTypeParameters(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("GenericList", "GenericLookup"),
	})
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}
	if validationErrors := schema.Validate(); len(validationErrors) != 0 {
		t.Fatalf("schema validation failed: %v", validationErrors)
	}

	tests := []struct {
		name               string
		wantUnderlyingKind ir.DescriptorKind
	}{
		{name: "GenericList", wantUnderlyingKind: ir.KindArray},
		{name: "GenericLookup", wantUnderlyingKind: ir.KindMap},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alias, ok := findType(schema, tt.name).(*ir.AliasDescriptor)
			if !ok {
				t.Fatalf("%s should be an AliasDescriptor", tt.name)
			}
			if len(alias.TypeParameters) != 1 || alias.TypeParameters[0].ParamName != "T" {
				t.Fatalf("%s type parameters = %#v, want [T]", tt.name, alias.TypeParameters)
			}
			if alias.Underlying.Kind() != tt.wantUnderlyingKind {
				t.Errorf("%s underlying kind = %v, want %v", tt.name, alias.Underlying.Kind(), tt.wantUnderlyingKind)
			}
		})
	}
}

func TestSourceProvider_AnonymousStruct_Basic(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AnonymousStructField"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find the parent type
	parentType := findType(schema, "AnonymousStructField")
	if parentType == nil {
		t.Fatal("AnonymousStructField type not found")
	}

	parentStruct, ok := parentType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("AnonymousStructField is not a StructDescriptor, got %T", parentType)
	}

	// Find the Inner field
	innerField := findFieldByName(parentStruct.Fields, "Inner")
	if innerField == nil {
		t.Fatal("Inner field not found")
	}

	// Inner should be a ReferenceDescriptor pointing to synthetic type
	refDesc, ok := innerField.Type.(*ir.ReferenceDescriptor)
	if !ok {
		t.Fatalf("Inner field should be ReferenceDescriptor, got %T", innerField.Type)
	}

	// Check synthetic name follows pattern ParentType_FieldName
	expectedName := "AnonymousStructField_Inner"
	if refDesc.Target.Name != expectedName {
		t.Errorf("Expected synthetic name %s, got %s", expectedName, refDesc.Target.Name)
	}

	// Find the synthetic type in Schema.Types
	syntheticType := findType(schema, expectedName)
	if syntheticType == nil {
		t.Fatalf("Synthetic type %s not found in schema", expectedName)
	}

	syntheticStruct, ok := syntheticType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Synthetic type should be StructDescriptor, got %T", syntheticType)
	}

	// Verify synthetic struct has the expected fields
	xField := findFieldByName(syntheticStruct.Fields, "X")
	if xField == nil {
		t.Error("X field not found in synthetic struct")
	}
	yField := findFieldByName(syntheticStruct.Fields, "Y")
	if yField == nil {
		t.Error("Y field not found in synthetic struct")
	}
}

func TestSourceProvider_AnonymousStruct_Nested(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("NestedAnonymousStructs"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Verify all synthetic types exist with correct chained names
	expectedTypes := []string{
		"NestedAnonymousStructs",
		"NestedAnonymousStructs_Level1",
		"NestedAnonymousStructs_Level1_Level2",
		"NestedAnonymousStructs_Level1_Level2_Level3",
	}

	for _, typeName := range expectedTypes {
		typ := findType(schema, typeName)
		if typ == nil {
			t.Errorf("Expected type %s not found", typeName)
		}
	}

	// Verify Level3 has the DeepField
	level3Type := findType(schema, "NestedAnonymousStructs_Level1_Level2_Level3")
	if level3Type != nil {
		level3Struct, ok := level3Type.(*ir.StructDescriptor)
		if !ok {
			t.Errorf("Level3 should be StructDescriptor, got %T", level3Type)
		} else {
			deepField := findFieldByName(level3Struct.Fields, "DeepField")
			if deepField == nil {
				t.Error("DeepField not found in Level3")
			}
		}
	}
}

func TestSourceProvider_AnonymousStruct_Multiple(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("MultipleAnonymousFields"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Both synthetic types should exist
	firstType := findType(schema, "MultipleAnonymousFields_First")
	if firstType == nil {
		t.Error("MultipleAnonymousFields_First not found")
	}

	secondType := findType(schema, "MultipleAnonymousFields_Second")
	if secondType == nil {
		t.Error("MultipleAnonymousFields_Second not found")
	}
}

func TestSourceProvider_AnonymousStruct_WithComplexTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AnonymousWithSliceAndMap"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find synthetic type
	dataType := findType(schema, "AnonymousWithSliceAndMap_Data")
	if dataType == nil {
		t.Fatal("AnonymousWithSliceAndMap_Data not found")
	}

	dataStruct, ok := dataType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Data should be StructDescriptor, got %T", dataType)
	}

	// Verify Items field is slice
	itemsField := findFieldByName(dataStruct.Fields, "Items")
	if itemsField == nil {
		t.Fatal("Items field not found")
	}
	if itemsField.Type.Kind() != ir.KindArray {
		t.Errorf("Items should be KindArray, got %v", itemsField.Type.Kind())
	}

	// Verify Props field is map
	propsField := findFieldByName(dataStruct.Fields, "Props")
	if propsField == nil {
		t.Fatal("Props field not found")
	}
	if propsField.Type.Kind() != ir.KindMap {
		t.Errorf("Props should be KindMap, got %v", propsField.Type.Kind())
	}
}

func TestSourceProvider_AnonymousStruct_WithEmbedding(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AnonymousWithEmbedding"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find synthetic type
	configType := findType(schema, "AnonymousWithEmbedding_Config")
	if configType == nil {
		t.Fatal("AnonymousWithEmbedding_Config not found")
	}

	configStruct, ok := configType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Config should be StructDescriptor, got %T", configType)
	}

	if len(configStruct.Extends) != 0 {
		t.Errorf("Expected no Extends entries, got %d", len(configStruct.Extends))
	}
	if baseField := findFieldByName(configStruct.Fields, "BaseField"); baseField == nil {
		t.Error("promoted BaseField not found")
	}

	// Should have Value field
	valueField := findFieldByName(configStruct.Fields, "Value")
	if valueField == nil {
		t.Error("Value field not found")
	}
}

func TestSourceProvider_AnonymousStruct_WithNamedEmbedding(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AnonymousWithNamedEmbedding"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find synthetic type
	settingsType := findType(schema, "AnonymousWithNamedEmbedding_Settings")
	if settingsType == nil {
		t.Fatal("AnonymousWithNamedEmbedding_Settings not found")
	}

	settingsStruct, ok := settingsType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Settings should be StructDescriptor, got %T", settingsType)
	}

	// Should NOT have anything in Extends (embedded with JSON tag is a regular field)
	if len(settingsStruct.Extends) != 0 {
		t.Errorf("Expected 0 extends, got %d", len(settingsStruct.Extends))
	}

	// Should have Base as regular field
	baseField := findFieldByName(settingsStruct.Fields, "Base")
	if baseField == nil {
		t.Error("Base field not found")
	} else if baseField.JSONName != "base" {
		t.Errorf("Expected JSON name 'base', got %q", baseField.JSONName)
	}
}

func TestSourceProvider_AnonymousStruct_NameCollision(t *testing.T) {
	provider := &SourceProvider{}
	// This should fail because CollisionTest_Inner already exists as a named type
	// and CollisionTest.Inner would generate the same synthetic name
	_, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("CollisionTest_Inner", "CollisionTest"),
	})

	if err == nil {
		t.Fatal("Expected error due to name collision, got nil")
	}

	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("Expected error message to mention 'collision', got: %v", err)
	}
}

func TestSourceProvider_NameCollision(t *testing.T) {
	// Create a test scenario where we try to extract the same type twice
	// This simulates what would happen if there were duplicate type names
	provider := &SourceProvider{}

	// First extraction should succeed
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("CollisionTestA", "CollisionTestA"), // Same type twice
	})

	// Should not error - duplicate entries in RootTypes should be deduplicated
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have exactly one CollisionTestA
	count := 0
	for _, typ := range schema.Types {
		if typ.TypeName().Name == "CollisionTestA" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected exactly 1 CollisionTestA, got %d", count)
	}
}

func TestSourceProvider_NoCollisionDifferentTypes(t *testing.T) {
	// Test that different types don't cause collisions
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("CollisionTestA", "CollisionTestB"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have both types
	foundA := false
	foundB := false
	for _, typ := range schema.Types {
		if typ.TypeName().Name == "CollisionTestA" {
			foundA = true
		}
		if typ.TypeName().Name == "CollisionTestB" {
			foundB = true
		}
	}

	if !foundA {
		t.Error("CollisionTestA not found")
	}
	if !foundB {
		t.Error("CollisionTestB not found")
	}
}

func TestSourceProvider_CollisionDetection_SameType(t *testing.T) {
	// Test that the collision detection properly deduplicates
	provider := &SourceProvider{}

	// Extract a type multiple times in RootTypes - should deduplicate
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("DuplicateType", "DuplicateType", "DuplicateType"),
	})

	if err != nil {
		t.Fatalf("BuildSchema should not error on duplicate root types: %v", err)
	}

	// Count how many times DuplicateType appears
	count := 0
	for _, typ := range schema.Types {
		if typ.TypeName().Name == "DuplicateType" {
			count++
		}
	}

	if count != 1 {
		t.Errorf("Expected DuplicateType to appear exactly once, got %d", count)
	}
}

func TestSourceProvider_AliasChains(t *testing.T) {
	// This test verifies that type alias chains are handled correctly
	// and don't cause infinite recursion in convertType.
	// See: convertType has no cycle detection for type aliases
	provider := &SourceProvider{}

	// Use a timeout context to detect infinite loops
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	schema, err := provider.BuildSchema(ctx, SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AliasContainer", "Node"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Verify AliasContainer was processed
	containerType := findType(schema, "AliasContainer")
	if containerType == nil {
		t.Fatal("AliasContainer type not found")
	}

	containerStruct, ok := containerType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("AliasContainer is not a StructDescriptor, got %T", containerType)
	}

	// Check that alias chain fields resolved to correct underlying types
	testCases := []struct {
		fieldName    string
		expectedKind ir.DescriptorKind
	}{
		{"Level1", ir.KindPrimitive}, // string via AliasLevel1
		{"Level2", ir.KindPrimitive}, // string via AliasLevel2 -> AliasLevel1
		{"Level3", ir.KindPrimitive}, // string via AliasLevel3 -> AliasLevel2 -> AliasLevel1
		{"Named", ir.KindReference},  // User via AliasToNamed
		{"Struct", ir.KindReference}, // BaseStruct via AliasToStruct
		{"Deep", ir.KindReference},   // BaseStruct via AliasToAliasStruct -> AliasToStruct
	}

	for _, tc := range testCases {
		t.Run(tc.fieldName, func(t *testing.T) {
			field := findFieldByName(containerStruct.Fields, tc.fieldName)
			if field == nil {
				t.Fatalf("Field %s not found", tc.fieldName)
			}

			if field.Type.Kind() != tc.expectedKind {
				t.Errorf("Field %s: expected kind %v, got %v", tc.fieldName, tc.expectedKind, field.Type.Kind())
			}
		})
	}

	// Verify Node (self-referential via alias) was processed without infinite loop
	nodeType := findType(schema, "Node")
	if nodeType == nil {
		t.Fatal("Node type not found - may indicate infinite loop was triggered")
	}

	nodeStruct, ok := nodeType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Node is not a StructDescriptor, got %T", nodeType)
	}

	// Check Next field is a pointer to a reference (breaking the cycle)
	nextField := findFieldByName(nodeStruct.Fields, "Next")
	if nextField == nil {
		t.Fatal("Next field not found in Node")
	}

	ptrDesc, ok := nextField.Type.(*ir.PtrDescriptor)
	if !ok {
		t.Fatalf("Next field should be a pointer, got %T", nextField.Type)
	}

	// The element should be a reference (to Node or NodeAlias, depending on how alias resolved)
	if ptrDesc.Element.Kind() != ir.KindReference {
		t.Errorf("Next field element should be a reference, got %v", ptrDesc.Element.Kind())
	}
}

func TestSourceProvider_JSONEmbeddingParity(t *testing.T) {
	tests := []struct {
		root       string
		wantFields []string
		absent     []string
	}{
		{root: "EqualDepthConflict", wantFields: []string{"A", "B", "Own"}, absent: []string{"Clash"}},
		{root: "TaggedDominance", wantFields: []string{"Tagged"}, absent: []string{"Same"}},
		{root: "OptionsOnlyEmbedding", wantFields: []string{"Clash", "A"}},
		{root: "TaggedEmbedding", wantFields: []string{"ConflictA"}},
		{root: "ScalarEmbedding", wantFields: []string{"EmbeddedString", "EmbeddedInterface"}},
	}

	provider := &SourceProvider{}
	for _, tt := range tests {
		t.Run(tt.root, func(t *testing.T) {
			schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
				RootTypes: rootTypes(tt.root),
			})
			if err != nil {
				t.Fatalf("BuildSchema failed: %v", err)
			}
			if validationErrors := schema.Validate(); len(validationErrors) != 0 {
				t.Fatalf("schema validation failed: %v", validationErrors)
			}
			root := findType(schema, tt.root).(*ir.StructDescriptor)
			if len(root.Extends) != 0 {
				t.Fatalf("provider emitted Extends: %v", root.Extends)
			}
			for _, name := range tt.wantFields {
				if findFieldByName(root.Fields, name) == nil {
					t.Errorf("field %s not found", name)
				}
			}
			for _, name := range tt.absent {
				if findFieldByName(root.Fields, name) != nil {
					t.Errorf("ambiguous field %s was not omitted", name)
				}
			}
		})
	}

	t.Run("pointer promotion is optional", func(t *testing.T) {
		schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
			Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
			RootTypes: rootTypes("PointerEmbedding"),
		})
		if err != nil {
			t.Fatal(err)
		}
		field := findFieldByName(findType(schema, "PointerEmbedding").(*ir.StructDescriptor).Fields, "FromPointer")
		if field == nil || !field.Optional {
			t.Fatalf("promoted pointer field = %#v, want optional", field)
		}
	})
}

func TestSourceProvider_WireTypeEdgeCases(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes(
			"EscapedTag",
			"MarshaledEnum",
			"MapTypes",
			"GenericUse",
			"StringEncodedNamedScalars",
		),
	})
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	escaped := findFieldByName(findType(schema, "EscapedTag").(*ir.StructDescriptor).Fields, "Escaped")
	if escaped.JSONName != "foo" || !escaped.Optional {
		t.Errorf("escaped tag = (%q, optional=%v), want (foo, true)", escaped.JSONName, escaped.Optional)
	}

	marshaled, ok := findType(schema, "MarshaledEnum").(*ir.AliasDescriptor)
	if !ok || marshaled.Underlying.(*ir.PrimitiveDescriptor).PrimitiveKind != ir.PrimitiveAny {
		t.Fatalf("MarshaledEnum = %T, want any alias", findType(schema, "MarshaledEnum"))
	}

	uintptrMap := findFieldByName(findType(schema, "MapTypes").(*ir.StructDescriptor).Fields, "UintptrMap")
	if uintptrMap == nil || uintptrMap.Type.Kind() != ir.KindMap {
		t.Fatalf("UintptrMap = %#v, want map", uintptrMap)
	}

	box := findFieldByName(findType(schema, "GenericUse").(*ir.StructDescriptor).Fields, "Box")
	ref, ok := box.Type.(*ir.ReferenceDescriptor)
	if !ok || len(ref.TypeArguments) != 1 || ref.TypeArguments[0].Kind() != ir.KindPrimitive {
		t.Fatalf("generic box reference = %#v", box.Type)
	}

	if validationErrors := schema.Validate(); len(validationErrors) != 0 {
		t.Fatalf("schema validation failed for named string-encoded scalars: %v", validationErrors)
	}
}

func TestSourceProvider_JSONWireClassification(t *testing.T) {
	schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes(
			"StringEncodingDepths",
			"DefinedByteSlices",
			"CustomElementByteSlice",
			"AliasResultMarshalers",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONWireClassification(t, schema)
}

func TestSourceProvider_RejectsWireTypesIncompatibleWithGenericConstraints(t *testing.T) {
	for _, test := range []struct {
		root     string
		wireType string
	}{
		{root: "ConstrainedCustomMarshalerPayload", wireType: "unknown"},
		{root: "ConstrainedJSONNumberPayload", wireType: "number"},
		{root: "RejectedDependentCustomMarshalerPayload", wireType: "unknown"},
		{root: "CapturedDependentWire", wireType: "unknown"},
		{root: "RejectedRecursiveWire", wireType: "unknown"},
	} {
		t.Run(test.root, func(t *testing.T) {
			_, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericwire"},
				RootTypes: rootTypes(test.root),
			})
			if err == nil || !strings.Contains(err.Error(), "has JSON wire type "+test.wireType+", incompatible with preserved constraint") {
				t.Fatalf("generic wire constraint error = %v", err)
			}
		})
	}
}

func TestSourceProvider_AllowsTopWireGenericConstraints(t *testing.T) {
	for _, root := range []string{
		"UnconstrainedCustomMarshalerPayload",
		"ExactCustomMarshalerPayload",
		"MethodConstrainedCustomMarshalerPayload",
		"AcceptedDependentCustomMarshalerPayload",
		"ForwardedDependentWire",
		"AliasScopedCustomMarshalerPayload",
		"CompatibleRecursiveWire",
		"ForwardedRecursiveWire",
	} {
		t.Run(root, func(t *testing.T) {
			schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericwire"},
				RootTypes: rootTypes(root),
			})
			if err != nil {
				t.Fatal(err)
			}
			if validationErrors := schema.Validate(); len(validationErrors) != 0 {
				t.Fatalf("schema validation failed: %v", validationErrors)
			}
		})
	}
}

func TestSourceProvider_RejectsStringEncodingOnUnresolvedTypeParameter(t *testing.T) {
	wire, err := json.Marshal(genericstring.GenericStringEncoded[int]{Value: 7})
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"value":"7"}` {
		t.Fatalf("Go JSON wire = %s, want quoted generic integer", wire)
	}

	_, err = (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericstring"},
		RootTypes: rootTypes("GenericStringEncoded"),
	})
	if err == nil || !strings.Contains(err.Error(), `json:",string" on unresolved type parameter T is not supported`) {
		t.Fatalf("generic string-encoding error = %v", err)
	}

	value := 7
	wire, err = json.Marshal(genericstring.GenericPointerAliasStringEncoded[int]{Value: &value})
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"value":"7"}` {
		t.Fatalf("Go pointer-alias JSON wire = %s, want quoted generic integer", wire)
	}
	_, err = (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericstring"},
		RootTypes: rootTypes("GenericPointerAliasStringEncoded"),
	})
	if err == nil || !strings.Contains(err.Error(), `json:",string" on unresolved type parameter T is not supported`) {
		t.Fatalf("generic pointer-alias string-encoding error = %v", err)
	}

	pointer := &value
	wire, err = json.Marshal(genericstring.GenericDoublePointerStringEncoded[int]{Value: &pointer})
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"value":7}` {
		t.Fatalf("Go double-pointer JSON wire = %s, want unquoted generic integer", wire)
	}
	schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericstring"},
		RootTypes: rootTypes("GenericDoublePointerStringEncoded"),
	})
	if err != nil {
		t.Fatal(err)
	}
	field := findFieldByName(findType(schema, "GenericDoublePointerStringEncoded").(*ir.StructDescriptor).Fields, "Value")
	if field.StringEncoded {
		t.Fatal("encoding/json-ignored ,string option was applied through two pointer levels")
	}
}

func TestSourceProvider_RejectsPotentiallyByteEncodedGenericSlice(t *testing.T) {
	wire, err := json.Marshal(genericbytes.Payload{
		Data: genericbytes.GenericBytes[uint8]{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"data":"AQI="}` {
		t.Fatalf("Go JSON wire = %s, want base64 byte-slice encoding", wire)
	}

	methodWire, err := json.Marshal(genericbytes.MethodPayload{
		Data: genericbytes.MethodBytes[genericbytes.Octet]{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(methodWire) != `{"data":"AQI="}` {
		t.Fatalf("method-constrained Go JSON wire = %s, want base64 byte-slice encoding", methodWire)
	}
	structWire, err := json.Marshal(genericbytes.ConcreteStructPayload{
		Value: genericbytes.StructPayload[uint8]{Data: []uint8{1, 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(structWire) != `{"value":{"data":"AQI="}}` {
		t.Fatalf("generic struct Go JSON wire = %s, want nested base64 byte-slice encoding", structWire)
	}
	mixedWire, err := json.Marshal(genericbytes.MixedResponse{
		Value: genericbytes.MixedContainer[uint8, []uint8]{Values: []uint8{1, 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(mixedWire) != `{"value":{"values":"AQI="}}` {
		t.Fatalf("mixed generic constraint Go JSON wire = %s, want nested base64 byte-slice encoding", mixedWire)
	}

	for _, root := range []string{
		"GenericBytes", "Payload", "AnyList", "MethodPayload",
		"StructPayload", "ConcreteStructPayload", "NestedPayload", "ConcreteAnyStructPayload",
	} {
		t.Run(root, func(t *testing.T) {
			_, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericbytes"},
				RootTypes: rootTypes(root),
			})
			if err == nil || !strings.Contains(err.Error(), "base64 byte") {
				t.Fatalf("generic byte-slice error = %v", err)
			}
		})
	}

	t.Run("generic struct remains supported until byte instantiation", func(t *testing.T) {
		schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
			Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericbytes"},
			RootTypes: rootTypes("AnyStruct"),
		})
		if err != nil {
			t.Fatalf("BuildSchema failed: %v", err)
		}
		if findType(schema, "AnyStruct") == nil {
			t.Fatal("AnyStruct was not extracted")
		}
	})

	for _, root := range []string{"StringList", "CustomSlice"} {
		t.Run(root+" remains supported", func(t *testing.T) {
			schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericbytes"},
				RootTypes: rootTypes(root),
			})
			if err != nil {
				t.Fatalf("BuildSchema failed: %v", err)
			}
			typeDescriptor := findType(schema, root)
			if typeDescriptor == nil {
				t.Fatalf("%s was not extracted", root)
			}
			if root == "CustomSlice" {
				alias, ok := typeDescriptor.(*ir.AliasDescriptor)
				if !ok {
					t.Fatalf("custom generic slice = %#v, want an alias", typeDescriptor)
				}
				primitive, ok := alias.Underlying.(*ir.PrimitiveDescriptor)
				if !ok || primitive.PrimitiveKind != ir.PrimitiveAny {
					t.Fatalf("custom generic slice = %#v, want unknown alias", typeDescriptor)
				}
			}
		})
	}

	_, err = (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericbytes"},
		RootTypes: rootTypes("MixedResponse"),
	})
	if err == nil || !strings.Contains(err.Error(), "JSON wire type string") {
		t.Fatalf("mixed byte-compatible application error = %v", err)
	}
	schema, err := (&SourceProvider{}).BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata/genericbytes"},
		RootTypes: rootTypes("SafeMixedContainer"),
	})
	if err != nil {
		t.Fatalf("byte-excluding mixed constraint schema failed: %v", err)
	}
	safe := findType(schema, "SafeMixedContainer").(*ir.StructDescriptor)
	union, ok := safe.TypeParameters[1].Constraint.(*ir.UnionDescriptor)
	if !ok || len(union.Types) != 2 {
		t.Fatalf("byte-excluding mixed constraint = %#v, want both alternatives", safe.TypeParameters[1].Constraint)
	}
}

func assertJSONWireClassification(t *testing.T, schema *ir.Schema) {
	t.Helper()
	if validationErrors := schema.Validate(); len(validationErrors) != 0 {
		t.Fatalf("schema validation failed: %v", validationErrors)
	}

	depths := findType(schema, "StringEncodingDepths").(*ir.StructDescriptor)
	value := 1
	defined := testdata.DefinedIntPointer(&value)
	probeType := reflect.StructOf([]reflect.StructField{{
		Name: "Value", Type: reflect.TypeOf(defined), Tag: `json:"value,string"`,
	}})
	probe := reflect.New(probeType).Elem()
	probe.Field(0).Set(reflect.ValueOf(defined))
	wire, err := json.Marshal(probe.Interface())
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(wire, &actual); err != nil {
		t.Fatal(err)
	}
	_, definedStringEncoded := actual["value"].(string)
	for name, want := range map[string]bool{
		"Direct": true, "Single": true, "Double": false,
		"Triple": false, "Duration": true, "Defined": definedStringEncoded,
	} {
		field := findFieldByName(depths.Fields, name)
		if field == nil || field.StringEncoded != want {
			t.Errorf("StringEncodingDepths.%s StringEncoded = %v, want %v", name, field != nil && field.StringEncoded, want)
		}
	}
	if field := findFieldByName(depths.Fields, "Double"); field.RawTags["json"] != "double,string" {
		t.Fatalf("ignored ,string option was not preserved in RawTags: %#v", field.RawTags)
	}

	bytesField := findFieldByName(findType(schema, "DefinedByteSlices").(*ir.StructDescriptor).Fields, "Data")
	primitive, ok := bytesField.Type.(*ir.PrimitiveDescriptor)
	if !ok || primitive.PrimitiveKind != ir.PrimitiveBytes {
		t.Fatalf("[]Octet descriptor = %#v, want PrimitiveBytes", bytesField.Type)
	}
	customField := findFieldByName(findType(schema, "CustomElementByteSlice").(*ir.StructDescriptor).Fields, "Data")
	if _, ok := customField.Type.(*ir.ArrayDescriptor); !ok {
		t.Fatalf("[]MarshaledOctet descriptor = %#v, want an array descriptor", customField.Type)
	}

	marshalers := findType(schema, "AliasResultMarshalers").(*ir.StructDescriptor)
	for _, name := range []string{"JSONValue", "JSONPointer", "TextValue", "TextPointer"} {
		field := findFieldByName(marshalers.Fields, name)
		fieldType := field.Type
		if pointer, ok := fieldType.(*ir.PtrDescriptor); ok {
			fieldType = pointer.Element
		}
		primitive, ok := fieldType.(*ir.PrimitiveDescriptor)
		if !ok || primitive.PrimitiveKind != ir.PrimitiveAny {
			t.Errorf("AliasResultMarshalers.%s = %#v, want PrimitiveAny", name, field.Type)
		}
	}
}

func TestSourceProvider_PackageSelectorAndAliasRoot(t *testing.T) {
	provider := &SourceProvider{}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, selector := range []string{
		"./testdata",
		filepath.Join(cwd, "testdata"),
		"tygor.dev/tygorgen/provider/testdata",
	} {
		t.Run(selector, func(t *testing.T) {
			schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
				Packages:  []string{selector},
				RootTypes: []RootType{{Package: selector, Name: "User"}},
			})
			if err != nil {
				t.Fatalf("BuildSchema failed: %v", err)
			}
			if schema.Package.Path != "tygor.dev/tygorgen/provider/testdata" || findType(schema, "User") == nil {
				t.Fatalf("resolved package = %#v", schema.Package)
			}
		})
	}

	_, err = provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AliasLevel1"),
	})
	if err == nil || !strings.Contains(err.Error(), "type alias") {
		t.Fatalf("alias root error = %v", err)
	}
}
