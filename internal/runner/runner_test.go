package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tygor.dev/internal/discover"
)

func TestExec(t *testing.T) {
	t.Setenv("GOWORK", "off")

	dir := t.TempDir()

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	goMod := `module testapp

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write main.go with unexported setupApp and main()
	mainGo := `package main

import (
	"context"
	"tygor.dev/tygor"
)

type HelloRequest struct {
	Name string
}

type HelloResponse struct {
	Message string
}

func setupApp() *tygor.App {
	app := tygor.NewApp()
	svc := app.Service("Test")
	svc.Register("Hello", tygor.Query(func(_ context.Context, req HelloRequest) (HelloResponse, error) {
		return HelloResponse{Message: "hello " + req.Name}, nil
	}))
	return app
}

func main() {
	setupApp().Handler()
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy go.sum from tygor root to get transitive dependency checksums
	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	// Use go mod download to fetch dependencies without modifying go.sum
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(dir, "rpc")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Run with unexported function name
	output, err := Exec(Options{
		Export: discover.Export{
			Name: "setupApp",
			Type: discover.ExportTypeApp,
		},
		OutDir: outDir,
		PkgDir: dir,
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// Verify output was generated
	typesPath := filepath.Join(outDir, "types.ts")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		t.Errorf("types.ts was not generated")
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest.ts was not generated")
	}

	// Verify types_main.ts contains our types (types from package main go here)
	typesMainPath := filepath.Join(outDir, "types_main.ts")
	content, err := os.ReadFile(typesMainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "HelloRequest") {
		t.Errorf("types_main.ts missing HelloRequest")
	}
	if !strings.Contains(string(content), "HelloResponse") {
		t.Errorf("types_main.ts missing HelloResponse")
	}
}

func TestRemoveMain(t *testing.T) {
	dir := t.TempDir()

	// Write a file with main()
	src := `package main

import "fmt"

func helper() {
	fmt.Println("helper")
}

func main() {
	helper()
}
`
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	hasMain, modified, err := removeMain(file)
	if err != nil {
		t.Fatal(err)
	}

	if !hasMain {
		t.Error("expected hasMain=true")
	}

	// Check that main is removed but helper remains
	modStr := string(modified)
	if strings.Contains(modStr, "func main()") {
		t.Error("main() was not removed")
	}
	if !strings.Contains(modStr, "func helper()") {
		t.Error("helper() was removed")
	}
}

func TestRemoveMainNoMain(t *testing.T) {
	dir := t.TempDir()

	// Write a file without main()
	src := `package main

func helper() {}
`
	file := filepath.Join(dir, "helper.go")
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	hasMain, modified, err := removeMain(file)
	if err != nil {
		t.Fatal(err)
	}

	if hasMain {
		t.Error("expected hasMain=false")
	}
	if modified != nil {
		t.Error("expected nil modified")
	}
}

// TestExecImport_App tests the execImport path with an app-type export.
func TestExecImport_App(t *testing.T) {
	t.Setenv("GOWORK", "off")

	// Create module directory structure
	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "internal", "mylib")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// Create go.mod at module root
	goMod := `module testmodule

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a library package (no main function)
	libGo := `package mylib

import (
	"context"
	"tygor.dev/tygor"
)

type HelloRequest struct {
	Name string
}

type HelloResponse struct {
	Message string
}

func ExportApp() *tygor.App {
	app := tygor.NewApp()
	svc := app.Service("Test")
	svc.Register("Hello", tygor.Query(func(_ context.Context, req HelloRequest) (HelloResponse, error) {
		return HelloResponse{Message: "hello " + req.Name}, nil
	}))
	return app
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "mylib.go"), []byte(libGo), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy go.sum from tygor root
	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	// Download dependencies
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(pkgDir, "rpc")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Run with non-main package (should use execImport)
	output, err := Exec(Options{
		Export: discover.Export{
			Name: "ExportApp",
			Type: discover.ExportTypeApp,
		},
		OutDir:     outDir,
		PkgDir:     pkgDir,
		PkgPath:    "testmodule/internal/mylib",
		ModulePath: "testmodule",
		ModuleDir:  moduleDir,
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// Verify output was generated
	typesPath := filepath.Join(outDir, "types.ts")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		t.Errorf("types.ts was not generated")
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest.ts was not generated")
	}

	// Verify types file contains our types
	// The types file is named based on the full module path
	typesFile := filepath.Join(outDir, "types_testmodule_internal_mylib.ts")
	content, err := os.ReadFile(typesFile)
	if err != nil {
		// List what files were actually generated for debugging
		files, _ := filepath.Glob(filepath.Join(outDir, "*.ts"))
		t.Fatalf("failed to read types file: %v\nGenerated files: %v", err, files)
	}
	if !strings.Contains(string(content), "HelloRequest") {
		t.Errorf("types file missing HelloRequest")
	}
	if !strings.Contains(string(content), "HelloResponse") {
		t.Errorf("types file missing HelloResponse")
	}
}

// TestExecImport_Generator tests the execImport path with a generator-type export.
func TestExecImport_Generator(t *testing.T) {
	t.Setenv("GOWORK", "off")

	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "gen")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	goMod := `module testgen

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a library package with generator export
	genGo := `package gen

import (
	"context"
	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

type Request struct {
	Value int
}

type Response struct {
	Result int
}

func MyGenerator() *tygorgen.Generator {
	app := tygor.NewApp()
	svc := app.Service("Math")
	svc.Register("Double", tygor.Query(func(_ context.Context, req Request) (Response, error) {
		return Response{Result: req.Value * 2}, nil
	}))
	return tygorgen.FromApp(app)
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "gen.go"), []byte(genGo), 0644); err != nil {
		t.Fatal(err)
	}

	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(pkgDir, "output")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	output, err := Exec(Options{
		Export: discover.Export{
			Name: "MyGenerator",
			Type: discover.ExportTypeGenerator,
		},
		OutDir:     outDir,
		PkgDir:     pkgDir,
		PkgPath:    "testgen/gen",
		ModulePath: "testgen",
		ModuleDir:  moduleDir,
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// Verify output was generated
	if _, err := os.Stat(filepath.Join(outDir, "types.ts")); os.IsNotExist(err) {
		t.Errorf("types.ts was not generated")
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest.ts")); os.IsNotExist(err) {
		t.Errorf("manifest.ts was not generated")
	}
}

// TestExecImport_WithFlavor tests the execImport path with flavor option.
func TestExecImport_WithFlavor(t *testing.T) {
	t.Setenv("GOWORK", "off")

	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "api")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	goMod := `module testflavor

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	apiGo := `package api

import (
	"context"
	"tygor.dev/tygor"
)

type Data struct {
	Value string
}

func GetApp() *tygor.App {
	app := tygor.NewApp()
	svc := app.Service("API")
	svc.Register("GetData", tygor.Query(func(_ context.Context, _ struct{}) (Data, error) {
		return Data{Value: "test"}, nil
	}))
	return app
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "api.go"), []byte(apiGo), 0644); err != nil {
		t.Fatal(err)
	}

	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(pkgDir, "ts")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	output, err := Exec(Options{
		Export: discover.Export{
			Name: "GetApp",
			Type: discover.ExportTypeApp,
		},
		OutDir:     outDir,
		PkgDir:     pkgDir,
		PkgPath:    "testflavor/api",
		ModulePath: "testflavor",
		ModuleDir:  moduleDir,
		Flavor:     "zod",
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// Verify Zod output was generated
	if _, err := os.Stat(filepath.Join(outDir, "types.ts")); os.IsNotExist(err) {
		t.Errorf("types.ts was not generated")
	}
}

// TestExecImport_CheckMode tests the execImport path in check mode.
func TestExecImport_CheckMode(t *testing.T) {
	t.Setenv("GOWORK", "off")

	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "check")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	goMod := `module testcheck

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	checkGo := `package check

import (
	"context"
	"tygor.dev/tygor"
)

type Input struct {
	X int
}

type Output struct {
	Y int
}

func CheckApp() *tygor.App {
	app := tygor.NewApp()
	svc := app.Service("Checker")
	svc.Register("Process", tygor.Query(func(_ context.Context, in Input) (Output, error) {
		return Output{Y: in.X * 2}, nil
	}))
	return app
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "check.go"), []byte(checkGo), 0644); err != nil {
		t.Fatal(err)
	}

	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(pkgDir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	output, err := Exec(Options{
		Export: discover.Export{
			Name: "CheckApp",
			Type: discover.ExportTypeApp,
		},
		OutDir:     outDir,
		PkgDir:     pkgDir,
		PkgPath:    "testcheck/check",
		ModulePath: "testcheck",
		ModuleDir:  moduleDir,
		CheckMode:  true,
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// In check mode, should output stats (services endpoints types)
	outputStr := string(output)
	if !strings.Contains(outputStr, "1 1 2") {
		t.Errorf("expected stats '1 1 2' (1 service, 1 endpoint, 2 types), got: %s", outputStr)
	}

	// Verify no files were generated in check mode
	files, err := filepath.Glob(filepath.Join(outDir, "*.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) > 0 {
		t.Errorf("check mode should not generate files, but found: %v", files)
	}
}

// TestExecImport_Unexported tests the execImport path with an unexported function.
// This exercises the shim generation code path.
func TestExecImport_Unexported(t *testing.T) {
	t.Setenv("GOWORK", "off")

	// Create module directory structure
	moduleDir := t.TempDir()
	pkgDir := filepath.Join(moduleDir, "api")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tygorRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// Create go.mod at module root
	goMod := `module testunexported

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a library package with unexported export function
	apiGo := `package api

import (
	"context"
	"tygor.dev/tygor"
)

type PingRequest struct{}

type PingResponse struct {
	Message string
}

// unexported - tests shim generation
func setupApp() *tygor.App {
	app := tygor.NewApp()
	svc := app.Service("API")
	svc.Register("Ping", tygor.Query(func(_ context.Context, req PingRequest) (PingResponse, error) {
		return PingResponse{Message: "pong"}, nil
	}))
	return app
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "api.go"), []byte(apiGo), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy go.sum from tygor root
	goSum, err := os.ReadFile(filepath.Join(tygorRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}

	// Download dependencies
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	outDir := filepath.Join(pkgDir, "rpc")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Run with unexported function name (should use shim)
	output, err := Exec(Options{
		Export: discover.Export{
			Name: "setupApp", // lowercase = unexported
			Type: discover.ExportTypeApp,
		},
		OutDir:     outDir,
		PkgDir:     pkgDir,
		PkgPath:    "testunexported/api",
		ModulePath: "testunexported",
		ModuleDir:  moduleDir,
	})
	if err != nil {
		t.Fatalf("Exec() error: %v\nOutput: %s", err, output)
	}

	// Verify output was generated
	typesPath := filepath.Join(outDir, "types.ts")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		t.Errorf("types.ts was not generated")
	}

	manifestPath := filepath.Join(outDir, "manifest.ts")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest.ts was not generated")
	}

	// Verify types file contains our types
	typesFile := filepath.Join(outDir, "types_testunexported_api.ts")
	content, err := os.ReadFile(typesFile)
	if err != nil {
		files, _ := filepath.Glob(filepath.Join(outDir, "*.ts"))
		t.Fatalf("failed to read types file: %v\nGenerated files: %v", err, files)
	}
	if !strings.Contains(string(content), "PingRequest") {
		t.Errorf("types file missing PingRequest")
	}
	if !strings.Contains(string(content), "PingResponse") {
		t.Errorf("types file missing PingResponse")
	}
}

// TestExecImport_MissingPkgPath tests error handling when PkgPath is missing.
func TestExecImport_MissingPkgPath(t *testing.T) {
	dir := t.TempDir()

	_, err := Exec(Options{
		Export: discover.Export{
			Name: "SomeFunc",
			Type: discover.ExportTypeApp,
		},
		OutDir:    filepath.Join(dir, "out"),
		PkgDir:    dir,
		ModuleDir: dir,
		// PkgPath intentionally omitted
	})

	if err == nil {
		t.Fatal("expected error for missing PkgPath")
	}
	if !strings.Contains(err.Error(), "PkgPath required") {
		t.Errorf("expected 'PkgPath required' error, got: %v", err)
	}
}

// TestExecImport_MissingModuleDir tests error handling when ModuleDir is missing.
func TestExecImport_MissingModuleDir(t *testing.T) {
	dir := t.TempDir()

	_, err := Exec(Options{
		Export: discover.Export{
			Name: "SomeFunc",
			Type: discover.ExportTypeApp,
		},
		OutDir:  filepath.Join(dir, "out"),
		PkgDir:  dir,
		PkgPath: "test/pkg",
		// ModuleDir intentionally omitted
	})

	if err == nil {
		t.Fatal("expected error for missing ModuleDir")
	}
	if !strings.Contains(err.Error(), "ModuleDir required") {
		t.Errorf("expected 'ModuleDir required' error, got: %v", err)
	}
}

// TestGenerateImportRunner tests the generateImportRunner function directly.
func TestGenerateImportRunner(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		wantImport string
		wantCall   string
	}{
		{
			name: "app runner",
			opts: Options{
				Export: discover.Export{
					Name: "MyApp",
					Type: discover.ExportTypeApp,
				},
				PkgPath: "example.com/myapp",
				OutDir:  "/tmp/out",
			},
			wantImport: `pkg "example.com/myapp"`,
			wantCall:   "pkg.MyApp()",
		},
		{
			name: "generator runner",
			opts: Options{
				Export: discover.Export{
					Name: "GenFunc",
					Type: discover.ExportTypeGenerator,
				},
				PkgPath: "example.com/gen",
				OutDir:  "/tmp/out",
			},
			wantImport: `pkg "example.com/gen"`,
			wantCall:   "pkg.GenFunc()",
		},
		{
			name: "app with flavor",
			opts: Options{
				Export: discover.Export{
					Name: "GetApp",
					Type: discover.ExportTypeApp,
				},
				PkgPath: "example.com/api",
				OutDir:  "/tmp/out",
				Flavor:  "zod",
			},
			wantImport: `pkg "example.com/api"`,
			wantCall:   "pkg.GetApp()",
		},
		{
			name: "app check mode",
			opts: Options{
				Export: discover.Export{
					Name: "TheApp",
					Type: discover.ExportTypeApp,
				},
				PkgPath:   "example.com/check",
				OutDir:    "/tmp/out",
				CheckMode: true,
			},
			wantImport: `pkg "example.com/check"`,
			wantCall:   "pkg.TheApp()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := generateImportRunner(tt.opts)
			if err != nil {
				t.Fatalf("generateImportRunner() error: %v", err)
			}

			srcStr := string(src)

			if !strings.Contains(srcStr, tt.wantImport) {
				t.Errorf("expected import %q in generated code", tt.wantImport)
			}

			if !strings.Contains(srcStr, tt.wantCall) {
				t.Errorf("expected call %q in generated code", tt.wantCall)
			}

			if !strings.Contains(srcStr, "package main") {
				t.Error("expected package main declaration")
			}

			if !strings.Contains(srcStr, "func main()") {
				t.Error("expected main function")
			}
		})
	}
}
