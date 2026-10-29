# Changelog

All notable changes to dokdosta are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/). The design history behind each change is in
[docs/PLAN.md](docs/PLAN.md) and the `docs/m*-notes.md` files.

The release workflow publishes the section of the tag that is pushed as the release notes. Before tagging, replace
`YYYY-MM-DD` of the version about to be released with the release date (see [docs/releasing.md](docs/releasing.md)); the
workflow refuses an undated section.

## [Unreleased]

### Added

- **`/metrics` shows notification drops, rate-limit holds and refused streams.** `dokdosta_notifications_rate_limited_total`
  counts the notifications held back by the per-URL rate limit, `dokdosta_notifications_dropped_total` those dropped
  because the queue was full, and `dokdosta_sse_refused_total` the event streams turned away with 503 at the stream
  limit. All URLs are counted together, and no URL appears in them.

### Changed

- **A sidecar's repair remembers its parent across a restart of dokdosta.** The parent identity (container ID and start
  time) the dependents step planned a repair against is now stored with the incident, so the first parent change after a
  restart opens a fresh incident at rung 0 instead of being taken for the parent being learned. An incident stored by an
  older release learns its parent at its first decision, as before.
- **The host's compose plugin is searched in the Docker CLI's order**: `/usr/local/lib`, `/usr/local/libexec`, `/usr/lib`,
  then `/usr/libexec`. A systemd install with a compose in two of these directories now uses the one `docker compose` uses,
  which may be a different version than before ([docs/upgrading.md](docs/upgrading.md#docker-engine-and-compose),
  [docs/deployment.md](docs/deployment.md)).
- **The store is schema 5.** Schema 4 adds two indexes, `events (type, id)` and `incidents (closed_at)`, so the overview's per-type event
  queries and the history list no longer read the whole table at the 100,000-row cap; the incidents list asks the store
  for only the rows it shows; schema 5 adds a `parent` column to `incidents`. The first start adds them in place; an older dokdosta refuses the database (back it up
  first, [docs/upgrading.md](docs/upgrading.md#the-store-sqlite-schema)).

### Fixed

- **A sidecar left down after its repair is now SUSPENDED, with a notification.** A sidecar without an opt-in of its own
  that dokdosta recreated or restarted with its parent, and that then exits or turns unhealthy, kept its incident open
  for ever with no message. It is now suspended once ("repaired with its parent, then failed again; it has no opt-in of
  its own"), but not while its parent is down or being healed; run `dokdosta unsuspend` once it is fixed. (B-008)
- **A container Docker restores after a Docker daemon restart is no longer taken for a hand start.** When dokdosta
  reconnected to the events stream, the `start` events the new subscription replays could be handled before the
  restore window was open, so a `restart: always` container that had been stopped by hand and was SUSPENDED was
  unsuspended and its ladder started over. The reconnect and the events behind it are now handled in the order they
  arrived, and those replayed starts and kills fall inside the restore window. (B-005; B-006 adds a regression test)
- A service built from a `build:` section in your compose file is no longer treated as not made by compose. Compose puts
  the project, service and version labels into the image it builds, and dokdosta took them for labels from the image: the
  container got a restart only, no recreate or pull, and a failed `docker compose run` task lost its one-off mark. The
  labels that only compose writes when it creates a container decide, and a `docker compose run` task keeps its one-off
  mark (plan v3.16).
- **A refusal that never clears now ends in SUSPENDED.** When a pre-action check fails the same way ten times in a row
  (for example "cannot recreate: ..."), dokdosta suspends the container with the reason "refused 10 times: ..." and sends
  the usual SUSPENDED notification, instead of refusing every few seconds for ever. Any action, or a wait for a parent,
  restarts the count. (B-004)
- **dokdosta's own start clears a manual-stop marker.** A hand `docker start` that failed left a manual-stop marker; the start dokdosta made to heal the container was attributed to its action and did not clear it, so a later crash was read as a manual stop and, with `restart: "no"`, was never healed. An attributed `start` now clears the marker (`kill` and `die` stay ignored). (B-011)
- A dependent whose parent was replaced, and whose own ladder reaches the pull rung, is now suspended once with a
  notification instead of being refused every few minutes for ever. It is still recreated with its parent, and never
  pulled.
- A recreate or a pull that took minutes no longer makes dokdosta think its own `create` and `start` events came from outside: the events are attributed to the action, and the incident stays open instead of closing as "recreated" and reopening at rung 0.
- **A relative `DOKDOSTA_COMPOSE_BIN` is refused at startup.** dokdosta now stops with "DOKDOSTA_COMPOSE_BIN must be an
  absolute path" instead of checking the path in one directory and running it from another. Set the full path, for
  example `/usr/local/bin/docker-compose`.
- **What dokdosta remembers about a removed sidecar is forgotten, whether or not it was opted in.** The parents learned for a compose service with `depends_on` that is not opted in, and its label warnings, were kept for ever once the container was removed; they are now dropped an hour after it leaves the list, like those of an opted-in one. A container that drops its `network_mode: container:` reference no longer keeps a stale namespace parent. Known gap: rows of containers removed while dokdosta was down are never loaded again and stay in the store. (B-018)
- `dokdosta pause --for` now refuses a duration longer than 100 years (use `--for 0` for no expiry). Before, a longer pause was accepted, its expiry overflowed and it ended at the next restart.

## [0.3.0] - YYYY-MM-DD

**whalenurse is renamed dokdosta** (plan v3.15). The sections below this one keep the old name, as released.

### Changed

- **Every name changed, and the old names are not read** ([docs/upgrading.md](docs/upgrading.md#from-whalenurse-020-and-earlier)):
  the image `ghcr.io/jackspiering/dokdosta`, the binary `dokdosta`, the variables `DOKDOSTA_*`, the labels `dokdosta.*`,
  the metrics `dokdosta_*`, the store `dokdosta.db`, the control socket `/run/dokdosta.sock`, the bundled compose at
  `/usr/libexec/dokdosta/docker-compose`, the systemd unit `dokdosta.service` (user, state directory, socket and
  environment file), the repository `github.com/jackspiering/dokdosta` and the Go module path. Releases up to 0.2.0
  verify with `make verify-release TAG=v0.2.0 REPO=jackspiering/whalenurse`.

### Added

- **Guards for a host upgraded from whalenurse.** dokdosta refuses to start while a `WHALENURSE_*` variable is set (the
  error names the variables, never a value); it leaves alone every container that carries a `whalenurse.*` label, so a
  `whalenurse.enable=false` is never healed under `DOKDOSTA_MODE=all`, and says why once; and it renames `whalenurse.db`
  (with its `-wal` and `-shm`) to `dokdosta.db` once, in place, refusing while a whalenurse still holds the directory or
  when both files are there.

## [0.3.0-rc.1] - 2026-10-28

The first release candidate of 0.3.0, the first release under the name dokdosta. It rehearses the release pipeline for the
new repository name and the new image `ghcr.io/jackspiering/dokdosta`, whose signatures and provenance name
`jackspiering/dokdosta`. It contains what 0.3.0 will contain; see the 0.3.0 section above for the full list, and
[docs/upgrading.md](docs/upgrading.md#from-whalenurse-020-and-earlier) before upgrading from whalenurse. A pre-release: it
never moves `latest`, `v0.3` or `v0`, and it is not meant for production.

## [0.2.0] - 2026-10-27

The second batch of the review decisions of plan v3.13 (items 1 to 17): labels that come from an image no longer steer
whalenurse, the drift guard and the pull rung check again before they act, a created container held back by a
`depends_on` condition is left alone, a reload signal or a reboot no longer costs a manual stop, an `on-failure`
container Docker will not restore stays down by policy, and the state of containers that are gone is forgotten.

### Changed

- The test compose fake now empties a sidecar's `depends_on` label on a recreate, like `compose up --no-deps`, and a test guards that the pull rung still finds the sidecar of a parent's moved tag (tests only).
- **The documentation says what the drift guard checks for a sidecar.** A service that shares another container's network,
  ipc or pid namespace, or uses `volumes_from`, gets only the image check and (with the bundled compose) the version check,
  not the compose-edit check; an edit to its own definition that was not deployed is deployed when whalenurse repairs it.
  The README, `docs/configuration.md`, `docs/troubleshooting.md` and `docs/labels.md` now say so. No behaviour changes.

### Fixed

- **A sidecar's own compose edit is now checked when it heals itself.** For a `network_mode`/`ipc`/`pid` or `volumes_from`
  container that is opted in and heals itself while what it references is current, the drift guard now asks compose's dry
  run, so an edit to its own definition that you have not deployed blocks the recreate instead of being deployed. The check
  is still not made when the sidecar is repaired after its parent was recreated, in the pull rung's image-move mode, or with
  compose v5.2.0, whose dry run said "recreate" for unchanged holders in the CI test.
- **A pull that took minutes no longer acts on a stale check.** After the pull rung's pull moved the image, the `up` ran on
  the pause and drift checks made before the pull. Now both are checked again first: a container paused meanwhile, or a
  compose file edited meanwhile, gets no `up`. The "upgraded during heal" message then says the recreate was not run (with the
  rollback, since the tag has moved), a pause follows the pause rules, and a finding is a drift block and SUSPENDED.
- **`whalenurse.heal.pull` no longer lets rung 2 replace a pinned or locally built image.** Rung 2 of a container with the
  label took a moved image tag in its recreate (and said "upgraded during heal"), even for a service whose
  `pull_policy` is `never` or `build`, where the pull rung can never apply: the label's only effect was to switch off the
  moved-tag block. The allowance now needs a readable `pull_policy` that is neither; otherwise a moved tag blocks and
  SUSPENDS as for a container without the label. whalenurse reads the policy (`docker compose config`) once more per heal
  of such a container.
- **The state of a container that is gone is forgotten.** A container removed while it was exited or SUSPENDED sends only
  `destroy`, and one removed while whalenurse was down is never listed again, so its SUSPENDED incident, manual-stop marker
  and flap history stayed in `whalenurse status` and the store for ever, and a new container under the same name inherited
  them. After each successful container list the poll and the startup sweep now forget every key without a container, and
  its open incident closes with the outcome `gone`. A list that is empty is believed only the second time, and a failed list
  forgets nothing. **Note:** forgetting wipes the key's SUSPENDED state and its flap history, so a container removed and
  created again more than `WHALENURSE_GRACE` apart starts with a clean slate (as a running one already did).
- **The bundled compose's version skew now blocks a sidecar's recreate.** A `network_mode`/`ipc`/`pid` or `volumes_from`
  container made by another compose release than the bundled one (no host plugin mounted) was recreated by the bundled
  compose with no check, by the dependents step after its parent's recreate and by its own rung 2 and pull. The guard now
  says "can't verify drift" and the container is not recreated (the block stays also with `WHALENURSE_DRIFT_GUARD=warn`).
  **Action:** mount the host's compose plugin at `/host/docker-compose`, as the examples do.
- **A container stopped by hand just after a reconnect or a reboot stays stopped.** Inside the restore window the `kill`
  set no marker, but the `die` after it was then taken for a crash, so a `docker stop` made within `STARTUP_SETTLE` of a
  reconnect, a daemon restart or a start of whalenurse was undone about `GRACE` later ("died with exit 143, no preceding
  kill"). The container is now judged without evidence: a signal exit (137/143) is notify-only.
- **An `on-failure` container that the host stopped and Docker will not restore is no longer left down in silence.** Docker's
  `on-failure` policy restarts only on a non-zero exit while retries remain, so a container that handled SIGTERM and exited 0, or
  whose `on-failure:N` retries were spent, stayed down after a daemon restart or reboot with no notification. It is now treated
  like `restart: "no"`: one "left down by policy" notification (naming its `network_mode` sidecars, which stay down with it);
  only `whalenurse.heal-exited=always` starts it.
- **A reboot no longer makes whalenurse forget a `docker stop` from long before it.** A changed boot id dropped every
  manual-stop marker, so a container you had stopped on purpose was started again after a reboot. Now, with the previous
  run's `last_seen` known, only the markers set in the 30 s before it (the shutdown's own kills) are dropped; older ones are
  kept. Without a `last_seen`, all are dropped as before.
- **A reload signal no longer counts as a manual stop.** `docker kill -s HUP` (and docker-gen's `-notify-sighup`, SIGUSR1,
  SIGUSR2, SIGWINCH) set a manual-stop marker, which then suppressed a later real heal of the container until its next
  start. These kills are recorded (`SignalIgnored`) and set no marker; every other signal still does.
- **A scaled service is healed at rung 1 only.** Every compose command names the service and acts on all of its containers,
  so healing one unhealthy replica of a service with more than one container force-recreated the healthy ones as well, and
  scaled a service started with `docker compose up --scale` back to its declared count (removing the container being healed,
  without a notification). Rungs 2 and 3 are now unavailable for such a service, and the ladder ends with SUSPENDED after
  the restart. `docker compose run` containers are not counted.
- **The integration tests' host-safety preflight (and the M0b one) now sees every spelling of an opt-in.** It listed strangers
  with an exact `whalenurse.enable=true` filter, so a container outside a test project with `1`, `T` or `TRUE` slipped past it;
  it now filters by key and reads the value with whalenurse's own parser. `CONTRIBUTING.md` says which tests the guard wraps
  and which rely on the preflight alone.
- **A very long `--for` or duration (`213504d`) is refused** instead of wrapping to a few minutes.
- **`WHALENURSE_HOST_ID` with a `/` is refused at startup.** It shifted the parts of every container key, so `pause` and
  `unsuspend` matched nothing.
- **The placeholder `WHALENURSE_HTTP_TOKEN` of the example env files is refused at startup.**
- **`whalenurse pause` says when no watched container is in the scope** (the pause still stands, as before).
- **`/static/` is a 404, not a directory listing.** The dashboard's embedded asset directory could be listed.
- **A container whose name has a `.` or `..` part gets no dashboard link** instead of a link the server would clean into
  another page. A multi-byte first letter in a reason is no longer cut in half.
- A sidecar without its own opt-in that shares its parent's `ipc` or `pid` namespace (not only `network_mode`) is now found and repaired when the parent restarts or is recreated while whalenurse runs, not only after a whalenurse restart.
- A stale sidecar whose depended-on service is down now waits with one `WaitingForParent` event instead of an `ActionRefused` every `GRACE` (which flooded the log and the event history).
- The store no longer grows an empty `parents:<key>` row for every removed container: forgetting a dependent deletes its row, other keys write none, and empty rows left by earlier versions are removed when the database is opened.
- A sidecar stopped by hand is no longer started again by the dependents step when its parent is recreated, also when the sidecar has no `whalenurse.enable` label of its own.
- A sidecar is repaired after every change of its parent, also when the parent is restarted or recreated again within `RECOVERY_PERIOD` of the previous repair; it used to be sent to `--force-recreate` and then SUSPENDED, left without a network.
- **A created container that compose is holding back is no longer started out of order.** `docker compose up` creates every
  container first and starts each when its `depends_on` conditions hold; whalenurse started a `created` container after
  `GRACE` whatever the condition. A `created` container waiting on a `service_healthy` or `service_completed_successfully`
  dependency that does not hold yet is now held, with no incident, and looked at again at each poll; `status` and the dashboard
  say what it waits for.
- Whether a sidecar without its own opt-in is repaired after its parent was recreated is decided by the service its `network_mode`/`ipc`/`pid` names, no longer by any opted-in service in its `depends_on`.
- A sidecar that shares its parent's image is no longer left stale and SUSPENDED when the parent is upgraded during heal by rung 2 (the recreate takes a tag someone else moved): it follows its parent, and the rollback in the "upgraded during heal" message names it.

### Security

- **Labels that come from an image no longer steer whalenurse.** Docker copies an image's own labels onto the containers
  made from it, so an image could opt itself in (`whalenurse.enable=true`), turn on the pull rung, start itself after a
  `docker stop`, or claim a compose identity. whalenurse now reads the image's labels by image ID: on a compose container a
  `whalenurse.*` label counts only if the compose file declares it; on any other container one with the image's own value
  is ignored; a container whose `com.docker.compose.*` labels are the image's is treated as not made by compose (restart
  rung only). One warning names the ignored labels. To opt in a container whose image ships the same value, declare the label
  in the compose file or use `WHALENURSE_MODE=all`.
- **The service name is always the operand after `--` in the compose commands whalenurse runs** (`up`, `pull` and the
  `pull_policy` read). The name is already validated before it reaches a command line; `--` makes sure no compose release
  can read it as a flag. The integration tests' compose filter accepts only that form.

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

[Unreleased]: https://github.com/jackspiering/dokdosta/compare/v0.2.0...HEAD
[0.3.0]: https://github.com/jackspiering/dokdosta/releases/tag/v0.3.0
[0.3.0-rc.1]: https://github.com/jackspiering/dokdosta/releases/tag/v0.3.0-rc.1
[0.2.0]: https://github.com/jackspiering/dokdosta/releases/tag/v0.2.0
[0.1.3]: https://github.com/jackspiering/dokdosta/releases/tag/v0.1.3
[0.1.2]: https://github.com/jackspiering/dokdosta/releases/tag/v0.1.2
[0.1.1]: https://github.com/jackspiering/dokdosta/releases/tag/v0.1.1
[0.1.0]: https://github.com/jackspiering/dokdosta/releases/tag/v0.1.0
[0.1.0-rc.1]: https://github.com/jackspiering/dokdosta/releases/tag/v0.1.0-rc.1
