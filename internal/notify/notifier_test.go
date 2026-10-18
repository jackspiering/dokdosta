package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/jackspiering/whalenurse/internal/engine"
)

func suspended(incident string) engine.Event {
	return ev(engine.Suspended, webKey, "every rung ran or was unavailable: container is unhealthy",
		map[string]string{"reason": "ladder", engine.AttrIncident: incident})
}

func TestTheSameEventIsSentOncePerIncident(t *testing.T) {
	nt, s, _, _ := newTest(t, 2, nil)
	nt.Handle(suspended("inc-1"))
	nt.Handle(suspended("inc-1")) // the same thing, same incident: said once
	nt.Handle(suspended("inc-2")) // a new incident is news again
	nt.Handle(ev(engine.Recovered, webKey, "healthy", map[string]string{engine.AttrIncident: "inc-2"}))
	nt.Handle(ev(engine.Recovered, webKey, "healthy", map[string]string{engine.AttrIncident: "inc-2"}))
	flush(nt)
	for i, snd := range s {
		if got := len(snd.messages()); got != 3 {
			t.Errorf("url %d got %d messages, want 3 (suspended, suspended, recovered): %v", i+1, got, snd.titles())
		}
	}
}

func TestDedupeIsPerKey(t *testing.T) {
	nt, s, _, _ := newTest(t, 1, nil)
	nt.Handle(suspended("inc-1"))
	other := suspended("inc-1")
	other.Key, other.Container = "nas/stack/db/1", "stack-db-1"
	nt.Handle(other)
	flush(nt)
	if got := len(s[0].messages()); got != 2 {
		t.Fatalf("two containers, two messages; got %d", got)
	}
}

func TestADriftBlockedSuspensionIsExactlyOneMessage(t *testing.T) {
	nt, s, _, _ := newTest(t, 1, nil)
	nt.Handle(ev(engine.DriftBlocked, webKey, "recreate skipped by the drift guard: x", map[string]string{"findings": "the tag moved", engine.AttrIncident: "i"}))
	nt.Handle(ev(engine.Suspended, webKey, "recreate skipped by the drift guard (x): y", map[string]string{"reason": "drift", engine.AttrIncident: "i"}))
	flush(nt)
	if got := s[0].titles(); len(got) != 1 || !strings.Contains(got[0], "drift guard") {
		t.Fatalf("want one drift message, got %v", got)
	}
}

func TestAdvisoriesWithoutAnIncidentAreDedupedForADayOnly(t *testing.T) {
	nt, s, clk, _ := newTest(t, 1, nil)
	down := ev(engine.NotifyOnly, webKey, "left down", map[string]string{"class": "exited-host-stopped", "dependents": "stack-side-1"})
	nt.Handle(down)
	nt.Handle(down)
	settle(t, "first message", func() bool { return len(s[0].messages()) == 1 })
	clk.Advance(25 * time.Hour)
	nt.Handle(down)
	flush(nt)
	if got := len(s[0].messages()); got != 2 {
		t.Fatalf("a day later it is news again: got %d messages", got)
	}
}

func TestPauseAndResumeAreCommandsAndAreNeverDeduplicated(t *testing.T) {
	nt, s, _, _ := newTest(t, 1, nil)
	p := engine.Event{Host: testHost, Key: "nas/stack/web", Type: engine.Paused, Message: "paused"}
	nt.Handle(p)
	nt.Handle(p)
	flush(nt)
	if got := len(s[0].messages()); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

func TestVersionGateWarningIsSentOncePerProcess(t *testing.T) {
	nt, s, _, _ := newTest(t, 1, nil)
	g := engine.Event{Host: testHost, Type: engine.RungsDisabled, Message: "rungs 2 and 3 are disabled: Engine 27.0 is below 28.1"}
	nt.Handle(g)
	nt.Handle(g) // re-emitted after every reconnect
	flush(nt)
	if got := len(s[0].messages()); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestAnActionResultSaysWhyItWasStarted(t *testing.T) {
	nt, s, _, _ := newTest(t, 1, nil)
	nt.Handle(ev(engine.ActionStarted, webKey, "restart: container is unhealthy (3 failed checks)", map[string]string{"action": "restart", "rung": "restart"}))
	nt.Handle(ev(engine.ActionResult, webKey, "restart ok", map[string]string{"action": "restart", "rung": "restart", "ok": "true"}))
	flush(nt)
	m := s[0].messages()
	if len(m) != 1 || !strings.Contains(m[0].Body, "why: container is unhealthy (3 failed checks)") {
		t.Fatalf("%v", m)
	}
}

func TestEveryURLGetsEveryMessage(t *testing.T) {
	nt, s, _, _ := newTest(t, 3, nil)
	nt.Handle(suspended("i"))
	flush(nt)
	for i, snd := range s {
		if len(snd.messages()) != 1 {
			t.Errorf("url %d: %d messages", i+1, len(snd.messages()))
		}
	}
}

func TestRateLimitPerURLWithAReserveForCriticalMessages(t *testing.T) {
	nt, s, clk, _ := newTest(t, 2, func(o *Options) { o.RateLimit = 4 })
	// Routine messages may use half the budget (2). A storm of them must not stop "giving up" from getting through.
	for i := 0; i < 6; i++ {
		e := ev(engine.ActionResult, "nas/stack/web/"+string(rune('1'+i)), "restart ok", map[string]string{"action": "restart", "rung": "restart", "ok": "true"})
		nt.Handle(e)
	}
	nt.Handle(suspended("inc"))
	other := suspended("inc")
	other.Key = "nas/stack/db/1"
	nt.Handle(other)
	nt.Handle(ev(engine.NotifyOnly, "nas/stack/x/1", "left down", map[string]string{"class": "c"})) // the budget is spent by now
	settle(t, "delivery", func() bool { return len(s[0].messages()) >= 4 })
	flush(nt)
	got := s[0].messages()
	if len(got) != 4 {
		t.Fatalf("limit 4 per hour: got %d messages: %v", len(got), s[0].titles())
	}
	routine, critical := 0, 0
	for _, m := range got {
		if m.Critical {
			critical++
		} else {
			routine++
		}
	}
	if routine != 2 || critical != 2 {
		t.Errorf("want 2 routine + 2 critical, got %d + %d", routine, critical)
	}
	// Each URL has a limit of its own.
	if len(s[1].messages()) != 4 {
		t.Errorf("the second URL has its own budget: %d", len(s[1].messages()))
	}
	st := nt.Status().Targets[0]
	if st.RateLimited != 5 || st.Sent != 4 {
		t.Errorf("counted sent=%d limited=%d, want 4 and 5", st.Sent, st.RateLimited)
	}
	_ = clk
}

func TestHeldBackMessagesAreSummarisedInTheNextOne(t *testing.T) {
	nt, s, clk, _ := newTest(t, 1, func(o *Options) { o.RateLimit = 2 })
	rec := func(n int) engine.Event {
		return ev(engine.Recovered, "nas/stack/web/"+string(rune('0'+n)), "healthy", nil)
	}
	nt.Handle(rec(1)) // routine budget is 1
	nt.Handle(rec(2)) // held back
	nt.Handle(rec(3)) // held back
	settle(t, "first", func() bool { return len(s[0].messages()) == 1 && nt.Status().Targets[0].RateLimited == 2 })
	clk.Advance(61 * time.Minute)
	nt.Handle(rec(4))
	flush(nt)
	got := s[0].messages()
	if len(got) != 2 || !strings.Contains(got[1].Body, "2 earlier messages were held back") {
		t.Fatalf("%v", got)
	}
}

func TestAFailingURLIsRetriedCountedAndLoggedRedacted(t *testing.T) {
	var s *recSender
	nt, senders, _, lb := newTest(t, 1, func(o *Options) {
		o.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
		o.Redact = NewRedactor("ntfy://user:hunter2-token@ntfy.example.com/secret-topic")
	})
	s = senders[0]
	s.err = errors.New(`Post "https://ntfy.example.com/secret-topic": 401 for user hunter2-token`)
	nt.Handle(suspended("i"))
	flush(nt)
	st := nt.Status().Targets[0]
	if st.Failed != 1 || st.Sent != 0 || s.calls != 3 {
		t.Fatalf("failed=%d sent=%d attempts=%d, want 1, 0, 3", st.Failed, st.Sent, s.calls)
	}
	for _, text := range []string{lb.String(), st.LastError} {
		if strings.Contains(text, "hunter2-token") || strings.Contains(text, "secret-topic") {
			t.Errorf("a token leaked: %s", text)
		}
	}
	if !strings.Contains(lb.String(), "notification failed") || !strings.Contains(lb.String(), `"url":"#1 ntfy"`) {
		t.Errorf("the failure is not logged by label: %s", lb.String())
	}
}

// The hard rule: a failing or slow URL never blocks or delays the engine, which calls Handle with its lock held.
func TestHandleNeverBlocksEvenWithAHungURLAndAFullQueue(t *testing.T) {
	block := make(chan struct{})
	nt, senders, _, _ := newTest(t, 2, func(o *Options) { o.QueueSize = 4; o.Timeout = time.Minute })
	senders[0].block = block // hangs on every message
	start := time.Now()
	for i := 0; i < 500; i++ {
		nt.Handle(ev(engine.Suspended, "nas/stack/svc"+string(rune('a'+i%26))+"/"+string(rune('a'+i/26)), "x: y", map[string]string{"reason": "ladder", engine.AttrIncident: "i"}))
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Handle took %s for 500 events with a hung URL", d)
	}
	close(block)
}

func TestAHungURLDoesNotDelayAnother(t *testing.T) {
	block := make(chan struct{})
	nt, senders, _, _ := newTest(t, 2, func(o *Options) { o.Timeout = time.Minute })
	senders[0].block = block
	nt.Handle(suspended("i"))
	settle(t, "the healthy URL to get its message while the other hangs", func() bool { return len(senders[1].messages()) == 1 })
	close(block)
}

func TestATimeoutIsEnforcedPerAttempt(t *testing.T) {
	nt, senders, _, _ := newTest(t, 1, func(o *Options) { o.Timeout = 30 * time.Millisecond })
	senders[0].delay = time.Minute
	nt.Handle(suspended("i"))
	settle(t, "the timeout to count as a failure", func() bool { return nt.Status().Targets[0].Failed == 1 })
}

func TestShutdownDeliversWhatIsQueuedAndGivesUpOnWhatHangs(t *testing.T) {
	nt, senders, _, _ := newTest(t, 2, nil)
	senders[1].block = make(chan struct{}) // never released
	nt.Handle(suspended("i"))
	start := time.Now()
	nt.Shutdown(100 * time.Millisecond)
	if time.Since(start) > 2*time.Second {
		t.Fatal("Shutdown waited for a hung URL")
	}
	if len(senders[0].messages()) != 1 {
		t.Fatal("the message queued before the stop was not delivered")
	}
	// After Shutdown, Handle is a harmless no-op (the engine may still publish while the daemon stops).
	nt.Handle(suspended("j"))
	nt.Shutdown(time.Second)
}

func TestAnInvalidURLIsListedAndSendsNothing(t *testing.T) {
	good := &recSender{}
	nt, _, _, _ := newTest(t, 0, func(o *Options) {
		o.Targets = []Target{{Label: "#1 bogus", Err: "unknown service"}, {Label: "#2 ntfy", Sender: good}}
	})
	nt.Handle(suspended("i"))
	flush(nt)
	st := nt.Status()
	if len(good.messages()) != 1 || st.Targets[0].Invalid != "unknown service" || !strings.Contains(st.Summary(), "#1 bogus INVALID") {
		t.Fatalf("%+v / %s", st, st.Summary())
	}
}

func TestSendTestReportsEachURLAndBypassesTheLimits(t *testing.T) {
	ok, bad := &recSender{}, &recSender{err: errors.New("boom for secret-token-123")}
	targets := []Target{{Label: "#1 ntfy", Sender: ok}, {Label: "#2 gotify", Sender: bad}, {Label: "#3 bogus", Err: "unknown service"}}
	red := NewRedactor("gotify://h/secret-token-123")
	res := SendTest(context.Background(), targets, red, time.Second, TestMessage("nas", true))
	if !res[0].OK || res[1].OK || res[2].OK {
		t.Fatalf("%+v", res)
	}
	var out strings.Builder
	failed := PrintTest(&out, res)
	if failed != 2 || strings.Contains(out.String(), "secret-token-123") || !strings.Contains(out.String(), "#2 gotify") || !strings.Contains(out.String(), "FAILED") {
		t.Fatalf("failed=%d\n%s", failed, out.String())
	}
	m := ok.messages()[0]
	if !strings.HasPrefix(m.Title, DryRunMarker) || !strings.Contains(m.Body, "host nas") {
		t.Errorf("test message: %v", m)
	}
}

// With the dispatcher stalled and the queue full, Handle still returns at once and counts what it dropped: the engine
// calls it with its lock held.
func TestHandleDropsInsteadOfBlockingWhenTheQueueIsFull(t *testing.T) {
	lb := &logBuf{}
	n := &Notifier{in: make(chan engine.Event, 1), log: lb.logger()} // no dispatcher: nothing ever drains it
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			n.Handle(suspended("i"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked on a full queue")
	}
	if got := n.droppedEvents.Load(); got != 4 {
		t.Fatalf("dropped %d, want 4", got)
	}
	if n.Status().DroppedEvents != 4 {
		t.Error("the status does not show the dropped events")
	}
}

// A panic in a notification service must not take the healer down (Shoutrrr's router runs services in goroutines of its
// own with no recover, so whalenurse sends by calling the service itself, under a recover).
type panicSender struct{ recSender }

func (*panicSender) Send(context.Context, Message) error { panic("boom with token-in-panic-9999") }

func TestAPanickingSenderIsAFailedDeliveryAndOtherURLsStillGetTheirs(t *testing.T) {
	good := &recSender{}
	nt, _, _, lb := newTest(t, 0, func(o *Options) {
		o.Targets = []Target{{Label: "#1 bad", Sender: &panicSender{}}, {Label: "#2 good", Sender: good}}
		o.Redact = NewRedactor("ntfy://x/token-in-panic-9999")
	})
	nt.Handle(suspended("i"))
	nt.Handle(ev(engine.Recovered, webKey, "healthy", map[string]string{engine.AttrIncident: "i"}))
	flush(nt)
	st := nt.Status().Targets
	if st[0].Failed != 2 || st[0].Sent != 0 || len(good.messages()) != 2 {
		t.Fatalf("%+v / %d delivered to the good URL", st, len(good.messages()))
	}
	if strings.Contains(lb.String(), "token-in-panic-9999") || strings.Contains(st[0].LastError, "token-in-panic-9999") {
		t.Errorf("the panic value leaked a token: %s / %s", lb.String(), st[0].LastError)
	}
}

type fakeSvc struct {
	types.Service // never called: only the methods below are
	withCtx       bool
}

func (fakeSvc) GetID() string                    { return "fake" }
func (fakeSvc) Send(string, *types.Params) error { panic("plain send panics") }

type fakeCtxSvc struct{ fakeSvc }

func (fakeCtxSvc) SendContext(context.Context, string, *types.Params) error {
	panic("context send panics")
}

func TestShoutrrrSenderTurnsAServicePanicIntoAnError(t *testing.T) {
	for name, svc := range map[string]types.Service{"context sender": fakeCtxSvc{}, "plain sender": fakeSvc{}} {
		err := (&shoutrrrSender{svc: svc}).Send(context.Background(), Message{Title: "t", Body: "b"})
		if err == nil || !strings.Contains(err.Error(), "panic") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheHeldBackNoteNeverPushesTheBodyOverTheLimit(t *testing.T) {
	long := strings.Repeat("a long cause that goes on and on; ", 20)
	e := ev(engine.Suspended, webKey, "every rung ran or was unavailable: "+long, map[string]string{"reason": "ladder", engine.AttrIncident: "i"})
	m, _, _ := render(e, rctx{})
	got := withNote(m.Body, "(12 earlier messages were held back by the rate limit)", MaxBody)
	if utf8.RuneCountInString(got) > MaxBody || !strings.HasPrefix(got, webKey+"\n") || !strings.HasSuffix(got, "held back by the rate limit)") {
		t.Fatalf("%d runes: %q", utf8.RuneCountInString(got), got)
	}
	short := withNote("nas/p/web/1\nrecovered", "(1 earlier messages were held back by the rate limit)", MaxBody)
	if short != "nas/p/web/1\nrecovered\n(1 earlier messages were held back by the rate limit)" {
		t.Fatalf("%q", short)
	}
}
