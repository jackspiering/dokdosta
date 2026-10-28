package engine

import (
	"testing"
	"time"
)

// Subscribers that say something once per incident (internal/notify) key on the incident attribute, because events
// carry no incident id of their own and the engine's incident survives a restart of dokdosta.
func TestEventsOfAnIncidentCarryItsIdentity(t *testing.T) {
	e, clk, rec := newEngine(t, 0)
	opened := clk.Now()
	run(e, bad, cfg, hcObs) // opens the incident, restarts, then the ladder ends in SUSPENDED
	clk.Advance(time.Minute)
	for i := 0; i < 3; i++ {
		if p := e.Evaluate(tgt, bad, cfg, hcObs); acted(p) {
			e.Begin(tgt, p, bad)
			e.End(tgt, p, Result{}, cfg)
		}
		clk.Advance(5 * time.Minute)
	}
	want := opened.UTC().Format(time.RFC3339Nano)
	var seen int
	for _, ev := range rec.Events() {
		if ev.Type == IncidentOpened || ev.Type == ActionStarted || ev.Type == ActionResult || ev.Type == Suspended {
			seen++
			if ev.Attrs[AttrIncident] != want {
				t.Errorf("%s: incident attr = %q, want %q", ev.Type, ev.Attrs[AttrIncident], want)
			}
		}
	}
	if seen < 4 || rec.Count(Suspended) != 1 {
		t.Fatalf("expected the whole ladder, got %v", rec.Types())
	}
}

func TestEventsOutsideAnIncidentHaveNoIncidentAttr(t *testing.T) {
	e, _, rec := newEngine(t, 0)
	e.Note(tgt, RungsDisabled, "x", nil)
	e.Note(tgt, NeedsRecreate, "y", map[string]string{"class": "c"})
	for _, ev := range rec.Events() {
		if _, ok := ev.Attrs[AttrIncident]; ok {
			t.Errorf("%s has an incident attr with no incident open", ev.Type)
		}
	}
}
