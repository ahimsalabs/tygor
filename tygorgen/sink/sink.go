// Package sink provides output destinations for generated code.
// It implements the output sink interface from the Generator Interface Specification (§5.2-5.4).
package sink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// OutputSink receives generated file content.
// Implementations MUST be safe for concurrent calls.
type OutputSink interface {
	// WriteFile writes content to the specified path.
	// The path is relative; the sink determines the actual location.
	// Implementations MUST be safe for concurrent calls.
	WriteFile(ctx context.Context, path string, content []byte) error
}

// FilesystemSink writes to a directory on the local filesystem.
type FilesystemSink struct {
	// Root is the base directory for all writes.
	Root string

	// Mode is the file permission mode (default: 0644).
	Mode os.FileMode

	// Overwrite controls behavior for existing files.
	// If false, returns an error when a file exists.
	Overwrite bool
}

// NewFilesystemSink creates a new FilesystemSink writing to the specified root directory.
func NewFilesystemSink(root string) *FilesystemSink {
	return &FilesystemSink{
		Root:      root,
		Mode:      0644,
		Overwrite: true,
	}
}

// WriteFile writes content to path within the root directory.
// It creates parent directories as needed and performs atomic writes via temp file + rename.
// This method is safe for concurrent use.
func (s *FilesystemSink) WriteFile(ctx context.Context, path string, content []byte) error {
	// Validate path
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}

	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Construct full path
	fullPath := filepath.Join(s.Root, filepath.FromSlash(path))

	// Check for path traversal after resolution
	absRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return fmt.Errorf("failed to resolve root directory: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return fmt.Errorf("path escapes root directory: %q", path)
	}

	// Skip write if file exists with identical content
	if existing, err := os.ReadFile(fullPath); err == nil {
		if bytes.Equal(existing, content) {
			return nil // No change needed
		}
	}

	// Create parent directories
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Determine mode
	mode := s.Mode
	if mode == 0 {
		mode = 0644
	}

	// Atomic write: write to temp file, then rename
	// Use a unique temp file name to avoid conflicts in concurrent writes
	tempFile, err := os.CreateTemp(dir, ".tygor-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Write content and close
	_, writeErr := tempFile.Write(content)
	closeErr := tempFile.Close()

	// cleanupTempFile is a helper that attempts to remove the temp file.
	// Cleanup errors are intentionally not returned because:
	// 1. We're already in an error path returning a more important error
	// 2. Libraries shouldn't log directly
	// 3. Leftover temp files have a predictable prefix (.tygor-*.tmp) for manual cleanup
	cleanupTempFile := func() {
		_ = os.Remove(tempPath) // Best-effort cleanup; error intentionally ignored
	}

	if writeErr != nil {
		cleanupTempFile()
		return fmt.Errorf("failed to write temp file: %w", writeErr)
	}
	if closeErr != nil {
		cleanupTempFile()
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	// Set correct permissions after writing
	if err := os.Chmod(tempPath, mode); err != nil {
		cleanupTempFile()
		return fmt.Errorf("failed to set file mode: %w", err)
	}

	// Check context again before rename
	if err := ctx.Err(); err != nil {
		cleanupTempFile()
		return err
	}

	// Check if file exists when Overwrite is false
	if !s.Overwrite {
		if _, err := os.Stat(fullPath); err == nil {
			cleanupTempFile()
			return fmt.Errorf("file already exists: %q", path)
		}
	}

	// Rename temp file to final destination
	if err := os.Rename(tempPath, fullPath); err != nil {
		cleanupTempFile()
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// MemorySink stores generated files in memory.
// All operations are thread-safe.
type MemorySink struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewMemorySink creates a new MemorySink.
func NewMemorySink() *MemorySink {
	return &MemorySink{
		files: make(map[string][]byte),
	}
}

// WriteFile writes content to the in-memory store.
func (s *MemorySink) WriteFile(ctx context.Context, path string, content []byte) error {
	// Validate path
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("invalid path %q: %w", path, err)
	}

	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Make a copy of the content to prevent external modifications
	contentCopy := make([]byte, len(content))
	copy(contentCopy, content)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[path] = contentCopy
	return nil
}

// Files returns a copy of all written files.
func (s *MemorySink) Files() map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]byte, len(s.files))
	for path, content := range s.files {
		// Copy each slice to prevent external modifications
		contentCopy := make([]byte, len(content))
		copy(contentCopy, content)
		result[path] = contentCopy
	}
	return result
}

// Get returns the content of a single file, or nil if not found.
func (s *MemorySink) Get(path string) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	content, ok := s.files[path]
	if !ok {
		return nil
	}

	// Return a copy to prevent external modifications
	contentCopy := make([]byte, len(content))
	copy(contentCopy, content)
	return contentCopy
}

// Reset clears all stored files.
func (s *MemorySink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.files = make(map[string][]byte)
}

// ValidatePath checks if a path is valid for output.
// Paths MUST be relative (no leading /), use / as separator,
// not contain .. components, and be clean (no ./, duplicate /).
func ValidatePath(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}

	// Must be relative (no leading /)
	if filepath.IsAbs(path) {
		return errors.New("absolute paths not allowed")
	}

	// Check for Windows-style paths (C:, D:, etc.) even on Unix
	if len(path) >= 2 && path[1] == ':' && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) {
		return errors.New("absolute paths not allowed")
	}

	// No .. components (path traversal)
	if strings.Contains(path, "..") {
		return errors.New("path traversal not allowed")
	}

	// Clean the path using forward slashes
	cleaned := filepath.Clean(filepath.ToSlash(path))
	if cleaned != filepath.ToSlash(path) {
		return fmt.Errorf("path is not clean (expected %q, got %q)", cleaned, path)
	}

	// Additional check: path must not start with ../ after cleaning
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return errors.New("path traversal not allowed")
	}

	return nil
}
