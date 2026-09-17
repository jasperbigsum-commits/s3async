package task

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathStyle controls how S3 object keys map to local file names.
//
// PathCollapse drops empty and dot-only segments (traditional filesystem
// path resolution): "a//b" and "a/b" share one local file, and distinct
// keys may collide.
//
// PathFaithful escapes those segments reversibly so every distinct key maps
// to a distinct local path: "" -> "%2F", "." -> "%2E", ".." -> "%2E%2E".
// Real segments that would decode ambiguously ("%2F", "%2E", "%25", any
// letter case) have their "%" escaped as "%25" on the way down, so decoding
// is a bijection. Encoded names contain no slashes, are never exactly ".",
// ".." or empty, and keep every other LocalPath safety rule intact.
type PathStyle int

const (
	PathCollapse PathStyle = iota
	PathFaithful
)

// ParsePathStyle parses config and flag values. Empty means the default.
func ParsePathStyle(value string) (PathStyle, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "collapse":
		return PathCollapse, nil
	case "faithful":
		return PathFaithful, nil
	default:
		return PathCollapse, fmt.Errorf("invalid path style %q: want collapse or faithful", value)
	}
}

func (p PathStyle) String() string {
	if p == PathFaithful {
		return "faithful"
	}
	return "collapse"
}

// EncodeKeySegment maps one S3 key segment to one local name in faithful mode.
func EncodeKeySegment(segment string) string {
	switch segment {
	case "":
		return "%2F"
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	}
	if needsPercentEscape(segment) {
		return strings.ReplaceAll(segment, "%", "%25")
	}
	return segment
}

// needsPercentEscape reports whether decoding the segment would transform it,
// in which case its "%" signs must be escaped to keep decoding bijective.
func needsPercentEscape(segment string) bool {
	lower := strings.ToLower(segment)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%2e") || strings.Contains(lower, "%25")
}

// DecodeLocalSegment maps one faithful local name back to its S3 segment.
// Sequences outside the faithful alphabet pass through untouched, so names
// the encoder never produces (for example "100%20off") survive verbatim.
// Decoding is a single left-to-right pass: output is never rescanned, so
// "%252F" decodes to "%2F" rather than to "".
func DecodeLocalSegment(segment string) string {
	if !strings.Contains(segment, "%") {
		return segment
	}
	var b strings.Builder
	b.Grow(len(segment))
	for i := 0; i < len(segment); {
		if segment[i] != '%' || i+3 > len(segment) {
			b.WriteByte(segment[i])
			i++
			continue
		}
		switch strings.ToLower(segment[i+1 : i+3]) {
		case "2f":
			// Encoded empty segment: contributes no characters.
			i += 3
		case "2e":
			b.WriteByte('.')
			i += 3
		case "25":
			b.WriteByte('%')
			i += 3
		default:
			b.WriteByte(segment[i])
			i++
		}
	}
	return b.String()
}

// EncodeRelativeKey maps a full S3 relative key (slash-separated, may carry
// leading slashes from redundant separators) to its faithful local form.
func EncodeRelativeKey(relative string) string {
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		parts[i] = EncodeKeySegment(part)
	}
	return strings.Join(parts, "/")
}

// DecodeLocalRelative maps a faithful local relative path back to the S3
// relative key. OS-specific separators are normalized before decoding.
func DecodeLocalRelative(local string) string {
	slashified := strings.ReplaceAll(filepath.ToSlash(local), "\\", "/")
	parts := strings.Split(slashified, "/")
	for i, part := range parts {
		parts[i] = DecodeLocalSegment(part)
	}
	return strings.Join(parts, "/")
}
