package typescript

import (
	"context"
	"testing"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

func TestLargeIntegerWarnings(t *testing.T) {
	tests := []struct {
		name         string
		field        ir.FieldDescriptor
		typeName     string
		wantWarning  bool
		wantCode     string
		wantContains string
	}{
		{
			name: "int64 without string tag - should warn",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Int(64),
				StringEncoded: false,
			},
			typeName:     "Stats",
			wantWarning:  true,
			wantCode:     "LARGE_INT_PRECISION",
			wantContains: "int64",
		},
		{
			name: "uint64 without string tag - should warn",
			field: ir.FieldDescriptor{
				Name:          "ID",
				JSONName:      "id",
				Type:          ir.Uint(64),
				StringEncoded: false,
			},
			typeName:     "User",
			wantWarning:  true,
			wantCode:     "LARGE_INT_PRECISION",
			wantContains: "uint64",
		},
		{
			name: "int64 WITH string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Int(64),
				StringEncoded: true,
			},
			typeName:    "Stats",
			wantWarning: false,
		},
		{
			name: "uint64 WITH string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "ID",
				JSONName:      "id",
				Type:          ir.Uint(64),
				StringEncoded: true,
			},
			typeName:    "User",
			wantWarning: false,
		},
		{
			name: "int32 without string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "Age",
				JSONName:      "age",
				Type:          ir.Int(32),
				StringEncoded: false,
			},
			typeName:    "User",
			wantWarning: false,
		},
		{
			name: "uint32 without string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Uint(32),
				StringEncoded: false,
			},
			typeName:    "Stats",
			wantWarning: false,
		},
		{
			name: "int (platform-dependent) without string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "Value",
				JSONName:      "value",
				Type:          ir.Int(0),
				StringEncoded: false,
			},
			typeName:    "Data",
			wantWarning: false,
		},
		{
			name: "*int64 (pointer to int64) - should warn",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Ptr(ir.Int(64)),
				StringEncoded: false,
			},
			typeName:     "Stats",
			wantWarning:  true,
			wantCode:     "LARGE_INT_PRECISION",
			wantContains: "int64",
		},
		{
			name: "*uint64 (pointer to uint64) - should warn",
			field: ir.FieldDescriptor{
				Name:          "ID",
				JSONName:      "id",
				Type:          ir.Ptr(ir.Uint(64)),
				StringEncoded: false,
			},
			typeName:     "User",
			wantWarning:  true,
			wantCode:     "LARGE_INT_PRECISION",
			wantContains: "uint64",
		},
		{
			name: "**int64 (double pointer to int64) - should warn",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Ptr(ir.Ptr(ir.Int(64))),
				StringEncoded: false,
			},
			typeName:     "Stats",
			wantWarning:  true,
			wantCode:     "LARGE_INT_PRECISION",
			wantContains: "int64",
		},
		{
			name: "*int64 WITH string tag - no warning",
			field: ir.FieldDescriptor{
				Name:          "Count",
				JSONName:      "count",
				Type:          ir.Ptr(ir.Int(64)),
				StringEncoded: true,
			},
			typeName:    "Stats",
			wantWarning: false,
		},
		{
			name: "string type - no warning",
			field: ir.FieldDescriptor{
				Name:     "Name",
				JSONName: "name",
				Type:     ir.String(),
			},
			typeName:    "User",
			wantWarning: false,
		},
		{
			name: "float64 - no warning (not an integer)",
			field: ir.FieldDescriptor{
				Name:     "Price",
				JSONName: "price",
				Type:     ir.Float(64),
			},
			typeName:    "Product",
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{
				schema:   &ir.Schema{},
				config:   GeneratorConfig{},
				tsConfig: TypeScriptConfig{},
			}

			warning := emitter.checkLargeIntegerWarning(tt.field, tt.typeName)

			if tt.wantWarning {
				if warning == nil {
					t.Errorf("expected warning but got nil")
					return
				}
				if warning.Code != tt.wantCode {
					t.Errorf("warning code = %q, want %q", warning.Code, tt.wantCode)
				}
				if warning.TypeName != tt.typeName {
					t.Errorf("warning TypeName = %q, want %q", warning.TypeName, tt.typeName)
				}
				if tt.wantContains != "" && !contains(warning.Message, tt.wantContains) {
					t.Errorf("warning message %q does not contain %q", warning.Message, tt.wantContains)
				}
			} else {
				if warning != nil {
					t.Errorf("expected no warning but got: %+v", warning)
				}
			}
		})
	}
}

func TestLargeIntegerWarnings_IntegrationWithGenerate(t *testing.T) {
	tests := []struct {
		name         string
		schema       *ir.Schema
		wantWarnings int
		wantCodes    []string
	}{
		{
			name: "struct with int64 field - should warn",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "User", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:          "ID",
								JSONName:      "id",
								Type:          ir.Int(64),
								StringEncoded: false,
							},
						},
					},
				},
			},
			wantWarnings: 1,
			wantCodes:    []string{"LARGE_INT_PRECISION"},
		},
		{
			name: "struct with uint64 field - should warn",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "Stats", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:          "Count",
								JSONName:      "count",
								Type:          ir.Uint(64),
								StringEncoded: false,
							},
						},
					},
				},
			},
			wantWarnings: 1,
			wantCodes:    []string{"LARGE_INT_PRECISION"},
		},
		{
			name: "struct with int64 field WITH string tag - no warning",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "User", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:          "ID",
								JSONName:      "id",
								Type:          ir.Int(64),
								StringEncoded: true,
							},
						},
					},
				},
			},
			wantWarnings: 0,
			wantCodes:    []string{},
		},
		{
			name: "struct with multiple large int fields - multiple warnings",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "Data", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:          "Count",
								JSONName:      "count",
								Type:          ir.Int(64),
								StringEncoded: false,
							},
							{
								Name:          "Total",
								JSONName:      "total",
								Type:          ir.Uint(64),
								StringEncoded: false,
							},
							{
								Name:          "Age",
								JSONName:      "age",
								Type:          ir.Int(32),
								StringEncoded: false,
							},
						},
					},
				},
			},
			wantWarnings: 2,
			wantCodes:    []string{"LARGE_INT_PRECISION", "LARGE_INT_PRECISION"},
		},
		{
			name: "struct with pointer to int64 - should warn",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "User", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:          "ID",
								JSONName:      "id",
								Type:          ir.Ptr(ir.Int(64)),
								StringEncoded: false,
							},
						},
					},
				},
			},
			wantWarnings: 1,
			wantCodes:    []string{"LARGE_INT_PRECISION"},
		},
		{
			name: "struct with only safe types - no warnings",
			schema: &ir.Schema{
				Package: ir.PackageInfo{
					Path: "example.com/test",
					Name: "test",
				},
				Types: []ir.TypeDescriptor{
					&ir.StructDescriptor{
						Name: ir.GoIdentifier{Name: "User", Package: "example.com/test"},
						Fields: []ir.FieldDescriptor{
							{
								Name:     "Name",
								JSONName: "name",
								Type:     ir.String(),
							},
							{
								Name:     "Age",
								JSONName: "age",
								Type:     ir.Int(32),
							},
							{
								Name:     "Active",
								JSONName: "active",
								Type:     ir.Bool(),
							},
						},
					},
				},
			},
			wantWarnings: 0,
			wantCodes:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memSink := sink.NewMemorySink()
			gen := &TypeScriptGenerator{}

			result, err := gen.Generate(context.Background(), tt.schema, GenerateOptions{
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

			// Check warning count
			if len(result.Warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d", len(result.Warnings), tt.wantWarnings)
				for i, w := range result.Warnings {
					t.Logf("  warning %d: code=%s, msg=%s", i, w.Code, w.Message)
				}
			}

			// Check warning codes
			if len(tt.wantCodes) > 0 {
				gotCodes := make([]string, len(result.Warnings))
				for i, w := range result.Warnings {
					gotCodes[i] = w.Code
				}
				if len(gotCodes) != len(tt.wantCodes) {
					t.Errorf("got codes %v, want %v", gotCodes, tt.wantCodes)
				} else {
					for i, wantCode := range tt.wantCodes {
						if gotCodes[i] != wantCode {
							t.Errorf("warning %d: got code %q, want %q", i, gotCodes[i], wantCode)
						}
					}
				}
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && substringIndex(s, substr) >= 0)
}

func substringIndex(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
