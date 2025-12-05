package typescript

import (
	"context"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/sink"
)

// Generator transforms IR type descriptors into target language source code.
type Generator interface {
	// Name returns the generator's identifier (e.g., "typescript", "python").
	Name() string

	// Generate produces source code for the given schema.
	Generate(ctx context.Context, schema *ir.Schema, opts GenerateOptions) (*GenerateResult, error)
}

// GenerateOptions configures generation behavior.
type GenerateOptions struct {
	// Sink receives generated output files.
	Sink sink.OutputSink

	// Config contains generator-specific configuration.
	Config GeneratorConfig
}

// GenerateResult contains generation output metadata.
type GenerateResult struct {
	// Files lists all files that were written.
	Files []OutputFile

	// TypesGenerated is the count of types successfully generated.
	TypesGenerated int

	// Warnings contains non-fatal issues encountered.
	Warnings []ir.Warning
}

// OutputFile describes a generated file.
type OutputFile struct {
	// Path is the relative path of the generated file.
	Path string

	// Size is the number of bytes written.
	Size int64
}

// GeneratorConfig provides common configuration options.
type GeneratorConfig struct {
	// Naming
	TypePrefix         string // Prepended to all generated type names
	TypeSuffix         string // Appended to all generated type names
	FieldCase          string // "preserve", "camel", "pascal", "snake", "kebab"
	TypeCase           string // "preserve", "camel", "pascal", "snake", "kebab"
	PropertyNameSource string // "field" or "tag:json", "tag:xml", etc.

	// StripPackagePrefix removes this prefix from package paths when qualifying type names.
	// Types from packages matching this prefix are qualified with the remaining path.
	// Example: "github.com/foo/bar/" makes "github.com/foo/bar/api/v1.User" → "api_v1_User"
	// Types from the main package (Schema.Package) are never qualified.
	StripPackagePrefix string

	// Output structure
	SingleFile bool // If true, emit all types in one types.ts. Default false emits one file per package.

	// Formatting
	IndentStyle     string // "space" or "tab"
	IndentSize      int    // Spaces per indent level (when IndentStyle is "space")
	LineEnding      string // "lf" or "crlf"
	TrailingNewline bool   // Ensure files end with a newline

	// Features
	EmitComments bool // Include documentation comments in output

	// Frontmatter is content added to the top of generated type files (after the header comment).
	// Useful for custom type definitions, branded types, or additional imports.
	// Example: "export type DateTime = string & { readonly __brand: 'DateTime' };"
	Frontmatter string

	// TypeMappings maps Go type names to custom TypeScript types.
	// Keys are fully-qualified Go type names (e.g., "time.Time", "github.com/foo/bar.MyType").
	// Values are the TypeScript type to emit instead of the default.
	// Example: map[string]string{"time.Time": "DateTime"} makes time.Time fields emit as DateTime.
	TypeMappings map[string]string

	// Custom contains generator-specific options (e.g., TypeScriptConfig).
	Custom map[string]any
}

// TypeScriptConfig contains TypeScript-specific options.
type TypeScriptConfig struct {
	// EmitExport adds 'export' modifier to declarations.
	EmitExport bool

	// EmitDeclare adds 'declare' modifier (for .d.ts files).
	EmitDeclare bool

	// UseInterface prefers 'interface' over 'type' where possible.
	UseInterface bool

	// UseReadonlyArrays uses 'readonly T[]' instead of 'T[]'.
	UseReadonlyArrays bool

	// EnumStyle controls enum generation.
	// MUST be one of: "enum", "const_enum", "union", "object"
	EnumStyle string

	// OptionalType overrides the default §4.9 behavior for optional/nullable fields.
	// ""/"default" - §4.9 spec behavior: optional (?:) and nullable (| null) are independent
	//                omitempty/omitzero → optional (?:), pointers/slices/maps → nullable (| null)
	//                Both can apply: *T,omitempty → field?: T | null
	// "null"       - force all optional/nullable fields to use | null only (no ?:)
	// "undefined"  - force all optional/nullable fields to use ?: only (no | null)
	OptionalType string

	// UnknownType specifies the type for Go's 'any' or 'interface{}'.
	// SHOULD be one of: "unknown", "any"
	UnknownType string

	// EmitTypeHints adds Go type information as inline comments.
	// Example: `number /* int64 */`, `string /* RFC3339 */`
	// Useful for documentation and debugging precision concerns.
	EmitTypeHints bool

	// NullableSliceElements controls whether []*T generates T[] or (T | null)[].
	// false (default): []*T → T[] - pointers are treated as implementation detail
	// true:            []*T → (T | null)[] - pointers indicate nullable elements
	NullableSliceElements bool
}
