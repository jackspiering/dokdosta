package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/rules"
)

// M2b: rung 2 is planned by the engine and carried out (after the drift guard) by the daemon.

func TestLadderRestartThenRecreateThenSuspended(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	p := run(e, bad, cfg, hcRecreate)
	if p.Act != rules.ActionRestart || p.Rung != config.RungRestart {
		t.Fatalf("rung 1: %+v", p)
	}
	clk.Advance(31 * time.Second)
	p = run(e, bad, cfg, hcRecreate)
	if p.Act != rules.ActionRecreate || p.Rung != config.RungRecreate {
		t.Fatalf("rung 2 is a forced recreate: %+v", p)
	}
	// The cooldown doubled for the second rung.
	if want := clk.Now().Add(60 * time.Second); !p.RecheckAt.Equal(want) {
		t.Fatalf("cooldown after rung 2: %v want %v", p.RecheckAt, want)
	}
	clk.Advance(61 * time.Second)
	if p := e.Evaluate(tgt, bad, cfg, hcRecreate); acted(p) {
		t.Fatalf("one pass per incident: %+v", p)
	}
	if rec.Count(Suspended) != 1 || rec.Count(ActionStarted) != 2 || rec.Count(RungUnavailable) != 0 {
		t.Fatalf("%v", rec.Types())
	}
	for _, ev := range rec.Events() {
		if ev.Type == Suspended && ev.Attrs["reason"] != ReasonLadder {
			t.Fatalf("%+v", ev)
		}
	}
}

func TestRecreateRungIsRateLimited(t *testing.T) {
	e, clk, rec := newEngine(t, 1) // one action an hour: the restart used it
	run(e, bad, cfg, hcRecreate)
	clk.Advance(31 * time.Second)
	p := e.Evaluate(tgt, bad, cfg, hcRecreate)
	if acted(p) || p.RecheckAt.IsZero() || rec.Count(RateLimited) != 1 {
		t.Fatalf("%+v %v", p, rec.Types())
	}
}

func TestDriftBlockedSuspendsAndSpendsNoSlot(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	run(e, bad, cfg, hcRecreate)
	clk.Advance(31 * time.Second)
	p := e.Evaluate(tgt, bad, cfg, hcRecreate)
	if p.Act != rules.ActionRecreate {
		t.Fatalf("%+v", p)
	}
	// The daemon's drift guard says no, instead of Begin.
	if !e.Blocked(tgt, bad, []string{"config: compose would recreate this container", "image: the tag moved"}) {
		t.Fatal("Blocked should apply to an open incident")
	}
	var blocked, suspended Event
	for _, ev := range rec.Events() {
		switch ev.Type {
		case DriftBlocked:
			blocked = ev
		case Suspended:
			suspended = ev
		}
	}
	if !strings.Contains(blocked.Message, "compose would recreate") || !strings.Contains(blocked.Attrs["findings"], "the tag moved") {
		t.Fatalf("the cause must be in the event: %+v", blocked)
	}
	if suspended.Attrs["reason"] != ReasonDrift || !strings.Contains(suspended.Message, "drift guard") {
		t.Fatalf("%+v", suspended)
	}
	if rec.Count(ActionStarted) != 1 {
		t.Fatalf("a blocked rung is not an action: %v", rec.Types())
	}
	if len(e.spent) != 1 {
		t.Fatalf("no rate-limit slot for the blocked rung: %d", len(e.spent))
	}
	// SUSPENDED: nothing more is tried, and it is not announced again.
	clk.Advance(time.Hour)
	if p := e.Evaluate(tgt, bad, cfg, hcRecreate); acted(p) || rec.Count(Suspended) != 1 {
		t.Fatalf("%+v %v", p, rec.Types())
	}
	if e.Blocked(tgt, bad, []string{"x"}) {
		t.Fatal("an already suspended incident is not blocked twice")
	}
	// unsuspend is a fresh pass: the guard gets another say.
	if _, err := e.Unsuspend("p"); err != nil {
		t.Fatal(err)
	}
	if p := e.Evaluate(tgt, bad, cfg, hcRecreate); p.Act != rules.ActionRestart {
		t.Fatalf("%+v", p)
	}
}

func TestDriftWarnedOnlyReports(t *testing.T) {
	e, _, rec := newEngine(t, 0)
	e.Warned(tgt, []string{"image: the tag moved"})
	if rec.Count(DriftWarned) != 1 || rec.Count(Suspended) != 0 {
		t.Fatalf("%v", rec.Types())
	}
}

// A dependent whose parent was recreated is never restarted (E1): its ladder starts at rung 2.
func TestRecreateStaleSkipsTheRestartRung(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	stale := rules.Decision{Class: rules.ClassStaleParentGone, Action: rules.ActionRecreateStale, Reason: "parent gone"}
	p := run(e, stale, cfg, noHCRecreate)
	if p.Act != rules.ActionRecreateStale || p.Rung != config.RungRecreate {
		t.Fatalf("a dependent is recreated with the non-forced up, never restarted: %+v", p)
	}
	for _, ev := range rec.Events() {
		if ev.Type == ActionStarted && ev.Attrs["action"] == string(rules.ActionRestart) {
			t.Fatal("restart was planned for a dependent whose parent is gone")
		}
	}
	// It did not help: SUSPENDED, still without a restart.
	clk.Advance(2 * time.Minute)
	if p := e.Evaluate(tgt, stale, cfg, noHCRecreate); acted(p) || rec.Count(Suspended) != 1 {
		t.Fatalf("%+v %v", p, rec.Types())
	}

	// With recreate unavailable (the gate) nothing can be done for it: the ladder ends, nothing was restarted.
	e2, _, rec2 := newEngine(t, 0)
	if p := e2.Evaluate(tgt, stale, cfg, noHC); acted(p) {
		t.Fatalf("%+v", p)
	}
	if rec2.Count(RungUnavailable) != 1 || rec2.Count(Suspended) != 1 || rec2.Count(ActionStarted) != 0 {
		t.Fatalf("%v", rec2.Types())
	}
	// And with whalenurse.actions=restart the same.
	c := cfg
	c.Actions = []string{config.RungRestart}
	e3, _, rec3 := newEngine(t, 0)
	if p := e3.Evaluate(tgt, stale, c, noHCRecreate); acted(p) || rec3.Count(Suspended) != 1 {
		t.Fatalf("%+v %v", p, rec3.Types())
	}
}

func TestRecreateOnlyLadderForANetworkThatIsGone(t *testing.T) {
	e, _, _ := newEngine(t, 0)
	d := rules.Decision{Class: rules.ClassStartFailed, Action: rules.ActionRecreate, Reason: "network gone"}
	if p := e.Evaluate(tgt, d, cfg, noHCRecreate); p.Act != rules.ActionRecreate {
		t.Fatalf("%+v", p)
	}
}

func TestAdvisoryCarriesTheDependents(t *testing.T) {
	e, _, rec := newEngine(t, 0)
	d := rules.Decision{Class: rules.ClassExitedHostStopped, Action: rules.ActionNotify, Reason: "parent stays down", Dependents: []string{"p-a-1", "p-b-1"}}
	for i := 0; i < 4; i++ {
		e.Evaluate(tgt, d, cfg, noHC)
	}
	if rec.Count(NotifyOnly) != 1 {
		t.Fatalf("one notification, however many polls: %v", rec.Types())
	}
	for _, ev := range rec.Events() {
		if ev.Type == NotifyOnly && ev.Attrs["dependents"] != "p-a-1,p-b-1" {
			t.Fatalf("%+v", ev)
		}
	}
}

func TestNoteReachesTheStream(t *testing.T) {
	e, _, rec := newEngine(t, 0)
	e.Note(Target{}, RungsDisabled, "rungs 2 and 3 are disabled: compose too old", map[string]string{"rungs": "recreate,pull"})
	if rec.Count(RungsDisabled) != 1 {
		t.Fatal(rec.Types())
	}
}

// A dependent repaired by the dependents step belongs to its parent's incident: it does not count toward its own flap
// limit (plan v3.6). Its own failures do.
func TestDependentStepDoesNotCountTowardTheFlapLimit(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	stale := rules.Decision{Class: rules.ClassStaleParentRestarted, Action: rules.ActionRestart, Reason: "parent restarted"}
	for i := 0; i < 6; i++ { // FlapLimit is 3
		if p := run(e, stale, cfg, noHCRecreate); p.Act != rules.ActionRestart {
			t.Fatalf("round %d: %+v", i, p)
		}
		clk.Advance(31 * time.Second)
		e.Evaluate(tgt, ok, cfg, noHCRecreate) // recovering
		clk.Advance(11 * time.Minute)
		e.Evaluate(tgt, ok, cfg, noHCRecreate) // recovered: the incident closes
	}
	if rec.Count(Suspended) != 0 || rec.Count(Recovered) != 6 {
		t.Fatalf("%v", rec.Types())
	}
	for _, k := range e.Status().Keys {
		if k.Key == tgt.Key && k.Flaps != 0 {
			t.Fatalf("flaps = %d", k.Flaps)
		}
	}
	gone := rules.Decision{Class: rules.ClassStaleParentGone, Action: rules.ActionRecreateStale, Reason: "gone"}
	for i := 0; i < 6; i++ {
		run(e, gone, cfg, noHCRecreate)
		clk.Advance(31 * time.Second)
		e.Evaluate(tgt, ok, cfg, noHCRecreate)
		clk.Advance(11 * time.Minute)
		e.Evaluate(tgt, ok, cfg, noHCRecreate)
	}
	if rec.Count(Suspended) != 0 {
		t.Fatalf("%v", rec.Types())
	}
	// Its own failures still flap.
	for i := 0; i < 4; i++ {
		run(e, bad, cfg, noHCRecreate)
		clk.Advance(31 * time.Second)
		e.Evaluate(tgt, ok, cfg, noHCRecreate)
		clk.Advance(11 * time.Minute)
		e.Evaluate(tgt, ok, cfg, noHCRecreate)
	}
	if rec.Count(Suspended) != 1 {
		t.Fatalf("its own incidents still count: %v", rec.Types())
	}
}

// A dependent that waits for a service says so once per reason; once the service is ready (ClearWait) or the dependent
// acts, the same reason is news again.
func TestWaitIsSaidOncePerReasonUntilTheServiceIsReady(t *testing.T) {
	e, _, rec := newEngine(t, 0)
	d := rules.Decision{Class: rules.ClassStaleParentGone, Action: rules.ActionRecreateStale}
	e.Wait(tgt, d, "waiting for service db (exited)")
	e.Wait(tgt, d, "waiting for service db (exited)")
	if rec.Count(WaitingForParent) != 1 || rec.Count(ActionRefused) != 0 {
		t.Fatalf("%v", rec.Types())
	}
	e.Wait(tgt, d, "waiting for service db (created)")
	if rec.Count(WaitingForParent) != 2 {
		t.Fatalf("another reason is news: %v", rec.Types())
	}
	e.ClearWait(tgt)
	e.Wait(tgt, d, "waiting for service db (created)")
	if rec.Count(WaitingForParent) != 3 {
		t.Fatalf("after the service was ready the same wait is news again: %v", rec.Types())
	}
}
