package filter

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		includes []string
		excludes []string
		want     bool
	}{
		{name: "match include", path: "report.csv", includes: []string{"*.csv"}, want: true},
		{name: "reject exclude", path: "temp.tmp", excludes: []string{"*.tmp"}, want: false},
		{name: "reject non include", path: "image.png", includes: []string{"*.csv"}, want: false},
		{name: "match nested exclude glob", path: "dir/temp.tmp", excludes: []string{"*.tmp"}, want: false},
		{name: "match directory exclude", path: ".git/config", excludes: []string{".git/*"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Match(tt.path, tt.includes, tt.excludes)
			if got != tt.want {
				t.Fatalf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}
