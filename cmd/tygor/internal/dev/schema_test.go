package dev

import (
	json "encoding/json/v2"
	"os"
	"testing"

	"tygor.dev/tygorgen/ir"
)

// TestDiscoverySchemaRoundTrip verifies that our schema types can round-trip
// a real discovery.json file. This catches drift between the IR's MarshalJSON
// output and our schema struct definitions.
func TestDiscoverySchemaRoundTrip(t *testing.T) {
	// Use the devtools example's discovery.json as a test fixture
	data, err := os.ReadFile("../../../../examples/devtools/src/rpc/discovery.json")
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	// Parse into our schema types
	var schema DiscoverySchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("failed to unmarshal discovery.json into DiscoverySchema: %v", err)
	}

	// Basic sanity checks
	if schema.Package.Name == "" {
		t.Error("Package.Name should not be empty")
	}
	if len(schema.Services) == 0 {
		t.Error("Services should not be empty")
	}

	// Check that services have endpoints with primitives
	for _, svc := range schema.Services {
		if svc.Name == "" {
			t.Error("service name should not be empty")
		}
		for _, ep := range svc.Endpoints {
			if ep.Primitive == "" {
				t.Errorf("endpoint %s.%s missing primitive", svc.Name, ep.Name)
			}
			if ep.Primitive != "query" && ep.Primitive != "exec" && ep.Primitive != "stream" && ep.Primitive != "livevalue" {
				t.Errorf("endpoint %s.%s has invalid primitive: %s", svc.Name, ep.Name, ep.Primitive)
			}
		}
	}

	// Round-trip: marshal back and compare structure
	remarshaled, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to re-marshal schema: %v", err)
	}

	// Parse both as generic maps and compare keys exist
	// (exact comparison may fail due to omitempty differences)
	var original, roundtripped map[string]any
	json.Unmarshal(data, &original)
	json.Unmarshal(remarshaled, &roundtripped)

	// Check top-level keys match (allowing omitempty to drop null values)
	for key := range original {
		if _, ok := roundtripped[key]; !ok {
			// Check if the original value was null - that's okay to drop
			if original[key] == nil {
				continue
			}
			t.Errorf("round-trip lost top-level key: %s", key)
		}
	}
}

// TestDiscoverySchemaTypes verifies type parsing works for various TypeRef kinds.
func TestDiscoverySchemaTypes(t *testing.T) {
	data, err := os.ReadFile("../../../../examples/devtools/src/rpc/discovery.json")
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	var schema DiscoverySchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Check that Types are parsed with their kind field
	for _, typ := range schema.Types {
		if typ.Kind == "" {
			t.Errorf("type %s missing kind", typ.Name.Name)
		}
		if typ.Kind != "struct" && typ.Kind != "enum" && typ.Kind != "alias" {
			t.Errorf("type %s has unexpected kind: %s", typ.Name.Name, typ.Kind)
		}

		// For structs, check fields have types with kinds
		if typ.Kind == "struct" {
			for _, field := range typ.Fields {
				if field.Type.Kind == "" {
					t.Errorf("field %s.%s missing type kind", typ.Name.Name, field.Name)
				}
			}
		}
	}
}

func TestDiscoverySchemaPreservesV2Metadata(t *testing.T) {
	pkg := "example.com/model"
	schema := &ir.Schema{
		Package: ir.PackageInfo{Path: pkg, Name: "model"},
		Types: []ir.TypeDescriptor{&ir.StructDescriptor{
			Name: ir.GoIdentifier{Name: "Payload", Package: pkg},
			Fields: []ir.FieldDescriptor{
				{Name: "Empty", JSONName: "empty", Type: ir.ByteArray(0), OmitEmpty: true},
				{Name: "Zero", JSONName: "zero", Type: ir.ByteArray(3), OmitZero: true},
				{Name: "Promoted", JSONName: "promoted", Type: ir.String(), OmitIfEmbeddedNil: true},
			},
		}},
		Warnings: []ir.Warning{{
			Code:     "V2_WARNING",
			Message:  "preserve me",
			Source:   &ir.Source{File: "model.go", Line: 7, Column: 3},
			TypeName: "Payload",
		}},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var got DiscoverySchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Types) != 1 || len(got.Types[0].Fields) != 3 {
		t.Fatalf("discovery fields = %#v", got.Types)
	}
	fields := got.Types[0].Fields
	if !fields[0].OmitEmpty || fields[0].Type.ByteArrayLength == nil || *fields[0].Type.ByteArrayLength != 0 {
		t.Fatalf("empty fixed bytes metadata = %#v", fields[0])
	}
	if !fields[1].OmitZero || fields[1].Type.ByteArrayLength == nil || *fields[1].Type.ByteArrayLength != 3 {
		t.Fatalf("fixed bytes metadata = %#v", fields[1])
	}
	if !fields[2].OmitIfEmbeddedNil {
		t.Fatalf("embedded omission metadata = %#v", fields[2])
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Code != "V2_WARNING" || got.Warnings[0].Message != "preserve me" || got.Warnings[0].Source == nil || got.Warnings[0].TypeName != "Payload" {
		t.Fatalf("warning metadata = %#v", got.Warnings)
	}
}
