package provider

import (
	"context"
	"testing"

	"tygor.dev/tygorgen/ir"
)

func TestSourceProvider_CustomMarshalers(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("CustomJSONType", "CustomTextType"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have warnings about custom marshalers
	foundWarning := false
	for _, w := range schema.Warnings {
		if w.Code == "CUSTOM_MARSHALER" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Expected warning for custom marshalers")
	}

	// Find CustomJSONType in schema
	customJSON := findType(schema, "CustomJSONType")
	if customJSON == nil {
		t.Fatal("CustomJSONType not found")
	}

	// Should be a struct (not yet processed for custom marshaler)
	// Actually, the custom marshaler is detected at field reference time
	// Let's just verify the types exist
	customText := findType(schema, "CustomTextType")
	if customText == nil {
		t.Fatal("CustomTextType not found")
	}
}

func TestSourceProvider_AllBasicTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("AllBasicTypes"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	basicTypes := findType(schema, "AllBasicTypes")
	if basicTypes == nil {
		t.Fatal("AllBasicTypes not found")
	}

	structDesc, ok := basicTypes.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("AllBasicTypes is not a StructDescriptor, got %T", basicTypes)
	}

	// Verify all basic types are present and correct
	expectedTypes := map[string]struct {
		kind    ir.PrimitiveKind
		bitSize int
	}{
		"Bool":    {ir.PrimitiveBool, 0},
		"Int":     {ir.PrimitiveInt, 0},
		"Int8":    {ir.PrimitiveInt, 8},
		"Int16":   {ir.PrimitiveInt, 16},
		"Int32":   {ir.PrimitiveInt, 32},
		"Int64":   {ir.PrimitiveInt, 64},
		"Uint":    {ir.PrimitiveUint, 0},
		"Uint8":   {ir.PrimitiveUint, 8},
		"Uint16":  {ir.PrimitiveUint, 16},
		"Uint32":  {ir.PrimitiveUint, 32},
		"Uint64":  {ir.PrimitiveUint, 64},
		"Uintptr": {ir.PrimitiveUint, 0},
		"Float32": {ir.PrimitiveFloat, 32},
		"Float64": {ir.PrimitiveFloat, 64},
		"String":  {ir.PrimitiveString, 0},
	}

	for fieldName, expected := range expectedTypes {
		field := findFieldByName(structDesc.Fields, fieldName)
		if field == nil {
			t.Errorf("Field %s not found", fieldName)
			continue
		}

		if field.Type.Kind() != ir.KindPrimitive {
			t.Errorf("Field %s should be KindPrimitive, got %v", fieldName, field.Type.Kind())
			continue
		}

		primDesc := field.Type.(*ir.PrimitiveDescriptor)
		if primDesc.PrimitiveKind != expected.kind {
			t.Errorf("Field %s should be %v, got %v", fieldName, expected.kind, primDesc.PrimitiveKind)
		}
		if primDesc.BitSize != expected.bitSize {
			t.Errorf("Field %s should have BitSize=%d, got %d", fieldName, expected.bitSize, primDesc.BitSize)
		}
	}
}

func TestSourceProvider_EnumVariations(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("EnumFloat", "EnumBool"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Check EnumFloat
	enumFloat := findType(schema, "EnumFloat")
	if enumFloat == nil {
		t.Fatal("EnumFloat not found")
	}

	if enumFloat.Kind() != ir.KindEnum {
		t.Errorf("EnumFloat should be KindEnum, got %v", enumFloat.Kind())
	}

	floatEnum := enumFloat.(*ir.EnumDescriptor)
	if len(floatEnum.Members) != 3 {
		t.Errorf("EnumFloat should have 3 members, got %d", len(floatEnum.Members))
	}

	// Verify float values
	for _, member := range floatEnum.Members {
		if _, ok := member.Value.(float64); !ok {
			t.Errorf("EnumFloat member %s should have float64 value, got %T", member.Name, member.Value)
		}
	}

	// Check EnumBool
	enumBool := findType(schema, "EnumBool")
	if enumBool == nil {
		t.Fatal("EnumBool not found")
	}

	if enumBool.Kind() != ir.KindEnum {
		t.Errorf("EnumBool should be KindEnum, got %v", enumBool.Kind())
	}

	boolEnum := enumBool.(*ir.EnumDescriptor)
	if len(boolEnum.Members) != 2 {
		t.Errorf("EnumBool should have 2 members, got %d", len(boolEnum.Members))
	}
}

func TestSourceProvider_MapWithTextMarshalerKey(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("MapWithTextMarshalerKey"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	mapType := findType(schema, "MapWithTextMarshalerKey")
	if mapType == nil {
		t.Fatal("MapWithTextMarshalerKey not found")
	}

	mapStruct, ok := mapType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("MapWithTextMarshalerKey is not a StructDescriptor, got %T", mapType)
	}

	dataField := findFieldByName(mapStruct.Fields, "Data")
	if dataField == nil {
		t.Fatal("Data field not found")
	}

	// Should be a map
	if dataField.Type.Kind() != ir.KindMap {
		t.Errorf("Data field should be KindMap, got %v", dataField.Type.Kind())
	}
}

func TestSourceProvider_NonexistentType(t *testing.T) {
	provider := &SourceProvider{}
	_, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("NonexistentType"),
	})

	if err == nil {
		t.Error("Expected error for nonexistent type")
	}
}

func TestSourceProvider_ConstrainedGeneric(t *testing.T) {
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

	wrapperStruct, ok := wrapperType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Wrapper is not a StructDescriptor, got %T", wrapperType)
	}

	// Should have a constrained type parameter
	if len(wrapperStruct.TypeParameters) != 1 {
		t.Fatalf("Wrapper should have 1 type parameter, got %d", len(wrapperStruct.TypeParameters))
	}

	tp := wrapperStruct.TypeParameters[0]
	if tp.ParamName != "T" {
		t.Errorf("expected type parameter name T, got %s", tp.ParamName)
	}

	// The Stringish constraint (~string | ~int) should be extracted as a union
	if tp.Constraint == nil {
		t.Fatal("expected constraint to be non-nil for Stringish constraint")
	}

	unionDesc, ok := tp.Constraint.(*ir.UnionDescriptor)
	if !ok {
		t.Fatalf("expected UnionDescriptor for Stringish constraint, got %T", tp.Constraint)
	}

	if len(unionDesc.Types) != 2 {
		t.Fatalf("expected 2 union types in Stringish, got %d", len(unionDesc.Types))
	}

	// Verify union contains string and int
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
		t.Error("Stringish constraint should include string")
	}
	if !hasInt {
		t.Error("Stringish constraint should include int")
	}
}

func TestSourceProvider_BuildSchemaErrors(t *testing.T) {
	provider := &SourceProvider{}

	// Test with package that has errors
	_, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages: []string{"github.com/nonexistent/broken/package"},
	})

	if err == nil {
		t.Error("Expected error for package with errors")
	}
}

func TestSourceProvider_NestedStructRef(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("NestedStruct"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	nestedType := findType(schema, "NestedStruct")
	if nestedType == nil {
		t.Fatal("NestedStruct not found")
	}

	nestedStruct, ok := nestedType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("NestedStruct is not a StructDescriptor, got %T", nestedType)
	}

	// User field should be a reference
	userField := findFieldByName(nestedStruct.Fields, "User")
	if userField == nil {
		t.Fatal("User field not found")
	}

	if userField.Type.Kind() != ir.KindReference {
		t.Errorf("User field should be KindReference, got %v", userField.Type.Kind())
	}

	refDesc := userField.Type.(*ir.ReferenceDescriptor)
	if refDesc.Target.Name != "User" {
		t.Errorf("User field should reference User type, got %s", refDesc.Target.Name)
	}

	// Verify that the referenced types are actually in the schema
	// This tests the fix for nested type extraction
	userType := findType(schema, "User")
	if userType == nil {
		t.Error("User type should be extracted into schema when referenced by NestedStruct")
	}

	simpleType := findType(schema, "SimpleStruct")
	if simpleType == nil {
		t.Error("SimpleStruct should be extracted into schema when referenced by NestedStruct")
	}
}
