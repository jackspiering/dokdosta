package store

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

// legacyDir makes a data directory as a whalenurse from before the rename left it while running: the database with
// committed transactions still in its -wal file (not checkpointed), under the old names.
func legacyDir(t *testing.T) string {
	t.Helper()
	live := t.TempDir()
	s, err := Open(live, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetMeta("before-the-rename", "kept")
	dir := t.TempDir()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		copyFile(t, filepath.Join(live, FileName+suffix), filepath.Join(dir, legacyFileName+suffix))
	}
	if fi, err := os.Stat(filepath.Join(dir, legacyFileName+"-wal")); err != nil || fi.Size() == 0 {
		t.Fatalf("the test needs a -wal file with content: %v %v", fi, err)
	}
	s.Close()
	return dir
}

// Plan v3.15 item 6: the store from before the rename is moved once, in place, with what its -wal file holds.
func TestTheStoreFromBeforeTheRenameIsMovedWithItsWAL(t *testing.T) {
	dir := legacyDir(t)
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, ok, err := s.Meta("before-the-rename"); err != nil || !ok || v != "kept" {
		t.Fatalf("lost what the old store held: %q %v %v", v, ok, err)
	}
	for _, gone := range []string{legacyFileName, legacyFileName + "-wal", legacyFileName + "-shm", legacyLockName} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", gone, err)
		}
	}
}

// An old process that still holds its lock keeps its files: nothing moves.
func TestTheStoreIsNotMovedWhileAWhalenurseHoldsItsLock(t *testing.T) {
	dir := legacyDir(t)
	f, err := os.OpenFile(filepath.Join(dir, legacyLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); !errors.Is(err, ErrLegacyLocked) {
		t.Fatalf("want ErrLegacyLocked, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, legacyFileName)); err != nil {
		t.Fatal("the old database moved while its process held it")
	}
	if _, err := os.Lstat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a new database exists: %v", err)
	}
}

// Both databases present is a startup error: dokdosta does not choose.
func TestBothStoresPresentIsAnError(t *testing.T) {
	dir := legacyDir(t)
	if err := os.WriteFile(filepath.Join(dir, FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil || !strings.Contains(err.Error(), "keep one") {
		t.Fatalf("want the refusal, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, legacyFileName)); err != nil {
		t.Fatal("the old database moved")
	}
}

// A move cut short after the -wal file (the database still under its old name) is finished on the next start. The
// marker is what a move leaves behind when it is cut short, and what tells this from a stale -wal.
func TestAMoveCutShortIsFinishedOnTheNextStart(t *testing.T) {
	dir := legacyDir(t)
	if err := os.WriteFile(filepath.Join(dir, movingMarkerName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Rename(filepath.Join(dir, legacyFileName+suffix), filepath.Join(dir, FileName+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, ok, _ := s.Meta("before-the-rename"); !ok || v != "kept" {
		t.Fatalf("lost what the -wal held: %q %v", v, ok)
	}
	if _, err := os.Lstat(filepath.Join(dir, movingMarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the marker is still there after the move was finished: %v", err)
	}
}

// A move cut short before the first rename (the marker exists, nothing moved) is finished too.
func TestAMoveCutShortBeforeAnyRenameIsFinished(t *testing.T) {
	dir := legacyDir(t)
	if err := os.WriteFile(filepath.Join(dir, movingMarkerName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, ok, _ := s.Meta("before-the-rename"); !ok || v != "kept" {
		t.Fatalf("lost what the -wal held: %q %v", v, ok)
	}
}

// A move cut short after the last rename leaves only the marker: it is removed at the next start, so that it cannot
// excuse a stale -wal later.
func TestAMarkerLeftAfterTheLastRenameIsRemoved(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, movingMarkerName)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker is still there: %v", err)
	}
}

// A -wal or -shm of a dokdosta.db that is not there, next to the old database and with no move in progress, is not
// ours to replay into the moved database: the start is refused and nothing moves (R-014).
func TestAStaleWALOrSHMIsRefusedWhenTheStoreWouldMove(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			dir := legacyDir(t)
			// A database that was cleanly closed has no -wal of its own.
			for _, s := range []string{"-wal", "-shm"} {
				if err := os.Remove(filepath.Join(dir, legacyFileName+s)); err != nil {
					t.Fatal(err)
				}
			}
			stale := filepath.Join(dir, FileName+suffix)
			if err := os.WriteFile(stale, []byte("foreign frames"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(dir, nil)
			if err == nil || !strings.Contains(err.Error(), FileName+suffix) || !strings.Contains(err.Error(), "no move in progress") {
				t.Fatalf("want the refusal naming %s, got %v", FileName+suffix, err)
			}
			if b, err := os.ReadFile(stale); err != nil || string(b) != "foreign frames" {
				t.Errorf("the stale file was touched: %q %v", b, err)
			}
			if _, err := os.Lstat(filepath.Join(dir, legacyFileName)); err != nil {
				t.Errorf("the old database moved: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a new database exists: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(dir, movingMarkerName)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a marker was made: %v", err)
			}
		})
	}
}

// The marker is gone after a normal move.
func TestTheMarkerIsGoneAfterAMove(t *testing.T) {
	dir := legacyDir(t)
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Lstat(filepath.Join(dir, movingMarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker is still there: %v", err)
	}
}

// A data directory without the old store is opened as before.
func TestNoStoreFromBeforeTheRenameChangesNothing(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Lstat(filepath.Join(dir, legacyLockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an old lock file was made: %v", err)
	}
}
