package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/rules"
)

// pullTimeout bounds `compose pull`: an image can be large. The loop is busy for that long (rung 2's recreate is too,
// for up to composeTimeout); Healthy tolerates an action in flight for this long plus a minute.
const pullTimeout = 10 * time.Minute

// metaTagMoved is the prefix of the store keys that remember which container moved which tag.
const metaTagMoved = "tagmoved:"

// tagMove is a record that whalenurse's own pull rung moved a tag: other stacks that share the tag see image drift
// afterwards, and the drift message names who did it and when (plan §3.6, §3.7).
type tagMove struct {
	Ref      string    `json:"ref"`       // the normalized reference
	Key      string    `json:"key"`       // the state key of the container whose pull rung moved it
	At       time.Time `json:"at"`        // when
	Old      string    `json:"old"`       // what the reference resolved to for this platform before ("" if there was no local image)
	New      string    `json:"new"`       // ... and after
	OldPlain string    `json:"old_plain"` // the same without a platform: the index digest on the containerd store
	NewPlain string    `json:"new_plain"`
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

// recordMove remembers the move: in memory, and in the store when there is one, so that it is still known after a restart
// of whalenurse (a drift block on another stack can come days later).
func (d *Daemon) recordMove(m tagMove) {
	if d.moves == nil {
		d.moves = map[string]tagMove{}
	}
	d.moves[m.Ref] = m
	if d.o.Meta != nil {
		if b, err := json.Marshal(m); err == nil {
			d.o.Meta.SetMeta(metaTagMoved+m.Ref, string(b))
		}
	}
}

func (d *Daemon) moveOf(ref string) (tagMove, bool) {
	ref = normalizeRef(ref)
	if m, ok := d.moves[ref]; ok {
		return m, true
	}
	if d.o.Meta != nil {
		if v, ok := d.o.Meta.Meta(metaTagMoved + ref); ok {
			var m tagMove
			if json.Unmarshal([]byte(v), &m) == nil && m.Ref == ref {
				return m, true
			}
		}
	}
	return tagMove{}, false
}

// moverOf says who moved the tag that the container's image reference points at, or returns false. It claims the move only
// while the reference still resolves to what that pull produced: if someone moved the tag again since, nothing is
// said about the earlier pull.
func (d *Daemon) moverOf(ctx context.Context, c docker.Container) (tagMove, bool) {
	m, ok := d.moveOf(c.Image)
	if !ok {
		return tagMove{}, false
	}
	arch := d.cs().server.Arch
	if now, err := d.rd.ImageID(ctx, c.Image, arch); err == nil && m.New != "" && now == m.New {
		return m, true
	}
	if now, err := d.rd.ImageID(ctx, c.Image, ""); err == nil && m.NewPlain != "" && now == m.NewPlain {
		return m, true
	}
	return tagMove{}, false
}

// guardVerdict runs the drift guard (plan §3.6) before rung 2's recreate (pull false) or rung 3's pull (pull true) and
// says whether the action may go ahead. When it may not, the container is SUSPENDED (Blocked); when the findings were
// verified drift and WHALENURSE_DRIFT_GUARD=warn, the action goes ahead and says so. It runs in dry-run too: it only reads.
func (d *Daemon) guardVerdict(ctx context.Context, t engine.Target, c docker.Container, dec rules.Decision, p compose.Project, pull bool) bool {
	fs := d.driftFindings(ctx, c, p, pull)
	if len(fs) == 0 {
		return true
	}
	var findings []string
	var extra map[string]string
	verifiedOnly := true
	for _, f := range fs {
		text := f.String()
		if f.Check == compose.CheckImage {
			if m, ok := d.moverOf(ctx, c); ok {
				text += fmt.Sprintf(" (whalenurse moved this tag: %s pulled it at %s)", m.Key, m.At.UTC().Format(time.RFC3339))
				extra = map[string]string{"moved_by": m.Key, "moved_at": m.At.UTC().Format(time.RFC3339), "moved_ref": m.Ref}
			}
		}
		findings = append(findings, text)
		verifiedOnly = verifiedOnly && f.Verified()
	}
	if d.cfg.DriftGuard == config.DriftWarn && verifiedOnly {
		d.eng.Warned(t, findings) // warn overrides verified drift only
		return true
	}
	if d.cfg.DriftGuard == config.DriftWarn {
		findings = append(findings, "WHALENURSE_DRIFT_GUARD=warn does not override a check that could not be made")
	}
	d.eng.BlockedWith(t, dec, findings, extra)
	return false
}

// executePull is rung 3 (plan §3.7), carried out in this order and no other:
//
//  1. the pre-action checks, then drift checks 1 and 3 (the guard in pull mode) BEFORE the pull, because after a pull that
//     changed the image compose's dry-run says Recreate, which would block every pull rung;
//  2. `compose pull <svc>`; the image the reference resolves to is read before and after;
//  3. unchanged: the ladder is spent and the container is SUSPENDED (Result.Exhausted), with a message that says so;
//  4. changed: the move is recorded at once (the tag has moved for every stack that shares it, whatever happens next),
//     then `up -d --no-deps --pull never --no-build <svc>`, and a check that the container really is a new one;
//  5. TagMoved (the "upgraded during heal" notification, with the rollback commands), then the dependents step, which
//     afterAction schedules exactly as it does after rung 2.
//
// In dry-run nothing is pulled: the guard runs (it only reads), and the pull goes to compose.DryRun.
func (d *Daemon) executePull(ctx context.Context, t engine.Target, c docker.Container, dec rules.Decision, plan engine.Plan, cfg config.Container) {
	if why := d.recreateBlocker(c); why != "" {
		d.refuse(t, dec, "cannot pull: "+why)
		return
	}
	p, err := compose.ProjectFromLabels(c.Labels)
	if err != nil {
		d.refuse(t, dec, "cannot pull: "+err.Error())
		return
	}
	if c.Image == "" {
		d.refuse(t, dec, "cannot pull: the container has no image reference")
		return
	}
	if why := d.refsRefusal(ctx, c); why != "" {
		d.refuse(t, dec, why)
		return
	}
	if d.siblingBusy(ctx, c, t.Key) {
		d.schedule(t.Key, d.clk.Now().Add(depRetry))
		return
	}
	if !d.guardVerdict(ctx, t, c, dec, p, true) {
		return
	}

	arch := d.cs().server.Arch
	ref := c.Image
	old, oldErr := d.rd.ImageID(ctx, ref, arch)
	if oldErr != nil && !errors.Is(oldErr, docker.ErrImageNotFound) {
		d.refuse(t, dec, fmt.Sprintf("cannot pull: the image %s cannot be read first (%v), so a changed image could not be told from an unchanged one", ref, oldErr))
		return
	}
	oldPlain, _ := d.rd.ImageID(ctx, ref, "")

	d.eng.Begin(t, plan, dec)
	pctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	pullErr := d.o.ComposeActor.Pull(pctx, p)
	if d.cfg.DryRun {
		recheck := d.eng.End(t, plan, engine.Result{DryRun: true}, cfg)
		d.afterAction(t, c, recheck)
		return
	}
	if pullErr != nil {
		recheck := d.eng.End(t, plan, engine.Result{Err: fmt.Errorf("compose pull: %w", pullErr)}, cfg)
		d.afterAction(t, c, recheck)
		return
	}
	now, nowErr := d.rd.ImageID(ctx, ref, arch)
	if nowErr != nil {
		recheck := d.eng.End(t, plan, engine.Result{Err: fmt.Errorf("the pull ran, but the image %s cannot be read afterwards: %w", ref, nowErr)}, cfg)
		d.afterAction(t, c, recheck)
		return
	}
	if old != "" && now == old {
		d.eng.End(t, plan, engine.Result{
			Exhausted: fmt.Sprintf("the pull found no newer image for %s (still %s)", ref, shortID(old)),
			Attrs:     map[string]string{"outcome": "unchanged", "ref": ref, "image": old},
		}, cfg)
		return
	}

	// The image changed. Record it first: the tag moved for every stack that shares it, whether or not the up works.
	nowPlain, _ := d.rd.ImageID(ctx, ref, "")
	at := d.clk.Now()
	d.recordMove(tagMove{Ref: normalizeRef(ref), Key: t.Key, At: at, Old: old, New: now, OldPlain: oldPlain, NewPlain: nowPlain})

	actx, acancel := context.WithTimeout(ctx, composeTimeout)
	defer acancel()
	upErr := d.o.ComposeActor.Up(actx, p)
	if upErr == nil {
		upErr = d.postCheckUpgraded(actx, t, c, p)
	}

	attrs := map[string]string{"ref": ref, "old": old, "new": now, "at": at.UTC().Format(time.RFC3339), "ok": fmt.Sprint(upErr == nil)}
	if back := firstNonEmpty(oldPlain, old); back != "" {
		attrs["old_image"] = back
		attrs["rollback_tag"], attrs["rollback_up"] = compose.RollbackCommands(p, ref, back)
	}
	outcome, msg := "upgraded", fmt.Sprintf("%s: %s moved to %s; recreated", ref, shortID(old), shortID(now))
	if old == "" {
		msg = fmt.Sprintf("%s: pulled %s (no local copy before); recreated", ref, shortID(now))
	}
	if upErr != nil {
		outcome, msg = "upgrade-failed", strings.Replace(msg, "recreated", "the recreate failed: "+upErr.Error(), 1)
		attrs["error"] = upErr.Error()
	}
	d.eng.Note(t, engine.TagMoved, msg, attrs)
	recheck := d.eng.End(t, plan, engine.Result{Err: upErr, Attrs: map[string]string{"outcome": outcome, "ref": ref, "old": old, "new": now}}, cfg)
	d.afterAction(t, c, recheck)
}

// postCheckUpgraded looks at the container after the non-forced up: it must be a new container (compose recreates on a
// changed image by itself, E5), and it must run. If compose left the old one in place, it is recreated with
// --force-recreate; "upgraded" is only announced for a container that was really replaced.
func (d *Daemon) postCheckUpgraded(ctx context.Context, t engine.Target, old docker.Container, p compose.Project) error {
	cur, err := d.currentOf(ctx, t.Key)
	if err != nil {
		return err
	}
	if cur.ID == old.ID {
		d.eng.Note(t, engine.UpgradePostCheck, "the container is still the old one after a non-forced up: recreating it with --force-recreate",
			map[string]string{"id": docker.ShortID(old.ID)})
		if err := d.o.ComposeActor.Recreate(ctx, p); err != nil {
			return err
		}
		if cur, err = d.currentOf(ctx, t.Key); err != nil {
			return err
		}
		if cur.ID == old.ID {
			return errors.New("the container was not replaced even by --force-recreate")
		}
	}
	if !cur.Running {
		return fmt.Errorf("the new container %s is %s, not running", docker.ShortID(cur.ID), cur.Status)
	}
	return nil
}

// currentOf inspects the container that now holds the key.
func (d *Daemon) currentOf(ctx context.Context, key string) (docker.Container, error) {
	if err := d.refresh(ctx); err != nil {
		return docker.Container{}, fmt.Errorf("cannot list containers after the up: %w", err)
	}
	s, ok := d.idx.byKey[key]
	if !ok {
		return docker.Container{}, errors.New("no container for the service after the up")
	}
	return d.rd.Inspect(ctx, s.ID)
}

func shortID(id string) string { return docker.ShortID(id) }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
