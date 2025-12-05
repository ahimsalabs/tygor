package dev

// Discovery schema types for TypeScript generation.
// These mirror the JSON output from tygorgen/ir/json.go.
// Having concrete structs (vs interfaces) allows tygor gen to produce TypeScript types.

// DiscoverySchema is the top-level discovery.json structure.
type DiscoverySchema struct {
	Package  PackageInfo         `json:"Package"`
	Types    []TypeDescriptor    `json:"Types"`
	Services []ServiceDescriptor `json:"Services"`
	Warnings []Warning           `json:"Warnings,omitempty"`
}

// PackageInfo describes the source Go package.
type PackageInfo struct {
	Path string `json:"Path"`
	Name string `json:"Name"`
	Dir  string `json:"Dir"`
}

// Warning represents a non-fatal issue during schema generation.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ServiceDescriptor describes a service and its endpoints.
type ServiceDescriptor struct {
	Name      string               `json:"name"`
	Endpoints []EndpointDescriptor `json:"endpoints"`
	Doc       string               `json:"doc,omitempty"`
}

// EndpointDescriptor describes an RPC endpoint.
type EndpointDescriptor struct {
	Name      string   `json:"name"`
	FullName  string   `json:"fullName"`
	Primitive string   `json:"primitive"` // "query", "exec", "stream", or "atom"
	Path      string   `json:"path"`
	Request   *TypeRef `json:"request,omitempty"`
	Response  *TypeRef `json:"response,omitempty"`
	Doc       string   `json:"doc,omitempty"`
}

// TypeDescriptor is a named type (struct, enum, or alias).
// Uses kind discriminator for polymorphism.
type TypeDescriptor struct {
	Kind           string            `json:"kind"` // "struct", "enum", "alias"
	Name           GoIdentifier      `json:"Name"`
	TypeParameters []TypeParameter   `json:"TypeParameters,omitempty"`
	Fields         []FieldDescriptor `json:"Fields,omitempty"`     // for struct
	Members        []EnumMember      `json:"Members,omitempty"`    // for enum
	Underlying     *TypeRef          `json:"Underlying,omitempty"` // for alias
	Extends        []GoIdentifier    `json:"Extends,omitempty"`
	Documentation  *Documentation    `json:"Documentation,omitempty"`
	Source         *SourceLocation   `json:"Source,omitempty"`
}

// TypeRef is a reference to a type (used in fields, requests, responses).
// Uses kind discriminator for the various type expression forms.
type TypeRef struct {
	Kind          string    `json:"kind"`                    // "primitive", "reference", "array", "map", "ptr", "union", "typeParameter"
	PrimitiveKind string    `json:"primitiveKind,omitempty"` // for primitive
	BitSize       int       `json:"bitSize,omitempty"`       // for numeric primitives
	Name          string    `json:"name,omitempty"`          // for reference
	Package       string    `json:"package,omitempty"`       // for reference
	Element       *TypeRef  `json:"element,omitempty"`       // for array, ptr
	Length        int       `json:"length,omitempty"`        // for array (0 = slice)
	Key           *TypeRef  `json:"key,omitempty"`           // for map
	Value         *TypeRef  `json:"value,omitempty"`         // for map
	Types         []TypeRef `json:"types,omitempty"`         // for union
	ParamName     string    `json:"paramName,omitempty"`     // for typeParameter
	Constraint    *TypeRef  `json:"constraint,omitempty"`    // for typeParameter
}

// GoIdentifier is a fully-qualified Go type name.
type GoIdentifier struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
}

// FieldDescriptor describes a struct field.
type FieldDescriptor struct {
	Name          string  `json:"name"`
	Type          TypeRef `json:"type"`
	JSONName      string  `json:"jsonName"`
	Optional      bool    `json:"optional,omitempty"`
	StringEncoded bool    `json:"stringEncoded,omitempty"`
	Skip          bool    `json:"skip,omitempty"`
	ValidateTag   string  `json:"validateTag,omitempty"`
	Doc           string  `json:"doc,omitempty"`
}

// EnumMember describes a single enum constant.
type EnumMember struct {
	Name  string `json:"name"`
	Value any    `json:"value"` // string, int64, or float64
	Doc   string `json:"doc,omitempty"`
}

// TypeParameter describes a generic type parameter.
type TypeParameter struct {
	Kind       string   `json:"kind"` // "typeParameter"
	ParamName  string   `json:"paramName"`
	Constraint *TypeRef `json:"constraint,omitempty"`
}

// Documentation holds doc comments.
type Documentation struct {
	Summary    string  `json:"Summary,omitempty"`
	Body       string  `json:"Body,omitempty"`
	Deprecated *string `json:"Deprecated,omitempty"`
}

// SourceLocation points to a position in source code.
type SourceLocation struct {
	File   string `json:"File"`
	Line   int    `json:"Line"`
	Column int    `json:"Column"`
}
