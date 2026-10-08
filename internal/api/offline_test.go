package api

import (
	"crypto/ed25519"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"exehub/internal/config"
	"exehub/internal/events"
	"exehub/internal/identity"
)

// TestWebOffline (PLAN.md, Offline): the service worker is served
// stamped, keeps pages and shows pushes; every page but the replies
// frame registers it; the four reading pages carry the Offline strip a
// kept copy shows, first in the window's frame; and /offline is the page
// shown for one never kept, in the reader's language, never cached by
// the browser, never indexed, never counted.
func TestWebOffline(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	s.Events = events.New()
	h := s.Handler()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://hub.example/sw.js", nil))
	sw := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("sw.js: %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
	}
	if !regexp.MustCompile(`const VERSION = "[0-9a-f]{16}";`).MatchString(sw) || strings.Contains(sw, "__HUB_BUILD__") {
		t.Error("sw.js is not stamped")
	}
	for _, want := range []string{`addEventListener("fetch"`, `addEventListener("push"`, `addEventListener("notificationclick"`, `"/offline"`} {
		if !strings.Contains(sw, want) {
			t.Errorf("sw.js lacks %s", want)
		}
	}

	pub, priv, _ := ed25519.GenerateKey(nil)
	root := ingest(t, s, priv, pub, 1, "post.create", map[string]any{"text": "a post"})
	ingest(t, s, priv, pub, 2, "post.create", map[string]any{"text": "a reply", "reply_to": root})
	const reg = `sw.register("/sw.js")`
	strip := regexp.MustCompile(`<div class="frame"[^>]*>\s*<div class="saved" role="status"><span class="sv"></span><button type="button" class="btn sv-re" hidden>Reload</button></div>`)
	for _, path := range []string{"/", "/?before=" + root, "/p/" + root, "/u/" + identity.Fingerprint(pub), "/search?q=post"} {
		code, body := get(t, h, path)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		if !strings.Contains(body, reg) {
			t.Errorf("%s does not register the service worker", path)
		}
		if !strip.MatchString(body) {
			t.Errorf("%s: no Offline strip first in its frame", path)
		}
	}
	if _, body := get(t, h, "/p/"+root+"/replies"); strings.Contains(body, reg) || strings.Contains(body, `class="saved"`) {
		t.Error("the replies frame registers the worker or carries the strip")
	}

	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://hub.example/offline", nil)
	r.Header.Set("Accept-Language", "ja")
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("/offline: %d %q %q", w.Code, w.Header().Get("Cache-Control"), w.Header().Get("X-Robots-Tag"))
	}
	for _, want := range []string{webStrings["ja"]["err.offline"], "<title>" + webStrings["ja"]["offline.title"] + " · hub.example</title>", `location.pathname === "/offline"`, reg} {
		if !strings.Contains(body, want) {
			t.Errorf("/offline (ja) lacks %q", want)
		}
	}
	if _, body := get(t, h, "/offline?lang=zh"); !strings.Contains(body, webStrings["zh"]["err.offline"]) || !strings.Contains(body, `href="/?lang=zh"`) {
		t.Error("/offline?lang=zh is not in Chinese, or its way back drops the language")
	}
	if _, body := get(t, h, "/"); strings.Contains(body, `location.pathname === "/offline"`) {
		t.Error("the feed carries the offline page's script")
	}
}
