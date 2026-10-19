package engine

import "time"

// KeyDetail is everything the engine knows about one state key, for the read-only API and the dashboard. Unlike
// Status, which lists only keys worth a line in `whalenurse status`, View includes every key the engine holds.
type KeyDetail struct {
	Name          string
	State         string // ok, incident, recovering, SUSPENDED, PAUSED
	Class         string
	Opened        time.Time // when the open incident began
	Rung          int       // index of the next rung to run
	CooldownUntil time.Time
	Spent         bool   // nothing left to try
	SpentReason   string // ladder, flapping or drift
	SuspendedAt   time.Time
	Recovering    bool
	RecoverySince time.Time
	PausedScope   string // the pause that covers the key
	PausedSince   time.Time
	PausedUntil   time.Time // zero: until resumed
	Flaps         int       // incidents opened within FlapWindow
	ManualStop    bool
	LoopIncreases int
}

// View is a consistent snapshot of the engine for the API.
type View struct {
	Keys         map[string]KeyDetail
	Pauses       []Pause
	ActionsLastH int
	RateLimit    int
	NextSlot     time.Time // when an action may run again; zero while the budget is not spent
	RestoreUntil time.Time
	FlapLimit    int
	FlapWindow   time.Duration
}

// View snapshots every key and the global budget under one lock.
func (e *Engine) View() View {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.clk.Now()
	v := View{Keys: make(map[string]KeyDetail, len(e.keys)), Pauses: []Pause{}, RateLimit: e.p.RateLimit, FlapLimit: e.p.FlapLimit, FlapWindow: e.p.FlapWindow}
	if now.Before(e.restoreUntil) {
		v.RestoreUntil = e.restoreUntil.UTC()
	}
	if !e.rateOK(now) {
		v.NextSlot = e.nextSlot(now).UTC()
	}
	v.ActionsLastH = len(e.spent)
	for _, p := range e.pauses {
		v.Pauses = append(v.Pauses, Pause{Scope: p.scope, Until: p.until.UTC(), Created: p.created.UTC()})
	}
	cut := now.Add(-e.p.FlapWindow)
	for k, st := range e.keys {
		d := KeyDetail{Name: st.name, State: "ok", ManualStop: st.tracker.Evidence().ManualStop, LoopIncreases: st.loop.N}
		for _, f := range st.flaps {
			if f.After(cut) {
				d.Flaps++
			}
		}
		if inc := st.incident; inc != nil {
			d.State, d.Class, d.Opened, d.Rung = "incident", string(inc.class), inc.opened.UTC(), inc.rung
			d.CooldownUntil, d.Spent, d.SpentReason = inc.cooldownUntil.UTC(), inc.spent, inc.spentReason
			switch {
			case inc.suspended:
				d.State, d.SuspendedAt = "SUSPENDED", inc.suspendedAt.UTC()
			case inc.recovering:
				d.State, d.Recovering, d.RecoverySince = "recovering", true, inc.recoverySince.UTC()
			}
		}
		if p := e.pausedLocked(k, now); p != nil {
			d.PausedScope, d.PausedSince, d.PausedUntil = p.scope, p.created.UTC(), p.until.UTC()
			if d.State == "ok" || d.State == "incident" {
				d.State = "PAUSED"
			}
		}
		v.Keys[k] = d
	}
	return v
}

// ScopeMatches reports whether a state key (host/project/service/number, or host/name) is inside a pause scope.
func ScopeMatches(scope, key string) bool { return scopeMatches(scope, key) }

// PauseFor returns the pause that covers the key (the most specific scope), if any is in effect in the view.
func (v View) PauseFor(key string, now time.Time) (Pause, bool) {
	var hit Pause
	found := false
	for _, p := range v.Pauses {
		if !p.Until.IsZero() && !now.Before(p.Until) {
			continue
		}
		if scopeMatches(p.Scope, key) && (!found || len(p.Scope) > len(hit.Scope)) {
			hit, found = p, true
		}
	}
	return hit, found
}
