package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Command is one invocation of the compose binary.
type Command struct {
	Bin  string
	Args []string
	Dir  string
	// ConfigDir, when set, is the DOCKER_CONFIG of this one command instead of the runner's. Only the pull rung's
	// `compose pull` sets it, to DOKDOSTA_REGISTRY_AUTH_DIR.
	ConfigDir string
}

// String is the command line, each word quoted for a POSIX shell when it needs it.
func (c Command) String() string {
	words := make([]string, 0, 1+len(c.Args))
	for _, w := range append([]string{c.Bin}, c.Args...) {
		words = append(words, ShellWord(w))
	}
	return strings.Join(words, " ")
}

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
// ConfigDir is the DOCKER_CONFIG the commands get: an empty directory of dokdosta's own (PrivateConfigDir); the zero
// value is NoDockerConfig.
type ExecRunner struct {
	ConfigDir string
}

// StopDelay is how long a cancelled command has between SIGTERM and SIGKILL (and how long Run then waits for its output
// to close). A compose up that is stopped mid-recreate with SIGTERM can still finish or undo a step; SIGKILL cannot.
var StopDelay = 10 * time.Second

func (r ExecRunner) Run(ctx context.Context, c Command) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = StopDelay
	// Its own process group: a Ctrl-C in the terminal reaches dokdosta, which lets the command finish, and not the
	// command itself.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = c.Dir
	dir := r.ConfigDir
	if c.ConfigDir != "" {
		dir = c.ConfigDir
	}
	cmd.Env = ChildEnv(os.Environ(), dir)
	out, errb := &capped{max: maxOutput}, &capped{max: maxOutput}
	cmd.Stdout, cmd.Stderr = out, errb
	err := cmd.Run()
	res := Result{Stdout: out.buf.Bytes(), Stderr: errb.buf.Bytes()}
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (%w)", err, ctx.Err())
		}
		return res, RunError(c, err, append(append([]byte(nil), res.Stderr...), errb.tail...))
	}
	return res, nil
}

// maxOutput caps what is kept of each stream of one command. The oracle's --progress json and a one-service config are
// far smaller; a command that prints more is not one dokdosta needs all of.
const maxOutput = 4 << 20

// capped keeps the first max bytes written to it, and the last tailSize bytes apart, so a cause printed at the end of a
// long output is still found. It never fails a write: an error would stop the copy and block or kill the command.
type capped struct {
	buf  bytes.Buffer
	max  int
	tail []byte
	over bool
}

const tailSize = 8 << 10

func (w *capped) Write(p []byte) (int, error) {
	// Decided before the write: after it, buf already holds this write and the test would always count it twice.
	overflow := w.over || w.buf.Len()+len(p) > w.max
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	if overflow {
		w.over = true
		w.tail = append(w.tail, p...)
		if len(w.tail) > tailSize {
			w.tail = append([]byte(nil), w.tail[len(w.tail)-tailSize:]...)
		}
	}
	return len(p), nil
}

// RunError is the error of a failed compose command. Compose's own output is not in it: a load error can quote a line of
// the stack's .env or env_file (compose-go's dotenv parser puts the value into "unterminated quoted value ..."), and the
// error reaches the events, the API, the store and the log (plan §3.9: the environment is never shown). Only the causes
// below, which dokdosta names in its own words, are kept from stderr; otherwise the error says how to see the rest.
func RunError(c Command, err error, stderr []byte) error {
	if found := knownCauses(stderr); len(found) > 0 {
		return fmt.Errorf("%s: %w: compose reported: %s", c, err, strings.Join(found, "; "))
	}
	return fmt.Errorf("%s: %w: compose's output is not shown, as it can quote the stack's .env; run `docker compose config` in the stack to see why", c, err)
}

// composeCauses are what an operator acts on, in a fixed order. Each is matched in compose's lowercased output and reported as
// its fixed text, never as the line it was found in. The pull rung's ones are the registry answers that
// docs/troubleshooting.md lists (the M7 positive control matches them in CI).
var composeCauses = []struct {
	re   *regexp.Regexp
	text string
}{
	{regexp.MustCompile(`port is already allocated`), "port is already allocated"},
	{regexp.MustCompile(`address already in use`), "address already in use"},
	{regexp.MustCompile(`bind source path does not exist`), "bind source path does not exist"},
	{regexp.MustCompile(`no such service`), "no such service"},
	{regexp.MustCompile(`no such image`), "no such image"},
	{regexp.MustCompile(`network \S+ not found`), "network not found"},
	{regexp.MustCompile(`is already in use by container`), "the container name is already in use"},
	{regexp.MustCompile(`no space left on device`), "no space left on device"},
	{regexp.MustCompile(`pull access denied`), "pull access denied"},
	{regexp.MustCompile(`no basic auth credentials`), "no basic auth credentials"},
	{regexp.MustCompile(`unauthorized`), "unauthorized"},
	{regexp.MustCompile(`authentication required`), "authentication required"},
	{regexp.MustCompile(`requested access to the resource is denied`), "access denied"},
	{regexp.MustCompile(`permission denied`), "permission denied"},
	{regexp.MustCompile(`manifest unknown`), "manifest unknown"},
	{regexp.MustCompile(`toomanyrequests`), "toomanyrequests"},
	{regexp.MustCompile(`no such host`), "no such host"},
	{regexp.MustCompile(`connection refused`), "connection refused"},
	{regexp.MustCompile(`i/o timeout`), "i/o timeout"},
}

func knownCauses(stderr []byte) []string {
	m := bytes.ToLower(stderr)
	var out []string
	for _, c := range composeCauses {
		if c.re.Match(m) {
			out = append(out, c.text)
		}
	}
	return out
}

// Compose must always get a DOCKER_CONFIG: unset, the Docker CLI reads $HOME/.docker, and a config.json there brings
// credentials and a currentContext that points compose at another daemon. The directory must be empty and must be
// writable: compose v5.2.0 to v5.5.x build buildx's store in their dry-run client (buildx store.New creates
// <config>/buildx/{instances,defaults,activity}), and when that fails the dry-run fails and the oracle is lost. So it is a
// fresh directory of dokdosta's own (PrivateConfigDir), and NoDockerConfig is only the last resort.

// NoDockerConfig is the DOCKER_CONFIG when there is no private directory: a path that cannot exist (nothing, root
// included, can create an entry in /proc), which the Docker CLI reads as "no config file". A path under a regular file
// (/dev/null/x) would not do: that is ENOTDIR, which the CLI warns about on every command, onto the stderr the oracle
// parses. With it, the dry-run of compose v5.2.0 to v5.5.x fails and the drift guard falls back to config --hash.
const NoDockerConfig = "/proc/dokdosta-no-docker-config"

// PrivateConfigDir creates an empty directory (mode 0700, a fresh random name, so nothing can be planted in it
// beforehand) in the first of the parents where that works, for the DOCKER_CONFIG of the compose commands. remove deletes
// it with whatever compose put there.
func PrivateConfigDir(parents ...string) (dir string, remove func(), err error) {
	var errs []error
	for _, p := range parents {
		if p == "" {
			continue
		}
		d, err := os.MkdirTemp(p, "dokdosta-docker-config-")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return d, func() { _ = os.RemoveAll(d) }, nil
	}
	if len(errs) == 0 {
		errs = append(errs, errors.New("no directory to create it in"))
	}
	return "", func() {}, errors.Join(errs...)
}

// ChildEnv is the environment compose runs with: the parent's, minus everything that could point compose at another
// project, file set, profile, Docker config or credentials (COMPOSE_*, DOCKER_CONTEXT, DOCKER_CONFIG,
// DOCKER_AUTH_CONFIG), and minus dokdosta's own settings, plus DOCKER_CONFIG=configDir (NoDockerConfig when empty) so
// $HOME/.docker is never read, and NO_COLOR so the output is plain.
func ChildEnv(env []string, configDir string) []string {
	if configDir == "" {
		configDir = NoDockerConfig
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "COMPOSE_"), strings.HasPrefix(k, "DOKDOSTA_"), strings.HasPrefix(k, "WHALENURSE_"), k == "DOCKER_CONTEXT", k == "DOCKER_CONFIG",
			k == "DOCKER_AUTH_CONFIG", k == "NO_COLOR":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "DOCKER_CONFIG="+configDir, "NO_COLOR=1")
}
