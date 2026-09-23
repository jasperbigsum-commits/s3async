package filter

import (
	"path/filepath"
	"strings"
)

type Rule struct {
	Pattern string
	Include bool
}

func Match(path string, includes []string, excludes []string) bool {
	// Preserve the historical API semantics: include patterns form the
	// allow-list and excludes always win. Ordered CLI rules use
	// MatchWithRules directly and can intentionally override an earlier rule.
	rules := make([]Rule, 0, len(includes)+len(excludes))
	for _, pattern := range includes {
		rules = append(rules, Rule{Pattern: pattern, Include: true})
	}
	for _, pattern := range excludes {
		rules = append(rules, Rule{Pattern: pattern})
	}
	return MatchWithRules(path, includes, rules)
}

// MatchWithRules applies patterns in order. The last matching pattern decides
// whether the path is included; when include patterns exist, unmatched paths
// remain excluded for backward-compatible allow-list behavior.
func MatchWithRules(path string, includes []string, rules []Rule) bool {
	normalizedPath := filepath.ToSlash(path)
	baseName := filepath.Base(path)

	included := len(includes) == 0
	for _, rule := range rules {
		if matches(rule.Pattern, normalizedPath, baseName) {
			included = rule.Include
		}
	}
	return included
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
