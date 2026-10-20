package compose

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Command is one invocation of the compose binary.
type Command struct {
	Bin  string
	Args []string
	Dir  string
}

func (c Command) String() string { return c.Bin + " " + strings.Join(c.Args, " ") }

// Result is what a command printed. Compose writes its progress, including the --progress json lines of the dry-run
// oracle, to stderr, so both streams are kept.
type Result struct {
	Stdout, Stderr []byte
}

// Combined is stdout followed by stderr.
func (r Result) Combined() []byte {
	return append(append([]byte(nil), r.Stdout...), r.Stderr...)
}

// Runner runs a command. Everything in this package goes through it, so a unit test never runs compose and the
// integration tests wrap it in a fail-closed filter (test/integration/guard.go).
type Runner interface {
	Run(ctx context.Context, c Command) (Result, error)
}

// ExecRunner runs real commands, with an environment that cannot redirect compose at another project or Docker config.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Command) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = ChildEnv(os.Environ())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	if err != nil {
		return res, fmt.Errorf("%s: %w: %s", c, err, tail(errb.String()))
	}
	return res, nil
}

// NoDockerConfig is the DOCKER_CONFIG every compose child gets: a directory that cannot exist (nothing, root included,
// can create an entry in /proc), which the Docker CLI reads as "no config file". Leaving DOCKER_CONFIG unset is not
// enough: the CLI then reads $HOME/.docker, and a config.json there brings credentials and a currentContext that points
// compose at another daemon. A path under a regular file (/dev/null/x) would not do either: that is ENOTDIR, which the
// CLI prints a warning about on every command, onto the stderr the oracle parses.
const NoDockerConfig = "/proc/whalenurse-no-docker-config"

// ChildEnv is the environment compose runs with: the parent's, minus everything that could point compose at another
// project, file set, profile, Docker config or credentials (COMPOSE_*, DOCKER_CONTEXT, DOCKER_CONFIG,
// DOCKER_AUTH_CONFIG), and minus whalenurse's own settings, plus DOCKER_CONFIG=NoDockerConfig so $HOME/.docker is never
// read, and NO_COLOR so the output is plain.
func ChildEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "COMPOSE_"), strings.HasPrefix(k, "WHALENURSE_"), k == "DOCKER_CONTEXT", k == "DOCKER_CONFIG",
			k == "DOCKER_AUTH_CONFIG", k == "NO_COLOR":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "DOCKER_CONFIG="+NoDockerConfig, "NO_COLOR=1")
}

// tail keeps the end of compose's complaint: the cause is last.
func tail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 600 {
		return "..." + s[len(s)-600:]
	}
	return s
}
