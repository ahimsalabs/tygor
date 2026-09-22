package tygorgen

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"tygor.dev/internal"
	"tygor.dev/tygor"
	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/provider"
	"tygor.dev/tygorgen/sink"
	"tygor.dev/tygorgen/typescript"
)

// GenerateResult contains the output from code generation.
type GenerateResult struct {
	// Files contains generated file contents when OutDir is empty.
	// When OutDir is set, files are written to disk and this is nil.
	Files []GeneratedFile

	// Warnings contains non-fatal issues encountered during generation.
	Warnings []Warning

	// Schema is the validated IR schema (available for inspection/check mode).
	Schema *ir.Schema
}

// GeneratedFile represents a generated output file.
type GeneratedFile struct {
	Path    string
	Content []byte
}

// Warning represents a non-fatal issue encountered during generation.
type Warning struct {
	Code    string
	Message string
}

// Config holds the configuration for code generation.
type Config struct {
	// OutDir is the directory where generated files will be written.
	// e.g. "./client/src/rpc"
	OutDir string

	// Provider selects the type extraction strategy.
	// "source" (default) - uses go/packages for full type info including enums and comments
	// "reflection" - uses runtime reflection (faster, but no enum values or comments)
	Provider string

	// Packages are additional Go package paths to analyze when using source provider.
	// By default, packages are inferred from the types registered in routes.
	// Use this to include additional packages not directly referenced in endpoints.
	// e.g. []string{"github.com/myorg/myapp/shared"}
	Packages []string

	// TypeMappings allows overriding type mappings for tygo.
	// e.g. map[string]string{"time.Time": "Date", "CustomType": "string"}
	TypeMappings map[string]string

	// PreserveComments controls whether Go doc comments are preserved in TypeScript output.
	// Supported values: "default" (preserve package and type comments), "types" (only type comments), "none".
	// Default: "default"
	PreserveComments string

	// EnumStyle controls how Go const groups are generated in TypeScript.
	// Supported values: "union" (type unions), "enum" (TS enums), "const" (individual consts).
	// Default: "union"
	EnumStyle string

	// OptionalType overrides the wire-correct distinction between omitted and null fields.
	// Supported values: "default", "undefined", "null".
	// Default: "default" (omitempty controls omission; nil-capable values permit null).
	OptionalType string

	// Frontmatter is content added to the top of each generated TypeScript file.
	// Useful for custom type definitions or imports.
	// e.g. "export type DateTime = string & { __brand: 'DateTime' };"
	Frontmatter string

	// StripPackagePrefix removes this prefix from package paths when qualifying type names.
	// Use this when you have same-named types in different packages (e.g., v1.User and v2.User).
	// Example: "github.com/myorg/myrepo/" makes "github.com/myorg/myrepo/api/v1.User" → "v1_User"
	// Without this, types from different packages with the same name will collide.
	StripPackagePrefix string

	// SingleFile emits all types in a single types.ts file.
	// Default (false) generates one file per Go package with a barrel types.ts that re-exports all.
	SingleFile bool

	// Flavors lists which additional output flavors to generate.
	// Each enabled flavor produces its own output file alongside or instead of types.ts.
	// Use FlavorZod, FlavorZodMini constants or the ConfigBuilder.WithFlavor() method.
	// Example: []Flavor{FlavorZod} generates schemas.zod.ts with Zod schemas
	Flavors []Flavor

	// EmitTypes controls whether base types.ts is generated.
	// Default (nil/true): generate types.ts. Set to false to only emit flavor outputs.
	// When false with Zod flavor, types are exported via z.infer<typeof Schema>.
	EmitTypes *bool

	// EmitDiscovery controls whether discovery.json is generated.
	// When true, outputs a JSON file containing the full IR schema for runtime introspection.
	// This enables API browsers and tooling to introspect services and types.
	EmitDiscovery bool
}

// GenerateTypes generates TypeScript types from Go types without a tygor app.
// This is the standalone equivalent of Generate() for use with FromTypes().
// No manifest is generated since there are no RPC endpoints.
func GenerateTypes(types []any, cfg *Config) (*GenerateResult, error) {
	if len(types) == 0 {
		return nil, fmt.Errorf("no types provided: pass at least one type to FromTypes()")
	}

	// Apply defaults
	cfg = applyConfigDefaults(cfg)

	ctx := context.Background()

	// Extract packages and type names from the provided types
	var packages []string
	var rootTypes []provider.RootType
	var reflectTypes []reflect.Type
	seen := make(map[string]bool)
	seenRoots := make(map[string]bool) // key: "pkg.name"

	// Helper to add a root type if not already seen
	addRoot := func(pkg, name string) {
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			packages = append(packages, pkg)
		}
		if name == "" {
			return
		}
		key := pkg + "." + name
		if !seenRoots[key] {
			seenRoots[key] = true
			rootTypes = append(rootTypes, provider.RootType{
				Name:    name,
				Package: pkg,
			})
		}
	}

	for _, t := range types {
		rt := reflect.TypeOf(t)
		if rt == nil {
			continue
		}
		// Unwrap pointers
		for rt.Kind() == reflect.Pointer && rt.Name() == "" {
			rt = rt.Elem()
		}
		reflectTypes = append(reflectTypes, rt)

		for _, id := range reflectedTypeReferences(reflectTypeToIRPreservePtr(rt, false)) {
			addRoot(id.Package, id.Name)
		}
	}

	// Add any extra packages specified in config
	packages = append(packages, cfg.Packages...)

	// Build schema using configured provider
	var schema *ir.Schema
	var err error

	switch cfg.Provider {
	case "source":
		if len(packages) == 0 {
			return nil, fmt.Errorf("no packages found: ensure types are from named packages")
		}
		p := &provider.SourceProvider{}
		opts := provider.SourceInputOptions{
			Packages:  packages,
			RootTypes: rootTypes,
		}
		schema, err = p.BuildSchema(ctx, opts)
	case "reflection":
		p := &provider.ReflectionProvider{}
		opts := provider.ReflectionInputOptions{
			RootTypes: reflectTypes,
		}
		schema, err = p.BuildSchema(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown provider: %q (expected \"source\" or \"reflection\")", cfg.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build schema: %w", err)
	}

	// Collect warnings
	var warnings []Warning
	for _, w := range schema.Warnings {
		warnings = append(warnings, Warning{Code: w.Code, Message: w.Message})
	}

	// No services for standalone type generation
	schema.Services = nil

	// Validate schema
	if errs := schema.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("schema validation failed: %w", errs[0])
	}

	// Configure TypeScript generator
	tsConfig := typescript.GeneratorConfig{
		TypePrefix:         "",
		TypeSuffix:         "",
		FieldCase:          "preserve",
		TypeCase:           "preserve",
		StripPackagePrefix: cfg.StripPackagePrefix,
		SingleFile:         cfg.SingleFile,
		IndentStyle:        "space",
		IndentSize:         2,
		LineEnding:         "lf",
		TrailingNewline:    true,
		EmitComments:       cfg.PreserveComments != "none",
		Frontmatter:        cfg.Frontmatter,
		TypeMappings:       cfg.TypeMappings,
		Custom: map[string]any{
			"EmitExport":        true,
			"EmitDeclare":       false,
			"UseInterface":      true,
			"UseReadonlyArrays": false,
			"EnumStyle":         cfg.EnumStyle,
			"OptionalType":      cfg.OptionalType,
			"UnknownType":       "unknown",
			"Flavors":           flavorsToStrings(cfg.Flavors),
			"EmitTypes":         cfg.EmitTypes,
		},
	}

	// Create sink (filesystem if OutDir set, memory otherwise)
	var outputSink sink.OutputSink
	var memorySink *sink.MemorySink
	if cfg.OutDir != "" {
		outputSink = sink.NewFilesystemSink(cfg.OutDir)
	} else {
		memorySink = sink.NewMemorySink()
		outputSink = memorySink
	}

	// Generate TypeScript
	gen := &typescript.TypeScriptGenerator{}
	tsResult, err := gen.Generate(ctx, schema, typescript.GenerateOptions{
		Sink:   outputSink,
		Config: tsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TypeScript: %w", err)
	}

	// Collect generator warnings
	for _, w := range tsResult.Warnings {
		warnings = append(warnings, Warning{Code: w.Code, Message: w.Message})
	}

	// Build result
	result := &GenerateResult{
		Warnings: warnings,
		Schema:   schema,
	}

	// Include files if using memory sink
	if memorySink != nil {
		for path, content := range memorySink.Files() {
			result.Files = append(result.Files, GeneratedFile{
				Path:    path,
				Content: content,
			})
		}
	}

	return result, nil
}

// Generate generates the TypeScript types and manifest for the registered services.
// If OutDir is set, files are written to disk. Otherwise, files are returned in the result.
func Generate(app *tygor.App, cfg *Config) (*GenerateResult, error) {
	routes := app.Routes()

	// Apply defaults
	cfg = applyConfigDefaults(cfg)

	ctx := context.Background()

	// 1. Build schema using configured provider
	var schema *ir.Schema
	var convertEndpointType endpointTypeConverter
	var err error

	switch cfg.Provider {
	case "source":
		schema, convertEndpointType, err = buildSchemaFromSource(ctx, routes, cfg.Packages)
	case "reflection":
		schema, err = buildSchemaFromReflection(ctx, routes)
		convertEndpointType = func(t reflect.Type, preserveTopPointer bool) (ir.TypeDescriptor, error) {
			return reflectTypeToIRRef(t, preserveTopPointer, true), nil
		}
	default:
		return nil, fmt.Errorf("unknown provider: %q (expected \"source\" or \"reflection\")", cfg.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build schema: %w", err)
	}

	// 2. Build service descriptors from routes
	services, err := buildServiceDescriptors(routes, convertEndpointType)
	if err != nil {
		return nil, err
	}
	schema.Services = services

	// Collect warnings after endpoint conversion because source-backed concrete
	// generic arguments can discover custom marshalers while resolving services.
	var warnings []Warning
	for _, w := range schema.Warnings {
		warnings = append(warnings, Warning{Code: w.Code, Message: w.Message})
	}

	// 3. Validate schema
	if errs := schema.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("schema validation failed: %w", errs[0])
	}

	// 4. Configure TypeScript generator
	tsConfig := typescript.GeneratorConfig{
		TypePrefix:         "",
		TypeSuffix:         "",
		FieldCase:          "preserve",
		TypeCase:           "preserve",
		StripPackagePrefix: cfg.StripPackagePrefix,
		SingleFile:         cfg.SingleFile,
		IndentStyle:        "space",
		IndentSize:         2,
		LineEnding:         "lf",
		TrailingNewline:    true,
		EmitComments:       cfg.PreserveComments != "none",
		Frontmatter:        cfg.Frontmatter,
		TypeMappings:       cfg.TypeMappings,
		Custom: map[string]any{
			"EmitExport":        true,
			"EmitDeclare":       false,
			"UseInterface":      true,
			"UseReadonlyArrays": false,
			"EnumStyle":         cfg.EnumStyle,
			"OptionalType":      cfg.OptionalType,
			"UnknownType":       "unknown",
			"Flavors":           flavorsToStrings(cfg.Flavors),
			"EmitTypes":         cfg.EmitTypes,
		},
	}

	// 5. Create sink (filesystem if OutDir set, memory otherwise)
	var outputSink sink.OutputSink
	var memorySink *sink.MemorySink
	if cfg.OutDir != "" {
		outputSink = sink.NewFilesystemSink(cfg.OutDir)
	} else {
		memorySink = sink.NewMemorySink()
		outputSink = memorySink
	}

	// 6. Generate TypeScript
	gen := &typescript.TypeScriptGenerator{}
	tsResult, err := gen.Generate(ctx, schema, typescript.GenerateOptions{
		Sink:   outputSink,
		Config: tsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TypeScript: %w", err)
	}

	// Collect generator warnings
	for _, w := range tsResult.Warnings {
		warnings = append(warnings, Warning{Code: w.Code, Message: w.Message})
	}

	// 7. Generate discovery.json if enabled
	if cfg.EmitDiscovery {
		discoveryJSON, err := json.Marshal(schema, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return nil, fmt.Errorf("failed to marshal discovery schema: %w", err)
		}
		if err := outputSink.WriteFile(ctx, "discovery.json", discoveryJSON); err != nil {
			return nil, fmt.Errorf("failed to write discovery.json: %w", err)
		}
	}

	// Build result
	result := &GenerateResult{
		Warnings: warnings,
		Schema:   schema,
	}

	// Include files if using memory sink
	if memorySink != nil {
		for path, content := range memorySink.Files() {
			result.Files = append(result.Files, GeneratedFile{
				Path:    path,
				Content: content,
			})
		}
	}

	return result, nil
}

type endpointTypeConverter func(t reflect.Type, preserveTopPointer bool) (ir.TypeDescriptor, error)

// buildServiceDescriptors converts route metadata to IR service descriptors.
func buildServiceDescriptors(routes internal.RouteMap, convert endpointTypeConverter) ([]ir.ServiceDescriptor, error) {
	// Group routes by service
	serviceMap := make(map[string]*ir.ServiceDescriptor)

	// Sort route keys for deterministic output
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		route := routes[key]

		// Parse service and method name from key (e.g., "Users.Create")
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			continue // Skip malformed keys
		}
		serviceName, methodName := parts[0], parts[1]

		// Get or create service
		service, exists := serviceMap[serviceName]
		if !exists {
			service = &ir.ServiceDescriptor{
				Name:      serviceName,
				Endpoints: []ir.EndpointDescriptor{},
			}
			serviceMap[serviceName] = service
		}

		// Build endpoint descriptor
		// Note: key is typically "Service.Method" (one dot) from the registry,
		// but we replace all dots defensively in case of nested service names.
		endpoint := ir.EndpointDescriptor{
			Name:      methodName,
			FullName:  key,
			Primitive: route.Primitive,
			Path:      "/" + strings.ReplaceAll(key, ".", "/"),
		}

		// Convert request type to descriptor
		// Per spec §4.8: void requests use Request: nil
		if route.Request != nil && !isEmptyStructType(route.Request) {
			request, err := convert(route.Request, false)
			if err != nil {
				return nil, fmt.Errorf("convert request type for endpoint %s: %w", key, err)
			}
			endpoint.Request = request
		}

		// Convert response type to descriptor
		if route.Response != nil {
			response, err := convert(route.Response, true)
			if err != nil {
				return nil, fmt.Errorf("convert response type for endpoint %s: %w", key, err)
			}
			endpoint.Response = response
		} else {
			// No response type means void/empty
			endpoint.Response = ir.Ptr(ir.Empty())
		}

		service.Endpoints = append(service.Endpoints, endpoint)
	}

	// Convert map to sorted slice
	serviceNames := make([]string, 0, len(serviceMap))
	for name := range serviceMap {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	services := make([]ir.ServiceDescriptor, 0, len(serviceMap))
	for _, name := range serviceNames {
		services = append(services, *serviceMap[name])
	}

	return services, nil
}

// reflectTypeToIRRef converts a reflected endpoint type to its wire descriptor.
// Response pointers are preserved because a typed nil response encodes as null.
// The reflection provider emits monomorphized generic names; the source provider
// emits generic definitions plus applied ReferenceDescriptor arguments.
func reflectTypeToIRRef(t reflect.Type, preserveTopPointer, monomorphizeGenerics bool) ir.TypeDescriptor {
	if t == nil {
		return ir.Any()
	}

	isPointer := false
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		isPointer = true
		t = t.Elem()
	}
	base := reflectTypeToIR(t, monomorphizeGenerics)
	if preserveTopPointer && isPointer {
		return ir.Ptr(base)
	}
	return base
}

func sourceTypeToIRRef(t reflect.Type, preserveTopPointer bool, resolve provider.TypeExpressionResolver) (ir.TypeDescriptor, error) {
	if t == nil {
		return ir.Any(), nil
	}

	isPointer := false
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		isPointer = true
		t = t.Elem()
	}
	base, err := sourceTypeToIR(t, resolve)
	if err != nil {
		return nil, err
	}
	if preserveTopPointer && isPointer {
		return ir.Ptr(base), nil
	}
	return base, nil
}

func sourceTypeToIR(t reflect.Type, resolve provider.TypeExpressionResolver) (ir.TypeDescriptor, error) {
	if t == nil {
		return ir.Any(), nil
	}

	switch {
	case t == reflect.TypeFor[time.Time]():
		return ir.Time(), nil
	case t == reflect.TypeFor[time.Duration]():
		return nil, fmt.Errorf("time.Duration has no default encoding/json/v2 representation")
	case t == reflect.TypeFor[jsonv1.Number]():
		return ir.Float(64), nil
	case t == reflect.TypeFor[jsonv1.RawMessage]():
		return ir.Any(), nil
	case t.Name() != "" && t.PkgPath() != "":
		if resolve == nil {
			return nil, fmt.Errorf("source type resolver is unavailable for %s", t)
		}
		return resolve(t.PkgPath()+"."+t.Name(), t.PkgPath())
	case isReflectedJSONByteSlice(t):
		return ir.Bytes(), nil
	case isReflectedJSONByteArray(t):
		return ir.ByteArray(t.Len()), nil
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		element, err := sourceTypeToIRPreservePtr(t.Elem(), resolve)
		if err != nil {
			return nil, err
		}
		if t.Kind() == reflect.Slice {
			return ir.Slice(element), nil
		}
		return ir.Array(element, t.Len()), nil
	case t.Kind() == reflect.Map:
		var key ir.TypeDescriptor
		var err error
		if t.Key() == reflect.TypeFor[jsonv1.Number]() {
			key = ir.String()
		} else {
			key, err = sourceTypeToIRPreservePtr(t.Key(), resolve)
			if err != nil {
				return nil, err
			}
		}
		value, err := sourceTypeToIRPreservePtr(t.Elem(), resolve)
		if err != nil {
			return nil, err
		}
		return ir.Map(key, value), nil
	case t.Kind() == reflect.Struct && t.NumField() == 0 && t.Name() == "":
		return ir.Empty(), nil
	case t.PkgPath() == "":
		return reflectedPrimitive(t), nil
	default:
		return ir.Any(), nil
	}
}

func sourceTypeToIRPreservePtr(t reflect.Type, resolve provider.TypeExpressionResolver) (ir.TypeDescriptor, error) {
	isPointer := false
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		isPointer = true
		t = t.Elem()
	}
	base, err := sourceTypeToIR(t, resolve)
	if err != nil {
		return nil, err
	}
	if isPointer {
		return ir.Ptr(base), nil
	}
	return base, nil
}

func reflectTypeToIR(t reflect.Type, monomorphizeGenerics bool) ir.TypeDescriptor {
	if t == nil {
		return ir.Any()
	}

	switch {
	case t == reflect.TypeFor[time.Time]():
		return ir.Time()
	case t == reflect.TypeFor[time.Duration]():
		return ir.Duration()
	case t == reflect.TypeFor[jsonv1.Number]():
		return ir.Float(64)
	case t == reflect.TypeFor[jsonv1.RawMessage]():
		return ir.Any()
	case t.Name() != "" && t.PkgPath() != "":
		if strings.Contains(t.Name(), "[") && !monomorphizeGenerics {
			return parseReflectedTypeExpression(t.Name(), t.PkgPath())
		}
		return ir.Ref(sanitizeTypeName(t.Name()), t.PkgPath())
	case isReflectedJSONByteSlice(t):
		return ir.Bytes()
	case isReflectedJSONByteArray(t):
		return ir.ByteArray(t.Len())
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		elem := reflectTypeToIRPreservePtr(t.Elem(), monomorphizeGenerics)
		if t.Kind() == reflect.Slice {
			return ir.Slice(elem)
		}
		return ir.Array(elem, t.Len())
	case t.Kind() == reflect.Map:
		key := reflectMapKeyToIR(t.Key(), monomorphizeGenerics)
		value := reflectTypeToIRPreservePtr(t.Elem(), monomorphizeGenerics)
		return ir.Map(key, value)
	case t.Kind() == reflect.Struct && t.NumField() == 0 && t.Name() == "":
		return ir.Empty()
	case t.PkgPath() == "":
		return reflectedPrimitive(t)
	default:
		return ir.Any()
	}
}

func isReflectedJSONByteSlice(t reflect.Type) bool {
	return t.Kind() == reflect.Slice && t.Elem() == reflect.TypeFor[byte]()
}

func isReflectedJSONByteArray(t reflect.Type) bool {
	return t.Kind() == reflect.Array && t.Elem() == reflect.TypeFor[byte]()
}

func reflectMapKeyToIR(t reflect.Type, monomorphizeGenerics bool) ir.TypeDescriptor {
	if t == reflect.TypeFor[jsonv1.Number]() {
		return ir.String()
	}
	return reflectTypeToIRPreservePtr(t, monomorphizeGenerics)
}

func reflectTypeToIRPreservePtr(t reflect.Type, monomorphizeGenerics bool) ir.TypeDescriptor {
	if t == nil {
		return ir.Any()
	}

	// Track if we have pointer indirection (collapse multiple levels)
	isPointer := false
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		isPointer = true
		t = t.Elem()
	}

	base := reflectTypeToIR(t, monomorphizeGenerics)

	// Wrap in Ptr if the original type was a pointer
	if isPointer {
		return ir.Ptr(base)
	}
	return base
}

func reflectedPrimitive(t reflect.Type) ir.TypeDescriptor {
	switch t.Kind() {
	case reflect.Bool:
		return ir.Bool()
	case reflect.String:
		return ir.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return ir.Int(t.Bits())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return ir.Uint(t.Bits())
	case reflect.Float32, reflect.Float64:
		return ir.Float(t.Bits())
	case reflect.Interface:
		return ir.Any()
	default:
		return ir.Any()
	}
}

func parseReflectedTypeExpression(expr, currentPackage string) ir.TypeDescriptor {
	expr = strings.TrimSpace(expr)
	if expr == "[]byte" || expr == "[]uint8" {
		return ir.Bytes()
	}
	if strings.HasPrefix(expr, "*") {
		return ir.Ptr(parseReflectedTypeExpression(expr[1:], currentPackage))
	}
	if strings.HasPrefix(expr, "[]") {
		return ir.Slice(parseReflectedTypeExpression(expr[2:], currentPackage))
	}
	if strings.HasPrefix(expr, "map[") {
		if end := matchingBracket(expr, 3); end > 0 {
			keyExpr := expr[4:end]
			var key ir.TypeDescriptor
			if keyExpr == "encoding/json.Number" {
				key = ir.String()
			} else {
				key = parseReflectedTypeExpression(keyExpr, currentPackage)
			}
			return ir.Map(
				key,
				parseReflectedTypeExpression(expr[end+1:], currentPackage),
			)
		}
	}
	if strings.HasPrefix(expr, "[") {
		if end := strings.IndexByte(expr, ']'); end > 1 {
			var length int
			if _, err := fmt.Sscanf(expr[1:end], "%d", &length); err == nil {
				return ir.Array(parseReflectedTypeExpression(expr[end+1:], currentPackage), length)
			}
		}
	}
	if primitive := reflectedPrimitiveName(expr); primitive != nil {
		return primitive
	}

	base, args := splitGenericExpression(expr)
	pkg, name := splitQualifiedType(base, currentPackage)
	if len(args) == 0 {
		return ir.Ref(name, pkg)
	}
	typeArgs := make([]ir.TypeDescriptor, len(args))
	for i, arg := range args {
		typeArgs[i] = parseReflectedTypeExpression(arg, currentPackage)
	}
	return ir.RefWithArgs(name, pkg, typeArgs...)
}

func reflectedPrimitiveName(name string) ir.TypeDescriptor {
	switch name {
	case "bool":
		return ir.Bool()
	case "string":
		return ir.String()
	case "int":
		return ir.Int(0)
	case "int8":
		return ir.Int(8)
	case "int16":
		return ir.Int(16)
	case "int32", "rune":
		return ir.Int(32)
	case "int64":
		return ir.Int(64)
	case "uint":
		return ir.Uint(0)
	case "uint8", "byte":
		return ir.Uint(8)
	case "uint16":
		return ir.Uint(16)
	case "uint32":
		return ir.Uint(32)
	case "uint64":
		return ir.Uint(64)
	case "uintptr":
		return ir.Uint(0)
	case "float32":
		return ir.Float(32)
	case "float64":
		return ir.Float(64)
	case "time.Time":
		return ir.Time()
	case "time.Duration":
		return ir.Duration()
	case "encoding/json.Number":
		return ir.Float(64)
	case "any", "interface {}", "interface{}":
		return ir.Any()
	default:
		return nil
	}
}

func matchingBracket(value string, open int) int {
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitGenericExpression(value string) (string, []string) {
	open := strings.IndexByte(value, '[')
	if open < 0 || !strings.HasSuffix(value, "]") {
		return value, nil
	}
	body := value[open+1 : len(value)-1]
	var args []string
	start, depth := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(body[start:]))
	return value[:open], args
}

func splitQualifiedType(value, currentPackage string) (string, string) {
	if dot := strings.LastIndexByte(value, '.'); dot >= 0 {
		return value[:dot], value[dot+1:]
	}
	return currentPackage, value
}

// isEmptyStructType reports whether t has an empty struct representation.
// Named empty request types are still declarations, but are void requests per spec §4.8.
func isEmptyStructType(t reflect.Type) bool {
	// Unwrap pointers
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.NumField() == 0
}

func isUnnamedEmptyStructType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.NumField() == 0 && t.Name() == ""
}

// sanitizeTypeName applies the synthetic naming algorithm for generic instantiations.
// This must match the logic in provider/reflection.go generateSyntheticName.
func sanitizeTypeName(name string) string {
	result := strings.ReplaceAll(name, ".", "_")
	result = strings.ReplaceAll(result, "/", "_")
	result = strings.ReplaceAll(result, "[", "_")
	result = strings.ReplaceAll(result, "]", "")
	result = strings.ReplaceAll(result, ",", "_")
	result = strings.ReplaceAll(result, " ", "")
	result = strings.ReplaceAll(result, "*", "Ptr")
	return result
}

// applyConfigDefaults applies default values to Config.
func applyConfigDefaults(cfg *Config) *Config {
	// Make a copy to avoid mutating the input
	result := *cfg

	// Deep copy maps and slices to avoid shared references
	if result.TypeMappings != nil {
		copied := make(map[string]string, len(result.TypeMappings))
		for k, v := range result.TypeMappings {
			copied[k] = v
		}
		result.TypeMappings = copied
	}

	if result.Packages != nil {
		result.Packages = append([]string(nil), result.Packages...)
	}

	if result.Flavors != nil {
		result.Flavors = append([]Flavor(nil), result.Flavors...)
	}

	if result.Provider == "" {
		result.Provider = "source"
	}
	if result.PreserveComments == "" {
		result.PreserveComments = "default"
	}
	if result.EnumStyle == "" {
		result.EnumStyle = "union"
	}
	if result.OptionalType == "" {
		result.OptionalType = "default"
	}

	return &result
}

// buildSchemaFromSource uses the source provider to extract types.
func buildSchemaFromSource(ctx context.Context, routes internal.RouteMap, extraPackages []string) (*ir.Schema, endpointTypeConverter, error) {
	// Infer packages from route types
	packages := collectPackagesFromRoutes(routes)

	// Add any extra packages specified in config
	packages = append(packages, extraPackages...)

	if len(packages) == 0 {
		convert := func(t reflect.Type, preserveTopPointer bool) (ir.TypeDescriptor, error) {
			return sourceTypeToIRRef(t, preserveTopPointer, nil)
		}
		return &ir.Schema{Types: []ir.TypeDescriptor{}}, convert, nil
	}

	// Collect root types from routes
	rootTypes := collectRootTypes(routes)

	p := &provider.SourceProvider{}
	opts := provider.SourceInputOptions{
		Packages:  packages,
		RootTypes: rootTypes,
	}
	schema, resolve, err := p.BuildSchemaWithResolver(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	convert := func(t reflect.Type, preserveTopPointer bool) (ir.TypeDescriptor, error) {
		return sourceTypeToIRRef(t, preserveTopPointer, resolve)
	}
	return schema, convert, nil
}

// collectPackagesFromRoutes extracts unique package paths from route types.
func collectPackagesFromRoutes(routes internal.RouteMap) []string {
	seen := make(map[string]bool)
	var pkgs []string

	for _, route := range routes {
		for _, typ := range []reflect.Type{route.Request, route.Response} {
			if typ == nil {
				continue
			}
			for _, id := range reflectedTypeReferences(reflectTypeToIRPreservePtr(typ, false)) {
				pkg := id.Package
				if pkg == "main" {
					pkg = "."
				}
				if pkg != "" && !seen[pkg] {
					seen[pkg] = true
					pkgs = append(pkgs, pkg)
				}
			}
			for _, pkg := range reflectedTypePackages(typ) {
				if pkg == "main" {
					pkg = "."
				}
				if pkg != "" && !seen[pkg] {
					seen[pkg] = true
					pkgs = append(pkgs, pkg)
				}
			}
		}
	}

	sort.Strings(pkgs)
	return pkgs
}

func reflectedTypePackages(t reflect.Type) []string {
	seen := make(map[string]bool)
	var packages []string
	add := func(pkg string) {
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			packages = append(packages, pkg)
		}
	}
	var collectExpression func(string, string)
	collectExpression = func(expression, currentPackage string) {
		expression = strings.TrimSpace(expression)
		if strings.HasPrefix(expression, "*") {
			collectExpression(expression[1:], currentPackage)
			return
		}
		if strings.HasPrefix(expression, "[]") {
			collectExpression(expression[2:], currentPackage)
			return
		}
		if strings.HasPrefix(expression, "map[") {
			if end := matchingBracket(expression, 3); end > 0 {
				collectExpression(expression[4:end], currentPackage)
				collectExpression(expression[end+1:], currentPackage)
			}
			return
		}
		if strings.HasPrefix(expression, "[") {
			if end := strings.IndexByte(expression, ']'); end > 0 {
				collectExpression(expression[end+1:], currentPackage)
			}
			return
		}
		base, arguments := splitGenericExpression(expression)
		pkg, _ := splitQualifiedType(base, currentPackage)
		add(pkg)
		for _, argument := range arguments {
			collectExpression(argument, currentPackage)
		}
	}
	var collect func(reflect.Type)
	collect = func(current reflect.Type) {
		for current.Kind() == reflect.Pointer && current.Name() == "" {
			current = current.Elem()
		}
		if current.Name() != "" && current.PkgPath() != "" {
			if strings.Contains(current.Name(), "[") {
				collectExpression(current.Name(), current.PkgPath())
			}
			return
		}
		switch current.Kind() {
		case reflect.Slice, reflect.Array:
			if isReflectedJSONByteSlice(current) {
				return
			}
			collect(current.Elem())
		case reflect.Map:
			collect(current.Key())
			collect(current.Elem())
		}
	}
	collect(t)
	sort.Strings(packages)
	return packages
}

// buildSchemaFromReflection uses the reflection provider to extract types.
func buildSchemaFromReflection(ctx context.Context, routes internal.RouteMap) (*ir.Schema, error) {
	// Collect reflect.Types from routes
	rootTypes := make([]reflect.Type, 0, len(routes)*2)
	for _, route := range routes {
		if route.Request != nil && !isUnnamedEmptyStructType(route.Request) {
			rootTypes = append(rootTypes, route.Request)
		}
		if route.Response != nil && !isUnnamedEmptyStructType(route.Response) {
			rootTypes = append(rootTypes, route.Response)
		}
	}

	if len(rootTypes) == 0 {
		// Empty schema for apps with no handlers
		return &ir.Schema{
			Types:    []ir.TypeDescriptor{},
			Services: []ir.ServiceDescriptor{},
		}, nil
	}

	p := &provider.ReflectionProvider{}
	opts := provider.ReflectionInputOptions{
		RootTypes: rootTypes,
	}
	return p.BuildSchema(ctx, opts)
}

// collectRootTypes extracts types from routes for source provider.
func collectRootTypes(routes internal.RouteMap) []provider.RootType {
	seen := make(map[string]bool) // key: "pkg.name"
	var roots []provider.RootType

	addType := func(t reflect.Type) {
		for _, id := range reflectedTypeReferences(reflectTypeToIRPreservePtr(t, false)) {
			key := id.Package + "." + id.Name
			if !seen[key] {
				seen[key] = true
				roots = append(roots, provider.RootType{Name: id.Name, Package: id.Package})
			}
		}
	}

	for _, route := range routes {
		if route.Request != nil {
			addType(route.Request)
		}
		if route.Response != nil {
			addType(route.Response)
		}
	}

	return roots
}

func reflectedTypeReferences(typ ir.TypeDescriptor) []ir.GoIdentifier {
	seen := make(map[ir.GoIdentifier]bool)
	var walk func(ir.TypeDescriptor)
	walk = func(current ir.TypeDescriptor) {
		switch t := current.(type) {
		case *ir.ReferenceDescriptor:
			seen[t.Target] = true
			for _, arg := range t.TypeArguments {
				walk(arg)
			}
		case *ir.ArrayDescriptor:
			walk(t.Element)
		case *ir.MapDescriptor:
			walk(t.Key)
			walk(t.Value)
		case *ir.PtrDescriptor:
			walk(t.Element)
		case *ir.UnionDescriptor:
			for _, member := range t.Types {
				walk(member)
			}
		}
	}
	walk(typ)

	refs := make([]ir.GoIdentifier, 0, len(seen))
	for id := range seen {
		refs = append(refs, id)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Package == refs[j].Package {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Package < refs[j].Package
	})
	return refs
}

// flavorsToStrings converts []Flavor to []string for internal use.
func flavorsToStrings(flavors []Flavor) []string {
	if flavors == nil {
		return nil
	}
	result := make([]string, len(flavors))
	for i, f := range flavors {
		result[i] = string(f)
	}
	return result
}
