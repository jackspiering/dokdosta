// Package store persists dokdosta's state in SQLite (modernc.org/sqlite, no
// cgo) under DOKDOSTA_DATA_DIR: per-key state, incidents and their history,
// flap history, manual-stop markers, pauses, the rate-limit ledger and the boot
// id. It implements engine.Persister, so the engine never touches a file.
//
// Writes are synchronous and the connection is single: the volume is a handful
// of rows per incident, and a lost write only means the engine restarts from
// slightly older state. A write error is logged and counted, never fatal.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jackspiering/dokdosta/internal/engine"
)

// FileName is the database file inside the data directory.
const FileName = "dokdosta.db"

// schemaVersion is stored in PRAGMA user_version. Bump it with a migration step in migrate.
const schemaVersion = 5

// Store is an open database.
type Store struct {
	db   *sql.DB
	rdb  *sql.DB // a second, read-only connection for the API: a page view never queues behind (or in front of) a healer write
	log  *slog.Logger
	errs atomic.Int64
	lock *os.File // holds the flock on LockName while the store is open

	mu     sync.Mutex
	lastAt time.Time
	lastOp Op
}

// LockName is the file in the data directory that a running dokdosta holds an exclusive flock(2) on. A second dokdosta on
// the same directory would heal the same containers from the same state, so Open refuses it. The lock is not SQLite's
// own (those are fcntl locks, which any close in the process drops); it goes away with the process.
const LockName = "dokdosta.lock"

// ErrLocked is returned by Open when another process holds the data directory.
var ErrLocked = errors.New("another dokdosta is running with this data directory")

// Op names the kind of operation that failed. It is a fixed word, never a key or a scope, so it can be shown anywhere.
type Op string

const (
	OpWriteKey      Op = "write key"
	OpWriteIncident Op = "write incident"
	OpWritePause    Op = "write pause"
	OpWriteAction   Op = "write action"
	OpWriteMeta     Op = "write meta"
	OpWriteNotified Op = "write notification"
	OpWriteEvent    Op = "write event"
	OpReadMeta      Op = "read meta"
	OpReadEvents    Op = "read events"
)

// Open creates the data directory if needed, moves the store from before the rename (migrateLegacy), and opens (and
// migrates) the database.
func Open(dir string, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	lock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	if err := migrateLegacy(dir, log); err != nil {
		lock.Close()
		return nil, err
	}
	s, err := openDB(dir, log)
	if err != nil {
		lock.Close()
		return nil, err
	}
	s.lock = lock
	return s, nil
}

func lockDir(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, LockName), os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("data dir lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", ErrLocked, dir)
		}
		return nil, fmt.Errorf("data dir lock: %w", err)
	}
	return f, nil
}

func openDB(dir string, log *slog.Logger) (*Store, error) {
	path := filepath.Join(dir, FileName)
	dsn := "file:" + uriPath(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, log: log}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// Earlier versions emptied a parents:<key> row instead of deleting it, and wrote one for keys that never had a parent
	// set. An empty row means the same as none. A plain idempotent statement, not a schema step: a newer schema number
	// would stop an older dokdosta from opening this database.
	if _, err := db.Exec(`DELETE FROM meta WHERE key LIKE 'parents:%' AND value = ''`); err != nil && log != nil {
		log.Warn("store: could not remove empty parents rows", "error", err.Error())
	}
	// WAL lets a reader run beside the writer. The reads of the API use their own connection; if it cannot be opened
	// they share the writer's, which only costs a little latency.
	s.rdb = db
	if r, err := sql.Open("sqlite", "file:"+uriPath(path)+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"); err == nil {
		r.SetMaxOpenConns(2)
		if err := r.Ping(); err == nil {
			s.rdb = r
		} else {
			r.Close()
			log.Warn("store: no separate read connection; the API shares the healer's", "error", err.Error())
		}
	}
	// The bus is seeded from the last event ID: a wrong seed would make RecordEvent's ON CONFLICT drop new events, so
	// a store whose sequence cannot be read is not opened.
	if _, err := s.lastID(); err != nil {
		s.Close()
		return nil, fmt.Errorf("open %s: read the event sequence: %w", path, err)
	}
	return s, nil
}

// uriPath escapes a file path for SQLite's file: URI: the driver splits the DSN at the first '?', and SQLite ends the
// path at a '#' and decodes %HH in it, so a data directory with any of those characters would name another database
// (DOKDOSTA_DATA_DIR=/data?mode=memory would keep the state in memory).
func uriPath(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
}

// Close closes the database and releases the data directory.
func (s *Store) Close() error {
	if s.rdb != s.db {
		s.rdb.Close()
	}
	err := s.db.Close()
	if s.lock != nil {
		s.lock.Close()
	}
	return err
}

// Errors counts the operations (writes, and the reads of meta and of the event sequence) that failed since Open.
func (s *Store) Errors() int64 { return s.errs.Load() }

// Health is the error count, the time of the last failure and its kind (a fixed Op), for status, the API and /metrics.
func (s *Store) Health() (errors int64, lastAt time.Time, lastOp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs.Load(), s.lastAt, string(s.lastOp)
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > schemaVersion {
		return fmt.Errorf("database schema %d is newer than this dokdosta understands (%d)", v, schemaVersion)
	}
	if v < 1 {
		stmts := []string{
			`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
			`CREATE TABLE key_state (
				key TEXT PRIMARY KEY, name TEXT NOT NULL, marker_at INTEGER NOT NULL DEFAULT 0,
				loop_have INTEGER NOT NULL DEFAULT 0, loop_last_count INTEGER NOT NULL DEFAULT 0, loop_last_obs INTEGER NOT NULL DEFAULT 0,
				loop_n INTEGER NOT NULL DEFAULT 0, loop_first INTEGER NOT NULL DEFAULT 0, loop_last INTEGER NOT NULL DEFAULT 0,
				updated_at INTEGER NOT NULL)`,
			`CREATE TABLE flaps (key TEXT NOT NULL, at INTEGER NOT NULL)`,
			`CREATE INDEX flaps_key ON flaps (key)`,
			// One row per incident: closed_at NULL is the open one (at most one per key).
			`CREATE TABLE incidents (
				key TEXT NOT NULL, opened_at INTEGER NOT NULL, class TEXT NOT NULL,
				rung INTEGER NOT NULL DEFAULT 0, cooldown_until INTEGER NOT NULL DEFAULT 0,
				spent INTEGER NOT NULL DEFAULT 0, spent_reason TEXT NOT NULL DEFAULT '',
				suspended INTEGER NOT NULL DEFAULT 0, suspended_at INTEGER NOT NULL DEFAULT 0,
				recovering INTEGER NOT NULL DEFAULT 0, recovery_since INTEGER NOT NULL DEFAULT 0, recovery_count INTEGER NOT NULL DEFAULT 0,
				closed_at INTEGER, outcome TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (key, opened_at))`,
			`CREATE UNIQUE INDEX incidents_one_open ON incidents (key) WHERE closed_at IS NULL`,
			`CREATE TABLE pauses (scope TEXT PRIMARY KEY, until INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL)`,
			`CREATE TABLE actions (at INTEGER NOT NULL)`,
			"PRAGMA user_version = 1",
		}
		if err := s.exec(stmts); err != nil {
			return err
		}
		v = 1
	}
	if v < 2 {
		// M3 (plan v3.7): the notification dedupe, so a restart does not say the same thing twice.
		if err := s.exec([]string{
			`CREATE TABLE notified (id TEXT PRIMARY KEY, type TEXT NOT NULL, key TEXT NOT NULL, at INTEGER NOT NULL)`,
			`CREATE INDEX notified_key ON notified (type, key)`,
			"PRAGMA user_version = 2",
		}); err != nil {
			return err
		}
	}
	if v < 3 {
		// M5 (plan v3.9): the domain events, for the API's `since` cursor, the timelines and the history page.
		// AUTOINCREMENT keeps sqlite_sequence at the highest ID ever stored, so IDs stay monotonic after retention
		// has deleted the newest rows and the daemon restarts.
		if err := s.exec([]string{
			`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, host TEXT NOT NULL, key TEXT NOT NULL,
				container TEXT NOT NULL, type TEXT NOT NULL, message TEXT NOT NULL, attrs TEXT NOT NULL)`,
			`CREATE INDEX events_key ON events (key, id)`,
			`CREATE INDEX events_at ON events (at)`,
			"PRAGMA user_version = 3",
		}); err != nil {
			return err
		}
	}
	if v < 4 {
		// The overview asks for events of a few types on every refresh and the history page lists closed incidents
		// newest first; without these both read the whole table at the 100,000-row cap.
		if err := s.exec([]string{
			`CREATE INDEX events_type ON events (type, id)`,
			`CREATE INDEX incidents_closed ON incidents (closed_at)`,
			"PRAGMA user_version = 4",
		}); err != nil {
			return err
		}
	}
	if v < 5 {
		// The parent a dependents-step repair was planned against, so the first parent change after a restart of
		// dokdosta is seen as a change. Container IDs and start times only: a re-key leaves it alone.
		return s.exec([]string{
			`ALTER TABLE incidents ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
			"PRAGMA user_version = 5",
		})
	}
	return nil
}

// exec runs the statements in one transaction.
func (s *Store) exec(stmts []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", firstWords(q), err)
		}
	}
	return tx.Commit()
}

func firstWords(q string) string {
	if len(q) > 40 {
		return q[:40]
	}
	return q
}

// tn stores a time as unix nanoseconds; the zero time is 0.
func tn(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// fn is the inverse of tn.
func fn(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// fail counts and logs a failed operation; the engine carries on from memory. what (a key, a scope) goes to the log only.
func (s *Store) fail(op Op, what string, err error) {
	s.mu.Lock()
	s.errs.Add(1)
	s.lastAt, s.lastOp = time.Now().UTC(), op
	s.mu.Unlock()
	msg := "store write failed; continuing from memory"
	if strings.HasPrefix(string(op), "read") {
		msg = "store read failed"
	}
	args := []any{"op", string(op), "error", err.Error()}
	if what != "" {
		args = append(args, "what", what)
	}
	s.log.Error(msg, args...)
}

// ---- meta ----------------------------------------------------------------------------------------------

// Meta returns a meta value: ok is false when there is none, and err is set (and counted) when it could not be read,
// which is not the same as absent.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		s.fail(OpReadMeta, key, err)
		return "", false, err
	}
	return value, true, nil
}

// SetMeta stores a meta value.
func (s *Store) SetMeta(key, value string) {
	if _, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		s.fail(OpWriteMeta, key, err)
	}
}

// DeleteMeta removes a meta value.
func (s *Store) DeleteMeta(key string) {
	if _, err := s.db.Exec(`DELETE FROM meta WHERE key = ?`, key); err != nil {
		s.fail(OpWriteMeta, key, err)
	}
}

// ---- engine.Persister -------------------------------------------------------------------------------

var _ engine.Persister = (*Store)(nil)

// SaveKey writes the key's state and its flap history, and the open incident if any.
func (s *Store) SaveKey(r engine.KeyRecord) {
	err := s.tx(func(tx *sql.Tx) error {
		now := time.Now().UnixNano()
		l := r.Loop
		if _, err := tx.Exec(`INSERT INTO key_state (key, name, marker_at, loop_have, loop_last_count, loop_last_obs, loop_n, loop_first, loop_last, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET name = excluded.name, marker_at = excluded.marker_at, loop_have = excluded.loop_have,
				loop_last_count = excluded.loop_last_count, loop_last_obs = excluded.loop_last_obs, loop_n = excluded.loop_n,
				loop_first = excluded.loop_first, loop_last = excluded.loop_last, updated_at = excluded.updated_at`,
			r.Key, r.Name, tn(r.MarkerAt), b2i(l.Have), l.LastCount, tn(l.LastObs), l.N, tn(l.First), tn(l.Last), now); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM flaps WHERE key = ?`, r.Key); err != nil {
			return err
		}
		for _, f := range r.Flaps {
			if _, err := tx.Exec(`INSERT INTO flaps (key, at) VALUES (?, ?)`, r.Key, tn(f)); err != nil {
				return err
			}
		}
		// Any open row other than this incident is stale: close it.
		var keep int64
		if r.Incident != nil {
			keep = tn(r.Incident.Opened)
		}
		if _, err := tx.Exec(`UPDATE incidents SET closed_at = ?, outcome = 'cleared' WHERE key = ? AND closed_at IS NULL AND opened_at != ?`, now, r.Key, keep); err != nil {
			return err
		}
		if i := r.Incident; i != nil {
			_, err := tx.Exec(`INSERT INTO incidents (key, opened_at, class, rung, cooldown_until, spent, spent_reason, suspended, suspended_at, recovering, recovery_since, recovery_count, parent)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(key, opened_at) DO UPDATE SET class = excluded.class, rung = excluded.rung, cooldown_until = excluded.cooldown_until,
					spent = excluded.spent, spent_reason = excluded.spent_reason, suspended = excluded.suspended, suspended_at = excluded.suspended_at,
					recovering = excluded.recovering, recovery_since = excluded.recovery_since, recovery_count = excluded.recovery_count,
					parent = excluded.parent
				WHERE incidents.closed_at IS NULL`,
				r.Key, tn(i.Opened), i.Class, i.Rung, tn(i.CooldownUntil), b2i(i.Spent), i.SpentReason, b2i(i.Suspended), tn(i.SuspendedAt),
				b2i(i.Recovering), tn(i.RecoverySince), i.RecoveryCount, i.Parent)
			return err
		}
		return nil
	})
	if err != nil {
		s.fail(OpWriteKey, r.Key, err)
	}
}

// DeleteKey removes a key's state. Its closed incidents stay as history.
func (s *Store) DeleteKey(key string) {
	err := s.tx(func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM key_state WHERE key = ?`, `DELETE FROM flaps WHERE key = ?`} {
			if _, err := tx.Exec(q, key); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`UPDATE incidents SET closed_at = ?, outcome = 'gone' WHERE key = ? AND closed_at IS NULL`, time.Now().UnixNano(), key)
		return err
	})
	if err != nil {
		s.fail(OpWriteKey, key, err)
	}
}

// CloseIncident writes the end of an incident into the history.
func (s *Store) CloseIncident(c engine.ClosedIncident) {
	_, err := s.db.Exec(`INSERT INTO incidents (key, opened_at, class, closed_at, outcome) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key, opened_at) DO UPDATE SET closed_at = excluded.closed_at, outcome = excluded.outcome`,
		c.Key, tn(c.Opened), c.Class, tn(c.Closed), c.Outcome)
	if err != nil {
		s.fail(OpWriteIncident, c.Key, err)
	}
}

// SavePause stores a pause.
func (s *Store) SavePause(p engine.Pause) {
	if _, err := s.db.Exec(`INSERT INTO pauses (scope, until, created) VALUES (?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET until = excluded.until, created = excluded.created`, p.Scope, tn(p.Until), tn(p.Created)); err != nil {
		s.fail(OpWritePause, p.Scope, err)
	}
}

// DeletePause removes a pause.
func (s *Store) DeletePause(scope string) {
	if _, err := s.db.Exec(`DELETE FROM pauses WHERE scope = ?`, scope); err != nil {
		s.fail(OpWritePause, scope, err)
	}
}

// SaveAction records an action for the rate limit.
func (s *Store) SaveAction(at time.Time) {
	if _, err := s.db.Exec(`INSERT INTO actions (at) VALUES (?)`, tn(at)); err != nil {
		s.fail(OpWriteAction, "", err)
	}
}

// LoadNotified returns the notification dedupe ledger: the identity of every message sent within ttl, and when. Older
// rows are deleted. It implements notify.Ledger.
func (s *Store) LoadNotified(now time.Time, ttl time.Duration) (map[string]time.Time, error) {
	if _, err := s.db.Exec(`DELETE FROM notified WHERE at < ?`, tn(now.Add(-ttl))); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, at FROM notified`)
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = fn(at)
	}
	return out, closeRows(rows)
}

// SaveNotified records that the message with this identity was sent.
func (s *Store) SaveNotified(id, typ, key string, at time.Time) {
	if _, err := s.db.Exec(`INSERT INTO notified (id, type, key, at) VALUES (?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET at = excluded.at`, id, typ, key, tn(at)); err != nil {
		s.fail(OpWriteNotified, "", err)
	}
}

// ForgetNotified deletes the ledger rows of one event type for one key (a recovery makes the next "giving up" news again).
func (s *Store) ForgetNotified(typ, key string) {
	if _, err := s.db.Exec(`DELETE FROM notified WHERE type = ? AND key = ?`, typ, key); err != nil {
		s.fail(OpWriteNotified, "", err)
	}
}

// PruneNotified deletes ledger rows older than ttl.
func (s *Store) PruneNotified(now time.Time, ttl time.Duration) error {
	_, err := s.db.Exec(`DELETE FROM notified WHERE at < ?`, tn(now.Add(-ttl)))
	return err
}

// Load reads everything the engine persisted. It prunes the rate-limit ledger to the last hour first.
func (s *Store) Load(now time.Time) (engine.Snapshot, error) {
	var snap engine.Snapshot
	if _, err := s.db.Exec(`DELETE FROM actions WHERE at < ?`, tn(now.Add(-time.Hour))); err != nil {
		return snap, err
	}

	flaps := map[string][]time.Time{}
	rows, err := s.db.Query(`SELECT key, at FROM flaps ORDER BY at`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var k string
		var at int64
		if err := rows.Scan(&k, &at); err != nil {
			rows.Close()
			return snap, err
		}
		flaps[k] = append(flaps[k], fn(at))
	}
	if err := closeRows(rows); err != nil {
		return snap, err
	}

	incs := map[string]*engine.IncidentRecord{}
	rows, err = s.db.Query(`SELECT key, opened_at, class, rung, cooldown_until, spent, spent_reason, suspended, suspended_at, recovering, recovery_since, recovery_count, parent
		FROM incidents WHERE closed_at IS NULL`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var k string
		var opened, cool, susAt, recSince int64
		var spent, sus, rec int
		r := &engine.IncidentRecord{}
		if err := rows.Scan(&k, &opened, &r.Class, &r.Rung, &cool, &spent, &r.SpentReason, &sus, &susAt, &rec, &recSince, &r.RecoveryCount, &r.Parent); err != nil {
			rows.Close()
			return snap, err
		}
		r.Opened, r.CooldownUntil, r.SuspendedAt, r.RecoverySince = fn(opened), fn(cool), fn(susAt), fn(recSince)
		r.Spent, r.Suspended, r.Recovering = spent != 0, sus != 0, rec != 0
		incs[k] = r
	}
	if err := closeRows(rows); err != nil {
		return snap, err
	}

	rows, err = s.db.Query(`SELECT key, name, marker_at, loop_have, loop_last_count, loop_last_obs, loop_n, loop_first, loop_last FROM key_state ORDER BY key`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var k engine.KeyRecord
		var marker, obs, first, last int64
		var have int
		if err := rows.Scan(&k.Key, &k.Name, &marker, &have, &k.Loop.LastCount, &obs, &k.Loop.N, &first, &last); err != nil {
			rows.Close()
			return snap, err
		}
		k.MarkerAt, k.Loop.Have, k.Loop.LastObs, k.Loop.First, k.Loop.Last = fn(marker), have != 0, fn(obs), fn(first), fn(last)
		k.Flaps, k.Incident = flaps[k.Key], incs[k.Key]
		snap.Keys = append(snap.Keys, k)
	}
	if err := closeRows(rows); err != nil {
		return snap, err
	}

	rows, err = s.db.Query(`SELECT scope, until, created FROM pauses ORDER BY scope`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var p engine.Pause
		var until, created int64
		if err := rows.Scan(&p.Scope, &until, &created); err != nil {
			rows.Close()
			return snap, err
		}
		p.Until, p.Created = fn(until), fn(created)
		snap.Pauses = append(snap.Pauses, p)
	}
	if err := closeRows(rows); err != nil {
		return snap, err
	}

	rows, err = s.db.Query(`SELECT at FROM actions ORDER BY at`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			rows.Close()
			return snap, err
		}
		snap.Actions = append(snap.Actions, fn(at))
	}
	return snap, closeRows(rows)
}

func closeRows(r *sql.Rows) error {
	if err := r.Err(); err != nil {
		r.Close()
		return err
	}
	return r.Close()
}

// Prune applies retention: closed incidents older than retention, flap rows
// older than flapWindow, and rate-limit rows older than an hour. It returns how
// many closed incidents it deleted.
func (s *Store) Prune(now time.Time, retention, flapWindow time.Duration) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM incidents WHERE closed_at IS NOT NULL AND closed_at < ?`, tn(now.Add(-retention)))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := s.db.Exec(`DELETE FROM flaps WHERE at < ?`, tn(now.Add(-flapWindow))); err != nil {
		return n, err
	}
	if _, err = s.db.Exec(`DELETE FROM actions WHERE at < ?`, tn(now.Add(-time.Hour))); err != nil {
		return n, err
	}
	return n, s.pruneEvents(now, retention)
}

// MaxEvents caps the events table whatever the retention says: a flapping container can emit a lot.
const MaxEvents = 100_000

// pruneEvents deletes events older than the retention, except those of an incident that is still open, and then the
// oldest beyond MaxEvents: first events of closed incidents and events without one, those of open incidents only as the
// last resort (plan v3.13 item 21). AUTOINCREMENT keeps the ID counter, so the deletion never makes an ID come back.
func (s *Store) pruneEvents(now time.Time, retention time.Duration) error {
	return s.pruneEventsMax(now, retention, MaxEvents)
}

func (s *Store) pruneEventsMax(now time.Time, retention time.Duration, max int) error {
	open, args, err := s.openIncidentEvents()
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE at < ? AND NOT `+open, append([]any{tn(now.Add(-retention))}, args...)...); err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil || n <= max {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE id IN (SELECT id FROM events WHERE NOT `+open+` ORDER BY id LIMIT ?)`,
		append(args, n-max)...); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM events WHERE id <= (SELECT id FROM events ORDER BY id DESC LIMIT 1 OFFSET ?)`, max)
	return err
}

// openIncidentEvents is an SQL condition, with its arguments, that holds for the events of an incident still open: the
// event's key and its incident attribute (engine.AttrIncident, the opening time) match an incident without closed_at.
func (s *Store) openIncidentEvents() (string, []any, error) {
	rows, err := s.db.Query(`SELECT key, opened_at FROM incidents WHERE closed_at IS NULL`)
	if err != nil {
		return "", nil, err
	}
	var args []any
	for rows.Next() {
		var k string
		var opened int64
		if err := rows.Scan(&k, &opened); err != nil {
			rows.Close()
			return "", nil, err
		}
		args = append(args, k+"\x00"+fn(opened).Format(time.RFC3339Nano))
	}
	if err := closeRows(rows); err != nil {
		return "", nil, err
	}
	if len(args) == 0 {
		return "0", nil, nil
	}
	return `(key || char(0) || COALESCE(json_extract(attrs, '$.` + engine.AttrIncident + `'), '') IN (` + placeholders(len(args)) + `))`, args, nil
}

// ---- events (the API's log) ----------------------------------------------------------------------------

var _ engine.EventLog = (*Store)(nil)

// RecordEvent is the bus subscriber that stores every domain event under the ID the bus gave it.
func (s *Store) RecordEvent(e engine.Event) {
	attrs := "{}"
	if len(e.Attrs) > 0 {
		if b, err := json.Marshal(e.Attrs); err == nil {
			attrs = string(b)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO events (id, at, host, key, container, type, message, attrs) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, e.ID, tn(e.Time), e.Host, e.Key, e.Container, string(e.Type), e.Message, attrs); err != nil {
		s.fail(OpWriteEvent, "", err)
	}
}

// LastID is the highest event ID ever stored (also after retention deleted the newest rows); 0 for none. Open has
// already read it once, so a failure here is a store gone bad since; it is counted.
func (s *Store) LastID() uint64 {
	n, err := s.lastID()
	if err != nil {
		s.fail(OpReadEvents, "", err)
	}
	return n
}

// lastID is the larger of the AUTOINCREMENT sequence and the highest ID in the table: either alone can be behind (a
// sequence row lost or reset), and a seed behind a stored ID would make RecordEvent drop every new event as a conflict.
func (s *Store) lastID() (uint64, error) {
	var n int64
	err := s.rdb.QueryRow(`SELECT MAX(COALESCE((SELECT seq FROM sqlite_sequence WHERE name = 'events'), 0),
		COALESCE((SELECT MAX(id) FROM events), 0))`).Scan(&n)
	if err != nil || n < 0 {
		return 0, err
	}
	return uint64(n), nil
}

// placeholders is "?,?,...,?" for n > 0 values, built in linear time.
func placeholders(n int) string { return strings.Repeat(",?", n)[1:] }

// Query implements engine.EventLog.
func (s *Store) Query(q engine.EventQuery) ([]engine.Event, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	where, args := []string{"id > ?"}, []any{q.After}
	if q.Before != 0 {
		where, args = append(where, "id < ?"), append(args, q.Before)
	}
	if q.Key != "" {
		where, args = append(where, "key = ?"), append(args, q.Key)
	}
	if len(q.Types) > 0 {
		for _, t := range q.Types {
			args = append(args, string(t))
		}
		where = append(where, "type IN ("+placeholders(len(q.Types))+")")
	}
	if len(q.ExcludeTypes) > 0 {
		for _, t := range q.ExcludeTypes {
			args = append(args, string(t))
		}
		where = append(where, "type NOT IN ("+placeholders(len(q.ExcludeTypes))+")")
	}
	order := "ASC"
	if q.Newest {
		order = "DESC"
	}
	stmt := `SELECT id, at, host, key, container, type, message, attrs FROM events WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY id ` + order + ` LIMIT ?`
	rows, err := s.rdb.Query(stmt, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	var out []engine.Event
	for rows.Next() {
		var e engine.Event
		var at int64
		var typ, attrs string
		if err := rows.Scan(&e.ID, &at, &e.Host, &e.Key, &e.Container, &typ, &e.Message, &attrs); err != nil {
			rows.Close()
			return nil, err
		}
		e.Time, e.Type = fn(at), engine.EventType(typ)
		if attrs != "" && attrs != "{}" {
			_ = json.Unmarshal([]byte(attrs), &e.Attrs)
		}
		out = append(out, e)
	}
	return out, closeRows(rows)
}

// History returns the closed incidents of a key, newest first (the dashboard's
// History page and the tests use it).
func (s *Store) History(key string) ([]engine.ClosedIncident, error) { return s.HistoryLimit(key, 0) }

// HistoryLimit is History cut to the newest limit rows in SQL; a limit of 0 or less means all of them.
func (s *Store) HistoryLimit(key string, limit int) ([]engine.ClosedIncident, error) {
	if limit <= 0 {
		limit = -1 // SQLite: a negative LIMIT is no limit
	}
	rows, err := s.rdb.Query(`SELECT key, opened_at, class, closed_at, outcome FROM incidents WHERE closed_at IS NOT NULL AND (? = '' OR key = ?) ORDER BY closed_at DESC, opened_at DESC LIMIT ?`, key, key, limit)
	if err != nil {
		return nil, err
	}
	var out []engine.ClosedIncident
	for rows.Next() {
		var c engine.ClosedIncident
		var opened, closed int64
		if err := rows.Scan(&c.Key, &opened, &c.Class, &closed, &c.Outcome); err != nil {
			rows.Close()
			return nil, err
		}
		c.Opened, c.Closed = fn(opened), fn(closed)
		out = append(out, c)
	}
	return out, closeRows(rows)
}

func (s *Store) tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
