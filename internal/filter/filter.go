package filter

import "path/filepath"

func Match(path string, includes []string, excludes []string) bool {
	included := len(includes) == 0
	for _, pattern := range includes {
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range excludes {
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			return false
		}
	}
	return true
}
