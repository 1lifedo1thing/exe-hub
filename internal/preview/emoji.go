package preview

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"strconv"
	"strings"
	"unicode"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

// The emoji are Noto Color Emoji's own pictures (OFL 1.1, see
// FONTS.md), one PNG per sequence, cut from the font by genemoji.py.
// Each is named by its code points less U+FE0F, which a sequence may
// carry or leave out and the font ignores.
//
//go:embed emoji.zip
var emojiZip []byte

// The font's one strike, which genemoji.py checks every picture keeps:
// 109 pixels to the em, each picture 136×128 with its top 101 above the
// baseline, advancing 136.
const (
	emojiPPEM   = 109
	emojiAscent = 101
	emojiWidth  = 136
	emojiHeight = 128
)

const vs16, vs15 = '️', '︎' // the emoji and the text variation selectors

var (
	emojiFiles   map[string]*zip.File // sequence less U+FE0F -> its picture
	emojiStarts  map[rune]bool        // the first character of every sequence
	emojiLongest int                  // the most characters in one sequence
)

func loadEmoji() error {
	z, err := zip.NewReader(bytes.NewReader(emojiZip), int64(len(emojiZip)))
	if err != nil {
		return err
	}
	emojiFiles, emojiStarts = map[string]*zip.File{}, map[rune]bool{}
	for _, f := range z.File {
		var seq []rune
		for _, h := range strings.Split(strings.TrimSuffix(f.Name, ".png"), "-") {
			cp, err := strconv.ParseUint(h, 16, 32)
			if err != nil {
				return err
			}
			seq = append(seq, rune(cp))
		}
		emojiFiles[string(seq)] = f
		emojiStarts[seq[0]] = true
		emojiLongest = max(emojiLongest, len(seq))
	}
	return nil
}

// emojiRun is how many characters at the head of rs make the longest
// emoji sequence there is a picture for, the variation selector after
// it included; 0 when none starts there. A U+FE0F inside the sequence
// is passed over, as the font does.
func emojiRun(rs []rune) int {
	if len(rs) == 0 || !emojiStarts[rs[0]] {
		return 0
	}
	var seq []rune
	var ends []int // ends[k] is where the sequence's first k+1 characters end in rs
	for i, r := range rs {
		if r == vs16 {
			continue
		}
		if len(seq) == emojiLongest {
			break
		}
		seq = append(seq, r)
		ends = append(ends, i+1)
	}
	for k := len(seq); k > 0; k-- {
		if emojiFiles[string(seq[:k])] == nil {
			continue
		}
		n := ends[k-1]
		if n < len(rs) && (rs[n] == vs16 || rs[n] == vs15) {
			n++
		}
		return n
	}
	return 0
}

// clusters splits s into what is set as one: an emoji sequence whole,
// else a single character.
func clusters(s string) []string {
	rs := []rune(s)
	var out []string
	for i := 0; i < len(rs); {
		n := max(emojiRun(rs[i:]), 1)
		out = append(out, string(rs[i:i+n]))
		i += n
	}
	return out
}

// emoji is the picture cluster c is drawn as, nil when it is set in
// type. A sequence of more than one character is always a picture; one
// character is a picture when U+FE0F follows it, when it is an emoji
// by default (Emoji_Presentation) and no U+FE0E asks for text, or when
// no face has it — so ©, # and the digits stay type, as a browser
// sets them.
func (t typeface) emoji(c string) *zip.File {
	var seq []rune
	vs := rune(0)
	for _, r := range c {
		if r == vs16 || r == vs15 {
			vs = r
			continue
		}
		seq = append(seq, r)
	}
	f := emojiFiles[string(seq)]
	if f == nil || len(seq) > 1 {
		return f
	}
	switch r := seq[0]; {
	case vs == vs16 || t.face(r) == nil:
		return f
	case vs == vs15:
		return nil
	case unicode.Is(emojiPresentation, r):
		return f
	}
	return nil
}

// emojiAdvance is how far an emoji moves the pen at this size.
func (t typeface) emojiAdvance() fixed.Int26_6 {
	return fixed.I(t.size*emojiWidth) / emojiPPEM
}

// drawEmoji puts f's picture at dot, scaled from the font's strike to
// this size and sat on the baseline as the font sits it.
func (t typeface) drawEmoji(dst draw.Image, dot fixed.Point26_6, f *zip.File) {
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	src, _, err := image.Decode(rc)
	if err != nil {
		return
	}
	top := dot.Y - fixed.I(t.size*emojiAscent)/emojiPPEM
	r := image.Rect(dot.X.Round(), top.Round(),
		(dot.X + t.emojiAdvance()).Round(), (top + fixed.I(t.size*emojiHeight)/emojiPPEM).Round())
	xdraw.CatmullRom.Scale(dst, r, src, src.Bounds(), xdraw.Over, nil)
}
