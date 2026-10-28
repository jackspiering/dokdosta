package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/config"
	"github.com/jackspiering/dokdosta/internal/rules"
)

// M4: rung 3, the opt-in pull. The engine plans it as ActionPull after restart and recreate; the daemon carries it out.

var pullCfg = config.Container{OptedIn: true, Actions: []string{"restart", "recreate", "pull"}, HealExited: config.HealCrashed}

func TestLadderReachesThePullRungAfterRestartAndRecreate(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	if p := run(e, bad, pullCfg, hcRecreate); p.Act != rules.ActionRestart {
		t.Fatalf("rung 1: %+v", p)
	}
	clk.Advance(31 * time.Second)
	if p := run(e, bad, pullCfg, hcRecreate); p.Act != rules.ActionRecreate {
		t.Fatalf("rung 2: %+v", p)
	}
	clk.Advance(61 * time.Second)
	p := run(e, bad, pullCfg, hcRecreate)
	if p.Act != rules.ActionPull || p.Rung != config.RungPull {
		t.Fatalf("rung 3 is the pull: %+v", p)
	}
	// The cooldown doubles again: 30s, 60s, 120s.
	if want := clk.Now().Add(120 * time.Second); !p.RecheckAt.Equal(want) {
		t.Fatalf("cooldown after rung 3: %v want %v", p.RecheckAt, want)
	}
	clk.Advance(121 * time.Second)
	if p := e.Evaluate(tgt, bad, pullCfg, hcRecreate); acted(p) {
		t.Fatalf("one pass per incident: %+v", p)
	}
	if rec.Count(Suspended) != 1 || rec.Count(ActionStarted) != 3 || rec.Count(RungUnavailable) != 0 {
		t.Fatalf("%v", rec.Types())
	}
}

// Without the label the ladder has no third rung: it ends after the recreate, exactly as before M4.
func TestNoPullRungWithoutTheLabel(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	run(e, bad, cfg, hcRecreate)
	clk.Advance(31 * time.Second)
	run(e, bad, cfg, hcRecreate)
	clk.Advance(61 * time.Second)
	if p := e.Evaluate(tgt, bad, cfg, hcRecreate); acted(p) {
		t.Fatalf("%+v", p)
	}
	for _, ev := range rec.Events() {
		if ev.Attrs["action"] == "pull" {
			t.Fatalf("a pull without the label: %+v", ev)
		}
	}
}

// The version gate disables rungs 2 and 3 together: the daemon marks both unavailable, and the ladder ends after the restart.
func TestPullRungIsSkippedWhenUnavailable(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	off := Obs{HasHealthcheck: true, Unavailable: map[string]string{config.RungRecreate: "gate", config.RungPull: "gate"}}
	if p := run(e, bad, pullCfg, off); p.Act != rules.ActionRestart {
		t.Fatalf("%+v", p)
	}
	clk.Advance(31 * time.Second)
	if p := e.Evaluate(tgt, bad, pullCfg, off); acted(p) {
		t.Fatalf("%+v", p)
	}
	if rec.Count(RungUnavailable) != 2 || rec.Count(Suspended) != 1 || rec.Count(ActionStarted) != 1 {
		t.Fatalf("%v", rec.Types())
	}
}

func TestPullRungIsRateLimited(t *testing.T) {
	e, clk, rec := newEngine(t, 2) // the restart and the recreate used both slots
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(31 * time.Second)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(61 * time.Second)
	p := e.Evaluate(tgt, bad, pullCfg, hcRecreate)
	if acted(p) || p.RecheckAt.IsZero() || rec.Count(RateLimited) != 1 {
		t.Fatalf("%+v %v", p, rec.Types())
	}
}

// "Digest unchanged: notify and SUSPENDED": the ladder is spent at once, with no cooldown to wait out, and the cause says
// that there was no newer image.
func TestUnchangedPullSuspendsAtOnce(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(31 * time.Second)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(61 * time.Second)
	p := e.Evaluate(tgt, bad, pullCfg, hcRecreate)
	e.Begin(tgt, p, bad)
	recheck := e.End(tgt, p, Result{
		Exhausted: "the pull found no newer image for web:1 (still aaaaaaaaaaaa)",
		Attrs:     map[string]string{"outcome": "unchanged"},
	}, pullCfg)
	if !recheck.IsZero() {
		t.Fatalf("nothing to look at again: %v", recheck)
	}
	var last, res Event
	for _, ev := range rec.Events() {
		switch ev.Type {
		case Suspended:
			last = ev
		case ActionResult:
			res = ev
		}
	}
	if !strings.Contains(last.Message, "no newer image for web:1") || last.Attrs["reason"] != ReasonLadder {
		t.Fatalf("%+v", last)
	}
	if res.Attrs["outcome"] != "unchanged" || res.Attrs["action"] != "pull" || res.Attrs["ok"] != "true" {
		t.Fatalf("%+v", res)
	}
	if !e.Suspended(tgt.Key) {
		t.Fatal("not SUSPENDED")
	}
	// And it stays: one pass per incident.
	clk.Advance(10 * time.Minute)
	if p := e.Evaluate(tgt, bad, pullCfg, hcRecreate); acted(p) || rec.Count(ActionStarted) != 3 {
		t.Fatalf("%+v %v", p, rec.Types())
	}
}

// A pull that failed (the registry is down) is an ordinary failed action: the cooldown applies and Exhausted is ignored.
func TestFailedPullIsNotTheEndOfTheLadderByItself(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(31 * time.Second)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(61 * time.Second)
	p := e.Evaluate(tgt, bad, pullCfg, hcRecreate)
	e.Begin(tgt, p, bad)
	recheck := e.End(tgt, p, Result{Err: errors.New("pull failed"), Exhausted: "x"}, pullCfg)
	if recheck.IsZero() || e.Suspended(tgt.Key) || rec.Count(Suspended) != 0 {
		t.Fatalf("%v %v", recheck, rec.Types())
	}
}

// A dry run records that it would pull and nothing else: no suspension on a result that was never obtained.
func TestDryRunPullDoesNotSuspend(t *testing.T) {
	e, clk, rec := newEngineWith(t, nil, func(p *Params) { p.DryRun = true })
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(31 * time.Second)
	run(e, bad, pullCfg, hcRecreate)
	clk.Advance(61 * time.Second)
	p := e.Evaluate(tgt, bad, pullCfg, hcRecreate)
	e.Begin(tgt, p, bad)
	e.End(tgt, p, Result{DryRun: true, Exhausted: "x"}, pullCfg)
	if rec.Count(Suspended) != 0 {
		t.Fatalf("%v", rec.Types())
	}
}

func TestBlockedWithCarriesTheMoverOnTheEvent(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	run(e, bad, cfg, hcRecreate)
	clk.Advance(31 * time.Second)
	p := e.Evaluate(tgt, bad, cfg, hcRecreate)
	if p.Act != rules.ActionRecreate {
		t.Fatalf("%+v", p)
	}
	e.BlockedWith(tgt, bad, []string{"image: the tag moved"}, map[string]string{"moved_by": "h/other/web/1", "moved_at": "2026-10-19T10:00:00Z"})
	for _, ev := range rec.Events() {
		if ev.Type == DriftBlocked {
			if ev.Attrs["moved_by"] != "h/other/web/1" || ev.Attrs["findings"] != "image: the tag moved" {
				t.Fatalf("%+v", ev)
			}
			return
		}
	}
	t.Fatal("no DriftBlocked")
}
