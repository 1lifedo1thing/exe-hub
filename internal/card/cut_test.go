package card

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCut: a text that fits is itself; a longer one is cut at the last
// space in the second half, else at n, trailing spaces dropped, with
// an ellipsis; and the cut never splits a character — a CJK rune, a
// flag, a skin tone, a family, a decomposed accent, a joined sequence
// no font has — whichever side of the budget it straddles.
func TestCut(t *testing.T) {
	family, fox := "👨‍👩‍👧", "🦊‍🍜"
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 3, "abc"},
		{"", 3, ""},
		{"one two three", 8, "one two…"},
		{"one two three four", 10, "one two…"},
		{"abcdefghij", 5, "abcde…"},
		{"ab cdefghij", 8, "ab cdefg…"}, // the space is in the first half
		{"one two   three", 9, "one two…"},
		{"a" + family + "b", 7, "a" + family + "b"},
		{"a" + family + "b", 6, "a" + family + "…"},
		{"a" + family + "b", 5, "a…"},
		{"a" + family, 6, "a" + family},
		{"x👍🏽y", 2, "x…"},
		{"x👍🏽y", 3, "x👍🏽…"},
		{"🇯🇵🇺🇸", 3, "🇯🇵…"},
		{"café au lait", 5, "café…"},
		{"café au lait", 4, "caf…"},
		{"ab" + fox + "cd", 4, "ab…"},
		{"ab" + fox + "cd", 5, "ab" + fox + "…"},
	} {
		if got := Cut(c.in, c.n); got != c.want {
			t.Errorf("Cut(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	// Codex's case under the emoji preview post, 2026-10-17: 199 bowls
	// and a flag at 200 left a lone regional indicator
	if got := Cut(strings.Repeat("🍜", 199)+"🇯🇵", 200); got != strings.Repeat("🍜", 199)+"…" {
		t.Errorf("a flag at the budget: %q", got[len(got)-12:])
	}
	if got := Cut(strings.Repeat("字", 300), 200); !utf8.ValidString(got) || utf8.RuneCountInString(got) != 201 {
		t.Errorf("CJK: %d runes, valid %v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if got := Cut(strings.Repeat("word ", 100), 200); !strings.HasSuffix(got, "word…") || strings.Contains(got, " …") {
		t.Errorf("words: %q", got)
	}
}
