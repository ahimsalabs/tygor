package typescript

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"tygor.dev/tygorgen/ir"
)

// Emitter handles TypeScript code emission for IR type descriptors.
type Emitter struct {
	schema    *ir.Schema
	config    GeneratorConfig
	tsConfig  TypeScriptConfig
	indent    string // current indentation prefix (for nested emissions)
	indentStr string // single indent unit (tab or spaces from config)
}

// qualifyTypeName returns the qualified TypeScript name for a Go type.
// Types from the main package (Schema.Package) are not qualified.
// Types from other packages are qualified with the sanitized package path
// after removing StripPackagePrefix.
func (e *Emitter) qualifyTypeName(id ir.GoIdentifier) string {
	typeName := applyNameTransforms(id.Name, e.config)

	// If no StripPackagePrefix is configured, don't qualify anything (backward compat)
	if e.config.StripPackagePrefix == "" {
		return typeName
	}

	// Main package types are not qualified
	if id.Package == "" || id.Package == e.schema.Package.Path {
		return typeName
	}

	// External package - qualify with sanitized path
	pkgPath := id.Package

	// Strip the prefix
	pkgPath = strings.TrimPrefix(pkgPath, e.config.StripPackagePrefix)

	// If nothing was stripped (prefix didn't match), use full path
	// Sanitize the path: replace / and . with _
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, pkgPath)

	// Remove leading/trailing underscores and collapse multiple underscores
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}
	sanitized = strings.Trim(sanitized, "_")

	// Combine: pkg_prefix_TypeName
	if sanitized != "" {
		return sanitized + "_" + typeName
	}
	return typeName
}

// EmitType emits a top-level type declaration.
func (e *Emitter) EmitType(buf *bytes.Buffer, typ ir.TypeDescriptor) ([]ir.Warning, error) {
	// Build @see reference from package path + filename + symbol name
	var seeRef string
	if typeName := typ.TypeName(); !typeName.IsZero() {
		pkg := typeName.Package
		file := ""
		if src := typ.Src(); !src.IsZero() && src.File != "" {
			file = filepath.Base(src.File)
		}
		if pkg != "" && file != "" {
			seeRef = fmt.Sprintf("%s/%s#%s", pkg, file, typeName.Name)
		}
	}

	// Emit unified JSDoc block with doc comment and @see reference
	e.emitJSDocWithSee(buf, typ.Doc(), seeRef)

	switch t := typ.(type) {
	case *ir.StructDescriptor:
		return e.emitStruct(buf, t)
	case *ir.AliasDescriptor:
		return e.emitAlias(buf, t)
	case *ir.EnumDescriptor:
		return e.emitEnum(buf, t)
	default:
		return nil, fmt.Errorf("unsupported top-level type kind: %s", typ.Kind())
	}
}

// emitStruct emits a struct as an interface or type.
func (e *Emitter) emitStruct(buf *bytes.Buffer, s *ir.StructDescriptor) ([]ir.Warning, error) {
	var warnings []ir.Warning

	// Apply name transforms with package qualification
	typeName := e.qualifyTypeName(s.Name)
	typeName = escapeReservedWord(typeName)

	// Emit export keyword
	if e.tsConfig.EmitExport {
		buf.WriteString("export ")
	}
	if e.tsConfig.EmitDeclare {
		buf.WriteString("declare ")
	}

	// Emit type parameters
	typeParams := ""
	if len(s.TypeParameters) > 0 {
		var err error
		typeParams, err = e.emitTypeParameters(s.TypeParameters)
		if err != nil {
			return nil, err
		}
	}

	// Decide whether to use interface or type
	useInterface := e.tsConfig.UseInterface && len(s.Extends) == 0

	if useInterface {
		// Note: useInterface is only true when len(s.Extends) == 0,
		// so we don't need to handle extends here. Structs with extends
		// use the type alias syntax with intersection types below.
		buf.WriteString("interface ")
		buf.WriteString(typeName)
		buf.WriteString(typeParams)
		buf.WriteString(" {\n")
	} else {
		buf.WriteString("type ")
		buf.WriteString(typeName)
		buf.WriteString(typeParams)
		buf.WriteString(" = ")

		// Handle extends with intersection types
		if len(s.Extends) > 0 {
			for _, ext := range s.Extends {
				extName := e.qualifyTypeName(ext)
				extName = escapeReservedWord(extName)
				buf.WriteString(extName)
				buf.WriteString(" & ")
			}
		}

		buf.WriteString("{\n")
	}

	// Emit fields
	for _, field := range s.Fields {
		if field.Skip {
			continue
		}

		// Check for large integer warning (Appendix A.2)
		if w := e.checkLargeIntegerWarning(field, s.Name.Name); w != nil {
			warnings = append(warnings, *w)
		}

		// Field documentation
		if e.config.EmitComments && !field.Documentation.IsZero() {
			buf.WriteString(e.indent)
			buf.WriteString(e.indentStr)
			e.emitJSDoc(buf, field.Documentation)
		}

		buf.WriteString(e.indent)
		buf.WriteString(e.indentStr)

		// Field name (escape if reserved word or needs quoting)
		fieldName := e.getPropertyName(field)
		if needsQuoting(fieldName) {
			buf.WriteString(fmt.Sprintf("%q", fieldName))
		} else {
			fieldName = escapeReservedWord(fieldName)
			buf.WriteString(fieldName)
		}

		// Determine optional vs nullable based on §4.9
		optional, nullable, err := e.determineOptionalNullable(field)
		if err != nil {
			return nil, err
		}

		// Optional marker
		if optional {
			buf.WriteString("?")
		}

		buf.WriteString(": ")

		// Emit type
		// For pointer fields, unwrap ALL nested pointers before emitting since
		// emitPtr adds | null and we handle nullability/optionality separately
		// at field level. Go's encoding/json flattens multiple pointer levels,
		// so **string behaves like *string in JSON serialization.
		fieldType := field.Type
		for {
			if ptr, ok := fieldType.(*ir.PtrDescriptor); ok {
				fieldType = ptr.Element
			} else {
				break
			}
		}
		typeExpr, err := e.EmitTypeExpr(fieldType)
		if err != nil {
			return nil, fmt.Errorf("failed to emit field %s type: %w", field.Name, err)
		}
		buf.WriteString(typeExpr)

		// Nullable marker
		if nullable {
			buf.WriteString(" | null")
		}

		buf.WriteString(";\n")
	}

	buf.WriteString(e.indent)
	if useInterface {
		buf.WriteString("}")
	} else {
		buf.WriteString("};")
	}

	return warnings, nil
}

// emitAlias emits a type alias.
func (e *Emitter) emitAlias(buf *bytes.Buffer, a *ir.AliasDescriptor) ([]ir.Warning, error) {
	// Apply name transforms with package qualification
	typeName := e.qualifyTypeName(a.Name)
	typeName = escapeReservedWord(typeName)

	// Emit export keyword
	if e.tsConfig.EmitExport {
		buf.WriteString("export ")
	}
	if e.tsConfig.EmitDeclare {
		buf.WriteString("declare ")
	}

	buf.WriteString("type ")
	buf.WriteString(typeName)

	// Emit type parameters
	if len(a.TypeParameters) > 0 {
		typeParams, err := e.emitTypeParameters(a.TypeParameters)
		if err != nil {
			return nil, err
		}
		buf.WriteString(typeParams)
	}

	buf.WriteString(" = ")

	// Emit underlying type
	underlying, err := e.EmitTypeExpr(a.Underlying)
	if err != nil {
		return nil, fmt.Errorf("failed to emit alias underlying type: %w", err)
	}
	buf.WriteString(underlying)
	buf.WriteString(";")

	return nil, nil
}

// emitEnum emits an enum based on the configured style.
func (e *Emitter) emitEnum(buf *bytes.Buffer, enum *ir.EnumDescriptor) ([]ir.Warning, error) {
	// Apply name transforms with package qualification
	typeName := e.qualifyTypeName(enum.Name)
	typeName = escapeReservedWord(typeName)

	switch e.tsConfig.EnumStyle {
	case "union":
		return e.emitEnumAsUnion(buf, typeName, enum)
	case "enum":
		return e.emitEnumAsEnum(buf, typeName, enum, false)
	case "const_enum":
		return e.emitEnumAsEnum(buf, typeName, enum, true)
	case "object":
		return e.emitEnumAsObject(buf, typeName, enum)
	default:
		// Default to union
		return e.emitEnumAsUnion(buf, typeName, enum)
	}
}

// emitEnumAsUnion emits an enum as a union type.
func (e *Emitter) emitEnumAsUnion(buf *bytes.Buffer, typeName string, enum *ir.EnumDescriptor) ([]ir.Warning, error) {
	if e.tsConfig.EmitExport {
		buf.WriteString("export ")
	}
	if e.tsConfig.EmitDeclare {
		buf.WriteString("declare ")
	}

	buf.WriteString("type ")
	buf.WriteString(typeName)
	buf.WriteString(" = ")

	for i, member := range enum.Members {
		if i > 0 {
			buf.WriteString(" | ")
		}
		buf.WriteString(formatEnumValue(member.Value))
	}

	buf.WriteString(";")
	return nil, nil
}

// emitEnumAsEnum emits an enum as a TypeScript enum.
func (e *Emitter) emitEnumAsEnum(buf *bytes.Buffer, typeName string, enum *ir.EnumDescriptor, isConst bool) ([]ir.Warning, error) {
	if e.tsConfig.EmitExport {
		buf.WriteString("export ")
	}
	if e.tsConfig.EmitDeclare {
		buf.WriteString("declare ")
	}

	if isConst {
		buf.WriteString("const ")
	}

	buf.WriteString("enum ")
	buf.WriteString(typeName)
	buf.WriteString(" {\n")

	for i, member := range enum.Members {
		if i > 0 {
			buf.WriteString(",\n")
		}

		// Member documentation
		if e.config.EmitComments && !member.Documentation.IsZero() {
			buf.WriteString(e.indentStr)
			e.emitJSDoc(buf, member.Documentation)
			buf.WriteString(e.indentStr)
		} else {
			buf.WriteString(e.indentStr)
		}

		memberName := escapeReservedWord(member.Name)
		buf.WriteString(memberName)
		buf.WriteString(" = ")
		buf.WriteString(formatEnumValue(member.Value))
	}

	buf.WriteString(",\n")
	buf.WriteString("}")
	return nil, nil
}

// emitEnumAsObject emits an enum as a const object.
func (e *Emitter) emitEnumAsObject(buf *bytes.Buffer, typeName string, enum *ir.EnumDescriptor) ([]ir.Warning, error) {
	if e.tsConfig.EmitExport {
		buf.WriteString("export ")
	}
	if e.tsConfig.EmitDeclare {
		buf.WriteString("declare ")
	}

	buf.WriteString("const ")
	buf.WriteString(typeName)
	buf.WriteString(" = {\n")

	for i, member := range enum.Members {
		if i > 0 {
			buf.WriteString(",\n")
		}

		// Member documentation
		if e.config.EmitComments && !member.Documentation.IsZero() {
			buf.WriteString(e.indentStr)
			e.emitJSDoc(buf, member.Documentation)
			buf.WriteString(e.indentStr)
		} else {
			buf.WriteString(e.indentStr)
		}

		memberName := escapeReservedWord(member.Name)
		buf.WriteString(memberName)
		buf.WriteString(": ")
		buf.WriteString(formatEnumValue(member.Value))
	}

	buf.WriteString(",\n")
	buf.WriteString("} as const;")
	return nil, nil
}

// EmitTypeExpr emits a type expression (non-top-level types).
func (e *Emitter) EmitTypeExpr(typ ir.TypeDescriptor) (string, error) {
	switch t := typ.(type) {
	case *ir.PrimitiveDescriptor:
		return e.emitPrimitive(t), nil
	case *ir.ArrayDescriptor:
		return e.emitArray(t)
	case *ir.MapDescriptor:
		return e.emitMap(t)
	case *ir.ReferenceDescriptor:
		return e.emitReference(t), nil
	case *ir.PtrDescriptor:
		return e.emitPtr(t)
	case *ir.UnionDescriptor:
		return e.emitUnion(t)
	case *ir.TypeParameterDescriptor:
		return e.emitTypeParameter(t), nil
	default:
		return "", fmt.Errorf("unsupported type expression kind: %s", typ.Kind())
	}
}

// emitPrimitive emits a primitive type.
func (e *Emitter) emitPrimitive(p *ir.PrimitiveDescriptor) string {
	tsType, hint := e.primitiveTypeAndHint(p)
	if e.tsConfig.EmitTypeHints && hint != "" {
		return tsType + " /* " + hint + " */"
	}
	return tsType
}

// primitiveTypeAndHint returns the TypeScript type and optional hint for a primitive.
func (e *Emitter) primitiveTypeAndHint(p *ir.PrimitiveDescriptor) (tsType, hint string) {
	switch p.PrimitiveKind {
	case ir.PrimitiveBool:
		return "boolean", ""
	case ir.PrimitiveInt:
		hint = e.intHint(p.BitSize, true)
		return "number", hint
	case ir.PrimitiveUint:
		hint = e.intHint(p.BitSize, false)
		return "number", hint
	case ir.PrimitiveFloat:
		if p.BitSize == 32 {
			return "number", "float32"
		}
		return "number", "float64"
	case ir.PrimitiveString:
		return "string", ""
	case ir.PrimitiveBytes:
		return "string", "base64"
	case ir.PrimitiveTime:
		// Check for custom type mapping
		if mapped, ok := e.config.TypeMappings["time.Time"]; ok {
			return mapped, ""
		}
		return "string", "RFC3339"
	case ir.PrimitiveDuration:
		// Check for custom type mapping
		if mapped, ok := e.config.TypeMappings["time.Duration"]; ok {
			return mapped, ""
		}
		return "number", "nanoseconds"
	case ir.PrimitiveAny:
		return e.tsConfig.UnknownType, ""
	case ir.PrimitiveEmpty:
		return "Record<string, never>", ""
	default:
		return e.tsConfig.UnknownType, ""
	}
}

// intHint returns a type hint for integer types.
func (e *Emitter) intHint(bitSize int, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	if bitSize == 0 {
		return prefix // platform-dependent
	}
	return fmt.Sprintf("%s%d", prefix, bitSize)
}

// emitArray emits an array type.
// By default, pointer element types are unwrapped: []*T → T[] rather than (T | null)[].
// This matches Go semantics where []*T means "a slice of T values" - the pointer is an
// implementation detail (efficiency/mutability), not expressing optionality of elements.
// Set NullableSliceElements=true to preserve pointer nullability in array elements.
func (e *Emitter) emitArray(a *ir.ArrayDescriptor) (string, error) {
	elem := a.Element

	// Unless NullableSliceElements is set, unwrap pointer elements
	if !e.tsConfig.NullableSliceElements {
		for {
			if ptr, ok := elem.(*ir.PtrDescriptor); ok {
				elem = ptr.Element
			} else {
				break
			}
		}
	}

	elemType, err := e.EmitTypeExpr(elem)
	if err != nil {
		return "", err
	}

	if a.Length > 0 {
		// Fixed-length array: emit as tuple if small, otherwise regular array
		if a.Length <= 10 {
			// Emit as tuple: [T, T, T]
			parts := make([]string, a.Length)
			for i := 0; i < a.Length; i++ {
				parts[i] = elemType
			}
			return "[" + strings.Join(parts, ", ") + "]", nil
		}
		// For large fixed arrays, just use T[]
	}

	// Slice or large array: T[] or readonly T[]
	if e.tsConfig.UseReadonlyArrays {
		return "readonly " + elemType + "[]", nil
	}
	return elemType + "[]", nil
}

// emitMap emits a map type.
func (e *Emitter) emitMap(m *ir.MapDescriptor) (string, error) {
	keyType, err := e.EmitTypeExpr(m.Key)
	if err != nil {
		return "", err
	}
	valueType, err := e.EmitTypeExpr(m.Value)
	if err != nil {
		return "", err
	}

	// All maps serialize to string keys in JSON
	// But preserve the key type for named string types
	if _, ok := m.Key.(*ir.ReferenceDescriptor); ok {
		// Named type - preserve it
		return fmt.Sprintf("Record<%s, %s>", keyType, valueType), nil
	}
	// Primitive key - always use string
	return fmt.Sprintf("Record<string, %s>", valueType), nil
}

// emitReference emits a reference to a named type.
func (e *Emitter) emitReference(r *ir.ReferenceDescriptor) string {
	typeName := e.qualifyTypeName(r.Target)
	return escapeReservedWord(typeName)
}

// emitPtr emits a pointer type with | null.
// Note: PtrDescriptor nullability is context-dependent (§4.9).
// For field-level pointers, determineOptionalNullable handles nullability.
// For array elements, emitArray unwraps pointers (Go's []*T is semantically T[]).
// For map values, we add | null here since map[K]*V can have nil values.
// This method recursively unwraps nested pointers to avoid (T | null) | null.
func (e *Emitter) emitPtr(p *ir.PtrDescriptor) (string, error) {
	// Unwrap all nested pointers to get to the base type
	elem := p.Element
	for {
		if innerPtr, ok := elem.(*ir.PtrDescriptor); ok {
			elem = innerPtr.Element
		} else {
			break
		}
	}

	elemType, err := e.EmitTypeExpr(elem)
	if err != nil {
		return "", err
	}
	// Parenthesize to ensure correct precedence in compound types
	// e.g., map[string]*int → Record<string, (number | null)>
	return "(" + elemType + " | null)", nil
}

// emitUnion emits a union type.
func (e *Emitter) emitUnion(u *ir.UnionDescriptor) (string, error) {
	var parts []string
	for _, t := range u.Types {
		part, err := e.EmitTypeExpr(t)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " | "), nil
}

// emitTypeParameter emits a type parameter reference.
func (e *Emitter) emitTypeParameter(tp *ir.TypeParameterDescriptor) string {
	return tp.ParamName
}

// emitTypeParameters emits type parameter declarations.
func (e *Emitter) emitTypeParameters(params []ir.TypeParameterDescriptor) (string, error) {
	if len(params) == 0 {
		return "", nil
	}

	var parts []string
	for _, param := range params {
		part := param.ParamName
		if param.Constraint != nil {
			constraintType, err := e.EmitTypeExpr(param.Constraint)
			if err != nil {
				return "", fmt.Errorf("failed to emit constraint for type parameter %s: %w", param.ParamName, err)
			}
			part += " extends " + constraintType
		}
		parts = append(parts, part)
	}

	return "<" + strings.Join(parts, ", ") + ">", nil
}

// determineOptionalNullable determines if a field should be optional and/or nullable.
// Implements the decision tree from §4.9. Optional and nullable are independent:
// - Optional (`?:`) is true when omitempty/omitzero is set
// - Nullable (`| null`) is true when the underlying type can be nil (pointer, slice, map)
func (e *Emitter) determineOptionalNullable(field ir.FieldDescriptor) (optional, nullable bool, err error) {
	// Optional is determined by the omitempty/omitzero tag
	optional = field.Optional

	// Nullable is determined by whether the type can hold nil
	// Check for pointer, slice, or map (unwrapping pointers to get to the base)
	fieldType := field.Type
	switch fieldType.(type) {
	case *ir.PtrDescriptor:
		nullable = true
	case *ir.ArrayDescriptor:
		nullable = true
	case *ir.MapDescriptor:
		nullable = true
	}

	// Apply OptionalType override if configured
	switch e.tsConfig.OptionalType {
	case "null":
		// Force all optional/nullable fields to use | null only (no ?:)
		if optional || nullable {
			return false, true, nil
		}
	case "undefined":
		// Force all optional/nullable fields to use ?: only (no | null)
		if optional || nullable {
			return true, false, nil
		}
	}
	// default: use the independent optional/nullable values as computed

	return optional, nullable, nil
}

// getPropertyName returns the property name for a field.
func (e *Emitter) getPropertyName(field ir.FieldDescriptor) string {
	switch e.config.PropertyNameSource {
	case "field":
		return applyCaseTransform(field.Name, e.config.FieldCase)
	default: // "tag:json" or empty (default to json)
		name := field.JSONName
		if name == "" {
			name = field.Name
		}
		return applyCaseTransform(name, e.config.FieldCase)
	}
}

// emitJSDocWithSee emits a unified JSDoc block with documentation and @see reference.
// If both doc and seeRef are empty, nothing is emitted.
// If only seeRef is provided, emits a single-line JSDoc with just @see.
// If doc is provided, emits doc content with @see appended (when present).
func (e *Emitter) emitJSDocWithSee(buf *bytes.Buffer, doc ir.Documentation, seeRef string) {
	hasDoc := e.config.EmitComments && !doc.IsZero()
	hasSee := seeRef != ""

	if !hasDoc && !hasSee {
		return
	}

	// Simple case: only @see, no doc
	if !hasDoc && hasSee {
		buf.WriteString("/** @see ")
		buf.WriteString(seeRef)
		buf.WriteString(" */\n")
		return
	}

	// Has doc content
	lines := strings.Split(doc.Body, "\n")
	needsMultiLine := len(lines) > 1 || doc.Deprecated != nil || hasSee

	if !needsMultiLine {
		// Single line doc without @see or @deprecated
		buf.WriteString("/** ")
		buf.WriteString(strings.TrimSpace(lines[0]))
		buf.WriteString(" */\n")
		return
	}

	// Multi-line format
	buf.WriteString("/**\n")
	for _, line := range lines {
		buf.WriteString(" * ")
		buf.WriteString(strings.TrimSpace(line))
		buf.WriteString("\n")
	}

	if doc.Deprecated != nil {
		buf.WriteString(" * @deprecated")
		if *doc.Deprecated != "" {
			buf.WriteString(" ")
			buf.WriteString(*doc.Deprecated)
		}
		buf.WriteString("\n")
	}

	if hasSee {
		buf.WriteString(" * @see ")
		buf.WriteString(seeRef)
		buf.WriteString("\n")
	}

	buf.WriteString(" */\n")
}

// emitJSDoc emits JSDoc-style documentation comments.
func (e *Emitter) emitJSDoc(buf *bytes.Buffer, doc ir.Documentation) {
	e.emitJSDocWithSee(buf, doc, "")
}

// formatEnumValue formats an enum member value for output.
func formatEnumValue(value any) string {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("%q", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		// Use strconv.FormatFloat to avoid scientific notation (e.g., 1e+06)
		// which is invalid in TypeScript enum values
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// checkLargeIntegerWarning checks if a field uses int64/uint64 without ,string tag
// and returns a warning per Appendix A.2 of the spec.
func (e *Emitter) checkLargeIntegerWarning(field ir.FieldDescriptor, typeName string) *ir.Warning {
	// If the field uses json:",string", no warning needed
	if field.StringEncoded {
		return nil
	}

	// Extract the base type, unwrapping pointers
	baseType := field.Type
	for {
		if ptr, ok := baseType.(*ir.PtrDescriptor); ok {
			baseType = ptr.Element
		} else {
			break
		}
	}

	// Check if it's a primitive int64 or uint64
	prim, ok := baseType.(*ir.PrimitiveDescriptor)
	if !ok {
		return nil
	}

	// Only warn for 64-bit integers
	if prim.BitSize != 64 {
		return nil
	}

	if prim.PrimitiveKind != ir.PrimitiveInt && prim.PrimitiveKind != ir.PrimitiveUint {
		return nil
	}

	// Construct warning
	kindName := "int64"
	if prim.PrimitiveKind == ir.PrimitiveUint {
		kindName = "uint64"
	}

	return &ir.Warning{
		Code:     "LARGE_INT_PRECISION",
		Message:  fmt.Sprintf("%s field %q in type %q may lose precision in JavaScript (max safe integer: 2^53-1). Consider using json:\",string\" tag.", kindName, field.Name, typeName),
		TypeName: typeName,
	}
}
