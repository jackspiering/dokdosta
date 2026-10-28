package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackspiering/dokdosta/internal/compose"
	"github.com/jackspiering/dokdosta/internal/docker"
)

// declaredTimeout bounds one `compose config` read made for the label filter; declaredRetry is how long a read that failed
// is not tried again (the labels are then judged as for a container that compose did not make).
const (
	declaredTimeout = 20 * time.Second
	declaredRetry   = 5 * time.Minute
	// declaredTTL is how long a good answer is kept. A little under docker.DeclaredTTL, so that a decision of the filter
	// that expires finds the answer expired too.
	declaredTTL = 9 * time.Minute
)

// declaredEntry is what one `compose config` read answered, for one service at one config-hash.
type declaredEntry struct {
	keys map[string]bool
	ok   bool
	at   time.Time
}

// declaredCache keeps the answers of Compose.DeclaredLabels (plan v3.13 item 10): a service's labels change only with its
// config-hash, which compose writes on the container, so one read serves every poll until the service is recreated from an
// edited file.
type declaredCache struct {
	mu        sync.Mutex
	m         map[string]declaredEntry
	downUntil time.Time // after a read timed out, none is tried before this: a hung compose must not stall every service in turn
}

// declaredLabels is docker.ImageLabelFilter.Declared: the dokdosta.* keys that the compose file declares in the
// `labels:` of the container's service. ok is false when compose is not usable for this container (version gate, no
// binary, labels or files unreadable, the read failed), and the filter then judges it as a container compose did not make.
func (d *Daemon) declaredLabels(ctx context.Context, labels map[string]string) (map[string]bool, bool) {
	if d.o.Compose == nil || !d.cs().gate.Recreate {
		return nil, false
	}
	p, err := compose.ProjectFromLabels(labels)
	if err != nil || p.Readable(d.o.Stat) != nil {
		return nil, false
	}
	key := p.Name + "/" + p.Service + "|" + labels[compose.LabelConfigHash]
	now := d.clk.Now()
	d.decl.mu.Lock()
	e, hit := d.decl.m[key]
	down := now.Before(d.decl.downUntil)
	d.decl.mu.Unlock()
	valid := declaredRetry
	if e.ok {
		valid = declaredTTL
	}
	if hit && now.Sub(e.at) < valid {
		return e.keys, e.ok
	}
	if down {
		return nil, false
	}
	rctx, cancel := context.WithTimeout(ctx, declaredTimeout)
	defer cancel()
	list, err := d.o.Compose.DeclaredLabels(rctx, p)
	e = declaredEntry{ok: err == nil, at: now}
	if err == nil {
		e.keys = make(map[string]bool, len(list))
		for _, k := range list {
			e.keys[k] = true
		}
	} else {
		d.log.Debug("cannot read the labels the compose file declares", "service", p.Name+"/"+p.Service)
	}
	d.decl.mu.Lock()
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(rctx.Err(), context.DeadlineExceeded) {
		d.decl.downUntil = now.Add(declaredRetry)
	}
	if d.decl.m == nil || len(d.decl.m) > 2048 {
		d.decl.m = map[string]declaredEntry{}
	}
	d.decl.m[key] = e
	d.decl.mu.Unlock()
	return e.keys, e.ok
}

// warnImageLabels is docker.ImageLabelFilter.Warn.
func (d *Daemon) warnImageLabels(container, reason string, keys []string) {
	d.log.Warn("labels from the image do not steer dokdosta", "container", container, "labels", keys, "detail", reason)
}

// newLabelFilter wraps the Docker reader: List and Inspect return the container's labels without those that come from its
// image (docker.ImageLabelFilter).
func (d *Daemon) newLabelFilter(r docker.Reader) *docker.ImageLabelFilter {
	return &docker.ImageLabelFilter{Reader: r, Declared: d.declaredLabels, Warn: d.warnImageLabels, Now: d.clk.Now}
}
