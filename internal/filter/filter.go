package filter

import (
	"path/filepath"
	"strings"
)

func Match(path string, includes []string, excludes []string) bool {
	normalizedPath := filepath.ToSlash(path)
	baseName := filepath.Base(path)

	included := len(includes) == 0
	for _, pattern := range includes {
		if matches(pattern, normalizedPath, baseName) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range excludes {
		if matches(pattern, normalizedPath, baseName) {
			return false
		}
	}
	return true
}

func matches(pattern string, normalizedPath string, baseName string) bool {
	normalizedPattern := filepath.ToSlash(pattern)
	if ok, _ := filepath.Match(normalizedPattern, normalizedPath); ok {
		return true
	}
	if ok, _ := filepath.Match(normalizedPattern, baseName); ok {
		return true
	}
	if strings.HasSuffix(normalizedPattern, "/*") {
		prefix := strings.TrimSuffix(normalizedPattern, "/*")
		return normalizedPath == prefix || strings.HasPrefix(normalizedPath, prefix+"/")
	}
	return false
}
