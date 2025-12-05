package tygorgen

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

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

	// OptionalType controls how optional fields (Go pointers) are typed in TypeScript.
	// Supported values: "undefined" (T | undefined), "null" (T | null).
	// Default: "undefined"
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
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		reflectTypes = append(reflectTypes, rt)

		pkg := rt.PkgPath()
		name := rt.Name()
		if name == "" {
			continue
		}

		// Parse generic type arguments: Page[github.com/.../v1.User] -> extract v1.User
		if idx := strings.Index(name, "["); idx >= 0 {
			typeArgs := name[idx+1 : len(name)-1] // strip [ and ]
			baseName := name[:idx]

			// Add the base generic type
			addRoot(pkg, baseName)

			// Parse and add type arguments (handles multiple: K, V)
			for _, arg := range strings.Split(typeArgs, ",") {
				arg = strings.TrimSpace(arg)
				// Format: github.com/pkg/path.TypeName
				if lastDot := strings.LastIndex(arg, "."); lastDot >= 0 {
					argPkg := arg[:lastDot]
					argName := arg[lastDot+1:]
					addRoot(argPkg, argName)
				}
			}
		} else {
			// Non-generic type
			addRoot(pkg, name)
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
	var err error

	switch cfg.Provider {
	case "source":
		schema, err = buildSchemaFromSource(ctx, routes, cfg.Packages)
	case "reflection":
		schema, err = buildSchemaFromReflection(ctx, routes)
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

	// 2. Build service descriptors from routes
	services := buildServiceDescriptors(routes)
	schema.Services = services

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
		discoveryJSON, err := json.MarshalIndent(schema, "", "  ")
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

// buildServiceDescriptors converts route metadata to IR service descriptors.
func buildServiceDescriptors(routes internal.RouteMap) []ir.ServiceDescriptor {
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
			endpoint.Request = reflectTypeToIRRef(route.Request)
		}

		// Convert response type to descriptor
		if route.Response != nil {
			endpoint.Response = reflectTypeToIRRef(route.Response)
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

	return services
}

// reflectTypeToIRRef converts a reflect.Type to an IR TypeDescriptor reference.
// This handles the basic mapping from Go types to IR type expressions.
//
// For endpoint Request/Response types, pointers are stripped because they're
// a Go idiom for efficiency, not an indication of nullability. Clients always
// send valid request objects, and handlers return valid responses (or errors).
//
// For nested types (slice elements, map values), pointers ARE preserved because
// they indicate nullability in the JSON output (e.g., []*User can have null elements).
func reflectTypeToIRRef(t reflect.Type) ir.TypeDescriptor {
	if t == nil {
		return ir.Any()
	}

	// Strip top-level pointers - they're a Go idiom for endpoint types
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch {
	// Handle slices/arrays - preserve pointer semantics for elements
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		elem := reflectTypeToIRRefPreservePtr(t.Elem())
		if t.Kind() == reflect.Slice {
			return ir.Slice(elem)
		}
		return ir.Array(elem, t.Len())

	// Handle maps - preserve pointer semantics for values
	case t.Kind() == reflect.Map:
		key := reflectTypeToIRRefPreservePtr(t.Key())
		value := reflectTypeToIRRefPreservePtr(t.Elem())
		return ir.Map(key, value)

	// Handle empty struct (struct{})
	case t.Kind() == reflect.Struct && t.NumField() == 0:
		return ir.Empty()

	// For named types (structs, aliases), create a reference
	case t.Name() != "":
		// Sanitize generic type names to match what the reflection provider generates
		name := sanitizeTypeName(t.Name())
		return ir.Ref(name, t.PkgPath())

	// Fallback for primitives and unnamed types
	default:
		return ir.Any()
	}
}

// reflectTypeToIRRefPreservePtr converts a reflect.Type to an IR TypeDescriptor,
// preserving pointer semantics. Used for nested types (slice elements, map values)
// where pointers indicate nullability in JSON.
func reflectTypeToIRRefPreservePtr(t reflect.Type) ir.TypeDescriptor {
	if t == nil {
		return ir.Any()
	}

	// Track if we have pointer indirection (collapse multiple levels)
	isPointer := false
	for t.Kind() == reflect.Pointer {
		isPointer = true
		t = t.Elem()
	}

	// Get the base type descriptor
	var base ir.TypeDescriptor

	switch {
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		elem := reflectTypeToIRRefPreservePtr(t.Elem())
		if t.Kind() == reflect.Slice {
			base = ir.Slice(elem)
		} else {
			base = ir.Array(elem, t.Len())
		}

	case t.Kind() == reflect.Map:
		key := reflectTypeToIRRefPreservePtr(t.Key())
		value := reflectTypeToIRRefPreservePtr(t.Elem())
		base = ir.Map(key, value)

	case t.Kind() == reflect.Struct && t.NumField() == 0:
		base = ir.Empty()

	case t.Name() != "":
		name := sanitizeTypeName(t.Name())
		base = ir.Ref(name, t.PkgPath())

	default:
		base = ir.Any()
	}

	// Wrap in Ptr if the original type was a pointer
	if isPointer {
		return ir.Ptr(base)
	}
	return base
}

// isEmptyStructType checks if a reflect.Type is an empty struct (*struct{} or struct{}).
// Used to detect void request types per spec §4.8.
func isEmptyStructType(t reflect.Type) bool {
	// Unwrap pointers
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.NumField() == 0
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
		result.OptionalType = "undefined"
	}

	return &result
}

// buildSchemaFromSource uses the source provider to extract types.
func buildSchemaFromSource(ctx context.Context, routes internal.RouteMap, extraPackages []string) (*ir.Schema, error) {
	// Infer packages from route types
	packages := collectPackagesFromRoutes(routes)

	// Add any extra packages specified in config
	packages = append(packages, extraPackages...)

	if len(packages) == 0 {
		return nil, fmt.Errorf("no packages to analyze: register at least one handler or specify Packages in config")
	}

	// Collect root types from routes
	rootTypes := collectRootTypes(routes)

	p := &provider.SourceProvider{}
	opts := provider.SourceInputOptions{
		Packages:  packages,
		RootTypes: rootTypes,
	}
	return p.BuildSchema(ctx, opts)
}

// collectPackagesFromRoutes extracts unique package paths from route types.
func collectPackagesFromRoutes(routes internal.RouteMap) []string {
	seen := make(map[string]bool)
	var pkgs []string

	for _, route := range routes {
		if route.Request != nil {
			// Unwrap pointers - pointer types return empty PkgPath()
			t := route.Request
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			pkg := t.PkgPath()
			// "main" is returned by reflect for types in package main,
			// but packages.Load("main") looks in stdlib. Use "." instead.
			if pkg == "main" {
				pkg = "."
			}
			if pkg != "" && !seen[pkg] {
				seen[pkg] = true
				pkgs = append(pkgs, pkg)
			}
		}
		if route.Response != nil {
			// Unwrap pointers - pointer types return empty PkgPath()
			t := route.Response
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			pkg := t.PkgPath()
			if pkg == "main" {
				pkg = "."
			}
			if pkg != "" && !seen[pkg] {
				seen[pkg] = true
				pkgs = append(pkgs, pkg)
			}
		}
	}

	sort.Strings(pkgs)
	return pkgs
}

// buildSchemaFromReflection uses the reflection provider to extract types.
func buildSchemaFromReflection(ctx context.Context, routes internal.RouteMap) (*ir.Schema, error) {
	// Collect reflect.Types from routes
	rootTypes := make([]reflect.Type, 0, len(routes)*2)
	for _, route := range routes {
		if route.Request != nil {
			rootTypes = append(rootTypes, route.Request)
		}
		if route.Response != nil {
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
		// Unwrap pointers
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		pkg := t.PkgPath()
		name := t.Name()
		if name == "" {
			return
		}

		// Strip generic type parameters
		if idx := strings.Index(name, "["); idx >= 0 {
			name = name[:idx]
		}

		key := pkg + "." + name
		if !seen[key] {
			seen[key] = true
			roots = append(roots, provider.RootType{
				Name:    name,
				Package: pkg,
			})
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
