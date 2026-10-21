package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackspiering/whalenurse/internal/control"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/notify"
)

// Status is the reply of `whalenurse status`.
type Status struct {
	Host         string        `json:"host"`
	Mode         string        `json:"mode"`
	DryRun       bool          `json:"dry_run"`
	Persistent   bool          `json:"persistent"` // false in dry-run: nothing is stored
	EventsUp     bool          `json:"events_up"`
	Settled      bool          `json:"settled"`
	Healthy      bool          `json:"healthy"`
	HealthDetail string        `json:"health_detail,omitempty"`
	Compose      ComposeStatus `json:"compose"`
	Engine       engine.Status `json:"engine"`
	Notify       notify.Status `json:"notify"`    // counts per URL by position and scheme; never a URL
	PullAuth     []string      `json:"pull_auth"` // registry hosts the pull rung has credentials for; never a user name or a secret
}

// Status snapshots the daemon for the CLI.
func (d *Daemon) Status() any {
	ok, detail := d.Healthy()
	st := Status{
		Host: d.cfg.HostID, Mode: string(d.cfg.Mode), DryRun: d.cfg.DryRun, Persistent: d.o.Persist != nil,
		EventsUp: d.eventsUp.Load(), Settled: d.settledFlag.Load(), Healthy: ok, HealthDetail: detail,
		Compose: d.composeStatus(), Engine: d.eng.Status(), PullAuth: pullAuthHosts(d.o.PullAuthHosts),
	}
	if d.o.NotifyStatus != nil {
		st.Notify = d.o.NotifyStatus()
	}
	return st
}

var _ control.Backend = (*Daemon)(nil)

// Pause implements control.Backend.
func (d *Daemon) Pause(scope string, dur time.Duration) (string, error) {
	p, err := d.eng.Pause(scope, dur)
	if err != nil {
		return "", err
	}
	if p.Until.IsZero() {
		return fmt.Sprintf("paused %s until you run resume", p.Scope), nil
	}
	return fmt.Sprintf("paused %s until %s", p.Scope, p.Until.Local().Format(time.RFC1123)), nil
}

// Resume implements control.Backend.
func (d *Daemon) Resume(scope string) (string, error) {
	was, err := d.eng.Resume(scope)
	if err != nil {
		return "", err
	}
	if !was {
		return "", fmt.Errorf("%w: %s is not paused", control.ErrNothingToDo, strings.TrimSpace(scope))
	}
	return fmt.Sprintf("resumed %s; its containers are looked at again within a second", strings.TrimSpace(scope)), nil
}

// Unsuspend implements control.Backend.
func (d *Daemon) Unsuspend(scope string) (string, error) {
	keys, err := d.eng.Unsuspend(scope)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("%w: no suspended container under %s", control.ErrNothingToDo, strings.TrimSpace(scope))
	}
	return "unsuspended " + strings.Join(keys, ", "), nil
}

// ParseFor reads the --for value: empty is the default pause, "0" is indefinite.
func ParseFor(v string, parse func(string) (time.Duration, error)) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return engine.DefaultPause, nil
	}
	d, err := parse(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("--for %q: want a duration such as 2h or 1d, or 0 for no expiry", v)
	}
	return d, nil
}

// pullAuthHosts is never nil, so the JSON is [] and not null.
func pullAuthHosts(h []string) []string {
	out := make([]string, 0, len(h))
	for _, s := range h {
		if len(s) > 253 {
			s = s[:253]
		}
		out = append(out, s)
	}
	return out
}
