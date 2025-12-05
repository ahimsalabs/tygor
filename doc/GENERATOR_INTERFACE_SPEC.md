<!-- snippet-lint-disable -->

# Generator Interface Specification

**Version**: 0.16.2
**Status**: Draft
**Authors**: tygor contributors

## Abstract

This document specifies a pluggable code generator interface for converting Go type definitions and API service definitions into target language representations. The design prioritizes simplicity, extensibility, and flexible output through a sink-based interface supporting filesystem writes and in-memory testing.

## Table of Contents

1. [Terminology](#1-terminology)
   - [1.1 Quick Start Examples](#11-quick-start-examples)
2. [Architecture Overview](#2-architecture-overview)
3. [Input Provider](#3-input-provider)
   - [3.1 Source Provider](#31-source-provider)
   - [3.2 Reflection Provider](#32-reflection-provider)
   - [3.3 Provider Behavioral Requirements](#33-provider-behavioral-requirements)
   - [3.4 Generic Type Handling](#34-generic-type-handling)
   - [3.5 Anonymous Struct Handling](#35-anonymous-struct-handling)
4. [Intermediate Representation](#4-intermediate-representation)
   - [4.9 Nullable and Optional Field Mapping](#49-nullable-and-optional-field-mapping)
5. [Generator Interface](#5-generator-interface)
6. [Configuration](#6-configuration)
7. [Output Requirements](#7-output-requirements)
8. [Extension Points](#8-extension-points)
- [Appendix A: Primitive Type Mappings](#appendix-a-primitive-type-mappings)
- [Appendix B: Reserved Words](#appendix-b-reserved-words)
- [Appendix C: Future Extensions](#appendix-c-future-extensions)
- [Appendix D: Design Rationale](#appendix-d-design-rationale)

## 1. Terminology

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### Core Concepts

- **IR**: Intermediate Representation - language-agnostic type descriptors
- **Generator**: A component that transforms IR into target language source code
- **Emitter**: A component responsible for writing formatted output
- **Resolver**: A component that resolves type references and dependencies
- **Provider**: A component that extracts type information from Go code and produces IR

### Type Descriptor Categories

- **Named type descriptor**: A top-level type that appears in `Schema.Types`. Only three kinds: `StructDescriptor`, `AliasDescriptor`, and `EnumDescriptor`. These represent Go type declarations.
- **Expression type descriptor**: A type expression nested within fields or other type expressions. Includes: `PrimitiveDescriptor`, `ArrayDescriptor`, `MapDescriptor`, `ReferenceDescriptor`, `PtrDescriptor`, `UnionDescriptor`, and `TypeParameterDescriptor`. These never appear directly in `Schema.Types`.

### 1.1 Quick Start Examples

**Example 1: Struct to TypeScript**

```go
// Go input
type User struct {
    ID    string `json:"id"`
    Email string `json:"email"`
    Age   *int   `json:"age,omitempty"`
}
```

```typescript
// TypeScript output
export interface User {
    readonly id: string;
    readonly email: string;
    readonly age?: number | null;
}
```

**Example 2: Enum generation**

```go
// Go input
type Status string

const (
    StatusPending  Status = "pending"
    StatusApproved Status = "approved"
    StatusRejected Status = "rejected"
)
```

```typescript
// TypeScript output (union style)
export type Status = "pending" | "approved" | "rejected";
```

**Example 3: Service endpoint**

```go
// Go service definition
type UsersService interface {
    Get(ctx context.Context, req *GetUserRequest) (*User, error)
}
```

```typescript
// TypeScript manifest output
export interface Manifest {
    "Users.Get": { req: GetUserRequest; res: User };
}
```

## 2. Architecture Overview

### 2.1 Data Flow

```
     ┌─────────────────────────────────┐   ┌─────────────────────────────────┐
     │      Source Input Provider      │   │    Reflection Input Provider    │
     │   (analyzes via go/types)       │   │    (extracts via reflect)       │
     │          [PRIMARY]              │   │         [SECONDARY]             │
     └────────────────┬────────────────┘   └────────────────┬────────────────┘
                      │                                     │
                      └──────────────────┬──────────────────┘
                                         │
                                         ▼
                      ┌─────────────────────────────────────┐
                      │           Schema Builder            │
                      │  (normalizes to common IR format)   │
                      └──────────────────┬──────────────────┘
                                       │
                                       ▼
                    ┌─────────────────────────────────────┐
                    │         Type Descriptors (IR)       │
                    └──────────────────┬──────────────────┘
                                       │
                    ┌──────────────────┼──────────────────┐
                    ▼                  ▼                  ▼
             ┌────────────┐     ┌────────────┐     ┌────────────┐
             │ TypeScript │     │    Zod     │     │    JSON    │
             │ Generator  │     │ Generator  │     │   Schema   │
             └─────┬──────┘     └─────┬──────┘     └─────┬──────┘
                   │                  │                  │
                   └──────────────────┼──────────────────┘
                                      │
                                      ▼
                    ┌─────────────────────────────────────┐
                    │            Output Sinks             │
                    ├───────────────────┬─────────────────┤
                    │    Filesystem     │     Memory      │
                    │       Sink        │      Sink       │
                    └───────────────────┴─────────────────┘
                              │                  │
                              ▼                  ▼
                           Files             Testing
                          on Disk            Fixtures
```

### 2.2 Design Principles

1. **No External Runtimes**: The core framework MUST NOT require external language runtimes. Individual generator implementations MAY use external tools (e.g., formatters, AST libraries) and SHOULD document these dependencies.

2. **Output Agnostic**: The framework specifies output interfaces, not implementation strategies. Generators MAY use string templates, AST construction, or any other method to produce output.

3. **Go-First IR**: The IR is derived from Go's type system. All IR constructs MUST have direct Go equivalents. Target language-specific concerns MUST be handled exclusively by generators.

4. **Deterministic Output**: Given identical input and configuration, generators MUST produce byte-identical output.

5. **Sink Flexibility**: Output MUST flow through a pluggable sink interface supporting filesystem writes, in-memory buffers, and streaming responses.

## 3. Input Provider

Input providers extract type information from Go code and convert it to the intermediate representation. Two provider types are supported:

- **Source Provider** (primary): Uses `go/types` to analyze Go source code at build time
- **Reflection Provider** (secondary): Uses `reflect` package for runtime type extraction

### 3.1 Source Provider

The source provider uses Go's `go/types` package to analyze source code statically. This is the **primary** input provider and supports the full IR feature set.

```go
// SourceProvider extracts types by analyzing Go source code.
type SourceProvider struct{}

// SourceInputOptions configures source-based type extraction.
type SourceInputOptions struct {
    // Packages are the Go package paths to analyze.
    Packages []string

    // RootTypes are the type names to extract (e.g., "User", "CreateRequest").
    // If empty, all exported types in the packages are extracted.
    RootTypes []string
}

// BuildSchema analyzes source code and returns a Schema.
// The provider recursively extracts all types reachable from RootTypes.
func (p *SourceProvider) BuildSchema(ctx context.Context, opts SourceInputOptions) (*Schema, error)
```

**Capabilities:**

| Feature | Source Provider | Reflection Provider |
|---------|-----------------|---------------------|
| Documentation | YES - extracts doc comments | NO - returns zero value |
| Source Locations | YES - file/line/column | NO - returns zero value |
| Unexported Fields | YES - full access | LIMITED - names only |
| Generics | FULL - preserves type parameters | INSTANTIATED ONLY |
| Enums | YES - enumerates const values | NO - appears as aliases |

**Usage Example:**

```go
provider := &SourceProvider{}
schema, err := provider.BuildSchema(ctx, SourceInputOptions{
    Packages:  []string{"github.com/example/api"},
    RootTypes: []string{"User", "CreateUserRequest", "ListUsersParams"},
})
// schema.Types contains User, CreateUserRequest, ListUsersParams, and all reachable types
```

### 3.2 Reflection Provider

The reflection provider uses Go's `reflect` package to extract type information at runtime. This secondary provider exists primarily to validate that the IR abstraction is not overly coupled to `go/types` internals. Production use cases SHOULD prefer the Source Provider for its richer feature set.

```go
// ReflectionProvider extracts types using runtime reflection.
type ReflectionProvider struct{}

// ReflectionInputOptions configures reflection-based type extraction.
type ReflectionInputOptions struct {
    // RootTypes are the types to extract, specified as reflect.Type values.
    RootTypes []reflect.Type
}

// BuildSchema extracts types and returns a Schema.
func (p *ReflectionProvider) BuildSchema(ctx context.Context, opts ReflectionInputOptions) (*Schema, error)
```

**Usage Example:**

```go
provider := &ReflectionProvider{}
schema, err := provider.BuildSchema(ctx, ReflectionInputOptions{
    RootTypes: []reflect.Type{
        reflect.TypeOf(MyStruct{}),
        reflect.TypeOf((*MyInterface)(nil)).Elem(),
    },
})
// schema.Types contains MyStruct and all types reachable from it
```

### 3.3 Provider Behavioral Requirements

Both providers MUST satisfy the following requirements. Requirements specific to one provider are noted.

#### 3.3.1 Type Extraction

1. Providers MUST extract struct field names and types. Source provider uses `go/types`; reflection provider uses `reflect`.
2. Providers MUST extract struct tag values (json, validate, etc.).
3. Providers MUST NOT emit duplicate types to `Schema.Types`.
4. Providers MUST use the package path and type name to construct `GoIdentifier` values. For generic instantiations, providers MUST apply the synthetic naming algorithm (§3.4).
5. **Source provider only**: MUST extract documentation comments for types and fields.
6. **Reflection provider only**: MUST represent generic types as their instantiated form (e.g., `Response[User]` becomes a concrete type with `User` substituted).

#### 3.3.2 Special Type Handling

| Go Type | IR Representation | Notes |
|---------|-------------------|-------|
| `[]byte`, `[]uint8` | `PrimitiveBytes` | Matches `encoding/json` behavior |
| `time.Time` | `PrimitiveTime` | RFC 3339 format in JSON |
| `time.Duration` | `PrimitiveDuration` | int64 nanoseconds in JSON |
| `interface{}`, `any` | `PrimitiveAny` | Empty interface |
| `uintptr` | `PrimitiveUint` | Encoded as unsigned integer |
| `json.Number` | `PrimitiveString` | Serializes as raw JSON number |
| `json.RawMessage` | `PrimitiveAny` | Embeds raw JSON content |

#### 3.3.3 Interface Handling

1. For named interface types (e.g., `io.Reader`, custom interfaces), providers MUST emit `PrimitiveAny` and MUST emit a warning.
2. For embedded interface types (e.g., `type Foo struct { io.Reader }`), providers MUST emit the embedded field as a regular field with the interface name as both Go name and JSON name, typed as `PrimitiveAny`. Providers SHOULD emit a warning, as embedded interfaces in API types usually indicate a design issue. Note: `encoding/json` does serialize embedded interfaces (as `"Reader":null` when nil).

#### 3.3.4 Field Visibility

Providers MUST skip unexported struct fields. Unexported fields are not serializable by `encoding/json` and MUST NOT appear in `FieldDescriptor` output.

#### 3.3.5 Enum Detection (Source Provider Only)

The source provider MUST extract enum values from const blocks where the const type is a defined type (e.g., `type Status string`). A type is treated as an enum if:
1. It is a defined type with an underlying primitive type (string, int, etc.), AND
2. There exist one or more package-level const declarations of that type.

Constants of built-in types (e.g., `const X = 5` without a typed const) are NOT considered enums.

#### 3.3.6 Custom Marshaler Handling

1. For types implementing `json.Marshaler` or `encoding.TextMarshaler`, providers MUST emit `PrimitiveAny` and SHOULD emit a warning. The provider cannot statically determine the JSON output of custom marshal methods. Exception: well-known types like `time.Time` and `time.Duration` have dedicated primitive kinds.
2. For types implementing `json.Unmarshaler` or `encoding.TextUnmarshaler` without a corresponding marshaler, providers SHOULD emit a warning noting that unmarshal-only custom handling will not affect the generated types.

#### 3.3.7 Error Conditions

Providers MUST return an error when encountering:

1. **Unmarshalable types**: `chan T`, `complex64`, `complex128`, `func(...)`, and `unsafe.Pointer`. These types cause `json.Marshal` to return an `UnsupportedTypeError`.
2. **Unsupported map key types**: Supported key types are `string`, integer types (`int`, `int8`..`int64`, `uint`, `uint8`..`uint64`), and types implementing `encoding.TextMarshaler`. Unsupported key types include `bool`, `float32`, `float64`, `complex64`, `complex128`, and structs without `TextMarshaler`. See Appendix A for the complete map key restrictions table.

### 3.4 Generic Type Handling

Generic type handling differs between providers:

**Source Provider:** Has access to type parameters and can preserve generic definitions. The provider MUST emit generic types with their type parameters preserved (e.g., `Response<T>`). This enables generators to produce generic output in target languages that support generics.

**Reflection Provider:** Only sees generic types after type arguments have been substituted. For example:

```go
type Response[T any] struct {
    Data T
    Meta Metadata
}

// When registered as Response[User], reflection sees:
// - A struct named "Response[example.com/api.User]" (or similar)
// - Field "Data" with type User (not T)
// - Field "Meta" with type Metadata
```

The reflection provider handles this by:
1. Emitting each concrete usage as a separate type (e.g., `Response[User]` and `Response[Post]` become two types)
2. Using a sanitized name (e.g., `GenBar_string` for `GenBar[string]`)
3. Using concrete types in all field types (e.g., `Data` has type `User`, not `T`)

**Synthetic Name Requirements:**

All synthetic names (for anonymous structs, generic instantiations, etc.) MUST be valid Go identifiers: they MUST match the pattern `[A-Za-z_][A-Za-z0-9_]*`. This ensures generators can rely on basic identifier safety without special-case handling for arbitrary characters.

Generators MUST accept whatever name the provider produces in `GoIdentifier.Name` and transform it appropriately for the target language (e.g., case conversion, reserved word escaping).

**Synthetic Name Algorithm for Generic Instantiations:**

Providers MUST use the following algorithm to generate synthetic names for generic type instantiations:

```
SyntheticGenericName(baseName string, typeArgs []string) string:
    result := baseName
    for each arg in typeArgs:
        // Normalize the type argument to a valid identifier fragment
        arg = strings.ReplaceAll(arg, ".", "_")   // package paths: "pkg.Type" -> "pkg_Type"
        arg = strings.ReplaceAll(arg, "[", "_")   // nested generics: "Outer[Inner]" -> "Outer_Inner"
        arg = strings.ReplaceAll(arg, "]", "")    // remove closing brackets
        arg = strings.ReplaceAll(arg, ",", "_")   // multiple args: "K, V" -> "K_V"
        arg = strings.ReplaceAll(arg, " ", "")    // remove spaces
        arg = strings.ReplaceAll(arg, "*", "Ptr") // pointers: "*T" -> "PtrT"
        result = result + "_" + arg
    return result
```

**Examples:**

| Go Type | Synthetic Name |
|---------|----------------|
| `Response[User]` | `Response_User` |
| `Map[string, int]` | `Map_string_int` |
| `Response[pkg.User]` | `Response_pkg_User` |
| `Nested[Outer[Inner]]` | `Nested_Outer_Inner` |
| `Pair[*Foo, Bar]` | `Pair_PtrFoo_Bar` |

This algorithm ensures:
1. Consistent naming across providers
2. Valid Go identifiers (no brackets, dots, or special characters)
3. Deterministic output for the same input
4. Readable names that preserve type argument information

**Recursive Generic Cycle Detection:**

Providers MUST detect and handle recursive generic type instantiation to avoid infinite expansion. A recursive generic cycle occurs when instantiating a generic type leads back to an instantiation of itself with different type arguments.

Example:

```go
type Container[T any] struct {
    Item   T
    Nested *Container[Container[T]]
}
```

When extracting `Container[string]`, the provider encounters:
1. `Container[string]` → field `Nested` has type `*Container[Container[string]]`
2. `Container[Container[string]]` → field `Nested` has type `*Container[Container[Container[string]]]`
3. This continues infinitely...

Providers MUST:
1. Track the set of generic instantiations currently being expanded (the "expansion stack")
2. When encountering a type reference that would create an infinite expansion, emit a `TypeRefDescriptor` pointing to the base generic type name
3. SHOULD emit a warning when a recursive generic cycle is detected

This allows generators to produce valid output while alerting users to potentially problematic type definitions. Note that such types may still be useful at runtime with careful construction.

### 3.5 Anonymous Struct Handling

Go allows anonymous struct types in field declarations:

```go
type Outer struct {
    Inner struct {
        X int
        Y string
    } `json:"inner"`
}
```

With `encoding/json`, anonymous structs serialize as nested objects:

```json
{"inner": {"X": 0, "Y": ""}}
```

Providers MUST handle anonymous structs by generating synthetic named types:

1. When encountering an anonymous struct, the provider MUST generate a synthetic name using the pattern `{ParentType}_{FieldName}`.
2. The synthetic type MUST be added to `Schema.Types` as a `StructDescriptor`.
3. The field MUST use a `ReferenceDescriptor` pointing to the synthetic type.

**Example:**

Given the Go type above, the provider emits:

```go
// Schema.Types contains:
&StructDescriptor{
    Name: GoIdentifier{Name: "Outer", Package: "example.com/api"},
    Fields: []FieldDescriptor{{
        Name:     "Inner",
        JSONName: "inner",
        Type:     &ReferenceDescriptor{Target: GoIdentifier{Name: "Outer_Inner", Package: "example.com/api"}},
    }},
}

&StructDescriptor{
    Name: GoIdentifier{Name: "Outer_Inner", Package: "example.com/api"},
    Fields: []FieldDescriptor{
        {Name: "X", JSONName: "X", Type: &PrimitiveDescriptor{PrimitiveKind: PrimitiveInt}},
        {Name: "Y", JSONName: "Y", Type: &PrimitiveDescriptor{PrimitiveKind: PrimitiveString}},
    },
}
```

**Nested Anonymous Structs:**

For deeply nested anonymous structs, names chain: `Outer_Inner_Nested`.

**Name Collision Handling:**

If the generated synthetic name collides with an existing type name in the same package, the provider MUST return an error. For example:

```go
type Outer struct {
    Inner struct { X int }  // Would generate "Outer_Inner"
}
type Outer_Inner struct { Y int }  // Collision!
```

This is an error condition, not something the provider attempts to resolve automatically. The user must rename one of the types.

## 4. Intermediate Representation

### 4.1 Core Types

The IR MUST support the following type descriptor interfaces:

```go
// TypeDescriptor is the base interface for all type descriptors.
type TypeDescriptor interface {
    // Kind returns the descriptor kind for type switching.
    Kind() DescriptorKind

    // TypeName returns the canonical name of this type.
    // Returns zero value for expression types (primitives, arrays, etc).
    TypeName() GoIdentifier

    // Doc returns associated documentation comments.
    // Returns zero value for expression types.
    Doc() Documentation

    // Src returns the original Go source location.
    // Returns zero value for expression types.
    Src() Source
}
```

### 4.2 Descriptor Kinds

```go
// DescriptorKind identifies the category of a type descriptor.
type DescriptorKind int

const (
    // Named type descriptors (appear in Schema.Types)
    KindStruct    DescriptorKind = iota // Object type with named fields (Go struct)
    KindAlias                           // Type alias (type X = Y)
    KindEnum                            // Enumeration of constants

    // Expression type descriptors (appear nested in fields/types)
    KindPrimitive                       // Built-in primitive type
    KindArray                           // Ordered collection ([]T or [N]T)
    KindMap                             // Key-value mapping (map[K]V)
    KindReference                       // Reference to another type
    KindPtr                             // Pointer wrapper (*T)
    KindUnion                           // Union of types (T1 | T2 | ...)
    KindTypeParameter                   // Generic type parameter (T, K, V, etc.)
)
```

Implementations MUST support the following descriptor kinds:

**Named Type Descriptors** (top-level, appear in `Schema.Types`):

| Kind | Description | Go Equivalent | Output Example (TypeScript) |
|------|-------------|---------------|----------------------------|
| `KindStruct` | Object type with named fields | `struct` | `interface Foo { }` |
| `KindAlias` | Type alias | `type X = Y` | `type X = Y` |
| `KindEnum` | Enumeration of constants | `const` block with `iota` | `enum X { }` |

**Expression Type Descriptors** (nested within fields and types, NOT in `Schema.Types`):

| Kind | Description | Go Equivalent | Output Example (TypeScript) |
|------|-------------|---------------|----------------------------|
| `KindPrimitive` | Built-in primitive type | `string`, `int`, `bool`, etc. | `string`, `number` |
| `KindArray` | Ordered collection | `[]T` or `[N]T` | `T[]` or `[T, T, T]` |
| `KindMap` | Key-value mapping | `map[K]V` | `Record<K, V>` |
| `KindReference` | Reference to another type | Named type usage | `Foo` |
| `KindPtr` | Pointer wrapper | `*T` | `T \| null` or `T?` (see §4.9) |
| `KindUnion` | Union of types | `~string \| ~int` (constraints) | `string \| number` |
| `KindTypeParameter` | Generic type parameter | `T` in `type Foo[T any]` | `T` |

### 4.3 Struct Descriptor

```go
// StructDescriptor represents a structured object type (Go struct).
type StructDescriptor struct {
    Name           GoIdentifier
    TypeParameters []TypeParameterDescriptor // Generic type parameters (source provider only)
    Fields         []FieldDescriptor
    Extends        []GoIdentifier  // Embedded types without json tags (inheritance)
    Documentation  Documentation
    Source         Source
}

func (d *StructDescriptor) Kind() DescriptorKind { return KindStruct }

// FieldDescriptor represents a single field within a struct.
type FieldDescriptor struct {
    // Name is the Go field name.
    Name string

    // Type is the field's type descriptor.
    Type TypeDescriptor

    // JSONName is the serialized property name (from json tag).
    // Falls back to Name if json tag is absent.
    JSONName string

    // Optional indicates the field can be absent from JSON output.
    // This is true when json:",omitempty" or json:",omitzero" is set.
    //
    // For type generation, omitempty and omitzero have identical effects:
    // both make a field optional (field?: T in TypeScript). The behavioral
    // differences (omitempty omits empty collections while omitzero keeps them;
    // omitzero omits zero structs while omitempty keeps them) are runtime
    // concerns that don't affect the generated type signature.
    //
    // Providers MUST set Optional=true when either tag is present.
    Optional bool

    // StringEncoded indicates json:",string" was set.
    // When true, the field is encoded as a JSON string on the wire.
    // Only valid for string, integer, floating-point, or boolean types.
    // Note: encoding/json silently ignores the ",string" option for other types
    // (structs, slices, maps, etc.) - the field is encoded normally without error.
    // Providers SHOULD only set StringEncoded=true when the field type is one of
    // the supported types; for unsupported types, providers SHOULD emit a warning.
    // Generators SHOULD emit the wire type (string) rather than the Go type,
    // since clients send and receive the string-encoded form.
    StringEncoded bool

    // Skip indicates json:"-" was set.
    // When true, the field should not appear in generated output.
    Skip bool

    // ValidateTag is the raw value from the `validate` struct tag.
    // Empty string if no validate tag is present.
    //
    // Example: "required,min=3,email" or "omitempty,gt=0,lte=100"
    //
    // The tag syntax follows go-playground/validator conventions. Generators
    // are responsible for parsing and interpreting the tag value as needed
    // for their target language. Generators that do not recognize a constraint
    // SHOULD pass it through unchanged or emit a warning, rather than failing.
    ValidateTag string

    // RawTags preserves all struct tags for generator-specific handling.
    // Keys are tag names (e.g., "json", "validate", "db").
    // Generators SHOULD prefer the parsed fields (JSONName, Optional,
    // StringEncoded, ValidateTag) over RawTags for JSON serialization concerns.
    // RawTags is provided for generator-specific tag handling (e.g., "db", "xml",
    // "gorm" tags) that falls outside the JSON serialization concern.
    RawTags map[string]string

    // Documentation for this field.
    Documentation Documentation
}
```

**Embedding Rules:**

The schema builder splits embedded struct fields based on json tag presence:

| Embedded Field | JSON Tag | Result |
|----------------|----------|--------|
| `Bar` | none | Added to `Extends` |
| `*Bar` | none | Added to `Extends` (pointer dereferenced) |
| `Bar` | `json:"bar"` | Added to `Fields` with `JSONName: "bar"` |
| `Bar` | `json:"-"` | Skipped entirely (not in `Extends` or `Fields`) |

This matches Go's `encoding/json` marshaling semantics:
- No tag: embedded fields are flattened (inheritance)
- Named tag: embedded field becomes a nested object
- Skip tag: embedded field is excluded from serialization

**Nil Embedded Pointers:**

When an embedded pointer field is nil at runtime, `encoding/json` omits all fields from that embedded struct. For example:

```go
type Outer struct {
    *Inner  // embedded pointer
    Own string `json:"own"`
}
type Inner struct {
    Field string `json:"field"`
}

// With Inner=nil: {"own":"value"}
// With Inner non-nil: {"field":"inner_value","own":"value"}
```

This is a runtime behavior. For type generation, providers emit the embedded fields as if they were present (via `Extends`). Generators MAY note that fields from pointer-embedded structs can be absent at runtime, but this is typically left to runtime validation.

**Embedded Field Name Conflicts:**

When multiple embedded structs have fields with the same JSON name at the same nesting depth, `encoding/json` applies these rules:

1. If any of the conflicting fields have explicit JSON tags, only tagged fields are considered
2. If exactly one field remains after step 1, that field is used
3. If multiple fields remain at the same depth, ALL are omitted (no error)

For example:
```go
type A struct { Field string `json:"x"` }
type B struct { Field string `json:"x"` }
type Outer struct { A; B }  // "x" is omitted due to ambiguity
```

Providers SHOULD return an error when detecting same-depth field name conflicts, as this usually indicates a design issue. Alternatively, providers MAY accept a configuration option to choose a disambiguation strategy (e.g., prefer first, prefer tagged).

**TypeScript Output Example:**

Given:
```go
type Foo struct {
    Bar
    Baz `json:"baz"`
}
```

Generates:
```typescript
export interface Foo extends Bar {
    readonly baz: Baz;
}
```

**Multiple Embedding Example:**

Given:
```go
type Foo struct {
    Bar
    GenBar[string]
}

type Bar struct {
    BarField int
}

type GenBar[T comparable] struct {
    GenBarField T
}
```

Generates (with reflection provider using instantiated names):
```typescript
export interface Foo extends Bar, GenBar_string {
}

export interface Bar {
    readonly BarField: number;
}

export interface GenBar_string {
    readonly GenBarField: string;
}
```

Note: Source-based providers may preserve generic syntax (`GenBar<string>`) for languages that support generics.

### 4.4 Alias Descriptor

```go
// AliasDescriptor represents a type alias or defined type.
//
// Note: Constraint interfaces (e.g., `type Stringish interface { ~string | ~int }`)
// are emitted as AliasDescriptor with Underlying set to a UnionDescriptor. These
// appear in Schema.Types for reference resolution when type parameters use them
// as constraints. Generators typically emit these as type aliases in the target
// language. Since Go forbids using constraint-only interfaces as variable types,
// these are NOT intended to be instantiable types—they exist solely for type
// parameter constraints.
type AliasDescriptor struct {
    Name           GoIdentifier
    TypeParameters []TypeParameterDescriptor // Generic type parameters (source provider only)
    Underlying     TypeDescriptor
    Documentation  Documentation
    Source         Source
}

func (d *AliasDescriptor) Kind() DescriptorKind { return KindAlias }
```

### 4.5 Enum Descriptor

```go
// EnumDescriptor represents an enumeration.
// NOTE: Reflection provider cannot produce EnumDescriptor (cannot enumerate const values).
// This descriptor is only available from source-based providers.
type EnumDescriptor struct {
    Name          GoIdentifier
    Members       []EnumMember
    Documentation Documentation
    Source        Source
}

func (d *EnumDescriptor) Kind() DescriptorKind { return KindEnum }

// EnumMember represents a single enum variant.
type EnumMember struct {
    Name  string
    // Value is the constant value. Providers MUST convert Go constant values
    // to one of exactly three types: string, int64, or float64.
    // Generators can rely on type assertions to these concrete types.
    Value         any
    Documentation Documentation
}
```

### 4.6 Expression Type Descriptors

These descriptors represent **type expressions** used within fields, aliases, and other contexts. Unlike named type descriptors (Struct, Alias, Enum), expression descriptors:

- Implement the `TypeDescriptor` interface (so they can be assigned to `TypeDescriptor` fields)
- Return zero values from `TypeName()`, `Doc()`, and `Src()` methods
- Appear nested within fields and other type expressions, not in `Schema.Types`

```go
// All expression descriptors embed this to provide zero-value implementations
// of the TypeDescriptor interface methods they don't use.
type exprBase struct{}

func (exprBase) TypeName() GoIdentifier { return GoIdentifier{} }
func (exprBase) Doc() Documentation     { return Documentation{} }
func (exprBase) Src() Source            { return Source{} }
```

**Expression Descriptors:**

```go
type PrimitiveKind int

const (
    PrimitiveBool PrimitiveKind = iota
    PrimitiveInt      // Signed integer (see BitSize)
    PrimitiveUint     // Unsigned integer (see BitSize)
    PrimitiveFloat    // Floating point (see BitSize)
    PrimitiveString
    PrimitiveBytes    // []byte (base64-encoded in JSON)
    PrimitiveTime     // time.Time (RFC 3339 string in JSON)
    PrimitiveDuration // time.Duration (nanoseconds as int64 in JSON)
    PrimitiveAny      // interface{} / any
    PrimitiveEmpty    // struct{} (empty struct, serializes as {})
)

// PrimitiveDescriptor represents a built-in primitive type.
type PrimitiveDescriptor struct {
    exprBase
    PrimitiveKind PrimitiveKind

    // BitSize specifies the size for numeric types (PrimitiveInt, PrimitiveUint, PrimitiveFloat).
    // Valid values:
    // - 0: Platform-dependent size (Go's `int`, `uint`)
    // - 8, 16, 32, 64: Explicit bit width
    //
    // Ignored for non-numeric primitive kinds.
    //
    // Generators targeting languages with rich numeric types (Rust, Zod) SHOULD use BitSize
    // to emit precise types or validation. Generators targeting languages with single numeric
    // types (TypeScript, Python) MAY ignore BitSize.
    //
    // Example mappings:
    // - {PrimitiveKind: PrimitiveInt, BitSize: 32} -> TypeScript: number, Rust: i32, Zod: z.number().int()
    // - {PrimitiveKind: PrimitiveInt, BitSize: 64} -> TypeScript: number (⚠️), Rust: i64, Zod: z.number().int()
    // - {PrimitiveKind: PrimitiveFloat, BitSize: 64} -> TypeScript: number, Rust: f64, Zod: z.number()
    BitSize int
}

func (d *PrimitiveDescriptor) Kind() DescriptorKind { return KindPrimitive }

// ArrayDescriptor represents an ordered collection (slice or fixed-length array).
//
// Nullability: Go slices (Length == 0) can be nil, which serializes to JSON null.
// This is NOT represented with PtrDescriptor; instead, generators derive nullability
// from context:
// - If Optional=false: field: T[] | null (always present, can be null)
// - If Optional=true: field?: T[] (optional, never null when present)
// See §4.9 for the complete decision tree.
//
// Note: [N]byte fixed arrays serialize as JSON arrays of numbers, NOT base64.
// Only []byte slices are base64-encoded (represented as PrimitiveBytes).
type ArrayDescriptor struct {
    exprBase
    Element TypeDescriptor

    // Length is 0 for slices ([]T), or >0 for fixed-length arrays ([N]T).
    // Generators MAY emit tuples for fixed arrays in languages that support them.
    Length int
}

func (d *ArrayDescriptor) Kind() DescriptorKind { return KindArray }

// MapDescriptor represents a key-value mapping.
//
// Nullability: Go maps can be nil, which serializes to JSON null.
// This is NOT represented with PtrDescriptor; instead, generators derive nullability
// from context:
// - If Optional=false: field: Record<K,V> | null (always present, can be null)
// - If Optional=true: field?: Record<K,V> (optional, never null when present)
// See §4.9 for the complete decision tree.
type MapDescriptor struct {
    exprBase
    Key   TypeDescriptor
    Value TypeDescriptor
}

func (d *MapDescriptor) Kind() DescriptorKind { return KindMap }
```

**Map Key Serialization:**

Go's `encoding/json` marshals map keys to JSON object property names, which are always strings. The `MapDescriptor.Key` field preserves the original Go key type, but generators MUST understand that keys are serialized as strings on the wire.

| Go Map Type | JSON Wire Format | TypeScript Output |
|-------------|------------------|-------------------|
| `map[string]T` | `{"key": value}` | `Record<string, T>` |
| `map[int]T` | `{"123": value}` | `Record<string, T>` |
| `map[MyEnum]T` | `{"enumValue": value}` | `Record<string, T>` |
| `map[K]T` where K implements `TextMarshaler` | `{"marshaledKey": value}` | `Record<string, T>` |

For non-string key types, `encoding/json` converts keys as follows:
- Integer types: decimal string representation
- Types implementing `encoding.TextMarshaler`: result of `MarshalText()`
- String-based types (e.g., `type MyKey string`): the underlying string value

**Unsupported key types**: `bool` and other non-integer, non-string types that don't implement `TextMarshaler` will cause `json.Marshal` to return an error. Providers SHOULD reject or warn on such types.

**Generator Key Type Handling:**

For primitive key types (`int`, `bool`, etc.), generators SHOULD emit `Record<string, V>` since the wire format uses string keys and there's no additional type safety to preserve.

For named string-based types (type aliases like `type UserID string`), generators SHOULD preserve the key type to maintain type safety in the target language:

```go
// Go source
type UserID string
type UserMap map[UserID]User
```

```typescript
// TypeScript output (preserving key type)
type UserID = string & { readonly __brand: "UserID" };
type UserMap = Record<UserID, User>;
```

This is possible because:
1. The IR represents the key as a `ReferenceDescriptor` pointing to `UserID`
2. `UserID` appears in `Schema.Types` as an `AliasDescriptor` with underlying type `PrimitiveString`
3. The generator can emit a branded/newtype string alias and use it in the Record

Generators that support branded types SHOULD preserve string-based alias keys. Generators that don't support branded types SHOULD fall back to `Record<string, V>`.

```go
// ReferenceDescriptor represents a reference to a named type.
type ReferenceDescriptor struct {
    exprBase
    Target GoIdentifier
}

func (d *ReferenceDescriptor) Kind() DescriptorKind { return KindReference }

// PtrDescriptor represents a Go pointer type (*T).
// The TypeScript output depends on field context (see §4.9):
// - If Optional=false: field: T | null (always present, can be null)
// - If Optional=true: field?: T (optional, never null when present)
type PtrDescriptor struct {
    exprBase
    Element TypeDescriptor
}

func (d *PtrDescriptor) Kind() DescriptorKind { return KindPtr }

// UnionDescriptor represents a union of types (T1 | T2 | ...).
//
// SCOPE: UnionDescriptor currently appears ONLY within TypeParameterDescriptor.Constraint
// to represent Go type constraint unions (e.g., `~string | ~int`). It does NOT appear
// as field types or in other contexts.
//
// Note: Go's `~T` (approximate type) syntax is not preserved in the IR. Both `~string`
// and `string` in a constraint produce the same PrimitiveDescriptor. The tilde only
// affects Go compile-time type checking, not JSON serialization behavior.
//
// Note: This is NOT used for nullable types (use PtrDescriptor) or
// optional fields (use Optional on FieldDescriptor).
type UnionDescriptor struct {
    exprBase
    // Types contains the union members. Must have at least 1 element.
    // Single-element unions are valid (e.g., [T ~string] has one union term).
    Types []TypeDescriptor
}

func (d *UnionDescriptor) Kind() DescriptorKind { return KindUnion }

// TypeParameterDescriptor represents a generic type parameter.
// This descriptor is only produced by the source provider; the reflection
// provider sees instantiated types and emits concrete types instead.
//
// Note: TypeParameterDescriptor appears in two contexts:
// - Declaration: In StructDescriptor.TypeParameters or AliasDescriptor.TypeParameters,
//   where ParamName and Constraint define the type parameter.
// - Usage: As a field type (FieldDescriptor.Type), where only ParamName is used to
//   reference back to the declaration. In usage context, Constraint is ignored.
type TypeParameterDescriptor struct {
    exprBase
    // ParamName is the type parameter name (e.g., "T", "K", "V").
    ParamName string

    // Constraint is the type set constraint, represented as a TypeDescriptor.
    // nil means unconstrained (equivalent to `any`).
    //
    // Common constraint patterns and their IR representation:
    // - [T any]              -> Constraint: nil
    // - [T comparable]       -> Constraint: nil (see note below)
    // - [T ~string]          -> Constraint: &UnionDescriptor{Types: [PrimitiveString]}
    // - [T ~string | ~int]   -> Constraint: &UnionDescriptor{Types: [PrimitiveString, PrimitiveInt]}
    // - [T MyConstraint]     -> Constraint: &ReferenceDescriptor{Target: "MyConstraint"}
    //
    // For named constraint interfaces (like `type MyConstraint interface { ~string | ~int }`),
    // providers SHOULD emit the constraint as a named type in Schema.Types and use a
    // ReferenceDescriptor here. For inline union constraints, use UnionDescriptor directly.
    //
    // Note on `comparable`: The `comparable` constraint is a Go compile-time concept
    // that does not affect JSON serialization. It is NOT preserved in the IR because:
    // - TypeScript: All JS values support equality comparison
    // - JSON Schema/Zod: No equivalent concept
    // - It adds no information for type generation
    // Providers seeing [T comparable] emit Constraint: nil (same as [T any]).
    Constraint TypeDescriptor
}

func (d *TypeParameterDescriptor) Kind() DescriptorKind { return KindTypeParameter }
```

**Generic Type Examples:**

**Example 1: Unconstrained parameter**

```go
type Response[T any] struct {
    Data  T      `json:"data"`
    Error string `json:"error,omitempty"`
}
```

The source provider emits:
```go
&StructDescriptor{
    Name: GoIdentifier{Name: "Response", Package: "example.com/api"},
    TypeParameters: []TypeParameterDescriptor{{
        ParamName:  "T",
        Constraint: nil, // unconstrained (any)
    }},
    Fields: []FieldDescriptor{
        {
            Name:     "Data",
            JSONName: "data",
            Type:     &TypeParameterDescriptor{ParamName: "T"},
        },
        {
            Name:     "Error",
            JSONName: "error",
            Optional: true,
            Type:     &PrimitiveDescriptor{PrimitiveKind: PrimitiveString},
        },
    },
}
```

Generators emit:
```typescript
export interface Response<T> {
    readonly data: T;
    readonly error?: string;
}
```

**Example 2: Constrained parameter with union**

```go
type Stringish interface {
    ~string | ~[]byte
}

type Container[T Stringish] struct {
    Value T `json:"value"`
}
```

The source provider emits the constraint interface as a type:

```go
// Schema.Types includes:
&AliasDescriptor{
    Name: GoIdentifier{Name: "Stringish", Package: "example.com/api"},
    Underlying: &UnionDescriptor{
        Types: []TypeDescriptor{
            &PrimitiveDescriptor{PrimitiveKind: PrimitiveString}, // ~string
            &PrimitiveDescriptor{PrimitiveKind: PrimitiveBytes},  // ~[]byte
        },
    },
}

&StructDescriptor{
    Name: GoIdentifier{Name: "Container", Package: "example.com/api"},
    TypeParameters: []TypeParameterDescriptor{{
        ParamName:  "T",
        Constraint: &ReferenceDescriptor{Target: GoIdentifier{Name: "Stringish", Package: "example.com/api"}},
    }},
    Fields: []FieldDescriptor{{
        Name:     "Value",
        JSONName: "value",
        Type:     &TypeParameterDescriptor{ParamName: "T"},
    }},
}
```

Generators emit:
```typescript
export type Stringish = string; // ~string and ~[]byte both map to string in JSON

export interface Container<T extends Stringish> {
    readonly value: T;
}
```

**Note on union constraints:** Go's type set semantics (the `~` prefix meaning "underlying type") don't have a direct TypeScript equivalent. Generators SHOULD emit the concrete types in the union. For `~string`, emit `string`; for `~[]byte`, emit `string` (since `[]byte` is base64 in JSON). When multiple Go types map to the same target type, generators SHOULD deduplicate (e.g., `~string | ~[]byte` becomes `string`, not `string | string`).

### 4.7 Supporting Types

```go
// GoIdentifier represents a named Go entity with package context.
// The Name field contains a sanitized identifier that is always a valid Go identifier.
// For generic instantiations, providers MUST use the synthetic naming algorithm (§3.4)
// to produce names like "Response_User" instead of "Response[User]".
type GoIdentifier struct {
    // Name is the sanitized identifier, always matching [A-Za-z_][A-Za-z0-9_]*.
    // For generic instantiations, synthetic names are generated per §3.4.
    // Examples: "User", "Response_User", "Response_pkg_User", "Map_string_int"
    //
    // Generators can use this name directly without additional sanitization for
    // most target languages. Generators are still responsible for handling
    // reserved words and case conversion as needed.
    Name string

    // Package is the fully qualified package path.
    // Empty for builtin types.
    Package string
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

// Source represents source code location information.
type Source struct {
    File   string
    Line   int
    Column int
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
```

### 4.8 Service Descriptors

Service descriptors represent API service definitions, capturing endpoint metadata alongside type information. This enables generators to produce typed API clients in addition to type definitions.

```go
// ServiceDescriptor represents a group of related endpoints.
type ServiceDescriptor struct {
    // Name is the service identifier (e.g., "Users", "Posts").
    Name string

    // Endpoints contains all endpoints in this service.
    Endpoints []EndpointDescriptor

    // Documentation for this service.
    Documentation Documentation
}

// EndpointDescriptor represents a single API endpoint.
type EndpointDescriptor struct {
    // Name is the endpoint identifier within the service (e.g., "Create", "List").
    Name string

    // FullName is the qualified name: "ServiceName.EndpointName" (e.g., "Users.Create").
    FullName string

    // Primitive is the tygor communication primitive: "query", "exec", or "stream".
    //   - "query": cacheable read (HTTP GET)
    //   - "exec": mutation (HTTP POST)
    //   - "stream": server-sent events (HTTP POST + SSE response)
    Primitive string

    // Path is the URL path: "/{ServiceName}/{EndpointName}".
    // Example: "/Users/Create", "/News/List"
    Path string

    // Request describes the request payload type.
    // Typically a ReferenceDescriptor pointing to a type in Schema.Types.
    // For query endpoints, fields become query parameters.
    // For exec/stream endpoints, this is the JSON request body.
    Request TypeDescriptor

    // Response describes the response payload type.
    // For stream endpoints, this is the type of each streamed event.
    // May be a ReferenceDescriptor, ArrayDescriptor, MapDescriptor, etc.
    Response TypeDescriptor

    // Documentation for this endpoint.
    Documentation Documentation
}
```

**Relationship to Schema.Types:**

`EndpointDescriptor.Request` and `EndpointDescriptor.Response` use `TypeDescriptor` to describe types. Named types use `ReferenceDescriptor` pointing to entries in `Schema.Types`:

```go
// Example: An endpoint returning a User type
endpoint := EndpointDescriptor{
    Name:      "Get",
    FullName:  "Users.Get",
    Primitive: "query",
    Path:      "/Users/Get",
    Request: &ReferenceDescriptor{
        Target: GoIdentifier{Name: "GetUserRequest", Package: "example.com/api"},
    },
    Response: &ReferenceDescriptor{
        Target: GoIdentifier{Name: "User", Package: "example.com/api"},
    },
}

// For array responses, wrap in ArrayDescriptor:
listEndpoint := EndpointDescriptor{
    Name:      "List",
    FullName:  "Users.List",
    Primitive: "query",
    Path:      "/Users/List",
    Request: &ReferenceDescriptor{
        Target: GoIdentifier{Name: "ListUsersRequest", Package: "example.com/api"},
    },
    Response: &ArrayDescriptor{
        Element: &ReferenceDescriptor{
            Target: GoIdentifier{Name: "User", Package: "example.com/api"},
        },
    },
}
```

**Schema Validation Rules:**

Implementations MUST validate schemas according to these rules:

1. **Type References**: All `ReferenceDescriptor` targets in endpoint `Request` and `Response` fields MUST resolve to a type in `Schema.Types`. Missing references are an error.

2. **Endpoint Names**: Within a single service, endpoint names MUST be unique. Across services, duplicate endpoint names are allowed (e.g., `Users.Create` and `Posts.Create` are both valid).

3. **FullName Format**: `EndpointDescriptor.FullName` MUST match the pattern `{ServiceName}.{EndpointName}` with exactly one dot.

4. **Path Format**: `EndpointDescriptor.Path` MUST match `/{ServiceName}/{EndpointName}` derived from the `FullName`.

**Protocol Integration:**

The Generator Spec defines types and endpoint metadata; the tygor Protocol Spec defines the wire format. Key relationships:

1. **Response Envelope**: The protocol wraps all responses in `{"result": T}` or `{"error": {...}}`. Endpoint `Response` types represent the inner `T`, not the envelope. Client libraries handle envelope wrapping/unwrapping.

2. **Error Types**: The protocol defines a standard error structure (`code`, `message`, `details`). Error handling is a runtime concern handled by the client library, not generated types. Generators MAY emit error type definitions separately if needed.

3. **Empty/Void Responses**: For endpoints that return nothing meaningful (Go type `tygor.Empty` / `*struct{}`), use the literal IR representation:

   ```go
   Response: &PtrDescriptor{
       Elem: &PrimitiveDescriptor{PrimitiveKind: PrimitiveEmpty},
   }
   ```

   This represents `*struct{}`, which serializes to `null` when the pointer is nil. The protocol wire format is `{"result": null}`. Generators SHOULD map this to:
   - TypeScript: `void` or `null`
   - Python: `None`
   - Go: `tygor.Empty` (`*struct{}`)

   **Semantics distinction:**
   | IR Representation | Go Type | Wire Format | Use Case |
   |-------------------|---------|-------------|----------|
   | `&PtrDescriptor{Elem: &PrimitiveDescriptor{PrimitiveKind: PrimitiveEmpty}}` | `*struct{}` | `null` | Void responses |
   | `&PrimitiveDescriptor{PrimitiveKind: PrimitiveEmpty}` | `struct{}` | `{}` | Empty object (rare) |
   | `Response: nil` | (unspecified) | — | Invalid; providers MUST specify a response type |

4. **Empty/Void Requests**: For endpoints with no request parameters (e.g., a GET with no query params), use `Request: nil`. Generators SHOULD map this to an empty object type or void parameter:
   - TypeScript: `{}` or omit the parameter
   - Python: `None` or no parameter

**HTTP Method Conventions:**

| Handler Type | HTTP Method | Request Encoding | Use Case |
|--------------|-------------|------------------|----------|
| Query | GET | URL query params | Read operations, idempotent |
| Exec | POST | JSON body | Write operations, mutations |

**Query Parameter Encoding (GET Requests):**

For GET endpoints, the request type's fields are encoded as URL query parameters per the tygor Protocol Specification (§3.2.1). Generators MUST understand these encoding rules to produce correct client code:

| Field Type | Encoding | Example |
|------------|----------|---------|
| Primitives (`string`, `int`, `bool`, etc.) | Direct value | `?name=alice&age=30&active=true` |
| Arrays (`[]T`) | Repeated parameter | `?ids=1&ids=2&ids=3` |
| Nested structs | Bracket notation | `?user[name]=alice&user[age]=30` |
| Optional fields (`omitempty`) | Omit if zero/nil | `?name=alice` (age omitted) |
| Pointers (`*T`) | Omit if nil, value if non-nil | `?limit=10` or omitted |

**Type Restrictions for GET Request Types:**

Providers SHOULD emit a warning when a GET endpoint's request type contains:
- `map[K]V` fields (no standard URL encoding; consider POST instead)
- Deeply nested structs (more than 2 levels; leads to unwieldy URLs)
- `[]byte` or `PrimitiveBytes` fields (binary data not suitable for URLs)
- `any`/`interface{}` fields (type unknown at generation time)

Generators MAY reject or warn on these types. When a complex type must be supported, generators SHOULD serialize it as a JSON string in a single query parameter (e.g., `?filter={"key":"value"}`), but this is NOT RECOMMENDED for interoperability.

**Path Construction:**

Paths are constructed from the service and endpoint names using the format `/{ServiceName}/{EndpointName}`. Per the tygor protocol specification (Appendix A: ABNF Grammar):

```abnf
service-name = 1*ALPHA *(ALPHA / DIGIT / "_")
method-name  = 1*ALPHA *(ALPHA / DIGIT / "_")
```

This means:
- Service and endpoint names contain only letters, digits, and underscores
- Names MUST start with a letter
- Dots are NOT allowed in names (so `FullName` is always `{Service}.{Endpoint}`)
- All request parameters for GET operations are passed via URL query string
- All request parameters for POST operations are passed via JSON request body
- Path parameters (e.g., `/users/{id}`) are NOT used

### 4.9 Nullable and Optional Field Mapping

Go's `encoding/json` accepts and produces JSON based on field type and struct tags. TypeScript types MUST represent all valid JSON values for bidirectional contracts (both request bodies and response parsing). The default mapping follows directly from Go semantics. Generators MAY provide configuration options to override this behavior for compatibility with existing codebases.

**Decision Tree:**

For each field, determine two independent boolean properties:

1. **Optional** (`?:`): True if `FieldDescriptor.Optional` is true (i.e., `omitempty` or `omitzero` tag is present)
2. **Nullable** (`| null`): True if the field's IR type is `*PtrDescriptor`, `*ArrayDescriptor`, or `*MapDescriptor`

Then emit the field according to this table:

| Optional | Nullable | TypeScript Syntax |
|----------|----------|-------------------|
| false | false | `field: T` |
| true | false | `field?: T` |
| false | true | `field: T \| null` |
| true | true | `field?: T \| null` |

**Rationale:** While Go's *output* with `omitempty` will never produce `null` (nil values are omitted), Go's *input* accepts `null` for any pointer/slice/map field. Since generated types serve bidirectional contracts (request bodies and responses), types must represent all valid inputs, not just possible outputs.

**Complete Mapping Table:**

| Go Field | Optional | Nullable | TypeScript |
|----------|----------|----------|------------|
| `Field string` | false | false | `field: string` |
| `Field *string` | false | true | `field: string \| null` |
| `Field string ,omitempty` | true | false | `field?: string` |
| `Field *string ,omitempty` | true | true | `field?: string \| null` |
| `Field []T` | false | true | `field: T[] \| null` |
| `Field []T ,omitempty` | true | true | `field?: T[] \| null` |
| `Field *[]T` | false | true | `field: T[] \| null` |
| `Field *[]T ,omitempty` | true | true | `field?: T[] \| null` |
| `Field map[K]V` | false | true | `field: Record<K,V> \| null` |
| `Field map[K]V ,omitempty` | true | true | `field?: Record<K,V> \| null` |
| `Field *map[K]V` | false | true | `field: Record<K,V> \| null` |
| `Field *map[K]V ,omitempty` | true | true | `field?: Record<K,V> \| null` |
| `Field Struct` | false | false | `field: Struct` |
| `Field Struct ,omitempty` | true | false | `field?: Struct` |
| `Field *Struct` | false | true | `field: Struct \| null` |
| `Field *Struct ,omitempty` | true | true | `field?: Struct \| null` |

**Key Insight:** Any Go type that can hold `nil` (pointers, slices, maps) produces `| null` in TypeScript. The `omitempty`/`omitzero` tag independently controls whether the field can be absent (`?:`). These are orthogonal concerns:
- `| null` = "Go accepts/produces `null` for this field"
- `?:` = "Go accepts/produces absence for this field"

**Nested Pointers:** Go allows nested pointers (`**T`, `***T`, etc.), but `encoding/json` marshals all nil pointer indirections identically—they all serialize to `null`. A non-nil outer pointer to a nil inner pointer also serializes to `null`. Generators MUST flatten nested pointers to a single `| null` in the output type (i.e., `**string` becomes `string | null`, not `string | null | null`).

**Note on `omitempty` vs `omitzero` runtime behavior:**

While both tags set `Optional=true` in the IR, they have different runtime semantics:

| Field Type | `omitempty` | `omitzero` |
|------------|-------------|------------|
| `[]T` nil | Omitted | Omitted |
| `[]T` empty (`[]T{}`) | Omitted | **Present** (`[]`) |
| `map[K]V` nil | Omitted | Omitted |
| `map[K]V` empty | Omitted | **Present** (`{}`) |
| `Struct` zero | **Present** | Omitted |

These differences are runtime concerns that don't affect type generation. Both result in `Optional=true` because in both cases the field can be absent from JSON output.

## 5. Generator Interface

### 5.1 Core Interface

Generators MUST implement the following interface:

```go
// Generator transforms IR type descriptors into target language source code.
type Generator interface {
    // Name returns the generator's identifier (e.g., "typescript", "python").
    Name() string

    // Generate produces source code for the given schema.
    Generate(ctx context.Context, schema *Schema, opts GenerateOptions) (*GenerateResult, error)
}

// Schema represents a complete set of types and services to generate.
type Schema struct {
    // Package is the source Go package information.
    Package PackageInfo

    // Types contains top-level named type descriptors to generate.
    // Only Struct, Alias, and Enum descriptors appear here.
    // Expression types (Primitive, Array, Map, etc.) appear nested
    // within these named types' fields and type expressions.
    //
    // Ordering: Providers emit types in topological order (dependencies before
    // dependents) as a convenience. However, generators MUST NOT rely on this
    // ordering for correctness—they MUST handle types in any order, including
    // circular references. See §7.1 for declaration order requirements.
    Types []TypeDescriptor

    // Services contains service descriptors with their endpoints.
    // This field is OPTIONAL - schemas containing only types (no services)
    // are valid. Generators that only emit type definitions MAY ignore this.
    // When present, endpoint Request/Response fields reference types in Types.
    Services []ServiceDescriptor
}

// GenerateOptions configures generation behavior.
type GenerateOptions struct {
    // Sink receives generated output files.
    Sink OutputSink

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
    Warnings []Warning
}

// OutputFile describes a generated file.
type OutputFile struct {
    // Path is the relative path of the generated file.
    Path string

    // Size is the number of bytes written.
    Size int64
}
```

### 5.2 Output Sink Interface

Output sinks receive generated content. The sink interface is minimal to support filesystem writes and in-memory testing.

```go
// OutputSink receives generated file content.
type OutputSink interface {
    // WriteFile writes content to the specified path.
    // The path is relative; the sink determines the actual location.
    // Implementations MUST be safe for concurrent calls.
    WriteFile(ctx context.Context, path string, content []byte) error
}
```

**Design Rationale:**

The `[]byte` content parameter (rather than `io.Reader`) is intentional:
- Generated files are typically small (KB-MB range)
- Allows atomic writes and easy content inspection
- Simplifies implementation for most sinks
- Generators can still stream internally and buffer only at write time

### 5.3 Standard Sink Implementations

#### 5.3.1 Filesystem Sink

Writes files to a directory on disk.

```go
// FilesystemSink writes to a directory on the local filesystem.
type FilesystemSink struct {
    // Root is the base directory for all writes.
    Root string

    // Mode is the file permission mode (default: 0644).
    Mode os.FileMode

    // Overwrite controls behavior for existing files.
    // If false, returns an error when a file exists.
    Overwrite bool
}

func NewFilesystemSink(root string) *FilesystemSink
func (s *FilesystemSink) WriteFile(ctx context.Context, path string, content []byte) error
```

**Behavior Requirements:**

1. The sink MUST create parent directories as needed.
2. The sink MUST reject paths that escape the root via `..` traversal.
3. The sink SHOULD perform atomic writes (write to temp file, then rename). On platforms where atomic rename is not supported, the sink MAY write directly.

#### 5.3.2 Memory Sink

Stores files in memory for testing or further processing.

```go
// MemorySink stores generated files in memory.
type MemorySink struct {
    mu    sync.RWMutex
    files map[string][]byte
}

func NewMemorySink() *MemorySink
func (s *MemorySink) WriteFile(ctx context.Context, path string, content []byte) error

// Files returns a copy of all written files.
func (s *MemorySink) Files() map[string][]byte

// Get returns the content of a single file, or nil if not found.
func (s *MemorySink) Get(path string) []byte

// Reset clears all stored files.
func (s *MemorySink) Reset()
```

**Use Cases:**
- Unit testing generators
- Diffing generated output against expected fixtures
- Building file bundles before writing

### 5.4 Output Path Conventions

Generators MUST follow these path conventions:

1. **Relative Paths**: All paths MUST be relative (no leading `/`).
2. **Forward Slashes**: Use `/` as separator (sinks convert for OS).
3. **No Traversal**: Paths MUST NOT contain `..` components.
4. **Clean Paths**: Paths SHOULD be cleaned (no `./`, duplicate `/`).

```go
// ValidatePath checks if a path is valid for output.
func ValidatePath(path string) error {
    if filepath.IsAbs(path) {
        return errors.New("absolute paths not allowed")
    }
    if strings.Contains(path, "..") {
        return errors.New("path traversal not allowed")
    }
    return nil
}
```

### 5.5 File Layout Strategies

File layout is a **generator-specific concern**. Different target languages have different conventions and requirements:

- TypeScript: Single file is common; per-type may require import management
- Python: Per-type (one class per file) is idiomatic
- JSON Schema: Single file or per-type both work; per-type allows `$ref` across files

Generators SHOULD define their own layout options appropriate to their target language. This specification does not prescribe a particular layout strategy.

**Example Layout Patterns:**

| Pattern | Description | Output Example |
|---------|-------------|----------------|
| Single file | All types in one file | `types.ts` |
| Per type | One file per top-level type | `user.ts`, `post.ts` |
| Per package | One file per source Go package | `api.ts`, `models.ts` |

### 5.6 Type Emitter Interface

Generators MAY decompose generation into per-type emitters:

```go
// TypeEmitter generates output for a single type descriptor.
type TypeEmitter interface {
    // CanEmit returns true if this emitter handles the given descriptor.
    CanEmit(desc TypeDescriptor) bool

    // Emit writes the type definition to the writer.
    Emit(ctx *EmitContext, desc TypeDescriptor) error
}

// EmitContext provides utilities for emission.
type EmitContext struct {
    // Writer is the output destination for the current file.
    Writer io.Writer

    // Schema provides access to the full type schema.
    Schema *Schema

    // Config provides generator configuration.
    Config GeneratorConfig

    // CurrentFile is the path of the file being written.
    CurrentFile string

    // ResolveRef returns the output name for a type reference.
    // Handles cross-file imports when using multi-file layouts.
    ResolveRef(ref GoIdentifier) (string, error)

    // FormatDoc formats documentation for the target language.
    FormatDoc(doc Documentation) string
}
```

## 6. Configuration

### 6.1 Generator Configuration

```go
// GeneratorConfig provides common configuration options.
type GeneratorConfig struct {
    // Naming
    TypePrefix         string // Prepended to all generated type names
    TypeSuffix         string // Appended to all generated type names
    FieldCase          string // "preserve", "camel", "pascal", "snake", "kebab"
    TypeCase           string // "preserve", "camel", "pascal", "snake", "kebab"
    PropertyNameSource string // "field" or "tag:json", "tag:xml", etc.

    // Formatting
    IndentStyle     string // "space" or "tab"
    IndentSize      int    // Spaces per indent level (when IndentStyle is "space")
    LineEnding      string // "lf" or "crlf"
    TrailingNewline bool   // Ensure files end with a newline

    // Features
    EmitComments bool // Include documentation comments in output

    // Custom contains generator-specific options (e.g., TypeScriptConfig).
    Custom map[string]any
}
```

### 6.2 TypeScript-Specific Configuration

TypeScript generators SHOULD support:

```go
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

    // UnknownType specifies the type for Go's 'any' or 'interface{}'.
    // SHOULD be one of: "unknown", "any"
    UnknownType string
}
```

## 7. Output Requirements

### 7.1 Declaration Order

**Provider responsibility:** Providers emit `Schema.Types` in topological order (dependencies before dependents) as a convenience. For circular references, providers break cycles arbitrarily.

**Generator responsibility:** Generators MUST NOT rely on input ordering for correctness. Generators MUST handle types in any order, including circular references.

Specific requirements:

1. **Forward declarations**: Generators targeting languages that require forward declarations MUST emit types after their dependencies. For languages without such requirements, generators SHOULD emit types in alphabetical order for determinism.
2. **Circular references**: Circular type references (e.g., `TreeNode` containing `[]TreeNode`) are valid and MUST be supported. Generators MAY use forward declarations, interfaces, or other target-language mechanisms to handle them. Note: While recursive *type definitions* are valid, `encoding/json` will return an error if cyclic *data* is encountered at runtime (e.g., a node whose Children array contains itself). This is a runtime concern outside the scope of type generation.
3. **Determinism**: When multiple valid orderings exist, alphabetical ordering by name SHOULD be used for determinism.

### 7.2 Identifier Escaping

Generators MUST handle identifiers that are:

1. **Reserved Words**: Identifiers matching target language reserved words MUST be escaped or renamed.
2. **Invalid Characters**: Identifiers containing characters invalid in the target language MUST be transformed.
3. **Numeric Prefixes**: Identifiers starting with numbers MUST be prefixed or quoted.

```go
// IdentifierEscaper handles identifier transformation.
type IdentifierEscaper interface {
    // Escape transforms an identifier for safe use in the target language.
    Escape(name string) string

    // NeedsQuoting returns true if the identifier requires quoting.
    NeedsQuoting(name string) bool

    // Quote returns a quoted form of the identifier.
    Quote(name string) string
}
```

### 7.3 Documentation Formatting

Generators MUST transform documentation to target language conventions. The following transformations are RECOMMENDED for common target languages:

| Source | TypeScript | Python | Go |
|--------|------------|--------|-----|
| Single line | `/** text */` | `"""text"""` | `// text` |
| Multi-line | `/**\n * line\n */` | `"""\nline\n"""` | `// line\n// line` |
| Deprecation | `/** @deprecated msg */` | `@deprecated` decorator | `// Deprecated: msg` |

### 7.4 Literal Escaping

Generators MUST properly escape string literals:

1. Quote characters MUST be escaped.
2. Control characters MUST be escaped.
3. Unicode characters SHOULD be preserved. For target languages that do not support Unicode literals, generators MUST escape them using the target language's escape syntax.

### 7.5 Error Handling

Generators MUST distinguish between fatal errors and warnings:

1. **Fatal Errors**: Generators MUST return an error for unrecoverable conditions:
   - Missing required type references (a `ReferenceDescriptor` target not found in `Schema.Types`)
   - Invalid configuration (malformed options, conflicting settings)
   - I/O failures when writing to the output sink

2. **Warnings**: Generators SHOULD emit warnings (in `GenerateResult.Warnings`) for recoverable conditions:
   - Unsupported features in the target language (e.g., generics in a language without them)
   - Deprecated types or fields
   - Type mappings that may lose precision (e.g., `int64` to JavaScript `number`)
   - Unknown validation constraints that are passed through unchanged

3. **Partial Output**: When a fatal error occurs, generators SHOULD NOT leave partial output in the sink. Generators MAY buffer all output and write only on success, or MAY clean up on failure.

## 8. Extension Points

### 8.1 Custom Type Mappings

Generators SHOULD support custom type mappings:

```go
// TypeMapping overrides default type generation.
type TypeMapping struct {
    // Source identifies the Go type to map.
    Source TypeMatcher

    // Target specifies the replacement in the target language.
    Target string

    // Import specifies any required import statement.
    Import string
}

// TypeMatcher identifies types for mapping.
type TypeMatcher struct {
    // Package is the fully qualified Go package path.
    Package string

    // Name is the type name within the package.
    Name string
}
```

## Appendix A: Primitive Type Mappings

### A.1 Go Type to IR Mapping

| Go Type | IR Representation |
|---------|-------------------|
| `bool` | `{PrimitiveKind: PrimitiveBool}` |
| `int` | `{PrimitiveKind: PrimitiveInt, BitSize: 0}` |
| `int8` | `{PrimitiveKind: PrimitiveInt, BitSize: 8}` |
| `int16` | `{PrimitiveKind: PrimitiveInt, BitSize: 16}` |
| `int32` | `{PrimitiveKind: PrimitiveInt, BitSize: 32}` |
| `int64` | `{PrimitiveKind: PrimitiveInt, BitSize: 64}` |
| `uint` | `{PrimitiveKind: PrimitiveUint, BitSize: 0}` |
| `uint8` | `{PrimitiveKind: PrimitiveUint, BitSize: 8}` |
| `uint16` | `{PrimitiveKind: PrimitiveUint, BitSize: 16}` |
| `uint32` | `{PrimitiveKind: PrimitiveUint, BitSize: 32}` |
| `uint64` | `{PrimitiveKind: PrimitiveUint, BitSize: 64}` |
| `uintptr` | `{PrimitiveKind: PrimitiveUint, BitSize: 0}` |
| `float32` | `{PrimitiveKind: PrimitiveFloat, BitSize: 32}` |
| `float64` | `{PrimitiveKind: PrimitiveFloat, BitSize: 64}` |
| `string` | `{PrimitiveKind: PrimitiveString}` |
| `[]byte` | `{PrimitiveKind: PrimitiveBytes}` |
| `time.Time` | `{PrimitiveKind: PrimitiveTime}` |
| `time.Duration` | `{PrimitiveKind: PrimitiveDuration}` |
| `any` / `interface{}` | `{PrimitiveKind: PrimitiveAny}` |
| `struct{}` | `{PrimitiveKind: PrimitiveEmpty}` |

### A.2 IR to Target Language Mapping

| IR Kind | BitSize | TypeScript | Zod | JSON Schema | Rust |
|---------|---------|------------|-----|-------------|------|
| `PrimitiveBool` | — | `boolean` | `z.boolean()` | `boolean` | `bool` |
| `PrimitiveInt` | 0 | `number` | `z.number().int()` | `integer` | `i64` † |
| `PrimitiveInt` | 8 | `number` | `z.number().int().min(-128).max(127)` | `integer` | `i8` |
| `PrimitiveInt` | 16 | `number` | `z.number().int().min(-32768).max(32767)` | `integer` | `i16` |
| `PrimitiveInt` | 32 | `number` | `z.number().int()` | `integer` | `i32` |
| `PrimitiveInt` | 64 | `number` ⚠️ | `z.number().int()` ⚠️ | `integer` | `i64` |
| `PrimitiveUint` | 0 | `number` | `z.number().int().nonnegative()` | `integer` | `u64` † |
| `PrimitiveUint` | 8 | `number` | `z.number().int().nonnegative().max(255)` | `integer` | `u8` |
| `PrimitiveUint` | 16 | `number` | `z.number().int().nonnegative().max(65535)` | `integer` | `u16` |
| `PrimitiveUint` | 32 | `number` | `z.number().int().nonnegative()` | `integer` | `u32` |
| `PrimitiveUint` | 64 | `number` ⚠️ | `z.number().int().nonnegative()` ⚠️ | `integer` | `u64` |
| `PrimitiveFloat` | 32 | `number` | `z.number()` | `number` | `f32` |
| `PrimitiveFloat` | 64 | `number` | `z.number()` | `number` | `f64` |
| `PrimitiveString` | — | `string` | `z.string()` | `string` | `String` |
| `PrimitiveBytes` | — | `string` | `z.string()` | `string` (format: byte) | `Vec<u8>` ‡ |
| `PrimitiveTime` | — | `string` | `z.string().datetime()` | `string` (format: date-time) | `DateTime<Utc>` |
| `PrimitiveDuration` | — | `number` | `z.number().int()` | `integer` | `i64` § |
| `PrimitiveAny` | — | `unknown` | `z.unknown()` | `{}` | `serde_json::Value` |
| `PrimitiveEmpty` | — | `Record<string, never>` | `z.object({})` | `object` | `()` |

### A.3 Composite Type Mappings

| Go Type | TypeScript | Zod | JSON Schema |
|---------|------------|-----|-------------|
| `json.Number` | `string` | `z.string()` | `string` (see note) |
| `json.RawMessage` | `unknown` | `z.unknown()` | `{}` |
| `map[string]T` | `Record<string, T>` | `z.record(z.string(), T)` | `object` |
| `[]T` | `T[]` (or `T[] \| null`, see §4.9) | `z.array(T)` | `array` |
| `[N]T` | `T[]` (or tuple) | `z.array(T).length(N)` | `array` |
| `[N]byte` | `number[]` | `z.array(z.number()).length(N)` | `array` (NOT base64) |
| `*T` | `T \| null` (or `T` with `?`, see §4.9) | `T.nullable()` | nullable |

**Notes:**

**Rust-specific notes:**
- † Go's `int`/`uint` (BitSize: 0) are platform-dependent (32 or 64 bits on the server). For JSON APIs, Rust generators SHOULD use `i64`/`u64` as the conservative upper bound, since the wire format is determined by the server architecture, not the client. Using `isize`/`usize` would incorrectly tie interpretation to the client's platform.
- ‡ Go's `[]byte` is base64-encoded by `encoding/json`. Rust generators MUST apply serde's base64 encoding, e.g., `#[serde(with = "base64")]` using the `base64` crate's serde support, or a wrapper type. Without this attribute, serde would expect a JSON array of numbers.
- § Go's `time.Duration` serializes as an `int64` representing nanoseconds and can be negative. Rust generators SHOULD use `i64` (possibly as a newtype for type safety). Neither `std::time::Duration` (unsigned only) nor `chrono::Duration` (different representation) directly match Go's semantics.

**General notes:**
- `time.Duration` serializes as an integer representing nanoseconds. Values up to ~104 days fit within JavaScript's `Number.MAX_SAFE_INTEGER` (2^53-1). Beyond that, sub-microsecond precision is lost, which is rarely a concern at such timescales. Generators MAY emit a branded type (e.g., `type Duration = number & { __brand: "Duration" }`) for additional type safety.
- `json.Number` is a Go type (`type Number string`) that serializes as a raw JSON number, not a quoted string. It's used to preserve numeric precision. At the type level, treat it as a string; at runtime, the JSON wire format is a number. Empty `json.Number` values become `0`.
- `uintptr` is encoded identically to other unsigned integers by `encoding/json`.
- `json.RawMessage` is a `[]byte` type that embeds raw JSON content directly without encoding. It is treated as `PrimitiveAny` in the IR.
- `struct{}` (empty struct) serializes as `{}` (empty JSON object). With `omitzero`, empty struct fields are omitted; with `omitempty`, they are NOT omitted (structs are never considered "empty" for `omitempty` purposes).
- **Void responses (`*struct{}`)**: For endpoints returning `tygor.Empty` (`*struct{}`), use `&PtrDescriptor{Elem: &PrimitiveDescriptor{PrimitiveKind: PrimitiveEmpty}}`. This serializes to `null` on the wire. Do not confuse with bare `PrimitiveEmpty` which represents `struct{}` (serializes to `{}`). See §4.8 Protocol Integration.

**Float Special Values:** `encoding/json` returns `UnsupportedValueError` when marshaling `NaN`, `+Inf`, or `-Inf` float values. These values have no JSON representation. Providers encountering these values at analysis time (e.g., in const declarations) SHOULD emit a warning. This is primarily a runtime concern rather than a type generation concern.

**Large Integer Warning:** Go's `int64` and `uint64` can represent values larger than JavaScript's `Number.MAX_SAFE_INTEGER` (2^53-1 = 9,007,199,254,740,991). Values exceeding this limit will lose precision when parsed by JavaScript. TypeScript generators SHOULD emit a warning when encountering `int64` or `uint64` fields without the `,string` struct tag. For APIs requiring large integers, consider using the `,string` struct tag to encode as JSON strings, or use a string-based ID type.

**String Escaping and UTF-8 Handling:**

`encoding/json` applies the following transformations when marshaling strings:

| Input | Output | Notes |
|-------|--------|-------|
| Invalid UTF-8 bytes | `\ufffd` | Replaced with Unicode replacement character |
| `"` | `\"` | Escaped |
| `\` | `\\` | Escaped |
| Control chars (< 0x20) | `\n`, `\r`, `\t`, `\b`, `\f`, or `\uXXXX` | Escaped |
| `<`, `>`, `&` | `\u003c`, `\u003e`, `\u0026` | HTML-escaped by default |
| U+2028 (Line Separator) | `\u2028` | Escaped for JavaScript safety |
| U+2029 (Paragraph Separator) | `\u2029` | Escaped for JavaScript safety |
| Valid UTF-8 (including emoji) | Preserved | No transformation |

The HTML escaping (`<`, `>`, `&`) can be disabled at runtime via `json.Encoder.SetEscapeHTML(false)`, but `json.Marshal` always escapes these characters. This is transparent to type generation.

**Map Key Restrictions:**

`encoding/json` only supports certain types as map keys:

| Key Type | Supported | Serialization |
|----------|-----------|---------------|
| `string` | ✓ | Used directly |
| `int`, `int8`..`int64` | ✓ | Decimal string (e.g., `"42"`) |
| `uint`, `uint8`..`uint64` | ✓ | Decimal string (e.g., `"123"`) |
| Types with `encoding.TextMarshaler` | ✓ | Result of `MarshalText()` |
| `bool` | ✗ | Error: `unsupported type: map[bool]T` |
| `float32`, `float64` | ✗ | Error: `unsupported type: map[float64]T` |
| `complex64`, `complex128` | ✗ | Error |
| Structs (without TextMarshaler) | ✗ | Error |

Providers MUST return an error when encountering map types with unsupported key types.

## Appendix B: Reserved Words

Generators MUST handle reserved words for their target language. TypeScript reserved words include:

```
break, case, catch, class, const, continue, debugger, default, delete,
do, else, enum, export, extends, false, finally, for, function, if,
implements, import, in, instanceof, interface, let, new, null, package,
private, protected, public, return, static, super, switch, this, throw,
true, try, typeof, var, void, while, with, yield
```

## Appendix C: Future Extensions

This appendix documents planned extensions that are explicitly out of scope for the current specification version but are being considered for future releases.

### C.1 Declared Error Types

**Status**: Planned for future version (optional feature)

**Current State**: The tygor protocol defines a standard error envelope (`{"error": {"code": "...", "message": "...", "details": {...}}}`). Error handling is currently a runtime concern handled by client libraries. Generated types do not include error type information.

**Future Extension**: A future version MAY add optional error type declarations to `EndpointDescriptor`:

```go
type EndpointDescriptor struct {
    // ... existing fields ...

    // Errors lists the error types this endpoint may return.
    // If nil or empty, the endpoint uses the generic protocol error shape.
    // When specified, enables discriminated union error types in generated clients.
    Errors []TypeDescriptor
}
```

This would enable:
- **Typed error handling** in generated clients (discriminated unions instead of generic catch blocks)
- **Runtime enforcement** preventing undeclared errors from leaking to clients
- **Richer API documentation** with explicit error contracts

Services without declared errors would continue using the generic protocol error shape, maintaining backwards compatibility.

**Design Note**: Error types cannot be reliably derived via static analysis. While a source provider could detect error types constructed in handler code, this approach is incomplete (can't see through interfaces, wrapped errors, or external calls) and would over-report internal errors. Declared errors require explicit metadata from the API author.

### C.2 Streaming Endpoints

**Status**: Implemented

The `Primitive` field in `EndpointDescriptor` supports streaming via Server-Sent Events:

- `"query"`: Cacheable read (HTTP GET, single response)
- `"exec"`: Mutation (HTTP POST, single response)
- `"stream"`: Server-push events (HTTP POST request, SSE response stream)

For stream endpoints, the `Response` type describes each individual event in the stream, not the stream itself. The client runtime handles the SSE protocol and exposes streams as `AsyncIterable<T>`.

**Future Extension**: Additional primitives may be added:

- `"atom"`: Synchronized values (last-write-wins state sync)
- `"channel"`: Bidirectional communication (WebSocket)

These would follow the same pattern: `Primitive` indicates the semantic communication pattern, and the client/server derive the appropriate transport protocol.

## Appendix D: Design Rationale

This section captures key design decisions that shaped the specification.

### Go-First IR

The IR models Go's type system directly rather than being a universal abstraction. This means:
- No tuples, intersection types, or literal types (Go doesn't have them)
- `PtrDescriptor` represents Go pointer semantics; generators derive nullability from context
- `UnionDescriptor` exists only for type parameter constraints, not general unions
- Enum detection requires source analysis (reflection can't enumerate const values)

**Tradeoff**: Less abstract, but simpler to implement correctly and impossible to generate invalid Go mappings.

### Deterministic Nullable/Optional Mapping

Field optionality (`field?:`) and nullability (`| null`) follow directly from Go semantics—not configurable. The mapping table in §4.9 is derived from `encoding/json` behavior:
- `omitempty`/`omitzero` → optional field
- Pointer/slice/map without omit tag → nullable (can be `null`, always present)
- Both → rare `field?: T | null` case

**Tradeoff**: Less flexibility, but generated types always match actual JSON output.

### BitSize on Primitives

Numeric types carry a `BitSize` field (0, 8, 16, 32, 64) rather than separate `PrimitiveInt8`, `PrimitiveInt16`, etc. kinds. Generators targeting TypeScript can ignore it (everything is `number`); generators targeting Rust or Zod use it for precise types/validation.

**Tradeoff**: Slightly more complex primitive handling, but avoids combinatorial explosion of primitive kinds.

### Synthetic Names for Generics

`GoIdentifier.Name` is always a valid Go identifier (`[A-Za-z_][A-Za-z0-9_]*`). For generic instantiations like `Response[User]`, providers apply the synthetic naming algorithm (§3.4) to produce `Response_User`. This ensures generators don't need special-case handling for brackets or dots.

**Tradeoff**: Names are less readable than `Response<User>`, but universally safe across target languages.

### Source Provider as Primary

The reflection provider exists to validate that the IR isn't over-coupled to `go/types`, but production use should prefer the source provider. Only source analysis can extract documentation, source locations, enum values, and preserved generic type parameters.

### No Transform Pipeline

Earlier drafts included pre/post hooks and transform pipelines. These were removed—users can transform the `Schema` before passing it to generators using regular Go code. The spec doesn't need to define middleware patterns.
