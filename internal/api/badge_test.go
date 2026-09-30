package api

import (
	"bytes"
	"html"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"exehub/internal/config"
)

// TestWebBadgeGIF: the badge is an 88×31 GIF that plays for good, served
// for other pages to link and kept four hours, so a new drawing reaches
// them the same day.
func TestWebBadgeGIF(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://hub.example/badge.gif", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("/badge.gif answered %d as %q", w.Code, w.Header().Get("Content-Type"))
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=14400" {
		t.Errorf("/badge.gif is kept as %q", got)
	}
	g, err := gif.DecodeAll(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if g.Config.Width != 88 || g.Config.Height != 31 {
		t.Errorf("the badge is %d×%d, want 88×31", g.Config.Width, g.Config.Height)
	}
	if len(g.Image) < 2 || g.LoopCount != 0 {
		t.Errorf("%d frames looping %d times; want an animation that plays for good", len(g.Image), g.LoopCount)
	}
	for i, d := range g.Delay {
		if d < 2 {
			t.Errorf("frame %d waits %d cs; a browser would slow it to its own floor", i, d)
		}
	}
}

// TestWebBadgePage: /badge/ hands the badge out — HTML and Markdown that
// link this hub by the address it was asked by, each in a post's code
// block with its Copy button — in the reader's language, and /badge
// leads there with the query kept.
func TestWebBadgePage(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	h := s.Handler()
	snippets := func(body string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`<div class="code one"><pre>\n([^<]*)</pre><button type="button" class="btn copy">`).FindAllStringSubmatch(body, -1) {
			out = append(out, html.UnescapeString(m[1]))
		}
		return out
	}
	for _, c := range []struct{ lang, title, alt, facts string }{
		{"", "Badge · hub.example", "hub.example, an exe-hub", "GIF, 88 × 31, " + strconv.Itoa(webBadgeFrames) + " frames, " + webBadgeSize},
		{"?lang=zh", "徽章 · hub.example", "hub.example，一个 exe-hub", "GIF，88 × 31，" + strconv.Itoa(webBadgeFrames) + " 帧，" + webBadgeSize},
		{"?lang=ja", "バッジ · hub.example", "hub.example（exe-hub）", "GIF、88 × 31、" + strconv.Itoa(webBadgeFrames) + " フレーム、" + webBadgeSize},
	} {
		code, body := get(t, h, "/badge/"+c.lang)
		if code != http.StatusOK {
			t.Fatalf("/badge/%s answered %d", c.lang, code)
		}
		if !strings.Contains(body, "<title>"+c.title+"</title>") {
			t.Errorf("/badge/%s is not titled %q", c.lang, c.title)
		}
		got := snippets(body)
		want := []string{
			`<a href="http://hub.example/"><img src="http://hub.example/badge.gif" width="88" height="31" alt="` + c.alt + `"></a>`,
			`[![` + c.alt + `](http://hub.example/badge.gif)](http://hub.example/)`,
		}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("/badge/%s hands out\n%q\nwant\n%q", c.lang, got, want)
		}
		for _, m := range []string{
			`<img src="/badge.gif" width="88" height="31" alt="` + c.alt + `">`,
			`<img src="/badge.gif" width="264" height="93" alt="">`,
			`<a href="/badge.gif" download="exe-hub-badge.gif">`,
			`<div class="statusbar"><span>` + c.facts + `</span></div>`,
			`<link rel="canonical" href="http://hub.example/badge/">`,
			`<span class="popup lang" id="lang">`,
		} {
			if !strings.Contains(body, m) {
				t.Errorf("/badge/%s has no %s", c.lang, m)
			}
		}
		if strings.Contains(body, "<!-- render:") {
			t.Errorf("/badge/%s did not render whole", c.lang)
		}
	}
	// the Feed button and the close box carry the reader's language home
	_, body := get(t, h, "/badge/?lang=ja")
	if !strings.Contains(body, `<a class="btn prev back" href="/?lang=ja">`) {
		t.Error("the badge page's way home loses ?lang=")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://hub.example/badge?lang=zh", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/badge/?lang=zh" {
		t.Errorf("/badge answered %d to %q", w.Code, w.Header().Get("Location"))
	}
	if got := s.StatsPathLabel("/badge/"); got != "The badge" {
		t.Errorf("the stats call /badge/ %q", got)
	}
}
