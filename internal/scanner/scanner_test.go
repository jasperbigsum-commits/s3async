package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "a.txt")
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
}
