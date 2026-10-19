//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
)

// M4: the pull rung (plan §3.7), against a real compose and a local registry:2.
//
// The registry is a whalenurse-test-registry-* compose project that publishes 127.0.0.1:<port>, and every image the tests
// push or pull lives in 127.0.0.1:<port>/whalenurse-test/ (the Guard refuses anything else). The images are busybox with
// a /version file: the service's healthcheck is `grep -q v2 /version`, so a v1 container is unhealthy however often it is
// restarted or recreated, and only a pulled v2 repairs it. That makes the ladder reach rung 3 and makes "newer image" and
// "no newer image" the only difference between two tests.

type testRegistry struct {
	host string // 127.0.0.1:<port>
	g    *Guard
}

func startRegistry(t *testing.T, g *Guard, cli *docker.Moby) *testRegistry {
	t.Helper()
	ctx := context.Background()
	if err := g.PullBase(ctx, "registry:2"); err != nil {
		t.Fatal(err)
	}
	s := newStack(t, g, cli, "registry", `
services:
  registry:
    image: registry:2
    stop_grace_period: 2s
    ports: ["127.0.0.1::5000"]
`)
	s.up()
	c := s.container("registry")
	port, err := g.PublishedPort(ctx, c.ID, "5000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	r := &testRegistry{host: "127.0.0.1:" + port, g: g}
	waitFor(t, "the registry to answer", 60*time.Second, func() bool {
		resp, err := http.Get("http://" + r.host + "/v2/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	t.Logf("registry at %s", r.host)
	return r
}

func (r *testRegistry) ref(name string) string {
	return r.host + "/whalenurse-test/" + name + ":latest"
}

func versionImage(marker string) string {
	return "FROM busybox:1.37\nRUN echo " + marker + " > /version\n"
}

// publish builds an image that says marker in /version, tags it ref locally and pushes it.
func (r *testRegistry) publish(t *testing.T, ref, marker string) {
	t.Helper()
	ctx := context.Background()
	if err := r.g.Build(ctx, ref, versionImage(marker)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.g.Untag(context.Background(), ref) })
	if err := r.g.Push(ctx, ref); err != nil {
		t.Fatal(err)
	}
}

// publishNewer pushes an image that says marker as ref, and then points the LOCAL ref back at what it was: the registry
// has a newer image than the host, as after someone pushed a release. (Without the re-tag the host would already have it
// and rung 2's drift guard would say "the tag moved" before rung 3 was reached.)
func (r *testRegistry) publishNewer(t *testing.T, ref, marker string) {
	t.Helper()
	ctx := context.Background()
	before, err := r.g.ImageID(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	var b [3]byte
	_, _ = rand.Read(b[:])
	tmp := TagPrefix + "pull-" + hex.EncodeToString(b[:])
	if err := r.g.Build(ctx, tmp, versionImage(marker)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.g.Untag(context.Background(), tmp) })
	if err := r.g.Tag(ctx, tmp, ref); err != nil {
		t.Fatal(err)
	}
	if err := r.g.Push(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := r.g.Tag(ctx, before, ref); err != nil {
		t.Fatal(err)
	}
	after, err := r.g.ImageID(ctx, ref)
	if err != nil || after != before {
		t.Fatalf("the local reference must point at the old image again: %s -> %s (%v)", before, after, err)
	}
}

// pullApp is the service whose healthcheck only a v2 image passes; sidecar adds a busybox container in its network namespace.
func pullApp(ref string, pull, sidecar bool) string {
	label := ""
	if pull {
		label = "\n      whalenurse.heal.pull: \"true\""
	}
	y := fmt.Sprintf(`
services:
  app:
    image: %s
    labels:
      whalenurse.enable: "true"%s
    stop_grace_period: 2s
    command: %s
    healthcheck:
      test: ["CMD", "grep", "-q", "v2", "/version"]
      interval: 1s
      timeout: 1s
      retries: 1
`, ref, label, loopCmd)
	if sidecar {
		y += fmt.Sprintf(`  side:
    image: busybox:1.37
    network_mode: service:app
    stop_grace_period: 2s
    command: %s
`, loopCmd)
	}
	return y
}

func (s *stack) version(service string) string {
	s.t.Helper()
	out, err := s.g.Ctr(context.Background(), "exec", s.container(service).ID, "cat", "/version")
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func pullEvents(rec *engine.Recorder, key string) (started []string, results []engine.Event) {
	for _, e := range rec.Events() {
		if e.Key != key {
			continue
		}
		switch e.Type {
		case engine.ActionStarted:
			started = append(started, e.Attrs["action"])
		case engine.ActionResult:
			results = append(results, e)
		}
	}
	return
}

func imagePulls(t *testing.T, g *Guard, since time.Time) int {
	t.Helper()
	n, err := g.ImagePulls(context.Background(), fmt.Sprint(since.Add(-time.Second).Unix()), fmt.Sprint(time.Now().Unix()+2))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The fixture first: a registry on loopback works on both image stores, a push of a newer image under the same tag does
// not change the local one, and a pull then does (what the pull rung relies on). It also logs the IDs, platform and plain.
func TestLocalRegistryRoundTrip(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("roundtrip")
	reg.publish(t, ref, "v1")
	v1, err := g.ImageID(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	reg.publishNewer(t, ref, "v2")
	if now, _ := g.ImageID(ctx, ref); now != v1 {
		t.Fatalf("the local reference moved by itself: %s -> %s", v1, now)
	}
	since := time.Now()
	if err := g.PullRegistry(ctx, ref); err != nil {
		t.Fatal(err)
	}
	v2, _ := g.ImageID(ctx, ref)
	if v2 == v1 || v2 == "" {
		t.Fatalf("the pull should have brought the newer image: %s -> %s", v1, v2)
	}
	// The same comparison whalenurse makes: with and without a platform.
	arch, err := cli.ServerVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plat, err1 := cli.ImageID(ctx, ref, arch.Arch)
	plain, err2 := cli.ImageID(ctx, ref, "")
	if err1 != nil || err2 != nil || plat == "" || plain == "" {
		t.Fatalf("%v %v", err1, err2)
	}
	t.Logf("v1=%s v2=%s; for v2 the platform id is %s and the plain id is %s", v1, v2, plat, plain)
	// Positive control for every "no image pull happened" assertion in this file: this store does report pulls.
	if n := imagePulls(t, g, since); n < 1 {
		t.Fatalf("a pull happened and docker events reported %d image pull events: the zero-pull assertions would prove nothing", n)
	}
}

// Without whalenurse.heal.pull=true there is never a pull, although the registry has a newer image and the container
// stays unhealthy to the end of the ladder.
func TestPullRungNeverRunsWithoutTheLabel(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("nolabel")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	s := newStack(t, g, cli, "pull-nolabel", pullApp(ref, false, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	localBefore, _ := g.ImageID(ctx, ref)
	since := time.Now()

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the app to be SUSPENDED", 180*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	started, _ := pullEvents(r.Rec, s.key("app"))
	if got := joined(started); got != "restart,recreate" {
		t.Fatalf("actions = %s, want restart,recreate", got)
	}
	if n := len(r.Compose.Pulls()); n != 0 {
		t.Fatalf("compose pull ran %d times without the label: %v", n, r.Compose.Pulls())
	}
	if n := imagePulls(t, g, since); n != 0 {
		t.Fatalf("%d image pull events without the label", n)
	}
	if now, _ := g.ImageID(ctx, ref); now != localBefore {
		t.Fatalf("the local image moved: %s -> %s", localBefore, now)
	}
	if countFor(r.Rec, engine.TagMoved, s.key("app")) != 0 {
		t.Fatal("TagMoved without a pull")
	}
	if v := s.version("app"); v != "v1" {
		t.Fatalf("the container runs %s", v)
	}
}

// No newer image in the registry: restart, recreate, pull (digest unchanged), SUSPENDED at once. Nothing is recreated
// after the pull, nothing is announced as an upgrade, and the one "giving up" message says there was no newer image.
func TestPullRungWithNoNewerImageEndsInSuspended(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("nonewer")
	reg.publish(t, ref, "v1")
	s := newStack(t, g, cli, "pull-nonewer", pullApp(ref, true, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	localBefore, _ := g.ImageID(ctx, ref)
	hook := newWebhook(t, s.project)

	r := startDaemonOpts(t, g, cli, daemonOpts{NotifyURLs: []string{hook.url()}})
	waitFor(t, "the app to be SUSPENDED", 180*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	started, results := pullEvents(r.Rec, s.key("app"))
	if got := joined(started); got != "restart,recreate,pull" {
		t.Fatalf("actions = %s, want restart,recreate,pull", got)
	}
	if pulls := r.Compose.Pulls(); len(pulls) != 1 || !strings.Contains(pulls[0], "pull --policy always app") {
		t.Fatalf("compose pull ran %v", pulls)
	}
	// The drift check came before the pull, and no up followed it.
	log := r.Compose.Log()
	pullAt, lastOracle := -1, -1
	for i, l := range log {
		if strings.Contains(l, " pull --policy ") {
			pullAt = i
		}
		if strings.Contains(l, "--dry-run --progress json up") {
			lastOracle = i
		}
	}
	if pullAt < 0 || lastOracle > pullAt || pullAt != len(log)-1 {
		t.Fatalf("the drift oracle must run before the pull and nothing after it: %v", log)
	}
	var pull engine.Event
	for _, e := range results {
		if e.Attrs["action"] == "pull" {
			pull = e
		}
	}
	if pull.Attrs["outcome"] != "unchanged" || pull.Attrs["ok"] != "true" {
		t.Fatalf("%+v", pull)
	}
	if msg := eventMessage(r.Rec, engine.Suspended, s.key("app")); !strings.Contains(msg, "the pull found no newer image for "+ref) {
		t.Fatalf("the cause must say there was no newer image: %q", msg)
	}
	if countFor(r.Rec, engine.TagMoved, s.key("app")) != 0 {
		t.Fatal("nothing moved")
	}
	if now, _ := g.ImageID(ctx, ref); now != localBefore {
		t.Fatalf("the local image moved: %s -> %s", localBefore, now)
	}
	waitFor(t, "the giving-up message", 30*time.Second, func() bool { return len(hook.matching("giving up, SUSPENDED")) >= 1 })
	giving := hook.matching("giving up, SUSPENDED")
	if len(giving) != 1 || !strings.Contains(giving[0].Message, "no newer image for "+ref) {
		t.Fatalf("one giving-up message that says why: %v", titles(hook.all()))
	}
	if len(hook.matching("upgraded")) != 0 || len(hook.matching("pulled")) != 0 {
		t.Fatalf("no upgrade message: %v", titles(hook.all()))
	}
	time.Sleep(15 * time.Second)
	if n := len(r.Compose.Pulls()); n != 1 {
		t.Fatalf("SUSPENDED means no more pulls: %d", n)
	}
}

// A newer image: restart and recreate do not help, the pull brings v2, a non-forced up replaces the container, the sidecar
// in its network namespace is recreated against the new parent, the heal ends in Recovered, and the one notification
// gives old -> new and a rollback that works when pasted.
func TestPullRungUpgradesRepairsTheSidecarAndNotifiesWithTheRollback(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("upgrade")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	oldID, _ := g.ImageID(ctx, ref)
	s := newStack(t, g, cli, "pull-upgrade", pullApp(ref, true, true))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	sideBefore := s.container("side")
	hook := newWebhook(t, s.project)
	since := time.Now()

	r := startDaemonOpts(t, g, cli, daemonOpts{NotifyURLs: []string{hook.url()}})
	waitFor(t, "the app to be healthy on the new image", 240*time.Second, func() bool {
		return countFor(r.Rec, engine.TagMoved, s.key("app")) == 1 && s.container("app").Health == "healthy"
	})
	waitFor(t, "the Recovered event", 60*time.Second, func() bool { return countFor(r.Rec, engine.Recovered, s.key("app")) == 1 })
	started, results := pullEvents(r.Rec, s.key("app"))
	if got := joined(started); got != "restart,recreate,pull" {
		t.Fatalf("actions = %s, want restart,recreate,pull", got)
	}
	if countFor(r.Rec, engine.Suspended, s.key("app")) != 0 {
		t.Fatal("it recovered; it must not be SUSPENDED")
	}
	// The order of the compose commands: the guard, the pull, then a plain up that cannot pull or build.
	log := r.Compose.Log()
	pullAt := -1
	for i, l := range log {
		if strings.Contains(l, " pull --policy always app") {
			pullAt = i
		}
	}
	if pullAt < 0 || len(r.Compose.Pulls()) != 1 {
		t.Fatalf("one pull expected: %v", log)
	}
	oracleBefore := false
	for _, l := range log[:pullAt] {
		oracleBefore = oracleBefore || strings.Contains(l, "--dry-run --progress json up -d --no-deps --pull never --no-build app")
	}
	if !oracleBefore {
		t.Fatalf("drift check 1 must run before the pull: %v", log)
	}
	up := ""
	for _, l := range log[pullAt+1:] {
		if strings.Contains(l, " up ") && !strings.Contains(l, "--dry-run") && strings.HasSuffix(l, " app") {
			up = l
			break
		}
	}
	if !strings.Contains(up, "up -d --no-deps --pull never --no-build app") || strings.Contains(up, "--force-recreate") {
		t.Fatalf("after the pull: a non-forced up of app, got %q (log %v)", up, log)
	}
	if n := imagePulls(t, g, since); n < 1 {
		t.Fatalf("the pull rung pulled and docker events reported %d image pull events", n)
	}
	if v := s.version("app"); v != "v2" {
		t.Fatalf("the container runs %s, want v2", v)
	}
	newID, _ := g.ImageID(ctx, ref)
	if newID == oldID {
		t.Fatal("the local reference did not move")
	}
	// The sidecar follows the new parent.
	waitFor(t, "the sidecar to be recreated against the new parent", 90*time.Second, func() bool {
		side, app := s.container("side"), s.container("app")
		return side.ID != sideBefore.ID && side.Running && side.NetworkMode == "container:"+app.ID
	})
	for _, c := range r.Compose.Log() {
		if strings.Contains(c, "side") && strings.Contains(c, "--force-recreate") {
			t.Logf("note: the sidecar needed --force-recreate: %s", c)
		}
	}

	// TagMoved says old -> new and carries the rollback.
	var moved engine.Event
	for _, e := range r.Rec.Events() {
		if e.Type == engine.TagMoved {
			moved = e
		}
	}
	a := moved.Attrs
	if a["ref"] != ref || a["ok"] != "true" || a["old"] == "" || a["new"] == "" || a["old"] == a["new"] || a["old_image"] != oldID {
		t.Fatalf("%+v (the old image was %s)", a, oldID)
	}
	var res engine.Event
	for _, e := range results {
		if e.Attrs["action"] == "pull" {
			res = e
		}
	}
	if res.Attrs["outcome"] != "upgraded" {
		t.Fatalf("%+v", res)
	}

	// The notification: one "upgraded during heal", the two digests, the two commands whole.
	waitFor(t, "the upgrade message", 30*time.Second, func() bool { return len(hook.matching("upgraded during heal")) >= 1 })
	msgs := hook.matching("upgraded during heal")
	if len(msgs) != 1 {
		t.Fatalf("one upgrade message: %v", titles(hook.all()))
	}
	body := msgs[0].Message
	short := func(id string) string { return strings.TrimPrefix(id, "sha256:")[:12] }
	for _, want := range []string{s.key("app"), ref, short(a["old"]) + " -> " + short(a["new"]), "roll back:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the message lacks %q:\n%s", want, body)
		}
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	tagLine, upLine := lines[len(lines)-2], lines[len(lines)-1]
	if tagLine != a["rollback_tag"] || upLine != a["rollback_up"] {
		t.Fatalf("the last two lines must be the rollback, whole:\n%s", body)
	}
	if len(hook.matching("pulled")) != 0 {
		t.Fatalf("the pull's own result must not be said twice: %v", titles(hook.all()))
	}

	// And the rollback works as pasted: the old image is back under the tag and the service runs it. The healer is
	// stopped first, or it would heal the v1 container again.
	r.Stop()
	if err := g.RunRollback(ctx, tagLine, upLine); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if now, _ := g.ImageID(ctx, ref); now != oldID {
		t.Fatalf("after the rollback the tag points at %s, want %s", now, oldID)
	}
	waitFor(t, "the service to run v1 again", 60*time.Second, func() bool { return s.version("app") == "v1" })
}

// A drift block on another stack that shares the tag, after the move, names the service that moved it and when.
func TestDriftBlockOnASharedTagNamesWhoMovedIt(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("shared")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	a := newStack(t, g, cli, "pull-a", pullApp(ref, true, false))
	b := newStack(t, g, cli, "pull-b", fmt.Sprintf(`
services:
  api:
    image: %s
    labels:
      whalenurse.enable: "true"
    stop_grace_period: 2s
    command: %s
    healthcheck:
      test: ["CMD", "sh", "-c", "test ! -f /broken"]
      interval: 1s
      timeout: 1s
      retries: 1
`, ref, loopCmd))
	a.up()
	b.up()
	waitFor(t, "a's app to be unhealthy", 30*time.Second, func() bool { return a.container("app").Health == "unhealthy" })
	waitFor(t, "b's api to be healthy", 30*time.Second, func() bool { return b.container("api").Health == "healthy" })
	apiBefore := b.container("api")
	hook := newWebhook(t, b.project)

	r := startDaemonOpts(t, g, cli, daemonOpts{NotifyURLs: []string{hook.url()}})
	waitFor(t, "a's pull to move the tag", 240*time.Second, func() bool { return countFor(r.Rec, engine.TagMoved, a.key("app")) == 1 })
	moved := time.Now()

	// Now b's service goes bad in a way only a recreate fixes: restart, then its recreate meets the moved tag.
	if _, err := g.Ctr(ctx, "exec", apiBefore.ID, "touch", "/broken"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the drift block on b", 180*time.Second, func() bool { return countFor(r.Rec, engine.DriftBlocked, b.key("api")) == 1 })
	var blocked engine.Event
	for _, e := range r.Rec.Events() {
		if e.Type == engine.DriftBlocked && e.Key == b.key("api") {
			blocked = e
		}
	}
	if blocked.Attrs["moved_by"] != a.key("app") || blocked.Attrs["moved_ref"] == "" {
		t.Fatalf("the block must name the container that moved the tag: %+v", blocked.Attrs)
	}
	at, err := time.Parse(time.RFC3339, blocked.Attrs["moved_at"])
	if err != nil || at.After(time.Now().Add(time.Second)) || at.Before(moved.Add(-5*time.Minute)) {
		t.Fatalf("moved_at = %q (%v)", blocked.Attrs["moved_at"], err)
	}
	if !strings.Contains(blocked.Message, "the tag moved") || !strings.Contains(blocked.Message, "whalenurse moved this tag: "+a.key("app")+" pulled it at ") {
		t.Fatalf("%s", blocked.Message)
	}
	if b.container("api").ID != apiBefore.ID {
		t.Fatal("a blocked recreate must leave the container alone")
	}
	waitFor(t, "the block message", 30*time.Second, func() bool { return len(hook.matching("blocked")) >= 1 })
	m := hook.matching("blocked")[0]
	if !strings.Contains(m.Message, "tag moved by whalenurse's pull rung: "+a.project+"/app at ") || !strings.Contains(m.Message, "whalenurse unsuspend "+b.project+"/api") {
		t.Fatalf("%s", m.Message)
	}
	for _, e := range r.Rec.Events() {
		if e.Type == engine.Suspended && e.Key == b.key("api") && e.Attrs["reason"] != engine.ReasonDrift {
			t.Fatalf("%+v", e)
		}
	}
}

// A dry run never pulls: the guard reads, the pull is only logged and notified as "would pull", and the host's image and
// container are untouched.
func TestDryRunNeverPulls(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startRegistry(t, g, cli)
	ref := reg.ref("dry")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	s := newStack(t, g, cli, "pull-dry", pullApp(ref, true, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	before := s.container("app")
	localBefore, _ := g.ImageID(ctx, ref)
	hook := newWebhook(t, s.project)
	since := time.Now()

	r := startDaemonOpts(t, g, cli, daemonOpts{Dry: true, NotifyURLs: []string{hook.url()}})
	waitFor(t, "the dry-run pull", 180*time.Second, func() bool {
		_, results := pullEvents(r.Rec, s.key("app"))
		for _, e := range results {
			if e.Attrs["action"] == "pull" && e.Attrs["dry_run"] == "true" {
				return true
			}
		}
		return false
	})
	time.Sleep(10 * time.Second)
	if n := len(r.Compose.Pulls()); n != 0 {
		t.Fatalf("a dry run ran compose pull: %v", r.Compose.Pulls())
	}
	if n := len(r.Compose.Mutating()); n != 0 {
		t.Fatalf("a dry run ran a mutating compose command: %v", r.Compose.Mutating())
	}
	if n := imagePulls(t, g, since); n != 0 {
		t.Fatalf("%d image pull events in a dry run", n)
	}
	if now, _ := g.ImageID(ctx, ref); now != localBefore {
		t.Fatalf("the local image moved: %s -> %s", localBefore, now)
	}
	if after := s.container("app"); after.ID != before.ID || !after.StartedAt.Equal(before.StartedAt) {
		t.Fatal("a dry run changed the container")
	}
	if countFor(r.Rec, engine.TagMoved, s.key("app")) != 0 {
		t.Fatal("a dry run moves no tag")
	}
	waitFor(t, "the would-pull message", 30*time.Second, func() bool { return len(hook.matching("would pull")) >= 1 })
	for _, m := range hook.all() {
		if !strings.HasPrefix(m.Title, "[dry-run]") {
			t.Errorf("a dry-run message is not marked: %+v", m)
		}
	}
}

// The version gate disables rungs 2 and 3 together: with an Engine below 28.1 the ladder is the restart and then SUSPENDED,
// and compose is never asked to pull.
func TestVersionGateDisablesThePullRung(t *testing.T) {
	g, cli := requireIntegration(t)
	reg := startRegistry(t, g, cli)
	ref := reg.ref("gate")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	s := newStack(t, g, cli, "pull-gate", pullApp(ref, true, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	since := time.Now()

	r := startDaemonOpts(t, g, cli, daemonOpts{Reader: oldEngine{cli}})
	waitFor(t, "the app to be SUSPENDED", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	started, _ := pullEvents(r.Rec, s.key("app"))
	if got := joined(started); got != "restart" {
		t.Fatalf("actions = %s, want restart only", got)
	}
	var rungs []string
	for _, e := range r.Rec.Events() {
		if e.Type == engine.RungUnavailable && e.Key == s.key("app") {
			rungs = append(rungs, e.Attrs["rung"])
		}
	}
	if joined(rungs) != "recreate,pull" {
		t.Fatalf("unavailable rungs = %v", rungs)
	}
	if len(r.Compose.Pulls()) != 0 || len(r.Compose.Mutating()) != 0 {
		t.Fatalf("compose ran below the version gate: %v", r.Compose.Log())
	}
	if n := imagePulls(t, g, since); n != 0 {
		t.Fatalf("%d image pull events below the gate", n)
	}
}
