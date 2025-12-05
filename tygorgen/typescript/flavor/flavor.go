// Package flavor provides the interface and utilities for TypeScript output flavors.
// Flavors generate alternative TypeScript outputs like Zod schemas.
package flavor

import (
	"bytes"
	"fmt"

	"tygor.dev/tygorgen/ir"
)

// Flavor represents a code generation flavor (e.g., Zod).
type Flavor interface {
	// Name returns the flavor identifier (e.g., "zod", "zod-mini").
	Name() string

	// FileExtension returns the output file suffix (e.g., ".zod.ts").
	FileExtension() string

	// EmitPreamble generates file-level preamble (imports, utilities).
	EmitPreamble(ctx *EmitContext) []byte

	// EmitType generates flavor-specific code for a single type.
	EmitType(ctx *EmitContext, typ ir.TypeDescriptor) ([]byte, error)

	// EmitInferredType returns true if this flavor exports types via inference
	// (e.g., `export type User = z.infer<typeof UserSchema>`).
	EmitInferredType() bool
}

// EmitContext provides shared context for flavor emission.
type EmitContext struct {
	Schema             *ir.Schema
	IndentStr          string
	EmitTypes          bool              // Whether base types.ts is being generated
	TypeMappings       map[string]string // Go type → TS type overrides
	StripPackagePrefix string

	// Warnings collects non-fatal issues during generation.
	Warnings []string
}

// AddWarning adds a warning message to the context.
func (ctx *EmitContext) AddWarning(format string, args ...any) {
	ctx.Warnings = append(ctx.Warnings, fmt.Sprintf(format, args...))
}

// Get returns a flavor by name, or an error if unknown.
func Get(name string) (Flavor, error) {
	switch name {
	case "zod":
		return &ZodFlavor{mini: false}, nil
	case "zod-mini":
		return &ZodFlavor{mini: true}, nil
	default:
		return nil, fmt.Errorf("unknown flavor: %q", name)
	}
}

// Generate runs a flavor against a schema and returns the output.
func Generate(f Flavor, ctx *EmitContext, types []ir.TypeDescriptor) ([]byte, error) {
	var buf bytes.Buffer

	buf.Write(f.EmitPreamble(ctx))

	for _, typ := range types {
		content, err := f.EmitType(ctx, typ)
		if err != nil {
			return nil, fmt.Errorf("emit %s: %w", typ.TypeName().Name, err)
		}
		if content != nil {
			buf.Write(content)
		}
	}

	return buf.Bytes(), nil
}
