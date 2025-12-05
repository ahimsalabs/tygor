// Package ir defines the Intermediate Representation for Go type descriptors.
// These types are language-agnostic representations of Go types that generators
// transform into target language source code.
package ir

// GoIdentifier represents a named Go entity with package context.
// The Name field contains a sanitized identifier that is always a valid Go identifier.
// For generic instantiations, providers use the synthetic naming algorithm (§3.4)
// to produce names like "Response_User" instead of "Response[User]".
type GoIdentifier struct {
	// Name is the sanitized identifier, always matching [A-Za-z_][A-Za-z0-9_]*.
	// For generic instantiations, synthetic names are generated per §3.4.
	// Examples: "User", "Response_User", "Response_pkg_User", "Map_string_int"
	Name string

	// Package is the fully qualified package path.
	// Empty for builtin types.
	Package string
}

// IsZero returns true if the identifier is empty.
func (id GoIdentifier) IsZero() bool {
	return id.Name == "" && id.Package == ""
}

// Documentation holds documentation comments extracted from Go source.
type Documentation struct {
	// Summary is the first sentence or paragraph, suitable for brief descriptions.
	// Use this for inline comments, tooltips, or single-line descriptions.
	// Example: "User represents a registered user in the system."
	Summary string

	// Body is the complete documentation text, including the summary.
	// Use this when emitting full doc comments (e.g., JSDoc blocks).
	// May contain multiple paragraphs separated by blank lines.
	Body string

	// Deprecated is non-nil if the symbol is marked deprecated.
	// The string value is the deprecation message (may be empty).
	// Use this to emit @deprecated annotations or warnings.
	Deprecated *string
}

// IsZero returns true if the documentation is empty.
func (d Documentation) IsZero() bool {
	return d.Summary == "" && d.Body == "" && d.Deprecated == nil
}

// Source represents source code location information.
type Source struct {
	File   string
	Line   int
	Column int
}

// IsZero returns true if the source location is empty.
func (s Source) IsZero() bool {
	return s.File == "" && s.Line == 0 && s.Column == 0
}

// Warning represents a non-fatal issue encountered during generation.
type Warning struct {
	// Code is a machine-readable warning identifier.
	Code string

	// Message is a human-readable description.
	Message string

	// Source is the location that triggered the warning, if applicable.
	Source *Source

	// TypeName is the type that triggered the warning, if applicable.
	TypeName string
}

// PackageInfo describes a Go package.
type PackageInfo struct {
	// Path is the import path (e.g., "github.com/foo/bar").
	Path string

	// Name is the package name (e.g., "bar").
	Name string

	// Dir is the filesystem directory, if known.
	Dir string
}

// IsZero returns true if the package info is empty.
func (p PackageInfo) IsZero() bool {
	return p.Path == "" && p.Name == "" && p.Dir == ""
}
