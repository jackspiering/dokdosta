package compose

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func authB64(userPass string) string { return base64.StdEncoding.EncodeToString([]byte(userPass)) }

func writeAuthDir(t *testing.T, config string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RegistryAuthFile), []byte(config), mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadRegistryAuthAcceptsInlineCredentials(t *testing.T) {
	cfg := `{"auths": {
	  "ghcr.io": {"auth": "` + authB64("alice:s3cr3t-PASS") + `"},
	  "registry.example.lan:5000": {"username": "bob-USER", "password": "bob-PASS-1234"},
	  "https://index.docker.io/v1/": {"identitytoken": "idtok-SECRET-9876", "email": "x@example.com"}}}`
	a, err := LoadRegistryAuth(writeAuthDir(t, cfg, 0o444))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(a.Hosts, "|"); got != "ghcr.io|https://index.docker.io/v1/|registry.example.lan:5000" {
		t.Errorf("hosts: %s", got)
	}
	all := strings.Join(a.Secrets, "\n")
	for _, want := range []string{authB64("alice:s3cr3t-PASS"), "alice:s3cr3t-PASS", "s3cr3t-PASS", "bob-PASS-1234", "bob-USER", "idtok-SECRET-9876"} {
		if !strings.Contains(all, want) {
			t.Errorf("secret %q not collected: %v", want, a.Secrets)
		}
	}
}

func TestLoadRegistryAuthRefusesAnythingButInlineAuths(t *testing.T) {
	good := `"auths": {"ghcr.io": {"auth": "` + authB64("u:p") + `"}}`
	for _, tc := range []struct {
		name, config, want string
	}{
		{"credsStore", `{` + good + `, "credsStore": "pass"}`, `"credsStore"`},
		{"credHelpers", `{` + good + `, "credHelpers": {"ghcr.io": "x"}}`, `"credHelpers"`},
		{"currentContext", `{` + good + `, "currentContext": "remote"}`, `"currentContext"`},
		{"proxies", `{` + good + `, "proxies": {"default": {"httpProxy": "http://p"}}}`, `"proxies"`},
		{"an unknown key", `{` + good + `, "somethingElse": 1}`, `"somethingElse"`},
		{"no auths", `{}`, `"auths" is empty or missing`},
		{"empty auths", `{"auths": {}}`, `"auths" is empty or missing`},
		{"auths of the wrong type", `{"auths": ["ghcr.io"]}`, `not an object of registry entries`},
		{"not json", `not json at all`, `not a JSON object`},
		{"entry without a credential", `{"auths": {"ghcr.io": {}}}`, `has no inline credential`},
		{"username without a password", `{"auths": {"ghcr.io": {"username": "u"}}}`, `has no inline credential`},
		{"auth that is not base64", `{"auths": {"ghcr.io": {"auth": "***not base64***"}}}`, `is not base64`},
		{"auth without a colon", `{"auths": {"ghcr.io": {"auth": "` + authB64("nocolon") + `"}}}`, `does not decode to user:password`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRegistryAuth(writeAuthDir(t, tc.config, 0o444))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadRegistryAuthRefusesTheDirectoryItself(t *testing.T) {
	dir := writeAuthDir(t, `{"auths": {"ghcr.io": {"auth": "`+authB64("u:p")+`"}}}`, 0o444)
	if err := os.Mkdir(filepath.Join(dir, "contexts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistryAuth(dir); err == nil || !strings.Contains(err.Error(), "contexts") {
		t.Errorf("contexts: %v", err)
	}
	for name, d := range map[string]string{"missing": filepath.Join(t.TempDir(), "nope"), "empty dir": t.TempDir(), "a file": writeAuthDir(t, "{}", 0o444) + "/config.json", "unset": ""} {
		if _, err := LoadRegistryAuth(d); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// No error quotes a value from the file, whatever is wrong with it.
func TestLoadRegistryAuthErrorsNeverQuoteTheFile(t *testing.T) {
	const pw = "pw-SECRET-4242"
	for _, cfg := range []string{
		`{"auths": {"ghcr.io": {"username": "u", "password": "` + pw + `"}}, "credsStore": "x"}`,
		`{"auths": {"ghcr.io": {"auth": "` + authB64("nocolon-"+pw) + `"}}}`,
		`{"auths": {"ghcr.io": {"auth": "` + pw + `"}}}`,
		`{"auths": "` + pw + `"}`,
		`{"auths": {"ghcr.io": {"password": "` + pw + `"}}}`,
		`{"auths": {"ghcr.io": {"password": "` + pw + `"}}` + pw, // truncated JSON
	} {
		_, err := LoadRegistryAuth(writeAuthDir(t, cfg, 0o444))
		if err == nil {
			t.Fatalf("accepted %s", cfg)
		}
		if strings.Contains(err.Error(), pw) || strings.Contains(err.Error(), "nocolon") {
			t.Errorf("the error quotes the file: %v", err)
		}
	}
}

func TestLoadRegistryAuthWarnsWhenItCanWrite(t *testing.T) {
	dir := writeAuthDir(t, `{"auths": {"ghcr.io": {"auth": "`+authB64("u:p")+`"}}}`, 0o644)
	a, err := LoadRegistryAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && len(a.Warnings) == 0 {
		t.Error("a writable directory and file gave no warning")
	}
	for _, w := range a.Warnings {
		if !strings.Contains(w, "read-only") {
			t.Errorf("warning: %s", w)
		}
	}
}

// Only the pull gets the credentials directory (plan v3.12 item 3): every other command keeps the runner's.
func TestOnlyPullGetsTheRegistryAuthDir(t *testing.T) {
	dir := writeAuthDir(t, `{"auths": {"ghcr.io": {"auth": "`+authB64("u:p")+`"}}}`, 0o444)
	r := &recRunner{out: func(c Command) (Result, error) {
		if len(c.Args) > 0 && c.Args[len(c.Args)-2] == "--format" {
			return Result{Stdout: []byte(`{"services": {"web": {}}}`)}, nil
		}
		return Result{}, nil
	}}
	c := New(Binary{Path: "/x/docker-compose"}, r).WithRegistryAuth(dir)
	ctx := context.Background()
	_ = c.Recreate(ctx, proj())
	_ = c.Up(ctx, proj())
	_, _ = c.ConfigHash(ctx, proj())
	_, _ = c.PullPolicy(ctx, proj())
	_, _ = c.DryRunUp(ctx, proj(), "p-web-1")
	if err := c.Pull(ctx, proj()); err != nil {
		t.Fatal(err)
	}
	var pulls int
	for _, cmd := range r.cmds {
		isPull := false
		for _, a := range cmd.Args {
			if a == "pull" {
				isPull = true
			}
		}
		switch {
		case isPull && cmd.ConfigDir == dir && strings.Contains(cmd.String(), "--policy always"):
			pulls++
		case isPull:
			t.Errorf("the pull ran with ConfigDir %q", cmd.ConfigDir)
		case cmd.ConfigDir != "":
			t.Errorf("%s got ConfigDir %q", cmd, cmd.ConfigDir)
		}
	}
	if pulls != 1 {
		t.Errorf("%d pulls: %v", pulls, r.cmds)
	}
}

func TestPullChecksTheFileAgainAndWithoutADirectoryUsesNone(t *testing.T) {
	dir := writeAuthDir(t, `{"auths": {"ghcr.io": {"auth": "`+authB64("u:p")+`"}}}`, 0o444)
	r := &recRunner{out: func(Command) (Result, error) { return Result{}, nil }}
	c := New(Binary{Path: "/x/docker-compose"}, r).WithRegistryAuth(dir)
	if err := os.Chmod(filepath.Join(dir, RegistryAuthFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, RegistryAuthFile), []byte(`{"currentContext": "remote"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := c.Pull(context.Background(), proj())
	if err == nil || !strings.Contains(err.Error(), "registry credentials") || !strings.Contains(err.Error(), "currentContext") {
		t.Fatalf("a file that went bad: %v", err)
	}
	if len(r.cmds) != 0 {
		t.Errorf("compose ran: %v", r.cmds)
	}
	plain := New(Binary{Path: "/x/docker-compose"}, r)
	if err := plain.Pull(context.Background(), proj()); err != nil || len(r.cmds) != 1 || r.cmds[0].ConfigDir != "" {
		t.Errorf("without a directory: %v %v", err, r.cmds)
	}
}
