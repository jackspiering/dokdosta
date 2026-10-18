# M2b notes

Decisions, deviations and open questions for milestone M2b (compose recreate, the drift guard, dependents) of [PLAN.md](PLAN.md). Where the plan is ambiguous this file says what was chosen and why, so the maintainer can overrule it. Rules that changed are in the plan as v3.6.

## Upstream check (17 Oct 2026)

| Item | Version | Source |
|---|---|---|
| Go | 1.27.2, still the latest stable | go.dev/dl |
| `github.com/moby/moby/client` / `api` | v0.6.2 (8 Oct) / v1.56.1 (1 Oct), unchanged. `ImageInspectWithPlatform` (API 1.49) and `ServerVersion` are in v0.6.2; `ocispec` (`image-spec` v1.1.1) is now a direct dependency | proxy.golang.org, module source |
| `modernc.org/sqlite` | v1.60.1, unchanged | proxy.golang.org |
| Docker Engine | 29.9.1 (14 Oct), the latest `docker-v29.*` release | GitHub releases |
| Docker Compose | v5.6.0 (2 Oct), still the latest release. Linux assets carry `.sha256` files; the Dockerfile pins the v5.6.0 amd64 and arm64 sums | GitHub releases |

## Decisions the plan leaves open

1. **Fixed paths.** Host plugin mounted read-only at `/host/docker-compose`; bundled copy at `/usr/libexec/whalenurse/docker-compose`. They are different paths on purpose: "bundled" (and with it the version-skew check) comes from the path that matched, never from comparing versions. Under systemd, the host's plugin directories (`/usr/libexec/docker/cli-plugins/…` and three more) are searched last. An explicit `WHALENURSE_COMPOSE_BIN` that does not work is an error, never a reason to use another binary.
2. **The image comparator goes through the API, not a shell-out.** The image has no `docker` CLI. `docker.Reader.ImageID` is `ImageInspect` with `ImageInspectWithPlatform(linux/<arch>)` and returns `.ID`: the call behind `docker image inspect --platform linux/<arch> --format '{{.Id}}'`. The arch is the Engine's (`ServerVersion().Arch`). The integration tests pin it on both stores (see *CI*).
2a. **Found by the CI matrix: the compose image label differs by release (needs approval, plan v3.6 item 3).** On the containerd store, compose v5.2.0 writes the image (index) ID into `com.docker.compose.image`, where v5.6.0 writes the platform manifest digest (M0, M0b). The 5.2.0 × containerd leg blocked every recreate as "the tag moved" with `busybox:1.37 now resolves to 66a6306db78b, but this container was created from bdf57e528e45`; the classic store and 5.6.0/latest were unaffected, and the oracle, `config --hash`, the dependents recreate and the containerized recreate all behaved on 5.2.0. The guard now accepts a label equal to either the platform ID or the plain ID; a re-tag changes both. It is its own commit ("accept the index ID in the compose image label") if you would rather revert it. The cutover release was not pinned (somewhere in v5.3.0 to v5.5.0; plan v3.3 says v5.5.0 changed digest reconciliation).
3. **The oracle's output is read from stdout and stderr together.** Compose prints progress on stderr; M0 captured both merged. "Parsed, but no line about this container" and "no JSON at all" (an old compose prints `null`) are both `ErrUnparseable`, which means *fall back to `config --hash`*, never *no drift*. Lines are matched on the container's name (`Container <name>`, also the temporary `<id>_<name>` compose renames to).
4. **A failed oracle is a fallback, not a block.** If the dry-run command itself fails (not just unparseable), the guard reports it and tries `config --hash`; if that fails too, the finding is `unverified` and blocks like drift.
5. **Where the explicit image comparison runs.** Always for reference holders; after the hash fallback (the hash covers config only); and, to say *why*, when the oracle found drift (its failure is then ignored: the oracle's finding stands). A reference holder without a `com.docker.compose.image` label cannot be verified, which is a block in strict mode.
6. **Dependents are incidents of their own, not an ad hoc step.** A dependent whose parent ID is gone (`ClassStaleParentGone`, also a failed start with `No such container`) gets `ActionRecreateStale`; one whose parent restarted later gets `ActionRestart` (class `ClassStaleParentRestarted`). They go through the engine like anything else, so the flap limit, rate limit, cooldown, SUSPENDED and persistence apply. The cost: a flaky parent that restarts four times a day can SUSPEND its sidecar for flapping. After whalenurse's own action on a key, its dependents are scheduled for when the key is looked at again, and a parent that is back in a good state with an incident still open triggers the same sweep. The parent's own `start`/`create` events fall inside its in-flight window and are not interpreted (plan §3.2), which is why that explicit trigger is needed.
7. **Checks 2 and 3 of the guard run on dependents before the dependents `up`** (check 1 is skipped for them, plan §3.6). A non-forced `up` deploys on-disk changes if compose decides to recreate, so a re-tagged image or a bundled-compose skew blocks it. The on-disk compose file is *not* checked for a dependent (compose hashes it after resolving the reference; E4 and E5): the parent's own check, and the maintainer's habit of editing a file and running `up`, are what cover it.
8. **A stale dependent waits for the services it depends on** (from the `com.docker.compose.depends_on` label, which includes `network_mode: service:X`): all of them that have a container in the project must run and not be busy. Its old parent ID is gone, so the rules cannot see the new parent; the label is how the daemon finds it. No label means no wait; the post-check and the next look decide.
9. **One at a time, with a health wait, as scheduled steps.** `depGate` records, per parent reference, the dependent that was just restarted or recreated; its siblings are rescheduled every 2 s until it runs (and is healthy if it has a healthcheck) or 90 s have passed. No sleep, so the loop, the heartbeat and `/healthz` keep running and the unit tests run on the fake clock.
10. **Rate-limit slots.** Every `Begin` spends one slot of `WHALENURSE_RATE_LIMIT`: restart, recreate, and each dependent's restart or recreate. A blocked recreate (no `Begin`) spends none. With the default of 10 an hour, a parent recreate with two sidecars costs 3.
11. **Dependents must be opted in themselves.** A sidecar without `whalenurse.enable` is not touched, although it breaks when its parent restarts. This follows plan rule 0 ("not opted in" is skipped). *Open question 1.*
12. **Without compose, a recreate-shaped decision is advice.** If the gate is off, the container is not a compose container, its compose inputs are unreadable, or `whalenurse.actions` lacks `recreate`, `ActionRecreate`/`ActionRecreateStale` become `ActionNeedsRecreate`: one `NeedsRecreate` event, no incident, exactly what M2a did. The ladder of an ordinary unhealthy container instead records `RungUnavailable` for rung 2 and ends at SUSPENDED after rung 1, as the plan says.
13. **A network that is gone** (`network … not found`) is `ActionRecreate`: rung 2 straight away, restart skipped, drift guard first. Not reproduced (as in M0).
14. **Dry-run runs the guard.** It only reads (compose's own `--dry-run`, `config --hash`, an image inspect), so a dry run tells you "this recreate would be blocked: …". The mutating commands go to no-op actors (`docker.DryRun`, `compose.DryRun`).
15. **The compose child environment** drops `COMPOSE_*`, `DOCKER_CONTEXT`, `DOCKER_CONFIG` and `WHALENURSE_*` and sets `NO_COLOR=1`; the working directory is `/`. Variables that compose interpolates from the shell in the original `up` and that are not in an env file will resolve differently, which shows as config drift and blocks (the safe direction).
16. **`compose` exposes typed methods only** (`Recreate`, `Up`, `DryRunUp`, `ConfigHash`): `down` cannot be expressed. The integration guard re-checks every command line (`CheckComposeCommand`), so the tests fail closed even if the package grew a bug.
17. **Staleness through `ipc`/`pid`** is found by inspect (the poll inspects every opted-in container), but the cheap sweep after a parent's `start` uses the container list, which carries only `NetworkMode`. So `ipc`/`pid` dependents are found by the next poll, not at once.

## What M2b built

- `internal/compose`: version parsing and the gate, `Project` from labels with a readability check that names the identical-path mount, the `Runner`, binary lookup, `Reader`/`Actor`/`DryRun`, the oracle and `config --hash` parsers, the drift `Guard`, a `Fake`.
- `internal/docker`: `ServerVersion`, `ImageID`, `Container.Image` and `VolumesFrom`.
- `internal/rules`: `ActionRecreate`, `ActionRecreateStale`, `ActionNeedsRecreate` (advice only), the stale-parent-restarted decision as an `ActionRestart` that waits for a busy parent.
- `internal/engine`: rung 2 in the ladder, `Obs.Unavailable`, `Blocked`, `Warned`, `Note`, `ReasonDrift`, new events (`DriftBlocked`, `DriftWarned`, `RungsDisabled`, `DependentPostCheck`).
- `internal/daemon`: the startup gate (re-checked after every stream reconnect), `executeRecreate`, the guard wiring, the stale-parent check, the dependents sweep and the sibling gate, the single notification for a parent left down, `status` fields.
- Image: the compose plugin bundled with a checksum; `examples/compose.yaml`, `README.md` and `docs/running.md` warn about the two mounts.
- CI: `scripts/ci/setup-docker.sh <store> [compose]` installs the release asked for with its checksum; the integration job is the 2 × 3 matrix.
- `test/m0/m0b/whalenurse-recreate/`: the throwaway-VM scenario.

## Tests

- Unit (no Docker, no process): `compose` (versions, gate table, labels, readability, every command line incl. "never down", env, the oracle across E5's outputs, lookup order, the guard's findings table), `rules` (new decisions), `engine` (rung 2 in the ladder, blocked, warned, stale skipping the restart rung, the advisory carrying dependents), `daemon` (`m2b_test.go`: ladder restart → recreate → SUSPENDED; recreate that helps; the guard blocks on a compose edit, a moved tag, the hash fallback, a missing image, version skew; warn mode; dry-run; the version gate on eight version combinations incl. one warning; unreadable inputs; non-compose containers; `volumes_from` holders; dependents recreated after a parent recreate, force-recreated by the post-check, after an external recreate, waiting for a parent that is down or busy, restarted one at a time, a moved tag blocking, the gate off; one notification for a parent left down). Mutating the sibling gate or the guard makes the matching tests fail (checked by hand).
- Integration (CI only, behind the guard, whalenurse-test-* projects): see `test/integration/compose_test.go`.

## Not verified

- Anything that runs compose. The integration tests were not run locally (the plan forbids it); CI is their only run.
- The throwaway-VM scenario (`whalenurse-recreate`): written and self-tested (stubs only), not run.
- A host with several compose projects sharing one image tag, a compose file using `extends`/`include` from outside the stacks root, and the hash fallback on a compose that prints `null` instead of JSON are covered by unit tests of the parsers only.

## Open questions for the maintainer

1. **Should a sidecar that is not opted in follow its parent?** Today it does not (decision 11).
2. **Flapping of dependents** (decision 6): a dependent's incident counts toward its own flap limit.
3. **Should `warn` also let through a guard that could not verify** (unverified findings), or only real drift? Today it lets through both.

## [maintainer]

- Run the `whalenurse-recreate` scenario on the throwaway VM (below, in the milestone report).
