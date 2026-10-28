package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/config"
	"github.com/jackspiering/dokdosta/internal/docker"
	"github.com/jackspiering/dokdosta/internal/engine"
)

// runDaemon starts Run on the harness's daemon, as main does, and stops it when the test ends. Tests that use it drive
// the daemon through the fake Docker's events stream (Emit, BreakStream) and read the engine's events from h.rec.
func runDaemon(t *testing.T, h *harness) {
	t.Helper()
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.d.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// The stream signals and the events travel one channel, so a kill that follows a loss and a reconnect is handled after
// them: it falls in the restore window, whatever Run was busy with. Run is held inside the first event (a gated
// Inspect) while the stream breaks, comes back and delivers the kill; with two channels the kill was taken before the
// loss and the reconnect and marked a manual stop.
func TestRunKeepsEventsAndSignalsInOrder(t *testing.T) {
	h := newHarness(t, func(c *config.Global) { c.StartupSettle = time.Minute }, nil)
	h.d.o.ReconnectBackoff = 5 * time.Millisecond
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.fake.InspectWait = func(ctx context.Context, _ string) error {
		first := false
		once.Do(func() { first = true })
		if first {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	runDaemon(t, h)

	waitFor(t, func() bool { return len(h.fake.Subscriptions()) == 1 })
	web := labels("p", "web")
	h.fake.Emit(docker.Event{Action: "start", ID: hexID("a1"), Name: "p-web-1", Labels: web, Time: h.clk.Now()})
	<-entered // Run is inside the first event, so nothing after it is read until release
	h.fake.BreakStream(fmt.Errorf("connection reset"))
	waitFor(t, func() bool { return len(h.fake.Subscriptions()) == 2 })
	h.fake.Emit(docker.Event{Action: "kill", Signal: "9", ID: hexID("a1"), Name: "p-web-1", Labels: web, Time: h.clk.Now()})
	waitFor(t, func() bool { return h.d.eventsUp.Load() }) // the new stream delivered the kill: connected, then the kill
	close(release)

	waitFor(t, func() bool { return h.rec.Count(engine.RestoreEvent)+h.rec.Count(engine.ManualStopMarked) > 0 })
	var got []engine.EventType
	for _, ty := range h.rec.Types() {
		switch ty {
		case engine.RestoreWindowOpened, engine.RestoreEvent, engine.ManualStopMarked:
			got = append(got, ty)
		}
	}
	want := []engine.EventType{engine.RestoreWindowOpened, engine.RestoreEvent}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("engine events %v, want %v: the kill must be handled after the loss and the reconnect", got, want)
	}
}
