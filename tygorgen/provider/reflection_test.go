package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider/testdata"
	"tygor.dev/tygorgen/provider/testdata/anona"
	"tygor.dev/tygorgen/provider/testdata/anonb"
	"tygor.dev/tygorgen/provider/testdata/jsonv2invalid"
)

// Test types for comprehensive coverage

type SimpleStruct struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email,omitempty"`
}

type AllPrimitives struct {
	Bool    bool    `json:"bool"`
	Int     int     `json:"int"`
	Int8    int8    `json:"int8"`
	Int16   int16   `json:"int16"`
	Int32   int32   `json:"int32"`
	Int64   int64   `json:"int64"`
	Uint    uint    `json:"uint"`
	Uint8   uint8   `json:"uint8"`
	Uint16  uint16  `json:"uint16"`
	Uint32  uint32  `json:"uint32"`
	Uint64  uint64  `json:"uint64"`
	Uintptr uintptr `json:"uintptr"`
	Float32 float32 `json:"float32"`
	Float64 float64 `json:"float64"`
	String  string  `json:"string"`
	// unexported string  // Should be skipped (commented out to pass linter)
}

type PointerFields struct {
	PtrString *string `json:"ptr_string"`
	PtrInt    *int    `json:"ptr_int,omitempty"`
}

type SliceAndArray struct {
	Slice     []string `json:"slice"`
	SliceOmit []int    `json:"slice_omit,omitempty"`
	Array     [3]int   `json:"array"`
	ByteSlice []byte   `json:"bytes"`
	ByteArray [16]byte `json:"byte_array"`
}

type MapTypes struct {
	StringMap map[string]int     `json:"string_map"`
	IntMap    map[int]string     `json:"int_map"`
	FloatMap  map[float64]string `json:"float_map"`
	MapOmit   map[string]string  `json:"map_omit,omitempty"`
}

type JSONNumberAlias = json.Number
type DefinedJSONNumber json.Number

type SpecialTypes struct {
	Time              time.Time              `json:"time"`
	JSONNumber        json.Number            `json:"json_number"`
	JSONNumberString  json.Number            `json:"json_number_string,string"`
	JSONNumberAlias   JSONNumberAlias        `json:"json_number_alias"`
	DefinedJSONNumber DefinedJSONNumber      `json:"defined_json_number"`
	JSONNumberMap     map[json.Number]string `json:"json_number_map"`
	RawMessage        json.RawMessage        `json:"raw_message"`
	AnyInterface      interface{}            `json:"any"`
	EmptyStruct       struct{}               `json:"empty"`
}

type EmbeddedNoTag struct {
	EmbeddedField string `json:"embedded_field"`
}

type EmbeddedWithTag struct {
	TaggedField string `json:"tagged_field"`
}

type EmbeddingStruct struct {
	EmbeddedNoTag                 // Flattened (Extends)
	Nested        EmbeddedWithTag `json:"nested"` // Nested (Fields)
	OwnField      string          `json:"own_field"`
}

type RecursiveStruct struct {
	Name  string           `json:"name"`
	Child *RecursiveStruct `json:"child,omitempty"`
}

type MutuallyRecursive1 struct {
	Name  string              `json:"name"`
	Other *MutuallyRecursive2 `json:"other,omitempty"`
}

type MutuallyRecursive2 struct {
	Value string              `json:"value"`
	Back  *MutuallyRecursive1 `json:"back,omitempty"`
}

type WithAnonymousStruct struct {
	Inner struct {
		X int    `json:"x"`
		Y string `json:"y"`
	} `json:"inner"`
	Name string `json:"name"`
}

type NestedAnonymous struct {
	Level1 struct {
		Level2 struct {
			Value string `json:"value"`
		} `json:"level2"`
	} `json:"level1"`
}

type StringEncoded struct {
	NumberAsString int `json:"number,string"`
}

type InvalidStringEncoded struct {
	BoolAsString bool `json:"bool,string"`
}

type InvalidDuration struct {
	Duration time.Duration `json:"duration"`
}

type InvalidFormat struct {
	Value []byte `json:"value,format:base64"`
}

type InvalidEmbed struct {
	Value struct{ X int } `json:",embed"`
}

type ValidateTags struct {
	Email    string `json:"email" validate:"required,email"`
	Age      int    `json:"age" validate:"gte=0,lte=120"`
	Password string `json:"password" validate:"required,min=8"`
}

type CustomTags struct {
	Field1 string `json:"field1" db:"field_1" xml:"Field1"`
	Field2 int    `json:"field2" schema:"field2" validate:"required"`
}

type SkipFields struct {
	Included string `json:"included"`
	Skipped  string `json:"-"`
	//lint:ignore SA5008 Intentionally malformed to verify only an exact "-" tag skips a field.
	AlsoSkip string `json:"-,"`
}

type OmitZero struct {
	Field1 string `json:"field1,omitzero"`
	Field2 int    `json:"field2,omitempty"`
}

type GenericResponse[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error,omitempty"`
}

type ConcreteGeneric struct {
	Response GenericResponse[string] `json:"response"`
}

type GenericList[T any] []T
type GenericLookup[T any] map[string]T

type ConcreteGenericContainers struct {
	List   GenericList[string] `json:"list"`
	Lookup GenericLookup[int]  `json:"lookup"`
}

type StringAlias string

type IntAlias int

type reflectionRecursivePointerAlias *reflectionRecursivePointerAlias

type AliasStruct struct {
	StringAlias StringAlias `json:"string_alias"`
	IntAlias    IntAlias    `json:"int_alias"`
}

// Test functions

func TestReflectionProvider_SimpleStruct(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(SimpleStruct{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	if len(schema.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(schema.Types))
	}

	structDesc, ok := schema.Types[0].(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("expected StructDescriptor, got %T", schema.Types[0])
	}

	if structDesc.Name.Name != "SimpleStruct" {
		t.Errorf("expected name 'SimpleStruct', got %q", structDesc.Name.Name)
	}

	if len(structDesc.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(structDesc.Fields))
	}

	// Check fields
	nameField := structDesc.Fields[0]
	if nameField.Name != "Name" || nameField.JSONName != "name" {
		t.Errorf("unexpected Name field: %+v", nameField)
	}

	emailField := structDesc.Fields[2]
	if !schema.FieldOptional(emailField) {
		t.Error("Email field should be optional")
	}
}

func TestReflectionProvider_AllPrimitives(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(AllPrimitives{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	// Should have 15 fields (all exported primitives)
	if len(structDesc.Fields) != 15 {
		t.Errorf("expected 15 fields, got %d", len(structDesc.Fields))
	}

	// Verify some specific types
	boolField := findField(structDesc.Fields, "Bool")
	if boolField == nil {
		t.Fatal("Bool field not found")
	}
	if boolField.Type.Kind() != ir.KindPrimitive {
		t.Errorf("Bool field should be primitive")
	}
	primDesc := boolField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveBool {
		t.Errorf("Bool field should be PrimitiveBool")
	}

	int32Field := findField(structDesc.Fields, "Int32")
	if int32Field == nil {
		t.Fatal("Int32 field not found")
	}
	primDesc = int32Field.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveInt || primDesc.BitSize != 32 {
		t.Errorf("Int32 field should be PrimitiveInt with BitSize 32, got kind=%v bitsize=%d", primDesc.PrimitiveKind, primDesc.BitSize)
	}
}

func TestReflectionProvider_PointerFields(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(PointerFields{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	ptrStringField := findField(structDesc.Fields, "PtrString")
	if ptrStringField == nil {
		t.Fatal("PtrString field not found")
	}

	if ptrStringField.Type.Kind() != ir.KindPtr {
		t.Errorf("PtrString should be KindPtr, got %v", ptrStringField.Type.Kind())
	}

	ptrIntField := findField(structDesc.Fields, "PtrInt")
	if !schema.FieldOptional(*ptrIntField) {
		t.Error("PtrInt should be optional")
	}
}

func TestReflectionProvider_SliceAndArray(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(SliceAndArray{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	sliceField := findField(structDesc.Fields, "Slice")
	if sliceField.Type.Kind() != ir.KindArray {
		t.Errorf("Slice should be KindArray, got %v", sliceField.Type.Kind())
	}
	arrayDesc := sliceField.Type.(*ir.ArrayDescriptor)
	if arrayDesc.Length != 0 {
		t.Errorf("Slice should have Length 0, got %d", arrayDesc.Length)
	}

	arrayField := findField(structDesc.Fields, "Array")
	arrayDesc = arrayField.Type.(*ir.ArrayDescriptor)
	if arrayDesc.Length != 3 {
		t.Errorf("Array should have Length 3, got %d", arrayDesc.Length)
	}

	byteSliceField := findField(structDesc.Fields, "ByteSlice")
	if byteSliceField.Type.Kind() != ir.KindPrimitive {
		t.Errorf("ByteSlice should be KindPrimitive, got %v", byteSliceField.Type.Kind())
	}
	primDesc := byteSliceField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveBytes {
		t.Errorf("ByteSlice should be PrimitiveBytes")
	}

	// ByteArray uses v2's base64 representation and retains its decoded length.
	byteArrayField := findField(structDesc.Fields, "ByteArray")
	byteArray, ok := byteArrayField.Type.(*ir.PrimitiveDescriptor)
	if !ok || byteArray.PrimitiveKind != ir.PrimitiveBytes || byteArray.ByteArrayLength == nil || *byteArray.ByteArrayLength != 16 {
		t.Errorf("ByteArray should be a 16-byte primitive, got %#v", byteArrayField.Type)
	}
}

func TestReflectionProvider_MapTypes(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(MapTypes{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	stringMapField := findField(structDesc.Fields, "StringMap")
	if stringMapField.Type.Kind() != ir.KindMap {
		t.Errorf("StringMap should be KindMap, got %v", stringMapField.Type.Kind())
	}

	mapOmitField := findField(structDesc.Fields, "MapOmit")
	if !schema.FieldOptional(*mapOmitField) {
		t.Error("MapOmit should be optional")
	}
}

func TestReflectionProvider_SpecialTypes(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(SpecialTypes{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	specialType := findType(schema, "SpecialTypes")
	structDesc, ok := specialType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("SpecialTypes should be a StructDescriptor, got %T", specialType)
	}

	timeField := findField(structDesc.Fields, "Time")
	primDesc := timeField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveTime {
		t.Errorf("Time should be PrimitiveTime")
	}

	jsonNumberField := findField(structDesc.Fields, "JSONNumber")
	primDesc = jsonNumberField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveFloat || primDesc.BitSize != 64 {
		t.Errorf("JSONNumber should be PrimitiveFloat(64), got %#v", primDesc)
	}

	_, err = provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(InvalidDuration{})},
	})
	if err == nil || !strings.Contains(err.Error(), "no default encoding/json/v2 representation") {
		t.Fatalf("duration error = %v", err)
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[time.Duration](), reflect.TypeFor[*time.Duration]()} {
		_, err = provider.BuildSchema(context.Background(), ReflectionInputOptions{RootTypes: []reflect.Type{typ}})
		if err == nil || !strings.Contains(err.Error(), "no default encoding/json/v2 representation") {
			t.Fatalf("duration root %v error = %v", typ, err)
		}
	}
	_, err = provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(InvalidFormat{})},
	})
	if err == nil || !strings.Contains(err.Error(), `does not support "format:base64"`) {
		t.Fatalf("format error = %v", err)
	}
	_, err = provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(InvalidEmbed{})},
	})
	if err == nil || !strings.Contains(err.Error(), "explicit encoding/json/v2 embed fields are not supported") {
		t.Fatalf("embed error = %v", err)
	}

	jsonNumberStringField := findField(structDesc.Fields, "JSONNumberString")
	primDesc = jsonNumberStringField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveFloat || primDesc.BitSize != 64 || !jsonNumberStringField.StringEncoded {
		t.Errorf("JSONNumberString should be string-encoded PrimitiveFloat(64), got field %#v", jsonNumberStringField)
	}

	jsonNumberAliasField := findField(structDesc.Fields, "JSONNumberAlias")
	primDesc = jsonNumberAliasField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveFloat || primDesc.BitSize != 64 {
		t.Errorf("JSONNumberAlias should be PrimitiveFloat(64), got %#v", primDesc)
	}

	jsonNumberMapField := findField(structDesc.Fields, "JSONNumberMap")
	mapDesc := jsonNumberMapField.Type.(*ir.MapDescriptor)
	mapKey := mapDesc.Key.(*ir.PrimitiveDescriptor)
	if mapKey.PrimitiveKind != ir.PrimitiveString {
		t.Errorf("JSONNumberMap key should be PrimitiveString, got %#v", mapKey)
	}

	definedNumberField := findField(structDesc.Fields, "DefinedJSONNumber")
	definedRef, ok := definedNumberField.Type.(*ir.ReferenceDescriptor)
	if !ok || definedRef.Target.Name != "DefinedJSONNumber" {
		t.Fatalf("DefinedJSONNumber field should reference DefinedJSONNumber, got %#v", definedNumberField.Type)
	}
	definedAlias, ok := findType(schema, "DefinedJSONNumber").(*ir.AliasDescriptor)
	if !ok {
		t.Fatal("DefinedJSONNumber should be an AliasDescriptor")
	}
	definedUnderlying, ok := definedAlias.Underlying.(*ir.PrimitiveDescriptor)
	if !ok || definedUnderlying.PrimitiveKind != ir.PrimitiveString {
		t.Errorf("DefinedJSONNumber should remain string-backed, got %#v", definedAlias.Underlying)
	}

	rawMessageField := findField(structDesc.Fields, "RawMessage")
	primDesc = rawMessageField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveAny {
		t.Errorf("RawMessage should be PrimitiveAny")
	}

	anyField := findField(structDesc.Fields, "AnyInterface")
	primDesc = anyField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveAny {
		t.Errorf("AnyInterface should be PrimitiveAny")
	}

	emptyField := findField(structDesc.Fields, "EmptyStruct")
	primDesc = emptyField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveEmpty {
		t.Errorf("EmptyStruct should be PrimitiveEmpty")
	}
}

func TestReflectionProvider_Embedding(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(EmbeddingStruct{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// The promoted base is flattened; only referenced nested types are extracted.
	if len(schema.Types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(schema.Types))
	}

	var embeddingStruct *ir.StructDescriptor
	for _, typ := range schema.Types {
		if typ.TypeName().Name == "EmbeddingStruct" {
			embeddingStruct = typ.(*ir.StructDescriptor)
			break
		}
	}

	if embeddingStruct == nil {
		t.Fatal("EmbeddingStruct not found")
	}

	if len(embeddingStruct.Extends) != 0 {
		t.Errorf("expected no Extends entries, got %d", len(embeddingStruct.Extends))
	}

	if len(embeddingStruct.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(embeddingStruct.Fields))
	}
	if findField(embeddingStruct.Fields, "EmbeddedField") == nil {
		t.Error("promoted EmbeddedField not found")
	}

	nestedField := findField(embeddingStruct.Fields, "Nested")
	if nestedField == nil {
		t.Fatal("Nested field not found")
	}
	if nestedField.JSONName != "nested" {
		t.Errorf("expected JSONName 'nested', got %q", nestedField.JSONName)
	}
}

func TestReflectionProvider_Recursive(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(RecursiveStruct{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should handle recursion gracefully with cycle detection
	if len(schema.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(schema.Types))
	}

	// Should have a warning about cycle detection
	if len(schema.Warnings) == 0 {
		t.Error("expected cycle detection warning")
	}
}

func TestReflectionProvider_MutualRecursion(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{
			reflect.TypeOf(MutuallyRecursive1{}),
			reflect.TypeOf(MutuallyRecursive2{}),
		},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should handle mutual recursion
	if len(schema.Types) != 2 {
		t.Errorf("expected 2 types, got %d", len(schema.Types))
	}
}

func TestReflectionProvider_AnonymousStruct(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(WithAnonymousStruct{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have 2 types: WithAnonymousStruct and synthetic name for Inner
	if len(schema.Types) != 2 {
		t.Errorf("expected 2 types, got %d", len(schema.Types))
	}

	var mainStruct *ir.StructDescriptor
	var anonStruct *ir.StructDescriptor

	for _, typ := range schema.Types {
		structDesc := typ.(*ir.StructDescriptor)
		if structDesc.Name.Name == "WithAnonymousStruct" {
			mainStruct = structDesc
		} else {
			anonStruct = structDesc
		}
	}

	if mainStruct == nil {
		t.Fatal("WithAnonymousStruct not found")
	}
	if anonStruct == nil {
		t.Fatal("Anonymous struct not found")
	}

	// Check that Inner field references the synthetic type
	innerField := findField(mainStruct.Fields, "Inner")
	if innerField == nil {
		t.Fatal("Inner field not found")
	}

	if innerField.Type.Kind() != ir.KindReference {
		t.Errorf("Inner field should be KindReference, got %v", innerField.Type.Kind())
	}
}

func TestReflectionProvider_StringEncoded(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(StringEncoded{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	numberField := findField(structDesc.Fields, "NumberAsString")
	if !numberField.StringEncoded {
		t.Error("NumberAsString should have StringEncoded=true")
	}

	_, err = provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(InvalidStringEncoded{})},
	})
	if err == nil || !strings.Contains(err.Error(), "only applies to numeric types") {
		t.Fatalf("invalid bool ,string error = %v", err)
	}
}

func TestReflectionProvider_ValidateTags(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(ValidateTags{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	emailField := findField(structDesc.Fields, "Email")
	if emailField.ValidateTag != "required,email" {
		t.Errorf("expected ValidateTag 'required,email', got %q", emailField.ValidateTag)
	}

	ageField := findField(structDesc.Fields, "Age")
	if ageField.ValidateTag != "gte=0,lte=120" {
		t.Errorf("expected ValidateTag 'gte=0,lte=120', got %q", ageField.ValidateTag)
	}
}

func TestReflectionProvider_CustomTags(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(CustomTags{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	field1 := findField(structDesc.Fields, "Field1")
	if field1.RawTags["db"] != "field_1" {
		t.Errorf("expected db tag 'field_1', got %q", field1.RawTags["db"])
	}
	if field1.RawTags["xml"] != "Field1" {
		t.Errorf("expected xml tag 'Field1', got %q", field1.RawTags["xml"])
	}

	field2 := findField(structDesc.Fields, "Field2")
	if field2.RawTags["schema"] != "field2" {
		t.Errorf("expected schema tag 'field2', got %q", field2.RawTags["schema"])
	}
}

func TestReflectionProvider_SkipFields(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(SkipFields{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	// Should have Included and AlsoSkip (which becomes "-")
	// Skipped should be skipped
	if len(structDesc.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(structDesc.Fields))
	}

	includedField := findField(structDesc.Fields, "Included")
	if includedField == nil {
		t.Error("Included field not found")
	}

	// AlsoSkip should become a field named "-"
	alsoSkipField := findField(structDesc.Fields, "AlsoSkip")
	if alsoSkipField == nil {
		t.Error("AlsoSkip field not found")
	} else if alsoSkipField.JSONName != "-" {
		t.Errorf("expected JSONName '-', got %q", alsoSkipField.JSONName)
	}

	// Skipped should not exist
	skippedField := findField(structDesc.Fields, "Skipped")
	if skippedField != nil {
		t.Error("Skipped field should not be present")
	}
}

func TestReflectionProvider_OmitZero(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(OmitZero{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	structDesc := schema.Types[0].(*ir.StructDescriptor)

	field1 := findField(structDesc.Fields, "Field1")
	if !field1.OmitZero {
		t.Error("Field1 with omitzero should be optional")
	}

	field2 := findField(structDesc.Fields, "Field2")
	if !field2.OmitEmpty {
		t.Error("Field2 with omitempty should be optional")
	}
}

func TestReflectionProvider_GenericInstantiation(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(ConcreteGeneric{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have ConcreteGeneric and the instantiated GenericResponse[string]
	if len(schema.Types) < 2 {
		t.Errorf("expected at least 2 types, got %d", len(schema.Types))
	}

	// Check that generic name is sanitized
	var genericFound bool
	for _, typ := range schema.Types {
		name := typ.TypeName().Name
		if strings.Contains(name, "GenericResponse") {
			genericFound = true
			// Name should not contain brackets
			if strings.Contains(name, "[") || strings.Contains(name, "]") {
				t.Errorf("generic name should not contain brackets: %q", name)
			}
		}
	}

	if !genericFound {
		t.Error("GenericResponse type not found")
	}
}

func TestReflectionProvider_GenericContainerIdentity(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(ConcreteGenericContainers{})},
	})
	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}
	if validationErrors := schema.Validate(); len(validationErrors) != 0 {
		t.Fatalf("schema validation failed: %v", validationErrors)
	}

	container := findType(schema, "ConcreteGenericContainers").(*ir.StructDescriptor)
	for _, fieldName := range []string{"List", "Lookup"} {
		field := findField(container.Fields, fieldName)
		ref, ok := field.Type.(*ir.ReferenceDescriptor)
		if !ok {
			t.Fatalf("%s should be a ReferenceDescriptor, got %T", fieldName, field.Type)
		}
		if strings.ContainsAny(ref.Target.Name, "[]") {
			t.Errorf("%s reference name was not sanitized: %q", fieldName, ref.Target.Name)
		}
		decl := findType(schema, ref.Target.Name)
		if decl == nil {
			t.Errorf("%s reference target %q has no declaration", fieldName, ref.Target.Name)
		}
	}
}

func TestReflectionProvider_Aliases(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(AliasStruct{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should have AliasStruct, StringAlias, IntAlias
	if len(schema.Types) != 3 {
		t.Errorf("expected 3 types, got %d", len(schema.Types))
	}

	// Find the alias types
	var stringAlias, intAlias *ir.AliasDescriptor
	for _, typ := range schema.Types {
		if alias, ok := typ.(*ir.AliasDescriptor); ok {
			if alias.Name.Name == "StringAlias" {
				stringAlias = alias
			} else if alias.Name.Name == "IntAlias" {
				intAlias = alias
			}
		}
	}

	if stringAlias == nil {
		t.Error("StringAlias not found")
	} else {
		if stringAlias.Underlying.Kind() != ir.KindPrimitive {
			t.Errorf("StringAlias should have primitive underlying type")
		}
	}

	if intAlias == nil {
		t.Error("IntAlias not found")
	}
}

func TestReflectionProvider_UnsupportedTypes(t *testing.T) {
	tests := []struct {
		name    string
		typ     reflect.Type
		wantErr string
	}{
		{
			name:    "chan type",
			typ:     reflect.TypeOf(make(chan int)),
			wantErr: "unsupported type: chan",
		},
		{
			name:    "complex64",
			typ:     reflect.TypeOf(complex64(0)),
			wantErr: "unsupported type: complex64",
		},
		{
			name:    "func type",
			typ:     reflect.TypeOf(func() {}),
			wantErr: "unsupported type: func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a struct with the unsupported field
			structType := reflect.StructOf([]reflect.StructField{
				{
					Name: "Field",
					Type: tt.typ,
					Tag:  `json:"field"`,
				},
			})

			provider := &ReflectionProvider{}
			_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
				RootTypes: []reflect.Type{structType},
			})

			if err == nil {
				t.Error("expected error, got nil")
			} else if !strings.Contains(err.Error(), "unsupported type") {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestReflectionProvider_UnsupportedMapKeys(t *testing.T) {
	tests := []struct {
		name    string
		keyType reflect.Type
		wantErr string
	}{
		{
			name:    "bool key",
			keyType: reflect.TypeOf(true),
			wantErr: "unsupported map key type: bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapType := reflect.MapOf(tt.keyType, reflect.TypeOf(""))
			structType := reflect.StructOf([]reflect.StructField{
				{
					Name: "Field",
					Type: mapType,
					Tag:  `json:"field"`,
				},
			})

			provider := &ReflectionProvider{}
			_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
				RootTypes: []reflect.Type{structType},
			})

			if err == nil {
				t.Error("expected error, got nil")
			} else if !strings.Contains(err.Error(), "unsupported map key type") {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestReflectionProvider_NoRootTypes(t *testing.T) {
	provider := &ReflectionProvider{}
	_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: nil,
	})

	if err == nil {
		t.Error("expected error for no root types")
	}
}

func TestReflectionProvider_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	provider := &ReflectionProvider{}
	_, err := provider.BuildSchema(ctx, ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(SimpleStruct{})},
	})

	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestParseJSONTag(t *testing.T) {
	tests := []struct {
		tag           string
		fieldName     string
		wantJSON      string
		wantOptional  bool
		wantSkip      bool
		wantStringEnc bool
	}{
		{"", "Field", "Field", false, false, false},
		{"name", "Field", "name", false, false, false},
		{"name,omitempty", "Field", "name", true, false, false},
		{"name,omitzero", "Field", "name", true, false, false},
		{"name,string", "Field", "name", false, false, true},
		{"name,omitempty,string", "Field", "name", true, false, true},
		{"-", "Field", "", false, true, false},    // Skip field
		{"-,", "Field", "-", false, false, false}, // Field named "-"
		{",omitempty", "Field", "Field", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			jsonName, omitEmpty, omitZero, skip, stringEnc, format := parseJSONTag(tt.tag, tt.fieldName)
			optional := omitEmpty || omitZero

			if jsonName != tt.wantJSON {
				t.Errorf("jsonName: got %q, want %q", jsonName, tt.wantJSON)
			}
			if optional != tt.wantOptional {
				t.Errorf("optional: got %v, want %v", optional, tt.wantOptional)
			}
			if skip != tt.wantSkip {
				t.Errorf("skip: got %v, want %v", skip, tt.wantSkip)
			}
			if stringEnc != tt.wantStringEnc {
				t.Errorf("stringEncoded: got %v, want %v", stringEnc, tt.wantStringEnc)
			}
			if format != "" {
				t.Errorf("format: got %q, want empty", format)
			}
		})
	}
}

func TestGenerateSyntheticName(t *testing.T) {
	b := &reflectionSchemaBuilder{}

	tests := []struct {
		input string
		want  string
	}{
		{"Response[User]", "Response_User"},
		{"Map[string, int]", "Map_string_int"},
		{"Response[pkg.User]", "Response_pkg_User"},
		{"Nested[Outer[Inner]]", "Nested_Outer_Inner"},
		{"Pair[*Foo, Bar]", "Pair_PtrFoo_Bar"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := b.generateSyntheticName(tt.input)
			if got != tt.want {
				t.Errorf("generateSyntheticName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Test types for comprehensive coverage of typeToDescriptorForAlias

type SliceAlias []int
type ArrayAlias [5]string
type MapAlias map[string]int
type StructAlias SimpleStruct
type PtrAlias *SimpleStruct
type ByteSliceAlias []byte

type AliasCollection struct {
	SliceField     SliceAlias     `json:"slice_field"`
	ArrayField     ArrayAlias     `json:"array_field"`
	MapField       MapAlias       `json:"map_field"`
	StructField    StructAlias    `json:"struct_field"`
	PtrField       PtrAlias       `json:"ptr_field"`
	ByteSliceField ByteSliceAlias `json:"byte_slice_field"`
}

// Test types for validateMapKeyType

// TextMarshalerKey is a struct that implements encoding.TextMarshaler
type TextMarshalerKey struct {
	Value string
}

func (t TextMarshalerKey) MarshalText() ([]byte, error) {
	return []byte(t.Value), nil
}

type NamedStringKey string
type NamedIntKey int

type MapKeyTypes struct {
	TextMarshalerMap map[TextMarshalerKey]string `json:"text_marshaler_map"`
	StringMap        map[string]int              `json:"string_map"`
	IntMap           map[int]string              `json:"int_map"`
	Int8Map          map[int8]string             `json:"int8_map"`
	Int16Map         map[int16]string            `json:"int16_map"`
	Int32Map         map[int32]string            `json:"int32_map"`
	Int64Map         map[int64]string            `json:"int64_map"`
	UintMap          map[uint]string             `json:"uint_map"`
	Uint8Map         map[uint8]string            `json:"uint8_map"`
	Uint16Map        map[uint16]string           `json:"uint16_map"`
	Uint32Map        map[uint32]string           `json:"uint32_map"`
	Uint64Map        map[uint64]string           `json:"uint64_map"`
	NamedStringMap   map[NamedStringKey]int      `json:"named_string_map"`
	NamedIntMap      map[NamedIntKey]string      `json:"named_int_map"`
}

// Test types for handleEmbedded

type EmbeddedUnexported struct {
	ExportedField string `json:"exported_field"`
}

type EmbeddedPtrType struct {
	PtrField string `json:"ptr_field"`
}

type EmbeddingWithPtr struct {
	*EmbeddedPtrType               // Pointer embedding
	OwnField         string        `json:"own_field"`
	Tagged           *SimpleStruct `json:"tagged"` // Pointer embedding with tag
}

type Level3Embed struct {
	L3Field string `json:"l3_field"`
}

type Level2Embed struct {
	Level3Embed
	L2Field string `json:"l2_field"`
}

type Level1Embed struct {
	Level2Embed
	L1Field string `json:"l1_field"`
}

type EmbeddedWithSkipTag struct {
	SkippedField string `json:"skipped_field"`
}

type EmbeddingWithSkip struct {
	EmbeddedWithSkipTag `json:"-"` // Should skip entirely
	OwnField            string     `json:"own_field"`
}

// Test types for anonymous structs and edge cases

type CircularAnonymous struct {
	Name   string `json:"name"`
	Nested struct {
		Value   string             `json:"value"`
		BackRef *CircularAnonymous `json:"back_ref,omitempty"`
	} `json:"nested"`
}

type DeeplyNestedAnonymous struct {
	Level1 struct {
		A      string `json:"a"`
		Level2 struct {
			B      string `json:"b"`
			Level3 struct {
				C      string `json:"c"`
				Level4 struct {
					D string `json:"d"`
				} `json:"level4"`
			} `json:"level3"`
		} `json:"level2"`
	} `json:"level1"`
}

type NamedWithMethods struct {
	Value string `json:"value"`
}

func (n NamedWithMethods) String() string {
	return n.Value
}

type WithNamedMethods struct {
	Field NamedWithMethods `json:"field"`
}

// Test: typeToDescriptorForAlias comprehensive coverage
// Note: The reflection provider currently inlines composite type aliases (slices, arrays, maps)
// rather than extracting them as separate types. These tests verify that when such aliases
// are used as root types, typeToDescriptorForAlias handles them correctly.

func TestReflectionProvider_TypeAliases_Primitives(t *testing.T) {
	provider := &ReflectionProvider{}

	// Test all primitive type aliases
	// Only declaring types that are used in the test table below
	type BoolAlias bool
	type IntAlias int
	type Int32Alias int32
	type UintAlias uint
	type UintptrAlias uintptr
	type Float32Alias float32
	type Float64Alias float64
	type StringAlias string

	tests := []struct {
		name     string
		typ      reflect.Type
		wantKind ir.DescriptorKind
		checkFn  func(*testing.T, ir.TypeDescriptor)
	}{
		{
			name:     "BoolAlias",
			typ:      reflect.TypeOf((*BoolAlias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveBool {
					t.Errorf("expected PrimitiveBool, got %v", prim.PrimitiveKind)
				}
			},
		},
		{
			name:     "IntAlias",
			typ:      reflect.TypeOf((*IntAlias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveInt || prim.BitSize != 0 {
					t.Errorf("expected PrimitiveInt with BitSize 0, got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "Int32Alias",
			typ:      reflect.TypeOf((*Int32Alias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveInt || prim.BitSize != 32 {
					t.Errorf("expected PrimitiveInt with BitSize 32, got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "UintAlias",
			typ:      reflect.TypeOf((*UintAlias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveUint || prim.BitSize != 0 {
					t.Errorf("expected PrimitiveUint with BitSize 0, got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "UintptrAlias",
			typ:      reflect.TypeOf((*UintptrAlias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveUint || prim.BitSize != 0 {
					t.Errorf("expected PrimitiveUint with BitSize 0 (uintptr), got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "Float32Alias",
			typ:      reflect.TypeOf((*Float32Alias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveFloat || prim.BitSize != 32 {
					t.Errorf("expected PrimitiveFloat with BitSize 32, got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "Float64Alias",
			typ:      reflect.TypeOf((*Float64Alias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveFloat || prim.BitSize != 64 {
					t.Errorf("expected PrimitiveFloat with BitSize 64, got kind=%v bitsize=%d", prim.PrimitiveKind, prim.BitSize)
				}
			},
		},
		{
			name:     "StringAlias",
			typ:      reflect.TypeOf((*StringAlias)(nil)).Elem(),
			wantKind: ir.KindPrimitive,
			checkFn: func(t *testing.T, desc ir.TypeDescriptor) {
				prim := desc.(*ir.PrimitiveDescriptor)
				if prim.PrimitiveKind != ir.PrimitiveString {
					t.Errorf("expected PrimitiveString, got %v", prim.PrimitiveKind)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
				RootTypes: []reflect.Type{tt.typ},
			})

			if err != nil {
				t.Fatalf("BuildSchema failed: %v", err)
			}

			if len(schema.Types) != 1 {
				t.Fatalf("expected 1 type, got %d", len(schema.Types))
			}

			alias, ok := schema.Types[0].(*ir.AliasDescriptor)
			if !ok {
				t.Fatalf("expected AliasDescriptor, got %T", schema.Types[0])
			}

			if alias.Underlying.Kind() != tt.wantKind {
				t.Errorf("underlying kind should be %v, got %v", tt.wantKind, alias.Underlying.Kind())
			}

			if tt.checkFn != nil {
				tt.checkFn(t, alias.Underlying)
			}
		})
	}
}

func TestReflectionProvider_TypeAliases_SliceAlias(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf((*SliceAlias)(nil)).Elem()},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// SliceAlias should create an AliasDescriptor with slice underlying type
	if len(schema.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(schema.Types))
	}

	alias, ok := schema.Types[0].(*ir.AliasDescriptor)
	if !ok {
		t.Fatalf("expected AliasDescriptor, got %T", schema.Types[0])
	}

	if alias.Name.Name != "SliceAlias" {
		t.Errorf("expected name 'SliceAlias', got %q", alias.Name.Name)
	}

	if alias.Underlying.Kind() != ir.KindArray {
		t.Errorf("underlying should be KindArray, got %v", alias.Underlying.Kind())
	}

	arrayDesc := alias.Underlying.(*ir.ArrayDescriptor)
	if arrayDesc.Length != 0 {
		t.Errorf("slice should have length 0, got %d", arrayDesc.Length)
	}
}

func TestReflectionProvider_TypeAliases_ArrayAlias(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf((*ArrayAlias)(nil)).Elem()},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	alias, ok := schema.Types[0].(*ir.AliasDescriptor)
	if !ok {
		t.Fatalf("expected AliasDescriptor, got %T", schema.Types[0])
	}

	if alias.Underlying.Kind() != ir.KindArray {
		t.Errorf("underlying should be KindArray, got %v", alias.Underlying.Kind())
	}

	arrayDesc := alias.Underlying.(*ir.ArrayDescriptor)
	if arrayDesc.Length != 5 {
		t.Errorf("array should have length 5, got %d", arrayDesc.Length)
	}
}

func TestReflectionProvider_TypeAliases_MapAlias(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf((*MapAlias)(nil)).Elem()},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	alias, ok := schema.Types[0].(*ir.AliasDescriptor)
	if !ok {
		t.Fatalf("expected AliasDescriptor, got %T", schema.Types[0])
	}

	if alias.Underlying.Kind() != ir.KindMap {
		t.Errorf("underlying should be KindMap, got %v", alias.Underlying.Kind())
	}
}

func TestReflectionProvider_TypeAliases_ByteSliceAlias(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf((*ByteSliceAlias)(nil)).Elem()},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	alias, ok := schema.Types[0].(*ir.AliasDescriptor)
	if !ok {
		t.Fatalf("expected AliasDescriptor, got %T", schema.Types[0])
	}

	// []byte should be PrimitiveBytes
	if alias.Underlying.Kind() != ir.KindPrimitive {
		t.Errorf("underlying should be KindPrimitive, got %v", alias.Underlying.Kind())
	}

	primDesc := alias.Underlying.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveBytes {
		t.Errorf("should be PrimitiveBytes, got %v", primDesc.PrimitiveKind)
	}
}

func TestReflectionProvider_TypeAliases_StructAlias(t *testing.T) {
	provider := &ReflectionProvider{}

	// Test struct alias - StructAlias is an alias to SimpleStruct
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf((*StructAlias)(nil)).Elem()},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// When a struct alias is used as root type, it extracts the struct itself
	// This tests that typeToDescriptorForAlias handles struct types correctly
	if len(schema.Types) < 1 {
		t.Fatalf("expected at least 1 type, got %d", len(schema.Types))
	}

	// Should have StructAlias (the struct keeps its alias name)
	var structAlias *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "StructAlias" {
			structAlias = s
			break
		}
	}

	if structAlias == nil {
		t.Error("StructAlias not found")
	}
}

// Test: validateMapKeyType comprehensive coverage

func TestReflectionProvider_MapKeyTypes_AllIntegers(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(MapKeyTypes{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find MapKeyTypes struct
	var structDesc *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "MapKeyTypes" {
			structDesc = s
			break
		}
	}

	if structDesc == nil {
		t.Fatalf("MapKeyTypes struct not found")
	}

	// Verify all integer key types are accepted
	integerMaps := []string{"IntMap", "Int8Map", "Int16Map", "Int32Map", "Int64Map",
		"UintMap", "Uint8Map", "Uint16Map", "Uint32Map", "Uint64Map"}

	for _, mapName := range integerMaps {
		field := findField(structDesc.Fields, mapName)
		if field == nil {
			t.Errorf("%s field not found", mapName)
			continue
		}
		if field.Type.Kind() != ir.KindMap {
			t.Errorf("%s should be KindMap, got %v", mapName, field.Type.Kind())
		}
	}

	// Verify named string key
	namedStringField := findField(structDesc.Fields, "NamedStringMap")
	if namedStringField == nil {
		t.Error("NamedStringMap field not found")
	}

	// Verify named int key
	namedIntField := findField(structDesc.Fields, "NamedIntMap")
	if namedIntField == nil {
		t.Error("NamedIntMap field not found")
	}
}

func TestReflectionProvider_MapKeyTypes_TextMarshaler(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{
			reflect.TypeOf(MapKeyTypes{}),
			reflect.TypeFor[testdata.AliasResultTextMap](),
		},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find MapKeyTypes struct
	var structDesc *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "MapKeyTypes" {
			structDesc = s
			break
		}
	}

	if structDesc == nil {
		t.Fatalf("MapKeyTypes struct not found")
	}

	// TextMarshaler as map key should be accepted
	tmField := findField(structDesc.Fields, "TextMarshalerMap")
	if tmField == nil {
		t.Fatal("TextMarshalerMap field not found")
	}
	if tmField.Type.Kind() != ir.KindMap {
		t.Errorf("TextMarshalerMap should be KindMap, got %v", tmField.Type.Kind())
	}

	aliasResultMap, ok := findType(schema, "AliasResultTextMap").(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("AliasResultTextMap = %T, want StructDescriptor", findType(schema, "AliasResultTextMap"))
	}
	valuesField := findField(aliasResultMap.Fields, "Values")
	if valuesField == nil {
		t.Fatalf("AliasResultTextMap.Values = %#v, want map", valuesField)
	}
	mapDescriptor, ok := valuesField.Type.(*ir.MapDescriptor)
	if !ok {
		t.Fatalf("AliasResultTextMap.Values = %#v, want map", valuesField.Type)
	}
	key, ok := mapDescriptor.Key.(*ir.PrimitiveDescriptor)
	if !ok || key.PrimitiveKind != ir.PrimitiveAny {
		t.Fatalf("AliasResultTextMap key = %#v, want custom-marshaled unknown", mapDescriptor.Key)
	}
}

func TestReflectionProvider_MapKeyTypes_BoolKey_ShouldFail(t *testing.T) {
	provider := &ReflectionProvider{}

	// Create a struct with bool key map
	mapType := reflect.MapOf(reflect.TypeOf(true), reflect.TypeOf(""))
	structType := reflect.StructOf([]reflect.StructField{
		{
			Name: "BoolMap",
			Type: mapType,
			Tag:  `json:"bool_map"`,
		},
	})

	_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{structType},
	})

	if err == nil {
		t.Error("expected error for bool map key")
	} else if !strings.Contains(err.Error(), "unsupported map key type: bool") {
		t.Errorf("expected 'unsupported map key type: bool', got %q", err.Error())
	}
}

func TestReflectionProvider_MapKeyTypes_StructWithoutTextMarshaler_ShouldFail(t *testing.T) {
	provider := &ReflectionProvider{}

	// Create a struct key type without TextMarshaler
	type PlainStructKey struct {
		Field string
	}

	mapType := reflect.MapOf(reflect.TypeOf(PlainStructKey{}), reflect.TypeOf(""))
	structType := reflect.StructOf([]reflect.StructField{
		{
			Name: "StructMap",
			Type: mapType,
			Tag:  `json:"struct_map"`,
		},
	})

	_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{structType},
	})

	if err == nil {
		t.Error("expected error for struct map key without TextMarshaler")
	} else if !strings.Contains(err.Error(), "unsupported map key type: struct without TextMarshaler") {
		t.Errorf("expected 'unsupported map key type: struct without TextMarshaler', got %q", err.Error())
	}
}

// Test: handleEmbedded comprehensive coverage

func TestReflectionProvider_EmbeddedPointer(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(EmbeddingWithPtr{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find EmbeddingWithPtr struct
	var embeddingStruct *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "EmbeddingWithPtr" {
			embeddingStruct = s
			break
		}
	}

	if embeddingStruct == nil {
		t.Fatal("EmbeddingWithPtr not found")
	}

	promotedField := findField(embeddingStruct.Fields, "PtrField")
	if promotedField == nil {
		t.Fatal("promoted PtrField not found")
	}
	if !schema.FieldOptional(*promotedField) {
		t.Error("field promoted through a pointer embed should be optional")
	}

	// Pointer embedding with tag should be in Fields
	taggedField := findField(embeddingStruct.Fields, "Tagged")
	if taggedField == nil {
		t.Fatal("Tagged field not found")
	}
	if taggedField.JSONName != "tagged" {
		t.Errorf("expected JSONName 'tagged', got %q", taggedField.JSONName)
	}
}

func TestReflectionProvider_EmbeddedUnexported(t *testing.T) {
	provider := &ReflectionProvider{}

	// Test with unexported embedded type
	type unexportedEmbed struct {
		Field string `json:"field"`
	}

	type WithUnexportedEmbed struct {
		unexportedEmbed
		OwnField string `json:"own_field"`
	}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(WithUnexportedEmbed{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// The unexported embedded type should still be processed
	// Check that it's extracted
	if len(schema.Types) < 1 {
		t.Error("expected at least 1 type")
	}
}

func TestReflectionProvider_MultipleLevelEmbedding(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(Level1Embed{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	if len(schema.Types) != 1 {
		t.Errorf("expected only the flattened root type, got %d", len(schema.Types))
	}

	// Find Level1Embed
	var level1 *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "Level1Embed" {
			level1 = s
			break
		}
	}

	if level1 == nil {
		t.Fatal("Level1Embed not found")
	}

	for _, fieldName := range []string{"L1Field", "L2Field", "L3Field"} {
		if findField(level1.Fields, fieldName) == nil {
			t.Errorf("promoted field %s not found", fieldName)
		}
	}
}

func TestReflectionProvider_EmbeddedWithSkipTag(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(EmbeddingWithSkip{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find EmbeddingWithSkip
	var embeddingStruct *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "EmbeddingWithSkip" {
			embeddingStruct = s
			break
		}
	}

	if embeddingStruct == nil {
		t.Fatal("EmbeddingWithSkip not found")
	}

	// Embedded field with json:"-" should not appear in Extends or Fields
	for _, ext := range embeddingStruct.Extends {
		if ext.Name == "EmbeddedWithSkipTag" {
			t.Error("EmbeddedWithSkipTag should not be in Extends (json:\"-\")")
		}
	}

	// Should only have OwnField
	if len(embeddingStruct.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(embeddingStruct.Fields))
	}
	if embeddingStruct.Fields[0].Name != "OwnField" {
		t.Errorf("expected OwnField, got %s", embeddingStruct.Fields[0].Name)
	}
}

// Test: Edge cases

func TestReflectionProvider_CircularAnonymousStruct(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(CircularAnonymous{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should handle circular reference through anonymous struct
	// Check that CircularAnonymous and synthetic type for Nested are created
	if len(schema.Types) < 2 {
		t.Errorf("expected at least 2 types, got %d", len(schema.Types))
	}

	// Find the main struct
	var mainStruct *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "CircularAnonymous" {
			mainStruct = s
			break
		}
	}

	if mainStruct == nil {
		t.Fatal("CircularAnonymous not found")
	}

	// Nested field should reference the synthetic type
	nestedField := findField(mainStruct.Fields, "Nested")
	if nestedField == nil {
		t.Fatal("Nested field not found")
	}
	if nestedField.Type.Kind() != ir.KindReference {
		t.Errorf("Nested should be KindReference, got %v", nestedField.Type.Kind())
	}
}

func TestReflectionProvider_DeeplyNestedAnonymous(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(DeeplyNestedAnonymous{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should create synthetic types for all nested levels
	// DeeplyNestedAnonymous + Level1 + Level2 + Level3 + Level4 = 5 types
	if len(schema.Types) < 5 {
		t.Errorf("expected at least 5 types for deeply nested anonymous structs, got %d", len(schema.Types))
	}
}

func TestReflectionProvider_NamedTypeWithMethods(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(WithNamedMethods{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should extract both types
	if len(schema.Types) < 2 {
		t.Errorf("expected at least 2 types, got %d", len(schema.Types))
	}

	// Find NamedWithMethods
	var namedType *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "NamedWithMethods" {
			namedType = s
			break
		}
	}

	if namedType == nil {
		t.Fatal("NamedWithMethods not found")
	}

	// Should have the Value field
	if len(namedType.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(namedType.Fields))
	}
}

func TestReflectionProvider_EmbeddedInterface(t *testing.T) {
	provider := &ReflectionProvider{}

	type Reader interface {
		Read() string
	}

	type WithEmbeddedInterface struct {
		Reader `json:"reader"`
		Value  string `json:"value"`
	}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(WithEmbeddedInterface{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find the struct
	var mainStruct *ir.StructDescriptor
	for _, typ := range schema.Types {
		if s, ok := typ.(*ir.StructDescriptor); ok && s.Name.Name == "WithEmbeddedInterface" {
			mainStruct = s
			break
		}
	}

	if mainStruct == nil {
		t.Fatal("WithEmbeddedInterface not found")
	}

	// Reader field with json tag should be in Fields as PrimitiveAny
	readerField := findField(mainStruct.Fields, "Reader")
	if readerField == nil {
		t.Fatal("Reader field not found")
	}
	if readerField.Type.Kind() != ir.KindPrimitive {
		t.Errorf("Reader should be KindPrimitive (any), got %v", readerField.Type.Kind())
	}
	primDesc := readerField.Type.(*ir.PrimitiveDescriptor)
	if primDesc.PrimitiveKind != ir.PrimitiveAny {
		t.Errorf("Reader should be PrimitiveAny, got %v", primDesc.PrimitiveKind)
	}

	// Should have a warning about interface type
	if len(schema.Warnings) == 0 {
		t.Error("expected warning about interface type")
	}
}

func TestReflectionProvider_MapWithNamedKeyType(t *testing.T) {
	provider := &ReflectionProvider{}

	type UserID string
	type UserMap struct {
		Users map[UserID]string `json:"users"`
	}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(UserMap{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should extract UserMap, UserID alias
	if len(schema.Types) < 2 {
		t.Errorf("expected at least 2 types, got %d", len(schema.Types))
	}

	// Find UserID alias
	var userIDAlias *ir.AliasDescriptor
	for _, typ := range schema.Types {
		if alias, ok := typ.(*ir.AliasDescriptor); ok && alias.Name.Name == "UserID" {
			userIDAlias = alias
			break
		}
	}

	if userIDAlias == nil {
		t.Error("UserID alias not found")
	}
}

// Test types for custom marshaler detection

// JSONMarshalerType implements json.Marshaler
type JSONMarshalerType struct {
	internal string
}

func (j JSONMarshalerType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + j.internal + `"`), nil
}

// TextMarshalerType implements encoding.TextMarshaler
type TextMarshalerType struct {
	internal string
}

func (t TextMarshalerType) MarshalText() ([]byte, error) {
	return []byte(t.internal), nil
}

// PtrJSONMarshalerType has MarshalJSON on pointer receiver
type PtrJSONMarshalerType struct {
	internal string
}

func (p *PtrJSONMarshalerType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.internal + `"`), nil
}

// ContainsCustomMarshalers has fields with custom marshaler types
type ContainsCustomMarshalers struct {
	JSON JSONMarshalerType     `json:"json"`
	Text TextMarshalerType     `json:"text"`
	Ptr  *PtrJSONMarshalerType `json:"ptr"`
}

func TestReflectionProvider_CustomMarshaler(t *testing.T) {
	provider := &ReflectionProvider{}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{
			reflect.TypeOf(JSONMarshalerType{}),
			reflect.TypeOf(TextMarshalerType{}),
		},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Both types should generate CUSTOM_MARSHALER warnings
	foundWarnings := make(map[string]bool)
	for _, w := range schema.Warnings {
		if w.Code == "CUSTOM_MARSHALER" {
			foundWarnings[w.TypeName] = true
		}
	}

	for _, typeName := range []string{
		"provider.JSONMarshalerType",
		"provider.TextMarshalerType",
	} {
		if !foundWarnings[typeName] {
			t.Errorf("expected CUSTOM_MARSHALER warning for %s", typeName)
		}
	}
}

func TestReflectionProvider_CustomMarshalerAsField(t *testing.T) {
	provider := &ReflectionProvider{}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(ContainsCustomMarshalers{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Find ContainsCustomMarshalers struct
	var structDesc *ir.StructDescriptor
	for _, typ := range schema.Types {
		if sd, ok := typ.(*ir.StructDescriptor); ok && sd.Name.Name == "ContainsCustomMarshalers" {
			structDesc = sd
			break
		}
	}

	if structDesc == nil {
		t.Fatal("ContainsCustomMarshalers not found")
	}

	// All three fields should be PrimitiveAny
	for _, fieldName := range []string{"JSON", "Text", "Ptr"} {
		field := findField(structDesc.Fields, fieldName)
		if field == nil {
			t.Errorf("field %s not found", fieldName)
			continue
		}

		// Dereference PtrDescriptor for Ptr field
		fieldType := field.Type
		if ptr, ok := fieldType.(*ir.PtrDescriptor); ok {
			fieldType = ptr.Element
		}

		primDesc, ok := fieldType.(*ir.PrimitiveDescriptor)
		if !ok {
			t.Errorf("field %s: expected PrimitiveDescriptor, got %T", fieldName, fieldType)
			continue
		}
		if primDesc.PrimitiveKind != ir.PrimitiveAny {
			t.Errorf("field %s: expected PrimitiveAny, got %v", fieldName, primDesc.PrimitiveKind)
		}
	}

	// Should have warnings for the custom marshaler types
	hasWarning := false
	for _, w := range schema.Warnings {
		if w.Code == "CUSTOM_MARSHALER" {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected CUSTOM_MARSHALER warning")
	}
}

func TestReflectionProvider_CustomMarshalerPtrReceiver(t *testing.T) {
	provider := &ReflectionProvider{}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(PtrJSONMarshalerType{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should detect that *PtrJSONMarshalerType implements json.Marshaler
	hasWarning := false
	for _, w := range schema.Warnings {
		if w.Code == "CUSTOM_MARSHALER" && strings.Contains(w.TypeName, "PtrJSONMarshalerType") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected CUSTOM_MARSHALER warning for PtrJSONMarshalerType (pointer receiver)")
	}
}

func TestReflectionProvider_TimeNotCustomMarshaler(t *testing.T) {
	// time.Time implements json.Marshaler but should be handled specially,
	// not as a generic custom marshaler

	provider := &ReflectionProvider{}

	type WithTime struct {
		T time.Time `json:"t"`
	}

	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(WithTime{})},
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Should NOT have a CUSTOM_MARSHALER warning for time.Time
	for _, w := range schema.Warnings {
		if w.Code == "CUSTOM_MARSHALER" && strings.Contains(w.TypeName, "time.Time") {
			t.Errorf("time.Time should not generate CUSTOM_MARSHALER warning: %v", w)
		}
	}

	// Find WithTime struct
	var structDesc *ir.StructDescriptor
	for _, typ := range schema.Types {
		if sd, ok := typ.(*ir.StructDescriptor); ok && sd.Name.Name == "WithTime" {
			structDesc = sd
			break
		}
	}

	if structDesc == nil {
		t.Fatal("WithTime not found")
	}

	// The T field should be PrimitiveTime, not PrimitiveAny
	tField := findField(structDesc.Fields, "T")
	if tField == nil {
		t.Fatal("T field not found")
	}

	primDesc, ok := tField.Type.(*ir.PrimitiveDescriptor)
	if !ok {
		t.Fatalf("T field: expected PrimitiveDescriptor, got %T", tField.Type)
	}
	if primDesc.PrimitiveKind != ir.PrimitiveTime {
		t.Errorf("T field: expected PrimitiveTime, got %v", primDesc.PrimitiveKind)
	}
}

func TestReflectionProvider_JSONEmbeddingParity(t *testing.T) {
	tests := []struct {
		name       string
		typ        reflect.Type
		wantFields []string
		absent     []string
	}{
		{name: "equal-depth conflict", typ: reflect.TypeOf(testdata.EqualDepthConflict{}), wantFields: []string{"A", "B", "Own"}, absent: []string{"Clash"}},
		{name: "tagged dominance", typ: reflect.TypeOf(testdata.TaggedDominance{}), wantFields: []string{"Tagged"}, absent: []string{"Same"}},
		{name: "tagged embed", typ: reflect.TypeOf(testdata.TaggedEmbedding{}), wantFields: []string{"ConflictA"}},
		{name: "Unicode JSON name", typ: reflect.TypeOf(testdata.UnicodeJSONName{}), wantFields: []string{"Value"}},
	}

	provider := &ReflectionProvider{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{RootTypes: []reflect.Type{tt.typ}})
			if err != nil {
				t.Fatalf("BuildSchema failed: %v", err)
			}
			if validationErrors := schema.Validate(); len(validationErrors) != 0 {
				t.Fatalf("schema validation failed: %v", validationErrors)
			}
			root := findType(schema, tt.typ.Name()).(*ir.StructDescriptor)
			if len(root.Extends) != 0 {
				t.Fatalf("provider emitted Extends: %v", root.Extends)
			}
			for _, name := range tt.wantFields {
				if findField(root.Fields, name) == nil {
					t.Errorf("field %s not found", name)
				}
			}
			for _, name := range tt.absent {
				if findField(root.Fields, name) != nil {
					t.Errorf("ambiguous field %s was not omitted", name)
				}
			}
		})
	}

	for _, tt := range []struct {
		name    string
		typ     reflect.Type
		wantErr string
	}{
		{name: "options-only embed", typ: reflect.TypeOf(jsonv2invalid.OptionsOnlyEmbedding{}), wantErr: "cannot have options without an explicit JSON name"},
		{name: "scalar embeds", typ: reflect.TypeOf(jsonv2invalid.ScalarEmbedding{}), wantErr: "embedded non-struct field"},
		{name: "direct name conflict", typ: reflect.TypeOf(jsonv2invalid.DirectNameConflict{}), wantErr: "conflict over JSON object name"},
	} {
		t.Run(tt.name+" rejected", func(t *testing.T) {
			_, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{RootTypes: []reflect.Type{tt.typ}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("BuildSchema error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	t.Run("Unicode JSON name", func(t *testing.T) {
		schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
			RootTypes: []reflect.Type{reflect.TypeOf(testdata.UnicodeJSONName{})},
		})
		if err != nil {
			t.Fatal(err)
		}
		field := findField(findType(schema, "UnicodeJSONName").(*ir.StructDescriptor).Fields, "Value")
		if field == nil || field.JSONName != "💡" {
			t.Fatalf("Unicode field = %#v, want JSON name 💡", field)
		}
	})

	t.Run("pointer promotion is optional", func(t *testing.T) {
		schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
			RootTypes: []reflect.Type{reflect.TypeOf(testdata.PointerEmbedding{})},
		})
		if err != nil {
			t.Fatal(err)
		}
		field := findField(findType(schema, "PointerEmbedding").(*ir.StructDescriptor).Fields, "FromPointer")
		if field == nil || !schema.FieldOptional(*field) {
			t.Fatalf("promoted pointer field = %#v, want optional", field)
		}
	})
}

func TestReflectionProvider_CustomMarshalerRoot(t *testing.T) {
	provider := &ReflectionProvider{}
	schema, err := provider.BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(testdata.MarshaledEnum(0))},
	})
	if err != nil {
		t.Fatal(err)
	}
	alias, ok := findType(schema, "MarshaledEnum").(*ir.AliasDescriptor)
	if !ok || alias.Underlying.(*ir.PrimitiveDescriptor).PrimitiveKind != ir.PrimitiveAny {
		t.Fatalf("MarshaledEnum = %T, want any alias", findType(schema, "MarshaledEnum"))
	}
}

func TestReflectionProvider_JSONWireClassification(t *testing.T) {
	schema, err := (&ReflectionProvider{}).BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{
			reflect.TypeOf(testdata.StringEncodingDepths{}),
			reflect.TypeOf(testdata.DefinedByteSlices{}),
			reflect.TypeOf(testdata.CustomElementByteSlice{}),
			reflect.TypeOf(testdata.AliasResultMarshalers{}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONWireClassification(t, schema)
}

func TestReflectionProvider_OuterPointerToRecursivePointerAliasTerminates(t *testing.T) {
	type result struct {
		schema *ir.Schema
		err    error
	}
	done := make(chan result, 1)
	go func() {
		schema, err := (&ReflectionProvider{}).BuildSchema(context.Background(), ReflectionInputOptions{
			RootTypes: []reflect.Type{reflect.TypeFor[*reflectionRecursivePointerAlias]()},
		})
		done <- result{schema: schema, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		alias, ok := findType(got.schema, "reflectionRecursivePointerAlias").(*ir.AliasDescriptor)
		if !ok || !got.schema.HasPointerOnlyAliasCycle(alias) {
			t.Fatalf("recursive reflected alias = %#v, want detected pointer-only cycle", alias)
		}
	case <-time.After(time.Second):
		t.Fatal("reflection provider hung on outer pointer to recursive pointer alias")
	}
}

func TestReflectionProvider_AnonymousIdentityIncludesPackage(t *testing.T) {
	schema, err := (&ReflectionProvider{}).BuildSchema(context.Background(), ReflectionInputOptions{
		RootTypes: []reflect.Type{reflect.TypeOf(anona.A{}), reflect.TypeOf(anonb.B{})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if validationErrors := schema.Validate(); len(validationErrors) != 0 {
		t.Fatalf("schema validation failed: %v", validationErrors)
	}

	aField := findField(findType(schema, "A").(*ir.StructDescriptor).Fields, "X")
	bField := findField(findType(schema, "B").(*ir.StructDescriptor).Fields, "X")
	aRef := aField.Type.(*ir.ReferenceDescriptor)
	bRef := bField.Type.(*ir.ReferenceDescriptor)
	if aRef.Target != bRef.Target || bRef.Target.Package != reflect.TypeOf(anona.A{}).PkgPath() || schema.FindType(bRef.Target) == nil {
		t.Fatalf("cached anonymous identities = %v and %v, want one resolvable package-qualified identity", aRef.Target, bRef.Target)
	}
}

// Helper function to find a field by name
func findField(fields []ir.FieldDescriptor, name string) *ir.FieldDescriptor {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
