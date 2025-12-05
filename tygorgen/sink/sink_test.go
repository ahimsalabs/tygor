package sink

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestValidatePath tests the ValidatePath function
func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple path",
			path:    "foo/bar.ts",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			path:    "a/b/c/d/file.txt",
			wantErr: false,
		},
		{
			name:    "valid single file",
			path:    "file.txt",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "absolute path with leading slash",
			path:    "/absolute/path.txt",
			wantErr: true,
			errMsg:  "absolute paths not allowed",
		},
		{
			name:    "path traversal with ..",
			path:    "foo/../bar.txt",
			wantErr: true,
			errMsg:  "path traversal not allowed",
		},
		{
			name:    "path starting with ..",
			path:    "../foo/bar.txt",
			wantErr: true,
			errMsg:  "path traversal not allowed",
		},
		{
			name:    "path with just ..",
			path:    "..",
			wantErr: true,
			errMsg:  "path traversal not allowed",
		},
		{
			name:    "path with current dir prefix",
			path:    "./foo/bar.txt",
			wantErr: true,
			errMsg:  "not clean",
		},
		{
			name:    "path with double slashes",
			path:    "foo//bar.txt",
			wantErr: true,
			errMsg:  "not clean",
		},
		{
			name:    "path with trailing slash",
			path:    "foo/bar/",
			wantErr: true,
			errMsg:  "not clean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidatePath() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

// TestMemorySink tests the MemorySink implementation
func TestMemorySink(t *testing.T) {
	t.Run("basic write and read", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		content := []byte("hello world")
		err := sink.WriteFile(ctx, "test.txt", content)
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		got := sink.Get("test.txt")
		if string(got) != string(content) {
			t.Errorf("Get() = %q, want %q", got, content)
		}
	})

	t.Run("get non-existent file", func(t *testing.T) {
		sink := NewMemorySink()
		got := sink.Get("nonexistent.txt")
		if got != nil {
			t.Errorf("Get() = %v, want nil", got)
		}
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		// Write initial content
		err := sink.WriteFile(ctx, "test.txt", []byte("first"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Overwrite
		err = sink.WriteFile(ctx, "test.txt", []byte("second"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		got := sink.Get("test.txt")
		if string(got) != "second" {
			t.Errorf("Get() = %q, want %q", got, "second")
		}
	})

	t.Run("Files returns copy", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		err := sink.WriteFile(ctx, "a.txt", []byte("aaa"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		err = sink.WriteFile(ctx, "b.txt", []byte("bbb"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		files := sink.Files()
		if len(files) != 2 {
			t.Errorf("Files() length = %d, want 2", len(files))
		}

		// Modify the returned map
		files["c.txt"] = []byte("ccc")

		// Original should not be affected
		files2 := sink.Files()
		if len(files2) != 2 {
			t.Errorf("Files() after modification length = %d, want 2", len(files2))
		}
	})

	t.Run("Get returns copy", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		original := []byte("original")
		err := sink.WriteFile(ctx, "test.txt", original)
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		got := sink.Get("test.txt")
		// Modify the returned slice
		got[0] = 'X'

		// Get again and verify original is unchanged
		got2 := sink.Get("test.txt")
		if string(got2) != "original" {
			t.Errorf("Get() = %q, want %q (modification leaked)", got2, "original")
		}
	})

	t.Run("Reset clears all files", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		err := sink.WriteFile(ctx, "a.txt", []byte("aaa"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		err = sink.WriteFile(ctx, "b.txt", []byte("bbb"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		sink.Reset()

		files := sink.Files()
		if len(files) != 0 {
			t.Errorf("Files() after Reset() length = %d, want 0", len(files))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		sink := NewMemorySink()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with cancelled context should return error")
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		sink := NewMemorySink()
		ctx := context.Background()

		err := sink.WriteFile(ctx, "../escape.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with invalid path should return error")
		}
	})
}

// TestMemorySink_Concurrent tests concurrent access to MemorySink
func TestMemorySink_Concurrent(t *testing.T) {
	sink := NewMemorySink()
	ctx := context.Background()

	const numGoroutines = 100
	const numWrites = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numWrites; j++ {
				path := filepath.Join("dir", "file"+string(rune('0'+id))+".txt")
				content := []byte("content from goroutine " + string(rune('0'+id)))
				if err := sink.WriteFile(ctx, path, content); err != nil {
					t.Errorf("WriteFile() error = %v", err)
				}
			}
		}(i)
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numWrites; j++ {
				_ = sink.Files()
				_ = sink.Get("dir/file0.txt")
			}
		}()
	}

	wg.Wait()

	// Verify some files were written
	files := sink.Files()
	if len(files) == 0 {
		t.Error("No files written during concurrent test")
	}
}

// TestFilesystemSink tests the FilesystemSink implementation
func TestFilesystemSink(t *testing.T) {
	t.Run("basic write and read", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		content := []byte("hello world")
		err := sink.WriteFile(ctx, "test.txt", content)
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Verify file exists and has correct content
		path := filepath.Join(tmpDir, "test.txt")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("ReadFile() = %q, want %q", got, content)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		err := sink.WriteFile(ctx, "a/b/c/test.txt", []byte("nested"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		path := filepath.Join(tmpDir, "a", "b", "c", "test.txt")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != "nested" {
			t.Errorf("ReadFile() = %q, want %q", got, "nested")
		}
	})

	t.Run("respects file mode", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		sink.Mode = 0600
		ctx := context.Background()

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		path := filepath.Join(tmpDir, "test.txt")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}

		mode := info.Mode().Perm()
		if mode != 0600 {
			t.Errorf("File mode = %o, want %o", mode, 0600)
		}
	})

	t.Run("uses default mode when Mode is zero", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		sink.Mode = 0 // Explicitly set to zero
		ctx := context.Background()

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		path := filepath.Join(tmpDir, "test.txt")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}

		mode := info.Mode().Perm()
		if mode != 0644 {
			t.Errorf("File mode = %o, want default 0644", mode)
		}
	})

	t.Run("overwrite by default", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		// Write initial content
		err := sink.WriteFile(ctx, "test.txt", []byte("first"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Overwrite
		err = sink.WriteFile(ctx, "test.txt", []byte("second"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		path := filepath.Join(tmpDir, "test.txt")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != "second" {
			t.Errorf("ReadFile() = %q, want %q", got, "second")
		}
	})

	t.Run("Overwrite=false prevents overwriting", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		sink.Overwrite = false
		ctx := context.Background()

		// Write initial content
		err := sink.WriteFile(ctx, "test.txt", []byte("first"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Try to overwrite
		err = sink.WriteFile(ctx, "test.txt", []byte("second"))
		if err == nil {
			t.Error("WriteFile() with Overwrite=false should return error for existing file")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("WriteFile() error = %v, want error containing 'already exists'", err)
		}
	})

	t.Run("rejects absolute paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		err := sink.WriteFile(ctx, "/absolute/path.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with absolute path should return error")
		}
		if !strings.Contains(err.Error(), "absolute") {
			t.Errorf("WriteFile() error = %v, want error containing 'absolute'", err)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		err := sink.WriteFile(ctx, "../escape.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with path traversal should return error")
		}
	})

	t.Run("context cancellation before write", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with cancelled context should return error")
		}
	})

	t.Run("context cancellation during write", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// Use a large file to increase chance of catching cancellation
		largeContent := make([]byte, 10*1024*1024) // 10MB
		err := sink.WriteFile(ctx, "large.txt", largeContent)
		// It's okay if this succeeds (write was fast), but if it errors,
		// it should be a context error
		if err != nil && ctx.Err() == nil {
			t.Errorf("WriteFile() error = %v, but context not cancelled", err)
		}
	})

	t.Run("atomic write behavior", func(t *testing.T) {
		tmpDir := t.TempDir()
		sink := NewFilesystemSink(tmpDir)
		ctx := context.Background()

		// Write a file
		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Verify no temp files left behind
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("ReadDir() error = %v", err)
		}

		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasPrefix(entry.Name(), ".tygor-") {
				t.Errorf("Found temp file after write: %s", entry.Name())
			}
		}
	})
}

// TestFilesystemSink_Concurrent tests concurrent writes to FilesystemSink
func TestFilesystemSink_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	sink := NewFilesystemSink(tmpDir)
	ctx := context.Background()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writes to different files
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			path := filepath.Join("dir", "file"+string(rune('0'+(id%10)))+".txt")
			content := []byte("content from goroutine " + string(rune('0'+(id%10))))
			if err := sink.WriteFile(ctx, path, content); err != nil {
				t.Errorf("WriteFile() error = %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Verify files were written
	entries, err := os.ReadDir(filepath.Join(tmpDir, "dir"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) == 0 {
		t.Error("No files written during concurrent test")
	}

	// Verify no temp files left behind
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasPrefix(entry.Name(), ".tygor-") {
			t.Errorf("Found temp file after concurrent writes: %s", entry.Name())
		}
	}
}

// TestFilesystemSink_PathSecurity tests path security measures
func TestFilesystemSink_PathSecurity(t *testing.T) {
	tmpDir := t.TempDir()
	sink := NewFilesystemSink(tmpDir)
	ctx := context.Background()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "normal path",
			path:    "safe/path.txt",
			wantErr: false,
		},
		{
			name:    "path with multiple ..",
			path:    "a/../../escape.txt",
			wantErr: true,
		},
		{
			name:    "absolute path",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "windows absolute path",
			path:    "C:/Windows/System32/config",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sink.WriteFile(ctx, tt.path, []byte("test"))
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteFile(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestFilesystemSink_ErrorConditions tests various error conditions
func TestFilesystemSink_ErrorConditions(t *testing.T) {
	t.Run("invalid root directory", func(t *testing.T) {
		// Use a path with null bytes which is invalid
		sink := NewFilesystemSink("/tmp/test\x00invalid")
		ctx := context.Background()

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() with invalid root should return error")
		}
	})

	t.Run("permission denied creating directories", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("Skipping test when running as root")
		}

		tmpDir := t.TempDir()
		restrictedDir := filepath.Join(tmpDir, "restricted")
		if err := os.Mkdir(restrictedDir, 0000); err != nil {
			t.Fatalf("Failed to create restricted directory: %v", err)
		}
		defer os.Chmod(restrictedDir, 0755) // Clean up

		sink := NewFilesystemSink(restrictedDir)
		ctx := context.Background()

		err := sink.WriteFile(ctx, "subdir/test.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() should fail when cannot create subdirectories")
		}
	})

	t.Run("permission denied writing file", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("Skipping test when running as root")
		}

		tmpDir := t.TempDir()
		restrictedDir := filepath.Join(tmpDir, "restricted")
		if err := os.Mkdir(restrictedDir, 0500); err != nil {
			t.Fatalf("Failed to create restricted directory: %v", err)
		}
		defer os.Chmod(restrictedDir, 0755) // Clean up

		sink := NewFilesystemSink(restrictedDir)
		ctx := context.Background()

		err := sink.WriteFile(ctx, "test.txt", []byte("content"))
		if err == nil {
			t.Error("WriteFile() should fail when cannot write to directory")
		}
	})
}

// TestValidatePath_EdgeCases tests additional edge cases for ValidatePath
func TestValidatePath_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "lowercase windows drive",
			path:    "c:/path/to/file",
			wantErr: true,
		},
		{
			name:    "uppercase windows drive",
			path:    "D:/path/to/file",
			wantErr: true,
		},
		{
			name:    "single character filename",
			path:    "a",
			wantErr: false,
		},
		{
			name:    "path with only directory",
			path:    "dir",
			wantErr: false,
		},
		{
			name:    "deeply nested path",
			path:    "a/b/c/d/e/f/g/h/i/j/file.txt",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
