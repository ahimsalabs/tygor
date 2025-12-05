package tygorgen

import "tygor.dev/tygor"

// Flavor represents a code generation output flavor.
// Flavors produce alternative TypeScript outputs like Zod schemas.
type Flavor string

const (
	// FlavorZod generates Zod schemas for runtime validation.
	// Output: schemas.zod.ts with z.object() schemas matching Go validate tags.
	FlavorZod Flavor = "zod"

	// FlavorZodMini generates Zod schemas using zod/mini for smaller bundle size.
	// Output: schemas.zod-mini.ts with z.object() schemas.
	FlavorZodMini Flavor = "zod-mini"
)

// String returns the flavor name.
func (f Flavor) String() string {
	return string(f)
}

// Generator provides a fluent API for code generation.
// Create with FromApp() or FromTypes() and configure with method chaining.
//
// Example:
//
//	tygorgen.FromApp(app).
//	    WithFlavor(tygorgen.FlavorZod).
//	    ToDir("./client/src/rpc")
type Generator struct {
	app   *tygor.App
	types []any // For standalone type generation without an app
	cfg   Config
}

// FromApp creates a new Generator for the given app.
// This is the entry point for the fluent API.
func FromApp(app *tygor.App) *Generator {
	return &Generator{app: app}
}

// FromTypes creates a Generator for standalone type generation without a tygor app.
// Pass zero values of the types you want to generate TypeScript for.
//
// Example:
//
//	tygorgen.FromTypes(
//	    User{},
//	    CreateUserRequest{},
//	    ListUsersResponse{},
//	).ToDir("./client/src/types")
//
// By default, this uses the source provider for full enum and comment support.
// Use .Provider("reflection") for faster generation without source analysis.
func FromTypes(types ...any) *Generator {
	return &Generator{
		types: types,
	}
}

// WithFlavor adds a flavor to the generation output.
// Can be called multiple times to add multiple flavors.
func (g *Generator) WithFlavor(f Flavor) *Generator {
	g.cfg.Flavors = append(g.cfg.Flavors, f)
	return g
}

// WithoutTypes disables base types.ts generation.
// When disabled with Zod flavor, types are exported via z.infer<typeof Schema>.
func (g *Generator) WithoutTypes() *Generator {
	emitTypes := false
	g.cfg.EmitTypes = &emitTypes
	return g
}

// SingleFile emits all types in a single types.ts file.
func (g *Generator) SingleFile() *Generator {
	g.cfg.SingleFile = true
	return g
}

// Provider sets the type extraction strategy.
// Valid values: "source" (default), "reflection".
func (g *Generator) Provider(p string) *Generator {
	g.cfg.Provider = p
	return g
}

// PreserveComments controls whether Go doc comments are preserved.
// Valid values: "default", "types", "none".
func (g *Generator) PreserveComments(mode string) *Generator {
	g.cfg.PreserveComments = mode
	return g
}

// EnumStyle controls how Go const groups are generated.
// Valid values: "union" (default), "enum", "const".
func (g *Generator) EnumStyle(style string) *Generator {
	g.cfg.EnumStyle = style
	return g
}

// OptionalType controls how optional fields are typed.
// Valid values: "undefined" (default), "null".
func (g *Generator) OptionalType(t string) *Generator {
	g.cfg.OptionalType = t
	return g
}

// Frontmatter adds content to the top of generated TypeScript files.
func (g *Generator) Frontmatter(content string) *Generator {
	g.cfg.Frontmatter = content
	return g
}

// TypeMapping adds a Go type to TypeScript type mapping.
func (g *Generator) TypeMapping(goType, tsType string) *Generator {
	if g.cfg.TypeMappings == nil {
		g.cfg.TypeMappings = make(map[string]string)
	}
	g.cfg.TypeMappings[goType] = tsType
	return g
}

// StripPackagePrefix sets the prefix to remove from package paths.
func (g *Generator) StripPackagePrefix(prefix string) *Generator {
	g.cfg.StripPackagePrefix = prefix
	return g
}

// Packages adds additional Go packages to analyze.
func (g *Generator) Packages(pkgs ...string) *Generator {
	g.cfg.Packages = append(g.cfg.Packages, pkgs...)
	return g
}

// WithDiscovery enables discovery.json output.
// The discovery document contains the full API schema for runtime introspection.
func (g *Generator) WithDiscovery() *Generator {
	g.cfg.EmitDiscovery = true
	return g
}

// ToDir generates files to the specified directory.
// This is a terminal operation that writes files to disk.
func (g *Generator) ToDir(dir string) (*GenerateResult, error) {
	g.cfg.OutDir = dir
	if g.app != nil {
		return Generate(g.app, &g.cfg)
	}
	return GenerateTypes(g.types, &g.cfg)
}

// Generate returns generated files in memory without writing to disk.
// Use ToDir() to write files to disk instead.
func (g *Generator) Generate() (*GenerateResult, error) {
	if g.app != nil {
		return Generate(g.app, &g.cfg)
	}
	return GenerateTypes(g.types, &g.cfg)
}
