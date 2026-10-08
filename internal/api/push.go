package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"exehub/internal/push"
)

// Web Push (PLAN.md "Notifications"): the browser subscribes on the
// public page and the hub pushes to it — every new post, or, when the
// subscription names the profile the page is signed in as, the posts
// that concern that id. Subscribing takes no signature, like reading:
// the id is a filter over public posts; an endpoint is a secret the
// browser minted, so knowing it is owning it, and telling the hub again
// (a sign-in, a sign-out) or unsubscribing needs nothing more. The
// service worker that shows the pushes is offline.go's.

// pushMax bounds the notifier's work per post.
const pushMax = 10000

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		writeErr(w, http.StatusNotFound, errors.New("push is not enabled on this hub"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sub, err := push.ParseSubscription(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// an endpoint the hub holds is told again, not added: the cap is
	// for new subscribers
	held, err := s.St.PushHas(sub.Endpoint)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if n, err := s.St.PushCount(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	} else if n >= pushMax && !held {
		writeErr(w, http.StatusTooManyRequests, errors.New("this hub has all the subscribers it can notify"))
		return
	}
	sub.Base = webBase(r) // the VAPID subject: the hub as the subscriber reached it
	if err := s.St.PushAdd(sub); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		writeErr(w, http.StatusNotFound, errors.New("push is not enabled on this hub"))
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil || in.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, errors.New("endpoint is required"))
		return
	}
	if err := s.St.PushRemove(in.Endpoint); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
