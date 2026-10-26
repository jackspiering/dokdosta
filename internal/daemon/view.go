package daemon

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/rules"
)

// view is the read side of the daemon for the API and the dashboard. The loop goroutine writes it (the container list
// at every refresh, one inspect and its classification at every process); HTTP goroutines read it. The loop's own
// index is not shared, so the loop never waits for a reader beyond a short copy.
type view struct {
	mu   sync.RWMutex
	list []docker.Summary
	seen map[string]seen
}

// seen is the last inspect of a key and what the rules made of it.
type seen struct {
	c     docker.Container
	dec   rules.Decision
	decAt time.Time
}

func newView() *view { return &view{seen: map[string]seen{}} }

func (v *view) setList(list []docker.Summary) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.list = append([]docker.Summary(nil), list...)
}

func (v *view) observe(key string, c docker.Container, dec rules.Decision, at time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seen[key] = seen{c: c, dec: dec, decAt: at}
}

func (v *view) forget(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.seen, key)
}

func (v *view) snapshot() ([]docker.Summary, map[string]seen) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m := make(map[string]seen, len(v.seen))
	for k, s := range v.seen {
		m[k] = s
	}
	return append([]docker.Summary(nil), v.list...), m
}

// apiSource adapts the daemon to api.Source (Daemon.Status already names the CLI's reply).
type apiSource struct{ d *Daemon }

// NewAPI wires the read-only API service: the daemon's view, the event log (the store, or a ring in a dry run), the
// closed-incident history (nil in a dry run), the hub that wakes the event streams, and the redactor.
func NewAPI(d *Daemon, evlog engine.EventLog, history api.History, hub *api.Hub, redact func(string) string) *api.Service {
	return &api.Service{
		Host: d.cfg.HostID, Source: d.APISource(), Events: evlog, History: history, Redact: redact, Hub: hub, Metrics: d.cfg.Metrics,
	}
}

// APISource is the daemon's half of the read-only API.
func (d *Daemon) APISource() api.Source { return apiSource{d} }

var _ api.Source = apiSource{}

func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// relation describes the other end of a network_mode link, with its state, so a page can show whether the parent is
// the reason a sidecar is down.
func relation(d *Daemon, s docker.Summary, ev engine.View) api.RelationDoc {
	key := d.keyOf(s)
	r := api.RelationDoc{Host: d.cfg.HostID, Key: key, Name: s.Name, State: s.State, Health: s.Health, HealerState: "ok"}
	if k, ok := ev.Keys[key]; ok {
		r.HealerState = k.State
	}
	return r
}

// Containers implements api.Source: the last container list merged with the engine's state.
func (s apiSource) Containers() []api.ContainerDoc { return s.d.containerDocs() }

func (d *Daemon) containerDocs() []api.ContainerDoc {
	list, seen := d.view.snapshot()
	ev := d.eng.View()

	// Two containers can share a key while compose renames the old one, and a `compose run` container shares its service's:
	// docker.PreferForKey decides, as in the loop's index.
	newest := map[string]docker.Summary{}
	for _, s := range list {
		k := d.keyOf(s)
		if old, ok := newest[k]; !ok || docker.PreferForKey(old, s) {
			newest[k] = s
		}
	}
	keys := make([]string, 0, len(newest))
	for k := range newest {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]api.ContainerDoc, 0, len(keys))
	for _, key := range keys {
		s := newest[key]
		cfg, _ := config.ParseLabels(s.Labels, d.cfg.Mode)
		sn := seen[key]
		inspected := sn.c.ID != "" && sn.c.ID == s.ID
		c := api.ContainerDoc{
			Host: d.cfg.HostID, Key: key, Name: s.Name, ID: docker.ShortID(s.ID), Image: s.Image,
			State: s.State, Health: s.Health, OptedIn: cfg.OptedIn,
			Healer: api.HealerDoc{State: "ok", FlapLimit: d.cfg.FlapLimit},
		}
		if p, sv, n, ok := docker.ComposeKeyParts(s.Labels); ok {
			c.Project, c.Service, c.Number = p, sv, n
		}
		if inspected {
			ic := sn.c
			c.RestartCount, c.ExitCode, c.OOMKilled = ic.RestartCount, ic.ExitCode, ic.OOMKilled
			c.StartedAt, c.FinishedAt, c.Policy, c.StartError = utc(ic.StartedAt), utc(ic.FinishedAt), ic.RestartPolicy, ic.Error
			if c.Image == "" {
				c.Image = ic.Image
			}
		}
		if sn.decAt != (time.Time{}) {
			c.Decision = &api.DecisionDoc{
				Class: string(sn.dec.Class), Action: string(sn.dec.Action), Skip: sn.dec.Skip, Reason: sn.dec.Reason, At: utc(sn.decAt),
			}
		}
		c.Watched = cfg.OptedIn || (sn.decAt != (time.Time{}) && sn.dec.Class != rules.ClassSkip)
		if c.Watched {
			c.Ladder, c.HealExited = cfg.Actions, string(cfg.HealExited)
		}
		if cfg.FlapLimit > 0 {
			c.Healer.FlapLimit = cfg.FlapLimit
		}
		c.Labels = map[string]string{}
		for k, v := range s.Labels {
			if strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "whalenurse.") {
				c.Labels[k] = v
			}
		}
		if ref := docker.ContainerRef(s.NetworkMode); ref != "" {
			c.Parent = &api.RelationDoc{Host: d.cfg.HostID, Name: ref}
			for _, p := range list {
				if p.ID != s.ID && docker.MatchesRef(p.ID, p.Name, ref) {
					r := relation(d, p, ev)
					c.Parent = &r
					break
				}
			}
		}
		for _, o := range list {
			if o.ID != s.ID && docker.MatchesRef(s.ID, s.Name, docker.ContainerRef(o.NetworkMode)) {
				c.Dependents = append(c.Dependents, relation(d, o, ev))
			}
		}
		sort.Slice(c.Dependents, func(i, j int) bool { return c.Dependents[i].Key < c.Dependents[j].Key })

		if _, ok := ev.Keys[key]; !ok {
			// No engine state yet (never evaluated): a pause still covers the container.
			if p, ok := ev.PauseFor(key, d.clk.Now()); ok {
				c.Healer.Paused = &api.PausedDoc{Scope: p.Scope, Since: utc(p.Created), Until: utc(p.Until)}
			}
		}
		if k, ok := ev.Keys[key]; ok {
			h := &c.Healer
			h.State, h.Class, h.IncidentOpened, h.CooldownUntil = k.State, k.Class, utc(k.Opened), utc(k.CooldownUntil)
			h.RecoverySince, h.Flaps, h.ManualStop, h.LoopIncreases = utc(k.RecoverySince), k.Flaps, k.ManualStop, k.LoopIncreases
			if !k.Opened.IsZero() {
				h.RungsRun = k.Rung
				if !k.Spent && k.Rung < len(c.Ladder) {
					h.NextRung = c.Ladder[k.Rung]
				}
			}
			if k.State == "SUSPENDED" {
				h.Suspended = &api.SuspendDoc{Reason: k.SpentReason, Since: utc(k.SuspendedAt)}
			}
			if k.PausedScope != "" {
				h.Paused = &api.PausedDoc{Scope: k.PausedScope, Since: utc(k.PausedSince), Until: utc(k.PausedUntil)}
			}
		}
		out = append(out, c)
	}
	return out
}

// Status implements api.Source.
func (s apiSource) Status() api.StatusDoc {
	d := s.d
	ok, detail := d.Healthy()
	ev := d.eng.View()
	cs := d.composeStatus()
	st := api.StatusDoc{
		Host: d.cfg.HostID, API: api.APIVersion, Version: d.o.Version, Time: d.clk.Now().UTC(), StartedAt: d.started.UTC(),
		Mode: string(d.cfg.Mode), DryRun: d.cfg.DryRun, Persistent: d.o.Persist != nil, Healthy: ok, HealthDetail: detail,
		EventsUp: d.eventsUp.Load(), Settled: d.settledFlag.Load(), DriftGuard: cs.DriftGuard,
		Compose:      api.ComposeDoc{Binary: cs.Binary, Source: cs.Source, Version: cs.Version, Engine: cs.Engine, Arch: cs.EngineArch},
		VersionGate:  api.VersionGateDoc{RecreateEnabled: cs.Rungs, DisabledRungs: cs.Disabled, Reason: cs.Reason, MinEngine: compose.MinEngine.String(), MinCompose: compose.MinCompose.String()},
		RestoreUntil: ev.RestoreUntil, HistoryRetention: int64(d.cfg.HistoryRetention.Seconds()), Store: d.storeDoc(),
	}
	st.RateLimit = api.RateLimitDoc{Limit: ev.RateLimit, Used: ev.ActionsLastH, NextSlot: ev.NextSlot}
	if ev.RateLimit > 0 {
		st.RateLimit.Remaining = max(ev.RateLimit-ev.ActionsLastH, 0)
	}
	for _, p := range ev.Pauses {
		st.Pauses = append(st.Pauses, api.PauseDoc{Host: d.cfg.HostID, Scope: p.Scope, Created: p.Created, Until: p.Until})
	}
	sort.Slice(st.Pauses, func(i, j int) bool { return st.Pauses[i].Scope < st.Pauses[j].Scope })

	docs := d.containerDocs()
	st.Counts.Containers = len(docs)
	for _, c := range docs {
		if !c.Watched {
			continue
		}
		st.Counts.Watched++
		switch c.Healer.State {
		case "incident":
			st.Counts.Incident++
		case "recovering":
			st.Counts.Recovering++
		case "SUSPENDED":
			st.Counts.Suspended++
		default:
			st.Counts.OK++
		}
		if c.Healer.Paused != nil {
			st.Counts.Paused++
		}
	}
	st.PullAuth.Registries = pullAuthHosts(d.o.PullAuthHosts)
	if d.o.NotifyStatus != nil {
		ns := d.o.NotifyStatus()
		n := api.NotificationsDoc{Enabled: ns.Enabled, URLs: len(ns.Targets), Dropped: ns.DroppedEvents}
		for _, t := range ns.Targets {
			n.Sent, n.Failed, n.RateLimited, n.Dropped = n.Sent+t.Sent, n.Failed+t.Failed, n.RateLimited+t.RateLimited, n.Dropped+t.Dropped
			if t.Invalid != "" {
				n.Invalid++
			}
		}
		if p := ns.Ping; p != nil {
			n.UptimePing = &api.PingDoc{Sent: p.Sent, Failed: p.Failed, SkippedUnhealthy: p.Skipped, IntervalSeconds: p.IntervalSec}
		}
		st.Notifications = n
	}
	return st
}
