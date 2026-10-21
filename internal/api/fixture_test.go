package api

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/engine"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fixtureSource is a hand-made host: a healthy container, a SUSPENDED parent with a sidecar, a paused one, and one
// that whalenurse does not watch (with labels that must never come out).
type fixtureSource struct{}

func (fixtureSource) Status() StatusDoc {
	return StatusDoc{
		Host: "nas", Version: "v-test", Time: t0.Add(time.Hour), StartedAt: t0, Mode: "label", Persistent: true, Healthy: true, HealthDetail: "ok",
		EventsUp: true, Settled: true, DriftGuard: "strict",
		Compose:     ComposeDoc{Binary: "/host/docker-compose", Source: "host-plugin", Version: "v5.6.0", Engine: "29.9.1", Arch: "amd64"},
		VersionGate: VersionGateDoc{RecreateEnabled: true, MinEngine: "28.1.0", MinCompose: "5.2.0"},
		RateLimit:   RateLimitDoc{Limit: 10, Used: 3, Remaining: 7},
		Counts:      CountsDoc{Containers: 4, Watched: 3, OK: 1, Incident: 0, Suspended: 1, Paused: 1},
		Pauses:      []PauseDoc{{Host: "nas", Scope: "web/proxy", Created: t0.Add(10 * time.Minute), Until: t0.Add(2*time.Hour + 10*time.Minute)}},
		Notifications: NotificationsDoc{
			Enabled: true, URLs: 2, Sent: 5, Failed: 1, UptimePing: &PingDoc{Sent: 12, IntervalSeconds: 300},
		},
		PullAuth:         PullAuthDoc{Registries: []string{"ghcr.io", "registry.example.lan:5000"}},
		HistoryRetention: 30 * 24 * 3600,
	}
}

func (fixtureSource) Containers() []ContainerDoc {
	compose := map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": "vpn", "whalenurse.enable": "true"}
	return []ContainerDoc{
		{
			Host: "nas", Key: "nas/media/jellyfin/1", Name: "media-jellyfin-1", ID: "aaaaaaaaaaaa", Project: "media", Service: "jellyfin", Number: "1",
			Image: "jellyfin/jellyfin:10", State: "running", Health: "healthy", Policy: "unless-stopped", StartedAt: t0, Watched: true, OptedIn: true,
			Ladder: []string{"restart", "recreate"}, HealExited: "crashed", Healer: HealerDoc{State: "ok", FlapLimit: 3},
			Decision: &DecisionDoc{Class: "ok", Reason: "healthy", At: t0.Add(30 * time.Minute)},
		},
		{
			Host: "nas", Key: "nas/media/vpn/1", Name: "media-vpn-1", ID: "bbbbbbbbbbbb", Project: "media", Service: "vpn", Number: "1",
			Image: "qmcgaw/gluetun:latest", State: "running", Health: "unhealthy", RestartCount: 2, ExitCode: 1, Policy: "always", StartedAt: t0,
			Watched: true, OptedIn: true, Ladder: []string{"restart", "recreate", "pull"}, HealExited: "crashed",
			Healer: HealerDoc{
				State: "SUSPENDED", Class: "unhealthy", IncidentOpened: t0.Add(20 * time.Minute), RungsRun: 2, CooldownUntil: t0.Add(25 * time.Minute),
				Flaps: 1, FlapLimit: 3, Suspended: &SuspendDoc{Reason: "drift", Since: t0.Add(24 * time.Minute)},
			},
			Decision:   &DecisionDoc{Class: "unhealthy", Action: "restart", Reason: "healthcheck failing", At: t0.Add(30 * time.Minute)},
			Dependents: []RelationDoc{{Host: "nas", Key: "nas/media/qbittorrent/1", Name: "media-qbittorrent-1"}},
			Labels:     compose,
		},
		{
			Host: "nas", Key: "nas/media/qbittorrent/1", Name: "media-qbittorrent-1", ID: "cccccccccccc", Project: "media", Service: "qbittorrent", Number: "1",
			Image: "lscr.io/linuxserver/qbittorrent", State: "running", Watched: true, Ladder: []string{"restart", "recreate"}, HealExited: "crashed",
			Healer: HealerDoc{State: "ok", FlapLimit: 3}, Parent: &RelationDoc{Host: "nas", Key: "nas/media/vpn/1", Name: "media-vpn-1"},
		},
		{
			Host: "nas", Key: "nas/web/proxy/1", Name: "web-proxy-1", ID: "dddddddddddd", Project: "web", Service: "proxy", Number: "1",
			Image: "caddy:2", State: "running", Watched: true, OptedIn: true, Ladder: []string{"restart", "recreate"}, HealExited: "crashed",
			Healer: HealerDoc{State: "PAUSED", FlapLimit: 3, Paused: &PausedDoc{Scope: "web/proxy", Since: t0.Add(10 * time.Minute), Until: t0.Add(2*time.Hour + 10*time.Minute)}},
		},
		{
			Host: "nas", Key: "nas/other-1", Name: "other-1", ID: "eeeeeeeeeeee", Image: "busybox", State: "running",
			Healer: HealerDoc{State: "ok", FlapLimit: 3},
			Labels: map[string]string{"com.example.password": "hunter2-LABEL", "traefik.http.routers.x.rule": "Host(`secret.example.org`)", "com.docker.compose.project": "x"},
		},
	}
}

// fixtureEvents is the event log behind the fixture: the story of the vpn container.
func fixtureEvents() *engine.Ring {
	r := engine.NewRing(100)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	for i, e := range []engine.Event{
		{Time: at(20), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.IncidentOpened, Message: "unhealthy", Attrs: map[string]string{"class": "unhealthy", "action": "restart", "incident": at(20).Format(time.RFC3339Nano)}},
		{Time: at(21), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.ActionResult, Message: "restarted", Attrs: map[string]string{"action": "restart", "rung": "restart", "ok": "true"}},
		{Time: at(22), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.TagMoved, Message: "qmcgaw/gluetun:latest: aaa moved to bbb; recreated",
			Attrs: map[string]string{"ref": "qmcgaw/gluetun:latest", "old": "sha256:aaaaaaaaaaaaaaaa", "new": "sha256:bbbbbbbbbbbbbbbb", "ok": "true", "old_image": "sha256:aaaaaaaaaaaaaaaa",
				"rollback_tag": "docker image tag sha256:aaaaaaaaaaaaaaaa qmcgaw/gluetun:latest", "rollback_up": "docker compose -p media --project-directory /opt/stacks/media -f compose.yaml up -d --no-deps --pull never vpn", "sidecars": "qbittorrent"}},
		{Time: at(23), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.DriftBlocked, Message: "recreate skipped by the drift guard: image moved",
			Attrs: map[string]string{"class": "unhealthy", "findings": "image moved", "moved_by": "nas/media/other/1", "moved_at": at(5).Format(time.RFC3339), "moved_ref": "qmcgaw/gluetun:latest"}},
		{Time: at(24), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.Suspended, Message: "giving up: recreate skipped by the drift guard (image moved)", Attrs: map[string]string{"reason": "drift"}},
		{Time: at(30), Host: "nas", Key: "nas/web/proxy/1", Container: "web-proxy-1", Type: engine.ActionResult, Message: "restarted", Attrs: map[string]string{"action": "restart", "ok": "true"}},
		{Time: at(31), Host: "nas", Key: "nas/web/Proxy 2", Type: engine.Paused, Message: "paused web/proxy", Attrs: map[string]string{"scope": "web/proxy"}},
	} {
		e.ID = uint64(i + 1)
		r.Record(e)
	}
	return r
}

type fixtureHistory struct{}

func (fixtureHistory) History(key string) ([]engine.ClosedIncident, error) {
	all := []engine.ClosedIncident{
		{Key: "nas/media/jellyfin/1", Opened: t0.Add(-48 * time.Hour), Closed: t0.Add(-47 * time.Hour), Class: "unhealthy", Outcome: "recovered"},
		{Key: "nas/media/vpn/1", Opened: t0.Add(-72 * time.Hour), Closed: t0.Add(-71 * time.Hour), Class: "crashed", Outcome: "recovered"},
	}
	var out []engine.ClosedIncident
	for _, c := range all {
		if key == "" || c.Key == key {
			out = append(out, c)
		}
	}
	return out, nil
}

func fixtureService() *Service {
	return &Service{Host: "nas", Source: fixtureSource{}, Events: fixtureEvents(), History: fixtureHistory{}, Hub: NewHub(), Metrics: true}
}

// golden compares got with testdata/golden/<name>; -update rewrites it.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/api -update)", err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the golden file (run go test ./internal/api -update to accept):\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func engineRing() *engine.Ring { return engine.NewRing(100) }

func engineEvent(id uint64, typ, msg string, attrs map[string]string) engine.Event {
	return engine.Event{ID: id, Time: t0.Add(time.Duration(id) * time.Minute), Host: "nas", Key: "nas/media/vpn/1", Container: "media-vpn-1", Type: engine.EventType(typ), Message: msg, Attrs: attrs}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
