package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/engine"
)

// ---- /healthz says ok or not ok ---------------------------------------------------------------------------

type flipChecker struct {
	ok     bool
	detail string
}

func (f *flipChecker) Healthy() (bool, string) { return f.ok, f.detail }

func TestPublicHealthzSaysOnlyOkOrNotOk(t *testing.T) {
	chk := &flipChecker{ok: true, detail: "ok (an action is running)"}
	h := Handler(Options{Health: chk, Token: "tok-secret-value-0123"})
	rec := get(t, h, "/healthz")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	chk.ok, chk.detail = false, "docker events stream down for 5m0s; main loop silent for 9m"
	rec = get(t, h, "/healthz")
	if rec.Code != 503 || strings.TrimSpace(rec.Body.String()) != "not ok" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	// The unix socket keeps the reason: it is for the person who can exec into the container.
	rec = httptest.NewRecorder()
	HealthHandler(chk).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if !strings.Contains(rec.Body.String(), "docker events stream down") {
		t.Fatalf("the socket's /healthz lost its detail: %q", rec.Body.String())
	}
}

// ---- Host header ------------------------------------------------------------------------------------------

func TestHostAllowed(t *testing.T) {
	allowed := NormalizeHosts([]string{"NAS.example.org:8484", "[fd00::5]", "media.lan.", " "})
	for host, want := range map[string]bool{
		"localhost": true, "localhost:8484": true, "LOCALHOST:1": true, "127.0.0.1": true, "127.0.0.1:8484": true,
		"[::1]": true, "[::1]:8484": true, "localhost.": true, "": true,
		"nas.example.org": true, "nas.example.org:9999": true, "NAS.EXAMPLE.ORG": true, "[fd00::5]:8484": true, "media.lan": true,
		"evil.example": false, "localhost.evil.example": false, "127.0.0.1.evil.example": false, "127.0.0.2": false,
		"192.168.1.5:8484": false, "[::2]:8484": false, "nas.example.org.evil.example": false, "xlocalhost": false,
	} {
		if got := HostAllowed(host, allowed); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	if len(allowed) != 3 {
		t.Errorf("normalized: %v", allowed)
	}
}

func TestWithoutATokenOnlyLoopbackAndListedHostsAreServed(t *testing.T) {
	h := Handler(Options{Health: okChecker{}, Service: fixtureService(), AllowedHosts: []string{"nas.example.org"}})
	for _, c := range []struct {
		host, path string
		want       int
	}{
		{"127.0.0.1:8484", "/api/v1/status", 200}, {"localhost:8484", "/api/v1/status", 200}, {"[::1]:8484", "/api/v1/status", 200},
		{"nas.example.org", "/api/v1/status", 200}, {"nas.example.org:443", "/api/v1/containers", 200},
		{"rebind.attacker.example", "/api/v1/status", 403}, {"rebind.attacker.example:8484", "/api/v1/containers", 403},
		{"192.168.1.5:8484", "/api/v1/events", 403}, {"rebind.attacker.example", "/api/v1/stream", 403},
		{"rebind.attacker.example", "/metrics", 403}, {"rebind.attacker.example", "/api/v1/containers/nas/media/vpn/1", 403},
		// /healthz says only ok or not ok and is what a proxy or monitor probes, by whatever name it uses.
		{"rebind.attacker.example", "/healthz", 200},
	} {
		rec := get(t, h, c.path, "Host", c.host)
		if rec.Code != c.want {
			t.Errorf("Host %s GET %s: %d, want %d", c.host, c.path, rec.Code, c.want)
		}
		if rec.Code == 403 && (strings.Contains(rec.Body.String(), "attacker") || rec.Header().Get("Content-Security-Policy") == "") {
			t.Errorf("the refusal echoes the host or lacks the headers: %q", rec.Body.String())
		}
	}
}

func TestWithATokenTheHostIsNotChecked(t *testing.T) {
	h := Handler(Options{Health: okChecker{}, Service: fixtureService(), Token: "a-long-random-token-0123"})
	rec := get(t, h, "/api/v1/status", "Host", "nas.example.org", "Authorization", "Bearer a-long-random-token-0123")
	if rec.Code != 200 {
		t.Fatalf("a proxy forwards its own name: %d", rec.Code)
	}
}

// ---- wrong-token limiter ----------------------------------------------------------------------------------

type clockT struct{ t time.Time }

func (c *clockT) now() time.Time          { return c.t }
func (c *clockT) advance(d time.Duration) { c.t = c.t.Add(d) }

const limTok = "a-long-random-token-0123"

func limited(clk *clockT, blocks *[]string) http.Handler {
	return Handler(Options{Health: okChecker{}, Service: fixtureService(), Token: limTok, Now: clk.now,
		OnBlock: func(client string, d time.Duration) { *blocks = append(*blocks, client+" "+d.String()) }})
}

func from(t *testing.T, h http.Handler, ip, auth string, path ...string) *httptest.ResponseRecorder {
	t.Helper()
	p := "/api/v1/status"
	if len(path) > 0 {
		p = path[0]
	}
	req := httptest.NewRequest("GET", p, nil)
	req.RemoteAddr = ip
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTenWrongTokensAMinuteThenBlockedWithBackoff(t *testing.T) {
	clk := &clockT{t: t0}
	var blocks []string
	h := limited(clk, &blocks)
	const ip = "203.0.113.7:50000"

	for i := 1; i <= 9; i++ {
		if rec := from(t, h, ip, "Bearer nope-"+string(rune('a'+i))); rec.Code != 401 {
			t.Fatalf("wrong token %d: %d", i, rec.Code)
		}
	}
	rec := from(t, h, ip, "Bearer nope-tenth")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "60" || len(blocks) != 1 {
		t.Fatalf("the tenth: %d retry %q blocks %v", rec.Code, rec.Header().Get("Retry-After"), blocks)
	}
	// Blocked: even the right token is refused, or the block would only slow the guessing down. /healthz is exempt.
	clk.advance(30 * time.Second)
	if rec := from(t, h, ip, "Bearer "+limTok); rec.Code != 429 || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("blocked, right token: %d retry %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := from(t, h, ip, "", "/"); rec.Code != 429 {
		t.Fatalf("blocked, no credentials: %d", rec.Code)
	}
	if rec := from(t, h, ip, "", "/healthz"); rec.Code != 200 {
		t.Fatalf("/healthz while blocked: %d", rec.Code)
	}
	// Another address is unaffected, and so is the same host after the block.
	if rec := from(t, h, "198.51.100.9:1", "Bearer "+limTok); rec.Code != 200 {
		t.Fatalf("another client: %d", rec.Code)
	}
	clk.advance(31 * time.Second)
	if rec := from(t, h, ip, "Bearer "+limTok); rec.Code != 200 {
		t.Fatalf("after the block: %d", rec.Code)
	}

	// The second block of the same address is twice as long, the third four times, up to 15 minutes.
	want := []string{"120", "240", "480", "900", "900"}
	for n, w := range want {
		for i := 0; i < AttemptLimit; i++ {
			rec = from(t, h, ip, "Bearer wrong")
		}
		if rec.Code != 429 || rec.Header().Get("Retry-After") != w {
			t.Fatalf("block %d: %d retry %q, want %s", n+2, rec.Code, rec.Header().Get("Retry-After"), w)
		}
		clk.advance(time.Duration(atoi(t, w))*time.Second + time.Second)
	}
	if len(blocks) != 1+len(want) || !strings.HasPrefix(blocks[0], "203.0.113.7 ") {
		t.Fatalf("OnBlock: %v", blocks)
	}
	// An hour without a failure forgets the strikes.
	clk.advance(2 * time.Hour)
	for i := 0; i < AttemptLimit; i++ {
		rec = from(t, h, ip, "Bearer wrong")
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("after an hour of quiet the block is the first again: %q", rec.Header().Get("Retry-After"))
	}
}

func atoi(t *testing.T, s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func TestFailuresMustFallWithinAMinute(t *testing.T) {
	clk := &clockT{t: t0}
	h := limited(clk, new([]string))
	const ip = "203.0.113.7:1"
	for i := 0; i < 40; i++ { // 40 wrong tokens, one every 7 seconds: never 10 within a minute
		if rec := from(t, h, ip, "Bearer wrong"); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
		clk.advance(7 * time.Second)
	}
}

func TestRequestsWithoutCredentialsAreNotGuessesAndRightTokensDoNotResetTheCount(t *testing.T) {
	clk := &clockT{t: t0}
	h := limited(clk, new([]string))
	const ip = "203.0.113.7:1"
	for i := 0; i < 50; i++ { // a browser's first request, before its password prompt
		if rec := from(t, h, ip, ""); rec.Code != 401 {
			t.Fatalf("no credentials: %d", rec.Code)
		}
	}
	for i := 0; i < 9; i++ {
		from(t, h, ip, "Bearer wrong")
		if rec := from(t, h, ip, "Bearer "+limTok); rec.Code != 200 {
			t.Fatalf("right token: %d", rec.Code)
		}
	}
	if rec := from(t, h, ip, "Bearer wrong"); rec.Code != 429 {
		t.Fatalf("9 + 1 wrong tokens in a minute, with right ones between: %d", rec.Code)
	}
}

func TestTheLimiterKeysOnThePeerAddressNotOnForwardingHeaders(t *testing.T) {
	clk := &clockT{t: t0}
	h := limited(clk, new([]string))
	for i := 0; i < AttemptLimit; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		req.RemoteAddr = "203.0.113.7:1"
		req.Header.Set("Authorization", "Bearer wrong")
		req.Header.Set("X-Forwarded-For", "10.0.0."+string(rune('1'+i)))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if rec := from(t, h, "203.0.113.7:9999", "Bearer "+limTok); rec.Code != 429 {
		t.Fatalf("a spoofed X-Forwarded-For escaped the limit: %d", rec.Code)
	}
}

func TestIPv6ClientsShareTheirSlash64AndTheTableIsBounded(t *testing.T) {
	clk := &clockT{t: t0}
	h := limited(clk, new([]string))
	for i := 0; i < AttemptLimit; i++ {
		from(t, h, fmt.Sprintf("[2001:db8:1:2:%x::1]:1", i+1), "Bearer wrong")
	}
	if rec := from(t, h, "[2001:db8:1:2:ffff::9]:1", "Bearer "+limTok); rec.Code != 429 {
		t.Fatalf("one /64 rotating its addresses: %d", rec.Code)
	}
	if rec := from(t, h, "[2001:db8:1:3::1]:1", "Bearer "+limTok); rec.Code != 200 {
		t.Fatalf("the next /64: %d", rec.Code)
	}

	l := newAttemptLimiter(clk.now)
	for i := 0; i < 3*maxTracked; i++ {
		l.fail("198.51.100." + string(rune('0'+i%10)) + "/" + string(rune(i)))
	}
	if len(l.ips) > maxTracked {
		t.Fatalf("the table grew to %d", len(l.ips))
	}
}

// ---- skipped rungs ----------------------------------------------------------------------------------------

type oneSource struct{ c ContainerDoc }

func (o oneSource) Status() StatusDoc          { return StatusDoc{Host: "nas"} }
func (o oneSource) Containers() []ContainerDoc { return []ContainerDoc{o.c} }

func skippedFixture() *Service {
	opened := t0.Add(20 * time.Minute)
	c := ContainerDoc{
		Host: "nas", Key: "nas/media/vpn/1", Name: "media-vpn-1", State: "running", Health: "unhealthy", Watched: true, OptedIn: true,
		Ladder: []string{"restart", "recreate", "pull"},
		Healer: HealerDoc{State: "SUSPENDED", Class: "unhealthy", IncidentOpened: opened, RungsRun: 3, FlapLimit: 3, Suspended: &SuspendDoc{Reason: "ladder", Since: opened.Add(time.Minute)}},
	}
	r := engineRing()
	inc := opened.Format(time.RFC3339Nano)
	old := t0.Add(-time.Hour).Format(time.RFC3339Nano)
	rec := func(id uint64, typ, msg string, attrs map[string]string) {
		r.Record(engineEvent(id, typ, msg, attrs))
	}
	rec(1, "RungUnavailable", `rung "recreate": the compose rungs are disabled: Docker Engine 27.5 is older than 28.1.0; skipped`, map[string]string{"rung": "recreate", "incident": old})
	rec(2, "RungUnavailable", `rung "recreate": the compose rungs are disabled: Docker Engine 27.5 is older than 28.1.0; skipped`, map[string]string{"rung": "recreate", "incident": inc})
	rec(3, "RungUnavailable", `rung "pull": the compose rungs are disabled: Docker Engine 27.5 is older than 28.1.0; skipped`, map[string]string{"rung": "pull", "incident": inc})
	rec(4, "RungUnavailable", `duplicate`, map[string]string{"rung": "pull", "incident": inc})
	return &Service{Host: "nas", Source: oneSource{c}, Events: r}
}

func TestSkippedRungsComeFromTheOpenIncidentsEventsOnly(t *testing.T) {
	s := skippedFixture()
	list := s.Containers(false)
	got := list[0].Healer.SkippedRungs
	if len(got) != 2 || got[0].Rung != "recreate" || got[1].Rung != "pull" || !strings.Contains(got[0].Reason, "Engine 27.5") {
		t.Fatalf("list: %+v", got)
	}
	d, _ := s.Container("nas/media/vpn/1")
	if len(d.Healer.SkippedRungs) != 2 {
		t.Fatalf("detail: %+v", d.Healer.SkippedRungs)
	}
	body := get(t, Handler(Options{Service: s}), "/api/v1/containers/nas/media/vpn/1").Body.Bytes()
	golden(t, "container-skipped.json", body)
}

// ---- names and noise ---------------------------------------------------------------------------------------

func TestFinalNameDropsComposesTemporaryPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"9d766305bcec_dokdosta-test-demo-worker-1": "dokdosta-test-demo-worker-1",
		"media-vpn-1": "media-vpn-1", "abc_def": "abc_def", "9D766305BCEC_x": "9D766305BCEC_x", "0123456789ab_": "0123456789ab_", "": "",
	} {
		if got := finalName(in); got != want {
			t.Errorf("finalName(%q) = %q, want %q", in, got, want)
		}
	}
	s := skippedFixture()
	if e := s.EventDoc(engineEvent(1, "ActionResult", "m", nil)); e.Container != "media-vpn-1" {
		t.Fatal(e.Container)
	}
	e := engineEvent(2, "ActionResult", "m", nil)
	e.Container = "0123456789ab_media-vpn-1"
	if got := s.EventDoc(e).Container; got != "media-vpn-1" {
		t.Errorf("event: %q", got)
	}
	if got := s.clean(ContainerDoc{Name: "0123456789ab_media-vpn-1"}).Name; got != "media-vpn-1" {
		t.Errorf("container: %q", got)
	}
}

func TestNoiseIsHiddenOnRequestAndTheJSONAPIKeepsIt(t *testing.T) {
	s := skippedFixture()
	r := s.Events.(*engine.Ring)
	r.Record(engineEvent(10, "EventAttributed", "docker event during a dokdosta action", map[string]string{"action": "die"}))
	r.Record(engineEvent(11, "ActionResult", "restart ok", nil))
	r.Record(engineEvent(12, "EventAttributed", "docker event during a dokdosta action", nil))
	all := s.EventsPage(EventsQuery{Newest: true, Limit: 100})
	hidden := s.EventsPage(EventsQuery{Newest: true, Limit: 100, HideNoise: true})
	count := func(d EventsDoc, typ string) (n int) {
		for _, e := range d.Events {
			if e.Type == typ {
				n++
			}
		}
		return
	}
	if count(all, "EventAttributed") != 2 || count(hidden, "EventAttributed") != 0 || count(hidden, "ActionResult") != 1 {
		t.Fatalf("all %d hidden %d", count(all, "EventAttributed"), count(hidden, "EventAttributed"))
	}
	// The timeline: the JSON detail keeps everything, the dashboard's variant leaves the noise out, and the 100 slots
	// are not spent on it.
	full, _ := s.Container("nas/media/vpn/1")
	quiet, _ := s.ContainerOpts("nas/media/vpn/1", false)
	has := func(tl []EventDoc) bool {
		for _, e := range tl {
			if e.Type == "EventAttributed" {
				return true
			}
		}
		return false
	}
	if !has(full.Timeline) || has(quiet.Timeline) {
		t.Fatalf("timelines: full %v quiet %v", has(full.Timeline), has(quiet.Timeline))
	}
	if body := get(t, Handler(Options{Service: s}), "/api/v1/events").Body.String(); !strings.Contains(body, "EventAttributed") {
		t.Error("the JSON events endpoint must stay complete (a hub follows the cursor)")
	}
}

func TestRelationDocCarriesTheOtherEndsState(t *testing.T) {
	c := ContainerDoc{Parent: &RelationDoc{Host: "nas", Key: "k", Name: "n", State: "running", Health: "unhealthy", HealerState: "SUSPENDED"}}
	b, _ := jsonMarshal(c.Parent)
	if !strings.Contains(string(b), `"healer_state":"SUSPENDED"`) || !strings.Contains(string(b), `"state":"running"`) {
		t.Fatalf("%s", b)
	}
}

// A client that stops reading must not hold a stream slot for ever. The socket buffers fill, the write blocks, and only
// the write deadline ends it (the server has no WriteTimeout: it would end every stream). The events are recorded before
// the connection, and Last-Event-ID: 0 makes the stream send them all from the start, so the first write is the one that
// blocks.
func TestStreamReturnsWhenTheClientNeverReads(t *testing.T) {
	old := streamWriteTimeout
	streamWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { streamWriteTimeout = old })
	svc := fixtureService()
	t.Cleanup(svc.Hub.Close)
	ring := engine.NewRing(20000)
	svc.Events = ring
	big := strings.Repeat("x", 2000)
	for i := 1; i <= 20000; i++ {
		ring.Record(engine.Event{ID: uint64(i), Time: t0, Host: "nas", Key: "nas/web/proxy/1", Type: engine.Recovered, Message: big})
	}
	srv := httptest.NewServer(Handler(Options{Health: okChecker{}, Service: svc}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "GET /api/v1/stream HTTP/1.1\r\nHost: 127.0.0.1\r\nLast-Event-ID: 0\r\n\r\n")
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: clients %d", what, svc.Hub.Clients())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("the stream did not start", func() bool { return svc.Hub.Clients() == 1 })
	waitFor("the stream held its slot after the client stopped reading", func() bool { return svc.Hub.Clients() == 0 })
}
