package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Pinger is the uptime ping (WHALENURSE_HEARTBEAT_URL): a plain GET to a dead-man's-switch service such as
// healthchecks.io or an Uptime Kuma push monitor, every Interval, for as long as whalenurse is healthy. When it is not
// (the events stream has been down too long) it stays quiet, so the monitor raises the alarm by the missing ping.
//
// It is called the uptime ping in code and logs: "heartbeat" already names the persisted last_seen of the shutdown
// handling. The URL holds a token and goes through the same Redactor as the notification URLs.
type Pinger struct {
	URL      string
	Interval time.Duration
	Timeout  time.Duration // default 10s
	Healthy  func() bool   // nil: always healthy
	Log      *slog.Logger
	Redact   *Redactor
	Client   *http.Client // default: a client with Timeout

	once   sync.Once
	mu     sync.Mutex
	st     PingStatus
	failed bool
}

func (p *Pinger) defaults() {
	p.once.Do(func() {
		if p.Log == nil {
			p.Log = slog.New(slog.DiscardHandler)
		}
		if p.Timeout <= 0 {
			p.Timeout = 10 * time.Second
		}
		if p.Interval <= 0 {
			p.Interval = 5 * time.Minute
		}
		if p.Client == nil {
			p.Client = &http.Client{Timeout: p.Timeout}
		}
	})
}

// PingStatus is the uptime ping in the status: counts only, never the URL.
type PingStatus struct {
	Sent        int64     `json:"sent"`
	Failed      int64     `json:"failed"`
	Skipped     int64     `json:"skipped_unhealthy"`
	LastOK      time.Time `json:"last_ok,omitzero"`
	LastError   string    `json:"last_error,omitempty"` // redacted
	IntervalSec int       `json:"interval_seconds"`
}

// Status snapshots the counters.
func (p *Pinger) Status() PingStatus {
	p.defaults()
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.st
	s.IntervalSec = int(p.Interval / time.Second)
	return s
}

// Run pings at once and then every Interval until ctx ends.
func (p *Pinger) Run(ctx context.Context) {
	p.defaults()
	p.Tick(ctx)
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Tick(ctx)
		}
	}
}

// Tick makes one ping, unless whalenurse is unhealthy.
func (p *Pinger) Tick(ctx context.Context) {
	p.defaults()
	if p.Healthy != nil && !p.Healthy() {
		p.mu.Lock()
		p.st.Skipped++
		p.mu.Unlock()
		p.Log.Warn("uptime ping skipped: whalenurse is not healthy")
		return
	}
	err := p.ping(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.st.Failed++
		p.st.LastError = p.Redact.Err(err)
		p.Log.Warn("uptime ping failed", "error", p.st.LastError)
		p.failed = true
		return
	}
	p.st.Sent++
	p.st.LastOK = time.Now()
	if p.failed {
		p.Log.Info("uptime ping works again")
	}
	p.failed = false
}

func (p *Pinger) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return fmt.Errorf("uptime ping URL: %w", err)
	}
	req.Header.Set("User-Agent", "whalenurse")
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("uptime ping: HTTP %d", resp.StatusCode)
	}
	return nil
}
