//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/daemon"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
)

// M2b integration tests: rung 2 and the dependents step through a real compose, in whalenurse-test-* projects only. CI runs
// them against compose v5.2.0, v5.6.0 and the latest release, on both image stores.

const loopCmd = `["sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1; done"]`

// brokenService is healthy until /broken exists. /broken lives in the container's writable layer, so a restart does not
// repair it and a recreate does: it needs rung 2.
func brokenService(name, extra string) string {
	return fmt.Sprintf(`
  %s:
    image: busybox:1.37
    labels:
      whalenurse.enable: "true"
    stop_grace_period: 2s
    command: %s
    healthcheck:
      test: ["CMD", "sh", "-c", "test ! -f /broken"]
      interval: 1s
      timeout: 1s
      retries: 1
%s`, name, loopCmd, extra)
}

func breakWritableLayer(t *testing.T, g *Guard, s *stack, service string) {
	t.Helper()
	if _, err := g.Ctr(context.Background(), "exec", s.container(service).ID, "touch", "/broken"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, service+" to turn unhealthy", 30*time.Second, func() bool { return s.container(service).Health == "unhealthy" })
}

func actionsFor(rec *engine.Recorder, key string) []string {
	var out []string
	for _, e := range rec.Events() {
		if e.Type == engine.ActionStarted && e.Key == key {
			out = append(out, e.Attrs["action"])
		}
	}
	return out
}

func joined(l []string) string { return strings.Join(l, ",") }

// unhealthy -> restart -> recreate -> SUSPENDED, when neither helps.
func TestLadderRestartRecreateSuspended(t *testing.T) {
	g, cli := requireIntegration(t)
	s := stuckStack(t, g, cli, "ladder")
	before := s.container("app")

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the app to be SUSPENDED", 150*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	if got := joined(actionsFor(r.Rec, s.key("app"))); got != "restart,recreate" {
		t.Fatalf("actions = %s, want restart,recreate", got)
	}
	after := s.container("app")
	if after.ID == before.ID {
		t.Fatal("rung 2 should have recreated the container (new ID)")
	}
	if n := countFor(r.Rec, engine.RungUnavailable, s.key("app")); n != 0 {
		t.Fatalf("no rung is unavailable on this host: %d", n)
	}
	for _, e := range r.Rec.Events() {
		if e.Type == engine.Suspended && e.Attrs["reason"] != engine.ReasonLadder {
			t.Fatalf("%+v", e)
		}
	}
	// The recreate ran the guard first and then exactly one forced up, with the flags that make a heal safe.
	var ups []string
	for _, l := range r.Compose.Mutating() {
		ups = append(ups, l)
	}
	if len(ups) != 1 || !strings.Contains(ups[0], "up -d --force-recreate --no-deps --pull never --no-build app") {
		t.Fatalf("mutating compose commands: %v", ups)
	}
	if !strings.Contains(strings.Join(r.Compose.Log(), "\n"), "--dry-run --progress json up -d --no-deps --pull never --no-build app") {
		t.Fatalf("the dry-run oracle did not run before the recreate: %v", r.Compose.Log())
	}
	time.Sleep(20 * time.Second) // several polls: SUSPENDED means no more attempts
	if again := s.container("app"); again.ID != after.ID || len(r.Compose.Mutating()) != 1 {
		t.Fatalf("a SUSPENDED container was touched again: %s -> %s", after.ID, again.ID)
	}
}

// A restart does not repair it, a recreate does: restart, then recreate, then recovered and no SUSPENDED.
func TestRecreateRepairsWhatARestartCannot(t *testing.T) {
	g, cli := requireIntegration(t)
	s := newStack(t, g, cli, "repair", "services:"+brokenService("app", ""))
	s.up()
	waitFor(t, "app to be healthy", 60*time.Second, func() bool { return s.container("app").Health == "healthy" })
	before := s.container("app")
	breakWritableLayer(t, g, s, "app")

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the app to be recreated and healthy", 150*time.Second, func() bool {
		c := s.container("app")
		return c.ID != before.ID && c.Health == "healthy"
	})
	waitFor(t, "the Recovered event", 60*time.Second, func() bool { return countFor(r.Rec, engine.Recovered, s.key("app")) == 1 })
	if got := joined(actionsFor(r.Rec, s.key("app"))); got != "restart,recreate" {
		t.Fatalf("actions = %s", got)
	}
	if countFor(r.Rec, engine.Suspended, s.key("app")) != 0 {
		t.Fatal("it recovered; it must not be SUSPENDED")
	}
}

// Heal never pulls (plan §3.1): a service with pull_policy: always, whose image exists only locally, is recreated by the
// ladder, so compose did not try to pull it; and no image pull event happened.
func TestHealNeverPulls(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	tag := TagPrefix + "pullprobe:1"
	if err := g.Tag(ctx, "busybox:1.37", tag); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Untag(context.Background(), tag) })
	s := newStack(t, g, cli, "nopull", `
services:
  app:
    image: `+tag+`
    pull_policy: always
    labels:
      whalenurse.enable: "true"
    stop_grace_period: 2s
    command: `+loopCmd+`
    healthcheck:
      test: ["CMD", "false"]
      interval: 1s
      timeout: 1s
      retries: 1
`)
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	before := s.container("app")
	since := fmt.Sprint(time.Now().Unix())

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the app to be SUSPENDED", 150*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	if s.container("app").ID == before.ID {
		t.Fatal("the recreate did not happen: with pull_policy: always and no registry that means compose tried to pull")
	}
	if pulls, err := g.ImagePulls(ctx, since, fmt.Sprint(time.Now().Unix()+1)); err != nil || pulls != 0 {
		t.Fatalf("image pull events during the heal: %d (%v)", pulls, err)
	}
	for _, l := range r.Compose.Log() {
		if strings.Contains(l, " up ") && !strings.Contains(l, "--pull never") {
			t.Fatalf("an up without --pull never: %s", l)
		}
	}
}

// editCompose changes the compose file of a running stack the way a person would: the next recreate would deploy it.
func editCompose(t *testing.T, s *stack, from, to string) {
	t.Helper()
	path := s.dir + "/compose.yaml"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), from) {
		t.Fatalf("%q not in the compose file", from)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// unparseableOracle makes the oracle's output unreadable, as on a compose that prints `null`: the guard must fall back to
// `config --hash`.
type unparseableOracle struct{ compose.Reader }

func (unparseableOracle) DryRunUp(context.Context, compose.Project, string) (compose.Oracle, error) {
	return compose.Oracle{}, compose.ErrUnparseable
}

// The config-drift guard blocks a recreate after a compose edit (the oracle is the primary check, `config --hash` the
// fallback), and the container is left as it was.
func TestDriftGuardBlocksAfterAComposeEdit(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(compose.Reader) compose.Reader
		want string
	}{
		{"dry-run oracle", nil, "compose would recreate this container"},
		{"config --hash fallback", func(r compose.Reader) compose.Reader { return unparseableOracle{r} }, "config --hash is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, cli := requireIntegration(t)
			s := stuckStack(t, g, cli, "driftcfg")
			editCompose(t, s, "command:", "environment:\n      EDITED: \"1\"\n    command:")
			before := s.container("app")

			r := startDaemonOpts(t, g, cli, daemonOpts{WrapCompose: tc.wrap})
			waitFor(t, "the app to be SUSPENDED", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
			blocked := eventMessage(r.Rec, engine.DriftBlocked, s.key("app"))
			if !strings.Contains(blocked, "config:") || !strings.Contains(blocked, tc.want) {
				t.Fatalf("DriftBlocked: %q", blocked)
			}
			if got := joined(actionsFor(r.Rec, s.key("app"))); got != "restart" {
				t.Fatalf("actions = %s: the restart ran, the recreate must not", got)
			}
			if m := r.Compose.Mutating(); len(m) != 0 {
				t.Fatalf("compose changed something despite the block: %v", m)
			}
			after := s.container("app")
			if after.ID != before.ID {
				t.Fatalf("the container was recreated: %s -> %s", before.ID, after.ID)
			}
			for _, e := range r.Rec.Events() {
				if e.Type == engine.Suspended && e.Attrs["reason"] != engine.ReasonDrift {
					t.Fatalf("%+v", e)
				}
			}
		})
	}
}

// retag moves a whalenurse-test/ tag onto a different image, the way `docker build -t` or a pull does.
func retag(t *testing.T, g *Guard, tag string) {
	t.Helper()
	if err := g.Build(context.Background(), tag, "FROM busybox:1.37\nRUN touch /moved\n"); err != nil {
		t.Fatal(err)
	}
}

// The image-drift guard blocks a recreate after a re-tag, on whichever image store this host uses (CI runs both), and does
// not block an unchanged container (TestLadderRestartRecreateSuspended recreates one).
func TestImageDriftGuardBlocksAfterAReTag(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(compose.Reader) compose.Reader
	}{
		{"dry-run oracle names the cause", nil},
		{"explicit comparison after the hash fallback", func(r compose.Reader) compose.Reader { return unparseableOracle{r} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, cli := requireIntegration(t)
			tag := TagPrefix + "drift:1"
			if err := g.Tag(context.Background(), "busybox:1.37", tag); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = g.Untag(context.Background(), tag) })
			s := newStack(t, g, cli, "driftimg", strings.Replace("services:"+stuckService("app"), "busybox:1.37", tag, 1))
			s.up()
			waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
			before := s.container("app")
			retag(t, g, tag)

			r := startDaemonOpts(t, g, cli, daemonOpts{WrapCompose: tc.wrap})
			waitFor(t, "the app to be SUSPENDED", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
			blocked := eventMessage(r.Rec, engine.DriftBlocked, s.key("app"))
			if !strings.Contains(blocked, "image: image "+tag+" now resolves to") || !strings.Contains(blocked, "the tag moved") {
				t.Fatalf("DriftBlocked: %q", blocked)
			}
			if len(r.Compose.Mutating()) != 0 || s.container("app").ID != before.ID {
				t.Fatalf("a recreate went ahead: %v", r.Compose.Mutating())
			}
		})
	}
}

// ---- dependents ------------------------------------------------------------------------------------------------

const wwwParent = `["sh", "-c", "mkdir -p /www; echo ok > /www/index.html; %strap 'exit 0' TERM; httpd -f -p 8080 -h /www & wait"]`

// sidecarStack is a parent that serves on 127.0.0.1:8080 and a network_mode: service:parent sidecar. The parent is healthy
// until /broken exists (writable layer, so only a recreate repairs it) or, with tmpfsFlag, until /ok/flag is removed (a
// restart repairs it).
func sidecarStack(t *testing.T, g *Guard, cli *docker.Moby, name string, tmpfsFlag bool, depHealthcheck string, depImage string, depOptIn bool) *stack {
	t.Helper()
	parent := fmt.Sprintf(`
  parent:
    image: busybox:1.37
    labels:
      whalenurse.enable: "true"
    stop_grace_period: 2s
    command: %s
    healthcheck:
      test: ["CMD", "sh", "-c", "test ! -f /broken"]
      interval: 1s
      timeout: 1s
      retries: 1
`, fmt.Sprintf(wwwParent, ""))
	if tmpfsFlag {
		parent = fmt.Sprintf(`
  parent:
    image: busybox:1.37
    labels:
      whalenurse.enable: "true"
    stop_grace_period: 2s
    tmpfs: ["/ok"]
    command: %s
    healthcheck:
      test: ["CMD", "test", "-f", "/ok/flag"]
      interval: 1s
      timeout: 1s
      retries: 1
`, fmt.Sprintf(wwwParent, "touch /ok/flag; "))
	}
	if depImage == "" {
		depImage = "busybox:1.37"
	}
	depLabels := "    labels:\n      whalenurse.enable: \"true\"\n"
	if !depOptIn {
		depLabels = "" // repaired by the dependents step all the same, because its parent is opted in (plan v3.6)
	}
	dep := fmt.Sprintf(`
  dep:
    image: %s
    network_mode: service:parent
%s    stop_grace_period: 2s
    command: %s
%s`, depImage, depLabels, loopCmd, depHealthcheck)
	s := newStack(t, g, cli, name, "services:"+parent+dep)
	s.up()
	waitFor(t, "parent to be healthy", 60*time.Second, func() bool { return s.container("parent").Health == "healthy" })
	return s
}

const reachHealthcheck = `    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/"]
      interval: 1s
      timeout: 1s
      retries: 1
      start_period: 1s
`

// reaches reports whether the container can fetch the parent's page on its own loopback: true only when it shares the
// parent's current network namespace (E1: a stale dependent has only a loopback of its own).
func reaches(g *Guard, id string) bool {
	_, err := g.Ctr(context.Background(), "exec", id, "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/")
	return err == nil
}

func sharesNamespace(t *testing.T, g *Guard, a, b string) bool {
	t.Helper()
	ns := func(id string) string {
		out, err := g.Ctr(context.Background(), "exec", id, "readlink", "/proc/self/ns/net")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	return ns(a) == ns(b)
}

// E1 and E2 through whalenurse: the parent needs a recreate (a restart does not repair it), and its sidecar, which is left
// pointing at a parent that is gone, is recreated by compose with the non-forced up and never restarted through the API.
func TestDependentIsRecreatedAfterItsParentIsRecreated(t *testing.T) {
	g, cli := requireIntegration(t)
	s := sidecarStack(t, g, cli, "deprecreate", false, reachHealthcheck, "", true)
	waitFor(t, "dep to be healthy", 60*time.Second, func() bool { return s.container("dep").Health == "healthy" })
	oldParent, oldDep := s.container("parent"), s.container("dep")
	breakWritableLayer(t, g, s, "parent")

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the parent to be recreated", 150*time.Second, func() bool { return s.container("parent").ID != oldParent.ID })
	waitFor(t, "the sidecar to be recreated", 150*time.Second, func() bool { return s.container("dep").ID != oldDep.ID })
	waitFor(t, "the sidecar to reach its parent again", 60*time.Second, func() bool {
		d := s.container("dep")
		return d.Running && d.Health == "healthy"
	})

	parent, dep := s.container("parent"), s.container("dep")
	if docker.ContainerRef(dep.NetworkMode) != parent.ID {
		t.Fatalf("the sidecar points at %s, the parent is %s", dep.NetworkMode, parent.ID)
	}
	if !sharesNamespace(t, g, dep.ID, parent.ID) || !reaches(g, dep.ID) {
		t.Fatal("the recreated sidecar does not share the parent's namespace")
	}
	if got := joined(actionsFor(r.Rec, s.key("parent"))); got != "restart,recreate" {
		t.Fatalf("parent actions = %s", got)
	}
	// Never an API restart or start of a dependent whose parent was gone (E1): its only action is the recreate.
	for _, a := range actionsFor(r.Rec, s.key("dep")) {
		if a == "restart" || a == "start" {
			t.Fatalf("the dependent was API-%sed: %v", a, actionsFor(r.Rec, s.key("dep")))
		}
	}
	var depUp string
	for _, l := range r.Compose.Mutating() {
		if strings.HasSuffix(l, " dep") {
			depUp = l
		}
	}
	if !strings.Contains(depUp, "up -d --no-deps --pull never --no-build dep") {
		t.Fatalf("no dependents up: %v", r.Compose.Mutating())
	}
	if strings.Contains(depUp, "--force-recreate") && countFor(r.Rec, engine.DependentPostCheck, s.key("dep")) == 0 {
		t.Fatalf("the dependents up is non-forced unless the post-check found it still stale: %s", depUp)
	}
	if countFor(r.Rec, engine.Suspended, s.key("dep")) != 0 || countFor(r.Rec, engine.Suspended, s.key("parent")) != 0 {
		t.Fatal("nothing should be SUSPENDED")
	}
}

// E1 with the parent recreated by someone else before whalenurse looks: the sidecar is running, unhealthy (it only has a
// loopback of its own), and a restart would turn it into an exited container. It is recreated instead. The sidecar is not
// opted in itself: the dependents step repairs it because its parent is (plan v3.6).
func TestStaleDependentOfAnExternallyRecreatedParentIsRecreatedNotRestarted(t *testing.T) {
	g, cli := requireIntegration(t)
	s := sidecarStack(t, g, cli, "depstale", false, reachHealthcheck, "", false)
	waitFor(t, "dep to be healthy", 60*time.Second, func() bool { return s.container("dep").Health == "healthy" })
	oldParent, oldDep := s.container("parent"), s.container("dep")
	s.compose("up", "-d", "--force-recreate", "--no-deps", "--pull", "never", "parent")
	if s.container("parent").ID == oldParent.ID {
		t.Fatal("setup: the parent was not recreated")
	}
	if d := s.container("dep"); docker.ContainerRef(d.NetworkMode) != oldParent.ID || !d.Running {
		t.Fatalf("setup: the dependent should be running with a stale network mode: %+v", d)
	}
	waitFor(t, "the dependent to be unhealthy", 30*time.Second, func() bool { return s.container("dep").Health == "unhealthy" })

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the sidecar to be recreated", 120*time.Second, func() bool { return s.container("dep").ID != oldDep.ID })
	waitFor(t, "the sidecar to be healthy", 60*time.Second, func() bool { return s.container("dep").Health == "healthy" })
	parent, dep := s.container("parent"), s.container("dep")
	if docker.ContainerRef(dep.NetworkMode) != parent.ID || !reaches(g, dep.ID) {
		t.Fatalf("%+v", dep)
	}
	if got := joined(actionsFor(r.Rec, s.key("dep"))); got != "recreate-stale" {
		t.Fatalf("dep actions = %s, want only the recreate", got)
	}
	if got := actionsFor(r.Rec, s.key("parent")); len(got) != 0 {
		t.Fatalf("a dependent must never trigger an action on its parent: %v", got)
	}
}

// Compose writes the depends_on label of the services it has in scope, so a sidecar that whalenurse recreated with
// `up --no-deps` comes back with an empty one, and its parent could no longer be found by it. The second time the parent is
// recreated the sidecar must still be repaired (plan v3.11). The sidecar is not opted in itself.
func TestSidecarIsRepairedAgainWhenTheParentIsRecreatedASecondTime(t *testing.T) {
	g, cli := requireIntegration(t)
	s := sidecarStack(t, g, cli, "deptwice", false, reachHealthcheck, "", false)
	waitFor(t, "dep to be healthy", 60*time.Second, func() bool { return s.container("dep").Health == "healthy" })
	if s.container("dep").Labels[compose.LabelDependsOn] == "" {
		t.Fatal("setup: a sidecar made by a full up should carry a depends_on label")
	}
	r := startDaemonOpts(t, g, cli, daemonOpts{})
	for round := 1; round <= 2; round++ {
		oldParent, oldDep := s.container("parent"), s.container("dep")
		s.compose("up", "-d", "--force-recreate", "--no-deps", "--pull", "never", "parent")
		if s.container("parent").ID == oldParent.ID {
			t.Fatalf("round %d: setup: the parent was not recreated", round)
		}
		waitFor(t, fmt.Sprintf("round %d: the sidecar to be recreated", round), 150*time.Second, func() bool { return s.container("dep").ID != oldDep.ID })
		waitFor(t, fmt.Sprintf("round %d: the sidecar to be healthy", round), 60*time.Second, func() bool {
			d := s.container("dep")
			return d.Running && d.Health == "healthy"
		})
		parent, dep := s.container("parent"), s.container("dep")
		if docker.ContainerRef(dep.NetworkMode) != parent.ID || !reaches(g, dep.ID) {
			t.Fatalf("round %d: %+v", round, dep)
		}
		if got := dep.Labels[compose.LabelDependsOn]; got != "" {
			// the positive control: this is what makes the second round meaningful
			t.Fatalf("round %d: compose kept the depends_on label (%q) of a service recreated with --no-deps; the premise of plan v3.11 no longer holds", round, got)
		}
		waitFor(t, fmt.Sprintf("round %d: the sidecar's incident to close", round), 60*time.Second, func() bool {
			return countFor(r.Rec, engine.Recovered, s.key("dep")) >= round
		})
	}
	if countFor(r.Rec, engine.Suspended, s.key("dep")) != 0 {
		t.Fatal("the sidecar was SUSPENDED")
	}
}

// A parent restarted through the API keeps its ID, and its sidecar keeps the old namespace: it is restarted too, not
// recreated. The sidecar is not opted in itself (plan v3.6).
func TestDependentIsRestartedAfterItsParentIsRestarted(t *testing.T) {
	g, cli := requireIntegration(t)
	s := sidecarStack(t, g, cli, "deprestart", true, "", "", false)
	oldParent, oldDep := s.container("parent"), s.container("dep")
	if _, err := g.Ctr(context.Background(), "exec", oldParent.ID, "rm", "/ok/flag"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "parent to turn unhealthy", 30*time.Second, func() bool { return s.container("parent").Health == "unhealthy" })

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the parent to be restarted", 90*time.Second, func() bool { return s.container("parent").StartedAt.After(oldParent.StartedAt) })
	waitFor(t, "the sidecar to be restarted", 90*time.Second, func() bool { return s.container("dep").StartedAt.After(oldDep.StartedAt) })
	waitFor(t, "the sidecar to reach its parent", 60*time.Second, func() bool {
		return s.container("dep").Running && reaches(g, s.container("dep").ID)
	})
	parent, dep := s.container("parent"), s.container("dep")
	if parent.ID != oldParent.ID || dep.ID != oldDep.ID {
		t.Fatalf("a restart keeps the IDs: parent %s -> %s, dep %s -> %s", oldParent.ID, parent.ID, oldDep.ID, dep.ID)
	}
	if !sharesNamespace(t, g, dep.ID, parent.ID) {
		t.Fatal("the restarted sidecar is not in the parent's namespace")
	}
	if got := joined(actionsFor(r.Rec, s.key("dep"))); got != "restart" {
		t.Fatalf("dep actions = %s, want one restart", got)
	}
	if m := r.Compose.Mutating(); len(m) != 0 {
		t.Fatalf("repairing a restart needs no compose: %v", m)
	}
}

// Checks 2 and 3 of the drift guard still apply to a dependent (check 1 is skipped for it): a re-tagged image blocks its
// recreate. This is the explicit image comparison, on a reference holder, on whichever store this host uses.
func TestStaleDependentWithAMovedTagIsBlocked(t *testing.T) {
	g, cli := requireIntegration(t)
	tag := TagPrefix + "depimg:1"
	if err := g.Tag(context.Background(), "busybox:1.37", tag); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Untag(context.Background(), tag) })
	s := sidecarStack(t, g, cli, "depdrift", false, reachHealthcheck, tag, true)
	oldParent, oldDep := s.container("parent"), s.container("dep")
	s.compose("up", "-d", "--force-recreate", "--no-deps", "--pull", "never", "parent")
	if s.container("parent").ID == oldParent.ID {
		t.Fatal("setup: the parent was not recreated")
	}
	retag(t, g, tag)

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the dependent to be SUSPENDED", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("dep")) == 1 })
	blocked := eventMessage(r.Rec, engine.DriftBlocked, s.key("dep"))
	if !strings.Contains(blocked, "image: image "+tag+" now resolves to") {
		t.Fatalf("DriftBlocked: %q", blocked)
	}
	if s.container("dep").ID != oldDep.ID || len(r.Compose.Mutating()) != 0 {
		t.Fatalf("the dependent was touched: %v", r.Compose.Mutating())
	}
	for _, l := range r.Compose.Log() {
		if strings.Contains(l, "--dry-run") || strings.Contains(l, "config --hash") {
			t.Fatalf("check 1 must be skipped for a reference holder: %s", l)
		}
	}
}

// ---- the version gate, dry-run -----------------------------------------------------------------------------------

// oldEngine reports an Engine below 28.1 whatever the real one is: the faked version of the gate test.
type oldEngine struct{ *docker.Moby }

func (oldEngine) ServerVersion(context.Context) (docker.ServerInfo, error) {
	return docker.ServerInfo{Version: "28.0.4", APIVersion: "1.48", Arch: "amd64", OS: "linux"}, nil
}

func TestVersionGateWithAnOldEngineEndsTheLadderAfterRung1(t *testing.T) {
	g, cli := requireIntegration(t)
	s := stuckStack(t, g, cli, "gate")
	before := s.container("app")
	r := startDaemonOpts(t, g, cli, daemonOpts{Reader: oldEngine{cli}})
	waitFor(t, "the app to be SUSPENDED", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	if got := joined(actionsFor(r.Rec, s.key("app"))); got != "restart" {
		t.Fatalf("actions = %s: only rung 1 may run", got)
	}
	if n := countFor(r.Rec, engine.RungUnavailable, s.key("app")); n != 1 {
		t.Fatalf("RungUnavailable events: %d", n)
	}
	if n := countFor(r.Rec, engine.RungsDisabled, ""); n != 1 {
		t.Fatalf("one RungsDisabled event, got %d", n)
	}
	if st := r.D.Status().(daemon.Status).Compose; st.Rungs || len(st.Disabled) != 2 || !strings.Contains(st.Reason, "28.0.4") {
		t.Fatalf("status: %+v", st)
	}
	if log := r.Compose.Log(); len(log) != 0 {
		t.Fatalf("compose must not run below the minimum: %v", log)
	}
	if after := s.container("app"); after.ID != before.ID {
		t.Fatal("the container was recreated")
	}
}

// dry-run changes nothing: it walks the whole ladder in its head, runs the read-only guard (compose's own --dry-run), and
// never runs an up that is not a dry run.
func TestDryRunNeverRecreates(t *testing.T) {
	g, cli := requireIntegration(t)
	s := stuckStack(t, g, cli, "drycompose")
	before := s.container("app")
	r := startDaemonOpts(t, g, cli, daemonOpts{Dry: true})
	waitFor(t, "the dry-run ladder to finish", 120*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	if n := countFor(r.Rec, engine.ActionResult, s.key("app")); n != 2 {
		t.Fatalf("restart and recreate, both dry: %d", n)
	}
	time.Sleep(10 * time.Second)
	after := s.container("app")
	if after.ID != before.ID || !after.StartedAt.Equal(before.StartedAt) || after.RestartCount != before.RestartCount {
		t.Fatalf("dry-run changed the container: %+v -> %+v", before.StartedAt, after.StartedAt)
	}
	if m := r.Compose.Mutating(); len(m) != 0 {
		t.Fatalf("dry-run ran a compose up that is not a dry run: %v", m)
	}
	if !strings.Contains(strings.Join(r.Compose.Log(), "\n"), "--dry-run") {
		t.Fatalf("the guard should have run compose --dry-run: %v", r.Compose.Log())
	}
}

// ---- the image: a containerized whalenurse recreating from the stack's own files -------------------------------------

type imageStatus struct {
	Compose struct {
		Binary  string `json:"binary"`
		Source  string `json:"source"`
		Version string `json:"version"`
		Rungs   bool   `json:"recreate_enabled"`
	} `json:"compose"`
	Engine struct {
		Keys []struct {
			Key           string `json:"key"`
			State         string `json:"state"`
			SuspendReason string `json:"suspend_reason"`
		} `json:"keys"`
	} `json:"engine"`
}

func imageEnv() string {
	return fmt.Sprintf(`      WHALENURSE_HOST_ID: %s
      WHALENURSE_GRACE: 2s
      WHALENURSE_POLL_INTERVAL: 5s
      WHALENURSE_COOLDOWN: 5s
      WHALENURSE_STARTUP_SETTLE: 0s
`, testHost)
}

// imageStack: a whalenurse container (read-only rootfs, nonroot, the socket's group) that heals a stuck app in the same
// project. hostPlugin mounts the host's compose at /host/docker-compose, read-only; the stacks directory is mounted
// read-only at the identical path either way (plan §3.4, E6).
func imageStack(t *testing.T, g *Guard, cli *docker.Moby, name string, hostPlugin bool) (*stack, string) {
	t.Helper()
	img := os.Getenv("WHALENURSE_TEST_IMAGE")
	if img == "" {
		t.Skip("set WHALENURSE_TEST_IMAGE to a locally built image to test it")
	}
	if !strings.HasPrefix(img, TagPrefix) {
		t.Fatalf("image %q: only whalenurse-test/* local tags are used", img)
	}
	fi, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	gid := fi.Sys().(*syscall.Stat_t).Gid
	// The stack directory is created before the compose file that mounts it, so the mount can name it.
	s := newStack(t, g, cli, name, "services: {}")
	vols := []string{"/var/run/docker.sock:/var/run/docker.sock", "data:/data", s.dir + ":" + s.dir + ":ro"}
	if hostPlugin {
		vols = append(vols, composeBin(t).Path+":"+compose.HostMountPath+":ro")
	}
	var vl strings.Builder
	for _, v := range vols {
		fmt.Fprintf(&vl, "      - %q\n", v)
	}
	body := fmt.Sprintf(`
services:
  whalenurse:
    image: %s
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    group_add: ["%d"]
    tmpfs: ["/run:uid=65532,gid=65532,mode=0700"]
    volumes:
%s    environment:
%s%s
volumes:
  data: {}
`, img, gid, vl.String(), imageEnv(), stuckService("app"))
	if err := os.WriteFile(s.dir+"/compose.yaml", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.up("app")
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	return s, img
}

func imageStatusOf(t *testing.T, g *Guard, s *stack) imageStatus {
	t.Helper()
	out, err := g.Ctr(context.Background(), "exec", s.container("whalenurse").ID, "/whalenurse", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var st imageStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return st
}

func suspendedIn(st imageStatus, key string) (bool, string) {
	for _, k := range st.Engine.Keys {
		if k.Key == key && k.State == "SUSPENDED" {
			return true, k.SuspendReason
		}
	}
	return false, ""
}

// The containerized whalenurse performs rung 2 itself: read-only rootfs, the nonroot user, the host's compose plugin
// mounted read-only, the stacks directory mounted read-only at the identical path.
func TestImageRecreatesWithTheHostPlugin(t *testing.T) {
	g, cli := requireIntegration(t)
	s, _ := imageStack(t, g, cli, "imgrecreate", true)
	before := s.container("app")
	s.up("whalenurse")
	if !inspectReadonly(t, s.container("whalenurse").ID) {
		t.Fatal("the image test must run with a read-only root filesystem")
	}
	waitFor(t, "the app to be recreated by the containerized whalenurse", 150*time.Second, func() bool { return s.container("app").ID != before.ID })
	waitFor(t, "the app to be SUSPENDED (the stuck app stays unhealthy)", 120*time.Second, func() bool {
		ok, _ := suspendedIn(imageStatusOf(t, g, s), s.key("app"))
		return ok
	})
	st := imageStatusOf(t, g, s)
	if !st.Compose.Rungs || st.Compose.Source != "host" || st.Compose.Binary != compose.HostMountPath {
		t.Fatalf("whalenurse should use the host plugin it was given: %+v", st.Compose)
	}
	if _, why := suspendedIn(st, s.key("app")); why != engine.ReasonLadder {
		t.Fatalf("suspended for %q, want the ladder (restart, recreate, nothing helped)", why)
	}
}

// Without the host plugin the bundled copy is used. If its release is the one that made the stack's containers, the
// recreate goes ahead; if not, the guard must say it cannot verify drift (version skew) rather than call it drift or
// recreate blindly.
func TestImageUsesTheBundledComposeWhenThereIsNoHostPlugin(t *testing.T) {
	g, cli := requireIntegration(t)
	s, _ := imageStack(t, g, cli, "imgbundled", false)
	before := s.container("app")
	hostVersion := composeBin(t).Version.String()
	s.up("whalenurse")
	waitFor(t, "the app to be SUSPENDED", 150*time.Second, func() bool {
		ok, _ := suspendedIn(imageStatusOf(t, g, s), s.key("app"))
		return ok
	})
	st := imageStatusOf(t, g, s)
	if !st.Compose.Rungs || st.Compose.Source != "bundled" {
		t.Fatalf("whalenurse should use the bundled compose: %+v", st.Compose)
	}
	recreated := s.container("app").ID != before.ID
	_, why := suspendedIn(st, s.key("app"))
	t.Logf("host compose %s, bundled %s: recreated=%v, suspended for %q", hostVersion, st.Compose.Version, recreated, why)
	switch {
	case st.Compose.Version == hostVersion:
		if !recreated || why != engine.ReasonLadder {
			t.Fatalf("the same release made the stack: the recreate must go ahead (recreated=%v, reason=%q)", recreated, why)
		}
	case recreated:
		// A different release that computed the same hash: fine.
	default:
		if why != engine.ReasonDrift {
			t.Fatalf("not recreated, so it must be the guard: reason=%q", why)
		}
		// And it must be the skew finding, not a config or image finding (which would be a false drift report).
		msgs := driftBlockedMessages(t, s.container("whalenurse").ID)
		if len(msgs) != 1 || !strings.Contains(msgs[0], "can't verify drift (compose ") || strings.Contains(msgs[0], "image:") || strings.Contains(msgs[0], "config:") {
			t.Fatalf("want one skew finding, got %q", msgs)
		}
	}
}

// driftBlockedMessages reads the DriftBlocked events from the JSON log of a whalenurse container.
func driftBlockedMessages(t *testing.T, id string) []string {
	t.Helper()
	// whalenurse logs to stderr, which ExecRunner does not return.
	out, err := exec.Command("docker", "logs", id).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, line := range strings.Split(string(out), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["type"] == "DriftBlocked" {
			msg, _ := m["msg"].(string)
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// The precondition of plan v3.13 item 4, on every compose release and image store of the CI matrix: for a reference holder
// whose references are current, compose's own hash matches the one it recorded, so the dry-run oracle says nothing for an
// unchanged holder and says Recreate after an edit to the holder's own definition. One holder of each kind (E4 hashed each
// after resolving it: network_mode, ipc, pid, volumes_from). If a release or a kind fails this, add the release to
// compose.refHolderOracleExcluded (or take the kind out of Daemon.refsCurrent) and record it in docs/m2b-notes.md.
func TestOracleForAReferenceHolderWithCurrentReferences(t *testing.T) {
	g, cli := requireIntegration(t)
	holder := func(name, ref string) string {
		return fmt.Sprintf(`
  %s:
    image: busybox:1.37
%s    stop_grace_period: 2s
    command: %s
`, name, ref, loopCmd)
	}
	file := "services:\n  parent:\n    image: busybox:1.37\n    ipc: shareable\n    volumes: [\"/shared\"]\n    stop_grace_period: 2s\n    command: " + loopCmd + "\n" +
		holder("netdep", "    network_mode: service:parent\n") +
		holder("ipcdep", "    ipc: service:parent\n") +
		holder("piddep", "    pid: service:parent\n") +
		holder("vfdep", "    volumes_from:\n      - parent\n")
	s := newStack(t, g, cli, "reforacle", file)
	s.up()
	anchors := map[string]string{
		"netdep": "network_mode: service:parent", "ipcdep": "ipc: service:parent", "piddep": "pid: service:parent",
		"vfdep": "volumes_from:\n      - parent",
	}
	c := compose.New(*composeBin(t), NewComposeRunner(t.TempDir()))
	ask := func(service string) (compose.Oracle, error) {
		t.Helper()
		dep := s.container(service)
		p, err := compose.ProjectFromLabels(dep.Labels)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		o, err := c.DryRunUp(ctx, p, dep.Name)
		return o, err
	}
	ids := map[string]string{}
	for service := range anchors {
		ids[service] = s.container(service).ID
		o, err := ask(service)
		if err != nil {
			t.Fatalf("the oracle for %s: %v", service, err)
		}
		if o.WouldRecreate() {
			t.Fatalf("%s: an unchanged holder whose reference is current must not be a Recreate: %v", service, o.Texts())
		}
	}
	for _, anchor := range anchors {
		editCompose(t, s, anchor, anchor+"\n    environment:\n      EDITED: \"1\"")
	}
	for service := range anchors {
		o, err := ask(service)
		if err != nil {
			t.Fatalf("the oracle for %s after the edit: %v", service, err)
		}
		if !o.WouldRecreate() {
			t.Fatalf("%s: an edit to the holder's own definition must be a Recreate: %v", service, o.Texts())
		}
		if after := s.container(service); after.ID != ids[service] {
			t.Fatalf("%s: the dry run changed the container: %s -> %s", service, ids[service], after.ID)
		}
	}
}
