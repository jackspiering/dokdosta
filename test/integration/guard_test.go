package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackspiering/dokdosta/internal/compose"
	"github.com/jackspiering/dokdosta/internal/config"
	"github.com/jackspiering/dokdosta/internal/docker"
)

// stub is a Runner that records argv and answers inspect lookups from a table.
type stub struct {
	calls    []string
	projects map[string]string // container -> compose project label
	context  string
	psOut    string
	psRows   []psRow // when set, `docker ps` honours --filter label=<k>[=<v>] and --format over these rows
}

// psRow is a container as `docker ps -a` sees it: its ID, its project label and its dokdosta.enable value.
type psRow struct{ id, project, enable string }

// psFiltered answers `docker ps` the way Docker does: a label filter with a value matches that exact string, one
// without matches the key; the --format string is rendered for the two labels and the ID the preflight reads.
func (s *stub) psFiltered(args []string) string {
	var key, val string
	var hasVal bool
	format := ""
	for i, a := range args {
		if a == "--filter" && i+1 < len(args) {
			k, v, ok := strings.Cut(strings.TrimPrefix(args[i+1], "label="), "=")
			key, val, hasVal = k, v, ok
		}
		if a == "--format" && i+1 < len(args) {
			format = args[i+1]
		}
	}
	var b strings.Builder
	for _, r := range s.psRows {
		if key == "dokdosta.enable" && r.enable == "" {
			continue // the label is absent
		}
		if key == "dokdosta.enable" && hasVal && r.enable != val {
			continue
		}
		line := strings.NewReplacer("{{.ID}}", r.id,
			`{{.Label "com.docker.compose.project"}}`, r.project,
			`{{.Label "dokdosta.enable"}}`, r.enable, `\t`, "\t").Replace(format)
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (s *stub) Run(_ context.Context, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, line)
	switch {
	case strings.HasPrefix(line, "docker context show"):
		return s.context + "\n", nil
	case strings.HasPrefix(line, "docker inspect --format {{index .Config.Labels"):
		p, ok := s.projects[args[len(args)-1]]
		if !ok {
			return "", errors.New("no such container")
		}
		return p + "\n", nil
	case strings.HasPrefix(line, "docker inspect --format {{.Id}}"):
		return "id-" + args[len(args)-1] + "\n", nil
	case strings.HasPrefix(line, "docker ps"):
		if s.psRows != nil {
			return s.psFiltered(args), nil
		}
		return s.psOut, nil
	case strings.HasPrefix(line, "docker port"):
		return "127.0.0.1:49153\n[::1]:49153\n", nil
	}
	return "", nil
}

func (s *stub) mutations() []string {
	var out []string
	for _, c := range s.calls {
		if !strings.HasPrefix(c, "docker inspect") && !strings.HasPrefix(c, "docker context") && !strings.HasPrefix(c, "docker ps") {
			out = append(out, c)
		}
	}
	return out
}

func refused(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("want a REFUSED error, got %v", err)
	}
}

var badProjects = []string{
	"", "dokdosta-test-", "dokdosta-test", "prod", "default", "dokdosta", "dokdosta-m0-e1", "xdokdosta-test-a",
	"dokdosta-test-A", "dokdosta-test-a b", "dokdosta-test-a/b", "dokdosta-test-a\nprod", "dokdosta-test--x", "DOKDOSTA-TEST-a",
	"dokdosta-test-a;rm", "../dokdosta-test-a", "dokdosta-test-é",
	// The names from before the rename (plan v3.15) are strangers too.
	"whalenurse-test-a", "whalenurse-m0-e1",
}

func TestProjectRegexRejectsBadNames(t *testing.T) {
	for _, p := range badProjects {
		if err := RequireProject(p); err == nil {
			t.Errorf("project %q was accepted", p)
		}
	}
	for _, p := range []string{"dokdosta-test-a", "dokdosta-test-heal-3f2a", "dokdosta-test-0_x-y"} {
		if err := RequireProject(p); err != nil {
			t.Errorf("project %q was refused: %v", p, err)
		}
	}
}

func TestComposeRefusesBadProjectsAndSmuggling(t *testing.T) {
	for _, p := range badProjects {
		s := &stub{}
		g := &Guard{R: s}
		_, err := g.Compose(context.Background(), p, "ps")
		refused(t, err)
		refused(t, g.Down(context.Background(), p))
		if len(s.calls) != 0 {
			t.Errorf("project %q reached docker: %v", p, s.calls)
		}
	}
	good := "dokdosta-test-a"
	for _, args := range [][]string{
		{"-p", "prod", "ps"}, {"-pprod", "ps"}, {"--project-name", "prod"}, {"--project-name=prod", "ps"},
		{"down"}, {"-f", "x.yml", "down", "-v"}, {"rm", "-f"}, {"up", "-d", "rm"},
		{"build", "app"}, {"up", "-d", "--build"},
	} {
		s := &stub{}
		g := &Guard{R: s}
		_, err := g.Compose(context.Background(), good, args...)
		refused(t, err)
		if len(s.calls) != 0 {
			t.Errorf("%v reached docker: %v", args, s.calls)
		}
	}
	// --pull and --progress are legitimate and start with two dashes.
	s := &stub{}
	g := &Guard{R: s}
	if _, err := g.Compose(context.Background(), good, "up", "-d", "--pull", "never", "--progress", "plain"); err != nil {
		t.Fatal(err)
	}
	if got := s.calls[0]; got != "docker compose -p dokdosta-test-a up -d --pull never --progress plain" {
		t.Fatal(got)
	}
	if err := g.Down(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	if got := s.calls[1]; got != "docker compose -p dokdosta-test-a down -v --timeout 2 --remove-orphans" {
		t.Fatal(got)
	}
}

func TestCtrChecksTheContainersProjectFirst(t *testing.T) {
	s := &stub{projects: map[string]string{"ours": "dokdosta-test-a", "theirs": "prod", "m0": "dokdosta-m0-e1", "plain": ""}}
	g := &Guard{R: s}
	ctx := context.Background()
	for _, ctr := range []string{"theirs", "m0", "plain", "unknown", ""} {
		_, err := g.Ctr(ctx, "restart", ctr)
		refused(t, err)
	}
	if m := s.mutations(); len(m) != 0 {
		t.Fatalf("mutations reached docker: %v", m)
	}
	for _, verb := range []string{"rm", "prune", "pause", "update", ""} {
		_, err := g.Ctr(ctx, verb, "ours")
		refused(t, err)
	}
	if _, err := g.Ctr(ctx, "restart", "ours"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Ctr(ctx, "exec", "ours", "rm", "/ok/flag"); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker restart id-ours", "docker exec id-ours rm /ok/flag"}
	if got := s.mutations(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", got)
	}
}

func TestPullOnlyBaseImages(t *testing.T) {
	s := &stub{}
	g := &Guard{R: s}
	refused(t, g.PullBase(context.Background(), "alpine:latest"))
	refused(t, g.PullBase(context.Background(), "busybox:1.37; rm -rf /"))
	if len(s.calls) != 0 {
		t.Fatal(s.calls)
	}
	if err := g.PullBase(context.Background(), "busybox:1.37"); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeEnv(t *testing.T) {
	ctx := context.Background()
	t.Setenv("DOCKER_HOST", "")
	if err := (&Guard{R: &stub{context: "default"}}).SanitizeEnv(ctx); err != nil {
		t.Fatal(err)
	}
	refused(t, (&Guard{R: &stub{context: "remote-prod"}}).SanitizeEnv(ctx))
	for _, h := range []string{"tcp://prod:2375", "ssh://root@prod", "unix:///tmp/other.sock"} {
		t.Setenv("DOCKER_HOST", h)
		refused(t, (&Guard{R: &stub{context: "default"}}).SanitizeEnv(ctx))
	}
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	if err := (&Guard{R: &stub{context: "default"}}).SanitizeEnv(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCleanEnvDropsRedirectingVariables(t *testing.T) {
	got := cleanEnv([]string{"PATH=/bin", "COMPOSE_PROJECT_NAME=prod", "DOCKER_CONTEXT=prod", "COMPOSE_FILE=x", "DOCKER_CONFIG=/x", "HOME=/h"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/h" {
		t.Fatal(got)
	}
}

func TestPreflightFindsOptedInStrangers(t *testing.T) {
	ctx := context.Background()
	t.Setenv("DOKDOSTA_MODE", "")
	ok := &stub{psRows: []psRow{{"abc", "dokdosta-test-a", "true"}, {"def", "dokdosta-test-b", "true"}}}
	if err := (&Guard{R: ok}).PreflightNoStrangers(ctx); err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][]psRow{
		{{"abc", "prod", "true"}},
		{{"abc", "", "true"}},
		{{"abc", "dokdosta-m0-x", "true"}},
		{{"abc", "dokdosta-test-a", "true"}, {"def", "media", "true"}},
	} {
		refused(t, (&Guard{R: &stub{psRows: rows}}).PreflightNoStrangers(ctx))
	}
	t.Setenv("DOKDOSTA_MODE", "all")
	refused(t, (&Guard{R: ok}).PreflightNoStrangers(ctx))
}

func optedIn(v string) bool {
	c, _ := config.ParseLabels(map[string]string{config.LabelEnable: v}, config.ModeLabel)
	return c.OptedIn
}

// The preflight must see every spelling dokdosta reads as an opt-in, not only the literal "true": the daemon lists every
// container and decides with config.ParseLabels.
func TestPreflightRefusesEveryOptInSpellingDokdostaAccepts(t *testing.T) {
	ctx := context.Background()
	t.Setenv("DOKDOSTA_MODE", "")
	for _, v := range []string{"1", "t", "T", "TRUE", "True", "true", " true", "true "} {
		if !optedIn(v) {
			t.Fatalf("premise: %q is not an opt-in for dokdosta", v)
		}
		refused(t, (&Guard{R: &stub{psRows: []psRow{{"abc", "prod", v}}}}).PreflightNoStrangers(ctx))
	}
	// Not an opt-in: not refused, whatever the project.
	for _, v := range []string{"false", "0", "f", "no", "garbage", ""} {
		if optedIn(v) {
			t.Fatalf("premise: %q is an opt-in for dokdosta", v)
		}
		if err := (&Guard{R: &stub{psRows: []psRow{{"abc", "prod", v}}}}).PreflightNoStrangers(ctx); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	// An opt-in inside a matching project is fine, whatever its spelling.
	if err := (&Guard{R: &stub{psRows: []psRow{{"abc", "dokdosta-test-a", "1"}}}}).PreflightNoStrangers(ctx); err != nil {
		t.Fatal(err)
	}
	// A line that does not split into three fields (a newline in the value, a tab in a label) is not guessed at.
	refused(t, (&Guard{R: &stub{psRows: []psRow{{"abc", "dokdosta-test-a", "\ntrue"}}}}).PreflightNoStrangers(ctx))
	refused(t, (&Guard{R: &stub{psRows: []psRow{{"abc", "prod\tx", "true"}}}}).PreflightNoStrangers(ctx))
	// No project label at all is a stranger.
	refused(t, (&Guard{R: &stub{psRows: []psRow{{"abc", "", "T"}}}}).PreflightNoStrangers(ctx))
}

func TestGuardedActorRefusesOtherProjects(t *testing.T) {
	f := docker.NewFake()
	f.Put(docker.Container{ID: "ours", Name: "ours", Labels: map[string]string{docker.LabelProject: "dokdosta-test-a"}})
	f.Put(docker.Container{ID: "theirs", Name: "theirs", Labels: map[string]string{docker.LabelProject: "media"}})
	f.Put(docker.Container{ID: "plain", Name: "plain", Labels: map[string]string{}})
	a := Actor{Reader: f, Inner: f}
	ctx := context.Background()
	for _, id := range []string{"theirs", "plain", "missing"} {
		refused(t, a.Restart(ctx, id))
		refused(t, a.Start(ctx, id))
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("calls reached the inner actor: %v", f.Calls())
	}
	if err := a.Restart(ctx, "ours"); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(ctx, "ours"); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.Calls()); !strings.Contains(got, "restart") || len(f.Calls()) != 2 {
		t.Fatal(got)
	}
}

// ---- the compose filter and the image helpers (M2b) ----------------------------------------------------------------

const goodUp = "-p dokdosta-test-a --project-directory /s -f /s/compose.yaml --env-file /s/.env up -d --force-recreate --no-deps --pull never --no-build -- web"

func TestComposeFilterAcceptsExactlyWhatTheComposePackageBuilds(t *testing.T) {
	for _, ok := range []string{
		goodUp,
		"-p dokdosta-test-a --project-directory /s -f /s/compose.yaml up -d --no-deps --pull never --no-build -- web",
		"-p dokdosta-test-a --project-directory /s -f /s/compose.yaml --dry-run --progress json up -d --no-deps --pull never --no-build -- web",
		"-p dokdosta-test-a --project-directory /s -f /s/compose.yaml config --hash=web",
		"-p dokdosta-test-a --project-directory /s -f /s/compose.yaml config --format json --no-env-resolution -- web", // the pull rung's policy read
		"-p dokdosta-test-a version --short",
		"-p dokdosta-test-a version",
		"-p dokdosta-test-a --project-directory /s -f /s/compose.yaml --env-file /s/.env pull --policy always -- web", // rung 3
	} {
		if err := CheckComposeCommand(strings.Fields(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

func TestComposeFilterRefuses(t *testing.T) {
	for name, args := range map[string]string{
		"down":                       "-p dokdosta-test-a down -v",
		"down after the files":       "-p dokdosta-test-a --project-directory /s -f /s/c.yaml down",
		"rm":                         "-p dokdosta-test-a rm -f web",
		"pull":                       "-p dokdosta-test-a pull web",
		"pull with the policy first": "-p dokdosta-test-a pull web --policy always",
		"pull policy missing":        "-p dokdosta-test-a pull --policy missing web",
		"pull with dependencies":     "-p dokdosta-test-a pull --policy always --include-deps web",
		"pull all services":          "-p dokdosta-test-a pull --policy always",
		"pull two services":          "-p dokdosta-test-a pull --policy always web db",
		"pull a flag as a service":   "-p dokdosta-test-a pull --policy always --quiet",
		"pull of another project":    "-p prod pull --policy always -- web",
		"build":                      "-p dokdosta-test-a build web",
		"run":                        "-p dokdosta-test-a run web",
		"stop":                       "-p dokdosta-test-a stop web",
		"another project":            "-p prod up -d --no-deps --pull never --no-build -- web",
		"an m0 project":              "-p dokdosta-m0-e1 up -d --no-deps --pull never --no-build -- web",
		"a pre-rename test project":  "-p whalenurse-test-a up -d --no-deps --pull never --no-build -- web",
		"no project":                 "up -d --no-deps --pull never --no-build -- web",
		"project via --project-name": "--project-name dokdosta-test-a up -d --no-deps --pull never --no-build -- web",
		"up may pull":                "-p dokdosta-test-a up -d --no-deps --pull always --no-build -- web",
		"up pull missing":            "-p dokdosta-test-a up -d --no-deps --pull missing --no-build -- web",
		"up without --pull":          "-p dokdosta-test-a up -d --no-deps --no-build -- web",
		"up may build":               "-p dokdosta-test-a up -d --no-deps --pull never --build web",
		"up without --no-build":      "-p dokdosta-test-a up -d --no-deps --pull never web",
		"up with dependencies":       "-p dokdosta-test-a up -d --pull never --no-build -- web",
		"up attached":                "-p dokdosta-test-a up --no-deps --pull never --no-build -- web",
		"up all services":            "-p dokdosta-test-a up -d --no-deps --pull never --no-build",
		"up two services":            "-p dokdosta-test-a up -d --no-deps --pull never --no-build web db",
		"up --remove-orphans":        "-p dokdosta-test-a up -d --no-deps --pull never --no-build --remove-orphans web",
		"up --renew-anon-volumes":    "-p dokdosta-test-a up -d --no-deps --pull never --no-build -V web",
		"config hash of all":         "-p dokdosta-test-a config --hash=*",
		"config hash of two":         "-p dokdosta-test-a config --hash=a,b",
		"config rendered":            "-p dokdosta-test-a config",
		"config json of all":         "-p dokdosta-test-a config --format json --no-env-resolution",
		"config json with env":       "-p dokdosta-test-a config --format json web",
		"config yaml":                "-p dokdosta-test-a config --format yaml --no-env-resolution -- web",
		"config json of two":         "-p dokdosta-test-a config --format json --no-env-resolution web db",
		"config json, flag service":  "-p dokdosta-test-a config --format json --no-env-resolution --hash=web",
		"version -f":                 "-p dokdosta-test-a version --format json",
		"flag without value":         "-p dokdosta-test-a -f",
		"no subcommand":              "-p dokdosta-test-a --dry-run",
		"up service without --":      "-p dokdosta-test-a up -d --no-deps --pull never --no-build web",
		"up flag after --":           "-p dokdosta-test-a up -d --no-deps --pull never --no-build -- --dry-run=false",
		"up flags after --":          "-p dokdosta-test-a up -d --no-deps --pull never -- --no-build web",
		"up two services after --":   "-p dokdosta-test-a up -d --no-deps --pull never --no-build -- web db",
		"up -- without a service":    "-p dokdosta-test-a up -d --no-deps --pull never --no-build --",
		"pull service without --":    "-p dokdosta-test-a pull --policy always web",
		"pull flag after --":         "-p dokdosta-test-a pull --policy always -- --include-deps",
		"config json without --":     "-p dokdosta-test-a config --format json --no-env-resolution web",
		"config json flag after --":  "-p dokdosta-test-a config --format json --no-env-resolution -- --hash=web",
	} {
		if err := CheckComposeCommand(strings.Fields(args)); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %s was not refused (%v)", name, args, err)
		}
	}
}

type recordingCompose struct{ ran []string }

func (r *recordingCompose) Run(_ context.Context, c compose.Command) (compose.Result, error) {
	r.ran = append(r.ran, c.String())
	return compose.Result{}, nil
}

func TestComposeRunnerOnlyRunsWhatTheFilterAccepts(t *testing.T) {
	rec := &recordingCompose{}
	r := &ComposeRunner{Inner: rec}
	if _, err := r.Run(context.Background(), compose.Command{Bin: "/usr/libexec/docker/cli-plugins/docker-compose", Args: strings.Fields(goodUp)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), compose.Command{Bin: "/usr/bin/docker", Args: strings.Fields(goodUp)}); !errors.Is(err, ErrRefused) {
		t.Fatalf("another binary: %v", err)
	}
	if _, err := r.Run(context.Background(), compose.Command{Bin: "/x/docker-compose", Args: strings.Fields("-p dokdosta-test-a down")}); !errors.Is(err, ErrRefused) {
		t.Fatalf("down: %v", err)
	}
	if len(rec.ran) != 1 || len(r.Log()) != 1 || len(r.Mutating()) != 1 || len(r.Pulls()) != 0 {
		t.Fatalf("%v %v", rec.ran, r.Log())
	}
	if _, err := r.Run(context.Background(), compose.Command{Bin: "/x/docker-compose", Args: strings.Fields("-p dokdosta-test-a pull --policy always -- web")}); err != nil {
		t.Fatal(err)
	}
	if len(r.Pulls()) != 1 || len(r.Mutating()) != 1 {
		t.Fatalf("a pull is counted by Pulls: %v / %v", r.Pulls(), r.Mutating())
	}
	if _, err := r.Run(context.Background(), compose.Command{Bin: "/x/docker-compose", Args: strings.Fields("-p dokdosta-test-a --dry-run --progress json up -d --no-deps --pull never --no-build -- web")}); err != nil {
		t.Fatal(err)
	}
	if len(r.Mutating()) != 1 {
		t.Fatalf("a --dry-run up is not a mutation: %v", r.Mutating())
	}
}

func TestImageHelpersStayInTheirNamespace(t *testing.T) {
	s := &stub{}
	g := &Guard{R: s}
	ctx := context.Background()
	for _, dst := range []string{"busybox:1.37", "prod/app:1", "dokdosta:1", "", "dokdosta-test", "dokdosta-test/x y", "dokdosta-test/x;rm"} {
		refused(t, g.Tag(ctx, "busybox:1.37", dst))
		refused(t, g.Untag(ctx, dst))
		refused(t, g.Build(ctx, dst, "FROM busybox:1.37\n"))
	}
	refused(t, g.Tag(ctx, "alpine:3", "dokdosta-test/x:1"))
	refused(t, g.Tag(ctx, "prod/app:1", "dokdosta-test/x:1"))
	refused(t, g.Build(ctx, "dokdosta-test/x:1", "FROM alpine:3.22\n"))
	refused(t, g.Build(ctx, "dokdosta-test/x:1", "FROM busybox:1.37\nFROM prod/app:1\n"))
	refused(t, g.Build(ctx, "dokdosta-test/x:1", "RUN true\n"))
	if len(s.mutations()) != 0 {
		t.Fatalf("a refused call ran: %v", s.mutations())
	}
	if err := g.Tag(ctx, "busybox:1.37", "dokdosta-test/x:1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Tag(ctx, "dokdosta-test/x:1", "dokdosta-test/y:1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Build(ctx, "dokdosta-test/z:1", "FROM busybox:1.37\nRUN touch /x\n"); err != nil {
		t.Fatal(err)
	}
	if err := g.Untag(ctx, "dokdosta-test/x:1"); err != nil {
		t.Fatal(err)
	}
	if len(s.mutations()) != 4 {
		t.Fatalf("%v", s.mutations())
	}
}

func TestComposeBuildOnlyBuildsOneServiceFromBaseImages(t *testing.T) {
	ctx := context.Background()
	stack := func(dockerfile string) string {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/Dockerfile", []byte(dockerfile), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	good := stack("FROM busybox:1.37\nRUN touch /x\n")
	s := &stub{}
	g := &Guard{R: s}
	for _, p := range badProjects {
		_, err := g.ComposeBuild(ctx, p, good, "app", "dokdosta-test/x:1")
		refused(t, err)
	}
	for _, svc := range []string{"", "--push", "-q", "a b", "a;b", "../x", "a/b"} {
		_, err := g.ComposeBuild(ctx, "dokdosta-test-a", good, svc, "dokdosta-test/x:1")
		refused(t, err)
	}
	for _, tag := range []string{"busybox:1.37", "prod/x:1", ""} {
		_, err := g.ComposeBuild(ctx, "dokdosta-test-a", good, "app", tag)
		refused(t, err)
	}
	for _, df := range []string{"FROM alpine:3.22\n", "FROM busybox:1.37\nFROM prod/app:1\n", "RUN true\n"} {
		_, err := g.ComposeBuild(ctx, "dokdosta-test-a", stack(df), "app", "dokdosta-test/x:1")
		refused(t, err)
	}
	dollar := filepath.Join(t.TempDir(), "${HOME}")
	if err := os.Mkdir(dollar, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dollar, "Dockerfile"), []byte("FROM busybox:1.37\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := g.ComposeBuild(ctx, "dokdosta-test-a", dollar, "app", "dokdosta-test/x:1")
	refused(t, err)
	if _, err := g.ComposeBuild(ctx, "dokdosta-test-a", t.TempDir(), "app", "dokdosta-test/x:1"); err == nil {
		t.Fatal("a stack without a Dockerfile was built")
	}
	if len(s.mutations()) != 0 {
		t.Fatalf("a refused build ran: %v", s.mutations())
	}
	if _, err := g.ComposeBuild(ctx, "dokdosta-test-a", good, "app", "dokdosta-test/x:1"); err != nil {
		t.Fatal(err)
	}
	// The temporary compose file is gone by now, so check the command line.
	got := s.mutations()
	if len(got) != 1 || !strings.HasPrefix(got[0], "docker compose -p dokdosta-test-a -f ") ||
		!strings.HasSuffix(got[0], "/compose.yaml build -- app") || strings.Contains(got[0], "--project-directory") {
		t.Fatalf("%v", got)
	}
}

// ---- the test registry (M4) ---------------------------------------------------------------------------------------

const regRef = "127.0.0.1:49153/dokdosta-test/pull-app:latest"

func TestRegistryHelpersStayInTheTestRegistry(t *testing.T) {
	s := &stub{projects: map[string]string{"reg": "dokdosta-test-registry-ab12", "other": "prod"}}
	g := &Guard{R: s}
	ctx := context.Background()
	for _, ref := range []string{
		"", "busybox:1.37", "localhost:5000/dokdosta-test/x:1", "127.0.0.1/dokdosta-test/x:1", "127.0.0.1:5000/x:1",
		"127.0.0.1:5000/dokdosta-test", "127.0.0.1:5000/other/x:1", "127.0.0.1:5000/dokdosta-test/x y", "127.0.0.1:5000/dokdosta-test/x;rm",
		"ghcr.io/jackspiering/dokdosta:latest", "10.0.0.5:5000/dokdosta-test/x:1", "127.0.0.1:5000/dokdosta-test/../x:1x/$(id)",
		"127.0.0.1:5000@evil.com/dokdosta-test/x:1", "docker.io/127.0.0.1:5000/dokdosta-test/x:1",
	} {
		refused(t, g.Push(ctx, ref))
		refused(t, g.PullRegistry(ctx, ref))
	}
	if len(s.mutations()) != 0 {
		t.Fatalf("a refused call ran: %v", s.mutations())
	}
	if err := g.Push(ctx, regRef); err != nil {
		t.Fatal(err)
	}
	if err := g.PullRegistry(ctx, regRef); err != nil {
		t.Fatal(err)
	}
	// Tag and Build accept a registry reference as a destination, an image ID as a source, and nothing looser.
	id := "sha256:" + strings.Repeat("a", 64)
	if err := g.Tag(ctx, id, regRef); err != nil {
		t.Fatal(err)
	}
	if err := g.Tag(ctx, "dokdosta-test/v2:1", regRef); err != nil {
		t.Fatal(err)
	}
	if err := g.Build(ctx, regRef, "FROM busybox:1.37\nRUN echo v1 > /version\n"); err != nil {
		t.Fatal(err)
	}
	refused(t, g.Tag(ctx, "sha256:abc", regRef))
	refused(t, g.Tag(ctx, id+"x", regRef))
	refused(t, g.Tag(ctx, id, "prod/app:1"))
	refused(t, g.Tag(ctx, "127.0.0.1:49153/other/x:1", regRef))
	// The registry itself is a base image, and only as itself.
	if err := g.PullBase(ctx, "registry:2"); err != nil {
		t.Fatal(err)
	}
	refused(t, g.PullBase(ctx, "registry:3"))
	// The published port is read only for a container of a test project.
	if p, err := g.PublishedPort(ctx, "reg", "5000/tcp"); err != nil || p != "49153" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := g.PublishedPort(ctx, "other", "5000/tcp"); !errors.Is(err, ErrRefused) {
		t.Fatalf("%v", err)
	}
}

func TestRunRollbackRunsOnlyWhatARollbackIs(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	tagLine := "docker image tag " + id + " " + regRef
	good := "docker compose -p dokdosta-test-a --project-directory /tmp/s -f /tmp/s/compose.yaml up -d --no-deps --pull never --no-build web"
	// The form written before plan v3.13 item 7, without --no-build, is still a rollback.
	if err := (&Guard{R: &stub{}}).RunRollback(context.Background(), tagLine, strings.Replace(good, " --no-build", "", 1)); err != nil {
		t.Fatal(err)
	}
	s := &stub{}
	g := &Guard{R: s}
	ctx := context.Background()
	if err := g.RunRollback(ctx, tagLine, good); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker tag " + id + " " + regRef, "docker " + strings.TrimPrefix(good, "docker ")}
	if got := s.mutations(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", got)
	}
	// The parent and the sidecars it was recreated with, in one up.
	s3 := &stub{}
	if err := (&Guard{R: s3}).RunRollback(ctx, tagLine, good+" side side2"); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string][2]string{
		"another project":       {tagLine, strings.Replace(good, "dokdosta-test-a", "prod", 1)},
		"an extra flag":         {tagLine, strings.Replace(good, "up -d", "up -d --force-recreate", 1)},
		"a different command":   {tagLine, "docker compose -p dokdosta-test-a down"},
		"down in the middle":    {tagLine, "docker compose -p dokdosta-test-a down && " + good},
		"quoted path":           {tagLine, strings.Replace(good, "/tmp/s ", "'/tmp/s;x' ", 1)},
		"a shell metacharacter": {tagLine, good + " ; id"},
		"pull may pull":         {tagLine, strings.Replace(good, "--pull never", "--pull always", 1)},
		"may build":             {tagLine, strings.Replace(good, "--no-build", "--build", 1)},
		"--no-build twice":      {tagLine, strings.Replace(good, "--no-build", "--no-build --no-build", 1)},
		"--no-build elsewhere":  {tagLine, strings.Replace(strings.Replace(good, " --no-build", "", 1), "up -d", "up --no-build -d", 1)},
		"not docker":            {tagLine, "sh -c id"},
		"tag onto a bad ref":    {"docker image tag " + id + " prod/app:1", good},
		"tag from a name":       {"docker image tag busybox:1.37 " + regRef, good},
		"tag with more":         {tagLine + " && docker rm x", good},
	} {
		s2 := &stub{}
		refused(t, (&Guard{R: s2}).RunRollback(ctx, tc[0], tc[1]))
		if m := s2.mutations(); len(m) != 0 {
			t.Errorf("%s: a refused rollback reached docker (not even its tag may run): %v", name, m)
		}
	}
}

// M7: only a pull, and only with the directory the test registered, may carry a DOCKER_CONFIG of its own.
func TestComposeRunnerOnlyLetsAPullUseTheCredentialsDirectory(t *testing.T) {
	rec := &recordingCompose{}
	r := &ComposeRunner{Inner: rec}
	r.AllowAuthDir("/tmp/dokdosta-test-auth-1")
	pull := strings.Fields("-p dokdosta-test-a pull --policy always -- web")
	ctx := context.Background()
	if _, err := r.Run(ctx, compose.Command{Bin: "/x/docker-compose", Args: pull, ConfigDir: "/tmp/dokdosta-test-auth-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, compose.Command{Bin: "/x/docker-compose", Args: pull, ConfigDir: "/home/me/.docker"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("another directory: %v", err)
	}
	if _, err := r.Run(ctx, compose.Command{Bin: "/x/docker-compose", Args: strings.Fields(goodUp), ConfigDir: "/tmp/dokdosta-test-auth-1"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("an up with the credentials: %v", err)
	}
	none := &ComposeRunner{Inner: rec}
	if _, err := none.Run(ctx, compose.Command{Bin: "/x/docker-compose", Args: pull, ConfigDir: "/tmp/dokdosta-test-auth-1"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("no directory registered: %v", err)
	}
	if got := r.PullConfigDirs(); len(got) != 1 || got[0] != "/tmp/dokdosta-test-auth-1" {
		t.Fatalf("%v", got)
	}
}

func TestPushWithOnlyUsesTheTestsOwnDockerConfig(t *testing.T) {
	s := &stub{}
	g := &Guard{R: s}
	ref := "127.0.0.1:5000/dokdosta-test/x:v1"
	for _, dir := range []string{"/home/me/.docker", os.TempDir() + "/other-dir", os.TempDir() + "/" + DockerConfigPrefix + "1/sub", ""} {
		if err := g.PushWith(context.Background(), ref, dir); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: %v", dir, err)
		}
	}
	if err := g.PushWith(context.Background(), "ghcr.io/x/y:v1", os.TempDir()+"/"+DockerConfigPrefix+"1"); !errors.Is(err, ErrRefused) {
		t.Errorf("a reference outside the test registry: %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("a refusal ran docker: %v", s.calls)
	}
}
