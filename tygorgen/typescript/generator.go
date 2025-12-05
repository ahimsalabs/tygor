// Package typescript generates TypeScript type definitions from IR schemas.
// It implements the Generator Interface Specification (§5-7).
package typescript

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"tygor.dev/tygorgen/ir"
	"tygor.dev/tygorgen/typescript/flavor"
)

// Generator transforms IR schemas into TypeScript source code.
type TypeScriptGenerator struct{}

// Ensure TypeScriptGenerator implements the Generator interface.
var _ Generator = (*TypeScriptGenerator)(nil)

// Name returns "typescript".
func (g *TypeScriptGenerator) Name() string {
	return "typescript"
}

// Generate produces TypeScript source code for the given schema.
func (g *TypeScriptGenerator) Generate(ctx context.Context, schema *ir.Schema, opts GenerateOptions) (*GenerateResult, error) {
	if err := validateOptions(opts); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// Apply defaults to TypeScript config
	tsConfig := getTypeScriptConfig(opts.Config)
	flavorCfg := getFlavorConfig(opts.Config)

	result := &GenerateResult{
		Files:          []OutputFile{},
		TypesGenerated: 0,
		Warnings:       append([]ir.Warning{}, schema.Warnings...),
	}

	// Generate base types unless EmitTypes is explicitly false
	if flavorCfg.EmitTypes {
		if opts.Config.SingleFile {
			// Single file mode: all types in one types.ts
			typesContent, typesGenerated, warnings, err := g.generateTypes(ctx, schema, opts.Config, tsConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to generate types: %w", err)
			}

			result.TypesGenerated = typesGenerated
			result.Warnings = append(result.Warnings, warnings...)

			typesPath := "types.ts"
			if err := opts.Sink.WriteFile(ctx, typesPath, typesContent); err != nil {
				return nil, fmt.Errorf("failed to write %s: %w", typesPath, err)
			}
			result.Files = append(result.Files, OutputFile{
				Path: typesPath,
				Size: int64(len(typesContent)),
			})
		} else {
			// Multi-file mode: one file per package + barrel
			files, typesGenerated, warnings, err := g.generateMultiFileTypes(ctx, schema, opts.Config, tsConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to generate types: %w", err)
			}

			result.TypesGenerated = typesGenerated
			result.Warnings = append(result.Warnings, warnings...)

			for _, f := range files {
				if err := opts.Sink.WriteFile(ctx, f.Path, f.Content); err != nil {
					return nil, fmt.Errorf("failed to write %s: %w", f.Path, err)
				}
				result.Files = append(result.Files, OutputFile{
					Path: f.Path,
					Size: int64(len(f.Content)),
				})
			}
		}
	}

	// Generate flavor outputs
	if len(flavorCfg.Flavors) > 0 {
		flavorFiles, err := g.generateFlavors(ctx, schema, opts.Config, flavorCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to generate flavors: %w", err)
		}

		for _, f := range flavorFiles {
			if err := opts.Sink.WriteFile(ctx, f.Path, f.Content); err != nil {
				return nil, fmt.Errorf("failed to write %s: %w", f.Path, err)
			}
			result.Files = append(result.Files, OutputFile{
				Path: f.Path,
				Size: int64(len(f.Content)),
			})
			// Add flavor warnings
			for _, w := range f.Warnings {
				result.Warnings = append(result.Warnings, ir.Warning{
					Code:    "UNSUPPORTED_VALIDATOR",
					Message: w,
				})
			}
		}
	}

	// Generate schema map if zod flavor is enabled
	for _, flavorName := range flavorCfg.Flavors {
		if flavorName == "zod" || flavorName == "zod-mini" {
			schemaMapContent, err := g.generateSchemaMap(ctx, schema, opts.Config, flavorName)
			if err != nil {
				return nil, fmt.Errorf("failed to generate schema map: %w", err)
			}

			// Each flavor gets its own map file
			schemaMapPath := fmt.Sprintf("schemas.map.%s.ts", flavorName)
			if err := opts.Sink.WriteFile(ctx, schemaMapPath, schemaMapContent); err != nil {
				return nil, fmt.Errorf("failed to write %s: %w", schemaMapPath, err)
			}
			result.Files = append(result.Files, OutputFile{
				Path: schemaMapPath,
				Size: int64(len(schemaMapContent)),
			})
		}
	}

	// Generate manifest.ts only when using app mode (services not nil)
	// This is skipped for types-only generation where Services is explicitly nil
	if schema.Services != nil {
		manifestContent, err := g.generateManifest(ctx, schema, opts.Config, tsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to generate manifest: %w", err)
		}

		manifestPath := "manifest.ts"
		if err := opts.Sink.WriteFile(ctx, manifestPath, manifestContent); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", manifestPath, err)
		}
		result.Files = append(result.Files, OutputFile{
			Path: manifestPath,
			Size: int64(len(manifestContent)),
		})
	}

	return result, nil
}

// generateTypes generates the types.ts file content.
func (g *TypeScriptGenerator) generateTypes(ctx context.Context, schema *ir.Schema, config GeneratorConfig, tsConfig TypeScriptConfig) ([]byte, int, []ir.Warning, error) {
	var buf bytes.Buffer
	var warnings []ir.Warning
	typesGenerated := 0

	// Header comment
	buf.WriteString("// Code generated by tygor. DO NOT EDIT.\n")
	buf.WriteString("\n")

	// Frontmatter (custom type definitions, imports, etc.)
	if config.Frontmatter != "" {
		buf.WriteString(config.Frontmatter)
		if !strings.HasSuffix(config.Frontmatter, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}

	// Create emitter context
	emitter := &Emitter{
		schema:    schema,
		config:    config,
		tsConfig:  tsConfig,
		indent:    "",
		indentStr: computeIndentStr(config),
	}

	// Sort types alphabetically for deterministic output (§7.1)
	sortedTypes := make([]ir.TypeDescriptor, len(schema.Types))
	copy(sortedTypes, schema.Types)
	sort.Slice(sortedTypes, func(i, j int) bool {
		return sortedTypes[i].TypeName().Name < sortedTypes[j].TypeName().Name
	})

	// Emit each type
	for _, typ := range sortedTypes {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}

		typeWarnings, err := emitter.EmitType(&buf, typ)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("failed to emit type %s: %w", typ.TypeName().Name, err)
		}
		warnings = append(warnings, typeWarnings...)
		typesGenerated++
		buf.WriteString("\n")
	}

	// Ensure trailing newline if configured
	content := buf.Bytes()
	if config.TrailingNewline && len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}

	// Apply line ending conversion
	content = applyLineEnding(content, config.LineEnding)

	return content, typesGenerated, warnings, nil
}

// generatedFile holds a file path and its content.
type generatedFile struct {
	Path     string
	Content  []byte
	Warnings []string
}

// generateMultiFileTypes generates one file per package plus a barrel file.
func (g *TypeScriptGenerator) generateMultiFileTypes(ctx context.Context, schema *ir.Schema, config GeneratorConfig, tsConfig TypeScriptConfig) ([]generatedFile, int, []ir.Warning, error) {
	var warnings []ir.Warning
	var files []generatedFile
	typesGenerated := 0

	// Group types by package
	typesByPackage := make(map[string][]ir.TypeDescriptor)
	for _, typ := range schema.Types {
		pkg := typ.TypeName().Package
		typesByPackage[pkg] = append(typesByPackage[pkg], typ)
	}

	// Get sorted package list for deterministic output
	packages := make([]string, 0, len(typesByPackage))
	for pkg := range typesByPackage {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)

	// Track generated filenames for the barrel
	var generatedFilenames []string

	// Create emitter context
	emitter := &Emitter{
		schema:    schema,
		config:    config,
		tsConfig:  tsConfig,
		indent:    "",
		indentStr: computeIndentStr(config),
	}

	// Generate one file per package
	for _, pkg := range packages {
		types := typesByPackage[pkg]

		// Sort types alphabetically within package
		sort.Slice(types, func(i, j int) bool {
			return types[i].TypeName().Name < types[j].TypeName().Name
		})

		var buf bytes.Buffer
		buf.WriteString("// Code generated by tygor. DO NOT EDIT.\n")
		buf.WriteString("\n")

		// Frontmatter (custom type definitions, imports, etc.)
		if config.Frontmatter != "" {
			buf.WriteString(config.Frontmatter)
			if !strings.HasSuffix(config.Frontmatter, "\n") {
				buf.WriteString("\n")
			}
			buf.WriteString("\n")
		}

		for _, typ := range types {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}

			typeWarnings, err := emitter.EmitType(&buf, typ)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("failed to emit type %s: %w", typ.TypeName().Name, err)
			}
			warnings = append(warnings, typeWarnings...)
			typesGenerated++
			buf.WriteString("\n")
		}

		content := buf.Bytes()
		if config.TrailingNewline && len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = applyLineEnding(content, config.LineEnding)

		// Generate filename from package path
		filename := "types_" + sanitizePkgPath(pkg) + ".ts"
		generatedFilenames = append(generatedFilenames, filename)

		files = append(files, generatedFile{
			Path:    filename,
			Content: content,
		})
	}

	// Generate barrel file (types.ts)
	var barrelBuf bytes.Buffer
	barrelBuf.WriteString("// Code generated by tygor. DO NOT EDIT.\n")
	barrelBuf.WriteString("\n")
	for _, filename := range generatedFilenames {
		importPath := "./" + strings.TrimSuffix(filename, ".ts")
		barrelBuf.WriteString(fmt.Sprintf("export * from '%s';\n", importPath))
	}

	barrelContent := barrelBuf.Bytes()
	if config.TrailingNewline && len(barrelContent) > 0 && barrelContent[len(barrelContent)-1] != '\n' {
		barrelContent = append(barrelContent, '\n')
	}
	barrelContent = applyLineEnding(barrelContent, config.LineEnding)

	files = append(files, generatedFile{
		Path:    "types.ts",
		Content: barrelContent,
	})

	return files, typesGenerated, warnings, nil
}

// sanitizePkgPath converts a Go package path to a safe filename component.
func sanitizePkgPath(pkgPath string) string {
	// Replace non-alphanumeric characters with underscores
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, pkgPath)

	// Collapse multiple underscores
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}

	// Trim leading/trailing underscores
	return strings.Trim(sanitized, "_")
}

// generateManifest generates the manifest.ts file content.
func (g *TypeScriptGenerator) generateManifest(ctx context.Context, schema *ir.Schema, config GeneratorConfig, tsConfig TypeScriptConfig) ([]byte, error) {
	var buf bytes.Buffer

	// Header comment
	buf.WriteString("// Code generated by tygor. DO NOT EDIT.\n")
	buf.WriteString("\n")

	// Import types
	buf.WriteString("import * as types from './types';\n")
	buf.WriteString("\n")

	// Create emitter for type references
	emitter := &Emitter{
		schema:    schema,
		config:    config,
		tsConfig:  tsConfig,
		indent:    "",
		indentStr: computeIndentStr(config),
	}

	// Generate manifest interface
	buf.WriteString("export interface Manifest {\n")

	// Collect all endpoints from all services
	var endpoints []ir.EndpointDescriptor
	for _, svc := range schema.Services {
		endpoints = append(endpoints, svc.Endpoints...)
	}

	// Sort endpoints by FullName for deterministic output
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].FullName < endpoints[j].FullName
	})

	// Emit each endpoint
	for _, endpoint := range endpoints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		buf.WriteString(fmt.Sprintf("  %q: {\n", endpoint.FullName))

		// Request type
		buf.WriteString("    req: ")
		if endpoint.Request == nil {
			buf.WriteString("Record<string, never>")
		} else {
			reqType, err := emitter.EmitTypeExpr(endpoint.Request)
			if err != nil {
				return nil, fmt.Errorf("failed to emit request type for %s: %w", endpoint.FullName, err)
			}
			// Prefix with "types." for references
			reqType = prefixTypeReferences(reqType, "types.")
			buf.WriteString(reqType)
		}
		buf.WriteString(";\n")

		// Response type
		buf.WriteString("    res: ")
		resType, err := emitter.EmitTypeExpr(endpoint.Response)
		if err != nil {
			return nil, fmt.Errorf("failed to emit response type for %s: %w", endpoint.FullName, err)
		}
		// Prefix with "types." for references (including those inside arrays)
		resType = prefixTypeReferences(resType, "types.")
		buf.WriteString(resType)
		buf.WriteString(";\n")

		// Primitive field for streaming endpoints (stream and atom)
		if endpoint.Primitive == "stream" || endpoint.Primitive == "atom" {
			buf.WriteString(fmt.Sprintf("    primitive: %q;\n", endpoint.Primitive))
		}

		buf.WriteString("  };\n")
	}

	buf.WriteString("}\n")
	buf.WriteString("\n")

	// Generate metadata constant
	buf.WriteString("const metadata = {\n")
	for _, endpoint := range endpoints {
		buf.WriteString(fmt.Sprintf("  %q: { path: %q, primitive: %q },\n",
			endpoint.FullName, endpoint.Path, endpoint.Primitive))
	}
	buf.WriteString("} as const;\n")
	buf.WriteString("\n")

	// Generate registry export for backward compatibility
	buf.WriteString("export const registry = {\n")
	buf.WriteString("  manifest: {} as Manifest,\n")
	buf.WriteString("  metadata,\n")
	buf.WriteString("};\n")

	// Apply line ending and trailing newline
	content := buf.Bytes()
	if config.TrailingNewline && len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = applyLineEnding(content, config.LineEnding)

	return content, nil
}

// validateOptions validates the generation options.
func validateOptions(opts GenerateOptions) error {
	if opts.Sink == nil {
		return fmt.Errorf("sink is required")
	}
	return nil
}

// computeIndentStr returns the indent string based on config.
// Defaults to 2 spaces if not configured.
func computeIndentStr(config GeneratorConfig) string {
	switch config.IndentStyle {
	case "tab":
		return "\t"
	default: // "space" or empty
		size := config.IndentSize
		if size <= 0 {
			size = 2 // default to 2 spaces
		}
		return strings.Repeat(" ", size)
	}
}

// getTypeScriptConfig extracts TypeScript config from Custom map with defaults.
func getTypeScriptConfig(config GeneratorConfig) TypeScriptConfig {
	tsConfig := TypeScriptConfig{
		EmitExport:        true,
		EmitDeclare:       false,
		UseInterface:      true,
		UseReadonlyArrays: false,
		EnumStyle:         "union",
		UnknownType:       "unknown",
		EmitTypeHints:     true, // Show Go types as comments (e.g., /* int64 */, /* RFC3339 */)
	}

	if config.Custom == nil {
		return tsConfig
	}

	if v, ok := config.Custom["EmitExport"].(bool); ok {
		tsConfig.EmitExport = v
	}
	if v, ok := config.Custom["EmitDeclare"].(bool); ok {
		tsConfig.EmitDeclare = v
	}
	if v, ok := config.Custom["UseInterface"].(bool); ok {
		tsConfig.UseInterface = v
	}
	if v, ok := config.Custom["UseReadonlyArrays"].(bool); ok {
		tsConfig.UseReadonlyArrays = v
	}
	if v, ok := config.Custom["EnumStyle"].(string); ok {
		tsConfig.EnumStyle = v
	}
	if v, ok := config.Custom["OptionalType"].(string); ok {
		tsConfig.OptionalType = v
	}
	if v, ok := config.Custom["UnknownType"].(string); ok {
		tsConfig.UnknownType = v
	}
	if v, ok := config.Custom["EmitTypeHints"].(bool); ok {
		tsConfig.EmitTypeHints = v
	}
	if v, ok := config.Custom["NullableSliceElements"].(bool); ok {
		tsConfig.NullableSliceElements = v
	}

	return tsConfig
}

// applyLineEnding converts line endings to the configured style.
func applyLineEnding(content []byte, lineEnding string) []byte {
	if lineEnding == "crlf" {
		// Convert LF to CRLF
		return bytes.ReplaceAll(content, []byte("\n"), []byte("\r\n"))
	}
	// Default to LF (no conversion needed if already LF)
	return content
}

// applyNameTransforms applies prefix, suffix, and case transformations to a name.
func applyNameTransforms(name string, config GeneratorConfig) string {
	// Apply case transformation
	name = applyCaseTransform(name, config.TypeCase)

	// Apply prefix and suffix
	return config.TypePrefix + name + config.TypeSuffix
}

// applyCaseTransform applies case transformation to a name.
func applyCaseTransform(name, caseStyle string) string {
	if name == "" {
		return name
	}

	switch caseStyle {
	case "camel":
		return toCamelCase(name)
	case "pascal":
		return toPascalCase(name)
	case "snake":
		return toSnakeCase(name)
	case "kebab":
		return toKebabCase(name)
	default: // "preserve" or empty
		return name
	}
}

// toCamelCase converts a name to camelCase.
func toCamelCase(s string) string {
	if s == "" {
		return s
	}
	// If there are underscores, use PascalCase and lowercase first letter
	if strings.Contains(s, "_") {
		s = toPascalCase(s)
		if len(s) > 0 {
			return strings.ToLower(s[:1]) + s[1:]
		}
		return s
	}
	// Otherwise, just lowercase the first letter
	if len(s) > 0 {
		return strings.ToLower(s[:1]) + s[1:]
	}
	return s
}

// toPascalCase converts a name to PascalCase.
func toPascalCase(s string) string {
	if s == "" {
		return s
	}
	// Split on underscores and capitalize each word
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, "")
}

// toSnakeCase converts a name to snake_case.
func toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// toKebabCase converts a name to kebab-case.
func toKebabCase(s string) string {
	return strings.ReplaceAll(toSnakeCase(s), "_", "-")
}

// typeIdentifierRe matches type identifiers including version-prefixed ones.
// Matches:
// - PascalCase identifiers: User, MigrationRequest
// - Version-prefixed identifiers: v1_User, v2_GetUserRequest (from StripPackagePrefix)
var typeIdentifierRe = regexp.MustCompile(`\b((?:[a-z][a-z0-9]*_)?[A-Z][A-Za-z0-9_]*)\b`)

// prefixTypeReferences adds a prefix to type references in a type expression.
// This is used to add "types." before user-defined types in manifest generation.
// It preserves TypeScript keywords, primitives, and utility types.
func prefixTypeReferences(typeExpr string, prefix string) string {
	// TypeScript primitives and utility types that should not be prefixed
	skipTypes := map[string]bool{
		"Record":      true,
		"Partial":     true,
		"Required":    true,
		"Readonly":    true,
		"Pick":        true,
		"Omit":        true,
		"Exclude":     true,
		"Extract":     true,
		"NonNullable": true,
		"ReturnType":  true,
		"Parameters":  true,
		"Array":       true,
		"Promise":     true,
		"Map":         true,
		"Set":         true,
	}

	return typeIdentifierRe.ReplaceAllStringFunc(typeExpr, func(match string) string {
		if skipTypes[match] {
			return match
		}
		return prefix + match
	})
}

// flavorConfig holds flavor-related configuration.
type flavorConfig struct {
	Flavors   []string
	EmitTypes bool
}

// getFlavorConfig extracts flavor config from the Custom map.
func getFlavorConfig(config GeneratorConfig) flavorConfig {
	cfg := flavorConfig{
		EmitTypes: true, // default: emit types
	}

	if config.Custom == nil {
		return cfg
	}

	if v, ok := config.Custom["Flavors"].([]string); ok {
		cfg.Flavors = v
	}

	if v, ok := config.Custom["EmitTypes"].(*bool); ok && v != nil {
		cfg.EmitTypes = *v
	}

	return cfg
}

// generateFlavors generates output for each enabled flavor.
func (g *TypeScriptGenerator) generateFlavors(ctx context.Context, schema *ir.Schema, config GeneratorConfig, flavorCfg flavorConfig) ([]generatedFile, error) {
	var files []generatedFile

	// Sort types for deterministic output
	sortedTypes := make([]ir.TypeDescriptor, len(schema.Types))
	copy(sortedTypes, schema.Types)
	sort.Slice(sortedTypes, func(i, j int) bool {
		return sortedTypes[i].TypeName().Name < sortedTypes[j].TypeName().Name
	})

	// Build emit context
	emitCtx := &flavor.EmitContext{
		Schema:             schema,
		IndentStr:          computeIndentStr(config),
		EmitTypes:          flavorCfg.EmitTypes,
		TypeMappings:       config.TypeMappings,
		StripPackagePrefix: config.StripPackagePrefix,
	}

	for _, name := range flavorCfg.Flavors {
		f, err := flavor.Get(name)
		if err != nil {
			return nil, err
		}

		content, err := flavor.Generate(f, emitCtx, sortedTypes)
		if err != nil {
			return nil, fmt.Errorf("flavor %s: %w", name, err)
		}

		// Apply line ending and trailing newline
		if config.TrailingNewline && len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = applyLineEnding(content, config.LineEnding)

		files = append(files, generatedFile{
			Path:     "schemas" + f.FileExtension(),
			Content:  content,
			Warnings: emitCtx.Warnings,
		})

		// Reset warnings for next flavor
		emitCtx.Warnings = nil
	}

	return files, nil
}

// generateSchemaMap generates the schemas.map.ts file for client-side validation.
// This file maps endpoint names to their input/output Zod schemas.
func (g *TypeScriptGenerator) generateSchemaMap(ctx context.Context, schema *ir.Schema, config GeneratorConfig, flavorName string) ([]byte, error) {
	var buf bytes.Buffer

	// Header comment
	buf.WriteString("// Code generated by tygor. DO NOT EDIT.\n")
	buf.WriteString("\n")

	// Import Zod and schemas for the specific flavor
	if flavorName == "zod-mini" {
		buf.WriteString("import * as z from 'zod/mini';\n")
		buf.WriteString("import * as s from './schemas.zod-mini';\n")
	} else {
		buf.WriteString("import { z } from 'zod';\n")
		buf.WriteString("import * as s from './schemas.zod';\n")
	}
	buf.WriteString("\n")

	// Collect all endpoints from all services
	var endpoints []ir.EndpointDescriptor
	for _, svc := range schema.Services {
		endpoints = append(endpoints, svc.Endpoints...)
	}

	// Sort endpoints by FullName for deterministic output
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].FullName < endpoints[j].FullName
	})

	// Generate the schema map
	buf.WriteString("export const schemaMap = {\n")

	for _, endpoint := range endpoints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		buf.WriteString(fmt.Sprintf("  %q: {\n", endpoint.FullName))

		// Request schema
		buf.WriteString("    request: ")
		isMini := flavorName == "zod-mini"
		if endpoint.Request == nil {
			buf.WriteString("z.object({})")
		} else {
			requestSchema := typeToZodSchema(endpoint.Request, isMini)
			buf.WriteString(requestSchema)
		}
		buf.WriteString(",\n")

		// Response schema
		buf.WriteString("    response: ")
		responseSchema := typeToZodSchema(endpoint.Response, isMini)
		buf.WriteString(responseSchema)
		buf.WriteString(",\n")

		buf.WriteString("  },\n")
	}

	buf.WriteString("} as const;\n")
	buf.WriteString("\n")

	// Generate SchemaMap type for the client
	buf.WriteString("export type SchemaMap = typeof schemaMap;\n")

	// Apply line ending and trailing newline
	content := buf.Bytes()
	if config.TrailingNewline && len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = applyLineEnding(content, config.LineEnding)

	return content, nil
}

// typeToZodSchema converts an IR type descriptor to a Zod schema expression.
// Uses "s." prefix to reference schemas from the schemas.zod.ts import.
// The mini parameter indicates whether to generate zod-mini compatible code.
func typeToZodSchema(typ ir.TypeDescriptor, mini bool) string {
	if typ == nil {
		return "z.unknown()"
	}

	switch t := typ.(type) {
	case *ir.ReferenceDescriptor:
		return "s." + t.Target.Name + "Schema"

	case *ir.ArrayDescriptor:
		elem := typeToZodSchema(t.Element, mini)
		return fmt.Sprintf("z.array(%s)", elem)

	case *ir.MapDescriptor:
		value := typeToZodSchema(t.Value, mini)
		return fmt.Sprintf("z.record(z.string(), %s)", value)

	case *ir.PtrDescriptor:
		elem := typeToZodSchema(t.Element, mini)
		if mini {
			return fmt.Sprintf("z.nullable(%s)", elem)
		}
		return elem + ".nullable()"

	case *ir.PrimitiveDescriptor:
		switch t.PrimitiveKind {
		case ir.PrimitiveBool:
			return "z.boolean()"
		case ir.PrimitiveString:
			return "z.string()"
		case ir.PrimitiveInt, ir.PrimitiveUint, ir.PrimitiveFloat:
			return "z.number()"
		case ir.PrimitiveBytes:
			return "z.string()"
		case ir.PrimitiveTime:
			return "z.string().datetime()"
		case ir.PrimitiveAny:
			return "z.unknown()"
		case ir.PrimitiveEmpty:
			return "z.object({}).strict()"
		default:
			return "z.unknown()"
		}

	default:
		return "z.unknown()"
	}
}
