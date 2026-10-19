# Upgrading

## Before every upgrade

1. Read the section of [CHANGELOG.md](../CHANGELOG.md) for each version between yours and the new one. Anything that needs
   action is under **Changed** or **Removed**, and called out.
2. [Verify](verifying-releases.md) the release you are about to run.
3. **Back up `/data`** (the file `whalenurse.db` and its `-wal`/`-shm` neighbours). The store migrates forward in place when the
   new version starts, and an older version refuses a database that a newer one has migrated (below).
4. Upgrade at a quiet moment: while whalenurse restarts, nothing is healed (a short gap, and the startup sweep catches up after
   `WHALENURSE_STARTUP_SETTLE`).

Container: `docker compose pull whalenurse && docker compose up -d whalenurse` (with the new tag in the file). Systemd: replace
`/usr/local/bin/whalenurse`, `systemctl restart whalenurse`. whalenurse does not heal itself and is not touched by its own
recreate rung.

## The store (SQLite schema)

The schema version is kept in the database (`PRAGMA user_version`). On start whalenurse migrates an older database **forward**,
in place, one step per version:

| Schema | Added | By |
|---|---|---|
| 1 | per-key state, incidents, flap history, pauses, the action budget, the heartbeat | first stored state (development milestone M2a) |
| 2 | `notified`: the notification dedupe, so a restart does not repeat a message | M3 |
| 3 | `events`: the persisted event log, the API's `since` cursor and the timelines | M5 |

v0.1.0 is schema 3, so a v0.1.0 database needs no migration; the steps exist for stores written by development builds. Each step
is a few `CREATE TABLE` statements and takes no time. Every release that changes the schema says so in the changelog and in this
table.

- **No downgrade.** A database with a newer schema than the binary knows is refused at start with
  `database schema N is newer than this whalenurse understands (M)`. To go back, restore the backup you took before upgrading
  (or delete `whalenurse.db*`: whalenurse then starts with empty history, which costs the flap history, pauses and SUSPENDED
  state, and the containers are looked at afresh).
- **A dry run stores nothing** and never touches the database.
- Retention (`WHALENURSE_HISTORY_RETENTION`, 30 days) prunes closed incidents and events hourly; events are capped at 100,000.

## What stays stable

- **`/api/v1`** only gains fields; nothing is renamed or removed within v1. Event IDs are monotonic per host and survive restarts.
  The golden files in `internal/api/testdata/golden/` are the contract.
- **Behaviour you may rely on:** a heal never pulls or builds (only the opt-in pull rung pulls), never runs `compose down`, and
  never acts on itself.
- Version numbers follow [Semantic Versioning](https://semver.org/). Before v1.0, anything else (a variable, a label, a CLI
  argument, the dashboard) may change in a minor release, and every such change is listed under **Changed** or **Removed** in the
  [changelog](../CHANGELOG.md).

## Docker Engine and Compose

whalenurse checks them at startup and says so. Upgrading Docker Engine or Compose on the host is independent of upgrading
whalenurse, with one effect to know: after you upgrade the host's compose plugin, containers made by the older release may be
reported as drifted when a recreate is considered ("compose upgraded from X to Y since this container was created, so this may
be a changed hash layout rather than an edit"). Recreate them once yourself (`docker compose up -d`) and the message goes
away. See [troubleshooting.md](troubleshooting.md#drift-block-messages).

## Rolling back

Run the previous image tag or binary again. If it refuses the database (it was migrated), restore the backup as above.
The pull rung's own "upgraded during heal" notification gives the exact commands to roll *a container* back; that is a different
thing from rolling back whalenurse.
