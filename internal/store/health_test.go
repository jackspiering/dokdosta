package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// A failed read is counted, named a read, and told apart from an absent value; the health names a fixed operation, never
// the key it was about.
func TestFailuresAreCountedWithAFixedOperation(t *testing.T) {
	s := open(t, t.TempDir())
	if n, at, op := s.Health(); n != 0 || !at.IsZero() || op != "" {
		t.Fatalf("a fresh store has no errors: %d %v %q", n, at, op)
	}
	s.Close()
	if _, ok, err := s.Meta("boot_id"); ok || err == nil {
		t.Fatalf("a read of a closed store is an error, not an absent value: ok=%v err=%v", ok, err)
	}
	if n, at, op := s.Health(); n != 1 || at.IsZero() || op != string(OpReadMeta) {
		t.Fatalf("health after a failed read: %d %v %q", n, at, op)
	}
	s.SavePause(engine.Pause{Scope: "secret-project/web"})
	if n, _, op := s.Health(); n != 2 || op != string(OpWritePause) || strings.Contains(op, "secret") {
		t.Fatalf("health after a failed write: %d %q", n, op)
	}
}

// The bus is seeded from LastID. If the AUTOINCREMENT sequence is behind the table (its row lost), the seed is the highest
// stored ID: a seed below it would make RecordEvent drop every new event as a conflict.
func TestLastIDIsTheHighestOfTheSequenceAndTheTable(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	for i := uint64(1); i <= 5; i++ {
		s.RecordEvent(engine.Event{ID: i, Time: t0, Host: "h", Key: "h/p/web/1", Type: engine.Paused})
	}
	s.Close()
	raw(t, dir, `DELETE FROM sqlite_sequence`)
	s = open(t, dir)
	defer s.Close()
	if got := s.LastID(); got != 5 {
		t.Fatalf("LastID = %d, want 5", got)
	}
}

// A store whose event sequence cannot be read is not opened: whalenurse would seed its events from 0 and lose them.
func TestOpenFailsWhenTheEventSequenceCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	open(t, dir).Close()
	raw(t, dir, `DROP TABLE events`)
	if s, err := Open(dir, nil); err == nil {
		s.Close()
		t.Fatal("Open should fail")
	} else if !strings.Contains(err.Error(), "event sequence") {
		t.Fatalf("the error should say what failed: %v", err)
	}
}

// A data directory with the characters of a URI query, fragment or escape opens the file at that path, not an in-memory
// or another database.
func TestDataDirWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data?mode=memory#x%41")
	s := open(t, dir)
	s.SetMeta("boot_id", "abc")
	s.Close()
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("the database is not at the path: %v", err)
	}
	s = open(t, dir)
	defer s.Close()
	if v, ok, err := s.Meta("boot_id"); err != nil || !ok || v != "abc" {
		t.Fatalf("the value did not survive: %q %v %v", v, ok, err)
	}
}

// raw runs a statement on the database file, behind the store's back.
func raw(t *testing.T, dir, stmt string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+uriPath(filepath.Join(dir, FileName)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}
