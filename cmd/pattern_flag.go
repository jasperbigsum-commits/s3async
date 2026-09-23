package cmd

import (
	"encoding/csv"
	"fmt"
	"strings"
)

// orderedPatternFlag preserves include/exclude flag order while retaining the
// comma-separated syntax provided by pflag's string-slice flags.
type orderedPatternFlag struct {
	values  *[]string
	rules   *[]filterRule
	include bool
}

type filterRule struct {
	pattern string
	include bool
}

func (f *orderedPatternFlag) Set(value string) error {
	reader := csv.NewReader(strings.NewReader(value))
	reader.TrimLeadingSpace = true
	patterns, err := reader.Read()
	if err != nil {
		return fmt.Errorf("parse filter patterns: %w", err)
	}
	for _, pattern := range patterns {
		*f.values = append(*f.values, pattern)
		*f.rules = append(*f.rules, filterRule{pattern: pattern, include: f.include})
	}
	return nil
}

func (f *orderedPatternFlag) String() string {
	return fmt.Sprintf("%q", *f.values)
}

func (f *orderedPatternFlag) Type() string { return "stringSlice" }
