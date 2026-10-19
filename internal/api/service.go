package api

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// Source is what the daemon provides: its own view of the host, already in the shape of the documents. The Service
// adds what only the event log knows (the timeline, the drift verdict, the last upgrade) and applies the output rules.
type Source interface {
	Status() StatusDoc
	// Containers lists every container the daemon saw in its last list, merged with the engine's state.
	Containers() []ContainerDoc
}

// History lists closed incidents, newest first. The store implements it; a dry run has none.
type History interface {
	History(key string) ([]engine.ClosedIncident, error)
}

// Service builds the documents of the API. The JSON handlers encode them and the dashboard (internal/web) renders
// them, so the pages show exactly what the API says. Everything that leaves whalenurse passes through it: that is where
// the output rules of plan §3.9 are enforced (label allow-list, attribute allow-list, redaction, length caps).
type Service struct {
	Host    string
	Source  Source
	Events  engine.EventLog
	History History             // nil: no closed incidents
	Redact  func(string) string // masks the notification URLs and the HTTP token; nil is the identity
	Hub     *Hub
	Metrics bool // serve /metrics
}

// Limits on what an event or a container may carry out.
const (
	maxText      = 2000 // runes of a message or an attribute value
	maxErrorText = 600  // runes of State.Error
)

// allowedAttrs are the event attributes the engine and the daemon emit. Anything else is dropped at the boundary, so a
// future attribute that carried something it should not stays inside until someone adds it here on purpose.
var allowedAttrs = map[string]bool{
	"action": true, "at": true, "class": true, "cause": true, "count": true, "dependents": true, "dry_run": true,
	"error": true, "findings": true, "held": true, "id": true, "image": true, "incident": true, "moved_at": true,
	"moved_by": true, "moved_ref": true, "new": true, "not_applicable": true, "ok": true, "old": true, "old_id": true,
	"old_image": true, "opened": true, "outcome": true, "reason": true, "ref": true, "rollback_tag": true,
	"rollback_up": true, "rung": true, "rungs": true, "scope": true, "sidecars": true, "until": true, "why": true,
}

// AttrAllowed reports whether an event attribute key may leave whalenurse. The daemon's tests subscribe a checker that
// fails on any key the healer emits outside this list, so every scenario of those tests proves the list is complete.
func AttrAllowed(key string) bool { return allowedAttrs[key] }

// labelAllowed is the rule for labels: com.docker.compose.* and whalenurse.*, nothing else (plan §3.9).
func labelAllowed(k string) bool {
	return strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "whalenurse.")
}

func (s *Service) redact(v string) string {
	if s.Redact != nil {
		v = s.Redact(v)
	}
	return v
}

func (s *Service) text(v string, max int) string {
	v = s.redact(v)
	if utf8.RuneCountInString(v) > max {
		r := []rune(v)
		v = string(r[:max-1]) + "…"
	}
	return v
}

// EventDoc converts and cleans an engine event.
func (s *Service) EventDoc(e engine.Event) EventDoc {
	d := EventDoc{ID: e.ID, Time: e.Time.UTC(), Host: e.Host, Key: e.Key, Container: e.Container, Type: string(e.Type), Message: s.text(e.Message, maxText)}
	if d.Host == "" {
		d.Host = s.Host
	}
	for k, v := range e.Attrs {
		if !allowedAttrs[k] {
			continue
		}
		if d.Attrs == nil {
			d.Attrs = map[string]string{}
		}
		d.Attrs[k] = s.text(v, maxText)
	}
	return d
}

func (s *Service) eventDocs(evs []engine.Event) []EventDoc {
	out := make([]EventDoc, 0, len(evs))
	for _, e := range evs {
		out = append(out, s.EventDoc(e))
	}
	return out
}

// clean applies the output rules to a container document the Source built.
func (s *Service) clean(c ContainerDoc) ContainerDoc {
	c.StartError = s.text(c.StartError, maxErrorText)
	if c.Host == "" {
		c.Host = s.Host
	}
	if len(c.Labels) > 0 {
		kept := make(map[string]string, len(c.Labels))
		for k, v := range c.Labels {
			if labelAllowed(k) {
				kept[k] = s.text(v, maxText)
			}
		}
		c.Labels = kept
	}
	return c
}

// Status is the status document with the event cursor filled in.
func (s *Service) Status() StatusDoc {
	d := s.Source.Status()
	d.Host, d.API = s.Host, APIVersion
	if s.Events != nil {
		d.LastEventID = s.Events.LastID()
	}
	if d.Pauses == nil {
		d.Pauses = []PauseDoc{}
	}
	if d.VersionGate.DisabledRungs == nil {
		d.VersionGate.DisabledRungs = []string{}
	}
	return d
}

// Containers lists the watched containers, or every container with all set, sorted by key.
func (s *Service) Containers(all bool) []ContainerDoc {
	out := []ContainerDoc{}
	for _, c := range s.Source.Containers() {
		if all || c.Watched {
			out = append(out, s.clean(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

const timelineLimit = 100

// Container returns the detail document of a key.
func (s *Service) Container(key string) (ContainerDetailDoc, bool) {
	var d ContainerDetailDoc
	found := false
	for _, c := range s.Source.Containers() {
		if c.Key == key {
			d.ContainerDoc, found = s.clean(c), true
			break
		}
	}
	if !found {
		return d, false
	}
	d.Timeline, d.Incidents = []EventDoc{}, []IncidentDoc{}
	if s.Events != nil {
		if evs, err := s.Events.Query(engine.EventQuery{Key: key, Newest: true, Limit: timelineLimit}); err == nil {
			d.Timeline = s.eventDocs(evs)
		}
	}
	d.Drift = s.drift(key, d.Healer)
	d.LastUpgrade = s.lastUpgrade(key)
	if sp := d.Healer.Suspended; sp != nil {
		sp.Cause = s.lastOf(key, engine.Suspended).Message
	}
	d.Incidents = s.incidents(key, 20)
	return d, true
}

// lastOf returns the newest event of the type for the key; the zero document for none.
func (s *Service) lastOf(key string, types ...engine.EventType) EventDoc {
	if s.Events == nil {
		return EventDoc{}
	}
	evs, err := s.Events.Query(engine.EventQuery{Key: key, Types: types, Newest: true, Limit: 1})
	if err != nil || len(evs) == 0 {
		return EventDoc{}
	}
	return s.EventDoc(evs[0])
}

const driftNote = "The drift guard runs right before a recreate, not on a timer: this is what it found the last time."

func (s *Service) drift(key string, h HealerDoc) DriftDoc {
	d := DriftDoc{Status: "none-recorded", Note: driftNote}
	last := s.lastOf(key, engine.DriftBlocked, engine.DriftWarned)
	if last.ID != 0 {
		d.At, d.Findings = last.Time, last.Attrs["findings"]
		d.MovedBy = last.Attrs["moved_by"]
		if t, err := time.Parse(time.RFC3339, last.Attrs["moved_at"]); err == nil {
			d.MovedAt = t.UTC()
		}
		if last.Type == string(engine.DriftWarned) {
			d.Status = "warned"
		} else {
			d.Status = "was-blocked"
		}
	}
	if h.Suspended != nil && h.Suspended.Reason == engine.ReasonDrift {
		d.Status, d.Blocking = "blocked", true
	}
	return d
}

func (s *Service) lastUpgrade(key string) *UpgradeDoc {
	e := s.lastOf(key, engine.TagMoved, engine.Upgraded)
	if e.ID == 0 {
		return nil
	}
	a := e.Attrs
	u := &UpgradeDoc{
		EventID: e.ID, Kind: e.Type, At: e.Time, Ref: a["ref"], OldImage: firstNonEmpty(a["old_image"], a["old"]), NewImage: a["new"],
		OK: a["ok"] != "false", Error: a["error"], RollbackTag: a["rollback_tag"], RollbackUp: a["rollback_up"], Message: e.Message,
	}
	if a["sidecars"] != "" {
		u.Sidecars = strings.Split(a["sidecars"], ",")
	}
	return u
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// incidents lists the open incident of the key (or of every key when key is empty) and then the closed ones.
func (s *Service) incidents(key string, limit int) []IncidentDoc {
	out := []IncidentDoc{}
	for _, c := range s.Source.Containers() {
		if (key != "" && c.Key != key) || c.Healer.IncidentOpened.IsZero() {
			continue
		}
		out = append(out, IncidentDoc{
			Host: s.Host, Key: c.Key, Opened: c.Healer.IncidentOpened, Open: true, Class: c.Healer.Class, State: c.Healer.State,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Opened.After(out[j].Opened) })
	if s.History != nil {
		if closed, err := s.History.History(key); err == nil {
			for _, c := range closed {
				if len(out) >= limit {
					break
				}
				out = append(out, IncidentDoc{Host: s.Host, Key: c.Key, Opened: c.Opened.UTC(), Closed: c.Closed.UTC(), Class: c.Class, Outcome: c.Outcome})
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Incidents is GET /api/v1/incidents.
func (s *Service) Incidents(key string, limit int) IncidentsDoc {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return IncidentsDoc{Host: s.Host, Incidents: s.incidents(key, limit)}
}

// EventsQuery is the parameters of GET /api/v1/events.
type EventsQuery struct {
	Since  uint64
	Limit  int // default 100, at most 1000
	Key    string
	Types  []string
	Newest bool // for the history page: the newest events, not the ones after a cursor
}

// EventsPage returns events after a cursor (oldest first), or the newest ones.
func (s *Service) EventsPage(q EventsQuery) EventsDoc {
	doc := EventsDoc{Host: s.Host, Events: []EventDoc{}}
	if s.Events == nil {
		return doc
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	q.Limit = min(q.Limit, 1000)
	doc.LastID = s.Events.LastID()
	eq := engine.EventQuery{After: q.Since, Key: q.Key, Limit: q.Limit, Newest: q.Newest}
	for _, t := range q.Types {
		eq.Types = append(eq.Types, engine.EventType(t))
	}
	if q.Since > doc.LastID {
		// The caller is ahead of us: a wiped store, or a dry run that restarted its counter. Start over.
		doc.Reset, eq.After = true, 0
	}
	evs, err := s.Events.Query(eq)
	if err != nil {
		return doc
	}
	doc.Events = s.eventDocs(evs)
	doc.Next = q.Since
	if doc.Reset {
		doc.Next = 0
	}
	if !q.Newest {
		if n := len(doc.Events); n > 0 {
			doc.Next = doc.Events[n-1].ID
		}
	} else {
		doc.Next = doc.LastID
	}
	return doc
}

func engineQuery(after uint64, key string, limit int) engine.EventQuery {
	return engine.EventQuery{After: after, Key: key, Limit: limit}
}
