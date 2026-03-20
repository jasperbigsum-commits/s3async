package scanner

import (
	"fmt"
	"os"
	"path/filepath"
)

type Entry struct {
	Path string
	Size int64
}

func Scan(root string) ([]Entry, error) {
	var entries []Entry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk path %s: %w", path, err)
		}
		if info.IsDir() {
			return nil
		}
		entries = append(entries, Entry{Path: path, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan root %s: %w", root, err)
	}
	return entries, nil
}
