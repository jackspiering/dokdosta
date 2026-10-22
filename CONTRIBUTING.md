# Contributing

whalenurse is a small project with a written design, and the way it is built is part of what keeps it safe to run next to
your containers. This page is how the work is organised, and how to send a change.

## The design comes first

[docs/PLAN.md](docs/PLAN.md) is the design (its version and approval status are at the top). It is built **one milestone at a
time, in order** (H, M0, M1, M0b, M2a, M2b, M3, M4, M5, M6, then the post-v1 milestones from M7); after each, the author
writes a report (what was built, how it was verified, what is uncertain, what the maintainer must do) and waits for review before the next. The per-milestone decisions, deviations and open
questions are in `docs/m*-notes.md`, and the facts about Docker's behaviour that the rules rest on are in
[docs/m0-findings.md](docs/m0-findings.md) and [docs/m0b-findings.md](docs/m0b-findings.md).

A few rules from the plan that you will meet in review:

- **If a finding or a test contradicts a plan rule, stop and say so**; do not quietly change the design. A change to a rule is a
  new plan version, written into `docs/PLAN.md` (a "Changes in vX.Y" list), and `CLAUDE.md` is updated in the same pull request.
- **Check upstream before relying on a fact** about Docker, Compose or a dependency, and record what you checked.
- Healing **never upgrades by accident**: every compose `up` is `--pull never --no-build`; only the opt-in pull rung pulls;
  `compose down` is never used.
- **Events are only triggers.** Decisions come from inspecting the container; see plan section 3.3.
- Secrets (notification URLs, the HTTP token) are never logged, shown or put in an error. Tests grep for them; extend them when
  you add output.
- No network-exposed write endpoints.
- Lowercase `whalenurse` everywhere: module path, image, binary. Build with `CGO_ENABLED=0`.

## Pull requests

- One topic per pull request, on a branch (not `main`); `main` is protected and needs the CI checks to pass.
- Explain what and why; link the plan section if there is one. Keep the code like its neighbours: comment density, naming,
  idiom.
- Behaviour changes need a test, and tests that can fail: the existing ones have mutation checks recorded in the notes. A bug fix
  starts with a test that fails.
- Add a line to the `[Unreleased]` section of [CHANGELOG.md](CHANGELOG.md) for anything a user would notice.
- Update the docs (`docs/configuration.md`, `docs/labels.md`) when you add a variable or a label: a test fails if you add one and
  forget (`TestEveryVariableAndLabelIsDocumented`).
- Workflows: every action pinned to a commit SHA with the version as a comment (`scripts/ci/check-pins.sh`); `actionlint` clean.

## Running the tests

```
go vet ./... && go vet -tags integration ./...
go test -race ./...                       # unit tests; they touch no Docker
CGO_ENABLED=0 go build -trimpath -o whalenurse ./cmd/whalenurse
make scripts-test                          # the shell tooling's self-tests; no Docker, no network
shellcheck -x $(grep -rIl --exclude-dir=testdata -E '^#!.*(ba)?sh' scripts test; find scripts test -name '*.sh')
```

## Integration tests, and the rule that protects your Docker host

`test/integration/` holds tests that run against a **real Docker daemon** (`-tags integration`, `WHALENURSE_INTEGRATION=1`).
They run in CI on GitHub-hosted runners, in a matrix of two image stores (classic and containerd) times three compose releases
(5.2.0, 5.6.0, latest). **Do not run them on a machine you care about** unless you have read `test/integration/guard.go`.

The rule, enforced in code and not by convention: **they act only on compose projects named `whalenurse-test-*`** (the M0 scripts
on `whalenurse-m0-*`). Every method of the test `Guard` that changes anything checks the project name of its target against
`^whalenurse-test-[a-z0-9][a-z0-9_-]*$` and **fails closed**: no match, no action. The guard cannot express `down` for another
project, pulls only the base images it lists, tags and pushes only under `whalenurse-test/` or the local test registry on
`127.0.0.1`, and wraps whalenurse's own Docker and compose actors so that the healer under test cannot touch anything else
either. The guard has its own unit tests that need no Docker. If you add a new thing the tests do to Docker, it goes through
the guard, with a test that it refuses the wrong project.

The scripts follow the same rule (`scripts/ci/setup-docker.sh` and `test/demo/run.sh` refuse to run outside GitHub Actions).
The M0 scenarios in `test/m0/` restart the daemon or reboot a machine and are run by the maintainer on a throwaway VM; do not
run a probe outside the agreed scenarios, or anything that can exhaust CPU, memory, disk or ports.

## Fixtures

Never commit raw `docker inspect`, `docker events` or `journalctl` output. Raw captures go in `/captures/` (gitignored); only
files scrubbed with `scripts/scrub-fixture` go in `testdata/`. The scrubber removes environment variables, command lines,
label values outside `com.docker.compose.*` and `whalenurse.*`, host paths, hostnames and IPs.

## Releases

Only the maintainer decides to tag: by hand, or by telling an agent to make one specific tag, from `main`
([docs/releasing.md](docs/releasing.md)). Contributors and agents never tag on their own. Nothing in a pull request publishes
anything; CI builds the same artifacts as a GoReleaser snapshot to prove they build.

## Licence

By contributing you agree your contribution is licensed under the MIT licence of the project.
