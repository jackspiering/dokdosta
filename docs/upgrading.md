# Upgrading

## Before every upgrade

1. Read the section of [CHANGELOG.md](../CHANGELOG.md) for each version between yours and the new one. Anything that needs
   action is under **Changed** or **Removed**, and called out.
2. [Verify](verifying-releases.md) the release you are about to run.
3. **Back up the store**, with dokdosta stopped ([below](#backing-up-and-restoring-the-store)). The store migrates forward in
   place when the new version starts, and an older version refuses a database that a newer one has migrated (below).
4. Upgrade at a quiet moment: while dokdosta restarts, nothing is healed (a short gap, and the startup sweep catches up after
   `DOKDOSTA_STARTUP_SETTLE`).

Container: `docker compose pull dokdosta && docker compose up -d dokdosta` (with the new tag in the file). Systemd: replace
`/usr/local/bin/dokdosta`, `systemctl restart dokdosta`. dokdosta does not heal itself and is not touched by its own
recreate rung. If it is healing something when it is stopped, it lets that action finish first (up to 30 s; an action cut
short is not counted and runs again after the restart), so give it the `stop_grace_period: 60s` of the examples, or the
unit's `KillMode=mixed` and `TimeoutStopSec=60s`; with Docker's default of 10 s a recreate in flight can be killed halfway.

**The host ID.** A container install without `DOKDOSTA_HOST_ID` names the host by its container ID, which the upgrade
changes; versions after 0.1.2 move the stored state to the new ID at startup (an upgrade from 0.1.2 too), so nothing is lost. Set
`DOKDOSTA_HOST_ID` (as the examples now do) for a name that stays the same in keys and notifications.

After replacing the systemd unit, run `systemctl daemon-reload`, and `systemctl reenable dokdosta` when its `[Install]`
section changed (it did after 0.1.2: `WantedBy=` gained `docker.service`, so dokdosta starts again with Docker).

## Backing up and restoring the store

The store is `dokdosta.db` in `/data` (the container's volume) or in `DOKDOSTA_DATA_DIR` (`/var/lib/dokdosta` with the
example systemd unit). It is a SQLite database in WAL mode that dokdosta writes every few seconds, so **stop dokdosta
before you copy it**: a clean stop closes the database and leaves consistent files. A copy taken while it runs (`docker cp`
included) may not open, or may miss the latest state, and you only find out when you need it. Keep `dokdosta.db` and any
`-wal`/`-shm` file next to it together.

Container (the image has no shell, so a throwaway container reads the volume; run this in the directory of dokdosta's compose
file):

```sh
docker compose stop dokdosta
vol=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$(docker compose ps -aq dokdosta)")
docker run --rm -v "$vol":/data:ro -v "$PWD":/backup alpine tar -C /data -cf /backup/dokdosta-data.tar .
```

(`vol` is empty for a bind mount: stop dokdosta and copy the host directory with `cp -a` instead.) Then pull and
`docker compose up -d dokdosta` as above.

Systemd:

```sh
systemctl stop dokdosta
rm -rf /var/lib/dokdosta.bak && cp -a /var/lib/dokdosta /var/lib/dokdosta.bak   # replaces an older backup
```

**Restore**, also with dokdosta stopped: first remove every `dokdosta.db*` file in the data directory (a `-wal` left by
the newer version would otherwise be replayed into the restored database), then copy the backup back, owned by the user
dokdosta runs as (uid 65532 in the image, the `dokdosta` user under systemd), then start it.

```sh
# Container
docker compose stop dokdosta
vol=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$(docker compose ps -aq dokdosta)")
docker run --rm -v "$vol":/data -v "$PWD":/backup:ro alpine \
  sh -c 'rm -f /data/dokdosta.db* && tar -C /data -xf /backup/dokdosta-data.tar && chown -R 65532:65532 /data'
docker compose up -d dokdosta

# Systemd
systemctl stop dokdosta
rm -f /var/lib/dokdosta/dokdosta.db*
cp -a /var/lib/dokdosta.bak/. /var/lib/dokdosta/
systemctl start dokdosta
```

If a restored database still will not open, delete `dokdosta.db*` instead: dokdosta starts with empty history (see
[No downgrade](#the-store-sqlite-schema)).

## The store (SQLite schema)

The schema version is kept in the database (`PRAGMA user_version`). On start dokdosta migrates an older database **forward**,
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
  `database schema N is newer than this dokdosta understands (M)`. To go back, [restore the backup](#backing-up-and-restoring-the-store) you took before
  upgrading (or delete `dokdosta.db*`: dokdosta then starts with empty history, which costs the flap history, pauses and SUSPENDED
  state, and the containers are looked at afresh).
- **A dry run stores nothing** and never touches the database.
- Retention (`DOKDOSTA_HISTORY_RETENTION`, 30 days) prunes closed incidents and events hourly; events are capped at 100,000.

## What stays stable

- **`/api/v1`** only gains fields; nothing is renamed or removed within v1. Event IDs are monotonic per host and survive restarts.
  The golden files in `internal/api/testdata/golden/` are the contract.
- **Behaviour you may rely on:** a heal never pulls or builds (only the opt-in pull rung pulls), never runs `compose down`, and
  never acts on itself.
- Version numbers follow [Semantic Versioning](https://semver.org/). Before v1.0, anything else (a variable, a label, a CLI
  argument, the dashboard) may change in a minor release, and every such change is listed under **Changed** or **Removed** in the
  [changelog](../CHANGELOG.md).

## Docker Engine and Compose

dokdosta checks them at startup and says so. Upgrading Docker Engine or Compose on the host is independent of upgrading
dokdosta, with one effect to know: after you upgrade the host's compose plugin, containers made by the older release may be
reported as drifted when a recreate is considered ("compose upgraded from X to Y since this container was created, so this may
be a changed hash layout rather than an edit"). Recreate them once yourself (`docker compose up -d`) and the message goes
away. See [troubleshooting.md](troubleshooting.md#drift-block-messages).

## Rolling back

Run the previous image tag or binary again. If it refuses the database (it was migrated), [restore the backup](#backing-up-and-restoring-the-store).
The pull rung's own "upgraded during heal" notification gives the exact commands to roll *a container* back; that is a different
thing from rolling back dokdosta. While `dokdosta.heal.pull` stays on, such a container rollback lasts only until the next
heal pulls again: pin the old image in the compose file or remove the label first.
