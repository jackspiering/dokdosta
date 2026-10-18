# Running whalenurse (M2b)

whalenurse watches Docker, and for opted-in containers that go unhealthy, crash-loop, or stop unexpectedly it runs rung 1 (a Docker API `restart` or `start`), then rung 2 (recreate from the stack's own compose files, behind a drift guard), and repairs `network_mode` sidecars that depend on a container it restarted or recreated. M2a added the persisted state, the full ladder (cooldown, recovery period, flap limit, SUSPENDED, PAUSED), the CLI and shutdown handling; M2b adds compose. Notifications arrive in M3 and the pull rung (rung 3) in M4. Nothing is published yet; build the image yourself.

## Recreate: two read-only mounts, and the versions it needs

Rung 2 runs `docker compose -p <project> --project-directory <working_dir> -f <files> [--env-file <f>] up -d --force-recreate --no-deps --pull never --no-build <service>`, with every input read from the container's own compose labels. It never runs `down`, never pulls, never builds. In a container it needs:

1. **the host's compose plugin, mounted read-only at `/host/docker-compose`** (usually `/usr/libexec/docker/cli-plugins/docker-compose`; `docker info` shows it). Lookup order: `WHALENURSE_COMPOSE_BIN`, then `/host/docker-compose`, then the copy bundled in the image at `/usr/libexec/whalenurse/docker-compose`, then (under systemd) the host's plugin directories. The bundled copy is a fallback: when a container was made by another compose release, the drift guard cannot tell drift from a changed hash layout and says "can't verify drift (compose X on host vs Y in whalenurse)" instead of recreating.
2. **the stacks root, mounted read-only at the identical path as on the host** (`/opt/stacks:/opt/stacks:ro`). Compose passes host paths to the daemon for relative bind mounts, so mounting it elsewhere (`/opt/stacks:/stacks:ro`) makes the recreate fail. Also mount whatever else compose reads from outside it: `env_file:` paths with `../`, `include:` and `extends:` files, file-based `secrets:` and `configs:`. The directory and its parents must be traversable by uid 65532.

If the files or labels are missing or unreadable, rung 2 is skipped for that container with a message that says so (`RungUnavailable`) and the ladder ends with SUSPENDED after the restart.

**Versions.** Rung 1 works from Engine v25. Rungs 2 and 3 and the drift guard need Engine 28.1 or later and Compose v5.2.0 or later. whalenurse checks both at startup (and again after the events stream comes back): below that, or when a version cannot be read, or without any compose binary, it logs **one warning**, disables every use of compose, and the ladder ends with SUSPENDED after rung 1. `whalenurse status` shows the binary, its source, the versions and the disabled rungs.

## The drift guard

A recreate deploys what is on disk and in the local image store *now*, so it runs checks first (plan §3.6). `WHALENURSE_DRIFT_GUARD=strict` (default) skips the recreate on any finding, reports the cause (`DriftBlocked`) and SUSPENDS the container; `warn` recreates anyway and says so (`DriftWarned`) when the findings are verified drift (a compose edit or a moved tag), but still blocks on a finding that says "can't verify" (a check that could not be made, or the bundled compose's version skew). If the host plugin was upgraded since the container was created, the drift message says so ("compose upgraded from X to Y since this container was created"): the change may be the hash layout rather than an edit.

1. **Config drift**: compose's own `--dry-run --progress json up ...` says whether it would recreate the container (it covers a compose edit, an env change and a moved image tag; it has no side effects). If its output cannot be read, `config --hash` is compared with the container's `com.docker.compose.config-hash` label.
2. **Image drift**: the container's `com.docker.compose.image` label against what its image reference resolves to for `linux/<arch>` now (the platform image ID; correct on both image stores). A reference that is gone locally is a failure.
3. **Version skew**: only for the bundled compose, see above.

A container whose `network_mode`, `ipc`, `pid` or `volumes_from` names another container skips check 1 (compose hashes those after resolving them, so they never match) and gets checks 2 and 3.

## Dependents (`network_mode: service:<parent>` sidecars)

A sidecar breaks whenever its parent restarts or is recreated, by anything. whalenurse finds them from the parent's `start`/`create` events and from the staleness sweep in every poll, and handles each as an incident of its own: parent restarted with the same ID (the sidecar started earlier) restarts the sidecar through the API; parent recreated (the sidecar's `container:<id>` is gone) recreates it with `up -d --no-deps --pull never --no-build <dep>` **without** `--force-recreate` (and with it only if a post-check still finds it stale). A sidecar whose parent ID is gone is never restarted: that fails and turns a running sidecar into an exited one. They go one at a time, each given up to 90 s to come back (running, and healthy if it has a healthcheck) before the next; they wait while their parent has an open incident or an action in flight; and a failing sidecar never triggers an action on its parent. The dependents of an opted-in parent are repaired **without an opt-in of their own**, unless they are labelled `whalenurse.enable=false`; this is the dependents step only: a sidecar that fails on its own (unhealthy, exited) is healed only if it is opted in itself. A dependent repaired this way does not count toward its own flap limit.

A `restart: "no"` parent stopped by a host shutdown stays down, and so do its sidecars: one `NotifyOnly` event names the parent and them.

## Opt a container in

```yaml
services:
  web:
    labels:
      whalenurse.enable: "true"
```

`WHALENURSE_MODE=all` opts in everything except containers labelled `whalenurse.enable=false`. whalenurse never acts on itself, on `docker compose run` containers, or on swarm tasks.

Labels read: `whalenurse.enable`, `whalenurse.actions` (`restart`, `recreate`; default both), `whalenurse.heal-exited` (`crashed` | `always` | `never`), `whalenurse.cooldown`, `whalenurse.flap-limit` (a positive integer replacing `WHALENURSE_FLAP_LIMIT` for this container). `whalenurse.heal.pull` is accepted and ignored until M4.

## Dry-run first

```
docker run --rm --read-only --group-add "$(stat -c %g /var/run/docker.sock)" \
  --tmpfs /run:uid=65532,gid=65532,mode=0700 \
  -v /var/run/docker.sock:/var/run/docker.sock whalenurse run --dry-run
```

Every decision is a JSON log line on stderr with a `type` such as `IncidentOpened`, `ActionResult` (with `attr.dry_run=true`), `Suspended`, `DriftBlocked`, `NeedsRecreate`, `WaitingForParent` or `NotifyOnly`. In dry-run the healer holds no-op actors for Docker and for compose, so it cannot change a container (the drift guard still runs, because it only reads: compose's own `--dry-run`, `config --hash`, an image inspect), **and it stores nothing**: incidents, SUSPENDED and pauses of a dry run live in memory only, so they are not there for the next real run to find. A dry run does not need `/data`.

## Docker socket access

Mounting `docker.sock` is **root-equivalent on the host**. The image is `gcr.io/distroless/static:nonroot` (uid 65532, no shell), so it cannot open the socket by default and fails with `permission denied while trying to connect to the docker API`. Give it the socket's group:

- container: `group_add: ["<gid>"]` where `<gid>` is `stat -c %g /var/run/docker.sock`; or
- run as root with `user: "0"` if you accept that.

Run it with a read-only root filesystem. Writable paths: `/run` (a tmpfs for the control socket) and **`/data`, a volume for the state store**. The image creates `/data` owned by uid 65532 and declares it a volume, so a fresh named volume (`data:/data`) is writable by the nonroot user. A bind mount keeps the host directory's owner: `chown 65532:65532` it. `whalenurse run` (not dry-run) refuses to start if it cannot open the store.

Under systemd, run `whalenurse run` as a user in the `docker` group and set `WHALENURSE_CONTROL_SOCKET` to a path that user can write, and `WHALENURSE_DATA_DIR` to a directory it owns.

## The CLI

The commands talk to the running daemon over its unix socket (`WHALENURSE_CONTROL_SOCKET`, mode 0600). Run them where the socket is, so with a container, through `docker exec`:

```
docker exec whalenurse /whalenurse status [--json]
docker exec whalenurse /whalenurse pause <project>[/<service>] [--for 2h]   # default 2h; --for 0 = until resume; 30m, 1d work
docker exec whalenurse /whalenurse resume <project>[/<service>]
docker exec whalenurse /whalenurse unsuspend <project>[/<service>]
```

- **pause** stops all actions for the project or service. Events are still recorded. A pause expires (default 2h) so a forgotten one cannot disable healing for good; when it ends, or on `resume`, the containers are looked at again within a second. A container outside compose is addressed by its name.
- **unsuspend** lifts SUSPENDED so the ladder may run again. For a container suspended for *flapping* it also forgets its flap history; otherwise its next incident would suspend it again at once.
- **status** lists every container that has an incident, a pause, flap history, a manual-stop marker or a crash-loop count, the pauses, and the rate-limit budget.

There are no write endpoints on the TCP listener (`WHALENURSE_HTTP_ADDR` serves `/healthz` only): the commands exist only on the unix socket.

## Settings

| Variable | Default |
|---|---|
| `WHALENURSE_DRY_RUN` | `false` (also `run --dry-run`) |
| `WHALENURSE_MODE` | `label` (or `all`) |
| `WHALENURSE_POLL_INTERVAL` | `60s` |
| `WHALENURSE_GRACE` | `30s` |
| `WHALENURSE_COOLDOWN` | `30s` (base; doubles per rung, capped at 5m) |
| `WHALENURSE_RECOVERY_PERIOD` | `10m` |
| `WHALENURSE_FLAP_LIMIT` | `3` |
| `WHALENURSE_FLAP_WINDOW` | `24h` |
| `WHALENURSE_CRASHLOOP_AFTER` | `5m` |
| `WHALENURSE_RATE_LIMIT` | `10` actions/hour |
| `WHALENURSE_STARTUP_SETTLE` | `2m` |
| `WHALENURSE_DATA_DIR` | `/data` |
| `WHALENURSE_HISTORY_RETENTION` | `30d` (closed incidents older than this are pruned hourly) |
| `WHALENURSE_HOST_ID` | hostname |
| `WHALENURSE_LOG_LEVEL` | `info` |
| `WHALENURSE_HTTP_ADDR` | empty (serves `/healthz` only when set) |
| `WHALENURSE_CONTROL_SOCKET` | `/run/whalenurse.sock` (`/healthz` and the CLI commands) |
| `WHALENURSE_DRIFT_GUARD` | `strict` (or `warn`) |
| `WHALENURSE_COMPOSE_BIN` | empty (see the lookup order above) |

Durations take `s`, `m`, `h` and also `d` (`30d`). Other `WHALENURSE_*` variables from the plan (`NOTIFY_URLS`, `HTTP_TOKEN`, `HEARTBEAT_URL`) are accepted with a warning and ignored until their milestone. An unknown `WHALENURSE_*` name logs a warning.

## What it does

- **The ladder, one pass per incident.** Rung 1 restarts (`restart` for running containers, `start` for created or exited ones), then the cooldown, then a re-inspect. If the container is still bad, rung 2 recreates it (drift guard first), then the cooldown and a re-inspect again; if it is still bad, the container is **SUSPENDED** (`Suspended`): nothing more is tried. A rung that cannot run (the version gate, no usable compose inputs, a container compose did not make) is recorded (`RungUnavailable`) and the ladder goes on. Each rung counts against the rate limit, dependents included. A failed start whose cause is a network that is gone goes straight to rung 2.
- **Recovered** means a healthcheck says `healthy`, or, with no healthcheck, the container is running and `RestartCount` has not changed for `RECOVERY_PERIOD`. SUSPENDED clears at that point. The incident itself closes (`Recovered`) only after the container has stayed recovered for `RECOVERY_PERIOD`; if it fails again before that, it is the same incident and the ladder goes on where it was, so a spent ladder means SUSPENDED again.
- **Flapping.** More than `FLAP_LIMIT` incidents within `FLAP_WINDOW` suspend the container the next time an incident opens (nothing is tried), even if every earlier one recovered. Closing an incident does not reset the count; only the window expiring does.
- **SUSPENDED also clears** on a `create` by someone else, or on a `start` that follows a manual stop (someone stopped it, someone started it). A `start` by the restart policy never clears it. Nor does a `start` inside a restore window, nor one that follows a quarantined shutdown kill (below).
- **Crash loops.** At least 3 `RestartCount` increases, the first at least `CRASHLOOP_AFTER` ago and the latest less than `CRASHLOOP_AFTER` ago (so a few quick restarts followed by a long quiet are not a loop); the count clears after `RECOVERY_PERIOD` without an increase. The poll reads `RestartCount` of every opted-in container, so a loop is found whether or not its `die` events were seen.
- **Shutdown kills.** A daemon restart or reboot sends `kill` to every container, whalenurse included. When the events stream is lost, every manual-stop marker set in the 30 s before is discarded; a changed boot id drops all markers; and whalenurse writes a `last_seen` heartbeat (every 5 s and when it stops cleanly), so that at its next start the markers set in the 30 s before the previous run's end are discarded too, and a gap longer than the poll interval opens a restore window. For `STARTUP_SETTLE` after a reconnect (or a new boot id, or such a gap) a `start` is a restore and a `kill` sets no marker. After every reconnect that follows a lost stream, everything is swept once the settle time is over. API errors while the daemon restores are "not ready", never an incident. The price: a real `docker stop` in the 30 s before whalenurse itself stops is forgotten.
- **Stopped by the host.** A container whose `FinishedAt` falls in the shutdown window (30 s before `last_seen` or the stream loss, up to the daemon's restore) was stopped by the host, whatever its exit code (an app that handles SIGTERM may exit 1). Its restart policy decides: `restart: "no"` stays down with one `NotifyOnly`; `always`, `unless-stopped` and `on-failure` are left to Docker's restore (a failed restore is a `State.Error`, handled as a failed start); `whalenurse.heal-exited=always` starts it. The window after a restart within one boot ends 5 minutes after `last_seen`.
- A container whose `network_mode: container:<id>` parent was recreated (the ID is gone) is **never** restarted: that fails and turns a running dependent into an exited one. It is recreated (see Dependents). If the compose rungs are disabled it is only advice (`NeedsRecreate`), as in M2a. If the parent is only stopped, whalenurse acts on the parent alone and starts the dependent after the parent runs.
- `/healthz` reports unhealthy when the Docker events stream has been down longer than twice the poll interval.

Decisions and open points: [m2a-notes.md](m2a-notes.md).
