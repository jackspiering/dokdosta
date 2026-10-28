package notify

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/clock"
	"github.com/jackspiering/dokdosta/internal/engine"
)

// recSender records what it is asked to send, and can fail, block or take time.
type recSender struct {
	mu    sync.Mutex
	got   []Message
	err   error         // returned by every Send
	block chan struct{} // when set, Send waits for it (or for the context)
	delay time.Duration
	calls int
}

func (s *recSender) Send(ctx context.Context, m Message) error {
	s.mu.Lock()
	s.calls++
	block, delay, err := s.block, s.delay, s.err
	s.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.got = append(s.got, m)
	s.mu.Unlock()
	return nil
}
func (s *recSender) Close() error { return nil }

func (s *recSender) messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.got...)
}

func (s *recSender) titles() []string {
	var out []string
	for _, m := range s.messages() {
		out = append(out, m.Title)
	}
	return out
}

// logBuf is a slog target the tests can grep.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *logBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
func (l *logBuf) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

const testHost = "nas"

func ev(typ engine.EventType, key, msg string, attrs map[string]string) engine.Event {
	return engine.Event{Host: testHost, Key: key, Container: "stack-web-1", Type: typ, Message: msg, Attrs: attrs}
}

const webKey = "nas/stack/web/1"

// newTest builds a Notifier over recording senders with no retry delay, on a fake clock.
func newTest(t *testing.T, n int, tweak func(*Options)) (*Notifier, []*recSender, *clock.Fake, *logBuf) {
	t.Helper()
	clk := clock.NewFake(time.Time{})
	lb := &logBuf{}
	var senders []*recSender
	var targets []Target
	for i := 0; i < n; i++ {
		s := &recSender{}
		senders = append(senders, s)
		targets = append(targets, Target{Label: Label(i, "ntfy://x"), Sender: s})
	}
	o := Options{Host: testHost, Clock: clk, Log: lb.logger(), Targets: targets, Timeout: time.Second, Backoff: []time.Duration{}, RateLimit: 100}
	if tweak != nil {
		tweak(&o)
	}
	nt := New(o)
	t.Cleanup(func() { nt.Shutdown(2 * time.Second) })
	return nt, senders, clk, lb
}

// settle waits until cond holds.
func settle(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// flush shuts the notifier down (delivering everything queued) so a test can count exactly.
func flush(nt *Notifier) { nt.Shutdown(2 * time.Second) }
