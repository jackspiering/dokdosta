package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// hook is a webhook receiver that records what the generic service posts.
type hook struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]string
	paths  []string
	status int
}

func newHook(t *testing.T, status int) *hook {
	h := &hook{status: status}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m := map[string]string{}
		_ = json.Unmarshal(b, &m)
		h.mu.Lock()
		h.bodies = append(h.bodies, m)
		h.paths = append(h.paths, r.URL.Path)
		h.mu.Unlock()
		w.WriteHeader(h.status)
	}))
	t.Cleanup(h.Close)
	return h
}

// genericURL is the Shoutrrr URL of the hook: the generic service, JSON, plain http.
func (h *hook) genericURL(path string) string {
	u, _ := url.Parse(h.URL)
	return "generic://" + u.Host + path + "?template=json&disabletls=yes"
}

func TestGenericServiceDeliversTitleAndMessageAsJSON(t *testing.T) {
	h := newHook(t, 200)
	red := NewRedactor(h.genericURL("/hook/tok-abcdef-123"))
	targets := NewTargets([]string{h.genericURL("/hook/tok-abcdef-123")}, red, 5*time.Second)
	if targets[0].Sender == nil {
		t.Fatalf("the generic URL was rejected: %s", targets[0].Err)
	}
	res := SendTest(context.Background(), targets, red, 5*time.Second, TestMessage("nas", false))
	if !res[0].OK {
		t.Fatalf("%+v", res[0])
	}
	if len(h.bodies) != 1 || h.paths[0] != "/hook/tok-abcdef-123" {
		t.Fatalf("%v %v", h.bodies, h.paths)
	}
	got := h.bodies[0]
	if got["title"] != "nas: test notification" || !strings.Contains(got["message"], "host nas") {
		t.Fatalf("%v", got)
	}
}

func TestAnInvalidShoutrrrURLBecomesAnInvalidTargetWithoutTheURL(t *testing.T) {
	targets := NewTargets([]string{"nosuchservice://tok-abcdef-123@host/secret-path", "not a url at all tok-xyz-987"}, NewRedactor("nosuchservice://tok-abcdef-123@host/secret-path", "not a url at all tok-xyz-987"), time.Second)
	for i, tg := range targets {
		if tg.Sender != nil || tg.Err == "" {
			t.Fatalf("target %d should be invalid: %+v", i, tg)
		}
		for _, s := range []string{"tok-abcdef-123", "secret-path", "tok-xyz-987"} {
			if strings.Contains(tg.Err, s) || strings.Contains(tg.Label, s) {
				t.Errorf("target %d leaks %q: %+v", i, s, tg)
			}
		}
	}
	if targets[0].Label != "#1 nosuchservice" || targets[1].Label != "#2 invalid" {
		t.Errorf("%q %q", targets[0].Label, targets[1].Label)
	}
}

func TestFailuresOfARealWebhookAreRedacted(t *testing.T) {
	h401 := newHook(t, 401)
	down := newHook(t, 200)
	downURL := down.genericURL("/hook/down-token-9999")
	down.Close() // connection refused: the error of Go's HTTP client quotes the URL
	urls := []string{h401.genericURL("/hook/token-aaaa-1111?x=1"), downURL}
	red := NewRedactor(urls...)
	targets := NewTargets(urls, red, 2*time.Second)
	res := SendTest(context.Background(), targets, red, 2*time.Second, TestMessage("nas", false))
	for i, r := range res {
		if r.OK {
			t.Fatalf("url %d should fail", i+1)
		}
		for _, s := range []string{"token-aaaa-1111", "down-token-9999", "127.0.0.1"} {
			if strings.Contains(r.Err, s) {
				t.Errorf("url %d: %q leaks %q", i+1, r.Err, s)
			}
		}
	}
}

// The whole path, with the engine's event types: the receiver sees the ladder end in a SUSPENDED and a recovery.
func TestEndToEndThroughTheGenericService(t *testing.T) {
	h := newHook(t, 200)
	u := h.genericURL("/hook")
	red := NewRedactor(u)
	nt, _, _, _ := newTest(t, 0, func(o *Options) {
		o.Targets = NewTargets([]string{u}, red, 5*time.Second)
		o.Redact = red
	})
	inc := map[string]string{engine.AttrIncident: "i"}
	with := func(a map[string]string) map[string]string {
		for k, v := range inc {
			a[k] = v
		}
		return a
	}
	nt.Handle(ev(engine.ActionStarted, webKey, "restart: container is unhealthy", with(map[string]string{"action": "restart", "rung": "restart"})))
	nt.Handle(ev(engine.ActionResult, webKey, "restart ok", with(map[string]string{"action": "restart", "rung": "restart", "ok": "true"})))
	nt.Handle(ev(engine.Suspended, webKey, "every rung ran or was unavailable: container is unhealthy", with(map[string]string{"reason": "ladder"})))
	nt.Handle(ev(engine.Recovered, webKey, "container has been healthy for 10m0s", with(map[string]string{})))
	flush(nt)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bodies) != 3 {
		t.Fatalf("%d messages: %v", len(h.bodies), h.bodies)
	}
	want := []string{"restarted", "giving up, SUSPENDED", "recovered"}
	for i, w := range want {
		if !strings.Contains(h.bodies[i]["title"], w) || !strings.Contains(h.bodies[i]["message"], webKey) {
			t.Errorf("message %d: %v, want %q", i, h.bodies[i], w)
		}
	}
}
