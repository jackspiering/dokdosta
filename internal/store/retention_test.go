package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// The events of an incident that is still open survive the age prune, so a long SUSPENDED keeps its cause, its drift
// findings, its mover and its rollback (plan v3.13 item 21). Events of a closed incident, and events without one, go.
func TestRetentionKeepsTheEventsOfAnOpenIncident(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	k := sampleKey() // h/p/web/1, an open incident opened at t0
	s.SaveKey(k)
	opened := t0.Format(time.RFC3339Nano)
	other := t0.Add(-time.Hour).Format(time.RFC3339Nano)
	events := []engine.Event{
		{ID: 1, Time: t0, Key: k.Key, Type: engine.IncidentOpened, Attrs: map[string]string{engine.AttrIncident: opened}},
		{ID: 2, Time: t0.Add(time.Minute), Key: k.Key, Type: engine.DriftBlocked, Attrs: map[string]string{engine.AttrIncident: opened, "findings": "config"}},
		{ID: 3, Time: t0, Key: k.Key, Type: engine.ActionResult, Attrs: map[string]string{engine.AttrIncident: other}},         // an earlier, closed incident
		{ID: 4, Time: t0, Key: "h/p/db/1", Type: engine.IncidentOpened, Attrs: map[string]string{engine.AttrIncident: opened}}, // same time, another key
		{ID: 5, Time: t0, Key: k.Key, Type: engine.Paused},                                                                     // no incident
		{ID: 6, Time: t0.Add(40 * 24 * time.Hour), Key: "h/p/db/1", Type: engine.Paused},                                       // young
	}
	for _, e := range events {
		s.RecordEvent(e)
	}
	if _, err := s.Prune(t0.Add(40*24*time.Hour), 30*24*time.Hour, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, s); got != "1,2,6" {
		t.Fatalf("kept %s, want 1,2,6", got)
	}
}

// The row cap is the last resort: it deletes events of closed incidents and events without an incident first, oldest
// first, and those of open incidents only when nothing else is left to delete.
func TestTheRowCapDeletesOpenIncidentEventsLast(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	k := sampleKey()
	s.SaveKey(k)
	opened := t0.Format(time.RFC3339Nano)
	for i := uint64(1); i <= 10; i++ {
		e := engine.Event{ID: i, Time: t0, Key: "h/p/db/1", Type: engine.Paused}
		if i <= 3 { // the oldest three belong to the open incident
			e.Key, e.Attrs = k.Key, map[string]string{engine.AttrIncident: opened}
		}
		s.RecordEvent(e)
	}
	if err := s.pruneEventsMax(t0, time.Hour, 5); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, s); got != "1,2,3,9,10" {
		t.Fatalf("cap 5 kept %s, want 1,2,3,9,10", got)
	}
	if err := s.pruneEventsMax(t0, time.Hour, 2); err != nil {
		t.Fatal(err)
	}
	// Everything else goes first (9, 10), then the oldest event of the open incident: the last resort.
	if got := ids(t, s); got != "2,3" {
		t.Fatalf("cap 2 kept %s, want 2,3 (the last resort)", got)
	}
}

func ids(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := ""
	for rows.Next() {
		var id int
		rows.Scan(&id)
		if out != "" {
			out += ","
		}
		out += fmt.Sprint(id)
	}
	return out
}
