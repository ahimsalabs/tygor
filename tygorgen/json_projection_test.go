package tygorgen

import (
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"strings"
	"testing"

	"tygor.dev/tygor"
	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider/testdata"
)

type JSONProjectionNested struct {
	Score int `json:"score"`
}

type JSONProjectionPayload struct {
	Count         int                   `json:"count"`
	OptionalCount int                   `json:"optionalCount,omitempty" validate:"gte=10"`
	Items         []int                 `json:"items"`
	Index         map[string]int        `json:"index"`
	Nested        *JSONProjectionNested `json:"nested"`
}

func TestGenerateProjectsResolvedJSONContractPerEndpointAndDirection(t *testing.T) {
	app := tygor.NewApp(tygor.WithJSONOptions(json.StringifyNumbers(true)))
	service := app.Service("Projection", tygor.WithJSONOptions(
		json.FormatNilSliceAsNull(true),
		json.FormatNilMapAsNull(true),
	))
	service.Exec("Custom", func(_ context.Context, request JSONProjectionPayload) (JSONProjectionPayload, error) {
		return request, nil
	}, tygor.WithJSONOptions(json.OmitZeroStructFields(true)))
	service.Exec("Default", func(_ context.Context, request JSONProjectionPayload) (JSONProjectionPayload, error) {
		return request, nil
	}, tygor.WithJSONOptions(
		json.StringifyNumbers(false),
		json.FormatNilSliceAsNull(false),
		json.FormatNilMapAsNull(false),
	))

	result, err := Generate(app, &Config{Provider: "reflection", Flavors: []Flavor{FlavorZod, FlavorZodMini}})
	if err != nil {
		t.Fatal(err)
	}
	custom := findGeneratedEndpoint(t, result.Schema, "Projection.Custom")
	request := referencedStruct(t, result.Schema, custom.Request)
	response := referencedStruct(t, result.Schema, custom.Response)
	if request.Name == response.Name {
		t.Fatalf("request and response share projection %v", request.Name)
	}
	if !custom.JSON.Decode.StringifyNumbers || !custom.JSON.Encode.StringifyNumbers ||
		!custom.JSON.Encode.FormatNilSliceAsNull || !custom.JSON.Encode.FormatNilMapAsNull ||
		!custom.JSON.Encode.OmitZeroStructFields {
		t.Fatalf("resolved endpoint JSON contract = %#v", custom.JSON)
	}

	assertStringEncodedPrimitive(t, fieldByJSONName(t, request, "count").Type, ir.PrimitiveInt)
	if result.Schema.FieldOptional(fieldByJSONName(t, request, "optionalCount")) {
		t.Fatal("stringified numeric omitempty field became optional")
	}
	requestItems := fieldByJSONName(t, request, "items").Type
	if _, nullable := requestItems.(*ir.PtrDescriptor); nullable {
		t.Fatalf("decode slice unexpectedly nullable: %#v", requestItems)
	}
	assertStringEncodedArrayElement(t, requestItems, ir.PrimitiveInt)

	for _, name := range []string{"count", "items", "index", "nested"} {
		if !fieldByJSONName(t, response, name).OmitZero {
			t.Fatalf("response field %q did not inherit OmitZeroStructFields", name)
		}
	}
	responseItems, ok := fieldByJSONName(t, response, "items").Type.(*ir.PtrDescriptor)
	if !ok {
		t.Fatalf("encoded nil slice projection = %T, want pointer nullability", fieldByJSONName(t, response, "items").Type)
	}
	assertStringEncodedArrayElement(t, responseItems.Element, ir.PrimitiveInt)
	if _, ok := fieldByJSONName(t, response, "index").Type.(*ir.PtrDescriptor); !ok {
		t.Fatalf("encoded nil map projection = %T, want pointer nullability", fieldByJSONName(t, response, "index").Type)
	}

	defaultEndpoint := findGeneratedEndpoint(t, result.Schema, "Projection.Default")
	defaultRequest := referencedStruct(t, result.Schema, defaultEndpoint.Request)
	if defaultRequest.Name.Name != "JSONProjectionPayload" {
		t.Fatalf("explicit false endpoint retained projected request type %q", defaultRequest.Name.Name)
	}
	assertPrimitiveKind(t, fieldByJSONName(t, defaultRequest, "count").Type, ir.PrimitiveInt)

	var types strings.Builder
	for _, file := range result.Files {
		if strings.HasPrefix(file.Path, "types") && strings.HasSuffix(file.Path, ".ts") {
			types.Write(file.Content)
		}
	}
	wantRequiredRequest := "interface " + request.Name.Name + " {\n  count: string;\n  optionalCount: string;"
	if !strings.Contains(types.String(), wantRequiredRequest) {
		t.Fatalf("generated numeric omitempty projection lost requiredness:\n%s", types.String())
	}
	for _, path := range []string{"schemas.zod.ts", "schemas.zod-mini.ts"} {
		content := generatedFile(t, result, path)
		if !strings.Contains(content, `BigInt(v) >= BigInt("10")`) {
			t.Fatalf("%s does not apply numeric validation to stringified number:\n%s", path, content)
		}
	}
}

func TestGenerateRejectsNonNativeAndOpaqueJSONContracts(t *testing.T) {
	t.Run("legacy compatibility", func(t *testing.T) {
		app := tygor.NewApp(tygor.WithJSONOptions(jsonv1.FormatDurationAsNano(true)))
		app.Service("Projection").Exec("Legacy", func(context.Context, JSONProjectionPayload) (JSONProjectionPayload, error) {
			return JSONProjectionPayload{}, nil
		})
		_, err := Generate(app, &Config{Provider: "reflection"})
		if err == nil || !strings.Contains(err.Error(), "native-v2") || !strings.Contains(err.Error(), "FormatDurationAsNano") {
			t.Fatalf("Generate() error = %v, want native-v2 compatibility rejection", err)
		}
	})

	t.Run("opaque marshaler registry", func(t *testing.T) {
		marshalers := json.MarshalFunc(func(JSONProjectionPayload) ([]byte, error) {
			return []byte(`null`), nil
		})
		app := tygor.NewApp(tygor.WithJSONOptions(json.WithMarshalers(marshalers)))
		app.Service("Projection").Query("Opaque", func(context.Context, JSONProjectionPayload) (JSONProjectionPayload, error) {
			return JSONProjectionPayload{}, nil
		})
		_, err := Generate(app, &Config{Provider: "reflection"})
		if err == nil || !strings.Contains(err.Error(), "explicit wire declaration") {
			t.Fatalf("Generate() error = %v, want opaque marshaler rejection", err)
		}
	})

	t.Run("cleared opaque marshaler registry", func(t *testing.T) {
		marshalers := json.MarshalFunc(func(JSONProjectionPayload) ([]byte, error) {
			return []byte(`null`), nil
		})
		app := tygor.NewApp(tygor.WithJSONOptions(json.WithMarshalers(marshalers)))
		app.Service("Projection").Query("Clear", func(context.Context, JSONProjectionPayload) (JSONProjectionPayload, error) {
			return JSONProjectionPayload{}, nil
		}, tygor.WithJSONOptions(json.WithMarshalers(nil)))
		if _, err := Generate(app, &Config{Provider: "reflection"}); err != nil {
			t.Fatalf("Generate() rejected cleared marshaler registry: %v", err)
		}
	})

	t.Run("query ignores opaque unmarshalers", func(t *testing.T) {
		unmarshalers := json.UnmarshalFunc(func([]byte, *JSONProjectionPayload) error { return nil })
		app := tygor.NewApp(tygor.WithJSONOptions(json.WithUnmarshalers(unmarshalers)))
		app.Service("Projection").Query("Query", func(context.Context, JSONProjectionPayload) (JSONProjectionPayload, error) {
			return JSONProjectionPayload{}, nil
		})
		if _, err := Generate(app, &Config{Provider: "reflection"}); err != nil {
			t.Fatalf("Generate() applied JSON request unmarshalers to Query: %v", err)
		}
	})

	t.Run("wire-distorting optional override", func(t *testing.T) {
		app := tygor.NewApp()
		_, err := Generate(app, &Config{Provider: "reflection", OptionalType: "undefined"})
		if err == nil || !strings.Contains(err.Error(), "OptionalType") {
			t.Fatalf("Generate() error = %v, want OptionalType rejection", err)
		}
	})
}

func findGeneratedEndpoint(t *testing.T, schema *ir.Schema, fullName string) ir.EndpointDescriptor {
	t.Helper()
	for _, service := range schema.Services {
		for _, endpoint := range service.Endpoints {
			if endpoint.FullName == fullName {
				return endpoint
			}
		}
	}
	t.Fatalf("endpoint %q not found", fullName)
	return ir.EndpointDescriptor{}
}

func generatedFile(t *testing.T, result *GenerateResult, path string) string {
	t.Helper()
	for _, file := range result.Files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	t.Fatalf("generated file %q not found", path)
	return ""
}

func referencedStruct(t *testing.T, schema *ir.Schema, descriptor ir.TypeDescriptor) *ir.StructDescriptor {
	t.Helper()
	reference, ok := descriptor.(*ir.ReferenceDescriptor)
	if !ok {
		t.Fatalf("descriptor = %T, want reference", descriptor)
	}
	structure, ok := schema.FindType(reference.Target).(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("reference %v = %T, want struct", reference.Target, schema.FindType(reference.Target))
	}
	return structure
}

func fieldByJSONName(t *testing.T, structure *ir.StructDescriptor, name string) ir.FieldDescriptor {
	t.Helper()
	for _, field := range structure.Fields {
		if field.JSONName == name {
			return field
		}
	}
	t.Fatalf("field %q not found in %s", name, structure.Name.Name)
	return ir.FieldDescriptor{}
}

func assertPrimitiveKind(t *testing.T, descriptor ir.TypeDescriptor, want ir.PrimitiveKind) {
	t.Helper()
	primitive, ok := descriptor.(*ir.PrimitiveDescriptor)
	if !ok || primitive.PrimitiveKind != want {
		t.Fatalf("descriptor = %#v, want %s primitive", descriptor, want)
	}
}

func assertArrayElementKind(t *testing.T, descriptor ir.TypeDescriptor, want ir.PrimitiveKind) {
	t.Helper()
	array, ok := descriptor.(*ir.ArrayDescriptor)
	if !ok {
		t.Fatalf("descriptor = %T, want array", descriptor)
	}
	assertPrimitiveKind(t, array.Element, want)
}

func assertStringEncodedPrimitive(t *testing.T, descriptor ir.TypeDescriptor, want ir.PrimitiveKind) {
	t.Helper()
	primitive, ok := descriptor.(*ir.PrimitiveDescriptor)
	if !ok || primitive.PrimitiveKind != want || !primitive.StringEncoded {
		t.Fatalf("descriptor = %#v, want string-encoded %s primitive", descriptor, want)
	}
}

func assertStringEncodedArrayElement(t *testing.T, descriptor ir.TypeDescriptor, want ir.PrimitiveKind) {
	t.Helper()
	array, ok := descriptor.(*ir.ArrayDescriptor)
	if !ok {
		t.Fatalf("descriptor = %T, want array", descriptor)
	}
	assertStringEncodedPrimitive(t, array.Element, want)
}

func TestStringifyEnumValueUsesJSONV2FloatFormatting(t *testing.T) {
	for _, test := range []struct {
		name       string
		value      float64
		underlying *ir.PrimitiveDescriptor
		want       string
	}{
		{name: "float64 fixed notation", value: 1e6, underlying: ir.Float(64), want: "1000000"},
		{name: "float32 rounding", value: 1.234567890123, underlying: ir.Float(32), want: "1.2345679"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := stringifyEnumValue(test.value, test.underlying)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("stringifyEnumValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGenerateSourcePreservesStringEnumWithStringifyNumbers(t *testing.T) {
	app := tygor.NewApp(tygor.WithJSONOptions(json.StringifyNumbers(true)))
	service := app.Service("Enums")
	service.Exec("Stringified", func(_ context.Context, value testdata.Status) (testdata.Status, error) {
		return value, nil
	})
	service.Exec("Default", func(_ context.Context, value testdata.Status) (testdata.Status, error) {
		return value, nil
	}, tygor.WithJSONOptions(json.StringifyNumbers(false)))

	result, err := Generate(app, &Config{Provider: "source", Flavors: []Flavor{FlavorZod, FlavorZodMini}})
	if err != nil {
		t.Fatal(err)
	}
	stringified := findGeneratedEndpoint(t, result.Schema, "Enums.Stringified")
	reference, ok := stringified.Response.(*ir.ReferenceDescriptor)
	if !ok {
		t.Fatalf("stringified enum response = %T, want reference", stringified.Response)
	}
	projected, ok := result.Schema.FindType(reference.Target).(*ir.EnumDescriptor)
	if !ok {
		t.Fatalf("projected enum = %T", result.Schema.FindType(reference.Target))
	}
	if len(projected.StringEncodedValues) != 0 {
		t.Fatalf("string enum received numeric wire values: %v", projected.StringEncodedValues)
	}

	for _, path := range []string{"types_tygor_dev_tygorgen_provider_testdata.ts", "schemas.zod.ts", "schemas.zod-mini.ts"} {
		content := generatedFile(t, result, path)
		for _, literal := range []string{`"active"`, `"inactive"`, `"pending"`} {
			if !strings.Contains(content, literal) {
				t.Fatalf("%s omitted string enum literal %s:\n%s", path, literal, content)
			}
		}
	}

	defaultEndpoint := findGeneratedEndpoint(t, result.Schema, "Enums.Default")
	defaultReference, ok := defaultEndpoint.Response.(*ir.ReferenceDescriptor)
	if !ok || defaultReference.Target.Name != "Status" {
		t.Fatalf("explicit false enum response = %#v, want original Status", defaultEndpoint.Response)
	}
}
