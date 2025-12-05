package gen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tygor.dev/internal/discover"
	"tygor.dev/internal/runner"
	"tygor.dev/tygorgen/provider"
	"tygor.dev/tygorgen/sink"
	"tygor.dev/tygorgen/typescript"
)

type Cmd struct {
	Out       string   `arg:"" help:"Output directory for generated files."`
	Export    string   `help:"Export function name (required if multiple exports exist)." short:"e"`
	Flavor    string   `help:"Output flavor (e.g., zod)." short:"f"`
	Discovery bool     `help:"Generate discovery.json." short:"d"`
	NoConfig  bool     `help:"Ignore config function."`
	Package   string   `help:"Package to scan (default: current directory)." short:"p" default:"."`
	Check     bool     `help:"Check if generated files are up-to-date (exit 1 if stale)." short:"c"`
	Types     []string `help:"Generate types without a tygor app (e.g., -t ErrorCode -t github.com/foo/bar.MyType)." short:"t" name:"type"`
}

func (c *Cmd) Run() error {
	// Standalone type generation mode
	if len(c.Types) > 0 {
		return c.runStandaloneTypes()
	}

	// Discover exports in the package
	result, err := discover.Find(c.Package)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	// Select the export to use
	export, err := discover.SelectExport(result.Exports, c.Export)
	if err != nil {
		return err
	}

	// Validate flags for *tygorgen.Generator exports
	if export.Type == discover.ExportTypeGenerator {
		if c.Flavor != "" {
			return fmt.Errorf("--flavor not supported with *tygorgen.Generator export\n\nYour export returns *tygorgen.Generator - configuration is in code.\nAdd flavors in your generator function:\n\n    return tygorgen.FromApp(setupApp()).\n        WithFlavor(tygorgen.FlavorZod)")
		}
		if c.Discovery {
			return fmt.Errorf("--discovery not supported with *tygorgen.Generator export\n\nYour export returns *tygorgen.Generator - configuration is in code.\nEnable discovery in your generator function:\n\n    return tygorgen.FromApp(setupApp()).\n        WithDiscovery()")
		}
	}

	// Resolve output directory to absolute path
	outDir, err := filepath.Abs(c.Out)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	// In check mode, generate to temp dir and compare
	genDir := outDir
	if c.Check {
		tmpDir, err := os.MkdirTemp("", "tygor-gen-check-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		genDir = tmpDir
	}

	// Build runner options
	opts := runner.Options{
		Export:     *export,
		OutDir:     genDir,
		Flavor:     c.Flavor,
		Discovery:  c.Discovery,
		NoConfig:   c.NoConfig,
		PkgDir:     result.Dir,
		PkgPath:    result.PackagePath,
		ModulePath: result.ModulePath,
		ModuleDir:  result.ModuleDir,
	}

	// Add config function if present and applicable
	if result.ConfigFunc != nil && export.Type == discover.ExportTypeApp {
		opts.ConfigFunc = result.ConfigFunc.Name
	}

	// Run the generator
	output, err := runner.Exec(opts)
	if err != nil {
		if len(output) > 0 {
			fmt.Fprint(os.Stderr, string(output))
		}
		return err
	}

	// Print any output (warnings, etc.)
	if len(output) > 0 {
		fmt.Print(string(output))
	}

	// In check mode, compare generated files with existing
	if c.Check {
		return c.compareFiles(genDir, outDir)
	}

	return nil
}

// compareFiles compares generated files in genDir with existing files in outDir.
// Returns an error if any files differ, are missing, or are stale.
func (c *Cmd) compareFiles(genDir, outDir string) error {
	var stale []string

	// Check all generated files exist and match in outDir
	entries, err := os.ReadDir(genDir)
	if err != nil {
		return fmt.Errorf("read generated dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		genPath := filepath.Join(genDir, name)
		outPath := filepath.Join(outDir, name)

		genContent, err := os.ReadFile(genPath)
		if err != nil {
			return fmt.Errorf("read generated file %s: %w", name, err)
		}

		outContent, err := os.ReadFile(outPath)
		if err != nil {
			if os.IsNotExist(err) {
				stale = append(stale, name+" (missing)")
				continue
			}
			return fmt.Errorf("read existing file %s: %w", name, err)
		}

		if !bytes.Equal(genContent, outContent) {
			stale = append(stale, name)
		}
	}

	if len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "Generated files are out of date in %s:\n", outDir)
		for _, f := range stale {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintf(os.Stderr, "\nRun 'tygor gen %s' to update.\n", outDir)
		return fmt.Errorf("generated files are stale")
	}

	return nil
}

// runStandaloneTypes generates TypeScript types directly from Go types
// without requiring a tygor app export.
func (c *Cmd) runStandaloneTypes() error {
	// Validate incompatible flags
	if c.Export != "" {
		return fmt.Errorf("--export cannot be used with --type")
	}
	if c.NoConfig {
		return fmt.Errorf("--no-config cannot be used with --type")
	}

	// Parse type specifications into packages and root types
	packages, rootTypes, err := parseTypeSpecs(c.Types, c.Package)
	if err != nil {
		return err
	}

	// Resolve output directory
	outDir, err := filepath.Abs(c.Out)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	// In check mode, generate to temp dir and compare
	genDir := outDir
	if c.Check {
		tmpDir, err := os.MkdirTemp("", "tygor-gen-check-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		genDir = tmpDir
	}

	// Build schema using source provider
	ctx := context.Background()
	p := &provider.SourceProvider{}
	schema, err := p.BuildSchema(ctx, provider.SourceInputOptions{
		Packages:  packages,
		RootTypes: rootTypes,
	})
	if err != nil {
		return fmt.Errorf("build schema: %w", err)
	}

	// Validate schema
	if errs := schema.Validate(); len(errs) > 0 {
		return fmt.Errorf("schema validation: %w", errs[0])
	}

	// Configure TypeScript generator
	tsConfig := typescript.GeneratorConfig{
		FieldCase:       "preserve",
		TypeCase:        "preserve",
		SingleFile:      true, // Standalone types go in single file
		IndentStyle:     "space",
		IndentSize:      2,
		LineEnding:      "lf",
		TrailingNewline: true,
		EmitComments:    true,
		Custom: map[string]any{
			"EmitExport":        true,
			"EmitDeclare":       false,
			"UseInterface":      true,
			"UseReadonlyArrays": false,
			"EnumStyle":         "union",
			"OptionalType":      "undefined",
			"UnknownType":       "unknown",
			"EmitTypes":         true,
		},
	}

	// Add flavors if specified
	if c.Flavor != "" {
		tsConfig.Custom["Flavors"] = []string{c.Flavor}
	}

	// Create filesystem sink
	outputSink := sink.NewFilesystemSink(genDir)

	// Generate TypeScript
	gen := &typescript.TypeScriptGenerator{}
	result, err := gen.Generate(ctx, schema, typescript.GenerateOptions{
		Sink:   outputSink,
		Config: tsConfig,
	})
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	// Print warnings
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}

	// In check mode, compare files
	if c.Check {
		return c.compareFiles(genDir, outDir)
	}

	return nil
}

// parseTypeSpecs parses type specifications like "ErrorCode" or "github.com/foo/bar.MyType"
// into packages to load and root types to extract.
func parseTypeSpecs(specs []string, defaultPkg string) ([]string, []provider.RootType, error) {
	seen := make(map[string]bool)
	var packages []string
	var rootTypes []provider.RootType

	for _, spec := range specs {
		var pkg, name string

		// Check if it's a fully qualified type (contains a dot after a slash)
		// e.g., "github.com/foo/bar.MyType" -> pkg="github.com/foo/bar", name="MyType"
		if lastDot := strings.LastIndex(spec, "."); lastDot > 0 {
			// Ensure there's a slash before the dot (it's a package path, not just "pkg.Type")
			if strings.Contains(spec[:lastDot], "/") {
				pkg = spec[:lastDot]
				name = spec[lastDot+1:]
			} else {
				// Could be "pkg.Type" which is ambiguous - treat as error
				return nil, nil, fmt.Errorf("ambiguous type %q: use fully qualified path (e.g., github.com/pkg.Type) or simple name with --package", spec)
			}
		} else {
			// Simple name like "ErrorCode" - use default package
			pkg = defaultPkg
			name = spec
		}

		if name == "" {
			return nil, nil, fmt.Errorf("invalid type specification: %q", spec)
		}

		// Add package if not seen
		if !seen[pkg] {
			seen[pkg] = true
			packages = append(packages, pkg)
		}

		rootTypes = append(rootTypes, provider.RootType{
			Package: pkg,
			Name:    name,
		})
	}

	return packages, rootTypes, nil
}
