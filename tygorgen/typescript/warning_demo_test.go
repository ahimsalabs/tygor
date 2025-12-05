package typescript

import (
	"context"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

// TestWarningDemo demonstrates the large integer warning feature
func TestWarningDemo(t *testing.T) {
	schema := &ir.Schema{
		Package: ir.PackageInfo{
			Path: "example.com/demo",
			Name: "demo",
		},
		Types: []ir.TypeDescriptor{
			&ir.StructDescriptor{
				Name: ir.GoIdentifier{Name: "User", Package: "example.com/demo"},
				Fields: []ir.FieldDescriptor{
					{
						Name:          "ID",
						JSONName:      "id",
						Type:          ir.Int(64),
						StringEncoded: false,
					},
					{
						Name:          "AccountID",
						JSONName:      "account_id",
						Type:          ir.Int(64),
						StringEncoded: true, // Has ,string tag - no warning
					},
					{
						Name:     "Name",
						JSONName: "name",
						Type:     ir.String(),
					},
				},
			},
		},
	}

	memSink := sink.NewMemorySink()
	gen := &TypeScriptGenerator{}

	result, err := gen.Generate(context.Background(), schema, GenerateOptions{
		Sink: memSink,
		Config: GeneratorConfig{
			SingleFile:      true,
			TrailingNewline: true,
			LineEnding:      "lf",
		},
	})

	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	t.Logf("Generated TypeScript:\n%s", string(memSink.Get("types.ts")))
	t.Logf("\nWarnings (%d):", len(result.Warnings))
	for i, w := range result.Warnings {
		t.Logf("  [%d] Code: %s", i, w.Code)
		t.Logf("      Type: %s", w.TypeName)
		t.Logf("      Message: %s", w.Message)
	}

	// Verify we got exactly 1 warning (for the ID field, not AccountID)
	if len(result.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(result.Warnings))
	}

	if len(result.Warnings) > 0 {
		if result.Warnings[0].Code != "LARGE_INT_PRECISION" {
			t.Errorf("expected warning code LARGE_INT_PRECISION, got %s", result.Warnings[0].Code)
		}
	}
}
