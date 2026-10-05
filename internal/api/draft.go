package api

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"exehub/internal/envelope"
	"exehub/internal/media"
)

// Draft uploads (PLAN.md, Draft uploads): a picture sent without a
// signature, so a wallet signs once, for the post that names it. The hub
// asks kubo what CID the bytes would get (only-hash: nothing is written),
// holds them in memory for draftTTL and answers the CID. A signed
// post.create that names the CID, from a key that may post, is what adds
// and pins the file (promoteDrafts, from handleMsg). Until then the bytes
// are in no table, in no IPFS repo and behind no address: /v1/embed
// serves pins only.
const (
	draftTTL       = 10 * time.Minute
	draftPerAddr   = 8              // live drafts one address may hold: a post's four, replaced once
	draftAddrBytes = 4 * MaxUpload  // and their bytes: four pictures at the cap
	draftAllBytes  = 16 * MaxUpload // everything held, for every address
	draftPerMinute = 24             // requests one address may make in a minute
)

// draftMIME is what a draft may be: the pictures a page draws inline.
// Anything else still goes through the signed /v1/upload.
var draftMIME = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

type draft struct {
	body    []byte
	mime    string
	addr    string
	expires time.Time
}

type drafts struct {
	mu    sync.Mutex
	m     map[string]*draft      // by CID
	bytes int64                  // held, in all
	hits  map[string][]time.Time // an address's requests in the last minute
	now   func() time.Time       // the tests' clock
}

func (d *drafts) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// sweep drops what has expired; the caller holds the lock.
func (d *drafts) sweep(now time.Time) {
	for cid, x := range d.m {
		if !now.Before(x.expires) {
			d.bytes -= int64(len(x.body))
			delete(d.m, cid)
		}
	}
	for addr, ts := range d.hits {
		for len(ts) > 0 && now.Sub(ts[0]) >= time.Minute {
			ts = ts[1:]
		}
		if len(ts) == 0 {
			delete(d.hits, addr)
		} else {
			d.hits[addr] = ts
		}
	}
}

// allow counts a request against its address and answers how many
// seconds to wait when the minute is full, 0 when it may go on.
func (d *drafts) allow(addr string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	d.sweep(now)
	ts := d.hits[addr]
	if len(ts) >= draftPerMinute {
		return int(time.Minute-now.Sub(ts[0]))/int(time.Second) + 1
	}
	if d.hits == nil {
		d.hits = map[string][]time.Time{}
	}
	d.hits[addr] = append(ts, now)
	return 0
}

var errDraftFull = errors.New("too many pictures waiting for their post: post one, or try again in a few minutes")

// put holds the bytes under their CID and answers when they lapse. The
// same file sent again only starts its ten minutes over, whoever sends it.
func (d *drafts) put(cid string, body []byte, mime, addr string) (time.Time, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock()
	d.sweep(now)
	exp := now.Add(draftTTL)
	if x := d.m[cid]; x != nil {
		x.expires = exp
		return exp, nil
	}
	n, size := 0, int64(0)
	for _, x := range d.m {
		if x.addr == addr {
			n++
			size += int64(len(x.body))
		}
	}
	add := int64(len(body))
	if n >= draftPerAddr || size+add > draftAddrBytes || d.bytes+add > draftAllBytes {
		return time.Time{}, errDraftFull
	}
	if d.m == nil {
		d.m = map[string]*draft{}
	}
	d.m[cid] = &draft{body: body, mime: mime, addr: addr, expires: exp}
	d.bytes += add
	// memory goes back when the draft lapses, whether or not anyone calls again
	if d.now == nil {
		time.AfterFunc(draftTTL+time.Second, func() {
			d.mu.Lock()
			d.sweep(time.Now())
			d.mu.Unlock()
		})
	}
	return exp, nil
}

// get is the live draft under a CID, nil when there is none.
func (d *drafts) get(cid string) *draft {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweep(d.clock())
	return d.m[cid]
}

func (d *drafts) drop(cid string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if x := d.m[cid]; x != nil {
		d.bytes -= int64(len(x.body))
		delete(d.m, cid)
	}
}

// clientAddr is who a request came from, for the drafts' budget: the
// address Cloudflare names when the hub stands behind it, else the
// connection's.
func clientAddr(r *http.Request) string {
	if a := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); a != "" {
		return a
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleDraft takes a picture nobody has signed for yet and answers the
// CID a post can name. No key is asked: the signature comes with the
// post, and until one does the bytes are only held.
func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	addr := clientAddr(r)
	if wait := s.drafts.allow(addr); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeErr(w, http.StatusTooManyRequests, fmt.Errorf("too many uploads: try again in %ds", wait))
		return
	}
	body, err := readUpload(w, r)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("embeds are capped at %dMB", MaxUpload>>20))
		return
	}
	if len(body) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("the body is the picture's bytes"))
		return
	}
	mime := media.Sniff(body)
	if !draftMIME[mime] {
		writeErr(w, http.StatusUnsupportedMediaType, fmt.Errorf("a draft upload is a picture (PNG, JPEG, GIF or WebP), not %s: sign other files to /v1/upload", mime))
		return
	}
	if err := s.IPFS.Available(); err != nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("IPFS backend unavailable: %v", err))
		return
	}
	cid, err := s.IPFS.Hash(bytes.NewReader(body), envelope.MsgID(body))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	out := map[string]any{"cid": cid, "mime": mime, "size": len(body)}
	// a file the hub already keeps needs no holding: a post can name it now
	if _, err := s.St.PinInfo(cid); err == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	exp, err := s.drafts.put(cid, body, mime, addr)
	if err != nil {
		w.Header().Set("Retry-After", "60")
		writeErr(w, http.StatusTooManyRequests, err)
		return
	}
	out["expires"] = exp.UnixMilli()
	writeJSON(w, http.StatusOK, out)
}

// promoteDrafts keeps the files a signed post names that are still
// drafts: added and pinned in kubo and given their pin row, as a signed
// upload is, so the store finds them where it looks for any embed. The
// caller has verified the signature and the policy. A CID with no draft
// is left to the store: it is a pin already, or the post is refused.
func (s *Server) promoteDrafts(pc *envelope.PostCreate) error {
	for _, em := range pc.Embeds {
		d := s.drafts.get(em.CID)
		if d == nil {
			continue
		}
		cid, err := s.IPFS.Add(bytes.NewReader(d.body), envelope.MsgID(d.body))
		if err != nil {
			return err
		}
		if cid != em.CID {
			// only-hash and add disagreed (kubo's import settings changed
			// between the two): what was signed names another file
			if err := s.IPFS.Unpin(cid); err != nil {
				return fmt.Errorf("ipfs add gave %s for the draft %s, and unpinning it: %w", cid, em.CID, err)
			}
			return fmt.Errorf("ipfs add gave %s for the draft %s", cid, em.CID)
		}
		if err := s.St.AddPin(cid, int64(len(d.body)), d.mime, false); err != nil {
			return err
		}
		s.drafts.drop(cid)
	}
	return nil
}
