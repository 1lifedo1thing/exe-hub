package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"exehub/internal/envelope"
	"exehub/internal/gate"
	"exehub/internal/identity"
	"exehub/internal/ipfs"
)

// sendMsg signs an envelope as a client does and posts it to /v1/msg
// through the whole handler, policy and all.
func sendMsg(t *testing.T, h http.Handler, priv ed25519.PrivateKey, pub ed25519.PublicKey, seq int64, typ string, body map[string]any) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"type": typ, "author": base64.StdEncoding.EncodeToString(pub),
		"seq": seq, "ts": 1756500000000 + seq, "body": body,
	})
	in, _ := json.Marshal(map[string]any{"envelope": raw, "sig": ed25519.Sign(priv, append([]byte(envelope.Prefix), raw...))})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "http://hub.example/v1/msg", bytes.NewReader(in)))
	return rec.Code, rec.Body.String()
}

// TestInvites (PLAN.md, Who may post — Invites): an admin's invite.set
// lets a key that holds nothing past the token gate, for posts, profiles
// and uploads alike, and nothing more — the cooldown still holds, a ban
// beats it, only an admin may invite, a lift puts the key back under the
// gate, and a rebuild from the log keeps the list. The gate endpoint and
// the profile say so.
func TestInvites(t *testing.T) {
	site, sitePriv, _ := ed25519.GenerateKey(nil)
	poor, poorPriv, _ := ed25519.GenerateKey(nil)
	admin, adminPriv, _ := ed25519.GenerateKey(nil)
	calls := 0
	rpc := gateRPC(t, map[string]uint64{gate.Base58(poor): 10}, &calls)
	s := tokenServer(t, rpc.URL, []string{identity.Fingerprint(admin)})
	h := s.Handler()
	b64 := func(k ed25519.PublicKey) string { return base64.StdEncoding.EncodeToString(k) }

	// the admin's own profile, so the invite reads with a name
	if code, body := sendMsg(t, h, adminPriv, admin, 1, "profile.set", map[string]any{"name": "Livid"}); code != 200 {
		t.Fatalf("admin profile: %d %s", code, body)
	}
	if code, _ := sendMsg(t, h, sitePriv, site, 1, "post.create", map[string]any{"text": "hello"}); code != http.StatusForbidden {
		t.Fatalf("an uninvited key holding nothing posted: %d", code)
	}
	if _, out := gateGet(t, s, site); out.Gate != "below" {
		t.Fatalf("before the invite: %+v", out)
	}

	// only an admin invites, and only a whole key
	if code, _ := sendMsg(t, h, poorPriv, poor, 1, "invite.set", map[string]any{"target": b64(site)}); code != http.StatusForbidden {
		t.Errorf("a non-admin invited: %d", code)
	}
	if code, _ := sendMsg(t, h, adminPriv, admin, 2, "invite.set", map[string]any{"target": identity.Fingerprint(site)}); code != http.StatusBadRequest {
		t.Errorf("an invite by profile id was taken: %d", code)
	}
	url64 := base64.URLEncoding.EncodeToString(site)
	if url64 != b64(site) {
		if code, _ := sendMsg(t, h, adminPriv, admin, 2, "invite.set", map[string]any{"target": url64}); code != http.StatusBadRequest {
			t.Errorf("an invite in URL base64 was taken: %d", code)
		}
	}
	if code, body := sendMsg(t, h, adminPriv, admin, 2, "invite.set", map[string]any{"target": b64(site), "note": "Site: exe"}); code != 200 {
		t.Fatalf("invite: %d %s", code, body)
	}

	// the invited key posts, sets its profile, and the gate says why
	if _, out := gateGet(t, s, site); out.Gate != "invited" || out.Cooldown != 60 || len(out.Mints) != 1 || out.Mints[0].Held != "0" {
		t.Errorf("gate after the invite: %+v", out)
	}
	if code, body := sendMsg(t, h, sitePriv, site, 1, "profile.set", map[string]any{"name": "exe blog"}); code != 200 {
		t.Fatalf("invited profile.set: %d %s", code, body)
	}
	if code, body := sendMsg(t, h, sitePriv, site, 2, "post.create", map[string]any{"text": "hello"}); code != 200 {
		t.Fatalf("invited post: %d %s", code, body)
	}
	// the cooldown still holds
	if code, _ := sendMsg(t, h, sitePriv, site, 3, "post.create", map[string]any{"text": "again"}); code != http.StatusTooManyRequests {
		t.Errorf("an invited key skipped the cooldown: %d", code)
	}
	// an upload (its avatar, a picture) meets the same policy as a post
	s.IPFS = ipfs.New(fakeAdd(t).URL)
	if rec := httptest.NewRecorder(); !s.uploadPolicy(rec, site) {
		t.Errorf("upload policy refused an invited key: %d %s", rec.Code, rec.Body.String())
	}
	if rec := httptest.NewRecorder(); s.uploadPolicy(rec, poor) {
		t.Error("upload policy let a poor uninvited key through")
	}

	// the profile says who invited it; the list is public
	code, body := get(t, h, "/v1/profile/"+identity.Fingerprint(site))
	var prof struct {
		Name    string `json:"name"`
		Invited *struct {
			Target, By, ByName, Note string
		} `json:"invited"`
	}
	json.Unmarshal([]byte(body), &prof)
	if code != 200 || prof.Name != "exe blog" || prof.Invited == nil || prof.Invited.By != identity.Fingerprint(admin) || prof.Invited.Note != "Site: exe" {
		t.Errorf("profile: %d %s", code, body)
	}
	if !strings.Contains(body, `"by_name":"Livid"`) {
		t.Errorf("the profile's invite carries no inviter name: %s", body)
	}
	if code, page := get(t, h, "/u/"+identity.Fingerprint(site)); code != 200 || !strings.Contains(page, ">invited by Livid</a>") ||
		!strings.Contains(page, `href="/u/`+identity.Fingerprint(admin)+`"`) {
		t.Errorf("profile page: %d, no invited line", code)
	}
	if code, body := get(t, h, "/v1/invites"); code != 200 || !strings.Contains(body, b64(site)) {
		t.Errorf("invites: %d %s", code, body)
	}
	if code, body := get(t, h, "/v1/profile/"+identity.Fingerprint(poor)); code == 200 && strings.Contains(body, "invited") {
		t.Errorf("an uninvited profile says invited: %s", body)
	}

	// a rebuild from the log keeps the list
	if err := s.St.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.St.Invited(b64(site)); !ok {
		t.Error("a rebuild dropped the invite")
	}

	// a ban beats an invite
	if code, _ := sendMsg(t, h, adminPriv, admin, 3, "ban.set", map[string]any{"target": identity.Fingerprint(site)}); code != 200 {
		t.Fatalf("ban: %d", code)
	}
	if code, _ := sendMsg(t, h, sitePriv, site, 3, "profile.set", map[string]any{"name": "banned"}); code != http.StatusForbidden {
		t.Errorf("a banned invited key set its profile: %d", code)
	}
	if code, _ := sendMsg(t, h, adminPriv, admin, 4, "ban.lift", map[string]any{"target": identity.Fingerprint(site)}); code != 200 {
		t.Fatalf("ban.lift: %d", code)
	}

	// a lift puts the key back under the gate; its posts stay
	if code, _ := sendMsg(t, h, adminPriv, admin, 5, "invite.lift", map[string]any{"target": b64(site)}); code != 200 {
		t.Fatalf("invite.lift: %d", code)
	}
	if code, _ := sendMsg(t, h, sitePriv, site, 3, "profile.set", map[string]any{"name": "after"}); code != http.StatusForbidden {
		t.Errorf("a lifted key still passes: %d", code)
	}
	if _, out := gateGet(t, s, site); out.Gate != "below" {
		t.Errorf("gate after the lift: %+v", out)
	}
	// (the scripts' string table carries the words on every page: the line is the link)
	if _, page := get(t, h, "/u/"+identity.Fingerprint(site)); strings.Contains(page, ">invited by Livid</a>") || !strings.Contains(page, "hello") {
		t.Errorf("after the lift the profile page still says invited, or lost the post")
	}
}
