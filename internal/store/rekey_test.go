package store

import (
	"encoding/json"
	"testing"
)

var testMeta = RekeyMeta{Keyed: []string{"parents:"}, WithKey: []string{"tagmoved:"}}

func execAll(t *testing.T, s *Store, qs ...string) {
	t.Helper()
	for _, q := range qs {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func count(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Every record keyed by a state key of another host moves to the current host ID; the events keep theirs.
func TestRekeyMovesEveryRecordToTheCurrentHost(t *testing.T) {
	s := open(t, t.TempDir())
	execAll(t, s,
		`INSERT INTO key_state (key, name, marker_at, updated_at) VALUES ('old/p/web/1', 'p-web-1', 5, 10), ('old/solo', 'solo', 0, 10)`,
		`INSERT INTO flaps (key, at) VALUES ('old/p/web/1', 7), ('old/p/web/1', 8)`,
		`INSERT INTO incidents (key, opened_at, class, rung, suspended) VALUES ('old/p/web/1', 9, 'unhealthy', 2, 1)`,
		`INSERT INTO incidents (key, opened_at, class, closed_at, outcome) VALUES ('old/p/web/1', 1, 'unhealthy', 2, 'recovered')`,
		`INSERT INTO notified (id, type, key, at) VALUES ('Suspended|old/p/web/1||2026-10-22T10:00:00Z', 'Suspended', 'old/p/web/1', 9)`,
		`INSERT INTO meta (key, value) VALUES ('parents:old/p/side/1', 'web'), ('tagmoved:docker.io/library/web:1', '{"ref":"docker.io/library/web:1","key":"old/p/web/1","at":"2026-10-22T10:00:00Z"}')`,
		`INSERT INTO events (at, host, key, container, type, message, attrs) VALUES (1, 'old', 'old/p/web/1', 'p-web-1', 'Suspended', 'x', '{}')`,
		// the current host's own state, and a key that is not a state key
		`INSERT INTO key_state (key, name, updated_at) VALUES ('new/p/db/1', 'p-db-1', 1), ('a/b/c/d/e', 'odd', 1)`,
	)
	n, err := s.Rekey("new", testMeta)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 { // old/p/web/1, old/solo, old/p/side/1
		t.Fatalf("moved %d keys, want 3", n)
	}
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM key_state WHERE key LIKE 'old/%'`, 0},
		{`SELECT count(*) FROM key_state WHERE key IN ('new/p/web/1', 'new/solo', 'new/p/db/1')`, 3},
		{`SELECT count(*) FROM key_state WHERE key = 'a/b/c/d/e'`, 1},
		{`SELECT count(*) FROM key_state WHERE key = 'new/p/web/1' AND marker_at = 5`, 1},
		{`SELECT count(*) FROM flaps WHERE key = 'new/p/web/1'`, 2},
		{`SELECT count(*) FROM incidents WHERE key = 'new/p/web/1'`, 2},
		{`SELECT count(*) FROM notified WHERE id = 'Suspended|new/p/web/1||2026-10-22T10:00:00Z' AND key = 'new/p/web/1'`, 1},
		{`SELECT count(*) FROM meta WHERE key = 'parents:new/p/side/1' AND value = 'web'`, 1},
		{`SELECT count(*) FROM meta WHERE key LIKE 'parents:old/%'`, 0},
		{`SELECT count(*) FROM events WHERE host = 'old' AND key = 'old/p/web/1'`, 1},
	} {
		if got := count(t, s, c.q); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
	var v string
	s.db.QueryRow(`SELECT value FROM meta WHERE key = 'tagmoved:docker.io/library/web:1'`).Scan(&v)
	var tm map[string]string
	if err := json.Unmarshal([]byte(v), &tm); err != nil || tm["key"] != "new/p/web/1" || tm["ref"] != "docker.io/library/web:1" || tm["at"] == "" {
		t.Fatalf("tagmoved value: %s", v)
	}
	snap, err := s.Load(t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range snap.Keys {
		if k.Key == "new/p/web/1" && (k.Incident == nil || !k.Incident.Suspended || len(k.Flaps) != 2) {
			t.Fatalf("loaded: %+v", k)
		}
	}
	if n, _ := s.Rekey("new", testMeta); n != 0 {
		t.Fatalf("a second run moved %d keys", n)
	}
}

// When the current key already has records, the key with the later activity wins and the other's records are dropped.
func TestRekeyKeepsTheLaterOnACollision(t *testing.T) {
	s := open(t, t.TempDir())
	execAll(t, s,
		// p/web: the old host's record is newer; p/db: the current one is.
		`INSERT INTO key_state (key, name, marker_at, updated_at) VALUES ('old/p/web/1', 'w', 1, 50), ('new/p/web/1', 'w', 0, 20)`,
		`INSERT INTO flaps (key, at) VALUES ('old/p/web/1', 40), ('new/p/web/1', 15)`,
		`INSERT INTO incidents (key, opened_at, class) VALUES ('old/p/web/1', 30, 'unhealthy'), ('new/p/web/1', 10, 'exited')`,
		`INSERT INTO key_state (key, name, updated_at) VALUES ('old/p/db/1', 'd', 5), ('new/p/db/1', 'd', 60)`,
		`INSERT INTO incidents (key, opened_at, class) VALUES ('old/p/db/1', 4, 'unhealthy')`,
	)
	if _, err := s.Rekey("new", testMeta); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM key_state WHERE key LIKE 'old/%'`, 0},
		{`SELECT count(*) FROM key_state WHERE key = 'new/p/web/1' AND marker_at = 1 AND updated_at = 50`, 1},
		{`SELECT count(*) FROM flaps WHERE key = 'new/p/web/1' AND at = 40`, 1},
		{`SELECT count(*) FROM flaps WHERE key = 'new/p/web/1'`, 1},
		{`SELECT count(*) FROM incidents WHERE key = 'new/p/web/1' AND class = 'unhealthy'`, 1},
		{`SELECT count(*) FROM incidents WHERE key = 'new/p/web/1'`, 1},
		{`SELECT count(*) FROM key_state WHERE key = 'new/p/db/1' AND updated_at = 60`, 1},
		{`SELECT count(*) FROM incidents WHERE key LIKE '%/p/db/1'`, 0},
	} {
		if got := count(t, s, c.q); got != c.want {
			t.Errorf("%s = %d, want %d", c.q, got, c.want)
		}
	}
}

// The notification dedupe also holds pause messages, keyed host/<scope>: a recent `pause p` under the current host must
// not take the place of a non-compose container named p and drop its state.
func TestRekeyIgnoresDedupeRowsThatAreNotState(t *testing.T) {
	s := open(t, t.TempDir())
	execAll(t, s,
		`INSERT INTO key_state (key, name, marker_at, updated_at) VALUES ('old/p', 'p', 3, 10)`,
		`INSERT INTO notified (id, type, key, at) VALUES ('Paused|new/p||', 'Paused', 'new/p', 100)`,
	)
	if _, err := s.Rekey("new", testMeta); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, `SELECT count(*) FROM key_state WHERE key = 'new/p' AND marker_at = 3`); got != 1 {
		t.Fatalf("the container's state was dropped for a dedupe row: %d", got)
	}
}

// A dedupe row that already exists under the new key does not stop the startup.
func TestRekeyMergesADedupeRowThatExistsUnderTheNewKey(t *testing.T) {
	s := open(t, t.TempDir())
	execAll(t, s,
		`INSERT INTO key_state (key, name, updated_at) VALUES ('old/p/web/1', 'w', 10)`,
		`INSERT INTO notified (id, type, key, at) VALUES ('Suspended|old/p/web/1||x', 'Suspended', 'old/p/web/1', 9), ('Suspended|new/p/web/1||x', 'Suspended', 'new/p/web/1', 5)`,
	)
	if _, err := s.Rekey("new", testMeta); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, `SELECT count(*) FROM notified WHERE id = 'Suspended|new/p/web/1||x' AND at = 9`); got != 1 {
		t.Fatalf("dedupe rows: %d", got)
	}
}

// The followers of a tagmoved record (plan v3.13 item 6) are keys too: each moves to the current host, also when the
// record's own key is already current, and the fields the rewrite does not know stay as they were.
func TestRekeyMovesTheFollowersOfTagMovedRecords(t *testing.T) {
	s := open(t, t.TempDir())
	execAll(t, s,
		`INSERT INTO meta (key, value) VALUES
			('tagmoved:docker.io/library/a:1', '{"ref":"docker.io/library/a:1","key":"old/p/web/1","followers":[{"key":"old/p/side/1","note":"kept"},{"key":"old/p/side/2"}]}'),
			('tagmoved:docker.io/library/b:1', '{"ref":"docker.io/library/b:1","key":"new/p/web/1","followers":[{"key":"old/p/side/3"}]}')`,
	)
	if _, err := s.Rekey("new", testMeta); err != nil {
		t.Fatal(err)
	}
	type follower struct {
		Key  string `json:"key"`
		Note string `json:"note"`
	}
	type record struct {
		Key       string     `json:"key"`
		Followers []follower `json:"followers"`
	}
	for _, c := range []struct {
		ref  string
		want []string
	}{
		{"docker.io/library/a:1", []string{"new/p/side/1", "new/p/side/2"}},
		{"docker.io/library/b:1", []string{"new/p/side/3"}},
	} {
		var v string
		if err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, "tagmoved:"+c.ref).Scan(&v); err != nil {
			t.Fatal(err)
		}
		var r record
		if err := json.Unmarshal([]byte(v), &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Followers) != len(c.want) {
			t.Fatalf("%s: followers %+v, want %v", c.ref, r.Followers, c.want)
		}
		for i, f := range r.Followers {
			if f.Key != c.want[i] {
				t.Errorf("%s: follower %d key %q, want %q", c.ref, i, f.Key, c.want[i])
			}
		}
		if c.ref == "docker.io/library/a:1" && r.Followers[0].Note != "kept" {
			t.Errorf("a follower's unknown field was dropped: %+v", r.Followers[0])
		}
	}
}
