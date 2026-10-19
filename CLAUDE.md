# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository state

The design lives in `docs/PLAN.md` (**v3.8, proposed 19 Oct 2026 for review**: it is v3.7 plus the M4 findings and decisions; v3.7 was approved 19 Oct 2026 and is v3.6 plus the M3 decisions and the maintainer's answers on its review; v3.6 was approved 18 Oct 2026 and is v3.5 plus the M2b findings and the maintainer's decisions on its review; v3.5 added three approved M2a verdicts (the `last_seen` heartbeat, the stricter crash-loop rule, the shutdown-window rule for containers stopped by the host), v3.4 split M2 into M2a and M2b, v3.3 holds the approved M0b verdicts). Milestones H (repo housekeeping), M0 (diagnose and verify), M1 (restart-only heal MVP), M0b (the scenarios M0 could not run), M2a (CI on Docker 29, the SQLite store, the full ladder, the CLI, shutdown handling) and M2b (`internal/compose`, rung 2 recreate, the drift guard with the version gate, dependents, the compose CI matrix) are built, M3 is built (plan §3.12: `internal/notify`, Shoutrrr, `notify test`, the uptime ping), and **M4 is built** (plan §3.7, v3.8: the opt-in pull rung, `TagMoved`, the "upgraded during heal" notification with its rollback). **M5 (the read-only dashboard and API) is next.** The M0 results are in `docs/m0-findings.md`, the M0b results in `docs/m0b-findings.md`, the M1 decisions and deviations in `docs/m1-notes.md`, the M2a ones in `docs/m2a-notes.md`, the M2b ones in `docs/m2b-notes.md`, the M3 ones in `docs/m3-notes.md`, the M4 ones (and its open questions) in `docs/m4-notes.md`, and how to run the daemon and the CLI in `docs/running.md`.

Layout:

- Go module `github.com/jackspiering/whalenurse` (Go 1.27.2). `cmd/whalenurse` has `run [--dry-run]`, `pause`, `resume`, `unsuspend`, `status` (over the control socket), `notify test` (direct, no daemon needed), `healthcheck` and `version`. `internal/` has `docker` (moby client behind interfaces, plus a `Fake`), `compose` (binary lookup, typed commands behind a `Runner`, the dry-run oracle, the drift `Guard`, rollback commands, a `Fake`), `rules`, `engine`, `daemon` (wires events, poll, sweep, the pre-action guard, the drift guard, the version gate, the dependents step and the actors), `config`, `clock`, `api` (`/healthz`), `control` (unix socket: `/healthz` and the CLI commands), `store` (SQLite via `modernc.org/sqlite`) and `notify` (Shoutrrr, `github.com/nicholas-fedor/shoutrrr` v0.22.0: templates, dedupe, per-URL rate limit, the redactor, the uptime ping). The planned package `web` does not exist yet.
- `test/integration/`: tests against a real Docker daemon (build tag `integration`), with the fail-closed guard in `guard.go` (also for the M4 `registry:2` on `127.0.0.1:<port>`, `whalenurse-test-registry-*`, and the rollback lines it runs after checking them).
- `scripts/m0/`, `scripts/scrub-fixture` (inspect, events and `--journal` modes), `test/m0/`, `testdata/inspect/`, `testdata/events/`, `testdata/journal/`: the M0 tooling and scrubbed fixtures.
- `test/m0/m0b/`: the M0b scenarios (and `whalenurse-restart/`, the M2a check with whalenurse in a container, and `whalenurse-recreate/`, the M2b check: whalenurse for real, a parent only a recreate repairs, and its sidecar), each `prepare.sh` → maintainer step (`step.sh`) → `collect.sh`, plus `image-drift.sh` (store-aware, for CI), `teardown.sh` and `selftest.sh`. State goes to `captures/m0b/` (gitignored). `image-drift.sh` also runs in CI (`image-drift` job in `ci.yml`, both image stores).
- `Dockerfile` (distroless/static:nonroot; `/data` is a volume owned by 65532; the compose plugin is bundled with a checksum at `/usr/libexec/whalenurse/docker-compose`), `.github/workflows/ci.yml` (the integration job is 2 image stores × compose 5.2.0, 5.6.0, latest), `scripts/ci/setup-docker.sh <store> [compose]` (CI's Engine 29.x + the compose release asked for, checksum verified, and the image-store switch; refuses to run outside GitHub Actions), `examples/compose.yaml`.

Commands:

```
go vet ./... && go test -race ./...                      # unit tests; touch no Docker
CGO_ENABLED=0 go build -trimpath -o whalenurse ./cmd/whalenurse
shellcheck -x $(grep -rIl --exclude-dir=testdata -E '^#!.*(ba)?sh' scripts test; find scripts test -name '*.sh')
scripts/m0/selftest.sh && scripts/scrub-fixture --self-test && test/m0/run.sh selftest && test/m0/m0b/selftest.sh   # self-tests: no Docker needed
```

Pushing a workflow change needs SSH (`git push git@github.com:jackspiering/whalenurse.git <branch>`): the `gh` HTTPS token has no `workflow` scope. The integration tests (`WHALENURSE_INTEGRATION=1 go test -tags integration ./test/integration/`), `test/m0/run.sh all` and the `test/m0/m0b/` scenarios act on a real Docker daemon; the M0b steps restart the daemon, reboot or switch the image store, so only the maintainer runs them. Never run a probe outside the agreed scenarios, or anything that can exhaust CPU, memory, disk or ports, without asking (M0b's 4000-port probe nearly exhausted the VM). The integration tests run in CI on GitHub-hosted runners; do not run them locally unless the maintainer has confirmed the host (see Docker host safety below).

whalenurse is a compose-aware self-healer for Docker hosts. For opted-in containers that go unhealthy or stop unexpectedly, it restarts them, then recreates them from the stack's own compose files, then (opt-in) pulls and recreates. It notifies on giving up. It is a single static Go binary that runs as a container (socket mounted) or under systemd.

## Working protocol (from the plan)

- Work **one milestone at a time, in order: H, M0, M1, M0b, M2a, M2b, M3 … M6**. After each, stop and report what was built, what was verified and how, what is uncertain, and which **[maintainer]** items are open. Wait for review before starting the next.
- **[maintainer]** items (GitHub settings, anything on the real Docker host) are not yours to do. List them in the report.
- **When you change `docs/PLAN.md`, update `CLAUDE.md` to match in the same PR.**
- **Check upstream before relying on facts.** Verify Docker/Compose behavior and dependency versions against current source, docs or releases. Use the latest stable Go and the latest release of each dependency. Record differences from the plan.
- **If a finding or test contradicts a plan rule, stop and report it.** Don't quietly change the design.
- **Docker host safety:**
  - Only act on containers in compose projects named `whalenurse-test-*` or `whalenurse-m0-*`.
  - Never stop, restart, recreate, pull, prune or remove anything else.
  - Integration tests and M0 scripts must enforce this in code with a project-name filter that fails closed.
  - Don't run M0 experiments (or the M0b scenarios) until the maintainer confirms the host.
- **Fixtures:** never commit raw `docker inspect`, `docker events` or `journalctl` output. Raw captures go in `/captures/` (gitignored). Only scrubbed fixtures (via `scripts/scrub-fixture`) go in `testdata/`.
- Build with `CGO_ENABLED=0`.
- Lowercase `whalenurse` everywhere: Go module path, GHCR image (`ghcr.io/jackspiering/whalenurse`) and binary.

## Architecture (planned)

```
cmd/whalenurse/      main + subcommands (run, pause, resume, unsuspend, status)
internal/docker/     interface over github.com/moby/moby/client (events, inspect, restart, list) + fake
internal/compose/    compose binary resolution, command builder, hash/dry-run helpers
internal/engine/     incidents, ladder, flap limit (pure; injected clock; no Docker calls)
internal/rules/      inspect classification (State.Error, exit codes, heal-exited), fixture-tested
internal/store/      SQLite (modernc.org/sqlite) persistence + retention
internal/notify/     Shoutrrr wrapper, templates, dedupe, rate limit, redactor, uptime ping
internal/api/        /api/v1 JSON + SSE, /healthz, /metrics
internal/web/        read-only dashboard (html/template, embedded assets, no Node build)
internal/control/    unix-socket control server for the CLI
internal/config/     env + label parsing
```

Cross-cutting design points that span several packages:

- **Events are only triggers.**
  - Decisions come from inspecting the container after `GRACE`, because Docker event order is unreliable. `docker stop` emits `kill`, `stop`, `die` on Engine 29.9.0, and the order differs between operations and Engine versions.
  - A failed restart-policy restart emits **no event**, so the poll detects it. The poll also re-inspects containers still `restarting` after `GRACE`.
  - `health_status` events are edge-triggered, so the safety poll (default 60s) is the level-triggered backstop.
  - A startup sweep runs after `STARTUP_SETTLE`, and again after every reconnect that follows a lost events stream. The Docker API is unavailable while the daemon restores containers (API errors then mean "not ready", not an incident), and `since=` replay holds only the new daemon's last 256 events, so replay never replaces the sweep.
  - **Shutdown kills:** a daemon shutdown or reboot sends `kill` to every running container. Those kills must not set manual-stop markers or clear SUSPENDED. Mechanisms: (a) discard markers created in the 30 s before an events-stream loss, (b) persist `/proc/sys/kernel/random/boot_id` with the markers and drop them when it changes, (c) a restore window of `STARTUP_SETTLE` after reconnecting to a daemon that was down, in which `start` events are restores, (d) a persisted `last_seen` heartbeat: whalenurse is itself killed by a daemon shutdown (no stream loss, same boot id), so at startup the previous run's end counts as a stream loss (markers from the 30 s before `last_seen` are discarded; a gap over `POLL_INTERVAL` opens the restore window). Do **not** add a rule that recognises a burst of `kill[15]`: `docker compose stop`/`down` send the same burst, so deliberate stops would be healed.
- **One domain event stream** (`IncidentOpened`, `ActionStarted`, `ActionResult`, `Suspended`, `Recovered`, `Paused`, `TagMoved`, …). Store, notify and API/SSE all subscribe to it, so notifications and history can't disagree. Every event of a key with an open incident carries the `incident` attribute (`engine.AttrIncident`, the opening time), which notify dedupes on.
- **Notifications (M3, plan §3.12).**
  - `internal/notify` is a subscriber; `Bus.Publish` runs under the engine's lock, so `Notifier.Handle` only queues (drops and counts when full). One dispatcher renders and dedupes (the dedupe is persisted in the store's `notified` table through `notify.Ledger`; a dry run keeps it in memory); one worker and one Shoutrrr sender per URL deliver with a timeout, two retries and a per-URL rate limit. **A notification never blocks or delays healing**; failures are logged (redacted) and counted and never published onto the bus.
  - **The notification URLs and the uptime-ping URL are secrets.** Never log one, never put one in `status`, `/healthz` or an error: a URL is `#<n> <scheme>` everywhere, and every error goes through `notify.Redactor`. `config` never quotes these variables. `TestNoTokenReachesTheLogsStatusHealthzOrTheTestOutput` (`cmd/whalenurse`) greps for tokens; keep it passing and extend it when you add output.
  - Only the events in the §3.12 table are sent (`ActionResult`, `Recovered`, `Suspended`, `Unsuspended`, `TagMoved` ("upgraded during heal"), `DriftBlocked`, `DriftWarned`, `NotifyOnly`, `NeedsRecreate`, `RateLimited` (at most once an hour per URL), `RungsDisabled`, `Paused`, `Resumed`); `Suspended` with reason `drift` is folded into `DriftBlocked`, and `Suspended`/`Unsuspended` make each other news again within an incident. Add a template (with a test of the worst-case length) before adding a type. Every message names the host and the container key; title ≤ 72 and body ≤ 360 runes (720 for the upgrade message, whose rollback commands are never cut).
  - **Dry-run sends notifications**, marked `[dry-run]`. `whalenurse notify test` bypasses dedupe and the rate limit.
  - The uptime ping (`WHALENURSE_HEARTBEAT_URL`) is called the "uptime ping" in code, because "heartbeat" means the persisted `last_seen` of the shutdown handling. It is skipped while the daemon is unhealthy.
- **State and control (M2a).**
  - The engine persists through an interface (`engine.Persister`), implemented by `internal/store`; it stays pure and is safe for concurrent use (one mutex), because the control socket calls it from HTTP goroutines. Events are published under that lock, so subscribers must not call back into the engine.
  - **Dry-run stores nothing** (it does not open the store or need `/data`); `run` fails fast if it cannot open the store.
  - The CLI commands (`pause`, `resume`, `unsuspend`, `status`) are served on the unix socket only. The TCP listener gets the read-only `/healthz` handler; a test in `cmd/whalenurse` asserts it has no write endpoints. Keep it that way.
  - The poll inspects every opted-in container, because only an inspect carries `RestartCount` (the crash-loop rule, §3.3 rule 3, lives in `engine.Observe`).
  - The crash-loop rule (§3.3 rule 3, `engine.Observe`) needs ≥3 `RestartCount` increases, the first ≥ `CRASHLOOP_AFTER` ago **and the latest < `CRASHLOOP_AFTER` ago**.
  - `test/m0/m0b/whalenurse-restart/` is the throwaway-VM check of the shutdown handling with whalenurse itself in a container (prepare → `sudo systemctl restart docker` → collect); only the maintainer runs the step.
- **Ladder: one pass per incident.**
  - The rungs are restart (Docker API), recreate (compose), then optional pull+recreate (rung 3, `whalenurse.heal.pull=true`; rungs 2 and 3 are unavailable together below the version gate).
  - After each rung comes a cooldown, then a re-inspect.
  - All rungs failing leads to SUSPENDED.
  - More than `FLAP_LIMIT` incidents within `FLAP_WINDOW` also suspends, and closing an incident does not reset the flap count.
  - PAUSED is separate and expires (default 2h).
- **State key is `host/project/service/container-number`**, from compose labels, never the container ID, because a recreate changes the ID. Non-compose containers fall back to the container name and only get rung 1. The host ID is carried in every key, event and API record for the future multi-server hub.
- **Action-in-flight window:** events for a key that whalenurse is acting on are recorded and attributed to the action, not interpreted. They must not trigger manual-stop markers, clear SUSPENDED or open an incident.
- **Rule precedence** (`internal/rules`, plan §3.3):
  - Skip rules come first: own container, `compose.oneoff=True`, swarm tasks, not opted in, PAUSED/SUSPENDED.
  - A non-empty `State.Error` means a failed start. It overrides everything else, and the exit code is never used for it. There are three shapes: a failed first start is `created` + exit 126/127/128; a failed restart-policy restart is `exited` + the previous run's exit code + `RestartCount` incremented; a failed boot-time restore is `exited` + exit 126/127/128 mapped from the error + `RestartCount` unchanged.
  - The cause splits three ways:
    - `No such container` (parent recreated): recreate the dependent.
    - `cannot join network namespace of a non running container` (parent stopped): heal the parent first, then restart the dependent.
    - "Other" causes (missing bind source, port in use, bad command): start, then recreate.
  - Then unhealthy, crash-loop, and exited handling via `whalenurse.heal-exited`.
  - Keep error-string matching in one place, backed by fixtures, because strings vary by Engine version. The port-conflict string appears as `Bind for 0.0.0.0:80` and `Bind for :::80`; match on `port is already allocated`.
  - A container whose `FinishedAt` falls in a shutdown window (30 s before `last_seen` or the stream loss, up to the restore) was stopped by the host, **whatever its exit code**, and its restart policy decides: `restart: "no"` stays down with one notification (its `network_mode` dependents stay down too), `always`/`unless-stopped`/`on-failure` are left to Docker's restore (a failed restore is rule 1), only `heal-exited=always` starts it (plan §3.3 rule 4, v3.5).
- **Heal never upgrades by accident.**
  - Every compose `up` uses `--pull never --no-build`.
  - Only the opt-in pull rung may pull, and it runs `pull --policy always <one service>` (a bare `pull` silently skips `pull_policy: never|build|missing`; plan v3.8 item 1).
  - Never use `compose down`, because it removes the shared project network.
  - The drift guard (config drift, image drift, compose version skew) runs before recreate. Any failure skips the recreate, notifies, and sets SUSPENDED. `WHALENURSE_DRIFT_GUARD=warn` overrides verified drift only (`config`, `image`); "can't verify" findings (a check that could not be made, the bundled compose's skew) still block. An upgraded host plugin is information in the drift message, not a block.
  - Config drift: the primary check is the dry-run oracle, `--dry-run --progress json up -d --no-deps --pull never --no-build <svc>`, where a `Recreate` text for the container means drift. `config --hash` is the fallback when that output can't be parsed.
  - Both are skipped for services that reference another service or container (`network_mode`/`ipc`/`pid`/`volumes_from`), because compose hashes those after resolving them.
  - Image drift, on both image stores: compare the `com.docker.compose.image` label with what the reference resolves to for `linux/<arch>` (`docker image inspect --platform linux/<arch> --format '{{.Id}}' <ref>`, through the Engine API since the image has no CLI). **Never use `.Descriptor.digest`** (empty on the classic store, so every container would look drifted), and never `.RepoDigests` or the container's `.Image`. Plain `.Id` is never the comparator *alone*, but v3.6 also accepts it: compose before v5.5.0 writes the index ID into the label on the containerd store. `image inspect --platform` needs Engine 28.1 or later; older Engines have no fallback: rungs 2 and 3 are disabled below Engine 28.1 / Compose v5.2.0 (plan §3.6, *Minimum versions per rung*).
- **The pull rung (M4, plan §3.7, v3.8).**
  - Order: pre-action checks, the drift guard in **pull mode** (checks 1 and 3 only; `GuardInput.Pull`; an oracle `Recreate` is a config finding unless `config --hash` matches the label and the image moved), read the image ID (platform, and plain for the rollback), `compose pull --policy always <svc>`, read it again.
  - Unchanged: `engine.Result.Exhausted` suspends at once with "the pull found no newer image for <ref>". Changed: record the move **before** the `up`, non-forced `up`, a post-check that the container was replaced (`--force-recreate` otherwise), `TagMoved`, then the usual dependents step via `afterAction`.
  - `TagMoved` is persisted in the store's meta (`tagmoved:<normalized ref>`). A drift block names the mover (`moved_by`, `moved_at` attributes and a line of the message) only while the reference still resolves to what that pull produced.
  - The upgrade message's rollback commands (`compose.RollbackCommands`: plain old ID, the host's `docker compose`) are never cut: `MaxBodyPinned` 720 instead of 360 for a message with pinned lines. A pull `ActionResult` with an `outcome` is not notified (the giving-up and `TagMoved` messages say it).
  - Dry-run never pulls (`compose.DryRun.Pull`); private registries are unsupported (no `DOCKER_CONFIG` in the compose child); `Healthy()` tolerates an action in flight (`pullTimeout` 10 min) because the loop is synchronous.
  - Integration tests: a `registry:2` as a `whalenurse-test-registry-*` project; the Guard accepts only `127.0.0.1:<port>/whalenurse-test/...` for push/pull/tag and `pull --policy always <one service>` from compose. Every "no pull happened" assertion has a positive control (`TestLocalRegistryRoundTrip` checks that docker events reports a pull).
- **Dependents (`network_mode: container:<parent>` sidecars) break whenever the parent restarts or recreates.** (M2b: they are incidents of their own, one at a time with a scheduled health wait, never a sleep. The dependents step repairs the dependents of an **opted-in** parent without an opt-in of their own, unless `whalenurse.enable=false`; their own ladder still needs one, and the step does not count toward their flap limit.)
  - On a parent restart (same ID, parent started later), restart dependents via the API, one at a time.
  - On a parent recreate (parent ID gone), recreate the dependents with the non-forced `up -d --no-deps --pull never --no-build <dep>`. Use `--force-recreate` only if a post-check still finds a stale `NetworkMode` (M0 E1/E2).
  - **Never API-restart a dependent whose parent ID is gone.** The restart fails and turns a running-but-disconnected dependent into an exited one. It is recreated (`ActionRecreateStale`); only if compose is unavailable is it advice (`NeedsRecreate`).
  - A stale dependent stays `running` with only a loopback interface, so only the staleness sweep (or the parent's `start`/`create` event) finds it. The sweep also covers the boot-time race. At boot Docker restores a dependent after its parent, also behind a slow parent start; a parent that is not restored takes its dependents down (M0b).
  - While a parent has an open incident or an in-flight action, suppress its dependents' ladders.
  - A dependent never triggers an action on its parent.
- **Compose binary lookup order:** `WHALENURSE_COMPOSE_BIN`, then the host plugin mounted `:ro` at a fixed path, then the bundled copy. In container mode the stacks root must be mounted at the **identical path** as on the host.
- **Dashboard and API:** read-only, off by default, built on the versioned `/api/v1` JSON API. The CLI controls the daemon over a local unix socket, and there are no network-exposed write endpoints in v1. Never render env vars or full inspect output.
- The moby client (`github.com/moby/moby/client` + `/api`, not `github.com/docker/docker`) is pre-1.0, so keep it behind `internal/docker`.
- Minimum supported Docker Engine is v25, for rung 1 (restart) only. Rungs 2 and 3 (recreate, pull) and the drift guard require Engine ≥ 28.1 and Compose ≥ v5.2.0, checked at startup: below that, one warning, rungs 2 and 3 disabled, and the ladder ends with notify + SUSPENDED after rung 1. No store-guessing fallback (plan §3.6).

## Configuration surface

All env vars are prefixed `WHALENURSE_` (poll interval, grace, cooldown, flap limit, rate limit, drift guard, host ID, data dir, HTTP addr/token, the notification URLs, timeout and rate limit, the uptime ping, and others). Container labels use the `whalenurse.*` namespace (`enable`, `actions`, `heal-exited`, `heal.pull`, `cooldown`, `flap-limit`). Plan §4 holds the full tables with defaults. Update that table (and later the docs) when adding or changing a variable or label.

## Testing approach

- Engine unit tests use a fake clock and fake Docker.
- Rules are tested against scrubbed `docker inspect` fixtures, including M0 captures.
- API endpoints get golden JSON tests, which become the future hub contract.
- A test must prove env values never appear in dashboard output.
- Integration tests run in CI against real Docker, and only in `whalenurse-test-*` projects. The M3 ones send to a local webhook receiver through Shoutrrr's `generic://` service.
- The CI matrix covers the classic and containerd image stores, and compose v5.6.0 (the M0 version), the latest release, and v5.2.0 (the only release with compose bug #13878). The `image-drift` job (M0b) already runs `test/m0/m0b/image-drift.sh` on both stores.
