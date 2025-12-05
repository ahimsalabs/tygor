package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFind(t *testing.T) {
	t.Setenv("GOWORK", "off")

	tests := []struct {
		name        string
		files       map[string]string
		wantExports []struct {
			name       string
			exportType ExportType
		}
		wantErr string
	}{
		{
			name: "single app export",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
			},
			wantExports: []struct {
				name       string
				exportType ExportType
			}{
				{name: "SetupApp", exportType: ExportTypeApp},
			},
		},
		{
			name: "single generator export",
			files: map[string]string{
				"main.go": `package main

import (
	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

func Gen() *tygorgen.Generator {
	return tygorgen.FromApp(SetupApp())
}

func main() {}
`,
			},
			wantExports: []struct {
				name       string
				exportType ExportType
			}{
				{name: "Gen", exportType: ExportTypeGenerator},
				{name: "SetupApp", exportType: ExportTypeApp},
			},
		},
		{
			name: "no exports",
			files: map[string]string{
				"main.go": `package main

func main() {}
`,
			},
			wantExports: nil,
		},
		{
			name: "ignores methods",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

type Builder struct{}

func (b *Builder) Build() *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
			},
			wantExports: nil,
		},
		{
			name: "ignores functions with parameters",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

func SetupApp(name string) *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
			},
			wantExports: nil,
		},
		{
			name: "finds unexported functions",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

func setupApp() *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
			},
			wantExports: []struct {
				name       string
				exportType ExportType
			}{
				{name: "setupApp", exportType: ExportTypeApp},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			tygorRoot, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}

			goMod := `module test

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
				t.Fatal(err)
			}

			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy: %v\n%s", err, out)
			}

			result, err := FindDir(".", dir)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result.Exports) != len(tt.wantExports) {
				t.Fatalf("got %d exports, want %d", len(result.Exports), len(tt.wantExports))
			}

			gotExports := make(map[string]ExportType)
			for _, e := range result.Exports {
				gotExports[e.Name] = e.Type
			}

			for _, want := range tt.wantExports {
				gotType, ok := gotExports[want.name]
				if !ok {
					t.Errorf("missing export %s", want.name)
					continue
				}
				if gotType != want.exportType {
					t.Errorf("export %s: got type %v, want %v", want.name, gotType, want.exportType)
				}
			}
		})
	}
}

func TestFindConfigFunc(t *testing.T) {
	t.Setenv("GOWORK", "off")

	tests := []struct {
		name           string
		files          map[string]string
		wantConfigFunc string // empty if none expected
	}{
		{
			name: "config in same file as export",
			files: map[string]string{
				"main.go": `package main

import (
	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

func Configure(g *tygorgen.Generator) *tygorgen.Generator {
	return g
}

func main() {}
`,
			},
			wantConfigFunc: "Configure",
		},
		{
			name: "config in separate file",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
				"config.go": `package main

import "tygor.dev/tygorgen"

func MyConfig(g *tygorgen.Generator) *tygorgen.Generator {
	return g
}
`,
			},
			wantConfigFunc: "MyConfig",
		},
		{
			name: "no config function",
			files: map[string]string{
				"main.go": `package main

import "tygor.dev/tygor"

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

func main() {}
`,
			},
			wantConfigFunc: "",
		},
		{
			name: "method is not config function",
			files: map[string]string{
				"main.go": `package main

import (
	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

func SetupApp() *tygor.App {
	return tygor.NewApp()
}

type Configurer struct{}

func (c *Configurer) Configure(g *tygorgen.Generator) *tygorgen.Generator {
	return g
}

func main() {}
`,
			},
			wantConfigFunc: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			tygorRoot, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}

			goMod := `module test

go 1.21

require tygor.dev v0.7.4

replace tygor.dev => ` + tygorRoot + `
`
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
				t.Fatal(err)
			}

			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy: %v\n%s", err, out)
			}

			result, err := FindDir(".", dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantConfigFunc == "" {
				if result.ConfigFunc != nil {
					t.Errorf("got config func %s, want none", result.ConfigFunc.Name)
				}
			} else {
				if result.ConfigFunc == nil {
					t.Errorf("got no config func, want %s", tt.wantConfigFunc)
				} else if result.ConfigFunc.Name != tt.wantConfigFunc {
					t.Errorf("got config func %s, want %s", result.ConfigFunc.Name, tt.wantConfigFunc)
				}
			}
		})
	}
}

func TestSelectExport(t *testing.T) {
	exports := []Export{
		{Name: "SetupApp", Type: ExportTypeApp},
		{Name: "SetupAdmin", Type: ExportTypeApp},
	}

	t.Run("single export no name", func(t *testing.T) {
		single := []Export{{Name: "SetupApp", Type: ExportTypeApp}}
		exp, err := SelectExport(single, "")
		if err != nil {
			t.Fatal(err)
		}
		if exp.Name != "SetupApp" {
			t.Errorf("got %s, want SetupApp", exp.Name)
		}
	})

	t.Run("multiple exports no name", func(t *testing.T) {
		_, err := SelectExport(exports, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !contains(err.Error(), "multiple exports") {
			t.Errorf("expected 'multiple exports' in error, got %q", err.Error())
		}
	})

	t.Run("multiple exports with name", func(t *testing.T) {
		exp, err := SelectExport(exports, "SetupAdmin")
		if err != nil {
			t.Fatal(err)
		}
		if exp.Name != "SetupAdmin" {
			t.Errorf("got %s, want SetupAdmin", exp.Name)
		}
	})

	t.Run("no exports", func(t *testing.T) {
		_, err := SelectExport(nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !contains(err.Error(), "no export found") {
			t.Errorf("expected 'no export found' in error, got %q", err.Error())
		}
	})

	t.Run("name not found", func(t *testing.T) {
		_, err := SelectExport(exports, "NotHere")
		if err == nil {
			t.Fatal("expected error")
		}
		if !contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got %q", err.Error())
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
