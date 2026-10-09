package card

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYouTube(t *testing.T) {
	type want struct {
		id    string
		start int
	}
	for link, w := range map[string]want{
		"https://www.youtube.com/watch?v=_00eQuc30kw":                     {"_00eQuc30kw", 0},
		"https://youtube.com/watch?v=dQw4w9WgXcQ&t=90":                    {"dQw4w9WgXcQ", 90},
		"https://m.youtube.com/watch?feature=share&v=dQw4w9WgXcQ&t=1m30s": {"dQw4w9WgXcQ", 90},
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVM":       {"dQw4w9WgXcQ", 0},
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL123&index=2":  {"dQw4w9WgXcQ", 0},
		"https://youtu.be/dQw4w9WgXcQ":                                    {"dQw4w9WgXcQ", 0},
		"https://youtu.be/dQw4w9WgXcQ?si=abc&t=1h2m3s":                    {"dQw4w9WgXcQ", 3723},
		"http://youtu.be/dQw4w9WgXcQ/":                                    {"dQw4w9WgXcQ", 0},
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":                      {"dQw4w9WgXcQ", 0},
		"https://www.youtube.com/live/dQw4w9WgXcQ?si=x":                   {"dQw4w9WgXcQ", 0},
		"https://www.youtube.com/embed/dQw4w9WgXcQ?start=42":              {"dQw4w9WgXcQ", 42},
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ":              {"dQw4w9WgXcQ", 0},
		"https://www.YouTube.com/watch?v=dQw4w9WgXcQ&t=2m":                {"dQw4w9WgXcQ", 120},
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=soon":              {"dQw4w9WgXcQ", 0},
		"https://www.youtube.com/watch?v=dQw4w9WgXc":                      {}, // ten: too short
		"https://www.youtube.com/watch?v=dQw4w9WgXcQQ":                    {}, // twelve
		"https://www.youtube.com/playlist?list=PL123":                     {},
		"https://www.youtube.com/@someone":                                {},
		"https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx":        {},
		"https://youtu.be/":                                               {},
		"https://notyoutube.com/watch?v=dQw4w9WgXcQ":                      {},
		"https://youtube.com.example.com/watch?v=dQw4w9WgXcQ":             {},
		"https://example.com/watch?v=dQw4w9WgXcQ":                         {},
		"ftp://youtu.be/dQw4w9WgXcQ":                                      {},
		"https://www.youtube.com/shorts/dQw4w9WgXcQ/extra":                {},
	} {
		if id, start := YouTube(link); id != w.id || start != w.start {
			t.Errorf("YouTube(%q) = %q, %d; want %q, %d", link, id, start, w.id, w.start)
		}
	}
}

// TestPlays: YouTube's oEmbed answer decides whether a video plays in
// its card — 200 yes; 401 (embedding off), 403, 404 and 400 no; a 5xx,
// a 429 or no connection is no answer, asked again by the sweep.
func TestPlays(t *testing.T) {
	codes := map[string]int{"aaaaaaaaaaa": 200, "bbbbbbbbbbb": 401, "ccccccccccc": 404, "ddddddddddd": 400,
		"eeeeeeeeeee": 403, "fffffffffff": 500, "ggggggggggg": 429}
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("url")
		id := strings.TrimPrefix(asked, "https://www.youtube.com/watch?v=")
		w.WriteHeader(codes[id])
	}))
	defer srv.Close()
	f := NewFetcher()
	f.AllowPrivate = true
	f.OEmbed = srv.URL + "/oembed"
	for id, code := range codes {
		plays, known := f.Plays(context.Background(), id)
		wantPlays, wantKnown := code == 200, code < 500 && code != 429
		if plays != wantPlays || known != wantKnown {
			t.Errorf("%d: plays %v known %v, want %v %v", code, plays, known, wantPlays, wantKnown)
		}
	}
	if !strings.HasPrefix(asked, "https://www.youtube.com/watch?v=") {
		t.Errorf("asked about %q, not a watch page", asked)
	}
	srv.Close()
	if plays, known := f.Plays(context.Background(), "aaaaaaaaaaa"); plays || known {
		t.Errorf("no connection: plays %v known %v, want no answer", plays, known)
	}
}
