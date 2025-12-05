// Package discover finds tygor export functions by signature.
//
// It scans a Go package for functions with these signatures:
//   - func() *tygor.App
//   - func() *tygorgen.Generator
//
// No directives or annotations needed — the signature is the marker.
package discover

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ExportType represents the return type of an export function.
type ExportType int

const (
	ExportTypeApp       ExportType = iota // func() *tygor.App
	ExportTypeGenerator                   // func() *tygorgen.Generator
)

func (t ExportType) String() string {
	switch t {
	case ExportTypeApp:
		return "*tygor.App"
	case ExportTypeGenerator:
		return "*tygorgen.Generator"
	default:
		return "unknown"
	}
}

// Export represents a discovered export function.
type Export struct {
	Name string         // function name
	Type ExportType     // return type
	Pos  token.Position // source location
}

// ConfigFunc represents a discovered config function.
// Signature: func(*tygorgen.Generator) *tygorgen.Generator
type ConfigFunc struct {
	Name string         // function name
	Pos  token.Position // source location
}

// Result contains discovered exports and package info.
type Result struct {
	Exports     []Export
	ConfigFunc  *ConfigFunc // optional config function
	PackagePath string
	ModulePath  string
	ModuleDir   string // directory containing go.mod
	Dir         string // directory containing the package
}

// Find scans a Go package for export functions.
//
// The pattern follows go command semantics:
//   - "." for current directory
//   - Import path like "github.com/foo/bar"
//   - Absolute or relative directory path
func Find(pattern string) (*Result, error) {
	return FindDir(pattern, "")
}

// FindDir is like Find but allows specifying a working directory.
func FindDir(pattern, dir string) (*Result, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedTypes | packages.NeedModule | packages.NeedSyntax,
		Dir: dir,
	}

	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found matching %q", pattern)
	}

	if len(pkgs) > 1 {
		return nil, fmt.Errorf("multiple packages found matching %q; specify a single package", pattern)
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		// Replace tabs with 2 spaces for cleaner error display
		errStr := strings.ReplaceAll(pkg.Errors[0].Error(), "\t", "  ")
		return nil, fmt.Errorf("package errors: %s", errStr)
	}

	result := &Result{
		PackagePath: pkg.PkgPath,
	}

	if pkg.Module != nil {
		result.ModulePath = pkg.Module.Path
		result.ModuleDir = pkg.Module.Dir
	}

	if len(pkg.GoFiles) > 0 {
		result.Dir = filepath.Dir(pkg.GoFiles[0])
	}

	// Scan package scope for export and config functions
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}

		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		// Must be package-level (no receiver)
		if sig.Recv() != nil {
			continue
		}

		// Check for config function: func(*tygorgen.Generator) *tygorgen.Generator
		if isConfigFunc(sig) {
			result.ConfigFunc = &ConfigFunc{
				Name: fn.Name(),
				Pos:  pkg.Fset.Position(fn.Pos()),
			}
			continue
		}

		// Export functions must have no parameters
		if sig.Params().Len() != 0 {
			continue
		}

		// Must return exactly one value
		if sig.Results().Len() != 1 {
			continue
		}

		// Check if return type is *tygor.App or *tygorgen.Generator
		ret := sig.Results().At(0).Type()
		exportType, ok := classifyType(ret)
		if !ok {
			continue
		}

		result.Exports = append(result.Exports, Export{
			Name: fn.Name(),
			Type: exportType,
			Pos:  pkg.Fset.Position(fn.Pos()),
		})
	}

	return result, nil
}

// isConfigFunc checks if a signature matches func(*tygorgen.Generator) *tygorgen.Generator.
func isConfigFunc(sig *types.Signature) bool {
	// Must have exactly one parameter
	if sig.Params().Len() != 1 {
		return false
	}

	// Must return exactly one value
	if sig.Results().Len() != 1 {
		return false
	}

	// Parameter must be *tygorgen.Generator
	param := sig.Params().At(0).Type()
	if !isGeneratorPtr(param) {
		return false
	}

	// Return must be *tygorgen.Generator
	ret := sig.Results().At(0).Type()
	return isGeneratorPtr(ret)
}

// isGeneratorPtr checks if a type is *tygorgen.Generator.
func isGeneratorPtr(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}

	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}

	pkg := named.Obj().Pkg()
	if pkg == nil {
		return false
	}

	return pkg.Path() == "tygor.dev/tygorgen" && named.Obj().Name() == "Generator"
}

// classifyType checks if a type is *tygor.App or *tygorgen.Generator.
func classifyType(t types.Type) (ExportType, bool) {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return 0, false
	}

	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return 0, false
	}

	pkg := named.Obj().Pkg()
	if pkg == nil {
		return 0, false
	}

	typeName := named.Obj().Name()
	pkgPath := pkg.Path()

	switch {
	case pkgPath == "tygor.dev/tygor" && typeName == "App":
		return ExportTypeApp, true
	case pkgPath == "tygor.dev/tygorgen" && typeName == "Generator":
		return ExportTypeGenerator, true
	default:
		return 0, false
	}
}

// SelectExport picks the export to use based on found exports and optional name.
//
// If name is empty:
//   - Returns the export if exactly one found
//   - Returns error if zero or multiple found
//
// If name is specified:
//   - Returns the export with that name
//   - Returns error if not found
func SelectExport(exports []Export, name string) (*Export, error) {
	if name != "" {
		for i := range exports {
			if exports[i].Name == name {
				return &exports[i], nil
			}
		}
		return nil, fmt.Errorf("export %q not found", name)
	}

	switch len(exports) {
	case 0:
		return nil, fmt.Errorf("no export found\n\nAdd a function that returns *tygor.App:\n\n    func setupApp() *tygor.App {\n        app := tygor.NewApp()\n        // ...\n        return app\n    }")
	case 1:
		return &exports[0], nil
	default:
		msg := "multiple exports found:\n"
		for _, e := range exports {
			msg += fmt.Sprintf("  - %s() %s\n", e.Name, e.Type)
		}
		msg += "\nSpecify which one: tygor gen --export <name> <outdir>"
		return nil, fmt.Errorf("%s", msg)
	}
}
