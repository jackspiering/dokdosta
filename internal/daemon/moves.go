package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackspiering/dokdosta/internal/compose"
	"github.com/jackspiering/dokdosta/internal/docker"
)

// metaTagMoved is the prefix of the store keys that remember which container moved which tag.
const metaTagMoved = "tagmoved:"

// tagMove is a record that dokdosta's own pull rung moved a tag: other stacks that share the tag see image drift
// afterwards, and the drift message names who did it and when (plan §3.6, §3.7).
type tagMove struct {
	Ref      string    `json:"ref"`       // the normalized reference
	Key      string    `json:"key"`       // the state key of the container whose pull rung moved it
	At       time.Time `json:"at"`        // when
	Old      string    `json:"old"`       // what the reference resolved to for this platform before ("" if there was no local image)
	New      string    `json:"new"`       // ... and after
	OldPlain string    `json:"old_plain"` // the same without a platform: the index digest on the containerd store
	NewPlain string    `json:"new_plain"`
	// Followed marks a move that was not made by dokdosta: rung 2 recreated a pull-enabled container onto the image the
	// tag had been moved to by someone else (plan v3.13 item 6). The dependents step may take it like a pull's, but it is
	// not named as the mover in a drift block of another stack.
	Followed bool `json:"followed,omitempty"`
	// Followers are other pull-enabled containers whose rung 2 took the same result (same New): each with its own old image,
	// so that their sidecars may take it too, and none of them replaces the mover or the others.
	Followers []mover `json:"followers,omitempty"`
}

// mover is one container's part in a move: who, when, and the image it was running (as the compose image label and as the
// image ID), which its sidecars were created from.
type mover struct {
	Key      string    `json:"key"`
	At       time.Time `json:"at"`
	Old      string    `json:"old"`
	OldPlain string    `json:"old_plain"`
}

func (m tagMove) movers() []mover {
	return append([]mover{{Key: m.Key, At: m.At, Old: m.Old, OldPlain: m.OldPlain}}, m.Followers...)
}

// moveLedger is the record of the tags dokdosta's pull rung moved and the moves rung 2 followed (plan §3.6, §3.7, v3.13
// item 6): kept by normalized reference in memory and in the store's meta ("tagmoved:<ref>", read lazily, none in a dry
// run), so a drift block on another stack days later can still name the mover. It answers who moved a tag while the tag
// still resolves to that move's result (standing); what a dependent may do with that is the daemon's question
// (dependentMayTakeMove), because it needs the container list.
type moveLedger struct {
	meta  Meta
	moves map[string]tagMove
}

func newMoveLedger(m Meta) *moveLedger {
	if m == nil {
		m = nopMeta{}
	}
	return &moveLedger{meta: m, moves: map[string]tagMove{}}
}

// record remembers the move, replacing what was known about its reference.
func (l *moveLedger) record(m tagMove) {
	l.moves[m.Ref] = m
	if b, err := json.Marshal(m); err == nil {
		l.meta.SetMeta(metaTagMoved+m.Ref, string(b))
	}
}

// follow remembers that rung 2 of the container took a tag that someone else moved. When the record of the tag already
// stands for the same result (a pull of another container, or another container's followed move), the container is added
// to it and nothing is replaced; otherwise it is the record.
func (l *moveLedger) follow(ref string, by mover, now, nowPlain string) {
	ref = normalizeRef(ref)
	if cur, ok := l.of(ref); ok && cur.New == now && cur.NewPlain == nowPlain {
		if cur.Key == by.Key {
			return
		}
		cur.Followers = slices.DeleteFunc(slices.Clone(cur.Followers), func(f mover) bool { return f.Key == by.Key })
		cur.Followers = append(cur.Followers, by)
		l.record(cur)
		return
	}
	l.record(tagMove{Ref: ref, Key: by.Key, At: by.At, Followed: true, Old: by.Old, OldPlain: by.OldPlain, New: now, NewPlain: nowPlain})
}

// of returns the record of a reference, whatever it resolves to now.
func (l *moveLedger) of(ref string) (tagMove, bool) {
	ref = normalizeRef(ref)
	if m, ok := l.moves[ref]; ok {
		return m, true
	}
	if v, ok, _ := l.meta.Meta(metaTagMoved + ref); ok { // a read error is counted and logged by the store
		var m tagMove
		if json.Unmarshal([]byte(v), &m) == nil && m.Ref == ref {
			return m, true
		}
	}
	return tagMove{}, false
}

// standing returns the record of a reference only while the reference still resolves to what that move produced (for
// this platform, or without one): if someone moved the tag again since, nothing is said about the earlier move.
func (l *moveLedger) standing(ctx context.Context, images compose.ImageResolver, ref, arch string) (tagMove, bool) {
	m, ok := l.of(ref)
	if !ok {
		return tagMove{}, false
	}
	if now, err := images.ImageID(ctx, ref, arch); err == nil && m.New != "" && now == m.New {
		return m, true
	}
	if now, err := images.ImageID(ctx, ref, ""); err == nil && m.NewPlain != "" && now == m.NewPlain {
		return m, true
	}
	return tagMove{}, false
}

// normalizeRef makes `nginx`, `nginx:latest` and `docker.io/library/nginx:latest` the same reference, so a move recorded
// under one spelling is found under another (plan §3.7). A reference with a digest is kept as it is.
func normalizeRef(ref string) string {
	r := strings.TrimSpace(ref)
	if r == "" || strings.Contains(r, "@") {
		return r
	}
	for _, p := range []string{"index.docker.io/", "registry-1.docker.io/", "docker.io/"} {
		r = strings.TrimPrefix(r, p)
	}
	if rest, ok := strings.CutPrefix(r, "library/"); ok && !strings.Contains(rest, "/") {
		r = rest
	}
	if last := r[strings.LastIndex(r, "/")+1:]; !strings.Contains(last, ":") {
		r += ":latest"
	}
	return r
}

// upgradeRollback adds an upgrade's rollback to the attributes of a TagMoved or Upgraded event: the image to go back to,
// the two commands (never cut, plan §3.7) and the sidecars the up line names. Nothing when there is no image to go back to.
func upgradeRollback(attrs map[string]string, p compose.Project, ref, back string, sidecars []string) {
	if back == "" {
		return
	}
	attrs["old_image"] = back
	attrs["rollback_tag"], attrs["rollback_up"] = compose.RollbackCommands(p, ref, back, sidecars...)
	if len(sidecars) > 0 {
		attrs["sidecars"] = strings.Join(sidecars, ",")
	}
}

// moverOf says who moved the tag that the container's image reference points at, while it still stands (moveLedger.standing).
func (d *Daemon) moverOf(ctx context.Context, c docker.Container) (tagMove, bool) {
	return d.moves.standing(ctx, d.rd, c.Image, d.cs().server.Arch)
}

// rekeyTagMove moves the keys inside a "tagmoved:" record (the mover's and its followers', plan v3.13 item 6) for
// store.Rekey. It edits the JSON in place, so a field it does not know stays as it was.
func rekeyTagMove(value string, rekey func(string) string) (string, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &obj) != nil {
		return value, false
	}
	changed := false
	var k string
	if json.Unmarshal(obj["key"], &k) == nil {
		if to := rekey(k); to != k {
			obj["key"], _ = json.Marshal(to)
			changed = true
		}
	}
	var followers []map[string]json.RawMessage
	if raw, ok := obj["followers"]; ok && json.Unmarshal(raw, &followers) == nil {
		followersChanged := false
		for _, f := range followers {
			var fk string
			if json.Unmarshal(f["key"], &fk) != nil {
				continue
			}
			if to := rekey(fk); to != fk {
				f["key"], _ = json.Marshal(to)
				followersChanged = true
			}
		}
		if followersChanged {
			obj["followers"], _ = json.Marshal(followers)
			changed = true
		}
	}
	if !changed {
		return value, false
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return value, false
	}
	return string(b), true
}
