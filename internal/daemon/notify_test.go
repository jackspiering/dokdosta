package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/notify"
)

// M3: the notifier as one more subscriber of the daemon's event stream.

type captureSender struct {
	mu    sync.Mutex
	got   []notify.Message
	block chan struct{}
}

func (s *captureSender) Send(ctx context.Context, m notify.Message) error {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	return nil
}
func (s *captureSender) Close() error { return nil }
func (s *captureSender) messages() []notify.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]notify.Message(nil), s.got...)
}

func withNotifier(t *testing.T, snd *captureSender, dry bool) func(*Options) {
	return func(o *Options) {
		nt := notify.New(notify.Options{Host: host, DryRun: dry, Clock: o.Clock, Targets: []notify.Target{{Label: "#1 test", Sender: snd}}, Timeout: time.Second})
		t.Cleanup(func() { nt.Shutdown(2 * time.Second) })
		o.Bus.Subscribe(nt.Handle)
		o.NotifyStatus = nt.Status
	}
}

func ladderToSuspended(h *cfH) docker.Container {
	web := h.svc(hexID("a1"), "p", "web")
	h.cf.OnRecreate = func(p compose.Project) error {
		h.replace("p", "web", func(c *docker.Container) { c.Health = "unhealthy" })
		return nil
	}
	h.tick()
	h.set(web.ID, unhealthy)
	h.advance(6 * time.Minute)
	return web
}

func TestTheLadderEndingInSuspendedIsNotifiedOncePerStepWithTheCause(t *testing.T) {
	snd := &captureSender{}
	h := newCompose(t, nil, nil, docker.ServerInfo{}, withNotifier(t, snd, false))
	ladderToSuspended(h)
	h.advance(30 * time.Minute) // nothing more happens, nothing more is said
	settleMessages(t, snd, 3)
	got := snd.messages()
	if len(got) != 3 {
		t.Fatalf("restart, recreate, giving up: got %d: %v", len(got), got)
	}
	for i, want := range []string{"restarted", "recreated", "giving up, SUSPENDED"} {
		if !strings.Contains(got[i].Title, want) {
			t.Errorf("message %d title %q lacks %q", i+1, got[i].Title, want)
		}
		if !strings.Contains(got[i].Body, key("p", "web")) || !strings.Contains(got[i].Title+got[i].Body, host) {
			t.Errorf("message %d lacks the host or the key: %v", i+1, got[i])
		}
	}
	if !strings.Contains(got[2].Body, "cause:") || !strings.Contains(got[2].Body, "unhealthy") || !strings.Contains(got[2].Body, "whalenurse unsuspend p/web") {
		t.Errorf("the giving-up message has no cause or next step: %v", got[2])
	}
	if !strings.Contains(got[0].Body, "why:") {
		t.Errorf("the action message does not say why: %v", got[0])
	}
}

func TestARecoveryIsNotified(t *testing.T) {
	snd := &captureSender{}
	h := newCompose(t, nil, nil, docker.ServerInfo{}, withNotifier(t, snd, false))
	web := h.svc(hexID("a1"), "p", "web")
	h.cf.OnRecreate = func(p compose.Project) error {
		h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy" })
		return nil
	}
	h.tick()
	h.set(web.ID, unhealthy)
	h.advance(15 * time.Minute)
	settleMessages(t, snd, 3)
	var titles []string
	for _, m := range snd.messages() {
		titles = append(titles, m.Title)
	}
	if len(titles) != 3 || !strings.Contains(titles[2], "recovered") || strings.Contains(strings.Join(titles, "|"), "SUSPENDED") {
		t.Fatalf("%v", titles)
	}
}

func TestADryRunNotifiesAndMarksIt(t *testing.T) {
	snd := &captureSender{}
	h := newCompose(t, func(c *config.Global) { c.DryRun = true }, nil, docker.ServerInfo{}, withNotifier(t, snd, true))
	ladderToSuspended(h)
	settleMessages(t, snd, 1)
	for _, m := range snd.messages() {
		if !strings.HasPrefix(m.Title, "[dry-run]") || !strings.HasPrefix(m.Body, "[dry-run] ") {
			t.Errorf("not marked: %v", m)
		}
	}
	if len(h.fake.Calls()) != 0 {
		t.Errorf("a dry run changed something: %v", h.fake.Calls())
	}
}

// The hard rule: a notification that never completes does not delay or block healing.
func TestAHungNotificationURLNeverBlocksHealing(t *testing.T) {
	snd := &captureSender{block: make(chan struct{})}
	h := newCompose(t, nil, nil, docker.ServerInfo{}, withNotifier(t, snd, false))
	start := time.Now()
	ladderToSuspended(h)
	if h.count(engine.Suspended) != 1 || h.count(engine.ActionStarted) != 2 {
		t.Fatalf("healing did not run to the end: %v", h.rec.Types())
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("healing took %s with a hung notification URL", d)
	}
	close(snd.block)
}

func TestStatusShowsNotificationCountsByLabelOnly(t *testing.T) {
	snd := &captureSender{}
	h := newCompose(t, nil, nil, docker.ServerInfo{}, withNotifier(t, snd, false))
	ladderToSuspended(h)
	settleMessages(t, snd, 3)
	st := h.d.Status().(Status)
	if !st.Notify.Enabled || len(st.Notify.Targets) != 1 || st.Notify.Targets[0].URL != "#1 test" {
		t.Fatalf("%+v", st.Notify)
	}
	settleSent(t, h, 3)
}

func settleMessages(t *testing.T, s *captureSender, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.messages()) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out: %d messages, want at least %d", len(s.messages()), n)
}

func settleSent(t *testing.T, h *cfH, n int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.d.Status().(Status).Notify.Targets[0].Sent >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("status.sent never reached %d: %+v", n, h.d.Status().(Status).Notify)
}

// Unsuspended is a routine message: a SUSPENDED container that comes back is told, and then it is recovered.
func TestAContainerThatComesBackAfterSuspendedIsAnnouncedAsUnsuspendedThenRecovered(t *testing.T) {
	snd := &captureSender{}
	h := newCompose(t, nil, nil, docker.ServerInfo{}, withNotifier(t, snd, false))
	ladderToSuspended(h)
	h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy" })
	h.advance(15 * time.Minute)
	settleMessages(t, snd, 5)
	var got []string
	for _, m := range snd.messages() {
		got = append(got, m.Title)
	}
	joined := strings.Join(got, "|")
	if len(got) != 5 || !strings.Contains(got[2], "SUSPENDED") || !strings.Contains(got[3], "unsuspended") || !strings.Contains(got[4], "recovered") {
		t.Fatalf("%s", joined)
	}
	if snd.messages()[3].Critical {
		t.Error("unsuspended is routine")
	}
}
