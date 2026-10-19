package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/notify"
	"github.com/jackspiering/whalenurse/internal/store"
	"github.com/jackspiering/whalenurse/internal/web"
)

// Secrets planted where a leak could come from. None of them may appear in any page, any API response, the event
// stream or /metrics. Every one is a long, unique string, so a plain substring search proves the absence.
const (
	secLabelA   = "lbl-SECRET-apikey-71113"
	secLabelB   = "lbl-SECRET-traefik-71114"
	secEnv      = "env-SECRET-dbpass-71115"
	secUserinfo = "tok-USERINFO-71116"
	secPath     = "tok-PATH-71117"
	secQuery    = "tok-QUERY-71118"
	secPing     = "tok-PINGUUID-71119"
	secHTTP     = "tok-HTTP-BEARER-71120"
	secAttr     = "attr-SECRET-71121"
)

var allSecrets = []string{secLabelA, secLabelB, secEnv, secUserinfo, secPath, secQuery, secPing, secHTTP, secAttr}

// leakyActor fails every restart with an error that quotes a notification URL and the HTTP token, as an HTTP client's
// or a compose child's error text can.
type leakyActor struct{ docker.Actor }

func (leakyActor) Restart(context.Context, string) error {
	return errors.New("restart failed: Post \"generic://u:" + secUserinfo + "@host/hook/" + secPath + "?k=" + secQuery + "\": boom " + secHTTP)
}

// dashboardRig is the whole read path: a daemon on a fake Docker and a fake clock, a real SQLite store behind the
// event log, the API service and the dashboard, started the way main starts them.
type dashboardRig struct {
	h    *harness
	srv  *httptest.Server
	svc  *api.Service
	hub  *api.Hub
	st   *store.Store
	urls []string
}

func newRig(t *testing.T, token string) *dashboardRig {
	t.Helper()
	notifyURL := "generic://u:" + secUserinfo + "@127.0.0.1:9/hook/" + secPath + "?k=" + secQuery
	ping := "http://127.0.0.1:9/ping/" + secPing
	t.Setenv("DB_PASSWORD", secEnv) // an environment variable of the process: nothing may dump it
	red := notify.NewRedactor(notifyURL, ping, token)

	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := api.NewHub()
	rig := &dashboardRig{hub: hub, st: st, urls: []string{notifyURL, ping}}
	rig.h = newHarnessOpts(t, func(c *config.Global) {
		c.HostID = "nas"
		c.HTTPToken, c.NotifyURLs, c.PingURL, c.Metrics = token, []string{notifyURL}, ping, true
	}, func(f *docker.Fake) docker.Actor { return leakyActor{f} }, func(o *Options) {
		o.Persist, o.Meta, o.Version = st, st, "v-test"
		o.Bus.Seed(st.LastID())
		o.Bus.Subscribe(st.RecordEvent)
		o.Bus.Subscribe(hub.Notify)
		o.NotifyStatus = func() notify.Status {
			return notify.Status{Enabled: true, Targets: []notify.TargetStatus{{URL: "#1 generic", Sent: 2, Failed: 1}}}
		}
	})
	rig.h.planted = map[string]bool{"labels": true, "env": true, "password": true}
	rig.svc = NewAPI(rig.h.d, st, st, hub, red.Redact)
	rig.srv = httptest.NewServer(api.Handler(api.Options{Health: rig.h.d, Service: rig.svc, Token: token, Web: web.Handler(rig.svc)}))
	t.Cleanup(rig.srv.Close)
	return rig
}

// drive runs a real incident through the daemon: an unhealthy container whose restart fails with a leaky error, then
// no recreate (no compose), then SUSPENDED. A container with planted labels is not opted in. A sidecar hangs on the
// first. Docker events go through HandleEvent, the way main feeds them.
func (r *dashboardRig) drive() {
	h := r.h
	a := h.running(hexID("a1"), "p", "web")
	h.set(a.ID, func(c *docker.Container) {
		c.Image, c.RestartPolicy = "nginx:1", "unless-stopped"
		c.Labels[config.LabelActions] = "restart,recreate"
		c.Labels["com.example.apikey"] = secLabelA
		c.Labels["traefik.http.routers.x.rule"] = secLabelB
		c.Error = "failed to fulfil mount request: open /srv/data: no such file or directory"
	})
	b := h.running(hexID("b2"), "p", "side")
	h.set(b.ID, func(c *docker.Container) { c.NetworkMode = docker.Ref(a.ID); c.Image = "busybox" })
	o := h.running(hexID("c3"), "other", "thing")
	h.set(o.ID, func(c *docker.Container) {
		delete(c.Labels, config.LabelEnable)
		c.Labels["com.example.apikey"] = secLabelA
		c.Labels["whalenurse.note"] = "visible-whalenurse-label"
	})
	h.tick()
	c := h.set(a.ID, unhealthy)
	h.event(c, "health_status", "unhealthy")
	h.advance(45 * time.Second)
	// A planted attribute on an event that goes through the bus, and the rest of the ladder.
	h.d.Engine().Note(engine.Target{Key: "nas/p/web/1", Name: "p-web-1"}, engine.NeedsRecreate, "note "+secUserinfo,
		map[string]string{"labels": secLabelA, "env": secEnv, "password": secAttr, "reason": "x " + secPath})
	h.advance(10 * time.Minute)
	h.d.Engine().Pause("p/side", time.Hour)
}

func (r *dashboardRig) fetch(t *testing.T, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", r.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// keys are the containers the rig knows, as the dashboard links them.
func (r *dashboardRig) keys() []string {
	var out []string
	for _, c := range r.svc.Containers(true) {
		out = append(out, c.Key)
	}
	return out
}

var inlineScript = regexp.MustCompile(`(?is)<script(?:\s[^>]*)?>`)
var scriptSrc = regexp.MustCompile(`(?is)<script\s[^>]*src=["']/static/app\.js["'][^>]*>\s*</script>`)
var handlerAttr = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
var styleAttr = regexp.MustCompile(`(?i)(\sstyle\s*=|<style[\s>])`)
var jsURL = regexp.MustCompile(`(?i)(href|src|action)\s*=\s*["']\s*javascript:`)

func TestNoSecretReachesAnyPageEndpointOrStream(t *testing.T) {
	r := newRig(t, secHTTP)
	r.drive()
	if len(r.h.rec.Types()) < 6 {
		t.Fatalf("the drive produced too little to prove anything: %v", r.h.rec.Types())
	}

	keys := r.keys()
	if len(keys) != 3 {
		t.Fatalf("keys: %v", keys)
	}
	paths := []string{
		"/api/v1/status", "/api/v1/containers", "/api/v1/containers?all=1", "/api/v1/events?limit=1000", "/api/v1/events?since=0",
		"/api/v1/incidents", "/metrics", "/", "/containers", "/containers?all=1", "/history", "/history?type=ActionResult,Suspended",
		"/history?key=" + url.QueryEscape(keys[0]), "/static/app.js", "/static/app.css",
	}
	for _, k := range keys {
		paths = append(paths, "/api/v1/containers/"+k, "/containers/"+k, "/history?key="+url.QueryEscape(k))
	}
	pages := map[string]string{}
	for _, p := range paths {
		code, body := r.fetch(t, p, secHTTP)
		if code != 200 {
			t.Errorf("GET %s: %d %.200s", p, code, body)
			continue
		}
		pages[p] = body
	}

	// The event stream, from the start: the replay carries every stored event.
	pages["SSE replay"] = r.readStream(t, "0", 400*time.Millisecond)

	for p, body := range pages {
		for _, s := range allSecrets {
			if strings.Contains(body, s) {
				t.Errorf("%s leaks %q", p, s)
			}
		}
		for _, u := range r.urls {
			if strings.Contains(body, u) {
				t.Errorf("%s holds a whole URL", p)
			}
		}
	}

	// The positive controls: the checks above are not vacuous. The secrets were fed through real paths, the redactor
	// masked them, and what is allowed is shown.
	all := strings.Join(mapValues(pages), "\n")
	for _, want := range []string{"p-web-1", "p-side-1", "restart failed", "***", "whalenurse.note", "visible-whalenurse-label", "com.docker.compose.project", "SUSPENDED", "PAUSED", "NeedsRecreate", "failed to fulfil mount request"} {
		if !strings.Contains(all, want) {
			t.Errorf("expected %q somewhere in the output", want)
		}
	}
	if strings.Contains(all, "com.example.apikey") || strings.Contains(all, "traefik.") {
		t.Error("a label outside com.docker.compose.* and whalenurse.* came out")
	}
	// The attribute the engine never emits was dropped; the allowed one ("reason") stayed, redacted.
	_, ev := r.fetch(t, "/api/v1/events?limit=1000", secHTTP)
	if strings.Contains(ev, `"labels"`) || strings.Contains(ev, `"env"`) || !strings.Contains(ev, `"reason": "x ***"`) {
		t.Errorf("event attributes: allow-list not applied:\n%s", ev)
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// readStream connects to the SSE stream with Last-Event-ID and returns what arrived within the time.
func (r *dashboardRig) readStream(t *testing.T, lastID string, wait time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/v1/stream", nil)
	req.Header.Set("Last-Event-ID", lastID)
	req.Header.Set("Authorization", "Bearer "+secHTTP)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		b.WriteString(sc.Text() + "\n")
	}
	return b.String()
}

func TestPagesRenderAndAreCleanHTML(t *testing.T) {
	r := newRig(t, "")
	r.drive()
	paths := []string{"/", "/containers", "/containers?all=1", "/history", "/history?type=Suspended,Unsuspended", "/history?key=" + url.QueryEscape("nas/p/web/1")}
	for _, k := range r.keys() {
		paths = append(paths, "/containers/"+k)
	}
	for _, p := range paths {
		code, body := r.fetch(t, p, "")
		if code != 200 {
			t.Errorf("GET %s: %d %.300s", p, code, body)
			continue
		}
		if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, `<meta name="viewport"`) {
			t.Errorf("%s: not a full page", p)
		}
		// No inline script, handler, style or javascript: URL: the CSP forbids them and the pages must not need them.
		if n := len(inlineScript.FindAllString(body, -1)); n != len(scriptSrc.FindAllString(body, -1)) {
			t.Errorf("%s: inline <script>", p)
		}
		if handlerAttr.MatchString(body) || styleAttr.MatchString(body) || jsURL.MatchString(body) {
			t.Errorf("%s: an inline handler, style or javascript: URL", p)
		}
		for _, bad := range []string{"http://", "https://", "//cdn", "@import"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: references %q: no third-party origin may be needed", p, bad)
			}
		}
	}
	if code, _ := r.fetch(t, "/containers/nas/none/zzz/1", ""); code != 404 {
		t.Errorf("unknown container page: %d", code)
	}
	if code, _ := r.fetch(t, "/nothing", ""); code != 404 {
		t.Errorf("unknown page: %d", code)
	}
	if code, _ := r.fetch(t, "/history?since=x", ""); code != 400 {
		t.Errorf("bad filter: %d", code)
	}

	// The content: the overview, the badges with reason and expiry, the detail page.
	_, overview := r.fetch(t, "/", "")
	for _, want := range []string{"Healer", "Version gate", "Action budget", "Needs attention", "p-web-1"} {
		if !strings.Contains(overview, want) {
			t.Errorf("overview lacks %q", want)
		}
	}
	_, list := r.fetch(t, "/containers", "")
	if !strings.Contains(list, "SUSPENDED: ladder") || strings.Contains(list, "other-thing-1") {
		t.Errorf("containers page:\n%s", list)
	}
	_, detail := r.fetch(t, "/containers/nas/p/web/1", "")
	for _, want := range []string{"Inspect summary", "Drift guard", "Timeline", "unsuspend p/web", "Dependents", "p-side-1", "restart ✓"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	_, side := r.fetch(t, "/containers/nas/p/side/1", "")
	if !strings.Contains(side, "Parent") || !strings.Contains(side, "p-web-1") {
		t.Errorf("sidecar page lacks its parent")
	}
}

func TestDryRunServesTheRingWithNoStore(t *testing.T) {
	h := newHarnessOpts(t, func(c *config.Global) { c.DryRun = true; c.HostID = "nas" }, nil, nil)
	ring := engine.NewRing(0)
	h.d.eng.Pause("x", time.Hour) // an event on the bus is not wired to the ring here; publish by hand below
	svc := NewAPI(h.d, ring, nil, api.NewHub(), nil)
	ring.Record(engine.Event{ID: 1, Time: time.Now(), Host: "nas", Key: "nas/x", Type: engine.Paused, Message: "paused x"})
	srv := httptest.NewServer(api.Handler(api.Options{Health: h.d, Service: svc, Web: web.Handler(svc)}))
	defer srv.Close()
	for _, p := range []string{"/api/v1/status", "/api/v1/events", "/api/v1/incidents", "/", "/history"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
		if p == "/" && !strings.Contains(string(b), "Dry run.") {
			t.Error("no dry-run banner")
		}
		if p == "/api/v1/status" && !strings.Contains(string(b), `"dry_run": true`) || p == "/api/v1/status" && !strings.Contains(string(b), `"persistent": false`) {
			t.Errorf("status: %s", b)
		}
	}
}

// IDs keep counting across a restart of the process: the bus continues after the store's highest ID.
func TestEventIDsContinueAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	run := func() (first, last uint64) {
		st, err := store.Open(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		var bus engine.Bus
		bus.Seed(st.LastID())
		bus.Subscribe(st.RecordEvent)
		first = bus.Publish(engine.Event{Time: time.Now(), Host: "nas", Type: engine.Paused}).ID
		last = bus.Publish(engine.Event{Time: time.Now(), Host: "nas", Type: engine.Resumed}).ID
		return
	}
	f1, l1 := run()
	f2, l2 := run()
	if f1 != 1 || l1 != 2 || f2 != 3 || l2 != 4 {
		t.Fatalf("IDs: %d %d then %d %d", f1, l1, f2, l2)
	}
}

func TestContainerDocsMergeListInspectAndEngine(t *testing.T) {
	r := newRig(t, "")
	r.drive()
	docs := map[string]api.ContainerDoc{}
	for _, c := range r.svc.Containers(true) {
		docs[c.Key] = c
	}
	web, side, other := docs["nas/p/web/1"], docs["nas/p/side/1"], docs["nas/other/thing/1"]
	if side.Healer.Paused == nil || side.Healer.Paused.Scope != "p/side" || side.Healer.Paused.Until.IsZero() {
		t.Errorf("a pause shows on the container it covers, with its expiry: %+v", side.Healer.Paused)
	}
	if !web.Watched || !web.OptedIn || web.Healer.State != "SUSPENDED" || web.Healer.Suspended == nil || web.Healer.Suspended.Reason != "ladder" {
		t.Errorf("web: %+v", web.Healer)
	}
	if web.Healer.RungsRun < 1 || web.Image != "nginx:1" || web.Policy != "unless-stopped" || len(web.Ladder) != 2 || len(web.Dependents) != 1 || web.Dependents[0].Key != "nas/p/side/1" {
		t.Errorf("web: %+v", web)
	}
	if side.Parent == nil || side.Parent.Key != "nas/p/web/1" || !side.Watched {
		t.Errorf("sidecar: parent %+v watched %v", side.Parent, side.Watched)
	}
	if other.Watched || other.Healer.Paused != nil {
		t.Errorf("other: %+v", other)
	}
	if other.Labels["whalenurse.note"] == "" {
		t.Error("whalenurse.* labels are shown")
	}
	st := r.svc.Status()
	if st.Counts.Containers != 3 || st.Counts.Watched != 2 || st.Counts.Suspended != 1 || st.Version != "v-test" || st.Host != "nas" || st.LastEventID == 0 || st.RateLimit.Used == 0 {
		t.Errorf("status: %+v", st)
	}
	if !st.Persistent || st.Notifications.URLs != 1 || st.Notifications.Failed != 1 {
		t.Errorf("status: %+v", st)
	}
	if got := fmt.Sprint(st.VersionGate.RecreateEnabled); got != "false" || st.VersionGate.Reason == "" {
		t.Errorf("no compose: the version gate says %+v", st.VersionGate)
	}
}
