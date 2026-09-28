package store

import (
	"database/sql"
	"testing"
)

// TestPushProfileMigration: a database from before subscriptions named
// a profile opens, its subscribers kept and with no id — so they hear
// of every post, as they did — and one told again with an id keeps it.
func TestPushProfileMigration(t *testing.T) {
	path := t.TempDir() + "/hub.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE push_subs (
		endpoint TEXT PRIMARY KEY, p256dh BLOB NOT NULL, auth BLOB NOT NULL,
		base TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL);
		INSERT INTO push_subs(endpoint, p256dh, auth, base, created) VALUES ('https://push.example/old', x'04', x'01', 'https://hub.example', 1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	subs, err := st.PushSubs()
	if err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push.example/old" || subs[0].Profile != "" || subs[0].Base != "https://hub.example" {
		t.Fatalf("after the migration: %v %+v", err, subs)
	}
	sub := subs[0]
	sub.Profile = "0123456789abcdef"
	if err := st.PushAdd(sub); err != nil {
		t.Fatal(err)
	}
	if held, err := st.PushHas(sub.Endpoint); err != nil || !held {
		t.Errorf("PushHas: %v %v", held, err)
	}
	if held, _ := st.PushHas("https://push.example/other"); held {
		t.Error("PushHas: an endpoint nobody sent")
	}
	subs, err = st.PushSubs()
	if err != nil || len(subs) != 1 || subs[0].Profile != "0123456789abcdef" {
		t.Errorf("told again with an id: %v %+v", err, subs)
	}
}
