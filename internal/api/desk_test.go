package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"exehub/internal/config"
	"exehub/internal/envelope"
	"exehub/internal/identity"
)

// TestWebDesk: the signed-in reader's desk (PLAN.md) — the Navigation
// window on the pages a reader moves between, the Notifications and
// Bookmarks pages with the Post window over their list, why each
// notification is one, the Bookmark button only where the wallet can
// sign, and the bookmarks API.
func TestWebDesk(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	mePub, mePriv, _ := ed25519.GenerateKey(nil)
	bobPub, bobPriv, _ := ed25519.GenerateKey(nil)
	me := identity.Fingerprint(mePub)
	ingest(t, s, mePriv, mePub, 1, "profile.set", map[string]any{"name": "Me"})
	root := ingest(t, s, mePriv, mePub, 2, "post.create", map[string]any{"text": "mine"})
	time.Sleep(2 * time.Millisecond)
	reply := ingest(t, s, bobPriv, bobPub, 1, "post.create", map[string]any{"text": "to you", "reply_to": root})
	time.Sleep(2 * time.Millisecond)
	named := ingest(t, s, bobPriv, bobPub, 2, "post.create", map[string]any{"text": "hello @" + me})
	h := s.Handler()

	// the home page: the window after the join window, the home row its own
	_, home := get(t, h, "/")
	j, n, m := strings.Index(home, `<div class="window join" id="join">`), strings.Index(home, `<div class="window nav" id="nav">`), strings.Index(home, `<div class="main">`)
	if !(0 < j && j < n && n < m) {
		t.Errorf("home: join %d, nav %d, main %d — want the Navigation window between", j, n, m)
	}
	for _, want := range []string{
		`<nav class="places" aria-label="Navigation" data-page="home" data-for="" data-q="">`,
		`<a class="place" data-to="home" href="/"><svg viewBox="0 0 24 24"`, `<span>Notifications</span>`, `<span>Bookmarks</span>`, `<span>Profile</span>`,
		".wallet .desk .join { display: none; }", ".wallet .nav { display: block; }",
		`window.hubNav = id =>`, `if (window.hubNav) hubNav(st.v.profile);`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	if strings.Count(home, `class="btn bm"`) != 2 {
		t.Errorf("home: %d Bookmark buttons, want one on each of its two posts", strings.Count(home, `class="btn bm"`))
	}
	// at the far right of the meta line: floated ahead of it, never on the foot
	if !strings.Contains(home, `</svg></button><div class="meta"><a href="/u/`) || strings.Contains(home, `.wallet .foot.own:has(.bm)`) ||
		!strings.Contains(home, `.post .bm { display: none; float: right;`) {
		t.Error("home: the Bookmark button does not stand at the head of the meta line")
	}

	// a thread: the window in the side, no row its own
	_, thread := get(t, h, "/p/"+root)
	if !strings.Contains(thread, `<div class="side" id="side"><div class="window nav" id="nav">`) || !strings.Contains(thread, `data-page="" data-for=""`) {
		t.Error("thread: the Navigation window is not in the side, or claims a row")
	}

	// a profile: a desk with the window, the wallet class from the head
	// script, but no own-post rule and no Bookmark button — nothing there
	// can sign
	_, prof := get(t, h, "/u/"+me)
	if !strings.Contains(prof, `data-page="profile" data-for="`+me+`"`) || !strings.Contains(prof, `classList.add("wallet")`) {
		t.Error("profile: no Navigation window for the reader, or no wallet class before layout")
	}
	if strings.Contains(prof, `id: "own"`) || strings.Contains(prof, `class="btn bm"`) || strings.Contains(prof, `id="compose"`) {
		t.Error("profile: a page with no Post window offers to sign")
	}

	// Notifications: the Post window over the list, which goes to the feed
	// once it has posted; each post says why
	code, notes := get(t, h, "/u/"+me+"/notifications")
	if code != http.StatusOK {
		t.Fatalf("notifications: %d", code)
	}
	for _, want := range []string{
		`<title>Notifications for Me · hub.example</title>`, `<meta name="robots" content="noindex">`,
		`data-page="notifications" data-for="` + me + `"`, `<div class="compose" id="compose" data-away>`,
		`<span class="title">Notifications</span>`,
		`<a class="why" href="/p/` + root + `">replied to you</a>`,
		`<a class="why" href="/p/` + named + `">mentioned you</a>`,
		`<div class="statusbar"><span>Replies, mentions, and replies in threads Me is in.</span></div>`,
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notifications lack %q", want)
		}
	}
	if strings.Index(notes, `id="`+named+`"`) > strings.Index(notes, `id="`+reply+`"`) {
		t.Error("notifications: not newest first")
	}
	if strings.Contains(notes, `id="`+root+`" data-au`) {
		t.Error("notifications: one's own post among them")
	}
	if _, ja := get(t, h, "/u/"+me+"/notifications?lang=ja"); !strings.Contains(ja, `>あなたに返信</a>`) || !strings.Contains(ja, `/u/`+me+`/notifications?lang=ja`) && !strings.Contains(ja, `data-q="?lang=ja"`) {
		t.Error("notifications in Japanese: no why in the page's language, or the links lose ?lang=")
	}

	// Bookmarks: none, then the one kept
	_, books := get(t, h, "/u/"+me+"/bookmarks")
	if !strings.Contains(books, `<div class="pad">No bookmarks yet.</div>`) || !strings.Contains(books, `Me’s bookmarks are public, like posts.`) {
		t.Error("bookmarks: no empty line, or no word that they are public")
	}
	ingest(t, s, mePriv, mePub, 3, "post.bookmark", map[string]any{"post": reply, "on": true})
	_, books = get(t, h, "/u/"+me+"/bookmarks")
	if !strings.Contains(books, `id="`+reply+`"`) || strings.Contains(books, `id="`+named+`"`) {
		t.Error("bookmarks: the kept post is not listed, or another is")
	}
	_, ids := get(t, h, "/v1/profile/"+me+"/bookmarks/ids")
	if ids != `{"posts":["`+reply+`"]}`+"\n" {
		t.Errorf("bookmark ids: %s", ids)
	}
	var feed struct{ Posts []struct{ ID, Why string } }
	_, body := get(t, h, "/v1/profile/"+me+"/notifications")
	if err := json.Unmarshal([]byte(body), &feed); err != nil || len(feed.Posts) != 2 || feed.Posts[0].Why != "mention" || feed.Posts[1].Why != "reply" {
		t.Errorf("notifications API: %s", body)
	}

	// an id that is no id
	if code, _ := get(t, h, "/u/nobody/bookmarks"); code != http.StatusNotFound {
		t.Errorf("a page for no id: %d", code)
	}
	if code, _ := get(t, h, "/v1/profile/nobody/notifications"); code != http.StatusBadRequest {
		t.Errorf("the API for no id: %d", code)
	}
}

// TestBookmarkPolicy: a bookmark is gated as a profile is — a key the
// token gate turns away is turned away here too — and has no cooldown.
func TestBookmarkPolicy(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	op := func(seq int64) *envelope.Envelope {
		raw, _ := json.Marshal(map[string]any{"type": "post.bookmark", "author": base64.StdEncoding.EncodeToString(pub),
			"seq": seq, "ts": 1, "body": map[string]any{"post": strings.Repeat("a", 64), "on": true}})
		e, err := envelope.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	open := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	post(t, open, priv, pub, 1) // a post a moment ago: the cooldown runs
	if err := open.policy(op(3)); err != nil {
		t.Errorf("an open hub refuses a bookmark: %v", err)
	}
	gated := testServer(t, &config.Config{Gate: config.Gate{Mode: "token", Token: config.TokenGate{
		Mints:   []config.MintReq{{Mint: "9raUVuzeWUk53co63M4WXLWPWE4Xc6Lpn7RS9dnkpump", MinAmount: "1", MinRaw: 1}},
		RPCURL:  "http://127.0.0.1:1", RPCUnavailable: "deny", Recheck: "10m"}}})
	if err := gated.policy(op(1)); err == nil {
		t.Error("a token gate that turns the key away lets its bookmark in")
	}
}
