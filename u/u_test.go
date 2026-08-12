package u

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 8/12 回归：裸字节切片切中文会 cut mid-rune 产生非法 UTF-8。
func TestTruncateStrRuneSafe(t *testing.T) {
	cn := "你好世界" // 4 runes, 12 bytes
	cases := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"ascii cut", "abcdef", 3, "abc..."},
		{"cn clean boundary", cn, 6, "你好..."}, // 6 落在 rune 边界
		{"cn mid-rune", cn, 5, "你..."},         // 5 在「好」中间，回退到 3
		{"cn mid-rune 2", cn, 7, "你好..."},      // 7 在「世」中间，回退到 6
		{"short no cut", cn, 20, cn},
		{"empty", "", 3, ""},
		{"cut at rune start", "ab" + cn, 3, "ab..."}, // 3 落在「你」起点
	}
	for _, c := range cases {
		got := TruncateStr(c.s, c.max)
		if got != c.want {
			t.Errorf("%s: TruncateStr(%q,%d) = %q, want %q", c.name, c.s, c.max, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: output invalid UTF-8: %q", c.name, got)
		}
		if strings.Contains(got, "\uFFFD") {
			t.Errorf("%s: output contains replacement char", c.name)
		}
	}
}

func TestSafePrefixRuneSafe(t *testing.T) {
	cn := "你好世界"
	if got := SafePrefix(cn, 5); got != "你..." || !utf8.ValidString(got) {
		t.Errorf("SafePrefix(%q,5) = %q (valid=%v), want %q", cn, got, utf8.ValidString(got), "你...")
	}
}
