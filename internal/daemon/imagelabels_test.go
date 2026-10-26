package daemon

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
)

const imgID = "sha256:img0000000000000000000000000000000000000000000000000000000000000"

// plain puts a container that is not from compose, running imgID, with the labels given.
func (h *cfH) plain(id, name string, labels map[string]string) docker.Container {
	c := docker.Container{ID: id, Name: name, Labels: labels, Status: "running", Running: true, ImageID: imgID,
		StartedAt: h.clk.Now().Add(-time.Hour), Created: h.clk.Now().Add(-time.Hour)}
	h.fake.Put(c)
	return c
}

func (h *cfH) breaks(c docker.Container) {
	h.tick()
	h.set(c.ID, unhealthy)
	h.advance(10 * time.Minute)
}

// WN-005, plan v3.13 item 10: a whalenurse.* label that has the value the image gives it is the image publisher's, not the
// operator's: it does not opt the container in, and one warning names it.
func TestImageProvidedLabelsDoNotOptIn(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	shipped := map[string]string{config.LabelEnable: "true", config.LabelHealExited: "always"}
	h.fake.SetImageLabels(imgID, shipped)
	c := h.plain(hexID("a1"), "stranger", map[string]string{config.LabelEnable: "true", config.LabelHealExited: "always"})
	h.breaks(c)
	h.wantCalls()
	if h.count(engine.IncidentOpened) != 0 {
		t.Fatal(h.rec.Types())
	}
	if n := strings.Count(h.log.String(), "labels from the image do not steer whalenurse"); n != 1 || !strings.Contains(h.log.String(), config.LabelEnable) {
		t.Fatalf("want one warning naming %s, got %d:\n%s", config.LabelEnable, n, h.log.String())
	}
}

// The positive control: the operator's own label, with a value the image does not give, is honoured.
func TestOperatorLabelThatDiffersFromTheImageIsHonoured(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	h.fake.SetImageLabels(imgID, map[string]string{config.LabelEnable: "false"})
	c := h.plain(hexID("a1"), "mine", map[string]string{config.LabelEnable: "true"})
	h.breaks(c)
	h.wantCalls("restart " + c.ID)
	if strings.Contains(h.log.String(), "do not steer") {
		t.Fatal(h.log.String())
	}
}

// An image with no labels, or one that is gone, changes nothing.
func TestNoImageLabelsChangeNothing(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	c := h.plain(hexID("a1"), "mine", map[string]string{config.LabelEnable: "true"}) // the Fake knows no such image
	h.breaks(c)
	h.wantCalls("restart " + c.ID)
}

// An image committed from a compose container (or one that claims a compose identity) carries com.docker.compose.* labels.
// The container is then not made by compose: its key is its name, it gets rung 1 only, and compose never runs for it (the
// service label of an image could be a flag, WN-001).
func TestComposeIdentityFromTheImageMakesItNonCompose(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	ident := map[string]string{
		compose.LabelProject: "victim", compose.LabelService: "--dry-run=false", docker.LabelNumber: "1",
		compose.LabelWorkingDir: "/opt/stacks/victim", compose.LabelConfigFiles: "/opt/stacks/victim/compose.yaml",
	}
	h.fake.SetImageLabels(imgID, ident)
	own := map[string]string{config.LabelEnable: "true"} // the operator's own opt-in, a value the image does not give
	for k, v := range ident {
		own[k] = v
	}
	c := h.plain(hexID("a1"), "imposter", own)
	h.breaks(c)
	h.wantCalls("restart " + c.ID)
	if len(h.cf.Calls()) != 0 {
		t.Fatalf("compose ran for a container whose compose identity is the image's: %s", h.verbs())
	}
	for _, e := range h.rec.Events() {
		if e.Key != "" && e.Key != host+"/imposter" {
			t.Errorf("event %s has key %q, want the container name", e.Type, e.Key)
		}
	}
	if ru := h.events(engine.RungUnavailable); len(ru) == 0 || !strings.Contains(ru[0].Message, "not a compose container") {
		t.Fatalf("%+v", ru)
	}
}

// A compose container takes a whalenurse.* label only if the compose file declares it.
func TestComposeContainerNeedsTheLabelDeclaredInTheComposeFile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared []string
		err      error
		image    map[string]string
		own      string // the container's value
		want     bool   // honoured
	}{
		{"declared, and the image ships the same value", []string{config.LabelEnable}, nil, map[string]string{config.LabelEnable: "true"}, "true", true},
		{"not declared, the image ships it", nil, nil, map[string]string{config.LabelEnable: "true"}, "true", false},
		{"not declared, the image does not ship it", nil, nil, nil, "true", false},
		{"declared elsewhere only", []string{config.LabelCooldown}, nil, map[string]string{config.LabelEnable: "true"}, "true", false},
		{"compose cannot be read, the image ships it", nil, errors.New("boom"), map[string]string{config.LabelEnable: "true"}, "true", false},
		{"compose cannot be read, the value differs from the image's", nil, errors.New("boom"), map[string]string{config.LabelEnable: "false"}, "true", true},
		{"compose cannot be read, the image ships nothing", nil, errors.New("boom"), nil, "true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCompose(t, nil, nil, docker.ServerInfo{})
			h.cf.DeclaredFn = func(compose.Project) ([]string, error) { return tc.declared, tc.err }
			if tc.image != nil {
				h.fake.SetImageLabels(imgID, tc.image)
			}
			web := h.svc(hexID("a1"), "p", "web", config.LabelEnable, tc.own)
			h.set(web.ID, func(c *docker.Container) { c.ImageID = imgID })
			h.breaks(web)
			got := len(h.fake.Calls()) > 0
			if got != tc.want {
				t.Fatalf("honoured = %v, want %v (calls %v)", got, tc.want, h.fake.Calls())
			}
		})
	}
}

// The compose file is read once per config-hash, not on every poll, and not at all for a container with no whalenurse.* label.
func TestDeclaredLabelsAreReadOncePerConfigHash(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	var reads atomic.Int32
	h.cf.DeclaredFn = func(compose.Project) ([]string, error) { reads.Add(1); return []string{config.LabelEnable}, nil }
	h.fake.SetImageLabels(imgID, map[string]string{config.LabelEnable: "true"})
	web := h.svc(hexID("a1"), "p", "web")
	h.set(web.ID, func(c *docker.Container) { c.ImageID = imgID })
	for range 5 {
		h.tick()
		h.advance(2 * time.Minute)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads, want 1", n)
	}
	if n := h.cf.Count("policy") + h.cf.Count("dry-run") + h.cf.Count("hash"); n != 0 {
		t.Fatalf("the label read must not count as a rung's compose call: %s", h.verbs())
	}
}

// A failed read is not repeated on every poll.
func TestFailedDeclaredReadIsNotRepeatedEveryPoll(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	var reads atomic.Int32
	h.cf.DeclaredFn = func(compose.Project) ([]string, error) { reads.Add(1); return nil, errors.New("boom") }
	h.fake.SetImageLabels(imgID, map[string]string{config.LabelEnable: "true"})
	web := h.svc(hexID("a1"), "p", "web")
	h.set(web.ID, func(c *docker.Container) { c.ImageID = imgID })
	for range 4 {
		h.tick()
		h.advance(time.Minute)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d reads in 4 minutes, want 1", n)
	}
}

// The dependents step asks whether the parent is opted in with the same filtered labels: a parent whose opt-in is the
// image's does not pull its sidecar into the step.
func TestParentOptInThatIsTheImagesDoesNotCountForItsSidecars(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	h.fake.SetImageLabels(imgID, map[string]string{config.LabelEnable: "true"})
	parent, dep := h.pair()
	h.set(parent.ID, func(c *docker.Container) { c.ImageID = imgID })
	h.set(dep.ID, func(c *docker.Container) { delete(c.Labels, config.LabelEnable) })
	h.cf.DeclaredFn = func(compose.Project) ([]string, error) { return nil, nil } // the compose file declares no label
	h.tick()
	parentNow := h.replace("p", "parent", nil) // a recreate: the sidecar is stale
	h.set(parentNow.ID, func(c *docker.Container) { c.ImageID = imgID })
	h.tick()
	h.advance(5 * time.Minute)
	if h.cf.Count("up") != 0 || len(h.fake.Calls()) != 0 {
		t.Fatalf("the sidecar was repaired for a parent whose opt-in is the image's: %s %v", h.verbs(), h.fake.Calls())
	}
}
