# Labels

dokdosta reads these labels from the containers it watches. In a compose file they go under the service:

```yaml
services:
  web:
    labels:
      dokdosta.enable: "true"
```

A boolean label accepts `1`, `t`, `T`, `TRUE`, `true`, `True` and `0`, `f`, `F`, `FALSE`, `false`, `False` (Go's
`strconv.ParseBool`), with surrounding spaces ignored. An invalid value falls back to the default and logs one warning for that container; it never opts a container in or out by
accident.

| Label | Values | Default | What it does |
|---|---|---|---|
| `dokdosta.enable` | `true`, `false` | — | `true` opts the container in (with `DOKDOSTA_MODE=label`, the default, nothing is watched without it). `false` leaves the container alone entirely, **also as the sidecar of an opted-in parent** (see below). |
| `dokdosta.actions` | `restart`, `recreate`, or `restart,recreate` | `restart,recreate` | The rungs of the ladder, in order. `restart` alone never recreates the container: after a failed restart it is SUSPENDED. `pull` is not accepted here; use `dokdosta.heal.pull`. |
| `dokdosta.heal.pull` | `true`, `false` | `false` | Appends rung 3: after restart and recreate failed, pull a newer image and recreate from it. **Needs `recreate` in `dokdosta.actions`** (else one warning and the pull stays off). Never runs for a service with `pull_policy: never` or `build`. A private registry needs `DOKDOSTA_REGISTRY_AUTH_DIR` ([security.md](security.md#private-registries)). It is the only thing in dokdosta that fetches an image. While it stays on, a pasted rollback lasts only until the next heal reaches the pull again: to keep an old image, pin it in the compose file or remove this label ([running.md](running.md)). |
| `dokdosta.heal-exited` | `crashed`, `always`, `never` | `crashed` | What to do with a container that has *exited*. `crashed`: heal one that crashed (a non-zero exit other than 137/143, an OOM kill, a `die` nobody asked for); leave an intended exit (`restart: "no"`/`on-failure` with exit 0) and a stop by hand alone. `always`: heal any exited container, including one a host shutdown stopped and Docker's restart policy did not restore. `never`: notify only. |
| `dokdosta.cooldown` | a positive duration | `DOKDOSTA_COOLDOWN` | Overrides the base cooldown between rungs for this container (it still doubles per rung, capped at 5 minutes; a value above 5 minutes acts as 5 minutes). |
| `dokdosta.flap-limit` | a positive integer | `DOKDOSTA_FLAP_LIMIT` | Overrides the flap limit for this container. |

## Sidecars

A container with `network_mode: service:<parent>` (or `container:<parent>`) breaks whenever its parent restarts or is
recreated. For an **opted-in** parent, dokdosta repairs such sidecars after it has healed the parent, even if they carry
no `dokdosta.enable` label, unless they are labelled `dokdosta.enable=false`. Only that repair needs no opt-in: a sidecar
that is unhealthy or exited *on its own* is healed only if it is opted in itself. The parent that counts is the container the sidecar's `network_mode`, `ipc` or `pid` names; another opted-in service in its `depends_on` does not make it a dependent. A sidecar you stop by hand (`docker stop`, `docker kill`) stays stopped when its parent is recreated, as an opted-in container would; one that exited on its own is recreated together with its parent. See the README for what "repair" does.

**What the drift guard checks for a sidecar.** Compose resolves a `network_mode`, `ipc`, `pid` or `volumes_from` reference to a
container ID before it hashes the service, so asking compose whether the service's own definition changed works only while
that reference is still the one the sidecar was created with: while everything it references is the current, running
container of its service. `config --hash` hashes the unresolved form and is never used for a sidecar.

- **A sidecar that heals itself** (its own ladder, when it is opted in) with current references: the dry run is asked, and an
  edit to its own definition that you have not deployed blocks the recreate like any other drift (`DriftBlocked`). Not with
  compose v5.2.0, whose dry run is not reliable for such a service: there the check is not made.
- **A sidecar repaired after its parent was recreated** (the dependents step), or one whose references are not current: its
  references are gone or changed, compose's answer would always be "recreate", so only the image (the tag still resolves to
  the image it was created from) and, when the bundled compose is in use, the compose release that made it are checked.
  **An edit to the sidecar's own definition** (its command, environment, volumes, an `image:` that names another local image)
  **that you have not deployed is deployed when dokdosta repairs it**, for example after you recreate only its parent by
  hand. Deploy such edits yourself before they matter.
- When a recreate may take a moved image tag on purpose (a container with `dokdosta.heal.pull=true`, or a sidecar taking
  the tag its parent's pull just moved), the guard raises no image and no config finding for a sidecar; the version check
  still applies with the bundled compose.

## Labels from the image

Docker puts an image's own labels (its `LABEL` lines) on every container made from it, for each key the container is not
given itself, so an image's publisher is a second source of labels. dokdosta therefore reads the image's own labels (by the
container's image ID, so a moved tag changes nothing) and does not let the ones that come from the image steer it:

- **`dokdosta.*` on a compose container** counts only if the service's `labels:` in your compose file declares it. dokdosta
  reads that with `docker compose config` and remembers the answer until the service is recreated from an edited file. If it
  cannot read the compose file (the stacks are not mounted, compose is below the minimum version, the read failed), the rule
  for any other container applies.
- **`dokdosta.*` on any other container** is ignored when its value is the same as the image's own label for that key,
  whatever the value is. Set a different value, or use `DOKDOSTA_MODE=all`. (A value you give a plain `docker run` container
  that happens to equal the image's cannot be told from the image's.)
- **`com.docker.compose.*`**: if any of these labels has the value the image gives it, the identity is the image's. The
  container is then treated as one that compose did not make (see below): its state key is its container name, it gets the
  restart rung only, no compose command is built from its labels, and it is neither a parent nor a sidecar by service. An
  image committed from a compose container carries these labels too, and the container made from it is treated the same way.

Each time, one warning in the log names the labels that were ignored. To see what an image brings:

```sh
docker image inspect -f '{{json .Config.Labels}}' <image>
```

To opt such a container in, declare the label under the service in the compose file (for example `dokdosta.enable: "true"`),
even if the image ships the same value. To keep an image's label from applying at all, set your own value for the key.

## Labels dokdosta reads from compose

The recreate rung uses the labels compose put on the container, not your current compose file: `com.docker.compose.project`,
`.project.working_dir`, `.project.config_files`, `.project.environment_file`, `.service`, `.config-hash`, `.image` and
`.version`. A container without them, one compose did not make, only gets the restart rung, and so does one whose compose
labels come from its image ([Labels from the image](#labels-from-the-image)). A container can carry them without compose
(set by hand with `--label`), and then it is treated as a compose container. A project or service label that is not a name
compose would write is refused, and that container gets the restart rung only.

Also read: `com.docker.compose.container-number` (part of the state key `host/project/service/number`, which survives a
recreate), `com.docker.compose.oneoff` (`True` for a `docker compose run` container, which is never touched),
`com.docker.compose.depends_on` (a sidecar's parent services; compose empties it when it recreates a container with
`up --no-deps`, so dokdosta remembers the parents it has seen instead of relying on it) and `com.docker.swarm.task.id` (a swarm
task, never touched).

## Per-container state, not labels

Pausing and unsuspending are done with the CLI (`docker exec dokdosta /dokdosta pause <project>[/<service>] --for 2h`,
`unsuspend`), not with labels; see the README.
