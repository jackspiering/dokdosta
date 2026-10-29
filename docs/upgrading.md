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

## From whalenurse (0.2.0 and earlier)

dokdosta was called whalenurse up to and including 0.2.0 (plan v3.15). **Every name changed, and the old ones are not
read**, so go through this list once, after the backup of step 3 above. dokdosta stops you where an old name would
otherwise be ignored quietly: it refuses to start while a `WHALENURSE_*` variable is set, and it leaves every container
that still carries a `whalenurse.*` label alone (it says why once in the log, and on the dashboard).

1. **Variables.** Rename every `WHALENURSE_*` to `DOKDOSTA_*`: in an env file `sed -i 's/^WHALENURSE_/DOKDOSTA_/' <file>`,
   and by hand in the `environment:` of dokdosta's compose file, including `${WHALENURSE_...}` interpolations and the
   shell or `.env` that sets them. The names after the prefix are unchanged. The error names every variable still set
   (never its value).
2. **Labels.** Rename every `whalenurse.*` label to `dokdosta.*` in your stacks (`whalenurse.enable` becomes
   `dokdosta.enable`, and so on; values and meaning are unchanged), then `docker compose up -d` each stack: a container's
   labels change only when it is recreated. Until then dokdosta leaves it alone, so a `whalenurse.enable=false` under
   `DOKDOSTA_MODE=all` is never healed by mistake. `grep -rn 'whalenurse\.' <your stacks>` finds what is left.
3. **The image and the service.** The image is `ghcr.io/jackspiering/dokdosta` (the old `ghcr.io/jackspiering/whalenurse`
   keeps the releases up to 0.2.0 and gets no new ones). Rename the service and container to `dokdosta` if you like (the
   commands in these docs use `docker exec dokdosta /dokdosta ...`), then `docker compose up -d --remove-orphans` removes
   the old container. **Keep the volume line as it is** (for example `whalenurse-data:/data`): a new volume name would be a
   new, empty volume. At its first start dokdosta renames `whalenurse.db` (and its `-wal`/`-shm`) to `dokdosta.db` in
   place; it refuses to if a whalenurse still holds the directory, if both files are there, or if a `dokdosta.db-wal` or
   `-shm` is there without a `dokdosta.db` (a leftover that SQLite would replay into the moved database: remove
   `dokdosta.db*` or keep one store; dokdosta deletes nothing itself).
4. **Overrides.** An `entrypoint:`, `command:` or `healthcheck:` that names `/whalenurse` becomes `/dokdosta`; a reference
   to the bundled compose becomes `/usr/libexec/dokdosta/docker-compose`; the default control socket is
   `/run/dokdosta.sock`.
5. **Metrics.** The Prometheus names are `dokdosta_*` (were `whalenurse_*`): update dashboards and alerts.
6. **Systemd.** As root:

   ```sh
   systemctl disable --now whalenurse
   mv /var/lib/whalenurse /var/lib/dokdosta              # the store; dokdosta renames the file inside at its first start
   usermod -l dokdosta -d /var/lib/dokdosta whalenurse && groupmod -n dokdosta whalenurse
   install -d -m 0750 -o root -g dokdosta /etc/dokdosta
   sed 's/^WHALENURSE_/DOKDOSTA_/' /etc/whalenurse/whalenurse.env > /etc/dokdosta/dokdosta.env
   chown root:dokdosta /etc/dokdosta/dokdosta.env && chmod 0640 /etc/dokdosta/dokdosta.env
   install -m 0755 dokdosta /usr/local/bin/dokdosta
   install -m 0644 dokdosta.service /etc/systemd/system/dokdosta.service
   rm /etc/systemd/system/whalenurse.service && systemctl daemon-reload && systemctl enable --now dokdosta
   ```

   The socket is `/run/dokdosta/dokdosta.sock`. Remove `/usr/local/bin/whalenurse` and `/etc/whalenurse` once it runs.
7. **Verifying the old releases** needs the old name: see
   [Releases from before the rename](verifying-releases.md#releases-from-before-the-rename).

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
| 4 | indexes `events (type, id)` and `incidents (closed_at)`: the overview's event queries and the history list | improvement cycle, B-027 |
| 5 | column `incidents.parent`: the parent a sidecar's repair was made against, kept across a restart | improvement cycle, B-028 |

Every release up to v0.3.0-rc.1 (v0.1.x and v0.2.0, released as whalenurse) is schema 3. v0.3.0 is schema 5: its first
start migrates a schema-3 database in two steps, adding the two indexes of schema 4 and the `parent` column of schema 5 (empty for
the incidents already stored; such an incident learns its parent at its first decision). Steps 1 to 3 exist for stores written by
development builds. Each step is one transaction of a few `CREATE` or `ALTER TABLE` statements and takes no noticeable time; back
up `dokdosta.db*` first if you may go back (see *No downgrade* below). Every release that changes the schema says so in the
changelog and in this table.

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

From this release, a systemd install that keeps compose in two plugin directories uses the one the Docker CLI uses, which is
searched first: `/usr/local/lib` before `/usr/libexec`. Earlier releases searched `/usr/libexec` first. Check which one
`docker compose version` runs, and make sure the other is not a different version you did not intend. The compose dokdosta
uses is reported in its startup log.

## Rolling back

Run the previous image tag or binary again. If it refuses the database (it was migrated), [restore the backup](#backing-up-and-restoring-the-store).
The pull rung's own "upgraded during heal" notification gives the exact commands to roll *a container* back; that is a different
thing from rolling back dokdosta. While `dokdosta.heal.pull` stays on, such a container rollback lasts only until the next
heal pulls again: pin the old image in the compose file or remove the label first.
