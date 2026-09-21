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
	"strconv"
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

	// PackageName is the package clause name reported by go/packages.
	PackageName string

	// CompiledGoFiles is the active file set selected by go/packages for the
	// current GOOS, GOARCH, and build tags.
	CompiledGoFiles []string

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
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if opts.PackageName == "main" {
		return execOverlay(opts)
	}
	return execImport(opts)
}

func validateOptions(opts Options) error {
	if !token.IsIdentifier(opts.Export.Name) {
		return fmt.Errorf("invalid export function name %q", opts.Export.Name)
	}
	if opts.ConfigFunc != "" && !token.IsIdentifier(opts.ConfigFunc) {
		return fmt.Errorf("invalid config function name %q", opts.ConfigFunc)
	}
	if !token.IsIdentifier(opts.PackageName) {
		return fmt.Errorf("invalid package name %q", opts.PackageName)
	}
	if opts.Flavor != "" && opts.Flavor != "zod" && opts.Flavor != "zod-mini" {
		return fmt.Errorf("invalid flavor %q: expected zod or zod-mini", opts.Flavor)
	}
	if opts.Export.Type == discover.ExportTypeGenerator && opts.ConfigFunc != "" && !opts.NoConfig {
		return fmt.Errorf("config function is only valid for *tygor.App exports")
	}
	if opts.PackageName == "main" && len(opts.CompiledGoFiles) == 0 {
		return fmt.Errorf("CompiledGoFiles required for package main")
	}
	return nil
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

	for _, file := range opts.CompiledGoFiles {
		if !filepath.IsAbs(file) {
			file = filepath.Join(opts.PkgDir, file)
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

	privateExport := !ast.IsExported(opts.Export.Name)
	privateConfig := opts.Export.Type == discover.ExportTypeApp && opts.ConfigFunc != "" && !opts.NoConfig && !ast.IsExported(opts.ConfigFunc)
	if privateExport || privateConfig {
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

	}

	if privateExport {
		opts.Export.Name = "TygorExport_"
	}
	if privateConfig {
		opts.ConfigFunc = "TygorConfig_"
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
	if !token.IsIdentifier(opts.Export.Name) {
		return nil, fmt.Errorf("invalid export function name %q", opts.Export.Name)
	}
	if opts.ConfigFunc != "" && !token.IsIdentifier(opts.ConfigFunc) {
		return nil, fmt.Errorf("invalid config function name %q", opts.ConfigFunc)
	}

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
	flavorLiteral := ""
	if opts.Flavor != "" {
		flavorLiteral = strconv.Quote(opts.Flavor)
	}

	data := struct {
		ExportFunc    string
		OutDirLiteral string
		FlavorLiteral string
		Discovery     bool
		ConfigFunc    string
	}{
		ExportFunc:    opts.Export.Name,
		OutDirLiteral: strconv.Quote(opts.OutDir),
		FlavorLiteral: flavorLiteral,
		Discovery:     opts.Discovery,
		ConfigFunc:    configFunc,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated runner: %w", err)
	}
	return formatted, nil
}

const appRunnerTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
)

func main() {
	g := tygorgen.FromApp({{.ExportFunc}}())
{{if .FlavorLiteral}}
	g = g.WithFlavor(tygorgen.Flavor({{.FlavorLiteral}}))
{{end}}
{{if .Discovery}}
	g = g.WithDiscovery()
{{end}}
{{if .ConfigFunc}}
	g = {{.ConfigFunc}}(g)
{{end}}
	result, err := g.ToDir({{.OutDirLiteral}})
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
	result, err := g.ToDir({{.OutDirLiteral}})
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
	if !token.IsIdentifier(opts.Export.Name) {
		return nil, fmt.Errorf("invalid export function name %q", opts.Export.Name)
	}
	if opts.ConfigFunc != "" && !token.IsIdentifier(opts.ConfigFunc) {
		return nil, fmt.Errorf("invalid config function name %q", opts.ConfigFunc)
	}

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
	flavorLiteral := ""
	if opts.Flavor != "" {
		flavorLiteral = strconv.Quote(opts.Flavor)
	}

	data := struct {
		PkgPathLiteral string
		ExportFunc     string
		OutDirLiteral  string
		FlavorLiteral  string
		Discovery      bool
		ConfigFunc     string
	}{
		PkgPathLiteral: strconv.Quote(opts.PkgPath),
		ExportFunc:     opts.Export.Name,
		OutDirLiteral:  strconv.Quote(opts.OutDir),
		FlavorLiteral:  flavorLiteral,
		Discovery:      opts.Discovery,
		ConfigFunc:     configFunc,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated import runner: %w", err)
	}
	return formatted, nil
}

const importAppRunnerTemplate = `package main

import (
	"fmt"
	"os"

	"tygor.dev/tygorgen"
	pkg {{.PkgPathLiteral}}
)

func main() {
	g := tygorgen.FromApp(pkg.{{.ExportFunc}}())
{{if .FlavorLiteral}}
	g = g.WithFlavor(tygorgen.Flavor({{.FlavorLiteral}}))
{{end}}
{{if .Discovery}}
	g = g.WithDiscovery()
{{end}}
{{if .ConfigFunc}}
	g = pkg.{{.ConfigFunc}}(g)
{{end}}
	result, err := g.ToDir({{.OutDirLiteral}})
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

	pkg {{.PkgPathLiteral}}
)

func main() {
	g := pkg.{{.ExportFunc}}()
	result, err := g.ToDir({{.OutDirLiteral}})
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
	pkg {{.PkgPathLiteral}}
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

	pkg {{.PkgPathLiteral}}
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

// generateShim exports private app/generator and config functions independently.
func generateShim(opts Options) ([]byte, error) {
	if !token.IsIdentifier(opts.PackageName) {
		return nil, fmt.Errorf("invalid package name %q", opts.PackageName)
	}
	privateExport := !ast.IsExported(opts.Export.Name)
	privateConfig := opts.Export.Type == discover.ExportTypeApp && opts.ConfigFunc != "" && !opts.NoConfig && !ast.IsExported(opts.ConfigFunc)
	if privateExport && !token.IsIdentifier(opts.Export.Name) {
		return nil, fmt.Errorf("invalid export function name %q", opts.Export.Name)
	}
	if privateConfig && !token.IsIdentifier(opts.ConfigFunc) {
		return nil, fmt.Errorf("invalid config function name %q", opts.ConfigFunc)
	}

	tmpl, err := template.New("shim").Parse(shimTemplate)
	if err != nil {
		return nil, err
	}

	data := struct {
		PkgName         string
		AppExport       string
		GeneratorExport string
		ConfigFunc      string
		NeedsTygor      bool
		NeedsTygorgen   bool
	}{
		PkgName:       opts.PackageName,
		ConfigFunc:    opts.ConfigFunc,
		NeedsTygor:    privateExport && opts.Export.Type == discover.ExportTypeApp,
		NeedsTygorgen: privateConfig || privateExport && opts.Export.Type == discover.ExportTypeGenerator,
	}
	if privateExport && opts.Export.Type == discover.ExportTypeApp {
		data.AppExport = opts.Export.Name
	} else if privateExport && opts.Export.Type == discover.ExportTypeGenerator {
		data.GeneratorExport = opts.Export.Name
	} else if privateExport {
		return nil, fmt.Errorf("unknown export type: %v", opts.Export.Type)
	}
	if !privateConfig {
		data.ConfigFunc = ""
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated shim: %w", err)
	}
	return formatted, nil
}

const shimTemplate = `package {{.PkgName}}
{{if .NeedsTygor}}
import "tygor.dev/tygor"
{{end}}
{{if .NeedsTygorgen}}
import "tygor.dev/tygorgen"
{{end}}
{{if .AppExport}}
// TygorExport_ is a generated wrapper to export the unexported function.
func TygorExport_() *tygor.App {
	return {{.AppExport}}()
}
{{end}}
{{if .GeneratorExport}}
// TygorExport_ is a generated wrapper to export the unexported function.
func TygorExport_() *tygorgen.Generator {
	return {{.GeneratorExport}}()
}
{{end}}
{{if .ConfigFunc}}
// TygorConfig_ is a generated wrapper to export the unexported config function.
func TygorConfig_(g *tygorgen.Generator) *tygorgen.Generator {
	return {{.ConfigFunc}}(g)
}
{{end}}
`
