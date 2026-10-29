package store

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// The store's files from before the rename (plan v3.15 item 6).
const (
	legacyFileName = "whalenurse.db"
	legacyLockName = "whalenurse.lock"

	// movingMarkerName exists while the move runs. The move renames -wal and -shm before the database, so one cut short
	// leaves dokdosta.db-wal beside whalenurse.db, the same files as a stale -wal; only the marker tells them apart.
	movingMarkerName = FileName + ".moving"
)

// ErrLegacyLocked is returned by Open when a process from before the rename still holds the data directory.
var ErrLegacyLocked = errors.New("a whalenurse is still running with this data directory; stop it first")

// migrateLegacy moves the database of a whalenurse from before the rename to FileName, once: in place, with os.Rename,
// never a copy. It runs under the new lock, and takes the old one too, so neither an old nor a second new process can
// use the files while they move. The -wal and -shm files go first and the database last: the -wal file holds committed
// transactions, and a move cut short (the database still under its old name) is finished on the next start.
func migrateLegacy(dir string, log *slog.Logger) error {
	old := filepath.Join(dir, legacyFileName)
	marker := filepath.Join(dir, movingMarkerName)
	if _, err := os.Lstat(old); errors.Is(err, os.ErrNotExist) {
		// A move cut short after its last rename leaves only the marker; it must not excuse a stale -wal later.
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn("state store: cannot remove the move marker", "file", movingMarkerName, "err", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	cur := filepath.Join(dir, FileName)
	if _, err := os.Lstat(cur); err == nil {
		return fmt.Errorf("both %s and %s are in %s: keep one (docs/upgrading.md)", legacyFileName, FileName, dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("data dir: %w", err)
	}
	moving := true
	if _, err := os.Lstat(marker); errors.Is(err, os.ErrNotExist) {
		moving = false
	} else if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	if !moving {
		// Without the marker no move was cut short, so these files are not ours: SQLite would replay their frames into
		// the moved database. Nothing is deleted; the operator decides.
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Lstat(cur + suffix); err == nil {
				return fmt.Errorf("%s is in %s without %s and no move in progress: remove dokdosta.db* or keep one store (docs/upgrading.md)",
					FileName+suffix, dir, FileName)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("data dir: %w", err)
			}
		}
	}
	lockPath := filepath.Join(dir, legacyLockName)
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("data dir lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("%w (%s)", ErrLegacyLocked, dir)
		}
		return fmt.Errorf("data dir lock: %w", err)
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cannot mark the move: %w", err)
	}
	f.Close()
	for _, suffix := range []string{"-wal", "-shm", ""} {
		from, to := old+suffix, cur+suffix
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("cannot move %s to %s: %w", filepath.Base(from), filepath.Base(to), err)
		}
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot remove the move marker: %w", err)
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn("state store: cannot remove the old lock file", "file", legacyLockName, "err", err)
	}
	log.Info("state store: moved the store from before the rename", "from", legacyFileName, "to", FileName, "dir", dir)
	return nil
}
