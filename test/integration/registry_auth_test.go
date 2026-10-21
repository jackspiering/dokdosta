//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
)

// M7: the pull rung against a registry that wants a login (plan v3.12 item 9). The registry is registry:2 with htpasswd
// auth, a whalenurse-test-registry-* project on 127.0.0.1:<port> like the one of M4. The credentials are a throwaway
// user and password made for the test; they are written to two directories the test makes and removes: a docker config
// for the test's own pushes, and the WHALENURSE_REGISTRY_AUTH_DIR given to whalenurse.

const (
	regUser = "whalenurse-test-user"
	regPass = "reg-PASS-SECRET-93017"
)

// startAuthRegistry starts a registry:2 that requires regUser/regPass, and returns it with the "auth" value of its login.
func startAuthRegistry(t *testing.T, g *Guard, cli *docker.Moby) *testRegistry {
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
    environment:
      REGISTRY_AUTH: htpasswd
      REGISTRY_AUTH_HTPASSWD_REALM: whalenurse-test
      REGISTRY_AUTH_HTPASSWD_PATH: /auth/htpasswd
    volumes:
      - ./auth:/auth:ro
`)
	hash, err := bcrypt.GenerateFromPassword([]byte(regPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.dir, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "auth", "htpasswd"), []byte(regUser+":"+string(hash)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.up()
	port, err := g.PublishedPort(ctx, s.container("registry").ID, "5000/tcp")
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
		return resp.StatusCode == http.StatusUnauthorized
	})
	req, _ := http.NewRequest("GET", "http://"+r.host+"/v2/", nil)
	req.SetBasicAuth(regUser, regPass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("the registry must accept the test login: %v %v", resp, err)
	}
	resp.Body.Close()
	r.pushConfig = tempDir(t, DockerConfigPrefix)
	writeAuths(t, r.pushConfig, r.authsJSON(), 0o600)
	t.Logf("authenticated registry at %s", r.host)
	return r
}

func (r *testRegistry) authValue() string {
	return base64.StdEncoding.EncodeToString([]byte(regUser + ":" + regPass))
}

func (r *testRegistry) authsJSON() string {
	return `{"auths": {"` + r.host + `": {"auth": "` + r.authValue() + `"}}}`
}

func tempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755); _ = os.RemoveAll(dir) })
	return dir
}

func writeAuths(t *testing.T, dir, config string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), mode); err != nil {
		t.Fatal(err)
	}
}

// authDirFor makes a WHALENURSE_REGISTRY_AUTH_DIR that a process other than the owner can read and no one can write.
func (r *testRegistry) authDir(t *testing.T) string {
	t.Helper()
	dir := tempDir(t, "whalenurse-test-auth-")
	writeAuths(t, dir, r.authsJSON(), 0o444)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	return dir
}

// snapshot is the content, mode and size of everything under dir: "the mount stayed as it was" is a comparison of two.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sum := ""
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h := sha256.Sum256(b)
			sum = hex.EncodeToString(h[:])
		}
		lines = append(lines, fmt.Sprintf("%s %v %d %s %d", rel, fi.Mode(), fi.Size(), sum, fi.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// The positive control: without WHALENURSE_REGISTRY_AUTH_DIR the same stack, with a newer image waiting in the registry,
// cannot pull it. The pull fails with a login error, the container ends SUSPENDED, nothing moved. Without this, the next
// test could pass for a reason other than the credentials.
func TestPullRungOnAnAuthenticatedRegistryFailsWithoutTheCredentials(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startAuthRegistry(t, g, cli)
	ref := reg.ref("auth-without")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	localBefore, _ := g.ImageID(ctx, ref)
	s := newStack(t, g, cli, "pull-auth-without", pullApp(ref, true, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })

	r := startDaemonOpts(t, g, cli, daemonOpts{})
	waitFor(t, "the app to be SUSPENDED", 180*time.Second, func() bool { return countFor(r.Rec, engine.Suspended, s.key("app")) == 1 })
	started, results := pullEvents(r.Rec, s.key("app"))
	if got := joined(started); got != "restart,recreate,pull" {
		t.Fatalf("actions = %s, want restart,recreate,pull", got)
	}
	var pull engine.Event
	for _, e := range results {
		if e.Attrs["action"] == "pull" {
			pull = e
		}
	}
	t.Logf("the refused pull: %+v", pull)
	if pull.Attrs["ok"] != "false" {
		t.Fatalf("the pull must fail without credentials: %+v", pull)
	}
	reason := strings.ToLower(pull.Message + " " + eventMessage(r.Rec, engine.Suspended, s.key("app")))
	if !strings.Contains(reason, "unauthorized") && !strings.Contains(reason, "no basic auth") && !strings.Contains(reason, "denied") && !strings.Contains(reason, "authentication") {
		t.Fatalf("the failure should be a login failure: %q", reason)
	}
	if dirs := r.Compose.PullConfigDirs(); len(dirs) != 1 || dirs[0] != "" {
		t.Fatalf("without the variable the pull has no credentials directory: %v", dirs)
	}
	if countFor(r.Rec, engine.TagMoved, s.key("app")) != 0 {
		t.Fatal("nothing may move")
	}
	if now, _ := g.ImageID(ctx, ref); now != localBefore {
		t.Fatalf("the local image moved: %s -> %s", localBefore, now)
	}
	if v := s.version("app"); v != "v1" {
		t.Fatalf("the container runs %s, want v1", v)
	}
}

// With WHALENURSE_REGISTRY_AUTH_DIR (mode 0555, the file 0444) the same pull succeeds: the pull rung upgrades and recovers.
// Only the pull ran with the directory, and compose wrote nothing into it.
func TestPullRungLogsInWithTheRegistryAuthDirAndLeavesItAlone(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	reg := startAuthRegistry(t, g, cli)
	ref := reg.ref("auth-with")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	oldID, _ := g.ImageID(ctx, ref)
	dir := reg.authDir(t)
	before := snapshot(t, dir)
	s := newStack(t, g, cli, "pull-auth-with", pullApp(ref, true, false))
	s.up()
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })

	r := startDaemonOpts(t, g, cli, daemonOpts{Tweak: func(c *config.Global) { c.RegistryAuthDir = dir }})
	waitFor(t, "the app to be healthy on the new image", 240*time.Second, func() bool {
		return countFor(r.Rec, engine.TagMoved, s.key("app")) == 1 && s.container("app").Health == "healthy"
	})
	waitFor(t, "the Recovered event", 60*time.Second, func() bool { return countFor(r.Rec, engine.Recovered, s.key("app")) == 1 })
	if countFor(r.Rec, engine.Suspended, s.key("app")) != 0 {
		t.Fatal("it recovered; it must not be SUSPENDED")
	}
	if v := s.version("app"); v != "v2" {
		t.Fatalf("the container runs %s, want v2", v)
	}
	if now, _ := g.ImageID(ctx, ref); now == oldID {
		t.Fatal("the local reference did not move")
	}
	if dirs := r.Compose.PullConfigDirs(); len(dirs) != 1 || dirs[0] != dir {
		t.Fatalf("the pull must run with the credentials directory: %v", dirs)
	}
	if after := snapshot(t, dir); after != before {
		t.Fatalf("compose changed the credentials directory:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// The real deployment shape: the image, a read-only root filesystem, uid 65532, the credentials directory mounted :ro. The
// containerized whalenurse pulls from the authenticated registry; the mount is unchanged; the password is nowhere in its
// log or in `status`; `status` names the registry.
func TestImagePullsFromAnAuthenticatedRegistryWithAReadOnlyMount(t *testing.T) {
	g, cli := requireIntegration(t)
	ctx := context.Background()
	img := os.Getenv("WHALENURSE_TEST_IMAGE")
	if img == "" {
		t.Skip("set WHALENURSE_TEST_IMAGE to a locally built image to test it")
	}
	reg := startAuthRegistry(t, g, cli)
	ref := reg.ref("auth-image")
	reg.publish(t, ref, "v1")
	reg.publishNewer(t, ref, "v2")
	dir := reg.authDir(t)
	before := snapshot(t, dir)
	s := authImageStack(t, g, cli, "imgauth", img, dir, reg.authsJSON(), pullAppWith(ref, pullOpts{pull: true}))
	s.up("app")
	waitFor(t, "app to be unhealthy", 30*time.Second, func() bool { return s.container("app").Health == "unhealthy" })
	s.up("whalenurse")
	id := s.container("whalenurse").ID
	if !inspectReadonly(t, id) {
		t.Fatal("the image test must run with a read-only root filesystem")
	}
	if out, err := g.R.Run(ctx, "docker", "inspect", "--format", `{{range .Mounts}}{{if eq .Destination "/registry-auth"}}{{.RW}}{{end}}{{end}}`, id); err != nil || strings.TrimSpace(out) != "false" {
		t.Fatalf("the credentials must be mounted read-only: %q %v", out, err)
	}
	waitFor(t, "the app to run the new image", 300*time.Second, func() bool { return s.tryVersion("app") == "v2" })
	waitFor(t, "the app to be healthy", 120*time.Second, func() bool { return s.container("app").Health == "healthy" })

	if after := snapshot(t, dir); after != before {
		t.Fatalf("compose changed the credentials directory:\n--- before\n%s\n--- after\n%s", before, after)
	}
	logs, err := exec.Command("docker", "logs", id).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.Ctr(ctx, "exec", id, "/whalenurse", "status")
	if err != nil {
		t.Fatal(err)
	}
	js, err := g.Ctr(ctx, "exec", id, "/whalenurse", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"log": string(logs), "status": out, "status --json": js} {
		for _, secret := range []string{regPass, reg.authValue()} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks %q", name, secret)
			}
		}
	}
	if !strings.Contains(out, "pull credentials: "+reg.host) || !strings.Contains(js, `"`+reg.host+`"`) {
		t.Errorf("status should name the registry %s:\n%s\n%s", reg.host, out, js)
	}
	if strings.Contains(out, regUser) || strings.Contains(js, regUser) {
		t.Error("status shows the user name")
	}
	if !strings.Contains(string(logs), "registry credentials for the pull rung") {
		t.Errorf("the log should say the credentials were loaded:\n%s", logs)
	}
}

// A credentials file with more than inline "auths" stops the image at startup with a message that names the key and never
// the credential.
func TestImageRefusesACredentialsFileWithACredsStore(t *testing.T) {
	g, cli := requireIntegration(t)
	img := os.Getenv("WHALENURSE_TEST_IMAGE")
	if img == "" {
		t.Skip("set WHALENURSE_TEST_IMAGE to a locally built image to test it")
	}
	dir := tempDir(t, "whalenurse-test-auth-")
	writeAuths(t, dir, `{"auths": {"127.0.0.1:1": {"auth": "`+base64.StdEncoding.EncodeToString([]byte(regUser+":"+regPass))+`"}}, "credsStore": "pass"}`, 0o444)
	s := authImageStack(t, g, cli, "imgauthbad", img, dir, "", "")
	s.up("whalenurse")
	id := s.container("whalenurse").ID
	waitFor(t, "whalenurse to exit", 60*time.Second, func() bool { return !s.container("whalenurse").Running })
	logs, _ := exec.Command("docker", "logs", id).CombinedOutput()
	text := string(logs)
	if !strings.Contains(text, "WHALENURSE_REGISTRY_AUTH_DIR") || !strings.Contains(text, `"credsStore"`) {
		t.Fatalf("the refusal should name the variable and the key:\n%s", text)
	}
	if strings.Contains(text, regPass) {
		t.Fatalf("the refusal leaks the password:\n%s", text)
	}
}

// authImageStack is a project with the whalenurse image (read-only root, nonroot, the socket's group, the host's compose
// plugin and the stacks directory read-only at the identical path) and dir mounted read-only at /registry-auth. services
// is more of the project's compose file ("" for none).
func authImageStack(t *testing.T, g *Guard, cli *docker.Moby, name, img, dir, _ string, services string) *stack {
	t.Helper()
	if !strings.HasPrefix(img, TagPrefix) {
		t.Fatalf("image %q: only whalenurse-test/* local tags are used", img)
	}
	fi, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	gid := fi.Sys().(*syscall.Stat_t).Gid
	s := newStack(t, g, cli, name, "services: {}")
	body := fmt.Sprintf(`
services:
  whalenurse:
    image: %s
    restart: "no"
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    group_add: ["%d"]
    tmpfs: ["/run:uid=65532,gid=65532,mode=0700"]
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock"
      - "data:/data"
      - "%s:%s:ro"
      - "%s:%s:ro"
      - "%s:/registry-auth:ro"
    environment:
%s      WHALENURSE_REGISTRY_AUTH_DIR: /registry-auth
%s
volumes:
  data: {}
`, img, gid, s.dir, s.dir, composeBin(t).Path, compose.HostMountPath, dir, imageEnv(), strings.TrimPrefix(services, "\nservices:"))
	if err := os.WriteFile(s.dir+"/compose.yaml", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}
