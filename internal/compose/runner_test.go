package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failWith runs, through the ExecRunner, a command that prints stderr and exits with code. The text is read from a file,
// so it is not in the argv (which the error names).
func failWith(t *testing.T, stderr, code string) error {
	t.Helper()
	f := filepath.Join(t.TempDir(), "stderr")
	if err := os.WriteFile(f, []byte(stderr+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ExecRunner{}.Run(context.Background(), Command{Bin: "/bin/sh", Args: []string{"-c", "cat " + f + " >&2; exit " + code}, Dir: "/"})
	if err == nil {
		t.Fatalf("%q: the command should fail", stderr)
	}
	return err
}

// Compose's load errors can quote a line of the stack's .env or env_file (a dotenv parse error does), so its stderr
// never goes into the error: only the exit status and the causes whalenurse knows by name do (plan §3.9: the environment
// is never shown).
func TestRunErrorDoesNotQuoteEnvFileValues(t *testing.T) {
	err := failWith(t, `failed to read /srv/s/.env: line 3: unterminated quoted value "hunter2-planted`, "15")
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("compose's stderr reached the error: %v", err)
	}
	for _, want := range []string{"exit status 15", "docker compose config"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should say %q: %v", want, err)
		}
	}
}

// The causes the operator acts on stay recognisable: the port conflict (rules and the m4 tests match on it) and the
// registry login failures that docs/troubleshooting.md lists (the M7 positive control checks them in CI).
func TestRunErrorKeepsTheKnownCauses(t *testing.T) {
	for stderr, want := range map[string][]string{
		"Error response from daemon: driver failed programming external connectivity on endpoint s-app-1 (0123): Bind for 0.0.0.0:80 failed: port is already allocated":                                                 {"port is already allocated"},
		`failed to resolve reference "127.0.0.1:5000/whalenurse-test/auth-without:latest": pull access denied, repository does not exist or may require authorization: authorization failed: no basic auth credentials`: {"pull access denied", "no basic auth credentials"},
		`Error response from daemon: Head "https://ghcr.io/v2/o/i/manifests/latest": unauthorized`:                                                                                                                      {"unauthorized"},
		`Error response from daemon: Get "https://r.example/v2/": authentication required`:                                                                                                                              {"authentication required"},
		"no such service: web":                                                           {"no such service"},
		"Error response from daemon: No such image: x:latest":                            {"no such image"},
		"Error response from daemon: network s_default not found":                        {"network not found"},
		"open /srv/s/compose.yaml: permission denied":                                    {"permission denied"},
		"Error response from daemon: denied: requested access to the resource is denied": {"access denied"},
	} {
		got := strings.ToLower(failWith(t, stderr, "1").Error())
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%q: the error should keep %q: %s", stderr, w, got)
			}
		}
	}
}

// Each stream keeps at most maxOutput bytes; the command is not blocked or killed by the cap, and a cause printed after a
// long output is still found.
func TestRunCapsTheOutputAndStillFindsTheCause(t *testing.T) {
	script := `head -c 6291456 /dev/zero | tr '\0' x; head -c 6291456 /dev/zero | tr '\0' y >&2; echo ' port is already allocated' >&2; exit 1`
	res, err := ExecRunner{}.Run(context.Background(), Command{Bin: "/bin/sh", Args: []string{"-c", script}, Dir: "/"})
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("the command should run to its own exit: %v", err)
	}
	if len(res.Stdout) != maxOutput || len(res.Stderr) != maxOutput {
		t.Fatalf("kept %d and %d bytes, want %d each", len(res.Stdout), len(res.Stderr), maxOutput)
	}
	if !strings.Contains(err.Error(), "port is already allocated") || strings.Contains(err.Error(), "yyy") {
		t.Fatalf("the cause at the end should be found, the output not quoted: %.300s", err)
	}
}

// A command line in an error or a log can be pasted into a shell.
func TestCommandStringIsQuoted(t *testing.T) {
	c := Command{Bin: "/usr/libexec/docker/cli-plugins/docker-compose", Args: []string{"-p", "media", "-f", "/srv/my stack/compose.yaml", "up", "-d", "it's"}}
	want := `/usr/libexec/docker/cli-plugins/docker-compose -p media -f '/srv/my stack/compose.yaml' up -d 'it'\''s'`
	if got := c.String(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
