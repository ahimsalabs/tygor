package ir

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
		if t.TypeName() == name {
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
	var errors []*ValidationError

	// Build a set of type names from Schema.Types, checking for duplicates
	typeNames := make(map[GoIdentifier]bool)
	for _, t := range s.Types {
		name := t.TypeName()
		if !name.IsZero() {
			if typeNames[name] {
				errors = append(errors, &ValidationError{
					Code:    "duplicate_type",
					Message: "duplicate type name: " + name.Name + " (package: " + name.Package + ")",
				})
			}
			typeNames[name] = true
		}
	}

	// Validate struct fields and Extends references
	for _, t := range s.Types {
		if sd, ok := t.(*StructDescriptor); ok {
			for _, field := range sd.Fields {
				if field.StringEncoded && !isStringEncodableType(field.Type) {
					errors = append(errors, &ValidationError{
						Code:    "invalid_string_encoded",
						Message: "StringEncoded set on incompatible type for field " + sd.Name.Name + "." + field.Name + ": only string, integer, float, and boolean types support json:\",string\"",
					})
				}
			}
			// Validate Extends references exist and are structs
			for _, ext := range sd.Extends {
				if !typeNames[ext] {
					errors = append(errors, &ValidationError{
						Code:    "missing_extends_reference",
						Message: "struct " + sd.Name.Name + " extends unknown type: " + ext.Name,
					})
				} else {
					// Verify that the referenced type is actually a struct
					extType := s.FindType(ext)
					if extType != nil && extType.Kind() != KindStruct {
						errors = append(errors, &ValidationError{
							Code:    "extends_non_struct",
							Message: "struct " + sd.Name.Name + " extends non-struct type: " + ext.Name,
						})
					}
				}
			}
		}
	}

	// Validate enum member value types (must be string, int64, or float64 per §4.5)
	for _, t := range s.Types {
		if ed, ok := t.(*EnumDescriptor); ok {
			for _, member := range ed.Members {
				if !isValidEnumValueType(member.Value) {
					errors = append(errors, &ValidationError{
						Code:    "invalid_enum_value_type",
						Message: "enum " + ed.Name.Name + " member " + member.Name + " has invalid value type: only string, int64, and float64 are allowed",
					})
				}
			}
		}
	}

	// Check for circular inheritance
	if circularErrs := s.detectCircularInheritance(); len(circularErrs) > 0 {
		errors = append(errors, circularErrs...)
	}

	// Walk all Services and Endpoints
	for _, service := range s.Services {
		// Track endpoint names within this service for uniqueness check
		endpointNames := make(map[string]bool)

		for _, endpoint := range service.Endpoints {
			// Check Request type references resolve (if not nil)
			if endpoint.Request != nil {
				errors = append(errors, validateTypeReferences(endpoint.Request, typeNames, "endpoint "+endpoint.FullName+" Request")...)
			}

			// Check Response type references resolve
			if endpoint.Response != nil {
				errors = append(errors, validateTypeReferences(endpoint.Response, typeNames, "endpoint "+endpoint.FullName+" Response")...)
			}

			// Check FullName format: exactly one dot, matches ServiceName.EndpointName
			expectedFullName := service.Name + "." + endpoint.Name
			if endpoint.FullName != expectedFullName {
				errors = append(errors, &ValidationError{
					Code:    "invalid_fullname",
					Message: "endpoint FullName must be ServiceName.EndpointName: expected " + expectedFullName + ", got " + endpoint.FullName,
				})
			}

			// Check Path format: matches /ServiceName/EndpointName
			expectedPath := "/" + service.Name + "/" + endpoint.Name
			if endpoint.Path != expectedPath {
				errors = append(errors, &ValidationError{
					Code:    "invalid_path",
					Message: "endpoint Path must be /ServiceName/EndpointName: expected " + expectedPath + ", got " + endpoint.Path,
				})
			}

			// Check endpoint name uniqueness within service
			if endpointNames[endpoint.Name] {
				errors = append(errors, &ValidationError{
					Code:    "duplicate_endpoint",
					Message: "duplicate endpoint name in service " + service.Name + ": " + endpoint.Name,
				})
			}
			endpointNames[endpoint.Name] = true
		}
	}

	// Convert ValidationErrors to regular errors
	var result []error
	for _, e := range errors {
		result = append(result, e)
	}
	return result
}

// isStringEncodableType checks if a type supports json:",string" encoding.
// Per Go's encoding/json, only string, integer, floating-point, and boolean
// types can use the string option.
func isStringEncodableType(td TypeDescriptor) bool {
	if td == nil {
		return false
	}

	switch d := td.(type) {
	case *PrimitiveDescriptor:
		switch d.PrimitiveKind {
		case PrimitiveString, PrimitiveBool, PrimitiveInt, PrimitiveUint, PrimitiveFloat:
			return true
		}
	case *PtrDescriptor:
		// Pointers to string-encodable types are also valid
		return isStringEncodableType(d.Element)
	}
	return false
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

// validateTypeReferences recursively walks a TypeDescriptor and checks that all
// ReferenceDescriptors point to types that exist in typeNames.
func validateTypeReferences(td TypeDescriptor, typeNames map[GoIdentifier]bool, context string) []*ValidationError {
	if td == nil {
		return nil
	}

	var errors []*ValidationError

	switch d := td.(type) {
	case *ReferenceDescriptor:
		if !typeNames[d.Target] {
			errors = append(errors, &ValidationError{
				Code:    "missing_type_reference",
				Message: context + " references unknown type: " + d.Target.Name,
			})
		}
	case *ArrayDescriptor:
		errors = append(errors, validateTypeReferences(d.Element, typeNames, context)...)
	case *MapDescriptor:
		errors = append(errors, validateTypeReferences(d.Key, typeNames, context)...)
		errors = append(errors, validateTypeReferences(d.Value, typeNames, context)...)
	case *PtrDescriptor:
		errors = append(errors, validateTypeReferences(d.Element, typeNames, context)...)
	case *UnionDescriptor:
		// Check for empty unions
		if len(d.Types) == 0 {
			errors = append(errors, &ValidationError{
				Code:    "empty_union",
				Message: context + " contains union with no types",
			})
		}
		for _, t := range d.Types {
			errors = append(errors, validateTypeReferences(t, typeNames, context)...)
		}
	case *TypeParameterDescriptor:
		if d.Constraint != nil {
			errors = append(errors, validateTypeReferences(d.Constraint, typeNames, context)...)
		}
	case *PrimitiveDescriptor:
		// Primitives don't have references
	default:
		// Unknown descriptor type - skip
	}

	return errors
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
		if sd, ok := t.(*StructDescriptor); ok {
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
