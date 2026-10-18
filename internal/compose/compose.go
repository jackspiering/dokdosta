package compose

import (
	"context"
	"fmt"
	"strings"
)

// Reader is the read-only half: nothing here changes a container, so it runs in dry-run mode too. `--dry-run` is
// compose's own flag and E5 showed it has no side effects.
type Reader interface {
	// Binary is the compose binary in use.
	Binary() Binary
	// DryRunUp runs `--dry-run --progress json up -d --no-deps --pull never --no-build <svc>` and parses what compose
	// would do to the named container (the oracle of plan §3.6 check 1). ErrUnparseable means: use ConfigHash.
	DryRunUp(ctx context.Context, p Project, containerName string) (Oracle, error)
	// ConfigHash runs `config --hash=<svc>` (the fallback of check 1).
	ConfigHash(ctx context.Context, p Project) (string, error)
}

// Actor is the mutating half. Both methods deploy what is on disk and in the local image store now, so the drift guard
// runs before them (plan §3.1, §3.6), and neither can pull, build or touch another service.
type Actor interface {
	// Recreate is rung 2: `up -d --force-recreate --no-deps --pull never --no-build <svc>`.
	Recreate(ctx context.Context, p Project) error
	// Up is the dependents step after a parent was recreated: `up -d --no-deps --pull never --no-build <svc>`, without
	// --force-recreate. Compose hashes the resolved container:<id>, so the new parent ID makes it recreate the dependent
	// itself (E2b, E2d).
	Up(ctx context.Context, p Project) error
}

// Compose runs a compose binary through a Runner.
type Compose struct {
	bin Binary
	run Runner
}

// New returns a Compose for the binary. A nil runner means ExecRunner.
func New(bin Binary, r Runner) *Compose {
	if r == nil {
		r = ExecRunner{}
	}
	return &Compose{bin: bin, run: r}
}

func (c *Compose) Binary() Binary { return c.bin }

// upArgs is the tail every `up` carries: detached, no dependencies, never pull, never build. Callers add
// --force-recreate; nothing else can be added.
func upArgs(p Project, force bool) []string {
	args := []string{"up", "-d"}
	if force {
		args = append(args, "--force-recreate")
	}
	return append(args, "--no-deps", "--pull", "never", "--no-build", p.Service)
}

func (c *Compose) cmd(p Project, rest ...string) Command {
	return Command{Bin: c.bin.Path, Args: append(p.baseArgs(), rest...), Dir: "/"}
}

func (c *Compose) DryRunUp(ctx context.Context, p Project, containerName string) (Oracle, error) {
	rest := append([]string{"--dry-run", "--progress", "json"}, upArgs(p, false)...)
	res, err := c.run.Run(ctx, c.cmd(p, rest...))
	if err != nil {
		return Oracle{}, err
	}
	return ParseOracle(res.Combined(), containerName)
}

func (c *Compose) ConfigHash(ctx context.Context, p Project) (string, error) {
	res, err := c.run.Run(ctx, c.cmd(p, "config", "--hash="+p.Service))
	if err != nil {
		return "", err
	}
	return ParseConfigHash(res.Stdout, p.Service)
}

func (c *Compose) Recreate(ctx context.Context, p Project) error {
	_, err := c.run.Run(ctx, c.cmd(p, upArgs(p, true)...))
	return err
}

func (c *Compose) Up(ctx context.Context, p Project) error {
	_, err := c.run.Run(ctx, c.cmd(p, upArgs(p, false)...))
	return err
}

var _ Reader = (*Compose)(nil)
var _ Actor = (*Compose)(nil)

// DryRun is an Actor that changes nothing: in dry-run mode it is the only compose Actor the healer holds.
type DryRun struct {
	// Would is called with what was skipped.
	Would func(verb string, p Project)
}

func (d DryRun) Recreate(_ context.Context, p Project) error { return d.note("recreate", p) }
func (d DryRun) Up(_ context.Context, p Project) error       { return d.note("up", p) }

func (d DryRun) note(verb string, p Project) error {
	if d.Would != nil {
		d.Would(verb, p)
	}
	return nil
}

var _ Actor = DryRun{}

// Describe is a one-line summary of a project for logs.
func (p Project) Describe() string {
	return fmt.Sprintf("%s/%s (%s)", p.Name, p.Service, strings.Join(p.ConfigFiles, ","))
}
