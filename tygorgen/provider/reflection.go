// Package provider implements input providers for extracting type information
// from Go code. Providers convert Go types into the intermediate representation (IR)
// that generators use to produce target language code.
package provider

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"tygor.dev/tygorgen/ir"
)

// marshalerInterfaces holds reflect.Type values for interface checks.
var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// ReflectionProvider extracts types using runtime reflection.
// This secondary provider exists primarily to validate that the IR abstraction
// is not overly coupled to go/types internals. Production use cases SHOULD
// prefer the Source Provider for its richer feature set.
type ReflectionProvider struct{}

// ReflectionInputOptions configures reflection-based type extraction.
type ReflectionInputOptions struct {
	// RootTypes are the types to extract, specified as reflect.Type values.
	RootTypes []reflect.Type
}

// BuildSchema extracts types and returns a Schema.
func (p *ReflectionProvider) BuildSchema(ctx context.Context, opts ReflectionInputOptions) (*ir.Schema, error) {
	if len(opts.RootTypes) == 0 {
		return nil, fmt.Errorf("no root types provided")
	}

	b := &reflectionSchemaBuilder{
		schema:           &ir.Schema{},
		visited:          make(map[reflect.Type]bool),
		processing:       make(map[reflect.Type]bool),
		expandingGeneric: make(map[string]bool),
		anonStructs:      make(map[reflect.Type]string),
		typeNames:        make(map[string]bool),
	}

	// Process all root types
	for _, t := range opts.RootTypes {
		if err := b.extractType(ctx, t); err != nil {
			return nil, err
		}
	}

	return b.schema, nil
}

// reflectionSchemaBuilder maintains state during schema construction.
type reflectionSchemaBuilder struct {
	schema           *ir.Schema
	visited          map[reflect.Type]bool   // Types already processed
	processing       map[reflect.Type]bool   // Types currently being processed (cycle detection)
	expandingGeneric map[string]bool         // Base generic names currently being expanded (§3.4 cycle detection)
	anonStructs      map[reflect.Type]string // Anonymous struct -> synthetic name
	typeNames        map[string]bool         // Track used type names for collision detection
	anonCounter      int                     // Counter for disambiguating anonymous struct names (Issue 2)
}

// extractType processes a type and adds it to the schema.
func (b *reflectionSchemaBuilder) extractType(ctx context.Context, t reflect.Type) error {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Issue 1 Fix: Check if the pointer type itself is named BEFORE dereferencing.
	// If it's a named pointer type (e.g., type PtrAlias *string), extract it as an alias.
	if t.Kind() == reflect.Ptr && t.Name() != "" && t.PkgPath() != "" {
		// This is a named pointer type - extract as alias
		if b.visited[t] {
			return nil
		}
		if b.processing[t] {
			b.addWarning("CYCLE_DETECTED", fmt.Sprintf("Recursive type detected: %s", t.String()), "")
			return nil
		}
		b.processing[t] = true
		defer delete(b.processing, t)

		err := b.extractAlias(ctx, t)
		if err == nil {
			b.visited[t] = true
		}
		return err
	}

	// Dereference pointers to get to the underlying type
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Skip if already visited
	if b.visited[t] {
		return nil
	}

	// Check for cycles
	if b.processing[t] {
		// Cycle detected, skip to avoid infinite recursion
		b.addWarning("CYCLE_DETECTED", fmt.Sprintf("Recursive type detected: %s", t.String()), "")
		return nil
	}

	b.processing[t] = true
	defer delete(b.processing, t)

	// Handle based on kind
	var err error
	switch t.Kind() {
	case reflect.Struct:
		// §3.3.6: Check for custom marshalers on named struct types.
		// These are emitted as AliasDescriptor with Underlying = PrimitiveAny.
		// Exception: well-known types (time.Time, etc.) are handled in checkSpecialType.
		if t.Name() != "" && t.PkgPath() != "" && b.checkSpecialType(t) == nil && b.hasCustomMarshaler(t) {
			err = b.extractCustomMarshalerAlias(t)
		} else {
			err = b.extractStruct(ctx, t)
		}
	case reflect.Slice, reflect.Array:
		// For named slice/array types (e.g., type MySlice []int), extract as alias
		if t.Name() != "" && t.PkgPath() != "" {
			err = b.extractAlias(ctx, t)
		}
		// Also recursively extract element type (needed for anonymous slice/array root types)
		if err == nil {
			err = b.extractType(ctx, t.Elem())
		}
	case reflect.Map:
		// For named map types (e.g., type StringMap map[string]string), extract as alias
		if t.Name() != "" && t.PkgPath() != "" {
			err = b.extractAlias(ctx, t)
		}
		// Recursively extract key and value types
		if err == nil {
			if keyErr := b.extractType(ctx, t.Key()); keyErr != nil {
				err = keyErr
			} else {
				err = b.extractType(ctx, t.Elem())
			}
		}
	default:
		// For named types (including type aliases), create an alias descriptor
		if t.Name() != "" && t.PkgPath() != "" {
			err = b.extractAlias(ctx, t)
		}
		// Anonymous non-struct types are handled inline
	}

	// Only mark as visited if extraction succeeded
	if err == nil {
		b.visited[t] = true
	}
	return err
}

// extractStruct extracts a struct type.
func (b *reflectionSchemaBuilder) extractStruct(ctx context.Context, t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", t.Kind())
	}

	// Generate name for this struct
	name := b.getTypeName(t)
	pkg := t.PkgPath()

	// §3.4 Generic expansion cycle detection: track base generic names to prevent
	// infinite recursion on types like Container[Container[Container[T]]]
	rawName := t.Name()
	if idx := strings.Index(rawName, "["); idx >= 0 {
		baseGeneric := pkg + "." + rawName[:idx]
		if b.expandingGeneric[baseGeneric] {
			// Already expanding this base generic - cycle detected
			b.addWarning("GENERIC_CYCLE", fmt.Sprintf(
				"Recursive generic expansion detected for %s, using reference", rawName), rawName)
			return nil
		}
		b.expandingGeneric[baseGeneric] = true
		defer delete(b.expandingGeneric, baseGeneric)
	}

	// Check for name collision
	fullName := pkg + "." + name
	if b.typeNames[fullName] {
		// Already processed
		return nil
	}
	b.typeNames[fullName] = true

	fields := []ir.FieldDescriptor{}
	extends := []ir.GoIdentifier{}

	// Process fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedding
		if field.Anonymous {
			if err := b.handleEmbedded(ctx, field, &fields, &extends, name, pkg); err != nil {
				return err
			}
			continue
		}

		// Parse json tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue // Skip this field
		}

		fd, err := b.buildFieldDescriptor(ctx, field, name, pkg)
		if err != nil {
			return fmt.Errorf("field %s.%s: %w", name, field.Name, err)
		}

		fields = append(fields, fd)
	}

	// Create struct descriptor
	desc := &ir.StructDescriptor{
		Name:    ir.GoIdentifier{Name: name, Package: pkg},
		Fields:  fields,
		Extends: extends,
		// Documentation and Source are zero values for reflection provider
	}

	b.schema.AddType(desc)
	return nil
}

// extractCustomMarshalerAlias creates an alias for a type implementing json.Marshaler or encoding.TextMarshaler.
// Per §3.3.6, these types are emitted as AliasDescriptor with Underlying = PrimitiveAny.
func (b *reflectionSchemaBuilder) extractCustomMarshalerAlias(t reflect.Type) error {
	name := t.Name()
	pkg := t.PkgPath()

	// Check for name collision
	fullName := pkg + "." + name
	if b.typeNames[fullName] {
		return nil
	}
	b.typeNames[fullName] = true

	// Add warning
	typeName := t.String()
	b.addWarning("CUSTOM_MARSHALER", fmt.Sprintf(
		"Type %s implements json.Marshaler or encoding.TextMarshaler, mapped to 'any'", typeName), typeName)

	// Create alias with PrimitiveAny as underlying type
	desc := &ir.AliasDescriptor{
		Name:       ir.GoIdentifier{Name: name, Package: pkg},
		Underlying: ir.Any(),
	}

	b.schema.AddType(desc)
	return nil
}

// extractAlias extracts a type alias or defined type.
func (b *reflectionSchemaBuilder) extractAlias(ctx context.Context, t reflect.Type) error {
	name := t.Name()
	pkg := t.PkgPath()

	// Check for name collision
	fullName := pkg + "." + name
	if b.typeNames[fullName] {
		return nil
	}
	b.typeNames[fullName] = true

	// Get underlying type - for aliases, we want the primitive type directly,
	// not a reference. Use typeToDescriptorNoAlias to skip alias handling.
	underlying, err := b.typeToDescriptorForAlias(ctx, t)
	if err != nil {
		return err
	}

	desc := &ir.AliasDescriptor{
		Name:       ir.GoIdentifier{Name: name, Package: pkg},
		Underlying: underlying,
	}

	b.schema.AddType(desc)
	return nil
}

// typeToDescriptorForAlias converts a type to a descriptor for use in an alias,
// without creating references to the alias itself (which would be circular).
func (b *reflectionSchemaBuilder) typeToDescriptorForAlias(ctx context.Context, t reflect.Type) (ir.TypeDescriptor, error) {
	// Check for special types first
	if desc := b.checkSpecialType(t); desc != nil {
		return desc, nil
	}

	// Check for error types (unsupported)
	if err := b.checkUnsupportedType(t); err != nil {
		return nil, err
	}

	// For aliases, just return the primitive type directly
	switch t.Kind() {
	case reflect.Bool:
		return ir.Bool(), nil

	case reflect.Int:
		return ir.Int(0), nil
	case reflect.Int8:
		return ir.Int(8), nil
	case reflect.Int16:
		return ir.Int(16), nil
	case reflect.Int32:
		return ir.Int(32), nil
	case reflect.Int64:
		return ir.Int(64), nil

	case reflect.Uint:
		return ir.Uint(0), nil
	case reflect.Uint8:
		return ir.Uint(8), nil
	case reflect.Uint16:
		return ir.Uint(16), nil
	case reflect.Uint32:
		return ir.Uint(32), nil
	case reflect.Uint64:
		return ir.Uint(64), nil
	case reflect.Uintptr:
		return ir.Uint(0), nil

	case reflect.Float32:
		return ir.Float(32), nil
	case reflect.Float64:
		return ir.Float(64), nil

	case reflect.String:
		return ir.String(), nil

	case reflect.Slice:
		// Special case: []byte
		if t.Elem().Kind() == reflect.Uint8 {
			return ir.Bytes(), nil
		}
		elem, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Slice(elem), nil

	case reflect.Array:
		elem, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Array(elem, t.Len()), nil

	case reflect.Map:
		// Validate key type
		if err := b.validateMapKeyType(t.Key()); err != nil {
			return nil, err
		}

		key, err := b.typeToDescriptor(ctx, t.Key(), "", "")
		if err != nil {
			return nil, err
		}
		value, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Map(key, value), nil

	case reflect.Ptr:
		elem, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Ptr(elem), nil

	case reflect.Struct:
		// Named struct - extract and reference
		if err := b.extractType(ctx, t); err != nil {
			return nil, err
		}
		structName := b.getTypeName(t)
		return ir.Ref(structName, t.PkgPath()), nil

	case reflect.Interface:
		// Interfaces map to PrimitiveAny with a warning
		typeName := t.String()
		b.addWarning("INTERFACE_TYPE", fmt.Sprintf("Interface type %s mapped to 'any'", typeName), typeName)
		return ir.Any(), nil

	default:
		return nil, fmt.Errorf("unsupported type: %s (kind: %s)", t.String(), t.Kind())
	}
}

// handleEmbedded processes an embedded field.
// parentStructName is the name of the containing struct, used for generating synthetic names for anonymous structs.
func (b *reflectionSchemaBuilder) handleEmbedded(ctx context.Context, field reflect.StructField, fields *[]ir.FieldDescriptor, extends *[]ir.GoIdentifier, parentStructName, parentPkg string) error {
	jsonTag := field.Tag.Get("json")

	// json:"-" means skip entirely
	if jsonTag == "-" {
		return nil
	}

	// Dereference pointer to get the embedded type
	embeddedType := field.Type
	for embeddedType.Kind() == reflect.Ptr {
		embeddedType = embeddedType.Elem()
	}

	// If there's a json tag (other than "-"), add as a field
	// Interfaces with json tags become fields with type `any` (PrimitiveAny)
	if jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		jsonName := parts[0]

		fd, err := b.buildFieldDescriptor(ctx, field, parentStructName, parentPkg)
		if err != nil {
			return err
		}
		fd.JSONName = jsonName
		*fields = append(*fields, fd)
		return nil
	}

	// No json tag case: would be added to Extends
	// Skip embedded interfaces without json tags - they map to PrimitiveAny
	// and would create dangling references when added to Extends.
	if embeddedType.Kind() == reflect.Interface {
		typeName := embeddedType.String()
		b.addWarning("EMBEDDED_INTERFACE", fmt.Sprintf("Embedded interface %s without json tag skipped (interfaces map to 'any' and cannot be extended)", typeName), typeName)
		return nil
	}

	// Extract the embedded type
	if err := b.extractType(ctx, embeddedType); err != nil {
		return err
	}

	// Get the type name
	typeName := b.getTypeName(embeddedType)
	pkg := embeddedType.PkgPath()

	// No json tag: add to Extends
	*extends = append(*extends, ir.GoIdentifier{Name: typeName, Package: pkg})

	return nil
}

// buildFieldDescriptor creates a FieldDescriptor from a reflect.StructField.
// parentStructName is the name of the containing struct, used for generating synthetic names.
// parentPkg is the package path of the containing struct type.
func (b *reflectionSchemaBuilder) buildFieldDescriptor(ctx context.Context, field reflect.StructField, parentStructName, parentPkg string) (ir.FieldDescriptor, error) {
	jsonTag := field.Tag.Get("json")
	jsonName, optional, skip, stringEncoded := parseJSONTag(jsonTag, field.Name)

	// Build type descriptor - use parentStructName + "_" + field.Name for anonymous struct naming
	syntheticName := parentStructName + "_" + field.Name
	fieldType, err := b.typeToDescriptor(ctx, field.Type, syntheticName, parentPkg)
	if err != nil {
		return ir.FieldDescriptor{}, err
	}

	// Build RawTags map
	rawTags := make(map[string]string)
	for _, tagName := range []string{"json", "validate", "db", "xml", "schema"} {
		if val := field.Tag.Get(tagName); val != "" {
			rawTags[tagName] = val
		}
	}

	return ir.FieldDescriptor{
		Name:          field.Name,
		Type:          fieldType,
		JSONName:      jsonName,
		Optional:      optional,
		StringEncoded: stringEncoded,
		Skip:          skip,
		ValidateTag:   field.Tag.Get("validate"),
		RawTags:       rawTags,
	}, nil
}

// typeToDescriptor converts a reflect.Type to a TypeDescriptor.
// parentName is used for generating synthetic names for anonymous structs.
// parentPkg is the package path of the containing type, used for anonymous struct references.
func (b *reflectionSchemaBuilder) typeToDescriptor(ctx context.Context, t reflect.Type, parentName, parentPkg string) (ir.TypeDescriptor, error) {
	// Check for special types first
	if desc := b.checkSpecialType(t); desc != nil {
		return desc, nil
	}

	// Check for error types (unsupported)
	if err := b.checkUnsupportedType(t); err != nil {
		return nil, err
	}

	switch t.Kind() {
	case reflect.Bool:
		// Check if this is a named bool type (alias)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		return ir.Bool(), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Check if this is a named integer type (alias)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		// Return appropriate primitive based on kind
		switch t.Kind() {
		case reflect.Int:
			return ir.Int(0), nil
		case reflect.Int8:
			return ir.Int(8), nil
		case reflect.Int16:
			return ir.Int(16), nil
		case reflect.Int32:
			return ir.Int(32), nil
		default: // Int64
			return ir.Int(64), nil
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		// Check if this is a named unsigned integer type (alias)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		// Return appropriate primitive based on kind
		switch t.Kind() {
		case reflect.Uint:
			return ir.Uint(0), nil
		case reflect.Uint8:
			return ir.Uint(8), nil
		case reflect.Uint16:
			return ir.Uint(16), nil
		case reflect.Uint32:
			return ir.Uint(32), nil
		case reflect.Uint64:
			return ir.Uint(64), nil
		default: // Uintptr
			return ir.Uint(0), nil
		}

	case reflect.Float32, reflect.Float64:
		// Check if this is a named float type (alias)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		if t.Kind() == reflect.Float32 {
			return ir.Float(32), nil
		}
		return ir.Float(64), nil

	case reflect.String:
		// Check if this is a named string type (alias)
		if t.Name() != "" && t.PkgPath() != "" {
			// Extract the alias and return a reference
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			name := b.getTypeName(t)
			return ir.Ref(name, t.PkgPath()), nil
		}
		return ir.String(), nil

	case reflect.Slice:
		// Check if this is a named slice type first (e.g., type MySlice []int)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		// Special case: []byte
		if t.Elem().Kind() == reflect.Uint8 {
			return ir.Bytes(), nil
		}
		elem, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Slice(elem), nil

	case reflect.Array:
		// Check if this is a named array type first (e.g., type Hash [32]byte)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		elem, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Array(elem, t.Len()), nil

	case reflect.Map:
		// Check if this is a named map type first (e.g., type StringMap map[string]string)
		if t.Name() != "" && t.PkgPath() != "" {
			if err := b.extractType(ctx, t); err != nil {
				return nil, err
			}
			return ir.Ref(b.getTypeName(t), t.PkgPath()), nil
		}
		// Validate key type
		if err := b.validateMapKeyType(t.Key()); err != nil {
			return nil, err
		}

		key, err := b.typeToDescriptor(ctx, t.Key(), "", "")
		if err != nil {
			return nil, err
		}
		value, err := b.typeToDescriptor(ctx, t.Elem(), "", "")
		if err != nil {
			return nil, err
		}
		return ir.Map(key, value), nil

	case reflect.Ptr:
		elem, err := b.typeToDescriptor(ctx, t.Elem(), parentName, parentPkg)
		if err != nil {
			return nil, err
		}
		return ir.Ptr(elem), nil

	case reflect.Struct:
		// Anonymous struct
		if t.Name() == "" {
			return b.handleAnonymousStruct(ctx, t, parentName, parentPkg)
		}

		// Named struct - extract and reference
		if err := b.extractType(ctx, t); err != nil {
			return nil, err
		}
		name := b.getTypeName(t)
		return ir.Ref(name, t.PkgPath()), nil

	case reflect.Interface:
		// Interfaces map to PrimitiveAny with a warning
		typeName := t.String()
		b.addWarning("INTERFACE_TYPE", fmt.Sprintf("Interface type %s mapped to 'any'", typeName), typeName)
		return ir.Any(), nil

	default:
		return nil, fmt.Errorf("unsupported type: %s (kind: %s)", t.String(), t.Kind())
	}
}

// checkSpecialType checks for special types that have dedicated IR representations.
func (b *reflectionSchemaBuilder) checkSpecialType(t reflect.Type) ir.TypeDescriptor {
	// Check for time.Time
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return ir.Time()
	}

	// Check for time.Duration
	if t.PkgPath() == "time" && t.Name() == "Duration" {
		return ir.Duration()
	}

	// Check for json.Number
	if t.PkgPath() == "encoding/json" && t.Name() == "Number" {
		return ir.String()
	}

	// Check for json.RawMessage
	if t.PkgPath() == "encoding/json" && t.Name() == "RawMessage" {
		return ir.Any()
	}

	// Check for empty interface
	if t.Kind() == reflect.Interface && t.NumMethod() == 0 {
		return ir.Any()
	}

	// Check for struct{}
	if t.Kind() == reflect.Struct && t.NumField() == 0 && t.Name() == "" {
		return ir.Empty()
	}

	// §3.3.6 Custom Marshaler Handling: Check for types implementing json.Marshaler
	// or encoding.TextMarshaler. These types emit PrimitiveAny since the provider
	// cannot statically determine the JSON output of custom marshal methods.
	// Note: This check comes AFTER well-known types (time.Time, etc.) which have
	// dedicated primitive kinds despite implementing these interfaces.
	if b.hasCustomMarshaler(t) {
		typeName := t.String()
		b.addWarning("CUSTOM_MARSHALER", fmt.Sprintf(
			"Type %s implements json.Marshaler or encoding.TextMarshaler, mapped to 'any'", typeName), typeName)
		return ir.Any()
	}

	return nil
}

// hasCustomMarshaler checks if a type implements json.Marshaler or encoding.TextMarshaler.
// Both the type and pointer-to-type are checked since marshaler methods may have
// either value or pointer receivers.
func (b *reflectionSchemaBuilder) hasCustomMarshaler(t reflect.Type) bool {
	// Check if the type or *type implements json.Marshaler
	if t.Implements(jsonMarshalerType) {
		return true
	}
	if t.Kind() != reflect.Ptr && reflect.PointerTo(t).Implements(jsonMarshalerType) {
		return true
	}

	// Check if the type or *type implements encoding.TextMarshaler
	if t.Implements(textMarshalerType) {
		return true
	}
	if t.Kind() != reflect.Ptr && reflect.PointerTo(t).Implements(textMarshalerType) {
		return true
	}

	return false
}

// checkUnsupportedType returns an error if the type is unsupported.
func (b *reflectionSchemaBuilder) checkUnsupportedType(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Chan:
		return fmt.Errorf("unsupported type: chan %s", t.Elem())
	case reflect.Complex64:
		return fmt.Errorf("unsupported type: complex64")
	case reflect.Complex128:
		return fmt.Errorf("unsupported type: complex128")
	case reflect.Func:
		return fmt.Errorf("unsupported type: func")
	case reflect.UnsafePointer:
		return fmt.Errorf("unsupported type: unsafe.Pointer")
	}
	return nil
}

// validateMapKeyType validates that the map key type is supported.
func (b *reflectionSchemaBuilder) validateMapKeyType(t reflect.Type) error {
	switch t.Kind() {
	case reflect.String:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return nil
	case reflect.Bool:
		return fmt.Errorf("unsupported map key type: bool")
	case reflect.Float32, reflect.Float64:
		return fmt.Errorf("unsupported map key type: %s", t.Kind())
	case reflect.Complex64, reflect.Complex128:
		return fmt.Errorf("unsupported map key type: %s", t.Kind())
	case reflect.Struct:
		// Check if it implements encoding.TextMarshaler
		if t.Implements(reflect.TypeOf((*interface{ MarshalText() ([]byte, error) })(nil)).Elem()) {
			return nil
		}
		return fmt.Errorf("unsupported map key type: struct without TextMarshaler")
	default:
		return fmt.Errorf("unsupported map key type: %s", t.Kind())
	}
}

// handleAnonymousStruct handles anonymous struct types by generating synthetic names.
// parentPkg is the package path of the containing type.
func (b *reflectionSchemaBuilder) handleAnonymousStruct(ctx context.Context, t reflect.Type, parentName, parentPkg string) (ir.TypeDescriptor, error) {
	// Check if we've already seen this anonymous struct
	if syntheticName, exists := b.anonStructs[t]; exists {
		return ir.Ref(syntheticName, parentPkg), nil
	}

	// Generate synthetic name
	if parentName == "" {
		return nil, fmt.Errorf("cannot generate synthetic name for anonymous struct without parent context")
	}

	// Issue 2 Fix: Make synthetic names unique by appending a counter.
	// This prevents collisions when multiple structs have fields with the same name
	// containing anonymous structs (e.g., User.Profile and Admin.Profile).
	syntheticName := parentName
	fullName := parentPkg + "." + syntheticName

	// If the name collides, append counter until we find a unique name
	for b.typeNames[fullName] {
		b.anonCounter++
		syntheticName = fmt.Sprintf("%s_%d", parentName, b.anonCounter)
		fullName = parentPkg + "." + syntheticName
	}

	b.anonStructs[t] = syntheticName

	// Extract the anonymous struct as a named type
	if err := b.extractAnonymousStruct(ctx, t, syntheticName, parentPkg); err != nil {
		return nil, err
	}

	return ir.Ref(syntheticName, parentPkg), nil
}

// extractAnonymousStruct extracts an anonymous struct with a synthetic name.
func (b *reflectionSchemaBuilder) extractAnonymousStruct(ctx context.Context, t reflect.Type, syntheticName, pkg string) error {
	// Mark the name as used (collision prevention is now handled in handleAnonymousStruct)
	fullName := pkg + "." + syntheticName
	b.typeNames[fullName] = true

	fields := []ir.FieldDescriptor{}
	extends := []ir.GoIdentifier{}

	// Process fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedding
		if field.Anonymous {
			if err := b.handleEmbedded(ctx, field, &fields, &extends, syntheticName, pkg); err != nil {
				return err
			}
			continue
		}

		// Parse json tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		// Generate field name for nested anonymous structs
		fieldSyntheticName := syntheticName + "_" + field.Name

		fd, err := b.buildFieldDescriptorWithName(ctx, field, fieldSyntheticName, pkg)
		if err != nil {
			return fmt.Errorf("field %s.%s: %w", syntheticName, field.Name, err)
		}

		fields = append(fields, fd)
	}

	// Create struct descriptor
	desc := &ir.StructDescriptor{
		Name:    ir.GoIdentifier{Name: syntheticName, Package: pkg},
		Fields:  fields,
		Extends: extends,
	}

	b.schema.AddType(desc)
	return nil
}

// buildFieldDescriptorWithName is like buildFieldDescriptor but passes along the synthetic name context.
// parentPkg is the package path of the containing struct type.
func (b *reflectionSchemaBuilder) buildFieldDescriptorWithName(ctx context.Context, field reflect.StructField, syntheticName, parentPkg string) (ir.FieldDescriptor, error) {
	jsonTag := field.Tag.Get("json")
	jsonName, optional, skip, stringEncoded := parseJSONTag(jsonTag, field.Name)

	// Build type descriptor
	fieldType, err := b.typeToDescriptor(ctx, field.Type, syntheticName, parentPkg)
	if err != nil {
		return ir.FieldDescriptor{}, err
	}

	// Build RawTags map
	rawTags := make(map[string]string)
	for _, tagName := range []string{"json", "validate", "db", "xml", "schema"} {
		if val := field.Tag.Get(tagName); val != "" {
			rawTags[tagName] = val
		}
	}

	return ir.FieldDescriptor{
		Name:          field.Name,
		Type:          fieldType,
		JSONName:      jsonName,
		Optional:      optional,
		StringEncoded: stringEncoded,
		Skip:          skip,
		ValidateTag:   field.Tag.Get("validate"),
		RawTags:       rawTags,
	}, nil
}

// getTypeName returns the name for a type, using synthetic naming for generic instantiations.
func (b *reflectionSchemaBuilder) getTypeName(t reflect.Type) string {
	name := t.Name()
	if name == "" {
		return ""
	}

	// Check for generic instantiation (contains brackets)
	if strings.Contains(name, "[") {
		return b.generateSyntheticName(name)
	}

	return name
}

// generateSyntheticName applies the synthetic naming algorithm for generic instantiations.
func (b *reflectionSchemaBuilder) generateSyntheticName(name string) string {
	// Replace special characters per §3.4 algorithm
	result := strings.ReplaceAll(name, ".", "_")
	result = strings.ReplaceAll(result, "/", "_")
	result = strings.ReplaceAll(result, "[", "_")
	result = strings.ReplaceAll(result, "]", "")
	result = strings.ReplaceAll(result, ",", "_")
	result = strings.ReplaceAll(result, " ", "")
	result = strings.ReplaceAll(result, "*", "Ptr")
	return result
}

// parseJSONTag parses a json struct tag and returns the JSON name and flags.
func parseJSONTag(tag, fieldName string) (jsonName string, optional, skip, stringEncoded bool) {
	if tag == "" {
		return fieldName, false, false, false
	}

	parts := strings.Split(tag, ",")
	jsonName = parts[0]

	// If name is exactly "-" and there are no options, skip the field
	if jsonName == "-" && len(parts) == 1 {
		return "", false, true, false
	}

	// If name is empty string (e.g., ",omitempty"), use field name
	if jsonName == "" {
		jsonName = fieldName
	}

	// Special case: "-," means a field literally named "-"
	// This is handled above when jsonName == "" gets set to fieldName,
	// but we need to preserve it if it's "-" with options
	if parts[0] == "-" && len(parts) > 1 {
		jsonName = "-"
	}

	// Parse options
	for i := 1; i < len(parts); i++ {
		switch parts[i] {
		case "omitempty", "omitzero":
			optional = true
		case "string":
			stringEncoded = true
		}
	}

	return jsonName, optional, false, stringEncoded
}

// addWarning adds a warning to the schema.
func (b *reflectionSchemaBuilder) addWarning(code, message, typeName string) {
	b.schema.AddWarning(ir.Warning{
		Code:     code,
		Message:  message,
		TypeName: typeName,
	})
}

// Check that our types implement json.Marshaler at compile time
var (
	_ json.Marshaler = (*time.Time)(nil)
)
