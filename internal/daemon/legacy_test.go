package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/compose"
	"github.com/jackspiering/dokdosta/internal/config"
	"github.com/jackspiering/dokdosta/internal/docker"
	"github.com/jackspiering/dokdosta/internal/engine"
	"github.com/jackspiering/dokdosta/internal/rules"
)

// Plan v3.15 item 5: a container that still carries labels from before the rename is left alone, whatever else it
// carries, and says why once. Under DOKDOSTA_MODE=all an ignored whalenurse.enable=false would otherwise be healed.
func TestContainersWithLabelsFromBeforeTheRenameAreLeftAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tweak  func(*config.Global)
		labels map[string]string
	}{
		{"mode all, the old opt-out", modeAll, map[string]string{"whalenurse.enable": "false"}},
		{"mode label, the old opt-in", nil, map[string]string{"whalenurse.enable": "true"}},
		{"old and new labels together", nil, map[string]string{"whalenurse.enable": "false", config.LabelEnable: "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCompose(t, tc.tweak, nil, docker.ServerInfo{})
			c := h.plain(hexID("a1"), "old", tc.labels)
			h.breaks(c)
			h.tick() // a second pass: still one warning
			h.wantCalls()
			if h.count(engine.IncidentOpened) != 0 {
				t.Fatal(h.rec.Types())
			}
			if n := strings.Count(h.log.String(), "labels from before the rename (whalenurse.enable)"); n != 1 {
				t.Fatalf("want one warning naming the key, got %d:\n%s", n, h.log.String())
			}
			_, seen := h.d.view.snapshot()
			s, ok := seen[docker.StateKey(host, c.Name, c.Labels)]
			if !ok || s.dec.Skip != rules.SkipLegacy || !strings.Contains(s.dec.Reason, "renamed dokdosta") {
				t.Fatalf("the reason must reach the view: %+v", s.dec)
			}
		})
	}
}

// The positive control: the same container with the new label is healed.
func TestTheNewLabelIsHealedWhereTheOldOneIsNot(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	c := h.plain(hexID("a1"), "new", map[string]string{config.LabelEnable: "true"})
	h.breaks(c)
	h.wantCalls("restart " + c.ID)
}

// A whalenurse.* label that the image ships does not count (plan v3.13 item 10 applies to the old namespace too).
func TestOldLabelFromTheImageDoesNotCount(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	h.fake.SetImageLabels(imgID, map[string]string{"whalenurse.enable": "true"})
	c := h.plain(hexID("a1"), "img", map[string]string{config.LabelEnable: "true", "whalenurse.enable": "true"})
	h.breaks(c)
	h.wantCalls("restart " + c.ID)
}

// The dependents step does not repair a sidecar with old labels: one that exited on its own is recreated with its
// parent (plan v3.13 item 2b, the control), one with an old label is not.
func TestSidecarWithLabelsFromBeforeTheRenameIsNotRepaired(t *testing.T) {
	for _, tc := range []struct {
		name   string
		old    bool
		wantUp int
	}{{"control", false, 1}, {"old label", true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCompose(t, nil, nil, docker.ServerInfo{})
			_, dep := h.pair()
			dep = h.noOptIn(dep.ID)
			if tc.old {
				dep = h.set(dep.ID, func(c *docker.Container) { c.Labels["whalenurse.enable"] = "true" })
			}
			var newParent docker.Container
			h.cf.OnUp = func(compose.Project) error {
				h.replace("p", "dep", func(c *docker.Container) { c.NetworkMode = "container:" + newParent.ID })
				return nil
			}
			h.tick()
			dep = h.set(dep.ID, func(c *docker.Container) { c.Status, c.Running, c.ExitCode = "exited", false, 1 })
			h.event(dep, "die", "")
			h.advance(time.Minute)
			newParent = h.replace("p", "parent", func(c *docker.Container) { c.Health = "healthy" })
			h.event(newParent, "create", "")
			h.event(newParent, "start", "")
			h.advance(3 * time.Minute)
			if got := h.cf.Count("up"); got != tc.wantUp {
				t.Fatalf("up calls = %d, want %d (%s) / %v", got, tc.wantUp, h.verbs(), h.rec.Types())
			}
		})
	}
}
