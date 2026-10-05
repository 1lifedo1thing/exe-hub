package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"exehub/internal/config"
	"exehub/internal/gate"
	"exehub/internal/identity"
	"exehub/internal/ipfs"
)

// draftKubo is a kubo that tells a hash from an add: only-hash answers
// the CID and keeps nothing, add keeps the bytes, and cat serves only
// what was added.
type draftKubo struct {
	mu     sync.Mutex
	kept   map[string][]byte
	hashes int
	adds   int
	pinned []string // the add queries that asked for a pin
}

func (k *draftKubo) counts() (hashes, adds int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.hashes, k.adds
}

func newDraftKubo(t *testing.T) (*draftKubo, *ipfs.Client) {
	k := &draftKubo{kept: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/version":
			w.Write([]byte(`{"Version":"fake"}`))
		case "/api/v0/add":
			mr, err := r.MultipartReader()
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			part, err := mr.NextPart()
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			data, _ := io.ReadAll(part)
			sum := sha256.Sum256(data)
			cid := "bafkrei" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))[:52]
			q := r.URL.Query()
			k.mu.Lock()
			if q.Get("cid-version") != "1" {
				t.Errorf("add without cid-version=1: %s", r.URL.RawQuery)
			}
			if q.Get("only-hash") == "true" {
				k.hashes++
				if q.Has("pin") {
					t.Errorf("a hash asked for a pin: %s", r.URL.RawQuery)
				}
			} else {
				k.adds++
				k.kept[cid] = data
				if q.Get("pin") == "true" {
					k.pinned = append(k.pinned, cid)
				}
			}
			k.mu.Unlock()
			w.Write([]byte(`{"Name":"f","Hash":"` + cid + `"}`))
		case "/api/v0/cat":
			k.mu.Lock()
			data, ok := k.kept[r.URL.Query().Get("arg")]
			k.mu.Unlock()
			if !ok {
				http.Error(w, "not found", 500)
				return
			}
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return k, ipfs.New(srv.URL)
}

// pngOf is bytes a sniffer takes for a PNG, different for each tag.
func pngOf(tag string) []byte { return append([]byte("\x89PNG\x0d\x0a\x1a\x0a"), tag...) }

func stage(t *testing.T, h http.Handler, body []byte, addr string) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest("POST", "http://hub.example/v1/draft", bytes.NewReader(body))
	if addr != "" {
		req.Header.Set("CF-Connecting-IP", addr)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out, w.Header()
}

func embedOf(cid string) map[string]any {
	return map[string]any{"embeds": []map[string]any{{"cid": cid, "mime": "image/png", "filename": "shot.png"}}}
}

// TestDraftPost (PLAN.md, Draft uploads): a picture goes up unsigned and
// is only held — no block, no pin, no address — until a signed post
// names it; then it is added, pinned and served like a signed upload.
func TestDraftPost(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	kubo, client := newDraftKubo(t)
	s.IPFS = client
	h := s.Handler()
	pub, priv, _ := ed25519.GenerateKey(nil)
	pic := pngOf("one signature")

	code, out, _ := stage(t, h, pic, "")
	cid, _ := out["cid"].(string)
	if code != 200 || cid == "" || out["mime"] != "image/png" || out["size"] != float64(len(pic)) || out["expires"] == nil {
		t.Fatalf("staging: %d %v", code, out)
	}
	if hashes, adds := kubo.counts(); hashes != 1 || adds != 0 {
		t.Fatalf("a draft reached kubo as more than a hash: %d hashes, %d adds", hashes, adds)
	}
	if c, body := get(t, h, "/v1/embed/"+cid); c != 404 {
		t.Fatalf("a draft is served before anyone signed for it: %d %s", c, body)
	}
	if _, err := s.St.PinInfo(cid); err == nil {
		t.Fatal("a draft has a pin row")
	}

	// a post that names no such file is refused by name
	if c, body := sendMsg(t, h, priv, pub, 1, "post.create", embedOf("bafkreinothingstagedhere")); c != 400 || !strings.Contains(body, `"code":"no_pin"`) {
		t.Fatalf("a post naming no file: %d %s", c, body)
	}
	if _, adds := kubo.counts(); adds != 0 {
		t.Fatal("a refused post added a file")
	}

	c, body := sendMsg(t, h, priv, pub, 1, "post.create", embedOf(cid))
	if c != 200 || !strings.Contains(body, `"status":"accepted"`) {
		t.Fatalf("the post that names the draft: %d %s", c, body)
	}
	if _, adds := kubo.counts(); adds != 1 || len(kubo.pinned) != 1 || kubo.pinned[0] != cid {
		t.Fatalf("the post did not add and pin its file once: %d adds, pinned %v", adds, kubo.pinned)
	}
	if s.drafts.get(cid) != nil {
		t.Error("the draft is still held after its post")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/embed/"+cid, nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), pic) || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("the posted picture: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	var refs int
	if err := s.St.DB().QueryRow(`SELECT refs FROM pins WHERE cid=?`, cid).Scan(&refs); err != nil || refs != 1 {
		t.Fatalf("the pin's refs: %d %v", refs, err)
	}

	// the answer was lost and the page sends the same envelope again: the
	// post is found, whatever became of the draft, and nothing is added.
	// The post's own cooldown is running by now, and is not the answer.
	if c, again := sendMsg(t, h, priv, pub, 1, "post.create", embedOf(cid)); c != 200 || !strings.Contains(again, `"status":"duplicate"`) ||
		!strings.Contains(again, body[strings.Index(body, `"id":"`):strings.Index(body, `"id":"`)+72]) {
		t.Fatalf("the same envelope again: %d %s (first %s)", c, again, body)
	}
	if _, adds := kubo.counts(); adds != 1 {
		t.Fatalf("a duplicate added a file: %d adds", adds)
	}
	// a file the hub keeps is not held again
	if code, out, _ := stage(t, h, pic, ""); code != 200 || out["cid"] != cid || out["expires"] != nil || s.drafts.get(cid) != nil {
		t.Fatalf("staging a kept file: %d %v", code, out)
	}
}

// TestDraftLapse: the draft lapses while the wallet is open. The signed
// envelope is refused by name, the page sends the bytes again, and the
// same envelope goes through: one post, no second signature.
func TestDraftLapse(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	kubo, client := newDraftKubo(t)
	s.IPFS = client
	now := time.Now()
	s.drafts.now = func() time.Time { return now }
	h := s.Handler()
	pub, priv, _ := ed25519.GenerateKey(nil)
	pic := pngOf("slow wallet")

	_, out, _ := stage(t, h, pic, "")
	cid := out["cid"].(string)
	now = now.Add(draftTTL - time.Second)
	if s.drafts.get(cid) == nil {
		t.Fatal("the draft lapsed early")
	}
	now = now.Add(2 * time.Second)
	if s.drafts.get(cid) != nil || s.drafts.bytes != 0 {
		t.Fatalf("the draft outlived its ten minutes: %d bytes held", s.drafts.bytes)
	}
	if c, body := sendMsg(t, h, priv, pub, 1, "post.create", embedOf(cid)); c != 400 || !strings.Contains(body, `"code":"no_pin"`) {
		t.Fatalf("a post after its draft lapsed: %d %s", c, body)
	}
	if code, again, _ := stage(t, h, pic, ""); code != 200 || again["cid"] != cid {
		t.Fatalf("staging the same bytes again: %d %v", code, again)
	}
	if c, body := sendMsg(t, h, priv, pub, 1, "post.create", embedOf(cid)); c != 200 || !strings.Contains(body, `"status":"accepted"`) {
		t.Fatalf("the same envelope after staging again: %d %s", c, body)
	}
	if _, adds := kubo.counts(); adds != 1 {
		t.Fatalf("%d adds", adds)
	}
	if posts, err := s.St.Feed("", 10, false); err != nil || len(posts) != 1 || len(posts[0].Embeds) != 1 || posts[0].Embeds[0].CID != cid {
		t.Fatalf("the feed after the retry: %v %v", posts, err)
	}
}

// TestDraftNeedsItsPost: nothing short of an acceptable signed post adds
// a draft — not a stale seq, and not a key below the gate.
func TestDraftNeedsItsPost(t *testing.T) {
	off := 0
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}, Cooldown: &off})
	kubo, client := newDraftKubo(t)
	s.IPFS = client
	h := s.Handler()
	pub, priv, _ := ed25519.GenerateKey(nil)

	if c, body := sendMsg(t, h, priv, pub, 5, "post.create", map[string]any{"text": "words first"}); c != 200 {
		t.Fatalf("%d %s", c, body)
	}
	_, out, _ := stage(t, h, pngOf("stale"), "")
	cid := out["cid"].(string)
	// another message from the key landed first: the held envelope is
	// stale, and the wallet must sign again
	if c, body := sendMsg(t, h, priv, pub, 5, "post.create", embedOf(cid)); c != 409 {
		t.Fatalf("a stale seq: %d %s", c, body)
	}
	if _, adds := kubo.counts(); adds != 0 || s.drafts.get(cid) == nil {
		t.Fatalf("a stale envelope added its draft (%d adds) or lost it", adds)
	}
	if c, body := sendMsg(t, h, priv, pub, 6, "post.create", embedOf(cid)); c != 200 {
		t.Fatalf("signed again: %d %s", c, body)
	}

	// a key that may not post keeps nothing either
	poor, poorPriv, _ := ed25519.GenerateKey(nil)
	calls := 0
	rpc := gateRPC(t, map[string]uint64{gate.Base58(poor): 10}, &calls)
	g := tokenServer(t, rpc.URL, []string{identity.Fingerprint(pub)})
	gk, gclient := newDraftKubo(t)
	g.IPFS = gclient
	gh := g.Handler()
	_, out, _ = stage(t, gh, pngOf("below the gate"), "")
	if c, body := sendMsg(t, gh, poorPriv, poor, 1, "post.create", embedOf(out["cid"].(string))); c != 403 {
		t.Fatalf("a key below the gate: %d %s", c, body)
	}
	if _, adds := gk.counts(); adds != 0 {
		t.Fatalf("a key below the gate added a file: %d adds", adds)
	}
}

// TestDraftLimits: a draft is a picture under the cap, and an address
// holds only so many and asks only so often.
func TestDraftLimits(t *testing.T) {
	s := testServer(t, &config.Config{Gate: config.Gate{Mode: "open"}})
	_, client := newDraftKubo(t)
	s.IPFS = client
	now := time.Now()
	s.drafts.now = func() time.Time { return now }
	h := s.Handler()

	if code, out, _ := stage(t, h, []byte("<html><script>alert(1)</script>"), "198.51.100.1"); code != 415 {
		t.Errorf("an HTML draft: %d %v", code, out)
	}
	if code, out, _ := stage(t, h, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "198.51.100.1"); code != 415 {
		t.Errorf("an SVG draft: %d %v", code, out)
	}
	if code, out, _ := stage(t, h, nil, "198.51.100.1"); code != 400 {
		t.Errorf("an empty draft: %d %v", code, out)
	}
	if code, out, _ := stage(t, h, append(pngOf("big"), make([]byte, MaxUpload)...), "198.51.100.1"); code != 413 {
		t.Errorf("a draft over the cap: %d %v", code, out)
	}
	if len(s.drafts.m) != 0 {
		t.Fatalf("a refused draft is held: %d", len(s.drafts.m))
	}

	for i := 0; i < draftPerAddr; i++ {
		if code, out, _ := stage(t, h, pngOf(strings.Repeat("a", i+1)), "198.51.100.2"); code != 200 {
			t.Fatalf("draft %d: %d %v", i, code, out)
		}
	}
	// the same file again is no new draft; a ninth is one too many
	if code, out, _ := stage(t, h, pngOf("a"), "198.51.100.2"); code != 200 {
		t.Errorf("a held file again: %d %v", code, out)
	}
	if code, out, hdr := stage(t, h, pngOf("ninth"), "198.51.100.2"); code != 429 || hdr.Get("Retry-After") == "" {
		t.Errorf("a ninth draft: %d %v", code, out)
	}
	if code, out, _ := stage(t, h, pngOf("ninth"), "198.51.100.3"); code != 200 {
		t.Errorf("another address: %d %v", code, out)
	}
	// they lapse, and the address may hold again
	now = now.Add(draftTTL + time.Second)
	if code, out, _ := stage(t, h, pngOf("ninth"), "198.51.100.2"); code != 200 {
		t.Errorf("after the drafts lapsed: %d %v", code, out)
	}

	// so many requests a minute, whatever they carry
	for i := 0; i < draftPerMinute; i++ {
		stage(t, h, pngOf("again"), "198.51.100.4")
	}
	if code, out, hdr := stage(t, h, pngOf("again"), "198.51.100.4"); code != 429 || hdr.Get("Retry-After") == "" {
		t.Errorf("request %d in a minute: %d %v", draftPerMinute+1, code, out)
	}
	now = now.Add(time.Minute)
	if code, out, _ := stage(t, h, pngOf("again"), "198.51.100.4"); code != 200 {
		t.Errorf("a minute later: %d %v", code, out)
	}

	// the big pictures: four at the cap is an address's all
	big := func(tag string) []byte { return append(pngOf(tag), make([]byte, MaxUpload-64)...) }
	for i := 0; i < 4; i++ {
		if code, out, _ := stage(t, h, big(strings.Repeat("b", i+1)), "198.51.100.5"); code != 200 {
			t.Fatalf("big draft %d: %d %v", i, code, out)
		}
	}
	if code, out, _ := stage(t, h, big("fifth"), "198.51.100.5"); code != 429 {
		t.Errorf("a fifth picture at the cap: %d %v", code, out)
	}
}
