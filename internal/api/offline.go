package api

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// Offline (PLAN.md, Offline): the service worker keeps the pages a
// reader has seen and their pictures on the device, and shows the kept
// copy when the hub cannot be reached; a page it never kept gets the
// offline page below. The worker also shows the hub's pushes (PLAN.md,
// Notifications).

//go:embed sw.js
var swSrc []byte

// swJS is the worker as served, VERSION stamped from the bytes the pages
// are made of — the template, their words in every language, the worker
// itself — so a deploy that changes any of them hands the browser a
// byte-different worker, which installs and keeps the offline page anew.
var swJS = func() []byte {
	h := sha256.New()
	h.Write(swSrc)
	h.Write([]byte(webHTML))
	words, _ := json.Marshal(webStrings) // map keys sorted: the same words, the same bytes
	h.Write(words)
	return bytes.ReplaceAll(swSrc, []byte("__HUB_BUILD__"), []byte(hex.EncodeToString(h.Sum(nil))[:16]))
}()

// handleSW serves the worker. no-cache: the browser asks for it again on
// every check, and the edge passes the question on.
func (s *Server) handleSW(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(swJS)
}

// handleOffline is the page the worker shows at the address of a page it
// never kept, while the hub cannot be reached: it says so, leads back to
// the feed (kept), and loads the page it stands for once the hub answers.
// The worker fetches it as it installs, in the browser's language, and
// in a language an address asks for (?lang=) once a page there is kept.
// No page view, and nothing for a search engine.
func (s *Server) handleOffline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	rd := webReadingOf(r)
	s.webRender(w, r, http.StatusOK, &webData{Page: "offline", Title: rd.L.T("offline.title") + " · " + r.Host,
		Message: rd.L.T("err.offline"), Lang: rd.Lang, Q: rd.Q})
}
