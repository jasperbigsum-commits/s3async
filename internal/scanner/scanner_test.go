package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	filePath := filepath.Join(nestedDir, "a.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entries, err := Scan(tempDir)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Scan() len = %d, want 1", len(entries))
	}
	if entries[0].Path != filePath {
		t.Fatalf("Scan() path = %s, want %s", entries[0].Path, filePath)
	}
	if entries[0].RelativePath != filepath.Join("nested", "a.txt") {
		t.Fatalf("Scan() relative path = %s, want %s", entries[0].RelativePath, filepath.Join("nested", "a.txt"))
	}
}
