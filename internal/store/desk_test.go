package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestBookmarks: a post.bookmark keeps a post or takes it off, the last
// one standing; a post that is not here is refused; the list is the
// newest bookmark first and pages by post id; a deleted post leaves it;
// Rebuild lands on the same list; and the op never replicates.
func TestBookmarks(t *testing.T) {
	s := openTest(t)
	alice, bob := newAuthor(t), newAuthor(t)
	ingest(t, s, alice, "profile.set", map[string]string{"name": "Alice"})
	var posts []string
	for i := range 4 {
		posts = append(posts, ingest(t, s, alice, "post.create", map[string]string{"text": fmt.Sprintf("post %d", i)}))
	}
	mine := ingest(t, s, bob, "post.create", map[string]string{"text": "bob's own"})

	raw, sig, e, op := bob.msg(t, "post.bookmark", map[string]any{"post": fmt.Sprintf("%064x", 7), "on": true})
	if _, _, err := s.Ingest(raw, sig, e, op); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bookmark of no post: want ErrNotFound, got %v", err)
	}
	bob.seq--

	keep := func(post string, on bool) {
		t.Helper()
		ingest(t, s, bob, "post.bookmark", map[string]any{"post": post, "on": on})
		time.Sleep(2 * time.Millisecond) // received times in order
	}
	keep(posts[0], true)
	keep(posts[2], true)
	keep(mine, true) // one's own too
	keep(posts[1], true)
	keep(posts[2], false) // taken off
	keep(posts[0], true)  // kept again: now the newest

	want := []string{posts[0], posts[1], mine}
	ids := func() []string {
		t.Helper()
		got, err := s.BookmarkIDs(bob.id(), 1000)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := ids(); !slices.Equal(got, want) {
		t.Fatalf("ids %v, want %v", got, want)
	}
	list, err := s.Bookmarks(bob.id(), "", 2)
	if err != nil || len(list) != 2 || list[0].ID != want[0] || list[1].ID != want[1] {
		t.Fatalf("first page: %v, %v", feedIDs(list), err)
	}
	list, err = s.Bookmarks(bob.id(), list[1].ID, 2)
	if err != nil || len(list) != 1 || list[0].ID != mine {
		t.Fatalf("second page: %v, %v", feedIDs(list), err)
	}
	list, err = s.BookmarksNewer(bob.id(), mine, 5)
	if err != nil || len(list) != 2 || list[0].ID != want[1] || list[1].ID != want[0] {
		t.Fatalf("newer from the last: %v, %v", feedIDs(list), err)
	}
	if _, err := s.Bookmarks(bob.id(), posts[2], 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a cursor no longer kept: want ErrNotFound, got %v", err)
	}
	if got, _ := s.BookmarkIDs(alice.id(), 1000); len(got) != 0 {
		t.Fatalf("alice keeps nothing, got %v", got)
	}

	ingest(t, s, alice, "post.delete", map[string]string{"post": posts[1]})
	want = []string{posts[0], mine}
	if got := ids(); !slices.Equal(got, want) {
		t.Fatalf("after a delete: %v, want %v", got, want)
	}
	if err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Equal(got, want) {
		t.Fatalf("after Rebuild: %v, want %v", got, want)
	}
	page, _, err := s.ReplicationPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range page {
		if strings.Contains(string(m.Envelope), "post.bookmark") {
			t.Fatal("a post.bookmark was offered to peers")
		}
	}
}

// TestNotifications: the replies to one's posts, the posts that mention
// one (as a page shows a mention), and the replies in a thread one wrote
// in that came after one's first post there — never one's own — newest
// first, each with why, paged.
func TestNotifications(t *testing.T) {
	s := openTest(t)
	me, bob, carol := newAuthor(t), newAuthor(t), newAuthor(t)
	step := func() { time.Sleep(2 * time.Millisecond) }
	post := func(a *author, text, replyTo string) string {
		t.Helper()
		body := map[string]string{"text": text}
		if replyTo != "" {
			body["reply_to"] = replyTo
		}
		id := ingest(t, s, a, "post.create", body)
		step()
		return id
	}
	// a mention as a page shows one: not inside a code span
	shows := func(text string) bool {
		return strings.Contains(text, "@"+me.id()) && !strings.Contains(text, "`@"+me.id())
	}

	mine := post(me, "my thread", "")
	r1 := post(bob, "a reply to me", mine)          // reply
	r2 := post(carol, "a reply to bob", r1)         // thread: in my thread, after my first post
	m1 := post(bob, "hello @"+me.id(), "")          // mention
	m2 := post(bob, "code `@"+me.id()+"`", "")      // not a mention as shown
	other := post(bob, "bob's thread", "")          // a thread I join later
	early := post(carol, "before I came", other)    // before my first post there: nothing
	in := post(me, "I join", other)                 // my own: nothing
	late := post(carol, "after I came", other)      // thread
	both := post(carol, "to me, and @"+me.id(), in) // reply before mention
	_ = post(bob, "elsewhere", "")                  // nothing to me
	_, _ = m2, early

	all, err := s.Notifications(me.id(), "", 50, shows)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ id, why string }
	var got []row
	for _, p := range all {
		got = append(got, row{p.ID, p.Why})
	}
	want := []row{{both, WhyReply}, {late, WhyThread}, {m1, WhyMention}, {r2, WhyThread}, {r1, WhyReply}}
	if !slices.Equal(got, want) {
		t.Fatalf("notifications\n got %v\nwant %v", got, want)
	}
	// paged by post id: two, then the rest from the second's cursor; the
	// false mention among them is passed over without shortening a page
	first, err := s.Notifications(me.id(), "", 2, shows)
	if err != nil || len(first) != 2 || first[1].ID != late {
		t.Fatalf("first page %v, %v", feedIDs(first), err)
	}
	second, err := s.Notifications(me.id(), first[1].ID, 2, shows)
	if err != nil || len(second) != 2 || second[0].ID != m1 || second[1].ID != r2 {
		t.Fatalf("second page %v, %v", feedIDs(second), err)
	}
	newer, err := s.NotificationsNewer(me.id(), r2, 2, shows)
	if err != nil || len(newer) != 2 || newer[0].ID != m1 || newer[1].ID != late {
		t.Fatalf("newer from r2 %v, %v", feedIDs(newer), err)
	}
	// someone with nothing: an empty list, not an error
	if none, err := s.Notifications(newAuthor(t).id(), "", 50, shows); err != nil || len(none) != 0 {
		t.Fatalf("a stranger's: %v, %v", feedIDs(none), err)
	}
}

func feedIDs(ps []FeedPost) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}
