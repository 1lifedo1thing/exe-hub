package api

import (
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"

	"exehub/internal/config"
	"exehub/internal/events"
)

// TestWebRepliesFrame (PLAN.md, Replies under a blog post): /p/{id}/replies
// is the thread's replies alone — no head post, no desk, no window, no
// composer — live, not indexed, its links opening a new tab while its
// pager stays in the frame, its Reply links ready for the page around it
// to show; a whole id this hub does not hold is an empty list that waits,
// anything else a 404; the words are in the reader's language.
func TestWebRepliesFrame(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	s.Events = events.New()
	site, sitePriv, _ := ed25519.GenerateKey(nil)
	ann, annPriv, _ := ed25519.GenerateKey(nil)
	bob, bobPriv, _ := ed25519.GenerateKey(nil)
	ingest(t, s, sitePriv, site, 1, "profile.set", map[string]any{"name": "exe blog"})
	ingest(t, s, annPriv, ann, 1, "profile.set", map[string]any{"name": "Ann"})
	root := ingest(t, s, sitePriv, site, 2, "post.create", map[string]any{"text": "Meet exe, the blog post"})
	r1 := ingest(t, s, annPriv, ann, 2, "post.create", map[string]any{"text": "first reply", "reply_to": root})
	r2 := ingest(t, s, bobPriv, bob, 1, "post.create", map[string]any{"text": "a reply to Ann", "reply_to": r1})
	h := s.Handler()

	req := httptest.NewRequest("GET", "http://hub.example/p/"+root+"/replies", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("frame: %d, X-Robots-Tag %q", rec.Code, rec.Header().Get("X-Robots-Tag"))
	}
	for _, want := range []string{
		`class="framed"`, `<base target="_blank">`, `<meta name="robots" content="noindex">`,
		`id="` + r1 + `"`, `id="` + r2 + `"`, "first reply", "a reply to Ann",
		`data-live="thread" data-root="` + root + `"`, // live, filtered to this thread
		`class="rlink"`, "in reply to Ann", // the Reply link, and a reply to a reply names its parent
		`hub: "height"`, `hub: "aim"`, // the frame's messages to the page around it
	} {
		if !strings.Contains(body, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
	for _, not := range []string{
		`id="` + root + `"`, "Meet exe, the blog post", // the blog shows the post itself
		`class="desk`, `class="window`, `id="compose"`, `id="join"`, "The picture viewer",
	} {
		if strings.Contains(body, not) {
			t.Errorf("frame carries %q", not)
		}
	}

	// a root this hub does not hold (yet) waits, empty and live; the rest is 404
	unknown := strings.Repeat("ab", 32)
	if code, body := get(t, h, "/p/"+unknown+"/replies"); code != 200 || !strings.Contains(body, "No replies yet.") ||
		!strings.Contains(body, `data-root="`+unknown+`"`) {
		t.Errorf("unknown whole id: %d", code)
	}
	for _, bad := range []string{root[:12], strings.ToUpper(root), "nope"} {
		if code, _ := get(t, h, "/p/"+bad+"/replies"); code != 404 {
			t.Errorf("/p/%s/replies: %d, want 404", bad, code)
		}
	}

	// the reader's language, as every page picks it
	if _, body := get(t, h, "/p/"+unknown+"/replies", "Accept-Language", "zh-CN,zh;q=0.9"); !strings.Contains(body, "还没有回复。") {
		t.Error("the frame ignores Accept-Language")
	}
	if _, body := get(t, h, "/p/"+unknown+"/replies?lang=ja"); !strings.Contains(body, "まだ返信はありません。") {
		t.Error("the frame ignores ?lang=")
	}

	// the thread page itself is untouched: its head post, its window
	if _, body := get(t, h, "/p/"+root); !strings.Contains(body, "Meet exe, the blog post") || strings.Contains(body, `class="framed"`) {
		t.Error("the thread page changed")
	}
}

// TestWebRepliesFramePaper: ?look=paper is the same frame drawn for a
// page that is not Platinum — the look class, the reader's light or
// dark, Noto Serif with the CJK face the page's words are in — and the
// look, like the language, rides the pager's links, which stay in the
// frame. Without it the frame is as it was.
func TestWebRepliesFramePaper(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	site, sitePriv, _ := ed25519.GenerateKey(nil)
	ann, annPriv, _ := ed25519.GenerateKey(nil)
	root := ingest(t, s, sitePriv, site, 1, "post.create", map[string]any{"text": "a blog post"})
	for i := 1; i <= webThreadPage+1; i++ {
		ingest(t, s, annPriv, ann, int64(i), "post.create", map[string]any{"text": "reply", "reply_to": root})
	}
	h := s.Handler()
	_, body := get(t, h, "/p/"+root+"/replies?look=paper&lang=zh-Hans")
	for _, want := range []string{
		`<html lang="zh-Hans" class="framed paper">`, `<meta name="color-scheme" content="light dark">`,
		`family=Noto+Serif:ital,wght@0,100..900;1,100..900&family=Noto+Serif+SC:wght@200..900`,
		`.paper .post`, `href="/p/` + root + `/replies?lang=zh-hans&amp;look=paper&amp;page=2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("paper frame lacks %q", want)
		}
	}
	_, body = get(t, h, "/p/"+root+"/replies?look=paper&page=2")
	if !strings.Contains(body, `href="/p/`+root+`/replies?look=paper"`) {
		t.Error("page 2's Prev drops the look")
	}
	if _, body = get(t, h, "/p/"+root+"/replies?look=paper&lang=ja"); !strings.Contains(body, `Noto+Serif+JP`) {
		t.Error("a Japanese frame is not set in Noto Serif JP")
	}
	_, body = get(t, h, "/p/"+root+"/replies?look=wood")
	if !strings.Contains(body, `class="framed"`) || strings.Contains(body, "fonts.googleapis.com") || strings.Contains(body, "look=") {
		t.Error("an unknown look changed the frame")
	}
}

// TestWebRepliesFrameHant: the frame speaks Traditional Chinese to a
// reader who asks for it, by ?lang= or by the browser — its words, its
// <html lang>, and in the paper look its face, Noto Serif TC — while a
// text in another script keeps that script's face: the Simplified
// translation such a reader is given is lang="zh-Hans", set in SC, and
// the page loads both. The hub's own pages fold Traditional into
// Simplified as before.
func TestWebRepliesFrameHant(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	site, sitePriv, _ := ed25519.GenerateKey(nil)
	ann, annPriv, _ := ed25519.GenerateKey(nil)
	root := ingest(t, s, sitePriv, site, 1, "post.create", map[string]any{"text": "紙的紋理"})
	en := ingest(t, s, annPriv, ann, 1, "post.create", map[string]any{"text": "Beautiful.", "reply_to": root})
	ja := ingest(t, s, annPriv, ann, 2, "post.create", map[string]any{"text": "美しい文章です。", "reply_to": root})
	s.St.SetLang(en, "en", "m", true)
	s.St.SetLang(ja, "ja", "m", true)
	if err := s.St.SetTranslation(en, "zh-Hans", "很美。", "m", true); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	empty := "/p/" + strings.Repeat("ab", 32) + "/replies"
	has := func(what, body string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %q", what, w)
			}
		}
	}
	lacks := func(what, body string, nots ...string) {
		t.Helper()
		for _, n := range nots {
			if strings.Contains(body, n) {
				t.Errorf("%s carries %q", what, n)
			}
		}
	}

	_, body := get(t, h, empty+"?look=paper&lang=zh-Hant")
	has("paper, ?lang=zh-Hant", body, `<html lang="zh-Hant" class="framed paper">`, "family=Noto+Serif+TC:wght", "還沒有回覆。",
		`.paper:lang(zh-Hant) { --serif: "Noto Serif TC"`, `.paper .text:lang(zh-Hans) { font-family: "Noto Serif SC"`)
	lacks("paper, ?lang=zh-Hant", body, "Noto+Serif+SC", "Noto+Serif+JP", "还没有回复。")
	for _, al := range []string{"zh-TW,zh;q=0.9,en;q=0.8", "zh-HK", "zh-Hant-TW"} {
		_, body = get(t, h, empty, "Accept-Language", al)
		has("the frame for "+al, body, `<html lang="zh-Hant" class="framed">`, "還沒有回覆。")
	}
	for _, q := range []string{"?lang=zh", "?lang=zh-CN", "?lang=zh-Hans"} {
		_, body = get(t, h, empty+q+"&look=paper", "Accept-Language", "zh-TW")
		has("the frame for "+q, body, `<html lang="zh-Hans" class="framed paper">`, "还没有回复。", "family=Noto+Serif+SC:wght")
		lacks("the frame for "+q, body, "Noto+Serif+TC")
	}
	_, body = get(t, h, empty+"?lang=en", "Accept-Language", "zh-TW")
	has("?lang=en over a zh-TW browser", body, `<html lang="en" class="framed">`, "No replies yet.")

	// the rows: a translation into Simplified keeps its script's face
	_, body = get(t, h, "/p/"+root+"/replies?look=paper&lang=zh-Hant")
	has("a Traditional frame's rows", body, `<div class="text" lang="zh-Hans">很美。</div>`, "譯自英文", "顯示原文",
		"family=Noto+Serif+SC:wght", "family=Noto+Serif+TC:wght", "family=Noto+Serif+JP:wght", `>回覆</a>`)

	// the hub's own pages are as they were
	for _, p := range []string{"/", "/p/" + root} {
		_, body = get(t, h, p, "Accept-Language", "zh-TW")
		has(p+" for zh-TW", body, `<html lang="zh-Hans">`)
	}
}
