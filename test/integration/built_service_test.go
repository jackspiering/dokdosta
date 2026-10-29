//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/docker"
	"github.com/jackspiering/dokdosta/internal/engine"
)

// syncBuffer is a log destination the healer writes from its own goroutines while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Compose writes com.docker.compose.project, .service and .version into every image it builds, and Docker copies an
// image's labels onto each container made from it. A service with `build:` must still be a compose container to dokdosta
// (plan v3.16 item 1, B-001): its key is the compose key, rung 2 is available and the log has no warning that the labels
// come from the image; and a one-off made from that image keeps its oneoff=True label, so SkipOneOff still protects it.
// Before B-001 the three shared labels made every compose label look image-supplied, and the container fell back to its
// name, rung 1 only.
func TestBuiltServiceKeepsItsComposeIdentity(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()

	var b [3]byte
	_, _ = rand.Read(b[:])
	tag := "dokdosta-test/built-" + hex.EncodeToString(b[:]) + ":latest"
	// Registered before the stack, so it runs after the stack's cleanup removed the containers that use the image.
	t.Cleanup(func() {
		if err := g.Untag(context.Background(), tag); err != nil {
			t.Logf("cleanup of %s: %v", tag, err)
		}
	})
	s := newStack(t, g, cli, "built", fmt.Sprintf(`services:
  app:
    build: .
    image: %s
    labels:
      dokdosta.enable: "true"
    stop_grace_period: 2s
    command: %s
    healthcheck:
      test: ["CMD", "sh", "-c", "test ! -f /broken"]
      interval: 1s
      timeout: 1s
      retries: 1
`, tag, loopCmd))
	if err := os.WriteFile(filepath.Join(s.dir, "Dockerfile"), []byte("FROM busybox:1.37\nRUN echo built > /built\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ComposeBuild(ctx, s.project, s.dir, "app", tag); err != nil {
		t.Fatal(err)
	}

	// The premise: compose put its identity into the image, so the container shares those labels with it.
	out, err := g.R.Run(ctx, "docker", "image", "inspect", "--format", "{{json .Config.Labels}}", tag)
	if err != nil {
		t.Fatal(err)
	}
	var imageLabels map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &imageLabels); err != nil {
		t.Fatalf("image labels %q: %v", out, err)
	}
	if imageLabels[docker.LabelProject] != s.project || imageLabels[docker.LabelService] != "app" {
		t.Fatalf("compose did not label the image it built with its project and service: %v", imageLabels)
	}
	t.Logf("image labels: %v", imageLabels)

	s.up()
	waitFor(t, "app to be healthy", 60*time.Second, func() bool { return s.container("app").Health == "healthy" })
	before := s.container("app")
	breakWritableLayer(t, g, s, "app")

	logs := &syncBuffer{}
	r := startDaemonOpts(t, g, cli, daemonOpts{Log: slog.New(slog.NewTextHandler(logs, nil))})
	waitFor(t, "the app to be recreated and healthy", 150*time.Second, func() bool {
		c := s.container("app")
		return c.ID != before.ID && c.Health == "healthy"
	})
	waitFor(t, "the Recovered event", 60*time.Second, func() bool { return countFor(r.Rec, engine.Recovered, s.key("app")) == 1 })
	r.Stop()

	// Events carry the compose key (host/project/service/1); a name key would not have matched s.key and nothing above
	// would have been seen. Rung 2 ran, so it was available.
	if got := joined(actionsFor(r.Rec, s.key("app"))); got != "restart,recreate" {
		t.Fatalf("actions = %s, want restart,recreate", got)
	}
	if n := countFor(r.Rec, engine.RungUnavailable, s.key("app")); n != 0 {
		t.Fatalf("a rung is unavailable for a service built by compose: %d", n)
	}
	if countFor(r.Rec, engine.Suspended, s.key("app")) != 0 {
		t.Fatal("it recovered; it must not be SUSPENDED")
	}
	if log := logs.String(); strings.Contains(log, "come from the image") || strings.Contains(log, "do not steer") {
		t.Fatalf("the log says labels come from the image:\n%s", log)
	}

	// A one-off from the same image: compose writes oneoff=True (and no container-number), and dokdosta must still see it.
	// This comes last: stack.container returns the first container of the service, one-off or not.
	s.compose("run", "-d", "--no-deps", "app", "sleep", "300")
	filtered := &docker.ImageLabelFilter{Reader: cli}
	list, err := filtered.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, sm := range list {
		if sm.Labels[docker.LabelProject] != s.project {
			continue
		}
		raw, err := cli.Inspect(ctx, sm.ID)
		if err != nil {
			t.Fatal(err)
		}
		if raw.Labels["com.docker.compose.oneoff"] != "True" {
			continue
		}
		found++
		t.Logf("one-off labels: container-number=%q", raw.Labels["com.docker.compose.container-number"])
		if got := sm.Labels["com.docker.compose.oneoff"]; got != "True" {
			t.Fatalf("the one-off lost its oneoff label (%q), so it would be healed as a service", got)
		}
		if sm.Labels[docker.LabelService] != "app" {
			t.Fatalf("the one-off lost its compose identity: %v", sm.Labels)
		}
	}
	if found != 1 {
		t.Fatalf("found %d one-off containers, want 1", found)
	}
}
