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
