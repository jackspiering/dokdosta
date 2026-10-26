# Changelog

All notable changes to whalenurse are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/). The design history behind each change is in
[docs/PLAN.md](docs/PLAN.md) and the `docs/m*-notes.md` files.

The release workflow publishes the section of the tag that is pushed as the release notes. Before tagging, replace
`YYYY-MM-DD` of the version about to be released with the release date (see [docs/releasing.md](docs/releasing.md)); the
workflow refuses an undated section.

## [Unreleased]

### Changed

- The test compose fake now empties a sidecar's `depends_on` label on a recreate, like `compose up --no-deps`, and a test guards that the pull rung still finds the sidecar of a parent's moved tag (tests only).

### Fixed

- **The integration tests' host-safety preflight (and the M0b one) now sees every spelling of an opt-in.** It listed strangers
  with an exact `whalenurse.enable=true` filter, so a container outside a test project with `1`, `T` or `TRUE` slipped past it;
  it now filters by key and reads the value with whalenurse's own parser. `CONTRIBUTING.md` says which tests the guard wraps
  and which rely on the preflight alone.
- A sidecar without its own opt-in that shares its parent's `ipc` or `pid` namespace (not only `network_mode`) is now found and repaired when the parent restarts or is recreated while whalenurse runs, not only after a whalenurse restart.
- A stale sidecar whose depended-on service is down now waits with one `WaitingForParent` event instead of an `ActionRefused` every `GRACE` (which flooded the log and the event history).

## [0.1.3] - 2026-10-26

Fixes from the code review of 21 Oct 2026 and the decisions of plan v3.13: compose's error output and event text no
longer leak secrets, a dropped Docker events stream and hung Docker calls are noticed, a stop lets a heal in flight
finish, a recreated whalenurse container keeps its state, and the notifications are quieter and exact.

### Security

- **Compose's error output no longer reaches the dashboard, the API, the store or the log.** A compose load error can quote
  a line of the stack's `.env` (an unterminated quote prints the value), and the drift guard put that text into its finding.
  A failed compose command now reports only its exit status and the causes whalenurse knows by name (a port conflict, a
  registry login failure, ...); run `docker compose config` in the stack to see the rest. Each output stream of a compose
  command is also capped at 4 MiB.
- **The last decision's reason is redacted like the start error.** For a failed start it quotes the container's
  `State.Error`, and the dashboard and `/api/v1/containers` showed it unredacted next to the masked start error. It is now
  redacted and cut to 600 characters; a long `State.Error` is cut on a character, not inside one.

### Added

- **Store failures are visible.** When `/data` is full or read-only, healing carries on from memory as before, but now
  `whalenurse status`, `/api/v1/status` (a new `store` object: the error count, the time and kind of the last failure),
  `/metrics` (`whalenurse_store_errors_total`) and the overview page say that the state may not survive a restart.
  `/healthz` is unchanged. A failed read of the store is no longer taken for an absent value: an unreadable boot id is
  not overwritten at startup. The event IDs are seeded from the highest one stored, and `run` refuses a store whose event
  sequence cannot be read. A data directory with `?`, `#` or `%` in its path now opens the database at that path.

### Changed

- **A stop of whalenurse lets a heal in flight finish, and no longer spends its rung.** A restart, recreate or the `up`
  after a pull in flight when whalenurse is stopped (an upgrade, `systemctl restart`, a reboot) may finish for up to 30 s;
  compose is then stopped with SIGTERM, not SIGKILL. An action cut short is recorded as interrupted (`ActionInterrupted`),
  not failed, and runs again after the restart instead of the container being SUSPENDED. **Action:** give the container
  `stop_grace_period: 60s` (the examples do), or the systemd unit `KillMode=mixed` and `TimeoutStopSec=60s`, then
  `systemctl daemon-reload`.
- **The systemd unit starts again with Docker.** After `systemctl stop docker` and a start, whalenurse stayed stopped
  until a reboot. The unit is now also `WantedBy=docker.service`. **Action:** install the new unit, then
  `systemctl daemon-reload && systemctl reenable whalenurse`.
- A unit test now checks that the binary gives a dry run only the actors that change nothing, for the restart rung and
  for the compose rungs; before, only the restart one was checked, by an integration test.
- **A container whose healthcheck keeps flipping no longer sends a stream of messages.** SUSPENDED and "unsuspended" were
  news again after each other every time within an incident; now that happens once per incident (suspended, unsuspended,
  suspended again), and further changes are recorded and shown on the dashboard but not sent until the next incident. An
  unsuspend you run is always sent.
- **The rollback `up` line has `--no-build`**, like every other `up` whalenurse runs. If the tag line failed or was skipped,
  the `up` now stops with an error instead of building an image that is not the old one. Rollbacks already shown keep
  their form.

### Fixed

- **Retention keeps the events of an incident that is still open.** A container SUSPENDED for longer than
  `WHALENURSE_HISTORY_RETENTION` lost its cause, its drift findings and its rollback from the dashboard and the API. Those
  events now stay while the incident is open; the 100,000-row cap removes them last.
- **A failed "upgraded during heal" no longer shows as a success.** A notification URL with a boolean option
  (`preview=false`, `usehtml=true`) made the redactor mask every `true`/`false`, so the API carried `"ok": "***"` and the
  dashboard read a failed upgrade as done. Boolean option values are no longer masked, and the last upgrade's result is
  read before redaction.
- **The events filter and the request size are bounded.** `/api/v1/events` and `/history` refuse more than 64 types, a type
  over 64 bytes or a key over 512 bytes with a 400, so a crafted `type=` list can no longer burn a CPU (or, in a dry run, stall
  the healing loop); the request header is capped at 32 KiB; `/api/v1/incidents?limit=` above 500 now gives 500 instead of
  the default 100.
- **HEAD on `/api/v1/stream` answers at once.** It used to hold one of the 32 stream slots until whalenurse stopped, so a
  monitor sending HEAD requests could stop the dashboard's live updates for everyone.
- **A dropped Docker events stream went unnoticed.** The Docker client reports the end of the stream only on its error
  channel, so whalenurse kept waiting for events that never came: no resubscribe, no sweep after the reconnect, and `/healthz`
  stayed ok. It now resubscribes, sweeps after the settle time, and `/healthz` reports a stream that stays down, also when
  every resubscribe fails at once.
- **The compose version gate was read only at startup.** After the events stream came back, an Engine upgraded in the
  meantime was not seen, and a failed first read of the Engine version disabled rungs 2 and 3 until whalenurse was restarted.
  The gate is now read again before the startup sweep and the sweep after a reconnect, and at every poll until a first read
  succeeds. A pre-release of a minimum version (`28.1.0-rc.1`, `5.2.0-rc.1`) now counts as below it.
- **Healthcheck exec events could crowd out a `kill`.** whalenurse subscribed to every container event, and the
  healthcheck `exec_*` events of busy hosts filled the subscription during a long pull or recreate, after which Docker drops
  events for a subscriber that does not keep up, a manual stop's `kill` too. The subscription now asks only for the events
  whalenurse uses.
- **One hung Docker call could stop all healing.** A container list or inspect that never returned held the main loop,
  and `/healthz` said ok for up to 11 minutes. Every list, inspect and image inspect now has a 30-second timeout: a call that
  hangs fails for that container, which is tried again at the next poll, and the rest are still healed. `/healthz` (and so the
  image's health check and the uptime ping) now reports not ok when the Docker API has been failing for longer than twice the
  poll interval, also while an action is running.
- `whalenurse run` refuses any argument other than `--dry-run`: `run dry-run` or `run -- --dry-run` started a live
  healer. `whalenurse -h` and `--help` print the usage and exit 0 instead of exiting 1.
- A second `whalenurse run` no longer takes over a running daemon's control socket (its exit left the live daemon
  unreachable by the CLI and its health check): it refuses to start, and a second live run on the same data directory is
  refused by a lock on it.
- Under systemd, whalenurse no longer takes a container with `ipc: shareable` for its own container (it saw that
  container's `/dev/shm` mount in the host's mount table) and so no longer skips healing it. Its own container ID is read
  only from the `/etc/hostname`, `/etc/hosts` and `/etc/resolv.conf` binds.
- **A recreate of the whalenurse container no longer loses its state.** In a container the default host ID is the
  container's ID, so every upgrade gave every key a new host segment: SUSPENDED containers were healed again from the
  start, hand-stopped ones could be started, and flap history was reset. At startup the stored state is now moved to the
  current `WHALENURSE_HOST_ID` (on a collision, the more recent record wins). whalenurse warns when the host ID is its
  own container ID, and the examples set `WHALENURSE_HOST_ID`.
- **Event text is redacted in the log and in notification bodies**, as it already was in the API. An error that quoted a
  notification URL, the uptime ping URL, the HTTP token or a registry credential reached the container log unmasked, and
  was pushed to every notification service (one URL's token to another URL's channel). The message and the free-text
  attributes of every event now pass the redactor; a rollback command it would change is withheld from the notification.
- **The "upgraded during heal" message keeps its rollback commands exact.** After the rate limit had held messages back,
  the note that says so cut the end of the message, which is where the rollback commands are; the note now goes before
  them. Whitespace inside a quoted path of the commands was collapsed to one space; the commands are now sent byte for byte.
- **A failed rung-2 recreate that still changed the image is announced.** For a `whalenurse.heal.pull=true` container,
  compose replaces the container before it starts it, so a start that failed (a port in use) left it on the moved image
  without the "upgraded during heal" message or its rollback. That message is now sent, saying that the recreate failed.
- **The dashboard and the API show the rollback commands exactly, or not at all.** The redactor that masks the
  notification URLs and the token could change ordinary words of a rollback command (a topic named `docker`, a registry on
  the receiver's host), and the page offered the masked command with a Copy button. Such a rollback is now withheld, and the
  page says so.
- **A container suspended again after a drift block is notified.** When a drift-blocked container recovered and failed
  again in the same incident, the second SUSPENDED was dropped as if it repeated the drift block, so the last message said
  "unsuspended". It is now sent as a giving-up message, and the unsuspend after it is news again.
- **A message no URL could take is no longer recorded as sent.** With no notification URL, or only invalid ones, the
  dedupe still remembered each message for 24 hours, so after a working URL was added and whalenurse restarted, the
  version-gate warning and the "left down" advisories for problems that already existed were not sent.
- **A notification title keeps its headline.** With a long host ID (the default in a container is its 12-character ID)
  or the dry-run marker, the end of the title was cut, so a phone push could lose "SUSPENDED" or "=warn)". The container
  name gives way first now, then the host; the message body still names both.
- **The unsuspend and `docker compose up` commands to paste are shell-quoted.** The SUSPENDED banner and the
  notifications put the project and service names from the container's labels (which an image can ship) into a command
  unquoted, so a name with `$(...)` or `;` would run in the shell it was pasted into. Ordinary names show unchanged.

### Documentation

- **A rollback does not last while `whalenurse.heal.pull` stays on.** The next heal reaches the pull again and the registry's
  newer image comes back. The "upgraded during heal" message, the dashboard and the docs now say so and how to keep the old
  image: pin it in the compose file or remove the label before running the `up` line.

## [0.1.2] - 2026-10-22

A security fix (a compose label could put a flag on the compose command line), fixes from a code review, private
registries for the pull rung, and a hardened release pipeline.

### Security

- **A compose label could put a flag on the compose command line.** The project and service labels, which an image
  can ship, were passed to compose unchecked; a service of `--dry-run=false` overrode the drift guard's `--dry-run` and ran a
  real `up` of the project, in dry-run mode too. whalenurse now refuses a project or service that is not a name compose would
  write, and that container gets the restart rung only. 0.1.0 and 0.1.1 are affected.
- **The release is built from an empty Go cache.** The release workflow no longer restores the Go build and module cache
  that `main`'s CI saves (`actions/setup-go` cached both by default), so nothing compiled in another run can reach the
  signed binaries and image. CI checkouts no longer keep the token in `.git/config`.
- **The image's builder is pinned.** The BuildKit that builds the release image is pinned by digest
  (`moby/buildkit:v0.33.1`, the release the floating `buildx-stable-1` named), and the Dockerfiles no longer fetch the
  floating `docker/dockerfile:1` frontend; BuildKit's built-in one is used.
- **Go 1.27.3.** The binaries and the image are built with Go 1.27.3, which fixes GO-2026-6809 (`crypto/x509` trusted
  compromised intermediate certificates; whalenurse reaches it through the SMTP notifications). CI and the release now run
  `govulncheck`, and check that the Dockerfiles build with the Go of `go.mod`.
- **Every action pin is checked against its tag.** CI resolves the release tag in each pinned action's comment and fails
  if the SHA is not that tag's commit (a drifted comment, or a commit that exists only in a fork), and refuses `uses` keys
  written in flow style or quoted, which the pin check could not see.

### Added

- **Private registries for the pull rung (R5).** `WHALENURSE_REGISTRY_AUTH_DIR` names a read-only directory holding a
  `config.json` with inline `auths` and nothing else. It is given to `docker compose pull`, the pull rung's pull, as
  `DOCKER_CONFIG`, and to no other command. Any other key (`credsStore`, `credHelpers`, `currentContext`, `proxies`, ...),
  a `contexts` entry, an entry without an inline credential, or an empty `auths` stops `run` at startup; the file is checked
  again before every pull. The credentials are redacted from logs, `status`, the API, the dashboard and notifications, and
  `status`, `GET /api/v1/status` (`pull_auth.registries`) and the home page name the registries, never a user name or a
  secret. The risk (credentials in a container that holds the Docker socket) and the advice (a pull-only token, mode
  0440, a `:ro` mount) are in [docs/security.md](docs/security.md#private-registries).

### Changed

- **examples/full trusts its Tailscale proxy.** The `dashboard` network has a fixed subnet (`DASHBOARD_SUBNET`, default
  `10.84.248.0/29`) set as `WHALENURSE_HTTP_TRUSTED_PROXIES`, so the wrong-token limit counts per tailnet client instead of
  one budget shared by everyone behind Tailscale Serve. **If you run this example already**, `docker compose up -d` has to
  give the existing `dashboard` network the new subnet; if compose refuses, run `docker compose down` for this example's own
  stack (whalenurse and its Tailscale container only), then `up -d`.

### Fixed

- **`make verify-release` says why a check failed.** With `gh` installed but not logged in it reported a genuine release as
  `NOT VERIFIED` with "rejected" lines; it now says that `gh` is not logged in (`gh auth login` or `GH_TOKEN`), and every failed
  cosign or gh call shows the tool's own error. It prints the image digest it verified and checks the image's provenance
  for that digest, and the by-hand `gh attestation verify` commands in the docs pin the release workflow (`--signer-workflow`).
- **A `docker compose run` container hid its service from healing and the dashboard.** Compose writes no
  container number on a one-off container, so it shared the state key of the service's first container and, being newer,
  replaced it in the index: the service went unhealed for as long as the one-off container existed. A one-off or swarm-task
  container no longer takes a key from a container whalenurse watches, and `com.docker.compose.oneoff` is read the same
  case-insensitive way everywhere.
- **A final release tagged on the same commit as its `-rc` is released as itself.** The release workflow now tells
  GoReleaser the tag it is releasing; before, GoReleaser took the first tag on the commit, the `-rc`, and would have
  re-published the `-rc` image tag and failed to create the release.
- **Docs: backing up the store.** [docs/upgrading.md](docs/upgrading.md#backing-up-and-restoring-the-store) now says to stop
  whalenurse before copying its SQLite database (a copy of a running one may not open), and gives the backup and restore
  commands for the container and systemd setups, including removing a newer version's `-wal`/`-shm` before a restore.
- **Docs: no root alternative for the socket.** [docs/running.md](docs/running.md#docker-socket-access) offered `user: "0"`
  instead of `group_add`, which cannot start with the documented hardening (`cap_drop: [ALL]`: root cannot write `/data` or the
  `/run` tmpfs, both owned by uid 65532). `group_add` is now the only way given, and troubleshooting explains the failure.
- **Docs corrections:** the boolean spellings, the cooldown cap and the compose labels read (labels.md); what whalenurse
  writes and when it forces a recreate (security.md); `%2C` for a comma in a notification URL; what a dry run cannot know
  after a reboot (running.md).

### Documentation

- **Labels from the image.** [docs/labels.md](docs/labels.md#labels-from-the-image) and
  [docs/security.md](docs/security.md) now say that Docker copies an image's own labels onto its containers, so an image's
  publisher can set `whalenurse.*` and compose labels too (an image can opt itself in), how to check an image, and how to
  override a label. labels.md no longer says that a container compose did not make always gets only the restart rung.

## [0.1.1] - 2026-10-20

A fix to how whalenurse starts compose.

### Fixed

- **The compose child could read `$HOME/.docker`.** whalenurse removed `DOCKER_CONFIG` from the environment of the compose
  commands it runs, but kept `HOME`, and with `DOCKER_CONFIG` unset the Docker CLI reads `$HOME/.docker/config.json`. Under
  the example systemd unit that is `/var/lib/whalenurse/.docker`: a `config.json` there would have given compose registry
  credentials and a `currentContext` pointing at another daemon. Every compose command now runs with
  `DOCKER_CONFIG` set to an empty directory whalenurse creates for itself at startup (mode 0700, next to the control socket,
  else in the temp directory; removed at exit), so no Docker config is ever read. It has to be writable: compose v5.2.0 to
  v5.5.x write buildx state there during the drift guard's dry-run. If no such directory can be created, a startup warning
  says so and the drift guard of those versions uses `config --hash` instead of the dry-run.
  `DOCKER_AUTH_CONFIG` (credentials in the environment, read by recent Docker CLIs) is removed too. The container image was
  not affected in practice: nothing in it creates `/home/nonroot/.docker`.

## [0.1.0] - 2026-10-20

The first public release.

### Added

- **The healer.** For opted-in containers (`whalenurse.enable=true`, or `WHALENURSE_MODE=all`) that go unhealthy, crash-loop or
  stop unexpectedly: rung 1 restarts (Docker API), rung 2 recreates from the stack's own compose files, and the optional rung 3
  (`whalenurse.heal.pull=true`) pulls a newer image and recreates. One pass per incident, then SUSPENDED and a notification.
  Cooldown, a flap limit, a global action rate limit, pause (default 2 h) and `--dry-run`.
- **Compose-aware recreate.** `up -d --no-deps --pull never --no-build <service>` with the project, files and env file recorded
  on the container; never `compose down`. `network_mode: service:<parent>` sidecars are repaired after their parent is
  restarted or recreated, one at a time.
- **The drift guard.** A recreate is skipped, and the container SUSPENDED with the reason, when it would deploy a compose edit,
  a moved image tag, or when compose cannot be trusted to judge (version skew, a check that could not be made).
  `WHALENURSE_DRIFT_GUARD=warn` overrides verified drift only.
- **Decisions from the container's own state**, not from the order of Docker events: failed starts (`State.Error`), crash loops,
  `heal-exited`, and containers stopped by a host shutdown, which are left to their restart policy. Daemon restarts and reboots
  do not set manual-stop markers or clear SUSPENDED.
- **Persistent state** in SQLite (`/data`): incidents, SUSPENDED, pauses, flap history, the event log and the notification
  dedupe all survive a restart.
- **Notifications** through [Shoutrrr](https://github.com/nicholas-fedor/shoutrrr) (ntfy, Gotify, Discord, Telegram, Slack and
  more): dedupe per incident, a per-URL rate limit, `whalenurse notify test`, an optional uptime ping. Notification URLs are
  secrets and are never logged or shown.
- **The pull rung** reads the service's `pull_policy` (`never` and `build` are respected), runs the drift guard in image-move
  mode, tells you what was upgraded and prints the exact rollback commands.
- **A read-only dashboard and JSON API** (`/api/v1`, a server-sent events stream, optional Prometheus `/metrics`), off unless
  `WHALENURSE_HTTP_ADDR` is set. GET and HEAD only. Optional token (bearer, or the password of HTTP Basic for browsers), a Host
  check without a token, and a limit on wrong tokens. Secrets, environment variables and foreign labels never leave the process.
- **The CLI** over a local unix socket: `status`, `pause`, `resume`, `unsuspend`, plus `notify test`, `healthcheck` and `version`.
- `WHALENURSE_HTTP_TRUSTED_PROXIES`: the CIDRs of reverse proxies whose `X-Forwarded-For` the wrong-token limit may use, so the
  limit is per real client instead of per proxy.
- **Distribution.** A multi-arch image (`linux/amd64`, `linux/arm64`) on `ghcr.io/jackspiering/whalenurse` with the compose plugin
  bundled as a fallback, and static binaries with checksums. Images and checksums are signed keyless with cosign, and
  every release carries SBOMs and build provenance ([docs/verifying-releases.md](docs/verifying-releases.md)).
- Documentation: [README](README.md), [configuration](docs/configuration.md), [labels](docs/labels.md),
  [security](docs/security.md), [troubleshooting](docs/troubleshooting.md), [deployment](docs/deployment.md) (container and
  systemd), [upgrading](docs/upgrading.md); examples in [examples/](examples/).

### Fixed

- A `network_mode: service:<parent>` sidecar without an opt-in of its own: the incident the dependents step opens for it now
  closes once it has recovered. Before, it stayed open, so the next time the parent was recreated the sidecar went straight to
  SUSPENDED and was not repaired. Found in the release-gate rehearsal on a real host.
- A sidecar recreated by whalenurse is repaired again the next time its parent is recreated. Compose writes an empty
  `depends_on` label on a service recreated with `up --no-deps`, and that label was how whalenurse found the parent of a sidecar
  whose parent was gone; from the second repair on, the sidecar was skipped without a word. whalenurse now remembers the
  services a sidecar depends on (in its store).

### Requirements

- Docker Engine 25 or later for rung 1 (restart). Rungs 2 and 3 and the drift guard need Engine 28.1 or later and Docker
  Compose v5.2.0 or later; below that they are disabled with one warning and the ladder ends after the restart.

### Known limits

- Rung 3 cannot pull from a private registry (the compose child runs without `DOCKER_CONFIG`).
- One host per whalenurse; there is no multi-host hub yet.
- No write endpoints on the network: `pause`, `resume` and `unsuspend` are CLI-only.
- The dashboard has been checked in headless Chromium, not on a real phone.

## [0.1.0-rc.1] - 2026-10-20

The first release candidate of 0.1.0, published to rehearse the release pipeline (signing, SBOMs, provenance, the image push and
`scripts/release/verify.sh`), which can only run for real on a tag. It contains what 0.1.0 will contain; see the 0.1.0 section
above for the full list. A pre-release: it never moves `latest`, `v0.1` or `v0`, and it is not meant for production.

[Unreleased]: https://github.com/jackspiering/whalenurse/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/jackspiering/whalenurse/releases/tag/v0.1.3
[0.1.2]: https://github.com/jackspiering/whalenurse/releases/tag/v0.1.2
[0.1.1]: https://github.com/jackspiering/whalenurse/releases/tag/v0.1.1
[0.1.0]: https://github.com/jackspiering/whalenurse/releases/tag/v0.1.0
[0.1.0-rc.1]: https://github.com/jackspiering/whalenurse/releases/tag/v0.1.0-rc.1
