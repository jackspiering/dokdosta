package engine

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestBusSeedContinuesAfterTheGivenID(t *testing.T) {
	var b Bus
	b.Seed(41)
	if e := b.Publish(Event{Time: t0}); e.ID != 42 {
		t.Fatalf("ID %d", e.ID)
	}
	b.Seed(5) // never goes back
	if e := b.Publish(Event{Time: t0}); e.ID != 43 {
		t.Fatalf("ID %d", e.ID)
	}
}

func TestRingKeepsTheLastEventsAndAnswersLikeTheStore(t *testing.T) {
	r := NewRing(3)
	for i := uint64(1); i <= 5; i++ {
		key := "h/p/a/1"
		if i%2 == 0 {
			key = "h/p/b/1"
		}
		r.Record(Event{ID: i, Time: t0, Key: key, Type: IncidentOpened})
	}
	if r.LastID() != 5 {
		t.Fatal(r.LastID())
	}
	all, _ := r.Query(EventQuery{})
	if len(all) != 3 || all[0].ID != 3 || all[2].ID != 5 {
		t.Fatalf("a ring of 3 keeps the last three: %+v", all)
	}
	if got, _ := r.Query(EventQuery{Key: "h/p/b/1", Newest: true}); len(got) != 1 || got[0].ID != 4 {
		t.Fatalf("%+v", got)
	}
	if got, _ := r.Query(EventQuery{After: 3, Limit: 1}); len(got) != 1 || got[0].ID != 4 {
		t.Fatalf("%+v", got)
	}
	if got, _ := r.Query(EventQuery{Before: 5, Newest: true, Limit: 5}); len(got) != 2 || got[0].ID != 4 {
		t.Fatalf("%+v", got)
	}
	if got, _ := r.Query(EventQuery{Types: []EventType{Suspended}}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestPauseForPicksTheMostSpecificScopeInEffect(t *testing.T) {
	v := View{Pauses: []Pause{
		{Scope: "p", Until: t0.Add(time.Hour)},
		{Scope: "p/web", Until: t0.Add(2 * time.Hour)},
		{Scope: "p/old", Until: t0.Add(-time.Hour)},
	}}
	if p, ok := v.PauseFor("h/p/web/1", t0); !ok || p.Scope != "p/web" {
		t.Fatalf("%+v %v", p, ok)
	}
	if p, ok := v.PauseFor("h/p/db/1", t0); !ok || p.Scope != "p" {
		t.Fatalf("%+v %v", p, ok)
	}
	if _, ok := v.PauseFor("h/p/old/1", t0.Add(30*time.Minute)); !ok { // covered by "p" still
		t.Fatal("p covers old")
	}
	if _, ok := v.PauseFor("h/q/web/1", t0); ok {
		t.Fatal("another project")
	}
}

func TestExcludeTypesAreNeverReturned(t *testing.T) {
	r := NewRing(10)
	r.Record(Event{ID: 1, Time: t0, Key: "h/p/a/1", Type: EventAttributed})
	r.Record(Event{ID: 2, Time: t0, Key: "h/p/a/1", Type: ActionResult})
	r.Record(Event{ID: 3, Time: t0, Key: "h/p/a/1", Type: EventAttributed})
	got, _ := r.Query(EventQuery{ExcludeTypes: []EventType{EventAttributed}, Newest: true})
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("%+v", got)
	}
	// Types and ExcludeTypes together: the exclusion wins.
	got, _ = r.Query(EventQuery{Types: []EventType{EventAttributed, ActionResult}, ExcludeTypes: []EventType{EventAttributed}})
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("%+v", got)
	}
}
