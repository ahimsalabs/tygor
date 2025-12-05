// Package runner executes tygor code generation by building and running
// a modified version of the user's package.
//
// It uses Go's -overlay flag to replace the user's main() with a runner
// that calls the export function and generates output.
package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"tygor.dev/internal/discover"
)

// Options configures the runner.
type Options struct {
	// Export is the function to call.
	Export discover.Export

	// OutDir is the output directory for generated files.
	OutDir string

	// Flavor is the optional flavor flag (e.g., "zod").
	// Only used when Export.Type is ExportTypeApp.
	Flavor string

	// Discovery enables discovery.json generation.
	// Only used when Export.Type is ExportTypeApp.
	Discovery bool

	// ConfigFunc is the optional config function name.
	// Only used when Export.Type is ExportTypeApp.
	ConfigFunc string

	// NoConfig disables the config function even if one exists.
	NoConfig bool

	// PkgDir is the directory containing the package.
	PkgDir string

	// PkgPath is the import path of the package (e.g., "github.com/foo/bar").
	// Required for non-main packages.
	PkgPath string

	// ModulePath is the module path (e.g., "github.com/foo").
	// Required for non-main packages.
	ModulePath string

	// ModuleDir is the directory containing the module's go.mod.
	// Required for non-main packages.
	ModuleDir string

	// CheckMode runs validation only, outputs JSON stats instead of generating files.
	CheckMode bool
}

// Exec builds and runs the generator.
//
// For package main: uses overlay to replace main() with runner main().
// For other packages: creates a temp module that imports the target package.
func Exec(opts Options) (output []byte, err error) {
	// Check if this is a main package by looking for func main()
	isMainPkg, err := hasMainFunc(opts.PkgDir)
	if err != nil {
		return nil, fmt.Errorf("check main: %w", err)
	}

	if isMainPkg {
		return execOverlay(opts)
	}
	return execImport(opts)
}

// execOverlay handles package main by using Go's overlay feature.
func execOverlay(opts Options) (output []byte, err error) {
	// Create temp directory for overlay files
	tmpDir, err := os.MkdirTemp("", "tygor-gen-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find and process files with main()
	overlay := make(map[string]string)

	files, err := filepath.Glob(filepath.Join(opts.PkgDir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}

	for _, file := range files {
		// Skip test files
		if filepath.Base(file) == "_test.go" || len(file) > 8 && file[len(file)-8:] == "_test.go" {
			continue
		}

		hasMain, modified, err := removeMain(file)
		if err != nil {
			return nil, fmt.Errorf("process %s: %w", file, err)
		}

		if hasMain {
			// Write modified file to temp dir
			tmpFile := filepath.Join(tmpDir, filepath.Base(file))
			if err := os.WriteFile(tmpFile, modified, 0644); err != nil {
				return nil, fmt.Errorf("write modified %s: %w", file, err)
			}
			overlay[file] = tmpFile
		}
	}

	// Generate runner
	runnerSrc, err := generateRunner(opts)
	if err != nil {
		return nil, fmt.Errorf("generate runner: %w", err)
	}

	runnerFile := filepath.Join(tmpDir, "tygor_runner_main_.go")
	if err := os.WriteFile(runnerFile, runnerSrc, 0644); err != nil {
		return nil, fmt.Errorf("write runner: %w", err)
	}

	// Add runner to overlay (maps to a "new" file in the package)
	overlay[filepath.Join(opts.PkgDir, "tygor_runner_main_.go")] = runnerFile

	// Write overlay JSON
	overlayData := struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: overlay}

	overlayJSON, err := json.Marshal(overlayData)
	if err != nil {
		return nil, fmt.Errorf("marshal overlay: %w", err)
	}

	overlayFile := filepath.Join(tmpDir, "overlay.json")
	if err := os.WriteFile(overlayFile, overlayJSON, 0644); err != nil {
		return nil, fmt.Errorf("write overlay: %w", err)
	}

	// Build with overlay
	// Use -mod=mod to allow updating go.mod/go.sum if needed
	binaryPath := filepath.Join(tmpDir, "runner")
	buildCmd := exec.Command("go", "build", "-mod=mod", "-overlay", overlayFile, "-o", binaryPath, ".")
	buildCmd.Dir = opts.PkgDir
	buildCmd.Env = append(os.Environ(), "GOWORK=off")
	if buildOut, err := buildCmd.CombinedOutput(); err != nil {
		return buildOut, fmt.Errorf("build: %w\n%s", err, buildOut)
	}

	// Run the binary
	runCmd := exec.Command(binaryPath)
	runCmd.Dir = opts.PkgDir
	output, err = runCmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run: %w\n%s", err, output)
	}

	return output, nil
}

// execImport handles non-main packages by using overlay to create a virtual
// runner package inside the module that imports the target package.
//
// For unexported functions, we also generate a shim file in the target package
// that exports the function under a known name.
func execImport(opts Options) (output []byte, err error) {
	if opts.PkgPath == "" {
		return nil, fmt.Errorf("PkgPath required for non-main packages")
	}
	if opts.ModuleDir == "" {
		return nil, fmt.Errorf("ModuleDir required for non-main packages")
	}

	// Create temp directory for overlay files
	tmpDir, err := os.MkdirTemp("", "tygor-gen-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	overlay := make(map[string]string)

	// Check if export function is unexported
	exportFunc := opts.Export.Name
	isUnexported := len(exportFunc) > 0 && exportFunc[0] >= 'a' && exportFunc[0] <= 'z'

	if isUnexported {
		// Generate a shim file in the target package that exports the function
		shimSrc, err := generateShim(opts)
		if err != nil {
			return nil, fmt.Errorf("generate shim: %w", err)
		}

		shimFile := filepath.Join(tmpDir, "tygor_shim_.go")
		if err := os.WriteFile(shimFile, shimSrc, 0644); err != nil {
			return nil, fmt.Errorf("write shim: %w", err)
		}

		// Add shim to overlay in the target package
		overlay[filepath.Join(opts.PkgDir, "tygor_shim_.go")] = shimFile

		// Update export name to use the shim's exported wrapper
		opts.Export.Name = "TygorExport_"
	}

	// Generate runner that imports the target package
	runnerSrc, err := generateImportRunner(opts)
	if err != nil {
		return nil, fmt.Errorf("generate runner: %w", err)
	}

	// Write runner to temp file
	runnerFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(runnerFile, runnerSrc, 0644); err != nil {
		return nil, fmt.Errorf("write runner: %w", err)
	}

	// Create overlay that places runner at a virtual path inside the module.
	// To import internal packages, the runner must be placed as a sibling of
	// or inside the "internal" directory's parent. We place it alongside the
	// target package.
	virtualRunnerDir := filepath.Join(opts.PkgDir, ".tygor-runner")
	virtualRunnerFile := filepath.Join(virtualRunnerDir, "main.go")

	overlay[virtualRunnerFile] = runnerFile

	overlayData := struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: overlay}

	overlayJSON, err := json.Marshal(overlayData)
	if err != nil {
		return nil, fmt.Errorf("marshal overlay: %w", err)
	}

	overlayFile := filepath.Join(tmpDir, "overlay.json")
	if err := os.WriteFile(overlayFile, overlayJSON, 0644); err != nil {
		return nil, fmt.Errorf("write overlay: %w", err)
	}

	// Build with overlay, targeting the virtual runner directory
	// Use -mod=mod to allow updating go.mod/go.sum if needed
	binaryPath := filepath.Join(tmpDir, "runner")
	buildCmd := exec.Command("go", "build", "-mod=mod", "-overlay", overlayFile, "-o", binaryPath, virtualRunnerDir)
	buildCmd.Dir = opts.ModuleDir
	buildCmd.Env = append(os.Environ(), "GOWORK=off")
	if buildOut, err := buildCmd.CombinedOutput(); err != nil {
		return buildOut, fmt.Errorf("build: %w\n%s", err, buildOut)
	}

	// Run the binary
	runCmd := exec.Command(binaryPath)
	runCmd.Dir = opts.PkgDir
	output, err = runCmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run: %w\n%s", err, output)
	}

	return output, nil
}

// hasMainFunc checks if any .go file in the directory has a main() function.
func hasMainFunc(dir string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return false, err
	}

	for _, file := range files {
		// Skip test files
		if filepath.Base(file) == "_test.go" || len(file) > 8 && file[len(file)-8:] == "_test.go" {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return false, err
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == "main" && fn.Recv == nil {
				return true, nil
			}
		}
	}

	return false, nil
}

// removeMain parses a Go file and returns a version with func main() renamed.
// We rename instead of removing so that imports used only by main() stay valid.
// Returns (hasMain, modifiedSource, error).
func removeMain(filename string) (bool, []byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return false, nil, err
	}

	// Find and rename main function
	hasMain := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			hasMain = true
			fn.Name.Name = "_tygor_original_main_"
			break
		}
	}

	if !hasMain {
		return false, nil, nil
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return false, nil, err
	}

	return true, buf.Bytes(), nil
}

// generateRunner creates the runner main() source.
func generateRunner(opts Options) ([]byte, error) {
	var tmplStr string
	if opts.CheckMode {
		switch opts.Export.Type {
		case discover.ExportTypeApp:
			tmplStr = appCheckTemplate
		case discover.ExportTypeGenerator:
			tmplStr = generatorCheckTemplate
		default:
			return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
		}
	} else {
		switch opts.Export.Type {
		case discover.ExportTypeApp:
			tmplStr = appRunnerTemplate
		case discover.ExportTypeGenerator:
			tmplStr = generatorRunnerTemplate
		default:
			return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
		}
	}

	tmpl, err := template.New("runner").Parse(tmplStr)
	if err != nil {
		return nil, err
	}

	configFunc := ""
	if opts.ConfigFunc != "" && !opts.NoConfig {
		configFunc = opts.ConfigFunc
	}

	data := struct {
		ExportFunc string
		OutDir     string
		Flavor     string
		Discovery  bool
		ConfigFunc string
	}{
		ExportFunc: opts.Export.Name,
		OutDir:     opts.OutDir,
		Flavor:     opts.Flavor,
		Discovery:  opts.Discovery,
		ConfigFunc: configFunc,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

const appRunnerTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
)

func main() {
	g := tygorgen.FromApp({{.ExportFunc}}())
{{if .Flavor}}
	g = g.WithFlavor(tygorgen.Flavor("{{.Flavor}}"))
{{end}}
{{if .Discovery}}
	g = g.WithDiscovery()
{{end}}
{{if .ConfigFunc}}
	g = {{.ConfigFunc}}(g)
{{end}}
	result, err := g.ToDir("{{.OutDir}}")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor gen: %v\n", err)
		os.Exit(1)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const generatorRunnerTemplate = `package main

import (
	"fmt"
	"os"
)

func main() {
	g := {{.ExportFunc}}()
	result, err := g.ToDir("{{.OutDir}}")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor gen: %v\n", err)
		os.Exit(1)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const appCheckTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
)

func main() {
	g := tygorgen.FromApp({{.ExportFunc}}())
{{if .ConfigFunc}}
	g = {{.ConfigFunc}}(g)
{{end}}
	result, err := g.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor check: %v\n", err)
		os.Exit(1)
	}

	// Print stats
	endpoints := 0
	for _, svc := range result.Schema.Services {
		endpoints += len(svc.Endpoints)
	}
	fmt.Printf("%d %d %d\n", len(result.Schema.Services), endpoints, len(result.Schema.Types))

	// Print warnings to stderr
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const generatorCheckTemplate = `package main

import (
	"fmt"
	"os"
)

func main() {
	g := {{.ExportFunc}}()
	result, err := g.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor check: %v\n", err)
		os.Exit(1)
	}

	// Print stats
	endpoints := 0
	for _, svc := range result.Schema.Services {
		endpoints += len(svc.Endpoints)
	}
	fmt.Printf("%d %d %d\n", len(result.Schema.Services), endpoints, len(result.Schema.Types))

	// Print warnings to stderr
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

// generateImportRunner creates runner source that imports a non-main package.
func generateImportRunner(opts Options) ([]byte, error) {
	var tmplStr string
	if opts.CheckMode {
		switch opts.Export.Type {
		case discover.ExportTypeApp:
			tmplStr = importAppCheckTemplate
		case discover.ExportTypeGenerator:
			tmplStr = importGeneratorCheckTemplate
		default:
			return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
		}
	} else {
		switch opts.Export.Type {
		case discover.ExportTypeApp:
			tmplStr = importAppRunnerTemplate
		case discover.ExportTypeGenerator:
			tmplStr = importGeneratorRunnerTemplate
		default:
			return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
		}
	}

	tmpl, err := template.New("runner").Parse(tmplStr)
	if err != nil {
		return nil, err
	}

	configFunc := ""
	if opts.ConfigFunc != "" && !opts.NoConfig {
		configFunc = opts.ConfigFunc
	}

	data := struct {
		PkgPath    string
		ExportFunc string
		OutDir     string
		Flavor     string
		Discovery  bool
		ConfigFunc string
	}{
		PkgPath:    opts.PkgPath,
		ExportFunc: opts.Export.Name,
		OutDir:     opts.OutDir,
		Flavor:     opts.Flavor,
		Discovery:  opts.Discovery,
		ConfigFunc: configFunc,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

const importAppRunnerTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
	pkg "{{.PkgPath}}"
)

func main() {
	g := tygorgen.FromApp(pkg.{{.ExportFunc}}())
{{if .Flavor}}
	g = g.WithFlavor(tygorgen.Flavor("{{.Flavor}}"))
{{end}}
{{if .Discovery}}
	g = g.WithDiscovery()
{{end}}
{{if .ConfigFunc}}
	g = pkg.{{.ConfigFunc}}(g)
{{end}}
	result, err := g.ToDir("{{.OutDir}}")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor gen: %v\n", err)
		os.Exit(1)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const importGeneratorRunnerTemplate = `package main

import (
	"fmt"
	"os"

	pkg "{{.PkgPath}}"
)

func main() {
	g := pkg.{{.ExportFunc}}()
	result, err := g.ToDir("{{.OutDir}}")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor gen: %v\n", err)
		os.Exit(1)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const importAppCheckTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
	pkg "{{.PkgPath}}"
)

func main() {
	g := tygorgen.FromApp(pkg.{{.ExportFunc}}())
{{if .ConfigFunc}}
	g = pkg.{{.ConfigFunc}}(g)
{{end}}
	result, err := g.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor check: %v\n", err)
		os.Exit(1)
	}

	// Print stats
	endpoints := 0
	for _, svc := range result.Schema.Services {
		endpoints += len(svc.Endpoints)
	}
	fmt.Printf("%d %d %d\n", len(result.Schema.Services), endpoints, len(result.Schema.Types))

	// Print warnings to stderr
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

const importGeneratorCheckTemplate = `package main

import (
	"fmt"
	"os"

	pkg "{{.PkgPath}}"
)

func main() {
	g := pkg.{{.ExportFunc}}()
	result, err := g.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tygor check: %v\n", err)
		os.Exit(1)
	}

	// Print stats
	endpoints := 0
	for _, svc := range result.Schema.Services {
		endpoints += len(svc.Endpoints)
	}
	fmt.Printf("%d %d %d\n", len(result.Schema.Services), endpoints, len(result.Schema.Types))

	// Print warnings to stderr
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.Code, w.Message)
	}
}
`

// generateShim creates a file that exports an unexported function.
// This is used when running tygor gen on a non-main package with an unexported export function.
func generateShim(opts Options) ([]byte, error) {
	// Get the package name by parsing an existing file
	pkgName, err := getPackageName(opts.PkgDir)
	if err != nil {
		return nil, fmt.Errorf("get package name: %w", err)
	}

	var tmplStr string
	switch opts.Export.Type {
	case discover.ExportTypeApp:
		tmplStr = shimAppTemplate
	case discover.ExportTypeGenerator:
		tmplStr = shimGeneratorTemplate
	default:
		return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
	}

	tmpl, err := template.New("shim").Parse(tmplStr)
	if err != nil {
		return nil, err
	}

	data := struct {
		PkgName    string
		ExportFunc string
	}{
		PkgName:    pkgName,
		ExportFunc: opts.Export.Name,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// getPackageName returns the package name from the first .go file in the directory.
func getPackageName(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return "", err
	}

	for _, file := range files {
		// Skip test files
		base := filepath.Base(file)
		if base == "_test.go" || len(base) > 8 && base[len(base)-8:] == "_test.go" {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}

		return f.Name.Name, nil
	}

	return "", fmt.Errorf("no Go files found in %s", dir)
}

const shimAppTemplate = `package {{.PkgName}}

import "tygor.dev/tygor"

// TygorExport_ is a generated wrapper to export the unexported function.
func TygorExport_() *tygor.App {
	return {{.ExportFunc}}()
}
`

const shimGeneratorTemplate = `package {{.PkgName}}

import "tygor.dev/tygorgen"

// TygorExport_ is a generated wrapper to export the unexported function.
func TygorExport_() *tygorgen.Generator {
	return {{.ExportFunc}}()
}
`
