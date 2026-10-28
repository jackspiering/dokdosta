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

// A move cut short after the -wal file (the database still under its old name) is finished on the next start.
func TestAMoveCutShortIsFinishedOnTheNextStart(t *testing.T) {
	dir := legacyDir(t)
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
