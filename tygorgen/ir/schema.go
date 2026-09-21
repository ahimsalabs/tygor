package ir

import (
	"encoding/json"
	"reflect"
	"strconv"
)

type stringEncodingProbePointer *int

type definedPointerStringEncodingSupport struct {
	direct  bool
	pointer bool
}

var definedPointerStringEncoding = func() definedPointerStringEncodingSupport {
	value := 1
	directValue := stringEncodingProbePointer(&value)
	probeType := reflect.StructOf([]reflect.StructField{
		{Name: "Direct", Type: reflect.TypeOf(directValue), Tag: `json:"direct,string"`},
		{Name: "Pointer", Type: reflect.TypeOf(&directValue), Tag: `json:"pointer,string"`},
	})
	probe := reflect.New(probeType).Elem()
	probe.Field(0).Set(reflect.ValueOf(directValue))
	probe.Field(1).Set(reflect.ValueOf(&directValue))
	payload, err := json.Marshal(probe.Interface())
	if err != nil {
		return definedPointerStringEncodingSupport{}
	}
	var decoded map[string]any
	if json.Unmarshal(payload, &decoded) != nil {
		return definedPointerStringEncodingSupport{}
	}
	_, direct := decoded["direct"].(string)
	_, pointer := decoded["pointer"].(string)
	return definedPointerStringEncodingSupport{direct: direct, pointer: pointer}
}()

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

	// Warnings contains non-fatal issues encountered during schema building.
	Warnings []Warning
}

// AddType adds a named type descriptor to the schema.
func (s *Schema) AddType(t TypeDescriptor) {
	s.Types = append(s.Types, t)
}

// AddService adds a service descriptor to the schema.
func (s *Schema) AddService(svc ServiceDescriptor) {
	s.Services = append(s.Services, svc)
}

// AddWarning adds a warning to the schema.
func (s *Schema) AddWarning(w Warning) {
	s.Warnings = append(s.Warnings, w)
}

// FindType looks up a type by name. Returns nil if not found.
func (s *Schema) FindType(name GoIdentifier) TypeDescriptor {
	for _, t := range s.Types {
		if !isNilTypeDescriptor(t) && t.TypeName() == name {
			return t
		}
	}
	return nil
}

// FindService looks up a service by name. Returns nil if not found.
func (s *Schema) FindService(name string) *ServiceDescriptor {
	for i := range s.Services {
		if s.Services[i].Name == name {
			return &s.Services[i]
		}
	}
	return nil
}

// Validate checks the schema for structural issues per §4.8.
// Returns all validation errors found (not just the first).
func (s *Schema) Validate() []error {
	if s == nil {
		return []error{&ValidationError{Code: "nil_schema", Message: "schema is nil"}}
	}

	var errors []*ValidationError

	// Build a set of valid top-level declarations before walking their contents.
	typeNames := make(map[GoIdentifier]bool)
	typeArities := make(map[GoIdentifier]int)
	for i, t := range s.Types {
		if isNilTypeDescriptor(t) {
			errors = append(errors, &ValidationError{
				Code:    "nil_top_level_type",
				Message: "schema type at index " + strconv.Itoa(i) + " is nil",
			})
			continue
		}

		var arity int
		switch d := t.(type) {
		case *StructDescriptor:
			arity = len(d.TypeParameters)
		case *AliasDescriptor:
			arity = len(d.TypeParameters)
		case *EnumDescriptor:
		default:
			errors = append(errors, &ValidationError{
				Code:    "invalid_top_level_type",
				Message: "schema type at index " + strconv.Itoa(i) + " must be a struct, alias, or enum",
			})
			continue
		}

		name := t.TypeName()
		if name.IsZero() {
			errors = append(errors, &ValidationError{
				Code:    "missing_type_name",
				Message: "schema type at index " + strconv.Itoa(i) + " has no name",
			})
			continue
		}
		if typeNames[name] {
			errors = append(errors, &ValidationError{
				Code:    "duplicate_type",
				Message: "duplicate type name: " + name.Name + " (package: " + name.Package + ")",
			})
		}
		typeNames[name] = true
		typeArities[name] = arity
	}

	// Validate all nested descriptors in top-level declarations.
	for _, t := range s.Types {
		switch d := t.(type) {
		case *StructDescriptor:
			if d == nil {
				continue
			}
			typeParameters, parameterErrors := validateTypeParameters(d.TypeParameters, "type "+d.Name.Name)
			errors = append(errors, parameterErrors...)
			for i := range d.TypeParameters {
				if d.TypeParameters[i].Constraint != nil {
					errors = append(errors, validateTypeDescriptor(d.TypeParameters[i].Constraint, typeNames, typeArities, typeParameters, "type "+d.Name.Name+" parameter "+d.TypeParameters[i].ParamName+" constraint")...)
				}
			}
			for _, field := range d.Fields {
				context := "field " + d.Name.Name + "." + field.Name
				errors = append(errors, validateTypeDescriptor(field.Type, typeNames, typeArities, typeParameters, context)...)
				if !isNilTypeDescriptor(field.Type) && field.StringEncoded && !s.StringEncodingApplies(field.Type) {
					errors = append(errors, &ValidationError{
						Code:    "invalid_string_encoded",
						Message: "StringEncoded set on incompatible type for field " + d.Name.Name + "." + field.Name + ": only string, integer, float, boolean, and duration types at encoding/json's supported pointer depth use json:\",string\"",
					})
				}
			}
			// Validate Extends references exist and are structs.
			for _, ext := range d.Extends {
				if !typeNames[ext] {
					errors = append(errors, &ValidationError{
						Code:    "missing_extends_reference",
						Message: "struct " + d.Name.Name + " extends unknown type: " + ext.Name,
					})
					continue
				}
				if typeArities[ext] != 0 {
					errors = append(errors, &ValidationError{
						Code:    "generic_extends_reference",
						Message: "struct " + d.Name.Name + " cannot extend generic type without type arguments: " + ext.Name,
					})
				}
				extType := s.FindType(ext)
				if extType != nil && extType.Kind() != KindStruct {
					errors = append(errors, &ValidationError{
						Code:    "extends_non_struct",
						Message: "struct " + d.Name.Name + " extends non-struct type: " + ext.Name,
					})
				}
			}
		case *AliasDescriptor:
			if d == nil {
				continue
			}
			typeParameters, parameterErrors := validateTypeParameters(d.TypeParameters, "type "+d.Name.Name)
			errors = append(errors, parameterErrors...)
			for i := range d.TypeParameters {
				if d.TypeParameters[i].Constraint != nil {
					errors = append(errors, validateTypeDescriptor(d.TypeParameters[i].Constraint, typeNames, typeArities, typeParameters, "type "+d.Name.Name+" parameter "+d.TypeParameters[i].ParamName+" constraint")...)
				}
			}
			errors = append(errors, validateTypeDescriptor(d.Underlying, typeNames, typeArities, typeParameters, "alias "+d.Name.Name+" underlying type")...)
		case *EnumDescriptor:
			if d == nil {
				continue
			}
			for _, member := range d.Members {
				if !isValidEnumValueType(member.Value) {
					errors = append(errors, &ValidationError{
						Code:    "invalid_enum_value_type",
						Message: "enum " + d.Name.Name + " member " + member.Name + " has invalid value type: only string, int64, and float64 are allowed",
					})
				}
			}
		default:
			// Nil and expression descriptors were reported in the first pass.
		}
	}

	// Check for circular inheritance.
	if circularErrs := s.detectCircularInheritance(); len(circularErrs) > 0 {
		errors = append(errors, circularErrs...)
	}

	// Walk all Services and Endpoints.
	for _, service := range s.Services {
		endpointNames := make(map[string]bool)

		for _, endpoint := range service.Endpoints {
			// A nil request denotes a no-parameter endpoint. Every endpoint must
			// declare a response; void responses use Ptr(Empty()).
			if endpoint.Request != nil {
				errors = append(errors, validateTypeDescriptor(endpoint.Request, typeNames, typeArities, nil, "endpoint "+endpoint.FullName+" Request")...)
			}
			errors = append(errors, validateTypeDescriptor(endpoint.Response, typeNames, typeArities, nil, "endpoint "+endpoint.FullName+" Response")...)

			expectedFullName := service.Name + "." + endpoint.Name
			if endpoint.FullName != expectedFullName {
				errors = append(errors, &ValidationError{
					Code:    "invalid_fullname",
					Message: "endpoint FullName must be ServiceName.EndpointName: expected " + expectedFullName + ", got " + endpoint.FullName,
				})
			}

			expectedPath := "/" + service.Name + "/" + endpoint.Name
			if endpoint.Path != expectedPath {
				errors = append(errors, &ValidationError{
					Code:    "invalid_path",
					Message: "endpoint Path must be /ServiceName/EndpointName: expected " + expectedPath + ", got " + endpoint.Path,
				})
			}

			if endpointNames[endpoint.Name] {
				errors = append(errors, &ValidationError{
					Code:    "duplicate_endpoint",
					Message: "duplicate endpoint name in service " + service.Name + ": " + endpoint.Name,
				})
			}
			endpointNames[endpoint.Name] = true
		}
	}

	result := make([]error, len(errors))
	for i, err := range errors {
		result[i] = err
	}
	return result
}

func isNilTypeDescriptor(td TypeDescriptor) bool {
	if td == nil {
		return true
	}
	switch d := td.(type) {
	case *StructDescriptor:
		return d == nil
	case *AliasDescriptor:
		return d == nil
	case *EnumDescriptor:
		return d == nil
	case *PrimitiveDescriptor:
		return d == nil
	case *ArrayDescriptor:
		return d == nil
	case *MapDescriptor:
		return d == nil
	case *ReferenceDescriptor:
		return d == nil
	case *PtrDescriptor:
		return d == nil
	case *UnionDescriptor:
		return d == nil
	case *TypeParameterDescriptor:
		return d == nil
	default:
		return false
	}
}

// validateTypeDescriptor recursively checks required children, references, and
// exact generic application arity.
func validateTypeDescriptor(td TypeDescriptor, typeNames map[GoIdentifier]bool, typeArities map[GoIdentifier]int, typeParameters map[string]bool, context string) []*ValidationError {
	return validateTypeDescriptorPath(td, typeNames, typeArities, typeParameters, context, make(map[TypeDescriptor]bool))
}

func validateTypeDescriptorPath(td TypeDescriptor, typeNames map[GoIdentifier]bool, typeArities map[GoIdentifier]int, typeParameters map[string]bool, context string, active map[TypeDescriptor]bool) []*ValidationError {
	if isNilTypeDescriptor(td) {
		return []*ValidationError{{
			Code:    "nil_type_descriptor",
			Message: context + " has a nil type descriptor",
		}}
	}
	if active[td] {
		return []*ValidationError{{
			Code:    "cyclic_type_expression",
			Message: context + " contains a cyclic type expression",
		}}
	}
	active[td] = true
	defer delete(active, td)

	var errors []*ValidationError
	switch d := td.(type) {
	case *ReferenceDescriptor:
		known := typeNames[d.Target]
		if !known {
			errors = append(errors, &ValidationError{
				Code:    "missing_type_reference",
				Message: context + " references unknown type: " + d.Target.Name,
			})
		} else if len(d.TypeArguments) != typeArities[d.Target] {
			errors = append(errors, &ValidationError{
				Code:    "invalid_type_argument_count",
				Message: context + " applies " + d.Target.Name + " with " + strconv.Itoa(len(d.TypeArguments)) + " type arguments; expected " + strconv.Itoa(typeArities[d.Target]),
			})
		}
		for i, arg := range d.TypeArguments {
			errors = append(errors, validateTypeDescriptorPath(arg, typeNames, typeArities, typeParameters, context+" type argument "+strconv.Itoa(i), active)...)
		}
	case *ArrayDescriptor:
		if d.Length < 0 {
			errors = append(errors, &ValidationError{
				Code:    "invalid_array_length",
				Message: context + " has a negative array length",
			})
		}
		errors = append(errors, validateTypeDescriptorPath(d.Element, typeNames, typeArities, typeParameters, context+" element", active)...)
	case *MapDescriptor:
		errors = append(errors, validateTypeDescriptorPath(d.Key, typeNames, typeArities, typeParameters, context+" key", active)...)
		errors = append(errors, validateTypeDescriptorPath(d.Value, typeNames, typeArities, typeParameters, context+" value", active)...)
	case *PtrDescriptor:
		errors = append(errors, validateTypeDescriptorPath(d.Element, typeNames, typeArities, typeParameters, context+" element", active)...)
	case *UnionDescriptor:
		if len(d.Types) == 0 {
			errors = append(errors, &ValidationError{
				Code:    "empty_union",
				Message: context + " contains union with no types",
			})
		}
		for i, member := range d.Types {
			errors = append(errors, validateTypeDescriptorPath(member, typeNames, typeArities, typeParameters, context+" union member "+strconv.Itoa(i), active)...)
		}
	case *TypeParameterDescriptor:
		if !typeParameters[d.ParamName] {
			errors = append(errors, &ValidationError{
				Code:    "undeclared_type_parameter",
				Message: context + " references undeclared type parameter: " + d.ParamName,
			})
		}
		if d.Constraint != nil {
			errors = append(errors, validateTypeDescriptorPath(d.Constraint, typeNames, typeArities, typeParameters, context+" constraint", active)...)
		}
	case *PrimitiveDescriptor:
	default:
		errors = append(errors, &ValidationError{
			Code:    "invalid_nested_type",
			Message: context + " contains a named declaration where a type expression is required",
		})
	}
	return errors
}

func validateTypeParameters(parameters []TypeParameterDescriptor, context string) (map[string]bool, []*ValidationError) {
	declared := make(map[string]bool, len(parameters))
	var errors []*ValidationError
	for _, parameter := range parameters {
		if parameter.ParamName == "" {
			errors = append(errors, &ValidationError{
				Code:    "empty_type_parameter",
				Message: context + " declares an empty type parameter",
			})
			continue
		}
		if declared[parameter.ParamName] {
			errors = append(errors, &ValidationError{
				Code:    "duplicate_type_parameter",
				Message: context + " declares duplicate type parameter: " + parameter.ParamName,
			})
			continue
		}
		declared[parameter.ParamName] = true
	}
	return declared, errors
}

// StringEncodingApplies reports whether encoding/json applies a field's
// json:",string" option to td. The exact defined-pointer behavior is probed
// because encoding/json v2 changed it while retaining the same public API.
func (s *Schema) StringEncodingApplies(td TypeDescriptor) bool {
	fieldPointer := false
	if ptr, ok := td.(*PtrDescriptor); ok {
		td = ptr.Element
		fieldPointer = true
	}

	visited := make(map[TypeDescriptor]bool)
	resolvedAlias := false
	for !isNilTypeDescriptor(td) && !visited[td] {
		visited[td] = true
		switch d := td.(type) {
		case *PrimitiveDescriptor:
			switch d.PrimitiveKind {
			case PrimitiveString, PrimitiveBool, PrimitiveInt, PrimitiveUint, PrimitiveFloat, PrimitiveDuration:
				return true
			}
			return false
		case *ReferenceDescriptor:
			td = s.FindType(d.Target)
		case *AliasDescriptor:
			resolvedAlias = true
			td = d.Underlying
		case *PtrDescriptor:
			if !resolvedAlias || (fieldPointer && !definedPointerStringEncoding.pointer) || (!fieldPointer && !definedPointerStringEncoding.direct) {
				return false
			}
			td = d.Element
			resolvedAlias = false
		case *EnumDescriptor:
			if len(d.Members) == 0 {
				return false
			}
			for _, member := range d.Members {
				if !isValidEnumValueType(member.Value) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	return false
}

// HasPointerOnlyAliasCycle reports whether resolving alias through only aliases
// and pointers reaches an unproductive cycle. Such a cycle has no concrete JSON
// value other than nil and cannot be represented as a finite TypeScript alias
// or Zod schema.
func (s *Schema) HasPointerOnlyAliasCycle(alias *AliasDescriptor) bool {
	if s == nil || alias == nil {
		return false
	}
	summaries := make(map[GoIdentifier]*pointerAliasSummary)
	return s.pointerAliasHead(alias, summaries).kind == pointerAliasHeadCycle
}

type pointerAliasHeadKind uint8

const (
	pointerAliasHeadProductive pointerAliasHeadKind = iota
	pointerAliasHeadParameter
	pointerAliasHeadCycle
)

type pointerAliasHeadResult struct {
	kind      pointerAliasHeadKind
	parameter int
}

type pointerAliasSummary struct {
	resolving bool
	resolved  bool
	result    pointerAliasHeadResult
}

func (s *Schema) pointerAliasHead(alias *AliasDescriptor, summaries map[GoIdentifier]*pointerAliasSummary) pointerAliasHeadResult {
	summary := summaries[alias.Name]
	if summary == nil {
		summary = &pointerAliasSummary{}
		summaries[alias.Name] = summary
	}
	if summary.resolved {
		return summary.result
	}
	if summary.resolving {
		return pointerAliasHeadResult{kind: pointerAliasHeadCycle}
	}

	summary.resolving = true
	summary.result = s.pointerAliasDescriptorHead(alias, alias.Underlying, summaries)
	summary.resolving = false
	summary.resolved = true
	return summary.result
}

func (s *Schema) pointerAliasDescriptorHead(owner *AliasDescriptor, td TypeDescriptor, summaries map[GoIdentifier]*pointerAliasSummary) pointerAliasHeadResult {
	switch t := td.(type) {
	case *PtrDescriptor:
		return s.pointerAliasDescriptorHead(owner, t.Element, summaries)
	case *TypeParameterDescriptor:
		for i := range owner.TypeParameters {
			if owner.TypeParameters[i].ParamName == t.ParamName {
				return pointerAliasHeadResult{kind: pointerAliasHeadParameter, parameter: i}
			}
		}
		return pointerAliasHeadResult{kind: pointerAliasHeadProductive}
	case *ReferenceDescriptor:
		target, ok := s.FindType(t.Target).(*AliasDescriptor)
		if !ok || len(target.TypeParameters) != len(t.TypeArguments) {
			return pointerAliasHeadResult{kind: pointerAliasHeadProductive}
		}
		result := s.pointerAliasHead(target, summaries)
		if result.kind != pointerAliasHeadParameter {
			return result
		}
		return s.pointerAliasDescriptorHead(owner, t.TypeArguments[result.parameter], summaries)
	default:
		return pointerAliasHeadResult{kind: pointerAliasHeadProductive}
	}
}

// isValidEnumValueType checks if a value is a valid enum member type.
// Per §4.5, providers must convert Go constant values to exactly one of:
// string, int64, or float64.
func isValidEnumValueType(value any) bool {
	if value == nil {
		return false
	}
	switch value.(type) {
	case string, int64, float64:
		return true
	}
	return false
}

// ValidationError represents a schema validation error.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// detectCircularInheritance checks for cycles in struct inheritance (Extends).
func (s *Schema) detectCircularInheritance() []*ValidationError {
	var errors []*ValidationError

	// Build a map of struct name -> struct descriptor
	structs := make(map[GoIdentifier]*StructDescriptor)
	for _, t := range s.Types {
		if sd, ok := t.(*StructDescriptor); ok && sd != nil {
			structs[sd.Name] = sd
		}
	}

	// DFS cycle detection
	visited := make(map[GoIdentifier]bool)
	inStack := make(map[GoIdentifier]bool)

	var detectCycle func(name GoIdentifier, path []string) bool
	detectCycle = func(name GoIdentifier, path []string) bool {
		if inStack[name] {
			// Cycle detected
			cyclePath := append(path, name.Name)
			errors = append(errors, &ValidationError{
				Code:    "circular_inheritance",
				Message: "circular inheritance detected: " + joinPath(cyclePath),
			})
			return true
		}
		if visited[name] {
			return false
		}

		visited[name] = true
		inStack[name] = true

		if sd, ok := structs[name]; ok {
			for _, ext := range sd.Extends {
				detectCycle(ext, append(path, name.Name))
			}
		}

		inStack[name] = false
		return false
	}

	for name := range structs {
		detectCycle(name, nil)
	}

	return errors
}

// joinPath joins path elements with " -> ".
func joinPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	result := path[0]
	for i := 1; i < len(path); i++ {
		result += " -> " + path[i]
	}
	return result
}
