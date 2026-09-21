package gen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareFilesUsesRecursiveOwnership(t *testing.T) {
	cmd := &Cmd{}

	t.Run("requires manifest", func(t *testing.T) {
		generated := t.TempDir()
		output := t.TempDir()
		writeTestFile(t, generated, "types.ts", "same")
		writeTestFile(t, output, "types.ts", "same")

		err := cmd.compareFiles(generated, output)
		if err == nil || !strings.Contains(err.Error(), "ownership manifest") {
			t.Fatalf("compareFiles error = %v, want migration diagnostic", err)
		}
	})

	t.Run("finds nested change", func(t *testing.T) {
		generated := t.TempDir()
		output := t.TempDir()
		writeTestFile(t, generated, "nested/types.ts", "new")
		writeTestFile(t, output, "nested/types.ts", "old")
		writeTestManifest(t, output, []string{"nested/types.ts"})

		if err := cmd.compareFiles(generated, output); err == nil {
			t.Fatal("compareFiles accepted changed nested output")
		}
	})

	t.Run("finds obsolete owned file but ignores user file", func(t *testing.T) {
		generated := t.TempDir()
		output := t.TempDir()
		writeTestFile(t, generated, "current.ts", "current")
		writeTestFile(t, output, "current.ts", "current")
		writeTestFile(t, output, "obsolete.ts", "old")
		writeTestFile(t, output, "notes.md", "user")
		writeTestManifest(t, output, []string{"current.ts", "obsolete.ts"})

		if err := cmd.compareFiles(generated, output); err == nil {
			t.Fatal("compareFiles accepted obsolete owned output")
		}

		writeTestManifest(t, output, []string{"current.ts"})
		if err := cmd.compareFiles(generated, output); err != nil {
			t.Fatalf("compareFiles rejected unowned user file: %v", err)
		}
	})
}

func TestReconcileFilesMigratesAndMaintainsOwnership(t *testing.T) {
	cmd := &Cmd{}
	output := t.TempDir()
	generated := t.TempDir()
	writeTestFile(t, output, "legacy/obsolete.ts", generatedFileMarker+"\nold")
	writeTestFile(t, output, "notes.md", "user content")
	writeTestFile(t, output, "discovery.json", `{"user":"unmarked legacy files are not adopted"}`)
	writeTestFile(t, generated, "nested/current.ts", generatedFileMarker+"\ncurrent")

	if err := cmd.reconcileFiles(generated, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "legacy", "obsolete.ts")); !os.IsNotExist(err) {
		t.Fatalf("legacy generated file was not removed: %v", err)
	}
	assertTestFile(t, output, "nested/current.ts", generatedFileMarker+"\ncurrent")
	assertTestFile(t, output, "notes.md", "user content")
	assertTestFile(t, output, "discovery.json", `{"user":"unmarked legacy files are not adopted"}`)
	if err := cmd.compareFiles(generated, output); err != nil {
		t.Fatalf("freshly reconciled output is stale: %v", err)
	}

	next := t.TempDir()
	writeTestFile(t, next, "types.ts", generatedFileMarker+"\nnext")
	if err := cmd.reconcileFiles(next, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "nested", "current.ts")); !os.IsNotExist(err) {
		t.Fatalf("obsolete nested output was not removed: %v", err)
	}
	assertTestFile(t, output, "types.ts", generatedFileMarker+"\nnext")
	assertTestFile(t, output, "notes.md", "user content")
	if err := cmd.compareFiles(next, output); err != nil {
		t.Fatalf("second reconciliation is stale: %v", err)
	}
}

func TestReconcileFilesMigrationPreservesNestedOwnedOutput(t *testing.T) {
	cmd := &Cmd{}
	output := t.TempDir()
	generated := t.TempDir()
	writeTestFile(t, output, "old.ts", generatedFileMarker+"\nold")
	writeTestFile(t, output, "nested/child.ts", generatedFileMarker+"\nchild")
	writeTestFile(t, output, "nested/discovery.json", generatedFileMarker+"\nchild discovery")
	writeTestManifest(t, filepath.Join(output, "nested"), []string{"child.ts", "discovery.json"})
	writeTestFile(t, generated, "current.ts", generatedFileMarker+"\ncurrent")

	if err := cmd.reconcileFiles(generated, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "old.ts")); !os.IsNotExist(err) {
		t.Fatalf("legacy root output was not removed: %v", err)
	}
	assertTestFile(t, output, "nested/child.ts", generatedFileMarker+"\nchild")
	assertTestFile(t, output, "nested/discovery.json", generatedFileMarker+"\nchild discovery")
	if _, found, err := readOwnershipManifest(filepath.Join(output, "nested")); err != nil || !found {
		t.Fatalf("nested ownership manifest = found %v, error %v", found, err)
	}
}

func TestReconcileFilesMigrationPreservesCompilerDerivatives(t *testing.T) {
	cmd := &Cmd{}
	output := t.TempDir()
	generated := t.TempDir()
	compiledJS := generatedFileMarker + "\nexport {};\n"
	declaration := generatedFileMarker + "\nexport interface Generated {}\n"
	writeTestFile(t, output, "types.ts", generatedFileMarker+"\nold")
	writeTestFile(t, output, "types.js", compiledJS)
	writeTestFile(t, output, "types.d.ts", declaration)
	writeTestFile(t, output, "legacy/types_removed_pkg.ts", generatedFileMarker+"\nold package")
	writeTestFile(t, output, "schemas.zod.ts", generatedFileMarker+"\nold schema")
	writeTestFile(t, output, "user.ts", "export const userOwned = true;\n")
	writeTestFile(t, generated, "types.ts", generatedFileMarker+"\ncurrent")

	if err := cmd.reconcileFiles(generated, output); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, output, "types.ts", generatedFileMarker+"\ncurrent")
	assertTestFile(t, output, "types.js", compiledJS)
	assertTestFile(t, output, "types.d.ts", declaration)
	assertTestFile(t, output, "user.ts", "export const userOwned = true;\n")
	for _, stale := range []string{"legacy/types_removed_pkg.ts", "schemas.zod.ts"} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(stale))); !os.IsNotExist(err) {
			t.Fatalf("legacy generated file %s was not removed: %v", stale, err)
		}
	}
	manifest, found, err := readOwnershipManifest(output)
	if err != nil || !found {
		t.Fatalf("readOwnershipManifest() = (%v, %v), want manifest", found, err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0] != "types.ts" {
		t.Fatalf("owned files = %v, want [types.ts]", manifest.Files)
	}
	if err := cmd.compareFiles(generated, output); err != nil {
		t.Fatalf("freshly reconciled output is stale: %v", err)
	}
}

func TestReadOwnershipManifestRejectsUnsafePaths(t *testing.T) {
	output := t.TempDir()
	writeTestManifest(t, output, []string{"../outside.ts"})
	if _, _, err := readOwnershipManifest(output); err == nil {
		t.Fatal("readOwnershipManifest accepted path traversal")
	}
}

func TestRemoveOwnedFilesRejectsSymlinkedDirectories(t *testing.T) {
	tests := []struct {
		name       string
		ownedPath  string
		linkPath   string
		targetPath func(root, output string) string
	}{
		{
			name:      "external target",
			ownedPath: "link/victim.ts",
			linkPath:  "link",
			targetPath: func(root, _ string) string {
				return filepath.Join(root, "external")
			},
		},
		{
			name:      "in-root target",
			ownedPath: "link/victim.ts",
			linkPath:  "link",
			targetPath: func(_, output string) string {
				return filepath.Join(output, "user-files")
			},
		},
		{
			name:      "intermediate parent",
			ownedPath: "generated/link/victim.ts",
			linkPath:  "generated/link",
			targetPath: func(root, _ string) string {
				return filepath.Join(root, "external")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			output := filepath.Join(root, "output")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(output, filepath.FromSlash(tt.linkPath))), 0o755); err != nil {
				t.Fatal(err)
			}
			target := tt.targetPath(root, output)
			writeTestFile(t, target, "victim.ts", "preserve victim")
			writeTestFile(t, target, "unrelated.txt", "preserve unrelated")
			link := filepath.Join(output, filepath.FromSlash(tt.linkPath))
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			err := removeOwnedFiles(output, []string{tt.ownedPath})
			if err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("removeOwnedFiles() error = %v, want symbolic-link rejection", err)
			}
			assertTestFile(t, target, "victim.ts", "preserve victim")
			assertTestFile(t, target, "unrelated.txt", "preserve unrelated")
			info, statErr := os.Lstat(link)
			if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlink changed after rejected cleanup: info=%v error=%v", info, statErr)
			}
		})
	}
}

func writeTestManifest(t *testing.T, root string, files []string) {
	t.Helper()
	data, err := json.Marshal(ownershipManifest{Version: ownershipVersion, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, ownershipManifestName, string(data))
}

func writeTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, root, path, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, content, want)
	}
}
