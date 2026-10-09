package card

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ---- YouTube links: a card that plays (PLAN.md, YouTube players) ----

// youTubeID is a video id: eleven characters of YouTube's URL-safe base64.
var youTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youTubeTime is a start time as links carry it: plain seconds ("90",
// "90s") or hours, minutes and seconds ("1h2m3s", "2m").
var youTubeTime = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$`)

// YouTube is the video a YouTube link names and the second it starts at,
// or "" for any other link: a watch page (www, m. or music.youtube.com,
// v= in the query), a youtu.be short link, or a /shorts/, /live/,
// /embed/ or /v/ path, the privacy-enhanced youtube-nocookie.com among
// them. The start is the link's t= (or start=), in seconds; a playlist's
// list= and the rest of the query are dropped, since the player shows
// the one video the link was posted for.
func YouTube(link string) (id string, start int) {
	pu, err := url.Parse(link)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		return "", 0
	}
	host := strings.TrimPrefix(strings.ToLower(pu.Hostname()), "www.")
	q := pu.Query()
	switch host {
	case "youtu.be":
		id = strings.TrimSuffix(strings.TrimPrefix(pu.Path, "/"), "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if pu.Path == "/watch" {
			id = q.Get("v")
			break
		}
		for _, p := range []string{"/shorts/", "/live/", "/embed/", "/v/"} {
			if rest, ok := strings.CutPrefix(pu.Path, p); ok {
				id = strings.TrimSuffix(rest, "/")
				break
			}
		}
	}
	if !youTubeID.MatchString(id) {
		return "", 0
	}
	t := q.Get("t")
	if t == "" {
		t = q.Get("start")
	}
	if m := youTubeTime.FindStringSubmatch(t); m != nil {
		for i, unit := range []int{3600, 60, 1} {
			if n, err := strconv.Atoi(m[i+1]); err == nil {
				start += n * unit
			}
		}
	}
	return id, start
}

// oEmbed is where YouTube says whether a video plays on other sites; a
// test points it at its own server.
var oEmbed = "https://www.youtube.com/oembed"

// Plays asks YouTube whether the video plays on other sites, through its
// oEmbed endpoint and the guarded client: 200 is yes, and 401 (the
// owner turned embedding off), 403, 404 (private, or gone) and 400 (no
// such id) are no. Anything else — no connection, a timeout, a 429, a
// 5xx — is no answer (known false), and the start-up pass asks again.
func (f *Fetcher) Plays(ctx context.Context, id string) (plays, known bool) {
	u := oEmbed + "?format=json&url=" + url.QueryEscape("https://www.youtube.com/watch?v="+id)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("User-Agent", "exe-hub/1 (link cards, linked pictures)")
	resp, err := f.client.Do(req)
	if err != nil {
		return false, false
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, true
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, true
	}
	return false, false
}
