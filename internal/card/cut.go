package card

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Cut is s cut to about n characters, for the places that show a
// post's first words — an excerpt, a title, a notification, a link
// card's description: s itself when it has no more than n, else its
// first n or fewer and an ellipsis. The cut falls at the last space in
// the second half when there is one, trailing spaces dropped, and
// never inside a grapheme cluster — what a reader sees as one
// character (UAX #29): a letter and its combining accent, a flag's two
// regional indicators, an emoji and its skin tone, a family joined by
// ZWJ, a joined sequence no font has — so no cut leaves a half the
// font draws as something else. The budget counts code points, as
// every caller's always did.
func Cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	end, count := 0, 0
	for rest, state := s, -1; rest != ""; {
		c, r, _, st := uniseg.FirstGraphemeClusterInString(rest, state)
		k := utf8.RuneCountInString(c)
		if count+k > n {
			break
		}
		count += k
		end += len(c)
		rest, state = r, st
	}
	head := s[:end]
	if i := strings.LastIndexByte(head, ' '); i >= 0 && utf8.RuneCountInString(head[:i]) >= n/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " ") + "…"
}
