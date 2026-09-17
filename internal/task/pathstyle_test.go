package task

import (
	"strings"
	"testing"
)

func TestParsePathStyle(t *testing.T) {
	for _, value := range []string{"", "collapse", "COLLAPSE", "  faithful  ", "Faithful"} {
		style, err := ParsePathStyle(value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		want := PathCollapse
		if strings.Contains(strings.ToLower(value), "faithful") {
			want = PathFaithful
		}
		if style != want {
			t.Fatalf("%q: style=%v want %v", value, style, want)
		}
		if style.String() != want.String() {
			t.Fatalf("%q: String()=%q", value, style.String())
		}
	}
	if _, err := ParsePathStyle("lossy"); err == nil {
		t.Fatal("accepted invalid path style")
	}
	if PathCollapse.String() != "collapse" || PathFaithful.String() != "faithful" {
		t.Fatal("bad String()")
	}
}

func TestEncodeKeySegment(t *testing.T) {
	cases := map[string]string{
		"":        "%2F",
		".":       "%2E",
		"..":      "%2E%2E",
		"a.txt":   "a.txt",
		"person":  "person",
		"a b":     "a b",
		"100%":    "100%",
		"100%20x": "100%20x",
		"%2F":     "%252F",
		"%2f":     "%252f",
		"%2E":     "%252E",
		"%25":     "%2525",
		"a%2Fb":   "a%252Fb",
		"%2":      "%2",
		"con":     "con",
	}
	for in, want := range cases {
		if got := EncodeKeySegment(in); got != want {
			t.Errorf("EncodeKeySegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeLocalSegment(t *testing.T) {
	cases := map[string]string{
		"%2F":       "",
		"%2f":       "",
		"%2E":       ".",
		"%2E%2E":    "..",
		"%252F":     "%2F",
		"%252f":     "%2f",
		"%252E":     "%2E",
		"%25":       "%",
		"%2525":     "%25",
		"a%252Fb":   "a%2Fb",
		"a.txt":     "a.txt",
		"100%20x":   "100%20x",
		"100%":      "100%",
		"%2":        "%2",
		"trailing%": "trailing%",
	}
	for in, want := range cases {
		if got := DecodeLocalSegment(in); got != want {
			t.Errorf("DecodeLocalSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPathStyleRoundTrip is the integrity contract: decoding inverts encoding
// for every segment, including hostile ones.
func TestPathStyleRoundTrip(t *testing.T) {
	segments := []string{
		"", ".", "..", "a", "a.txt", "8bae194e-a60f-453a-b9c3-54101ae20eb9.jpg",
		"%2F", "%2f", "%2E", "%2e", "%2E%2E", "%25", "%252F", "a%2Fb", "%",
		"%%2F", "100%20x", "100%", "a%%b", "%2", "x%2", "con", "NUL.txt",
		"空格", "emoji-🎉", "a/b",
	}
	for _, seg := range segments {
		if got := DecodeLocalSegment(EncodeKeySegment(seg)); got != seg {
			t.Errorf("round trip(%q) = %q", seg, got)
		}
	}
	keys := []string{
		"prod/thumbnail1/prod/person/30d62ff4/image/8bae194e.jpg",
		"prod/thumbnail1//prod/person/30d62ff4/image/8bae194e.jpg",
		"/a.txt",
		"a/./b",
		"a/../b",
		"./a.txt",
	}
	for _, key := range keys {
		if got := DecodeLocalRelative(EncodeRelativeKey(key)); got != key {
			t.Errorf("key round trip(%q) = %q", key, got)
		}
	}
}

// TestFaithfulEncodingKeepsKeysDistinct is the reported bug: keys that
// collapse to one local file must stay distinct in faithful mode.
func TestFaithfulEncodingKeepsKeysDistinct(t *testing.T) {
	seen := map[string]string{}
	keys := []string{
		"prod/thumbnail1/prod/person/x/image/a.jpg",
		"prod/thumbnail1//prod/person/x/image/a.jpg",
		"prod/thumbnail1/./prod/person/x/image/a.jpg",
		"prod/thumbnail1///prod/person/x/image/a.jpg",
	}
	for _, key := range keys {
		encoded := EncodeRelativeKey(key)
		if prev, ok := seen[encoded]; ok {
			t.Fatalf("faithful collision: %q and %q both encode to %q", prev, key, encoded)
		}
		seen[encoded] = key
	}
}
