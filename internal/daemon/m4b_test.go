package daemon

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
)

// M4 review decisions (plan v3.8): rung 3 requires rung 2, rung 2 lets an image-only move through for a pull-enabled
// container, a sidecar may take the tag its parent's pull moved in this incident, and the pull rung respects
// pull_policy never and build.

const imgOldID = "sha256:oldoldoldoldold0000000000000000000000000000000000000000000000000000"
const imgNewID = "sha256:newnewnewnewnew1111111111111111111111111111111111111111111111111111"

// ---- decision 4: pull_policy never and build ---------------------------------------------------------------------

func TestPullRungRespectsPullPolicyNeverAndBuild(t *testing.T) {
	for _, policy := range []string{"never", "build"} {
		t.Run(policy, func(t *testing.T) {
			h, web := pullStack(t, nil, docker.ServerInfo{})
			h.cf.PolicyFn = func(compose.Project) (string, error) { return policy, nil }
			h.cf.OnPull = func(compose.Project) error {
				t.Error("compose pull ran for a service with pull_policy " + policy)
				return nil
			}
			breakAndRun(h, web.ID, 30*time.Minute)

			if got := h.verbs(); got != "dry-run web, recreate web, policy web" {
				t.Fatalf("compose calls = %q: the policy is read, and nothing is pulled or checked after it", got)
			}
			var skipped []engine.Event
			for _, e := range h.events(engine.RungUnavailable) {
				if e.Attrs["rung"] == "pull" {
					skipped = append(skipped, e)
				}
			}
			if len(skipped) != 1 || skipped[0].Attrs["not_applicable"] != "true" || !strings.Contains(skipped[0].Message, "pull rung does not apply (pull_policy "+policy+")") {
				t.Fatalf("%+v", skipped)
			}
			sus := h.events(engine.Suspended)
			if len(sus) != 1 || !strings.Contains(sus[0].Message, "pull rung does not apply (pull_policy "+policy+")") || sus[0].Attrs["reason"] != engine.ReasonLadder {
				t.Fatalf("the giving-up cause must say it: %+v", sus)
			}
			if h.count(engine.ActionStarted) != 2 || h.count(engine.TagMoved) != 0 {
				t.Fatalf("restart and recreate only: %v", h.rec.Types())
			}
			h.advance(30 * time.Minute)
			if h.cf.Count("policy") != 1 || h.cf.Count("pull") != 0 {
				t.Fatalf("SUSPENDED: nothing more is read or pulled: %s", h.verbs())
			}
		})
	}
}

// missing, if_not_present, the refresh windows and no policy are replaced by --policy always: the pull runs.
func TestPullRungRunsForEveryOtherPolicy(t *testing.T) {
	for _, policy := range []string{"", "always", "missing", "if_not_present", "daily", "weekly", "every_12h", "refresh"} {
		t.Run("policy="+policy, func(t *testing.T) {
			h, web := pullStack(t, nil, docker.ServerInfo{})
			h.cf.PolicyFn = func(compose.Project) (string, error) { return policy, nil }
			breakAndRun(h, web.ID, 30*time.Minute)
			if h.cf.Count("pull") != 1 {
				t.Fatalf("%s", h.verbs())
			}
		})
	}
}

// A policy that cannot be read is a reason not to pull (fail closed), said as the cause.
func TestPullRungDoesNotRunWhenThePolicyCannotBeRead(t *testing.T) {
	h, web := pullStack(t, nil, docker.ServerInfo{})
	h.cf.PolicyFn = func(compose.Project) (string, error) { return "", errors.New("compose config: invalid compose file") }
	breakAndRun(h, web.ID, 30*time.Minute)
	if h.cf.Count("pull") != 0 || h.cf.Count("up") != 0 {
		t.Fatalf("%s", h.verbs())
	}
	sus := h.events(engine.Suspended)
	if len(sus) != 1 || !strings.Contains(sus[0].Message, "cannot read the service's pull_policy") {
		t.Fatalf("%+v", sus)
	}
}

// A dry run reads the policy too (it only reads), and tells that the rung would not apply instead of "would pull".
func TestDryRunSaysThePullRungDoesNotApply(t *testing.T) {
	h, web := pullStack(t, func(c *config.Global) { c.DryRun = true }, docker.ServerInfo{})
	h.cf.PolicyFn = func(compose.Project) (string, error) { return "never", nil }
	breakAndRun(h, web.ID, 30*time.Minute)
	for _, e := range h.events(engine.ActionResult) {
		if e.Attrs["action"] == "pull" {
			t.Fatalf("a dry run must not say it would pull a service with pull_policy never: %+v", e)
		}
	}
	if h.cf.Count("policy") != 1 || h.cf.Count("pull") != 0 || h.count(engine.Suspended) != 1 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
}

// ---- decision 1: rung 3 requires rung 2 ---------------------------------------------------------------------------

func TestPullWithoutRecreateInActionsKeepsThePullRungOffWithOneWarning(t *testing.T) {
	h, web := pullStack(t, nil, docker.ServerInfo{}, config.LabelActions, "restart")
	breakAndRun(h, web.ID, 40*time.Minute)
	if len(h.cf.Calls()) != 0 {
		t.Fatalf("no recreate, no pull: %s", h.verbs())
	}
	started := 0
	for _, e := range h.events(engine.ActionStarted) {
		if e.Attrs["action"] != "restart" {
			t.Fatalf("%+v", e)
		}
		started++
	}
	if started != 1 || h.count(engine.Suspended) != 1 {
		t.Fatalf("restart, then SUSPENDED: %v", h.rec.Types())
	}
	if n := strings.Count(h.log.String(), "the pull rung stays off"); n != 1 {
		t.Fatalf("one warning, got %d:\n%s", n, h.log.String())
	}
}

// ---- decision 2: rung 2 lets an image-only move through for a pull-enabled container ------------------------------

func upgradeSetup(t *testing.T, labelPull bool) (*cfH, docker.Container) {
	t.Helper()
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	extra := []string{}
	if labelPull {
		extra = append(extra, config.LabelHealPull, "true")
	}
	web := h.svc(hexID("a1"), "p", "web", extra...)
	web = h.set(web.ID, func(c *docker.Container) { c.ImageID = imgOldID })
	h.fake.SetImage("web:1", imgNewID) // someone else pulled: the tag moved under the container
	h.cf.OracleFn = func(_ compose.Project, name string) (compose.Oracle, error) { return compose.Recreated(name), nil }
	h.cf.HashFn = func(compose.Project) (string, error) { return "h-web", nil } // the compose file is what it was
	return h, web
}

func TestRung2TakesAnImageOnlyMoveForAPullEnabledContainerAndSaysSo(t *testing.T) {
	h, web := upgradeSetup(t, true)
	h.cf.OnRecreate = func(compose.Project) error {
		h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy"; c.ImageID = imgNewID })
		return nil
	}
	breakAndRun(h, web.ID, 15*time.Minute)

	if h.cf.Count("recreate") != 1 || h.count(engine.DriftBlocked) != 0 || h.count(engine.Suspended) != 0 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
	up := h.events(engine.Upgraded)
	if len(up) != 1 {
		t.Fatalf("%v", h.rec.Types())
	}
	a := up[0].Attrs
	if a["ref"] != "web:1" || a["old"] != imgOldID || a["new"] != imgNewID || a["old_image"] != imgOldID || a["ok"] != "true" {
		t.Fatalf("%+v", a)
	}
	if a["rollback_tag"] != "docker image tag "+imgOldID+" web:1" || !strings.HasSuffix(a["rollback_up"], "up -d --no-deps --pull never --no-build web") {
		t.Fatalf("%+v", a)
	}
	if h.count(engine.TagMoved) != 0 {
		t.Fatal("whalenurse did not move this tag: no TagMoved")
	}
	if h.cf.Count("pull") != 0 {
		t.Fatal("rung 2 never pulls")
	}
}

// Compose's up replaces the container (create, stop, remove, rename) before its start phase, so a start that fails still
// leaves the container on the moved image: that is an upgrade too, with ok=false and the rollback.
func TestRung2ThatReplacedTheContainerButFailedStillSaysUpgraded(t *testing.T) {
	h, web := upgradeSetup(t, true)
	h.cf.OnRecreate = func(compose.Project) error {
		h.replace("p", "web", func(c *docker.Container) {
			c.ImageID, c.Running, c.Status, c.ExitCode = imgNewID, false, "created", 128
			c.Error = "Bind for 0.0.0.0:80 failed: port is already allocated"
		})
		return errors.New("exit status 1")
	}
	breakAndRun(h, web.ID, 15*time.Minute)

	if h.cf.Count("recreate") != 1 {
		t.Fatalf("%s", h.verbs())
	}
	up := h.events(engine.Upgraded)
	if len(up) != 1 {
		t.Fatalf("want one Upgraded: %v", h.rec.Types())
	}
	a := up[0].Attrs
	if a["old"] != imgOldID || a["new"] != imgNewID || a["ok"] != "false" || a["error"] == "" {
		t.Fatalf("%+v", a)
	}
	if a["rollback_tag"] != "docker image tag "+imgOldID+" web:1" || !strings.HasSuffix(a["rollback_up"], " web") {
		t.Fatalf("%+v", a)
	}
}

// A recreate that failed before it replaced the container changed no image, so it says nothing of an upgrade.
func TestRung2ThatFailedBeforeReplacingSaysNothing(t *testing.T) {
	h, web := upgradeSetup(t, true)
	h.cf.OnRecreate = func(compose.Project) error { return errors.New("exit status 1") }
	breakAndRun(h, web.ID, 15*time.Minute)
	if h.cf.Count("recreate") != 1 || h.count(engine.Upgraded) != 0 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
}

// Config drift still blocks, whatever the label says.
func TestRung2StillBlocksConfigDriftForAPullEnabledContainer(t *testing.T) {
	h, web := upgradeSetup(t, true)
	h.cf.HashFn = func(compose.Project) (string, error) { return "h-edited", nil }
	breakAndRun(h, web.ID, 6*time.Minute)
	b := h.events(engine.DriftBlocked)
	if h.cf.Count("recreate") != 0 || len(b) != 1 || !strings.Contains(b[0].Message, "config:") || h.count(engine.Upgraded) != 0 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
}

// Without the pull label nothing changed: the moved tag blocks (v3.6).
func TestRung2StillBlocksAMovedTagWithoutThePullLabel(t *testing.T) {
	h, web := upgradeSetup(t, false)
	breakAndRun(h, web.ID, 6*time.Minute)
	b := h.events(engine.DriftBlocked)
	if h.cf.Count("recreate") != 0 || len(b) != 1 || !strings.Contains(b[0].Message, "the tag moved") {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
}

// A recreate of a pull-enabled container that ends on the same image is not an upgrade: no message.
func TestRung2OfAPullEnabledContainerOnTheSameImageIsNotAnUpgrade(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	web := h.svc(hexID("a1"), "p", "web", config.LabelHealPull, "true")
	h.set(web.ID, func(c *docker.Container) { c.ImageID = imgOldID })
	h.cf.OnRecreate = func(compose.Project) error {
		h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy" })
		return nil
	}
	breakAndRun(h, web.ID, 15*time.Minute)
	if h.cf.Count("recreate") != 1 || h.count(engine.Upgraded) != 0 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
}

// ---- decision 3: a sidecar may take the tag its parent's pull moved, in this incident, and only that ---------------

// sidecarStack: web (pull-enabled) and a sidecar in its network namespace that runs the same image reference.
func sidecarStack(t *testing.T, sideLabelImage string) (*cfH, docker.Container, docker.Container, *docker.Container) {
	t.Helper()
	h, web := pullStack(t, nil, docker.ServerInfo{})
	web = h.set(web.ID, func(c *docker.Container) { c.ImageID = imgOldID })
	side := h.svc(hexID("d1"), "p", "side", compose.LabelDependsOn, "web:service_started:true")
	side = h.set(side.ID, func(c *docker.Container) {
		c.NetworkMode, c.Image, c.ImageID = "container:"+web.ID, "web:1", imgOldID
		c.Labels[compose.LabelImage] = sideLabelImage
	})
	newWeb := &docker.Container{}
	h.cf.OnRecreate = func(compose.Project) error {
		*newWeb = h.replace("p", "web", func(c *docker.Container) { c.Health = "unhealthy" })
		return nil
	}
	h.cf.OnPull = func(compose.Project) error { h.fake.SetImage("web:1", imgNewID); return nil }
	h.cf.OnUp = func(p compose.Project) error {
		switch p.Service {
		case "web":
			*newWeb = h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy"; c.ImageID = imgNewID })
		case "side":
			h.replace("p", "side", func(c *docker.Container) {
				c.NetworkMode = "container:" + newWeb.ID
				c.ImageID = h.fakeImageID("web:1")
			})
		}
		return nil
	}
	return h, web, side, newWeb
}

func (h *cfH) fakeImageID(ref string) string { id, _ := h.fake.ImageID(h.ctx, ref, "amd64"); return id }

func TestSidecarTakesTheTagItsParentsPullMoved(t *testing.T) {
	h, web, side, newWeb := sidecarStack(t, imgLabel("web"))
	_ = side
	breakAndRun(h, web.ID, 6*time.Minute) // the pull and the sidecar's recreate, before the incident's recovery period is over

	for _, e := range h.events(engine.DriftBlocked) {
		t.Fatalf("nothing may be blocked: %+v", e)
	}
	verbs := h.verbs()
	if !strings.Contains(verbs, "pull web, up web") || !strings.HasSuffix(verbs, "up side") {
		t.Fatalf("compose calls = %q: the sidecar is recreated after the parent's up", verbs)
	}
	if now := h.current("p", "side"); now.NetworkMode != "container:"+h.current("p", "web").ID || !now.Running {
		t.Fatalf("the sidecar must follow the new parent: %+v", now)
	}
	moved := h.events(engine.TagMoved)
	if len(moved) != 1 {
		t.Fatalf("%v", h.rec.Types())
	}
	a := moved[0].Attrs
	if a["sidecars"] != "side" || !strings.HasSuffix(a["rollback_up"], "up -d --no-deps --pull never --no-build web side") {
		t.Fatalf("the sidecar's rollback must be in the same message: %+v", a)
	}
	if h.count(engine.Upgraded) != 0 {
		t.Fatal("one upgrade message: the sidecar's recreate says nothing of its own")
	}

	// And only that: each condition on its own turns the allowance off. The predicate runs on the sidecar as it was before
	// its recreate (compose's --no-deps empties the depends_on label of the recreated one, and the fake leaves its image label alone).
	pre := side
	pre.Labels = maps.Clone(side.Labels)
	m, _ := h.d.moveOf("web:1")
	if !h.d.dependentMayTakeMove(h.ctx, pre) {
		t.Fatalf("the parent's incident is open and the move is this one's: must be allowed (%+v)", m)
	}
	other := pre
	other.Labels = maps.Clone(pre.Labels)
	other.Labels[compose.LabelImage] = "sha256:someotherimage000000000000000000000000000000000000000000000000"
	if h.d.dependentMayTakeMove(h.ctx, other) {
		t.Error("a sidecar created from some other image than the one the pull replaced is drifted for another reason")
	}
	other.Labels[compose.LabelImage] = imgLabel("web")
	other.Labels[compose.LabelDependsOn] = "db:service_started:true"
	if h.d.dependentMayTakeMove(h.ctx, other) {
		t.Error("the move was made by a service the sidecar does not depend on")
	}
	h.fake.SetImage("web:1", "sha256:movedagain0000000000000000000000000000000000000000000000000000")
	if h.d.dependentMayTakeMove(h.ctx, pre) {
		t.Error("the tag moved again since that pull: nothing may be taken")
	}
	h.fake.SetImage("web:1", imgNewID)
	_ = newWeb
	// Once the parent's incident is over (it recovered), the allowance is over too.
	h.advance(15 * time.Minute)
	if open, _ := h.d.Engine().IncidentOpen(key("p", "web")); open {
		t.Fatal("the parent's incident should have closed")
	}
	if h.d.dependentMayTakeMove(h.ctx, pre) {
		t.Error("outside the parent's incident a moved tag is plain image drift")
	}
}

// A sidecar that was created from some other image than the one the pull replaced is still blocked by its own check.
func TestSidecarFromAnotherImageIsStillBlocked(t *testing.T) {
	h, web, _, _ := sidecarStack(t, "sha256:someotherimage000000000000000000000000000000000000000000000000")
	// Its own guard finds the image drift as soon as the dependents step meets it (the oracle is not used for it).
	breakAndRun(h, web.ID, 40*time.Minute)
	blocked := false
	for _, e := range h.events(engine.DriftBlocked) {
		blocked = blocked || e.Key == key("p", "side")
	}
	if !blocked {
		t.Fatalf("%v / %s", h.rec.Types(), h.verbs())
	}
	if h.cf.Count("up") > 1 {
		t.Fatalf("only the parent's up: %s", h.verbs())
	}
}

// compose's `up --no-deps` leaves a sidecar's depends_on label empty, so after one repair the readers of the pull rung find
// the parent only through the learned set (plan v3.11). The fake's replace empties the label like compose does.
func TestSidecarRecreatedOnceStillTakesItsParentsMovedTag(t *testing.T) {
	h, web, side, _ := sidecarStack(t, imgLabel("web"))
	h.tick()
	h.advance(time.Minute) // the parents are learned from the label
	h.set(side.ID, func(c *docker.Container) {
		c.Labels = maps.Clone(c.Labels)
		delete(c.Labels, compose.LabelDependsOn) // the state a previous `up --no-deps` repair leaves
	})
	h.advance(time.Minute)
	breakAndRun(h, web.ID, 6*time.Minute)

	for _, e := range h.events(engine.DriftBlocked) {
		if e.Key == key("p", "side") {
			t.Fatalf("the sidecar must take its parent's moved tag: %+v", e)
		}
	}
	moved := h.events(engine.TagMoved)
	if len(moved) != 1 || moved[0].Attrs["sidecars"] != "side" || !strings.HasSuffix(moved[0].Attrs["rollback_up"], "up -d --no-deps --pull never --no-build web side") {
		t.Fatalf("%v / %+v", h.rec.Types(), moved)
	}
}

// ---- plan v3.13 item 6: a sidecar follows its parent's rung-2 upgrade, and the rollback names it -----------------

// upgradeWithSidecar: web (pull-enabled) is recreated by rung 2 onto an image the tag moved to under it, and a sidecar in
// its network namespace runs the same reference, created from the image that is replaced.
func upgradeWithSidecar(t *testing.T) (*cfH, docker.Container, docker.Container) {
	t.Helper()
	h, web := upgradeSetup(t, true)
	side := h.svc(hexID("d1"), "p", "side", compose.LabelDependsOn, "web:service_started:true")
	side = h.set(side.ID, func(c *docker.Container) {
		c.NetworkMode, c.Image, c.ImageID = "container:"+web.ID, "web:1", imgOldID
		c.Labels[compose.LabelImage] = imgLabel("web")
	})
	var newWeb docker.Container
	h.cf.OnRecreate = func(compose.Project) error {
		newWeb = h.replace("p", "web", func(c *docker.Container) { c.Health = "healthy"; c.ImageID = imgNewID })
		return nil
	}
	h.cf.OnUp = func(p compose.Project) error {
		if p.Service == "side" {
			h.replace("p", "side", func(c *docker.Container) {
				c.NetworkMode = "container:" + newWeb.ID
				c.ImageID = h.fakeImageID("web:1")
			})
		}
		return nil
	}
	return h, web, side
}

func TestSidecarFollowsItsParentsRung2UpgradeAndTheRollbackNamesIt(t *testing.T) {
	h, web, side := upgradeWithSidecar(t)
	breakAndRun(h, web.ID, 6*time.Minute)

	for _, e := range h.events(engine.DriftBlocked) {
		t.Fatalf("nothing may be blocked: %+v", e)
	}
	if h.count(engine.Suspended) != 0 || h.cf.Count("recreate") != 1 || h.cf.Count("up") != 1 {
		t.Fatalf("%s / %v", h.verbs(), h.rec.Types())
	}
	if now := h.current("p", "side"); now.ID == side.ID || now.NetworkMode != "container:"+h.current("p", "web").ID {
		t.Fatalf("the sidecar must follow the new parent: %+v", now)
	}
	up := h.events(engine.Upgraded)
	if len(up) != 1 {
		t.Fatalf("%v", h.rec.Types())
	}
	a := up[0].Attrs
	if a["sidecars"] != "side" || !strings.HasSuffix(a["rollback_up"], "up -d --no-deps --pull never --no-build web side") {
		t.Fatalf("the rollback must name the sidecar: %+v", a)
	}
}

// Only the sidecars that share the image and were created from the replaced one follow; the move is no one's pull.
func TestFollowedMoveIsNotNamedAsAnotherStacksMoverAndOnlyServesTheParentsSidecars(t *testing.T) {
	h, web, _ := upgradeWithSidecar(t)
	breakAndRun(h, web.ID, 6*time.Minute)

	m, ok := h.d.moveOf("web:1")
	if !ok || !m.Followed || m.Key != key("p", "web") {
		t.Fatalf("a followed move is recorded for the parent: %+v %v", m, ok)
	}
	// Another stack's container on the same reference is image-drifted by this move; the drift block must not say that
	// whalenurse pulled it.
	other := h.svc(hexID("f1"), "q", "app")
	other = h.set(other.ID, func(c *docker.Container) {
		c.Image, c.ImageID = "web:1", imgOldID
		c.Labels[compose.LabelImage] = imgLabel("web")
	})
	h.set(other.ID, unhealthy)
	h.advance(10 * time.Minute)
	for _, e := range h.events(engine.DriftBlocked) {
		if e.Key == key("q", "app") {
			if e.Attrs["moved_by"] != "" || strings.Contains(e.Message, "whalenurse moved this tag") {
				t.Fatalf("someone else moved that tag: %+v", e)
			}
			return
		}
	}
	t.Fatalf("the other stack should have been blocked by the moved tag: %v", h.rec.Types())
}
