package scanner

import (
	"fmt"
	"os"
	"path/filepath"
)

type Entry struct {
	Path         string
	RelativePath string
	Size         int64
}

func Scan(root string) ([]Entry, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute root: %w", err)
	}

	var entries []Entry
	err = filepath.Walk(absoluteRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk path %s: %w", path, err)
		}
		if info.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(absoluteRoot, path)
		if err != nil {
			return fmt.Errorf("build relative path for %s: %w", path, err)
		}

		entries = append(entries, Entry{Path: path, RelativePath: relativePath, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan root %s: %w", root, err)
	}
	return entries, nil
}
