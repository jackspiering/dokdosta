//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/api"
)

// publishedPort is the host port Docker chose for 127.0.0.1::8484 of the container.
func publishedPort(t *testing.T, id string) string {
	t.Helper()
	out, err := ExecRunner{}.Run(context.Background(), "docker", "port", id, "8484/tcp")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "127.0.0.1:") {
			return strings.TrimPrefix(line, "127.0.0.1:")
		}
	}
	t.Fatalf("no 127.0.0.1 binding in %q: the dashboard must only be published on loopback", out)
	return ""
}

func dashboardImage(t *testing.T) (string, uint32) {
	t.Helper()
	img := os.Getenv("DOKDOSTA_TEST_IMAGE")
	if img == "" {
		t.Skip("set DOKDOSTA_TEST_IMAGE to a locally built image to test it")
	}
	if !strings.HasPrefix(img, "dokdosta-test/") {
		t.Fatalf("image %q: only dokdosta-test/* local tags are used", img)
	}
	fi, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	return img, fi.Sys().(*syscall.Stat_t).Gid
}

func dashboardCompose(img string, gid uint32, token string, withApp bool) string {
	env := ""
	if token != "" {
		env = fmt.Sprintf("      DOKDOSTA_HTTP_TOKEN: %s\n", token)
	}
	svc := fmt.Sprintf(`
services:
  dokdosta:
    image: %s
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    group_add: ["%d"]
    tmpfs: ["/run:uid=65532,gid=65532,mode=0700"]
    ports: ["127.0.0.1::8484"]
    volumes: ["/var/run/docker.sock:/var/run/docker.sock", "data:/data"]
    environment:
      DOKDOSTA_HOST_ID: %s
      DOKDOSTA_HTTP_ADDR: ":8484"
      DOKDOSTA_METRICS: "true"
      DOKDOSTA_GRACE: 2s
      DOKDOSTA_POLL_INTERVAL: 5s
      DOKDOSTA_STARTUP_SETTLE: 0s
%s`, img, gid, testHost, env)
	if withApp {
		svc += appService("app")
	}
	return svc + "\nvolumes:\n  data: {}\n"
}

func httpDo(t *testing.T, method, url string, hdr ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1]
			continue
		}
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, resp.Header, string(b)
}

// M5: the image with the dashboard on, published on loopback only. /api/v1/status is refused without the token and
// served with it (as a bearer token, and as the password of HTTP Basic, which is how a browser sends it); the
// dashboard, /metrics and the event stream sit behind the same check; /healthz needs none; nothing but GET answers.
func TestImageServesTheDashboardBehindTheToken(t *testing.T) {
	g, cli := requireIntegration(t)
	img, gid := dashboardImage(t)
	const token = "itest-token-9f2c61d84e7b4a35"
	s := newStack(t, g, cli, "dash-token", dashboardCompose(img, gid, token, true))
	s.up("app")
	waitFor(t, "app to be healthy", 60*time.Second, func() bool { return s.container("app").Health == "healthy" })
	s.up("dokdosta")
	waitFor(t, "the image HEALTHCHECK to pass", 90*time.Second, func() bool { return s.container("dokdosta").Health == "healthy" })
	base := "http://127.0.0.1:" + publishedPort(t, s.container("dokdosta").ID)

	if code, hdr, body := httpDo(t, "GET", base+"/api/v1/status"); code != 401 || strings.Contains(body, token) || len(hdr.Values("WWW-Authenticate")) == 0 {
		t.Fatalf("without a token: %d %v %s", code, hdr, body)
	}
	if code, _, _ := httpDo(t, "GET", base+"/api/v1/status", "Authorization", "Bearer "+token+"x"); code != 401 {
		t.Fatalf("a wrong token: %d", code)
	}
	if code, _, _ := httpDo(t, "GET", base+"/api/v1/status?token="+token); code != 401 {
		t.Fatalf("a token in the query must not count: %d", code)
	}
	code, hdr, body := httpDo(t, "GET", base+"/api/v1/status", "Authorization", "Bearer "+token)
	var st api.StatusDoc
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil || st.Host != testHost || st.API != "v1" || !st.Persistent || st.DryRun {
		t.Fatalf("with the token: %d %s", code, body)
	}
	if hdr.Get("Content-Security-Policy") == "" || hdr.Get("X-Frame-Options") != "DENY" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers: %v", hdr)
	}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("me:"+token))
	if code, _, body := httpDo(t, "GET", base+"/", "Authorization", basic); code != 200 || !strings.Contains(body, "dokdosta") || !strings.Contains(body, "Overview") {
		t.Fatalf("the dashboard with Basic: %d", code)
	}
	if code, _, _ := httpDo(t, "GET", base+"/"); code != 401 {
		t.Fatalf("the dashboard without a token: %d", code)
	}
	if code, _, body := httpDo(t, "GET", base+"/healthz"); code != 200 || strings.TrimSpace(body) != "ok" {
		t.Fatalf("/healthz needs no token and says only ok: %d %q", code, body)
	}
	if code, _, body := httpDo(t, "GET", base+"/metrics", "Authorization", "Bearer "+token); code != 200 || !strings.Contains(body, "dokdosta_healthy 1") {
		t.Fatalf("/metrics: %d", code)
	}
	if code, _, _ := httpDo(t, "GET", base+"/metrics"); code != 401 {
		t.Fatalf("/metrics without a token: %d", code)
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		for _, p := range []string{"/api/v1/status", "/v1/pause", "/v1/unsuspend", "/"} {
			if code, _, _ := httpDo(t, m, base+p, "Authorization", "Bearer "+token); code != 405 {
				t.Errorf("%s %s: %d, want 405", m, p, code)
			}
		}
	}

	// The opted-in container is listed, with the compose labels and nothing else of its configuration.
	key := testHost + "/" + s.project + "/app/1"
	waitFor(t, "the app in /api/v1/containers", 30*time.Second, func() bool {
		_, _, b := httpDo(t, "GET", base+"/api/v1/containers", "Authorization", "Bearer "+token)
		return strings.Contains(b, key)
	})
	code, _, body = httpDo(t, "GET", base+"/api/v1/containers/"+key, "Authorization", "Bearer "+token)
	var c api.ContainerDetailDoc
	if code != 200 || json.Unmarshal([]byte(body), &c) != nil || !c.Watched || c.Host != testHost || c.Image != "busybox:1.37" {
		t.Fatalf("detail: %d %s", code, body)
	}
	for k := range c.Labels {
		if !strings.HasPrefix(k, "com.docker.compose.") && !strings.HasPrefix(k, "dokdosta.") {
			t.Errorf("label %q is shown", k)
		}
	}
	if code, _, body := httpDo(t, "GET", base+"/containers/"+key, "Authorization", "Bearer "+token); code != 200 || !strings.Contains(body, "Timeline") {
		t.Fatalf("detail page: %d", code)
	}
	if _, _, body := httpDo(t, "GET", base+"/api/v1/events?limit=1000", "Authorization", "Bearer "+token); strings.Contains(body, token) {
		t.Fatal("the token is in the event history")
	}

	// Last, because it blocks this client: 10 wrong tokens within a minute earn a 429 with a Retry-After, and while the
	// block lasts the right token is refused too. /healthz is exempt.
	var last int
	var retry string
	for i := 0; i < 10; i++ {
		var hdr http.Header
		last, hdr, _ = httpDo(t, "GET", base+"/api/v1/status", "Authorization", fmt.Sprintf("Bearer wrong-%d", i))
		retry = hdr.Get("Retry-After")
	}
	if last != 429 || retry == "" {
		t.Fatalf("the tenth wrong token: %d retry %q", last, retry)
	}
	if code, _, _ := httpDo(t, "GET", base+"/api/v1/status", "Authorization", "Bearer "+token); code != 429 {
		t.Fatalf("the right token while blocked: %d", code)
	}
	if code, _, _ := httpDo(t, "GET", base+"/healthz"); code != 200 {
		t.Fatalf("/healthz while blocked: %d", code)
	}
}

// Without a token the API and the dashboard answer anyone who can reach the port, and are still read-only. The
// listener is published on 127.0.0.1 only.
func TestImageServesTheDashboardWithoutATokenOnLoopback(t *testing.T) {
	g, cli := requireIntegration(t)
	img, gid := dashboardImage(t)
	s := newStack(t, g, cli, "dash-open", dashboardCompose(img, gid, "", false))
	s.up("dokdosta")
	waitFor(t, "the image HEALTHCHECK to pass", 90*time.Second, func() bool { return s.container("dokdosta").Health == "healthy" })
	base := "http://127.0.0.1:" + publishedPort(t, s.container("dokdosta").ID)
	code, _, body := httpDo(t, "GET", base+"/api/v1/status")
	var st api.StatusDoc
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil || st.Host != testHost {
		t.Fatalf("%d %s", code, body)
	}
	if code, _, _ := httpDo(t, "POST", base+"/api/v1/status"); code != 405 {
		t.Fatalf("POST: %d", code)
	}
	// DNS rebinding: a browser sends the name it was given, and without a token only loopback names are served.
	if code, _, _ := httpDo(t, "GET", base+"/api/v1/status", "Host", "localhost:8484"); code != 200 {
		t.Fatalf("Host localhost: %d", code)
	}
	if code, _, body := httpDo(t, "GET", base+"/api/v1/status", "Host", "rebind.attacker.example:8484"); code != 403 || strings.Contains(body, "attacker") {
		t.Fatalf("Host rebind.attacker.example: %d %s", code, body)
	}
	if code, _, _ := httpDo(t, "GET", base+"/healthz", "Host", "rebind.attacker.example"); code != 200 {
		t.Fatalf("/healthz by any name: %d", code)
	}
}
