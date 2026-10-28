# dokdosta — implementation plan v3.15

Status: **v3.15, written on 28 Oct 2026, was approved by the maintainer on 28 Oct 2026** (the instruction to carry out the rename). It is v3.14 plus the rename of the product from whalenurse to dokdosta (items 1 to 9 below). **v3.14, written on 28 Oct 2026, was approved by the maintainer on 28 Oct 2026.** It is v3.13 plus two changes to how CI spends its runners (items 1 and 2 below); the code and its tests are unchanged. **v3.13, written on 22 Oct 2026, was approved by the maintainer on 22 Oct 2026.** It is v3.12 plus the maintainer's decisions of 22 Oct 2026 on the code review of 21 Oct 2026 (items 1 to 23 below); each item names the review finding (WN-NNN) or design question (DQn) it settles. The fixes land as stacked pull requests, not as a milestone. **v3.12, written on 20 Oct 2026 to start milestone M7 (private registries, R5), was approved by the maintainer on 21 Oct 2026; M7 is built ([m7-notes.md](m7-notes.md)).** It is v3.11 plus the compose child's Docker config fix (item 1) and the M7 design below; items 3, 4 and 7 hold the maintainer's answers of 20 Oct 2026. **v3.11, written on 20 Oct 2026 after the release-gate rehearsal and approved by the maintainer on 20 Oct 2026** (it is v3.10 plus the two fixes below: [m6-notes.md](m6-notes.md), "Found by the release-gate rehearsal"). **v3.10, written by the M6 build on 20 Oct 2026, was approved by the maintainer on 20 Oct 2026** with its four publish permissions, the trusted-proxy option as built, the deferral of private registries (R5, the first post-v1 item) and the `v0.1.0-rc.1` rehearsal before `v0.1.0` (it is v3.9 plus the release pipeline, the documentation set and the trusted-proxy option below, with the post-v1 list; [m6-notes.md](m6-notes.md)). **v3.9 was approved by the maintainer on 19 Oct 2026** (the M5 review: it is v3.8 plus the M5 decisions below, [m5-notes.md](m5-notes.md), with the maintainer's six answers in items 11 to 16 and the screenshot review's fixes in items 17 and 18). v3.8 was approved by the maintainer on 19 Oct 2026 (the M4 review: it is v3.7 plus the M4 findings and decisions below, [m4-notes.md](m4-notes.md), with the maintainer's answers in items 11 to 15). v3.7 was approved by the maintainer on 19 Oct 2026 (the M3 review: it is v3.6 plus the M3 decisions below, [m3-notes.md](m3-notes.md), with the maintainer's four answers, items 13 to 15). v3.6 was approved by the maintainer on 18 Oct 2026 (the M2b review, including item 3 and the decisions of items 9 to 12): v3.5 plus the M2b findings ([m2b-notes.md](m2b-notes.md)); v3.5 was v3.4 plus three M2a verdicts below (the `last_seen` heartbeat, the stricter crash-loop rule, and the shutdown-window rule for containers stopped by the host), which the maintainer approved on 17 Oct 2026. v3.4 was v3.3 plus a split of milestone M2 into M2a and M2b. v3.3 was v3.2 plus the M0b verdicts ([m0b-findings.md](m0b-findings.md) §4), which the maintainer approved on 17 Oct 2026. v3.2 was v3.1 plus the two M1 verdicts ([m1-notes.md](m1-notes.md) decisions 2 and 11), and v3.1 was v3 plus the M0 verdicts ([m0-findings.md](m0-findings.md) §4). Supersedes v3.10 (which was never approved; v3.11 contains it). Facts and versions are as of 20 Oct 2026 (16 Oct for the prior-art survey; the release tooling versions are in [m6-notes.md](m6-notes.md)).
Repo: `github.com/jackspiering/dokdosta` (Go module path, GHCR image and binary are all lowercase).

**Changes in v3.15** (the maintainer's decisions of 26 Oct 2026, carried out from 28 Oct 2026 after `v0.2.0`). **whalenurse is renamed dokdosta**: two Serbo-Croatian words, *dok* ("until") and *dosta* ("enough"). The GitHub repository is `jackspiering/dokdosta` (renamed by the maintainer; GitHub redirects the old URLs). The rename lands as a stack of three pull requests (this plan, the mechanical rename, the guards of items 4 to 6); the last two merge together, so `main` never has the new names without the guards. The v3.13 items not built yet are built afterwards, under the new names.

1. **Every name changes, and the old names are not aliases.** Lowercase `dokdosta` everywhere (Go module `github.com/jackspiering/dokdosta`, binary, `cmd/dokdosta`, image `ghcr.io/jackspiering/dokdosta`, labels `dokdosta.*`, metrics `dokdosta_*`, the store `dokdosta.db` and `dokdosta.lock`, the control socket `/run/dokdosta.sock`, the bundled compose at `/usr/libexec/dokdosta/docker-compose`, the systemd unit, user and directories, the User-Agent); `DOKDOSTA_*` for the variables. The safety rule's project names become `dokdosta-test-*` and `dokdosta-m0-*`, and the guards of the integration tests and the M0 scripts refuse the old `whalenurse-*` projects like any other. The `dokdosta.*` labels are the `whalenurse.*` ones with the new prefix; their meaning, the rule precedence and every other behaviour are unchanged.
2. **The next release is a new minor version (`v0.3.0`)** and goes through the release gate. Tagging stays the maintainer's alone. The old image `ghcr.io/jackspiering/whalenurse` and its tags are left as they are; published tags are never moved or deleted. The releases before the rename keep their signatures: their certificate identity names `jackspiering/whalenurse`, so `make verify-release` takes `REPO=jackspiering/whalenurse` for them.
3. **History keeps the old name.** The scrubbed fixtures in `testdata/` (their `whalenurse-m0-*` projects are what was captured), the milestone notes and findings (`docs/m*-notes.md`, `docs/m0-findings.md`, `docs/m0b-findings.md`), the released sections of `CHANGELOG.md`, the *Changes in* sections of this plan up to v3.14 and its appendices stay as written, with a one-line rename note; only their relative link targets follow renamed files. The tests that build keys from those fixtures keep the old project names. The finding IDs `WN-NNN` and the `wn` abbreviations in the code stay. The whale stays (item 9).
4. §4 (guard 1), **dokdosta refuses to start while a `WHALENURSE_*` variable is set**, even an empty one, in every subcommand but `version`. The error lists the variable names, sorted, and never a value (the token and the URLs are secrets), and points at `docs/upgrading.md`. Otherwise a lost `HTTP_TOKEN`, `NOTIFY_URLS` or `MODE` would go unnoticed. The compose child's environment drops both prefixes.
5. §3.3 (guard 2), **a container that carries a `whalenurse.*` label is skipped**, whatever else it carries (fail closed), as a skip rule right after the own-container rule. Otherwise `whalenurse.enable=false` would be ignored under `MODE=all` and dokdosta would heal a container the user excluded. The labels are read as v3.13 item 10 reads labels (a value the image supplies does not count). The decision reason names the label keys, never a value, and says to rename them; it is logged once per key and run, and shown on the dashboard as every skip reason is. The dependents step does not repair such a sidecar either. No new event type.
6. §3.10 (guard 3), **the store moves once.** In a live run, before the store opens: when `whalenurse.db` exists in the data directory and `dokdosta.db` does not, dokdosta takes the old `whalenurse.lock` (refusing if an old process holds it) and renames `whalenurse.db` and its `-wal` and `-shm` in place (never a copy), then removes the old lock. Both files present is a startup error. A dry run moves nothing. Under systemd the state directory moves too (`/var/lib/dokdosta`), which `docs/upgrading.md` covers.
7. **`docs/upgrading.md` gets a "from whalenurse" checklist**: the variables, the labels (a label changes only on a recreate; until then the container is skipped and says why), the image and the container name, the healthcheck or entrypoint overrides, the metric names, systemd (state directory, user, socket, environment file), and verifying the old releases.
8. **CI check names do not change** (the ruleset requires them). The six required `integration (...)` checks say "real Docker, whalenurse-test-* projects only"; their job keeps that name, with a comment, until the maintainer changes the ruleset and the job name together (the projects are `dokdosta-test-*`).
9. **Branding** (the maintainer's decision of 28 Oct 2026): no image shows the old name. The wordmark (`docs/assets/logo.svg`, `logo.png`) is redone as the whale beside "dokdosta" in two colours; the dashboard screenshots (`docs/screenshots/`, all of them showing whalenurse) are removed, and the README shows none until they are retaken. The whale in a nurse cap, the icons and the social preview carry no name and stay.

**Changes in v3.14** (the maintainer's decisions of 27 Oct 2026, built on 28 Oct 2026 after `v0.2.0`). CI waited up to an hour for runners: the account runs 20 jobs at once, a full run is 34 jobs and about 120 runner-minutes (93 % of it the 18 integration shards), so about 10 full runs fit in an hour. Two patterns filled that: every layer of a stack of pull requests ran the full matrix, again after each merge of `main` into the stack, and a burst of merges to `main` queued one full run per merge, none of which could be cancelled.

1. §6 *CI matrix*, **pushes to `main` share one concurrency group** (`ci-main`, `cancel-in-progress: false`). The run in progress finishes; of the pushes waiting behind it only the newest runs (GitHub cancels a pending run when a newer one joins the group). Every push to `main` is still tested in full, but a burst of merges costs two full runs instead of one per merge, and the head commit of `main` always gets a finished run, which is the run a release needs.
2. §6 *CI matrix*, **a pull request into a branch other than `main` (a layer of a stack) runs the light set**: build, vet and unit tests, the scripts, the workflows, the examples. The image, demo, GoReleaser snapshot, image-drift and integration jobs do not run. The ruleset applies to `main` only, so no check is required there, and the stack's pull request into `main` runs everything. The integration and image-drift matrices are skipped as a whole, so their required checks are not reported on the layer's head commit: a layer retargeted to `main` cannot merge on the light set, and needs a run with `main` as its base first (a push, or closing and reopening the pull request; re-running the old run keeps the old base). **A stack therefore merges top-down**: each layer into the one below it, then the bottom pull request into `main`, which runs the full matrix once for the whole stack. Merging only when a layer needs it, and never merging `main` into a stack only because `main` moved (the ruleset does not require a branch to be up to date), keeps a stack at about one full run.

Not changed: which legs run (the full matrix runs on every pull request into `main` and every push to `main`), the documentation-only rule, the required checks.

**Changes in v3.13** (the maintainer's decisions of 22 Oct 2026 on the code review of 21 Oct 2026, made at commit 64490da and checked again on `main` at 746bc0c; the review is not in the repository, so each item names its finding, WN-NNN, or design question, DQn):

*Dependents*

1. §3.2, §3.5 (WN-002; amends "one pass per incident" for the dependents step only), **a sidecar is repaired after every parent change.** A dependents-step repair records the parent it repaired against (container ID and `StartedAt`) with the sidecar's incident. When the sidecar is stale again and the parent's ID or `StartedAt` differs from that record, the old incident closes (reason "the parent changed again") and a new dependents-step incident opens at rung 0. It is not counted toward the flap limit (v3.6 item 10). A same-ID parent restart is always repaired by the API restart, never by `--force-recreate`, and a parent recreate always by the non-forced `up` (§3.5). While the parent is unchanged since the last repair, the one-pass rule stands: a repair that did not help moves up the ladder and ends SUSPENDED as before. The `recovering` flag is not used as the trigger, because a second parent event inside the post-action cooldown comes before it is set. The brakes are the global `WHALENURSE_RATE_LIMIT` and the parent's own flap and crash-loop rules. No flap brake is added for dependents.
2. §3.3 *Manual-stop markers*, §3.5 (WN-028; v3.6 item 9 confirmed), **a sidecar without its own opt-in gets manual-stop evidence.**
   - (a) For a container that holds a `container:` reference and is not opted in itself (unless it is labelled `whalenurse.enable=false`, which keeps it out of the engine entirely), the `kill`, `die`, `oom`, `start` and `create` events feed the engine's evidence as they do for an opted-in container. Scheduling stays gated on opt-in. So a sidecar stopped by hand stays stopped when its parent is recreated, which is the §3.3 marker rule applied to it, and whalenurse's own dependents-step actions on it are still attributed, not interpreted.
   - (b) A sidecar without its own opt-in that exited on its own, without a marker, **is** recreated by the dependents step when its parent's ID is gone, as v3.6 item 9 says ("a recreate of a dependent whose parent ID is gone"). Its own exit alone is still not healed without its own opt-in.
3. §3.5 (DQ5; amends v3.6 item 9 and v3.11 item 2), **a stale sidecar's opt-in is decided by its namespace parent.** The service of the container that the sidecar's `network_mode`/`ipc`/`pid` references is learned from the live `container:` reference and persisted on its own (beside the `parents:<key>` set of v3.11 item 2, in memory only in a dry run). Once the parent ID is gone, the dependents step repairs a sidecar without its own opt-in only if **that** service is opted in. Any other opted-in service in `depends_on` no longer counts. Only when the namespace parent was never learned (whalenurse first saw the sidecar after its parent was gone) do the `depends_on` services decide, as before. The `parents:<key>` set keeps its other uses (`staleParentRefusal`, `sidecarsSharing`, `dependentMayTakeMove`).
4. §3.6 check 1 (DQ3; amends the reference-holder skip), **the dry-run oracle also runs for a reference holder whose references are current.** On a reference holder's own failure (its own ladder, not the dependents step), check 1 runs the oracle when every container it references is the current, running container of its service. Then compose's resolved hash matches the one it recorded, so a `Recreate` means config drift. `config --hash` stays skipped for reference holders, because it hashes the unresolved form (E4). A reference holder whose references are not current is skipped as before. **Precondition:** an integration test on the CI compose matrix (both stores; v5.2.0, v5.6.0, latest) shows that the oracle reports no `Recreate` for an unchanged holder with current references and reports one after an edit. For any release where that does not hold, the check stays skipped and the reason is recorded in the notes.

*Upgrades and rollback*

5. §3.6 check 2, §3.7 (DQ4; amends v3.8 item 12), **rung 2 lets an image-only move through only when the pull rung would apply.** The allowance (`GuardInput.AllowImageMove`) for a `heal.pull=true` container now also needs its service's `pull_policy` to be read (§3.7 step 0) and to be neither `never` nor `build`. An unreadable policy gives no allowance (as v3.8 item 14). Without the allowance, an image-only move blocks as it does for a container without `heal.pull`: `DriftBlocked`, SUSPENDED.
6. §3.5, §3.7 (DQ2 and DQ6; amends v3.8 items 12 and 13), **a sidecar may follow its parent's rung-2 upgrade, and the rollback names it.** When rung 2 of a `heal.pull=true` container ends on a new image (`Upgraded`), the move is recorded like a pull's (the container, the time, the old and new IDs), marked as **followed**, not made by whalenurse. The dependents step may then take it under the conditions of v3.8 item 13: the reference still resolves to the move's result, the mover is a parent service of the sidecar, the move is not older than the mover's open incident, and the sidecar was created from the image that was replaced. The `Upgraded` rollback `up` line names those sidecars (`sidecarsSharing`), like `TagMoved`'s. A followed move is not named as the mover in another stack's drift block (v3.8 item 5): someone else moved that tag.
7. §3.7, §3.12 (polish item of the review; amends v3.8 item 6), **the rollback `up` line has `--no-build`**: `<docker compose> -p ... up -d --no-deps --pull never --no-build <svc> [<sidecar>...]`, like every other `up` whalenurse runs. `--pull never` already overrides every service's `pull_policy` (compose `cmd/compose/create.go`, `createOptions.Apply`), but compose still builds a service with `build:` whose image is missing; with `--no-build` a rollback whose tag line failed or was skipped stops with an error instead of building something that is not the old image. The lines stay pinned (never cut). The worst-case length test is re-checked against the 720-rune body, and the integration guard's rollback form accepts the flag. Events already stored keep the form they were written with.
8. §3.12 (WN-037), **the "upgraded during heal" message says the rollback does not last while `heal.pull` stays on.** One detail line, for example "to keep the old image, pin it in the compose file or remove whalenurse.heal.pull", stands before the pinned rollback lines. It is **cuttable**, the first line cut when the body would pass 720 runes. The rollback lines stay pinned and last. The docs and the dashboard's rollback section say the same (no plan rule needed for those).
9. §3.7 (DQ1; amends §3.7 step 1), **after the pull, before the `up`, the pause and the guard are checked again.** When the pull changed the image, whalenurse checks again, before the pull rung's `up`, (i) whether the key is now paused and (ii) the guard in image-move-allowed mode (checks 1 and 3; an oracle `Recreate` is a config finding unless `config --hash` matches the label and the image moved, v3.8 item 2). If either fails, the `up` is not run. The move is already recorded, so a `TagMoved` with `ok=false` says why the `up` was not run and carries the rollback. Then a pause follows the pause rules (no action; re-evaluated at resume or expiry), and a guard finding is a block (`DriftBlocked`, SUSPENDED). The check before the pull stays.

*Labels, identity and state*

10. §3.2, §3.4, §3.8, §4 (WN-005), **labels that come from the image do not steer whalenurse.** Docker copies an image's `LABEL`s into a container's labels for every key the create request does not set (moby `daemon/commit.go`, `merge`). whalenurse reads the image's own labels by the container's image ID (`docker.Container.ImageID`, not the reference, so a moved tag does not change the answer), cached per image ID:
    - **Compose identity:** a `com.docker.compose.*` label whose value equals the image's own label for that key is image-supplied. The container is then treated as not made by compose: its key falls back to the container name, it gets rung 1 only, no compose command is built from its labels, and it is neither a parent nor a dependent by service. One warning names the keys. (An image committed from a compose container carries these labels too; that case degrades the same safe way.)
    - **`whalenurse.*` on a compose container:** a key counts only if the service's `labels:` in the compose file declares it, read with `config --format json --no-env-resolution <svc>` (the reader of the `pull_policy`) and cached per `com.docker.compose.config-hash`. The value is the container's. If that read fails, or compose is unavailable, the rule for a non-compose container applies.
    - **`whalenurse.*` on any other container:** a value equal to the image's own label for that key is ignored, whatever it is, with one warning per container naming the keys.
    - The dependents step's parent opt-in uses the same filtered labels. No new variable. `WHALENURSE_MODE=all` is unchanged. `docs/labels.md` and `docs/security.md` name the image publisher as a label source.
11. §3.4 (WN-001), **the compose identity labels are validated before they reach a command line.** `compose.ProjectFromLabels` refuses a project that is not a normalized compose project name (`^[a-z0-9][a-z0-9_-]*$`, what compose-go's `NormalizeProjectName` produces) and a service that does not match the compose-spec service-name pattern `^[a-zA-Z0-9._-]+$` or starts with `-`. A refusal makes rungs 2 and 3 unavailable for that container (§3.4 *Fallbacks*: skip rung 2 and notify). As defense in depth, `--` goes before the service operand of `up`, `pull` and `config`, **only after** the CI compose matrix shows that every release in it accepts it, and the integration guard accepts the same form. If a release does not, validation alone stays and the notes say so.
12. §3.2, §4 (WN-010; the default stays the hostname), **persisted state is re-keyed to the current host ID at startup.** One store belongs to one installation, so at startup every persisted record keyed by a state key (per-key state, incidents, markers, flap and crash-loop history, the learned parents of v3.11 item 2 and of item 3, the key in the `tagmoved:` records) whose host segment differs from the current `WHALENURSE_HOST_ID` gets the current one. On a collision, the record with the later activity wins and the other is dropped. One log line gives the count. The stored event history keeps the host it was written with. This repairs a container install whose hostname (its container ID) changed at an upgrade, with no step by the user. The examples and `docs/deployment.md` still set `WHALENURSE_HOST_ID` for stable names in keys and notifications.
13. §3.8 (WN-027; amends the `/healthz` condition and v3.8 item 7), **the loop's Docker reads have a deadline, and `/healthz` reports a Docker API that stays down.** Every `List`, `Inspect` and `ImageID` call made on the main loop's goroutine, inside an action too, runs under a 30-second timeout. It is a constant, not a variable, and it applies only when the context has no earlier deadline. The events stream is not affected. A timeout is an API error and takes the existing paths (reschedule at the next poll, "cannot verify ... not acting", the pull rung refusing). `Healthy()` (so `/healthz`, the image's `HEALTHCHECK` and the uptime ping) is not ok when the Docker API has been failing for longer than 2 × `POLL_INTERVAL`. This check comes before the action-in-flight tolerance, which is otherwise unchanged. Without it, a timeout would keep the loop ticking and hide a dead API. `docs/running.md` and `docs/troubleshooting.md` state the new condition.

*Decision rules and markers*

14. §3.3 rule 3 (DQ7; the text is also corrected to the code), **a `created` container that compose is holding back is not started.** A `created` container (no `State.Error`) whose `com.docker.compose.depends_on` label (`<svc>:<condition>:<restart>`, compose `getCreateConfigs`; `up` creates every container first and runs `waitDependencies` before each start) names a service of its project with a condition that does not hold yet (`service_healthy`: that service's container is not healthy; `service_completed_successfully`: it has not exited 0) is held, with no incident. It is looked at again on each poll, and `status` and the dashboard say what it waits for. `service_started` holds nothing. Otherwise rule 3 applies, and its text now reads as the code does: a container that was `created` but never started → the ladder (start at rung 1, then recreate), not "→ recreate".
15. §3.3 *Manual-stop markers* (DQ8), **a reload signal sets no marker.** A `kill` event whose `signal` attribute is SIGHUP, SIGUSR1, SIGUSR2 or SIGWINCH (by number or name) is recorded but sets no manual-stop marker (`docker kill -s HUP`, docker-gen's `-notify-sighup`). Every other signal still sets one. An allow-list of SIGTERM and SIGKILL is rejected: `docker stop` sends the image's `STOPSIGNAL` (SIGQUIT for nginx, SIGINT for others), and those deliberate stops would then be healed.
16. §3.3 mechanism (b) (DQ10), **after a reboot, markers older than the previous run are kept.** When the boot id changed **and** the previous run's `last_seen` is known, only the markers set at or after `last_seen` − 30 s are dropped (as in (d)). Older ones are deliberate stops from before the reboot and are kept. Without a `last_seen`, every marker is dropped as before. The restore window of the first `STARTUP_SETTLE` is unchanged.
17. §3.3 rule 4 (DQ11), **an `on-failure` container that Docker will not restore is treated like `restart: "no"`.** Docker's `on-failure` policy restarts only on a non-zero exit while retries remain (moby `restartmanager`: `restart = exitCode != 0`). So a container stopped by the host with `on-failure` that exited 0, or whose `MaxRetries` are spent, stays down with one notification (`NotifyOnly`, "left down by policy"), and its `network_mode` dependents stay down with it. Only `heal-exited=always` starts it. A non-zero exit with retries left is still left to Docker's restore.

*Notifications, history and supply chain*

18. §3.12 *Dedupe* (DQ9; amends v3.7 item 13), **`Suspended` and `Unsuspended` make each other news again only once per incident.** The first `Unsuspended` after a `Suspended`, and the first `Suspended` after that, are sent. Further alternations in the same incident are recorded and shown but not sent. A new incident is news again.
19. §3.12 table (WN-018; wording only, the behaviour of v3.7 items 4 and 13 is unchanged): "`Suspended`, reason `drift`: not sent **when it directly follows its `DriftBlocked`**; a re-suspension later in the same incident (after an `Unsuspended`) is sent as a giving-up message, the first one only (item 18)."
20. §3.12 *Secrets* (WN-019; narrows the redactor of v3.7 item 3 and v3.9 item 6(c)), **boolean option values are not masked.** The redactor does not take a query value, or a comma piece of one, as a secret when it is a boolean word (`true`, `false`, `1`, `0`, `yes`, `no`, `on`, `off`, any case): none of them can be a token, and masking them turned whalenurse's own `ok=false` into `ok=***`. Every other rule of the redactor stays. Separately, without a plan change, the dashboard's "last upgrade" decides `ok` from the raw event, before redaction.
21. §3.10 (DQ12; amends v3.9 item 1), **retention keeps the events of an open incident.** The age prune skips events whose `incident` attribute belongs to an incident that is still open for that key, so a long SUSPENDED keeps its cause, its drift findings, its mover and its rollback. The 100,000-row cap still applies as a last resort. It deletes events of closed incidents and events without an incident first, and those of open incidents last.
22. §3.13 (WN-032, WN-033, WN-035), **three more supply-chain rules.**
    - **Pinned** also covers the Dockerfile syntax frontend (pinned identically by digest in both Dockerfiles, or dropped from both) and the BuildKit image `setup-buildx-action` runs (`driver-opts: image=moby/buildkit:vX.Y.Z@sha256:...`, in both workflows). `check-dockerfiles.sh` and `check-pins.sh` check both, and `docs/releasing.md` gives the manual bump, since Dependabot does not see them.
    - **Gates:** `govulncheck` (a pinned version) runs in the `go` job and in the release `test` job and fails on a reachable vulnerability. The Go version of `go.mod` (`toolchain`, else `go`) must equal the `golang:` base of both Dockerfiles, checked by a script with `--self-test`.
    - **The publish job builds with an empty Go cache** (`cache: false` on setup-go in `release.yml`, `go mod download` with retries before GoReleaser), checked statically.
    - The permissions of v3.10 are unchanged.
23. Declined or documented only. **DQ13** (warn when the bundled compose falls behind) is declined. It needs an outbound call that whalenurse does not make, or a guess from the build date; Dependabot keeps the pin current, and `status` shows the compose in use. **DQ14** (a dry run keeps no boot id, `last_seen` or markers, so after a reboot it can report "would start" for a container the live run leaves down) is documented in one sentence where the docs recommend a dry run first. There is no state file for dry runs (m2a-notes item 7 stands).

**Changes in v3.12** (the M7 design, written before any M7 code; the maintainer chose R5 as the first post-v1 item, a plan first, and items 3, 4 and 7 on 20 Oct 2026):
1. §3.4 (v3.6 item 1 amended), **the compose child never reads a Docker config.** v3.6 item 1 removed `DOCKER_CONFIG` from the compose child's environment but kept `HOME`. With `DOCKER_CONFIG` unset, the Docker CLI reads `$HOME/.docker` (docker/cli `config.Dir()`). Under the example systemd unit that is `/var/lib/whalenurse/.docker`, so a `config.json` there would have brought credentials and a `currentContext` pointing at another daemon. Confirmed with compose v5.6.0: a malformed `config.json` in that place is parsed. Recent docker/cli also reads credentials from `DOCKER_AUTH_CONFIG`, which was passed through. Now every compose child gets `DOCKER_CONFIG` = **an empty directory whalenurse creates at startup**:
   - `compose.PrivateConfigDir`: `MkdirTemp`, mode 0700, a fresh name, so nothing can be planted in it;
   - placed next to the control socket, otherwise in the temp directory, and removed at exit;
   - `DOCKER_AUTH_CONFIG` is removed too.

   The directory must be **writable**: compose v5.2.0 to v5.5.x depend on buildx (v0.35 to v0.36.1), and their dry-run client calls buildx `store.New`, which creates `<config>/buildx/...`. CI found this: a first version used `/proc/whalenurse-no-docker-config`, a path nothing can create, and the 5.2.0 oracle failed and fell back to `config --hash`. That path stays only as the last resort (`compose.NoDockerConfig`, also the zero value of `ExecRunner`), with a startup warning.

   **Inferred:** in the read-only image, compose v5.2.0 to v5.5.x have probably always lost the oracle this way, because `/home/nonroot/.docker` cannot be created. The private directory should restore it, but nothing tests the oracle inside the image.

   This is a fix to v0.1.0 (pull request #28; CHANGELOG under Unreleased). The container image was not affected by the credentials and context exposure: nothing in it creates `/home/nonroot/.docker`. Checked against docker/cli v29.6.0, the version compose v5.2.0 (the oldest supported) uses: it has no legacy `$HOME/.dockercfg` fallback.
2. §7 (new M7), §8, **R5 becomes milestone M7**, the first post-v1 milestone. The rest of §8 stays post-v1.
3. §3.7, §4, **`WHALENURSE_REGISTRY_AUTH_DIR`** (the maintainer's name). It is the path of a directory holding `config.json`: a read-only mount in container mode, any directory under systemd. Empty, the default, keeps today's behaviour, with no credentials. **Only `pull --policy always <svc>` gets it**, as `DOCKER_CONFIG=<dir>`. Every other compose command (the oracle, `config --hash`, `config --format json`, `up`) keeps the private empty directory of item 1, and whalenurse's own Engine API calls never send credentials. The dry-run actor still never pulls.
4. §3.7, **what the file may hold** (the maintainer's answer: only `auths`, else refuse):
   - The top level of `config.json` may hold **only `auths`**. Any other key is a **startup error that names the key** (`currentContext`, `credsStore`, `credHelpers`, `proxies`, `features`, ...), never a value, because the pull command reads the whole file.
   - Each entry of `auths` needs an inline credential: `auth` (base64 `user:password`, which must decode and contain a colon), `username` with `password`, or `identitytoken`.
   - The image has no credential helpers, so `credsStore` and `credHelpers` could not work anyway.
   - Each key of `auths` is a registry host (`ghcr.io`, `registry.example.lan:5000`; Docker Hub is `https://index.docker.io/v1/`).
   - A `contexts` entry in the directory is refused.
   - A missing or unreadable file, malformed JSON, or an empty `auths` is a startup error.
   - The file is checked at startup and again before each pull, because compose reads the file itself on each pull. If the file has become invalid by then, the pull is an ordinary failed action whose reason names the problem (redacted).
   - At startup whalenurse warns when the directory or the file is writable by its own process ("mount it read-only").
5. §3.9, §3.12, **the credentials are secrets**, like the notification URLs:
   - Never logged, and never in `status`, `/healthz`, the API, the pages or a notification.
   - The redactor learns every `auth` value, its decoded password, every `password` and every `identitytoken`, so compose's error text cannot leak one.
   - `config` never quotes the file's values in an error.
   - `TestNoTokenReachesTheLogsStatusHealthzOrTheTestOutput` and `TestNoSecretReachesAnyPageEndpointOrStream` plant registry credentials.
6. §3.7, **no new notification type.** A pull refused by the registry (401, `denied`) is an ordinary failed pull: `ActionResult`, then the giving-up message with compose's redacted reason. Troubleshooting says what to check.
7. §3.11, **what is shown** (the maintainer's answer: registry hosts only). `status`, `/api/v1/status` (a new field, `pull_auth.registries`: a sorted list of hosts, empty when off) and the home page name the registry hosts in `auths`, never a user name or a secret. The API only gains a field; the golden changes.
8. §3.9, `docs/security.md`, **the risk, written down.** The credentials sit in a container that holds the Docker socket. The socket is root on the host, so whoever controls whalenurse already controls the host; what is added is that the credentials can be read inside the container. The advice:
   - a **read-only, pull-only token** scoped to the repositories needed (not a personal password and not your `~/.docker/config.json`);
   - the file at mode `0400`/`0440`, owned by uid 65532 (container) or by the `whalenurse` user (systemd);
   - the directory mounted `:ro`.
9. §6, §7 M7, **the tests.**
   - **Unit:** the validation table (each refused key, each bad entry, the `contexts` directory, a writable directory warns); the environment per command (only `Pull` gets the directory, the private empty one otherwise); the redactor with each credential form; the status and API field; the two secrets tests.
   - **Integration** (CI, both image stores, the compose matrix): a second `registry:2` with htpasswd auth (`REGISTRY_AUTH=htpasswd`; the bcrypt file is written by the test with `golang.org/x/crypto/bcrypt`, because `registry:2` no longer ships `htpasswd`), as a `whalenurse-test-registry-*` project on `127.0.0.1:<port>`. The Guard also accepts `docker login` to that registry only, with a temporary `--config` directory that the test removes. Cases:
     - **positive control:** without the variable, the pull rung fails on the authenticated registry with an auth error and the container ends SUSPENDED with the image unchanged;
     - **with the directory** (mode `0555`, the file `0444`): the pull rung upgrades, and the directory tree is byte-identical afterwards (listing and sha256), so compose wrote nothing into it (#28's CI already showed that `pull` and `up` on compose 5.2.0 run with an uncreatable config directory; only the dry-run writes, and the pull never dry-runs);
     - a `config.json` with `credsStore` refuses to start;
     - **once with the image:** the directory mounted `:ro`, uid 65532, the real deployment shape;
     - no credential appears in the logs, `status` or the API of any of these runs.
10. §6, the documentation and examples:
    - `docs/configuration.md` (the variable), `docs/security.md` (item 8), `docs/deployment.md` (container and systemd), `docs/troubleshooting.md` (401 and `denied`, a refused key), `docs/running.md`;
    - `examples/full` gets the mount and the variable, commented out;
    - `CHANGELOG.md` under Unreleased.

    A new feature is a new minor version (v0.2.0), so it goes through the release gate in [releasing.md](releasing.md); the tag stays the maintainer's.

**Changes in v3.11** (from the release-gate rehearsal on a real host, 20 Oct 2026; evidence in [m6-notes.md](m6-notes.md)):
1. §3.5, **a sidecar's repair incident closes.** The incident the dependents step opens for a dependent that is not opted in never recovered: `rules.Classify` skipped a healthy non-opted-in container before the engine saw it, so `recovering()` never ran, and the next parent recreate found the incident at "rung 2 done" and suspended the sidecar unrepaired. Now a decision that needs no action reaches the engine while the container has an open incident (`rules.Input.IncidentOpen`). No change of rule: §3.5 already said the dependents step repairs a sidecar every time.
2. §3.5 (decision 9 of v3.6, amended), **the parent of a dependent is remembered.** Decision 9 found the parent, once its ID was gone, "through the services named in the sidecar's `depends_on` label". Compose writes that label from the services in scope, so a dependent recreated with `up -d --no-deps` (how whalenurse does it, and what the plan prescribes) comes back with an **empty** label (compose `pkg/compose/create.go` builds `com.docker.compose.depends_on` from `service.DependsOn`; `--no-deps` narrows the project with `types.IgnoreDependencies`; observed on v5.6.0 with real containers, and `config-hash` is unchanged, so no false drift). The first repair worked, every later one was skipped, silently. whalenurse now **learns** the services a dependent depends on and keeps them per state key (in memory, and in the store's meta as `parents:<key>`; a dry run keeps them in memory only): a non-empty label is authoritative and replaces the set; a live `container:` reference (network, ipc or pid) that resolves to a container of the same project adds that container's service and never shrinks the set. It is learned on every look at the container, before the skip rules, so non-opted-in dependents are covered. One resolver (`Daemon.parentServices`: the label while it has any, otherwise the learned set) serves all four readers of the label: the opt-in lookup, the wait for the services it depends on, the sidecars of a pull's rollback line, and the tag-move rule. An entry is dropped when its key has been out of the container list for an hour. Reading `network_mode` from the compose file is not done (a possible fallback for a lost store).

**Changes in v3.10** (from building M6; evidence, upstream versions and open points in [m6-notes.md](m6-notes.md)):
1. §3.13 (new), §7 M6, **the release pipeline.** `.github/workflows/release.yml` runs only on a `v*` tag. Its `test` job (`contents: read`) checks that the tag is `vX.Y.Z[-pre]`, that it points at a commit **on `main`**, that `CHANGELOG.md` has a **dated** section for it, and runs `go vet`, the unit tests and the release scripts' self-tests. Its `publish` job (`needs: test`) is the **only** job with `packages: write` and `id-token: write`, and also has `contents: write` (create the release) and `attestations: write` (store provenance): **four permissions, not the two the M6 brief named** (a deviation, recorded in [m6-notes.md](m6-notes.md)). It runs GoReleaser v2.18.3, which builds static `linux/amd64` and `linux/arm64` binaries (`CGO_ENABLED=0`, `-trimpath`, the version stamped with `-X main.version`), `.tar.gz` archives (binary, LICENSE, README, CHANGELOG, the systemd unit), one syft SPDX SBOM per archive and `checksums.txt`; builds and pushes the multi-arch image `ghcr.io/jackspiering/whalenurse` from `Dockerfile.release` with the tags `vX.Y.Z`, `vX.Y`, `vX` and `latest` (the last three only for a release that is not a pre-release, so an `-rc` tag never moves them); signs `checksums.txt` (`checksums.txt.sigstore.json`) and every image digest with **keyless cosign v3** (GitHub OIDC); creates the GitHub release with the notes of that version's `CHANGELOG.md` section (`scripts/release/release-notes.sh`, which fails closed: no section, no date, or an empty body is an error; GoReleaser's own changelog is off). `actions/attest` then stores GitHub build provenance for the archives and SBOMs (`checksums.txt`) and the image (`digests.txt`), and `scripts/release/verify.sh` runs against what was just published. Pull requests and `main` run a **GoReleaser snapshot** (`release --snapshot --skip=sign`) with `contents: read` only, no registry login and no OIDC: the same archives, checksums, SBOMs and image are built and checked, and nothing is pushed or signed.
2. §3.13, **`Dockerfile.release`** copies the binaries GoReleaser built (`$TARGETPLATFORM/whalenurse`); `Dockerfile` (compiles from source) stays the CI and development image. Both bundle the same compose release and checksums and use the same base images, **pinned by digest** (`golang:1.27.2@sha256:...`, `distroless/static:nonroot@sha256:...`); `scripts/ci/check-dockerfiles.sh` fails CI if one moved without the other. The compose download runs on the build platform (`FROM --platform=$BUILDPLATFORM`) and the final stage has no `RUN`, so the multi-arch build needs no emulation.
3. §3.13, **every action in every workflow is pinned to a full commit SHA** with the version as a trailing comment (`scripts/ci/check-pins.sh` and `actionlint` run in CI); Dependabot proposes updates for `github-actions`, `gomod`, `docker` and `docker-compose` (the examples' images) weekly, grouped.
4. §3.13, **verify a release**: `scripts/release/verify.sh <tag>` (`make verify-release TAG=...`) checks the cosign signature of `checksums.txt` pinned to the exact identity `https://github.com/jackspiering/whalenurse/.github/workflows/release.yml@refs/tags/<tag>` and the issuer `https://token.actions.githubusercontent.com` (no regexp), every file the signed checksums name, the image's signature, and the build provenance (`gh attestation verify`, skipped with a notice without the `gh` CLI). Documented in [verifying-releases.md](verifying-releases.md); the script has a self-test with stubbed tools that runs in CI.
5. §3.9, §4, **`WHALENURSE_HTTP_TRUSTED_PROXIES`** (the v3.9 M6 candidate; item 15's "a trusted-proxy option"): a list of CIDRs or addresses. The wrong-token limiter keys on the connection's peer address as before; **only when the peer is inside the list** does it read `X-Forwarded-For` (all header lines as one list) and take **the right-most entry that is not itself a trusted proxy** (the address the proxy appended; what a client wrote further left is never reached). An entry that is not an address, or a header of trusted proxies only, falls back to the proxy's own address (the strict choice). A peer outside the list is keyed on itself whatever it sends. The result goes through the same IPv6 /64 reduction. A prefix of length 0 (`0.0.0.0/0`, `::/0`) is a configuration error: every client would be a proxy. Ignored with a startup warning when there is no token. Default: empty, which is the v3.9 behaviour.
6. §3.13, §6, **the documentation set**: README (rewritten), `docs/configuration.md` (every variable), `docs/labels.md`, `docs/security.md`, `docs/troubleshooting.md` (exit codes, SUSPENDED, drift messages, "can't verify", notifications, reading the dashboard), `docs/deployment.md` (container and systemd), `docs/upgrading.md` (the store schema), `docs/verifying-releases.md`, `docs/releasing.md` (the **[maintainer]** steps and the release gate), `SECURITY.md`, `CHANGELOG.md` (v0.1.0, Keep a Changelog), `CONTRIBUTING.md`. Two tests keep them honest: every `WHALENURSE_*` name read in `config.go` and every label in `labels.go` must be documented (and every documented one must exist), and every relative link and anchor in the Markdown files must resolve. `docs/running.md` stays as the detailed behaviour reference and points at the new pages.
7. §6, §7 M6, **the examples are tested in CI**: `docker compose config -q` on `examples/minimal`, `examples/demo` and `examples/full` (the last with its `.env.example`; each must also refuse to start without its required variables), `systemd-analyze verify` on the unit (pointed at a real binary; any output at all fails), and the **demo job** runs `examples/demo` for real with the image built from the commit (project `whalenurse-test-demo`, GitHub Actions only): it breaks `bop` (`unhealthy sticky=true`) and waits for the restart, the recreate, the sidecar's recreate behind it and the history in the API.
8. §7 M6, §8, **post-v1** (not built; see [m6-notes.md](m6-notes.md)): the read-only `DOCKER_CONFIG` mount for private registries (R5: it reverses the environment scrubbing of v3.6 item 1, puts registry credentials in a container that has the Docker socket, and needs an integration test against an authenticated registry); the rehearsal of the signing steps before the first tag (only the publish job may have `id-token: write`, so it is done with an `-rc` tag, [releasing.md](releasing.md)); a dashboard looked at on real phones.

**Changes in v3.9** (from building M5; items 11 to 16 are the maintainer's decisions on its review; evidence, deviations and open questions in [m5-notes.md](m5-notes.md)):
1. §3.10, §3.11, **the domain events are persisted** (store schema 3, table `events`: `id INTEGER PRIMARY KEY AUTOINCREMENT`, time, host, key, container, type, message, attributes as JSON). The bus stamps the ID and the store inserts the event under that ID (`Bus.Seed(store.LastID())` at start, so IDs continue across restarts; `AUTOINCREMENT` keeps the counter after retention has deleted the newest rows, so an ID is never reused). Retention is `WHALENURSE_HISTORY_RETENTION` (applied by the hourly prune with the closed incidents) plus a cap of 100,000 rows. This is the API's `since` cursor and the timelines. **A dry run stores nothing**, so it serves an in-memory ring of the last 5,000 events (`engine.Ring`) and its IDs restart with the process. The API reads through a second read-only SQLite connection, so a page view never makes the healing loop wait. **A `since` above the host's last ID is a reset**: the reply has `reset: true` and starts from the beginning (a wiped store, a dry run that restarted), and the caller drops what it holds for that host.
2. §3.11, **the API**: `GET /api/v1/status`, `/containers` (watched containers; `?all=1` for every one), `/containers/{key}`, `/events?since=&limit=&key=&type=` (oldest first; `Next` is the cursor), `/incidents`, and the SSE stream `/api/v1/stream` (`id:` is the event ID, so `Last-Event-ID` or `?since=` resumes from the event log; `?key=` narrows it; a `reset` event when the cursor is ahead; keepalive comments every 15 s; at most 32 streams; it ends on shutdown). The stream wakes on a hub subscriber that only signals (the engine publishes under its lock), and each client reads the log from its own cursor, so a slow client costs nothing. Every record carries `host`, every time is UTC RFC 3339 (`omitzero`, never `0001-01-01`), and `/api/v1/...` only gains fields. The documents are in `internal/api/doc.go`; the golden files in `internal/api/testdata/golden/` are the contract the hub (R1) will read.
3. §3.11, `/metrics` (Prometheus text format 0.0.4, written by hand: no client library) is served **only with `WHALENURSE_METRICS=true`** (new variable, default false) and sits behind the same token.
4. §3.9, §4, **the token** (`WHALENURSE_HTTP_TOKEN`). A request needs `Authorization: Bearer <token>`, **or HTTP Basic with the token as the password** (any user name). The second form is an addition to "bearer": a browser cannot send a bearer header on a page load or an `EventSource`, and with no POST there is no login form or cookie, so Basic is how the dashboard works behind a token. Both compare the SHA-256 of the input with the SHA-256 of the token in constant time. **The token is never read from the URL.** A 401 offers both challenges. `/healthz` needs no token (item 12). Wrong tokens are limited per client IP (item 15). Use a long random token (a startup warning for under 16 characters).
5. §3.9, **read-only, enforced in one wrapper** (`api.Secure`): only GET and HEAD are served, every other method gets 405 with `Allow: GET, HEAD` (before the token check, and with no CORS headers); the control socket's paths do not exist on TCP. On every response: `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Resource-Policy`/`-Opener-Policy: same-origin`, `Cache-Control: no-store` (the static assets: `private, max-age=300`). So the templates have no inline script, `on*=` handler or `style=`; a test greps every rendered page for them.
6. §3.9, **what leaves whalenurse** is decided in one place (`api.Service`), whichever endpoint, page or stream: (a) labels: only `com.docker.compose.*` and `whalenurse.*`; (b) event attributes: an allow-list of the keys the engine and the daemon emit (anything else is dropped, so a future attribute stays inside until someone adds it); (c) every message, attribute value and `State.Error` goes through the notification redactor, which now also knows the HTTP token, and is cut (2,000 runes; 600 for `State.Error`); (d) the environment, command, mounts and network settings of an inspect never enter `docker.Container` (a test plants them). `State.Error` of a container that failed to start is shown (it is the reason the container is down; it names paths such as a missing bind source, and the same text is already in the event messages).
7. §3.11, **the dashboard** is server-rendered `html/template` over `api.Service`: `internal/web` imports `internal/api` and nothing else of whalenurse (a test checks its imports), so a page can show only what the API says. Assets are `embed`ded (one stylesheet with light/dark by `prefers-color-scheme` and a phone layout, one script of about 60 lines: it refetches the page when the stream announces an event and every 15 s, and swaps `<main>` and the banners; without JavaScript the pages work). Pages: `/` (healer status, compose binary and version, the version gate with its minimums, dry-run banner, action budget, counts, pauses, notifications, containers that need attention, the latest events), `/containers`, `/containers/{key}` (inspect summary, healer and ladder with the rungs run, SUSPENDED or PAUSED with reason, cause and expiry, the drift guard's last verdict, parent and dependents, the last upgrade with its rollback commands, incidents, timeline, allowed labels), `/history` (incidents, and events filtered by chips: links, because the CSP forbids forms).
8. §3.11, **the drift status on the detail page is the guard's last verdict** (the last `DriftBlocked` or `DriftWarned` event of the key, or "blocking" while the key is SUSPENDED for drift), not a live check: the guard runs `compose` dry-runs and `config --hash`, which a page view must not trigger.
9. §3.9, §3.8, **exposure**: the listener is off unless `WHALENURSE_HTTP_ADDR` is set. At startup whalenurse warns when it is not loopback and no token is set, and when the token is under 16 characters. Recommended: publish on `127.0.0.1:8484:8484`, behind a reverse proxy, or through a Tailscale container on a **shared bridge network** (`tailscale serve`), never `network_mode`. Without a token only loopback Host names are served, against DNS rebinding (item 14).
10. §6, §7: the M5 tests (goldens per endpoint, every page rendered against a real store through a real daemon, the secrets test with positive controls, every non-GET refused on every route, the token cases, SSE replay and bounds, the import rule, the store and ring tests) and the CI integration test of the image (`TestImageServesTheDashboardBehindTheToken`, `...WithoutATokenOnLoopback`). M5 is done; **the pages were not looked at in a browser** (m5-notes).
11. §3.9, §4 (maintainer decision): **Basic next to bearer is approved** (password = the token, user name ignored, the same constant-time compare). Both send the token in clear on every request, so the docs say: **use the dashboard only over TLS** (a reverse proxy, or Tailscale Serve) or on loopback.
12. §3.8, §3.9 (maintainer decision): **`/healthz` stays open with a token and, on the TCP listener, answers only `ok` or `not ok`** (503), never a reason (`PublicHealthHandler`). The reason is in the token-protected `/api/v1/status` and on the unix socket (`HealthHandler`, for the image's HEALTHCHECK and the CLI).
13. §3.11, §4 (maintainer decision): **`/metrics` is off by default** (`WHALENURSE_METRICS`) and, when on, behind the token.
14. §3.9, §4 (maintainer decision): **DNS rebinding.** Without a token, only the Host names `localhost`, `127.0.0.1` and `[::1]` (any port, case-insensitive) and the names in the new `WHALENURSE_HTTP_ALLOWED_HOSTS` (comma or space separated; port, brackets and a trailing dot are ignored) are served; any other name gets 403 (the body never echoes the host). A browser sends the name it was given, so a page that rebinds its own name to 127.0.0.1 is refused. `/healthz` is exempt (it says only ok or not ok, and monitors reach it by IP). **With a token the Host is not checked** (a reverse proxy forwards its own name, and the token is the defence); the variable is then ignored with a warning. A client that is not a browser can send any Host: the check defends the browser case only.
15. §3.9 (maintainer decision): **wrong-token limit: 10 per minute per client IP, then 429 with backoff.** Only requests that presented credentials and failed count (a browser's first request, before its prompt, does not). On the 10th wrong token within a minute the client is blocked for 1 minute, then 2, 4 ... up to 15 minutes for each further block (the strike count is forgotten after an hour without a failure). **While blocked every request gets 429 with `Retry-After`, a right token included** (otherwise the block would only slow guessing down); `/healthz` is exempt; a right token never resets the count. The client is the connection's peer address (IPv6: its /64); `X-Forwarded-For` is ignored because a client can forge it, so **behind a reverse proxy all users share the proxy's budget**: an attacker can lock the dashboard out for everyone, which is why the token should be long (a trusted-proxy option is a candidate for M6). The block is logged once (client IP, duration). The table holds 4,096 clients.
16. §3.11 (maintainer decision): **a skipped rung has its own marker.** The engine counts a rung it cannot run (version gate, no compose inputs, a `pull_policy` that forbids the pull) as passed, so the page drew `✓`. The `RungUnavailable` events of the open incident (matched by the `incident` attribute) now give `healer.skipped_rungs` (`rung`, `reason`) in the API, and the pages say "recreate skipped – not available" and "1 of 3 rungs run, 1 skipped (not available)" instead of a tick.

17. §3.11 (maintainer decisions after the screenshot review; evidence in [m5-notes.md](m5-notes.md) and `docs/screenshots/`): **dashboard fixes, no change to the API's contract except two optional fields.** (a) Tables become **cards up to 900 px** (was 640: a table of seven columns needs about 790 px) with each label and its value on one line, and every table sits in its own horizontally scrolling container as a safety net: no page-wide scroll at any width from 320 to 1440. (b) **A pause row links to the containers under its scope** (`/containers?scope=<project[/service]>`), and an event or incident of a container that is gone is plain text: no link leads to a 404. (c) **`EventAttributed` is hidden** in "Latest events", the timelines and History; History has a "Debug (everything)" filter and the detail page a "show them (debug)" link; `?debug=1` shows them. The JSON API still carries every event (a hub follows the cursor); `EventsQuery.HideNoise` and `engine.EventQuery.ExcludeTypes` do it, so the hidden events do not use up the 100 timeline slots. (d) **The final container name**: compose's temporary `<12 hex>_<name>` is stripped wherever a name is shown (API and pages). (e) The Time column does not wrap. (f) **Expiries read "2m left (until 14:32)"** (UTC; another day carries its date; a passed one says "ended 3m ago"). (g) Detail page: "Finished" only for a container that is not running; "Restart policy: no (compose default)"; empty labels are not shown (the JSON keeps them); no Container column in a container's own timeline and incidents; **the parent's and each dependent's state as a badge** under "Related" (new optional fields `state`, `health`, `healer_state` of `RelationDoc`). (h) Rollback: **one command per `<pre>`**, no wrapping (`white-space: pre`, `overflow-x: auto`), **a Copy button per command**, wired by `app.js` (delegated click, `navigator.clipboard` in a secure context, a hidden textarea and `execCommand` otherwise); the buttons exist only with JavaScript; the CSP is unchanged. (i) **The drift finding in plain language** ("compose would recreate this container: the configuration or the image changed since it was created"), compose's own words ("Recreate, Recreated, Starting, Started") under a "details" element, for every event text, the SUSPENDED banner and the drift card. Not changed, by design: host paths in the compose labels (they are `com.docker.compose.*` labels, §3.9) and in the rollback commands.

18. §3.11 (maintainer decisions, the last round of the screenshot review; fixes only): (a) **the Copy button sits beside the code block**, not on top of it (`.cmd` is a flex row: the `<pre>` scrolls, the button stays), so a long command is never covered. (b) **A drift-blocked SUSPENDED reads in plain language**: "Unhealthy; a restart didn't help, and the recreate was blocked because the configuration or image changed since the container was created." in the banner (the restart clause only when a restart ran), the original text under "details"; the `Suspended` row of a timeline, History and the overview is shortened to "Unhealthy; the recreate was blocked by the drift guard, so the container is SUSPENDED." because the `DriftBlocked` row beside it carries the explanation (also in plain words). A finding that is a moved tag says "the image tag moved since the container was created"; one that cannot be verified is shown as it is. (c) **The ladder marks a blocked rung**: "restart ✓ → recreate ✗ blocked (drift)" (the first rung that did not run, when the container is SUSPENDED for drift), and the list says "recreate blocked (drift)". (d) The "Last upgrade during heal" card no longer repeats the raw "…; recreated" line under its badge line (the timeline row keeps it). (e) **A Paused event reads "paused p/web for 2h, until 19:05"** (the Go duration and the ISO time are rewritten; a pause that ends on another day carries its date). (f) **Cards (up to 900 px) leave out the fields that say "none"** (health, incident, cooldown); the table keeps them. All of it is in `internal/web`; the API's documents and events are unchanged.

**Changes in v3.8** (from building M4; evidence and open questions in [m4-notes.md](m4-notes.md)):
1. §3.7, **`compose pull --policy always <svc>`**, not a bare `pull <svc>`. Upstream check (compose v5.2.0, v5.6.0 and main, `pkg/compose/pull.go`, `cmd/compose/pull.go`): `compose pull` skips an image whose `pull_policy` is `never` or `build` in every release, and `missing`/`if_not_present` when the image is already present (v5.2.0 for every tag; v5.6.0 and later only refresh `latest` and a service with no policy). A skipped pull leaves the digest unchanged and the rung would say "no newer image" for an image it never looked up. `--policy always` (present in v5.2.0 and v5.6.0) overrides the service's policy for the project, so it replaces `missing`, `if_not_present`, the refresh windows and no policy at all; **`never` and `build` are respected, not overridden** (item 14). Nothing else is added (no `--include-deps`): one service. Compose's `up` stays `--pull never --no-build`.
2. §3.7, §3.6 check 1, **the guard before a pull runs checks 1 and 3 only** (`GuardInput.AllowImageMove`). Compose's dry-run oracle says `Recreate` for a moved tag as well and cannot say why, so before a pull a `Recreate` is a config finding **unless** `config --hash` still equals the container's label **and** the explicit comparison shows the image moved: then it is the image alone, which a pull is meant to change, and it does not block. Anything that cannot be established blocks. (Rung 2's guard blocks and SUSPENDS on a moved tag, v3.6 item 6, except for a container with `heal.pull=true`, which takes the move: item 12.)
3. §3.2, §3.7, **an unchanged digest SUSPENDS at once** (no cooldown to wait out), with the cause "the pull found no newer image for <ref> (still <id>)". "Changed" means the platform image ID (`image inspect --platform linux/<arch>`, the drift comparator) moved, because that is what will run. The unchanged pull does not send its own message: the giving-up message says it.
4. §3.7, **after a changed pull**: the move is recorded at once (the tag has moved for every stack that shares it, whether or not the `up` works), then the non-forced `up -d --no-deps --pull never --no-build <svc>`, then a **post-check that the container really was replaced** (compose recreates on a changed image by itself, E5; if it left the old container, it is recreated with `--force-recreate`; "upgraded" is announced only for a container that was replaced and runs), then `TagMoved`, then the dependents step exactly as after rung 2. The pull spends a rate-limit slot and, like rung 2, is skipped (`RungUnavailable`) with the version gate or without usable compose inputs: **rungs 2 and 3 are unavailable together**.
5. §3.6, §3.7, **`TagMoved`** (new domain event; attributes: reference, old and new image ID, the image to roll back to, the two rollback commands, `ok`/`error`). The record (reference normalized, so `nginx`, `nginx:latest` and `docker.io/library/nginx:latest` are one; which container's pull rung; when; old and new IDs) is kept in the store's meta and survives a restart. A drift block (image finding) on another stack that shares the tag **names the container that moved it and when**, on its own line of the message and as event attributes, **only while the reference still resolves to what that pull produced**; if someone moved it again, nothing is claimed.
6. §3.12, **the "upgraded during heal" notification** (`TagMoved`; important): old → new image (12 hex digits each) and the exact rollback, two lines at the end of the body that are never cut: `docker image tag <old image id> <ref>` and `docker compose -p <project> --project-directory <dir> -f <file>... [--env-file <f>...] up -d --no-deps --pull never <svc>` (the host's `docker compose`, not whalenurse's binary). The old image is the **plain** ID read before the pull (the index digest on the containerd store, which can be tagged; the platform manifest digest may not be). **A change to the v3.7 size rule:** the body limit is 360 runes, except for a message with commands to copy, whose commands are kept whole and whose limit is 720; the title limit is unchanged. If the commands are longer than that, they are still kept whole. The pull's own `ActionResult` is not notified when it has an outcome (`unchanged`, `upgraded`, `upgrade-failed`): the giving-up and the upgrade message say it. A failed pull command is an ordinary failed action ("pull failed", with the error). A dry run says "would pull", marked, and records nothing.
7. §3.8, §3.3 (d), **actions that block the loop** (the loop is synchronous: a pull, an `up` and a forced recreate can run for minutes): (a) `/healthz` and the uptime ping tolerate an **action in flight**: the "main loop silent" check is skipped for 11 minutes from the start of the current phase (the pull, the up and a forced recreate each restart the count); a pull is bounded at 10 minutes (`pullTimeout`). (b) **The `last_seen` heartbeat is written from a goroutine of its own while an action runs** (every 5 s), because the shutdown window within one boot ends 5 minutes after `last_seen` (§3.3 rule 4): without it a daemon restart in the middle of a long action would put the containers it stopped outside the window, and a `restart: "no"` app that exits 1 on SIGTERM would be started, the bug of v3.5. This also covers rung 2's recreate (5 minutes), which sat on that edge.
8. §3.7, **private registries are not supported by rung 3**: the compose child process runs without `DOCKER_CONFIG` (§3.4) and the image carries no credentials, so a registry that needs a login fails the pull as an ordinary failed action. Recorded as a limitation, not a block.
9. §3.2: **rung 3 requires rung 2** (maintainer decision, item 11): see there. (The first M4 build appended the pull to whatever `actions` said.)
10. §6, §7: the M4 integration tests against a local `registry:2` (a `whalenurse-test-registry-*` project on `127.0.0.1:<port>`; the Guard accepts only `127.0.0.1:<port>/whalenurse-test/...` for push, pull and tags, only `pull --policy always <one service>` from compose, and runs the rollback lines only after checking them): no label, no pull; no newer image, SUSPENDED; newer image, upgraded with the sidecar repaired and the rollback executed as pasted; a drift block on a shared tag that names the mover; dry-run does not pull; the version gate disables the rung. A positive control (docker events reports the pull) backs every "no pull happened" assertion. The tests of items 11 to 14 are listed there. M4 is done.
11. §3.2, §4 (maintainer decision, 19 Oct 2026): **rung 3 requires rung 2.** `whalenurse.heal.pull=true` with no `recreate` in `whalenurse.actions` gives one warning (the label warning, once per container: "needs recreate in whalenurse.actions") and the pull rung stays off; the ladder is whatever `actions` says. With `recreate` present (the default) the label appends the pull as before.
12. §3.6 check 2, §3.7 (maintainer decision): **rung 2 lets an image-only drift through for a container with `heal.pull=true`** (the same guard mode as before a pull: `GuardInput.AllowImageMove`; config drift, an oracle `Recreate` that `config --hash` cannot attribute to the image alone, and "can't verify" still block). A recreate that ends on a different image than the container was created from (`.Image` of the container before and after, the same field the rollback tags) is **"upgraded during heal"**: a new domain event `Upgraded`, notified with the same message as `TagMoved` and the rollback (tag the old image ID, then up), and it says the tag was moved by someone else. Without the label a moved tag blocks and SUSPENDS as in v3.6.
13. §3.5, §3.7 (maintainer decision): **the dependents step may take a tag that its parent's own pull moved in this incident, and only that**: for a stale sidecar the drift guard runs in the same mode only if the reference still resolves to what that pull produced, the pull was made by a service the sidecar's `depends_on` names, after that service's incident opened (and the incident is still open), and the sidecar was created from the image that pull replaced. Anything else is image drift as before. The sidecar's rollback is **in the same upgrade message**: the sidecars that run the same image reference and were created from the replaced image are found when the pull starts (also when they are already stale), and the one `up` line names the parent and them (`... up -d --no-deps --pull never app side`; the tag line is the same for all). A sidecar's own recreate says nothing of its own.
14. §3.7 (maintainer decision): **the pull rung respects `pull_policy: never` and `build`.** Before the guard, whalenurse reads the service's policy with `config --format json --no-env-resolution <svc>` (a new typed compose method; only `services.<svc>.pull_policy` is read, the rest, which can hold interpolated secrets, is dropped and never logged). For `never` or `build`, or if the policy cannot be read (fail closed), rung 3 does not run, no rate-limit slot is spent, a `RungUnavailable` event records it, and, the pull being the last rung, the container is SUSPENDED with the cause **"pull rung does not apply (pull_policy never)"** (or "cannot read the service's pull_policy (...)"), which is the one giving-up message. A dry run reads the policy too. `--policy always` replaces `missing`, `if_not_present`, the refresh windows and no policy. The CI matrix pins this with a real compose: a service with `pull_policy: never` is not pulled; a service with `pull_policy: missing` on a tag other than latest (which a bare `pull` skips in v5.2.0 and v5.6.0) is.
15. §3.12, §5 (maintainer decision): **720 runes for the upgrade message is approved**; with sidecars the commands are still kept whole (the parent and sidecars are one `up` line). **Private registries stay unsupported** for rung 3 and are documented as such; an optional read-only `DOCKER_CONFIG` mount is a candidate for M6.

**Changes in v3.7** (from building M3; evidence in [m3-notes.md](m3-notes.md)):
1. §3.12 (new): the notification rules. Shoutrrr is `github.com/nicholas-fedor/shoutrrr` v0.22.0 (vetted, see §5). `internal/notify` is one more subscriber of the domain event stream. It sends **only** these events: `ActionResult` (action taken), `Recovered`, `Suspended` (giving up, or flapping), `Unsuspended`, `DriftBlocked`, `DriftWarned`, `NotifyOnly`, `NeedsRecreate`, `RateLimited`, `RungsDisabled` (the version-gate warning), `Paused` and `Resumed` (the last two of those, `Unsuspended` and `RateLimited`, are items 13 and 14 below). Everything else stays in the log and the store.
2. §3.12: delivery never touches healing. The subscriber only queues (the engine publishes under its lock); a dispatcher renders and deduplicates; one worker and one Shoutrrr sender per URL deliver with a timeout and two retries. A failure is logged (redacted) and counted, never published back onto the bus. A panic in a Shoutrrr service is a failed delivery too: Shoutrrr's router runs each service in a goroutine with no `recover` (v0.22.0), which would kill the daemon, so whalenurse locates the service through the router but calls it itself, under a `recover`. On a stop the queue is flushed for up to 5 s.
3. §3.12, **a hard rule on secrets:** the notification URLs (and the uptime-ping URL) are tokens. They are never logged, never in `status`, `/healthz` or an error: a URL is known by its position and scheme (`#2 discord`), and every error text goes through a redactor that masks the URL, its user info, host, path segments and query values (and their escaped forms). Tests grep the logs, the status (text and JSON), `/healthz`, and the output of `notify test` for tokens placed in the user info, the path and the query.
4. §3.12, dedupe: one message per (event type, container key, discriminator, **incident**). Events now carry an `incident` attribute (the opening time of the incident the key has open; `engine.AttrIncident`), so a restart of whalenurse does not mix incidents up. Events with no incident (`NotifyOnly`, `NeedsRecreate`, the version-gate warning) are deduplicated for 24 h. `Paused` and `Resumed` are commands and never deduplicated. `DriftBlocked` and its `Suspended(drift)` are one message. The dedupe is **persisted** in the store (item 15).
5. §3.12, rate limit: `WHALENURSE_NOTIFY_RATE_LIMIT` messages per hour **per URL** (default 20). Routine messages may use half of it; the "important" ones (giving up, flapping, drift blocked, `NotifyOnly`, `NeedsRecreate`, rungs disabled) may use all of it, so a storm of "restarted" cannot swallow "giving up". The messages held back are counted and said in the next message that goes out ("N earlier messages were held back"), never dropped silently.
6. §3.12, message shape: the title is `host: container headline`, the body starts with the container key (or `host <id>` for a host-wide event), then the cause and the next step. Limits: title 72 runes, body 360 runes (the "held back" note included: the details are cut to make room); a long container name is cut, never the headline or the key. Drift messages carry the guard's reason and a hint (the compose-upgrade wording is a hint, not a block, as in v3.6 item 12).
7. §3.12, **dry-run sends notifications**, marked `[dry-run]` in the title and in the first line of the body. `whalenurse notify test` sends one message to every URL, one attempt each (no dedupe, no rate limit), and prints one line per URL (`ok` or `FAILED` with the redacted reason); it needs no running daemon, reads the environment like the daemon (so `docker exec` works), and exits non-zero if any URL failed or none is set.
8. §3.8, §4: the uptime ping. `WHALENURSE_HEARTBEAT_URL` is pinged with a plain GET at once and then every `WHALENURSE_HEARTBEAT_INTERVAL` (default 5m), **only while `/healthz` would say healthy**; when it would not, the ping is skipped so the monitor alarms by the missing ping. (Called "uptime ping" in code and logs, because "heartbeat" already names the persisted `last_seen` of §3.3 (d).)
9. §4: new variables `WHALENURSE_NOTIFY_TIMEOUT` (10 s per attempt), `WHALENURSE_NOTIFY_RATE_LIMIT` (20), `WHALENURSE_HEARTBEAT_INTERVAL` (5m). `WHALENURSE_NOTIFY_URLS` is split on whitespace or a comma that is followed by `scheme://`, so a comma inside a URL (ntfy `tags=a,b`, Telegram `chats=a,b`) stays in it. An invalid URL is logged (redacted), shown in `status` and skipped; it does not stop whalenurse.
10. §7 M3: "upgraded during heal (with rollback command)" belongs to the pull rung and moves to **M4** with the `TagMoved` event, because nothing upgrades during a heal before M4. The M3 template list is the one in item 1.
11. §5: the binary grows from about 19.6 MB to 28.0 MB (`CGO_ENABLED=0`, `-trimpath`), because Shoutrrr's root package registers every service (MQTT, XMPP, websocket and others). Accepted for v1; the alternative is a fork or a vendored subset.
12. §6: unit tests for templates (every type, worst-case length, host and key always present), dedupe, rate limit, redaction and the token grep; the M3 integration tests as built.
13. §3.12 (maintainer decision, 19 Oct 2026): **`Unsuspended` is notified, as a routine message** ("unsuspended" with the reason: back to healthy, a `create`, a start after a manual stop, or the `unsuspend` command). `Recovered` still follows `RECOVERY_PERIOD` later. `Suspended` and `Unsuspended` alternate within one incident, so each sent message makes the other one news again (otherwise a second "giving up" in the same incident would be swallowed, and the last word the user heard would be wrong). An `unsuspend` by command is never deduplicated.
14. §3.12 (maintainer decision): **`RateLimited` (whalenurse's own action budget is spent) is notified**, as an important message, **at most once an hour per URL**, whichever container hit it ("action rate limit reached, healing waits": it is host-wide). The spacing is kept per URL in memory, and a skipped message spends none of the URL's own budget. After a restart of whalenurse the next one may come sooner.
15. §3.10, §3.12 (maintainer decision): **the dedupe is persisted**: schema 2 adds the table `notified` (message identity, event type, key, time), read when the notifier starts and written as messages are accepted, pruned hourly beyond 24 h. A restart no longer repeats a `NotifyOnly` or an incident's messages. A dry run stores nothing, so its dedupe is in memory. A schema-1 database is migrated in place. An unreadable ledger is logged and the dedupe starts empty.

**Changes in v3.6** (from building M2b; evidence in [m2b-notes.md](m2b-notes.md); the integration tests ran on compose v5.2.0, v5.6.0 and the latest release, on both image stores):
1. §3.4: the fixed paths are `/host/docker-compose` (the host plugin, mounted read-only) and `/usr/libexec/whalenurse/docker-compose` (the bundled copy). *Bundled* is decided by the path that matched, never by comparing versions, so the version-skew check cannot be fooled. Under systemd the host's plugin directories are searched last. An explicit `WHALENURSE_COMPOSE_BIN` that does not work is an error, not a reason to use another binary. The `compose` package exposes typed methods only (no `down` can be expressed), runs compose from `/` with `COMPOSE_*`, `DOCKER_CONTEXT`, `DOCKER_CONFIG` and `WHALENURSE_*` removed from its environment, and a recreate whose inputs are missing or unreadable is skipped with a message that names the identical-path read-only mount.
2. §3.6 check 1: compose prints the `--progress json` lines on **stderr**, so the oracle is read from both streams. Output with no JSON (an old compose prints `null`), with no line about the container, or a dry-run that fails, all mean *fall back to `config --hash`* and never *no drift*. If the hash fails too, the finding is `unverified`, which blocks like drift. v5.2.0, v5.6.0 and the latest release all print what E5 recorded.
3. **§3.6 check 2, a change to a v3.3 rule (approved 18 Oct 2026).** The label `com.docker.compose.image` is the platform manifest digest on the containerd store only from compose v5.5.0 (the release that "overhauled image digest reconciliation"). **Compose v5.2.0 on the containerd store writes the image (index) ID there**, which is what a plain `docker image inspect <ref>` `.Id` returns. With the v3.3 comparator alone (`--platform linux/<arch>` `.Id`), every container created by compose before v5.5.0 on the containerd store looked drifted: the 5.2.0 × containerd leg of the CI matrix blocked every recreate, and the classic store and every release from v5.6.0 were unaffected. The comparison is now: equal to the platform ID **or** to the plain ID of the reference. A moved tag changes both, so no drift is hidden; the v3.3 warning against using plain `.Id` *alone* stands. Without this, the guard fails closed and reports the container as drifted ("the tag moved") although it did not. The comparison goes through the Engine API (`ImageInspect`, optionally with `ImageInspectWithPlatform`), not the CLI: the image has no `docker` binary.
4. §3.5: dependents are incidents of their own and go through the ladder machinery: a dependent whose parent ID is gone (class `stale-parent-gone`, or a failed start with `No such container`) is recreated with the non-forced `up`, its ladder starting at rung 2 (a restart would hurt it); one whose parent restarted later (same ID) is restarted through the API. Whalenurse's own action on a parent schedules its dependents for when the parent is looked at again, because the parent's own events fall in its in-flight window. A stale dependent waits while a service it depends on (`com.docker.compose.depends_on`) is not running or is being healed. They go one at a time, each up to 90 s to come back (running, healthy if it has a healthcheck), as scheduled steps, not sleeps. Checks 2 and 3 of the drift guard apply to them before the `up`. A rate-limit slot is spent for each. The `restart: "no"` parent left down by the host gets one `NotifyOnly` event that names its dependents; they emit nothing.
5. §3.6, *Minimum versions per rung*: the gate covers **every** use of compose (rung 2, the dependents recreate, the rule-1 stale-reference and network-gone recreates, the drift guard) and is checked again after the events stream comes back. A recreate-shaped decision that cannot run (gate off, not a compose container, unreadable inputs, `recreate` not in `whalenurse.actions`) is advice only (`NeedsRecreate`, no incident), as in M2a; an ordinary unhealthy container records `RungUnavailable` and ends at SUSPENDED after rung 1.
6. §3.2: a recreate skipped by the drift guard SUSPENDS the container at once with reason `drift` (the pull rung would be stopped by the same findings); it spends no rate-limit slot.
7. §3.3 rule 1: a failed start whose cause is a network that is gone goes straight to rung 2 (the restart rung is skipped); not reproduced.
8. §6, §7: the integration list and the CI matrix are as built; M2b is done.
9. §3.5, §3.8 (maintainer decision, 18 Oct 2026): **the dependents step repairs the dependents of an opted-in parent even when they are not opted in themselves**, unless they are labelled `whalenurse.enable=false` (`config.Container.Disabled`). Only the dependents step does: a recreate of a dependent whose parent ID is gone, or a restart of one made stale by its parent's restart (also when its healthcheck fails because of it). A sidecar's own ladder (unhealthy on its own, exited, crash-looping) still needs its own opt-in. The parent is found by ID, or, when the ID is gone, through the services named in the sidecar's `depends_on` label; the poll looks at non-opted-in containers that hold a `container:` reference for that reason.
10. §3.2 (maintainer decision): a dependent repaired by the dependents step **does not count toward its own flap limit**; it belongs to its parent's incident. (An incident still opens for it, and its ladder, rate limit, drift guard and SUSPENDED apply.) Its own failures count as before.
11. §3.6 *Strictness* (maintainer decision): `warn` overrides **verified drift only** (a `config` or `image` finding). A finding that says "can't verify" (a check that could not be made, or the bundled compose's version skew) stays blocking in `warn`, and the message says so.
12. §3.6 check 3 (maintainer decision): version skew is also **reported for the host plugin, as information in the drift message** ("compose upgraded from X to Y since this container was created, so this may be a changed hash layout rather than an edit"), not as an extra block. A drift finding of the host plugin is still verified drift, so `warn` overrides it. The bundled copy keeps the blocking "can't verify drift" finding.

**Changes in v3.5** (from the M2a review of PR #8; [m2a-notes.md](m2a-notes.md) open questions 1 and 2):
- §3.3 *Manual-stop markers*: a fourth mechanism, **(d) the heartbeat**. whalenurse is itself one of the containers a daemon shutdown kills, so its events stream ends by its own SIGTERM and (a) never sees a loss, while the boot id (b) is unchanged: the persisted shutdown-kill markers then let the restore's `start` clear SUSPENDED. Fix: persist `last_seen`; at startup treat the previous run's end as a stream loss (discard markers set in the 30 s before it), and open the restore window when the gap exceeds `POLL_INTERVAL`. The burst rule stays rejected. In M2a this was a known gap, pinned by a test that asserted the opposite; that test is now a normal one.
- §3.3 rule 3: a crash loop also needs the **latest** `RestartCount` increase to be less than `CRASHLOOP_AFTER` ago. Under the v3.4 text three quick restarts followed by a long quiet were a loop from `CRASHLOOP_AFTER` after the first until `RECOVERY_PERIOD` after the last, and a stable container could be restarted and SUSPENDED.
- §3.3 rule 4: **a container stopped by a host shutdown is judged by its restart policy, whatever its exit code.** The v3.3 text assumed exit 137 or 143. The first M2a run on the throwaway VM showed an app that exits 1 on SIGTERM and has `restart: "no"` being started by whalenurse after the daemon restart (no marker, non-zero exit: "crashed"). *Stopped by the host* is now defined by `FinishedAt` falling inside a shutdown window (30 s before `last_seen` or the stream loss, up to the restore); `no` stays down with one notification, `always`/`unless-stopped`/`on-failure` are left to Docker's restore, `heal-exited=always` starts it.
- §3.2 *Persisted*: the heartbeat is persisted. §6: unit tests for all three.

**Changes in v3.4** (a process change; no design rule changes):
- §7: milestone M2 is split in two, because it bundled two different kinds of risk. Every M2 bullet is kept, each in exactly one of the two.
  - **M2a** (state and control): CI on Docker Engine 29.x, the SQLite store, the full ladder with the crash-loop rule, the CLI over the control socket, the manual-stop markers, and shutdown handling. Rung 2 does not exist yet: where the ladder would recreate, it records "needs recreate (M2b)", as M1 does, and never restarts a dependent whose parent ID is gone.
  - **M2b** (compose): `internal/compose`, the recreate rung, the drift guard with the startup version gate, dependents, and the rule-1 third-shape / image-drift-comparator bullet. The last bullet mixes rules work with the comparator; it stays whole in M2b, and the third shape is already pinned by the M0b fixtures (`testdata/inspect/m0b-daemon-restart-*`).
- §0 order: H, M0, M1, M0b, **M2a, M2b**, M3 … M6.
- §6: the integration list and the CI matrix say which milestone owns each item. M2a drives no compose binary (it only sets up test stacks), so it runs compose v5.6.0 on both image stores. The full compose matrix (v5.2.0, v5.6.0, latest) belongs to **M2b**, where whalenurse runs compose and the dry-run oracle's output has to be pinned across versions.
- §3.2, §3.3, §4, §9: references to "M2" now name M2a (persistence, the crash-loop rule, the CLI, SQLite).

**Changes in v3.3** (from the M0b review of PR #6; findings references are to [m0b-findings.md](m0b-findings.md) §4):
- §3.3 *Manual-stop markers*: kills seen during a daemon shutdown or reboot must not set manual-stop markers or clear SUSPENDED. Three mechanisms are adopted: (a) quarantine on stream loss, (b) persist the boot id, (c) a restore window after reconnecting to a daemon that was down. A fourth, recognising a burst of `kill[15]`, is **rejected**: `docker compose stop` and `down` send the same burst, so deliberate stops would be healed. The reason is recorded in the section.
- §3.3 rule 4 and §3.5: a `restart: "no"` container stopped by a host shutdown respects its policy. It and its `network_mode` dependents stay down and whalenurse notifies once; only `heal-exited=always` starts them.
- §3.3 rule 1: the third failed-start shape, a failed boot-time restore (`exited`, exit 126/127/128 mapped from the error, `RestartCount` unchanged). The port-conflict matcher accepts `0.0.0.0:` and `:::` (and `[::]:`). `failed to start container` is confirmed at runtime for boot restore, and the journal lines of E8 are confirmed.
- §3.3: the Docker API is unavailable while the daemon restores containers, and `since=` replay holds only the new daemon's last 256 events and never the old daemon's shutdown, so the startup sweep is mandatory after every reconnect that follows a lost stream, not only at process start.
- §3.5: the boot-time wait for `container:` parents is confirmed (dependents come up after their parent, also behind a parent whose start takes longer than 5s); a parent that is not restored takes its dependents down.
- §3.6 check 2: image drift compares the `com.docker.compose.image` label with `docker image inspect --platform linux/<arch> --format '{{.Id}}' <ref>`, on both image stores. `.Descriptor.digest` must never be used: it is empty on the classic store.
- §6: unit tests for the above; the CI image-drift job runs on both stores.
- §3.6, §3.2, §5, README requirements: **minimum versions per rung.** Rung 1 works from Engine v25; rungs 2 and 3 and the drift guard require Engine ≥ 28.1 and Compose ≥ v5.2.0. whalenurse checks at startup, and below that it logs one warning, disables rungs 2 and 3, and the ladder ends with notify + SUSPENDED after rung 1. No store-guessing fallback (answers §9 question 8, found by the CI `image-drift` job).
- §7: M0b is done; M2 carries the items above.

**Changes in v3.2** (from the M1 review of PR #5):
- §3.2: SUSPENDED clears on recovery, on an external `create`, or on a `start` that follows a manual-stop marker. A restart-policy `start` never clears it, because it would re-arm the ladder on every cycle of a loop.
- §3.3 rule 3: a crash loop is signalled by `RestartCount` rises and `die` events, not by `restarting`. The restart manager resets its backoff after a 10s run (moby `restartmanager.go` at docker-v29.9.1), so a loop that crashes every ~15s is almost always `running`. M2 defines the rule independent of how often whalenurse inspects.
- §7 M2: the ladder bullet carries the new loop rule and the recovery period for it.
- M1 keeps its simpler implementation (see [m1-notes.md](m1-notes.md) decision 11); M2 replaces it.

**Changes in v3.1** (E-numbers are the M0 experiments in [m0-findings.md](m0-findings.md) §3):
- §3.3: the event-order example is `kill`, `stop`, `die`, and it differs between operations and Engine versions (E9).
- §3.3 rule 1: a failed first start (`created` + 126/127/128) and a failed restart-policy restart (`exited` + the previous run's exit code, `RestartCount` incremented) are different cases. Rule 1 tests `State.Error` in both and never uses the exit code (E8, E8c, E8d).
- §3.3 rule 1: a failed restart emits no event, so the poll owns detecting it and re-inspects containers still `restarting` after GRACE (E8, E9).
- §3.3 rule 1: the stale-reference matching is split by cause. `No such container` (parent recreated) → recreate the dependent. `cannot join network namespace of a non running container` (parent stopped) → heal the parent first, then restart the dependent. The "other" strings are added (E8e, E8c, E8d, E8).
- §3.3: the exit-code table is corrected, and the scrubber wording describes what `scripts/scrub-fixture` does (E8; findings §4 item 7).
- §3.5: remedy mapping by staleness condition. A dependent whose parent ID is gone is recreated with the non-forced `up`, and never API-restarted (E1, E2). `--no-deps` is confirmed to suppress `depends_on.restart` (E3).
- §3.5: compose #13878 is closed, fixed by compose PR #13880, and absent in v5.6.0 (E2a; findings §1).
- §3.6: the `--dry-run --progress json` oracle is the primary check for non-reference services and parents. `config --hash` is the fallback, and the skip rule stays for all four reference types (E4, E5).
- §3.6: on the containerd store, image drift compares `com.docker.compose.image` with `docker image inspect --platform linux/<arch>` `.Descriptor.digest`, never `.Id` (E5). The classic store is to be verified in M0b and CI.
- §3.9: the base image is `gcr.io/distroless/static:nonroot` with `group_add` for the socket's group (E7).
- §3.7: drift checks 1 and 3 run before the pull; after it, only the digest change is checked (E5: the oracle reports `Recreate` once the image has changed).
- §6: the CI compose matrix is v5.6.0, the latest release and v5.2.0, the only release with #13878 (findings §1). It runs on both image stores.
- §7: new milestone M0b between M1 and M2, for the scenarios M0 could not run (findings §6). M1 never applies rung 1 to a dependent whose parent ID is gone (E1).
- §9: questions 2–5 are answered (E6, E7, E5, E2). Question 1 stays open.
- Consistency edits with no new decisions: §0 (milestone order), §3.4 (`:ro` confirmed by E6), §6 (unit-test list), §7 M2 (wording).

## 0. Instructions for Claude Code

- **One milestone at a time, in order:** H, M0, M1, M0b, M2a, M2b, M3 … M6, then M7 (the first post-v1 milestone, v3.12). At the end of each milestone, stop and report:
  - what you built;
  - what you verified, and how;
  - what is still uncertain;
  - the **[maintainer]** items that are still open.
  Wait for review before you start the next milestone.
- **[maintainer]** marks work only the maintainer can do: GitHub settings, and anything on the real Docker host. Don't attempt these items. List them in your milestone report.
- **Check sources first.** This plan dates from 16 Oct 2026, so some facts and versions may have changed. Before you rely on a Docker or Compose behavior, or add a dependency, check the current upstream source, docs or releases page. Use the latest stable Go and the latest release of each dependency. Record anything that differs from this plan.
- **Stop on conflicts.** If an M0 finding or a test contradicts a rule in this plan, report it. Don't quietly change the design.
- **Safety on any Docker host:**
  - Only act on containers in compose projects named `dokdosta-test-*` or `dokdosta-m0-*`. Never stop, restart, recreate, pull, prune or remove anything else.
  - Integration tests and M0 scripts must enforce this in code (a project-name filter that fails closed). Convention alone is not enough.
  - Don't run M0 experiments until the maintainer confirms which host to use.
- **Fixtures:** never commit raw `docker inspect`, `docker events` or `journalctl` output. Raw captures go in `/captures/`, which is gitignored. Fixtures in `testdata/` must be scrubbed first (§3.3).
- **Build rules:** use `CGO_ENABLED=0` (single static binary). `internal/engine` stays pure: no Docker calls, and the clock is injected.

## 1. Problem and positioning

Containers sometimes go `unhealthy`, or stop and stay stopped despite their restart policy. Today the fix is to SSH in and run `docker compose pull && down && up -d`.

Docker Engine does not restart unhealthy containers itself. Restart policies only act when the main process exits.

Prior art (checked 16 Oct 2026):

| Project | What it does | What it lacks |
|---|---|---|
| `willfarrell/autoheal` | Restarts unhealthy containers; alerts via `WEBHOOK_URL` | No recreate; not compose-aware |
| `qdm12/deunhealth` | Event-driven restart of labelled containers | Restart-triggers-other-restarts is still on its TODO list |
| `satya-sovan/docker-autoheal` | Python + React; dashboard, maintenance mode, persistent `/data` state, webhook alerts | Restart only; uses compose labels only as a stable ID; UI has no built-in auth |
| Watchtower | `containrrr/watchtower` was archived on 17 Dec 2025; `nicholas-fedor/watchtower` is the maintained fork (v1.22.3, 22 Sep 2026) | Updates containers; does not heal |

**Differentiator** (lead with this in the README and docs): dokdosta recreates from the stack's own compose files, handles `network_mode` sidecar dependents, and has a drift guard. Dashboard, pause and notifications are table stakes now.

## 2. Decisions

| Topic | Decision |
|---|---|
| Audience | Homelab first. Public repo from the start; announce once stable |
| Stack layout | Plain compose directories on the host |
| Failure modes | Unhealthy, and stopped-unexpectedly. Hosts already use `restart:` + `healthcheck:` |
| v1 scope | **Healing only**, plus the opt-in pull rung. Scheduled updates are post-v1 (R4) |
| Language / delivery | Go, single static binary; runs as a container (socket mounted) or under systemd |
| Detection | Docker events stream, plus a safety poll, plus a startup sweep |
| Selection | Opt-in per container via label; optional global `all` mode |
| Ladder | **One pass per incident** (restart → recreate → optional pull+recreate), then SUSPENDED. A **flap limit** suspends containers with repeated incidents |
| Compose binary (container mode) | **Host's compose plugin mounted `:ro` first; bundled copy as fallback** |
| Docker Go client | `github.com/moby/moby/client` + `github.com/moby/moby/api` (`github.com/docker/docker` no longer gets updates as of Engine v29) |
| Notifications | Shoutrrr, `nicholas-fedor/shoutrrr` v0.22.0 (vetted in M3, v3.7; §3.12) |
| Store | SQLite via `modernc.org/sqlite` (recommended in review; confirmed in M2a, v1.60.1) |
| Dashboard | Read-only, off by default, built on a versioned JSON API; default port 8484 |
| Control in v1 | CLI over a local unix socket (pause/resume/unsuspend/status). No network-exposed write endpoints |
| Multi-server | Post-v1 hub (R1). v1 already carries a host ID in every key, event and API record |
| Repo name | Lowercase `dokdosta` (rename is a **[maintainer]** task in H) |

## 3. Design

### 3.1 Heal never upgrades by accident
- Healing never pulls and never builds. The one exception is the opt-in pull rung (§3.7).
- This is enforced in three places: `--pull never --no-build` on every compose `up`, the image-drift check (§3.6), and integration tests.

### 3.2 Incidents, the ladder and the flap limit (per container)

```
healthy --(trigger)--> wait GRACE --> inspect + rules (3.3) --> incident opens
  rung 1: restart (Docker API)               [+ dependents step, 3.5]
  rung 2: recreate (compose)                 [+ dependents step]
  rung 3: pull; recreate only if newer image [opt-in, 3.7]
  after each rung: wait cooldown (grows per rung) -> re-inspect -> stop if recovering
  all rungs failed -> notify "giving up" -> SUSPENDED
recovered for RECOVERY_PERIOD (default 10m) -> incident closes, notify "recovered"
more than FLAP_LIMIT incidents (default 3) within FLAP_WINDOW (default 24h)
  -> SUSPENDED ("flapping"), even if the last incident recovered
any state --(pause)--> PAUSED (no actions; events recorded) --(resume / expiry)--> re-evaluate
```

- **Rungs:** set per container with `dokdosta.actions` (default `restart,recreate`). Rung 3 is appended only with `dokdosta.heal.pull=true`, and only if `recreate` is in the ladder (v3.8 item 11). Each rung runs once per incident. The one exception is the dependents step: a sidecar made stale again by a parent that changed since its last repair gets a fresh incident (v3.13 item 1). Rungs 2 and 3 are available only with Engine ≥ 28.1 and Compose ≥ v5.2.0; otherwise they are disabled at startup and the ladder ends after rung 1 (§3.6, *Minimum versions per rung*).
- **Recovered means:**
  - with a healthcheck: health is `healthy`;
  - without one: the container is running and `RestartCount` has not changed for `RECOVERY_PERIOD`.
- **Flap count:** closing an incident does not reset it. Only the window expiring does.
- **State key:** `host/project/service/container-number`, taken from the `com.docker.compose.project`, `.service` and `.container-number` labels. Never the container ID, because a recreate changes it.
  - `host` comes from `DOKDOSTA_HOST_ID` (default: the hostname). At startup, persisted keys with another host segment are re-keyed to the current one (v3.13 item 12).
  - Non-compose containers fall back to the container name and only get rung 1. So does a container whose compose identity labels come from its image (v3.13 item 10).
- **Persisted from M2a:** incident state, rung reached, SUSPENDED/PAUSED (with reason and expiry), flap history, manual-stop markers and the `last_seen` heartbeat.
- **Action-in-flight window:** while dokdosta is acting on a key, events for that key are *recorded and attributed to the action*, not interpreted. They must not:
  - trigger the manual-stop heuristic;
  - clear SUSPENDED;
  - open a new incident.
- **SUSPENDED clears** when:
  - the container returns to healthy;
  - an external `create` arrives outside any in-flight window;
  - a `start` arrives outside any in-flight window **and follows a manual-stop marker** (someone stopped it, someone started it). A `start` by the restart policy never clears SUSPENDED: it is also a `start` event, and clearing on it would re-arm the ladder on every cycle of a crash loop. Neither does a `start` inside a restore window, nor one that follows a quarantined shutdown kill (§3.3, *Manual-stop markers*): the restore of a daemon restart or reboot would otherwise unsuspend every container;
  - someone runs `dokdosta unsuspend`.
- **Pause:**
  - Syntax: `dokdosta pause <project>[/<service>] [--for 2h]` and `dokdosta resume ...`, run via `docker exec`. The CLI talks to the daemon over a unix socket.
  - Default duration is 2h; `--for 0` means indefinite. Pauses expire so a forgotten pause doesn't disable healing for good.
- **Global rate limit:** `DOKDOSTA_RATE_LIMIT` actions per hour (default 10). This prevents action storms during host-wide outages.

### 3.3 Decision rules (inspect after the grace period)

**Events are only triggers.** The decision is made by inspecting the container after `GRACE`, because event order is unreliable. For example, `docker stop` emits `kill` → `stop` → `die` on Engine 29.9.0. The order differs between operations and Engine versions.

**Health events are edge-triggered.** Docker emits `health_status` only when the status changes (verified in `daemon/health.go`), so a missed event never repeats. The poll (default 60s) is the level-triggered backstop and applies the same rules.

**The daemon restarts containers after a restart or a reboot, and that has consequences for the observer** (M0b):
- **The Docker API is unavailable while the daemon restores containers** (9s after a daemon restart; about 48s after a reboot, of which 7s was one slow start). An API error or a closed events stream during that time means "daemon not ready": reconnect with backoff and open no incident.
- **`since=` replay is not a substitute for the sweep.** After a reconnect, `since=<last event time>` returns only the new daemon's buffered events (the restore's `start` events), the buffer holds 256 events, and the old daemon's shutdown events are never available. A `stop` or `die` may never be emitted at all: three of twelve containers were force-killed at the end of a 15s shutdown and had neither. So **the startup sweep is mandatory after every reconnect that follows a lost stream**, not only at process start.

**Fields to inspect:** `State.Status`, `State.Running`, `State.Health`, `State.ExitCode`, `State.OOMKilled`, `State.Error`, `RestartCount`, `HostConfig.RestartPolicy`, `HostConfig.NetworkMode`, and labels.

**Rule precedence:**

0. **Skip entirely:**
   - dokdosta's own container;
   - a container with any `whalenurse.*` label, the namespace from before the rename, whatever else it carries (v3.15 item 5; read like `dokdosta.*` under v3.13 item 10, the reason names the keys);
   - `com.docker.compose.oneoff=True` (`compose run` containers carry the same project and service labels);
   - swarm tasks (`com.docker.swarm.task.id` present);
   - containers that are not opted in;
   - PAUSED or SUSPENDED keys.
1. **Non-empty `State.Error` means a failed start.** This rule overrides everything below. Test `State.Error != ""` on both `created` and `exited` containers, and never use the exit code to decide.
   - This is hypothesis A, confirmed in moby source and reproduced on Engine 29.9.0 (E8):
     - A start that fails during a restart-policy restart is **not retried** (`daemon/monitor.go`). Docker logs `failed to restart container` at debug level and `restartmanger wait error` at error level (the misspelling is in the source).
     - A start that fails during boot-time restore is not retried either; Docker logs `failed to start container` at error level, once per container (`daemon.go`). Confirmed in M0b after a daemon restart and a reboot (4 containers each, no retry over 183s). The E8 line `restartmanger wait error` is confirmed in the journal; `failed to restart container` is `debug` level and is not expected there.
   - There are three shapes:
     - **Failed first start** (`docker start`, `compose up`): status `created`, `RestartCount` 0, and an exit code of **126** (EACCES), **127** (command not found / ENOTDIR) or **128** (anything else), with `State.Error` set (E8c, E8d). There is no `die` event; the container never ran.
     - **Failed restart by the restart policy:** status `exited`, `State.Error` set, `RestartCount` incremented, and the exit code is the **previous run's** (for example 1), not 126–128 (E8).
     - **Failed boot-time restore** (daemon restart or reboot): status `exited`, `State.Error` set, exit code **126/127/128 mapped from the error** (127 for a bind source that is gone), `RestartCount` **unchanged** (0), `StartedAt` of the previous run and `FinishedAt` of the shutdown. It emits no `die`. The dependents of a parent that fails this way fail with `cannot join network namespace of a non running container` (M0b).
   - **A failed restart emits no event** (no `die`, no `start`). The only event is the `die` of the earlier run. So the poll owns this detection: every poll applies rule 1, and it re-inspects containers still `restarting` after GRACE, because a retry that fails later is silent (E8, E9).
   - Split by cause. Do the matching in one place, backed by fixtures, because error strings vary by Engine version:
     - **Stale reference:**
       - `No such container` (`joining network namespace of container: No such container: <old id>`: the parent was recreated or removed) → recreate the dependent (skip restart; §3.5 gives the command). Restarting it cannot work (E1, E8e).
       - `cannot join network namespace of a non running container` (the parent is stopped, not removed) → recreating the dependent cannot help until the parent runs. Heal the parent first (§3.5 already suppresses the dependent's ladder while the parent has an incident), then restart the dependent (E8e). **If the parent is left down by its policy** (a `restart: "no"` parent stopped by a host shutdown; rule 4), nothing heals it, so the dependent stays down too: no ladder, and one notification (§3.5).
       - `network ... not found` → recreate. Not reproduced in M0; kept from v3.
     - **Other** (missing bind-mount source, port already allocated, NFS or device not ready, command not executable): start first, then recreate. Strings seen in M0:
       - `failed to fulfil mount request: open <path>: no such file or directory` (bind source gone at restart; E8)
       - `invalid mount config for type "bind": bind source path does not exist: <path>` (at create, with `create_host_path: false`; also seen in the E6 negative control)
       - `Bind for <addr> failed: port is already allocated` (E8d). `<addr>` is `0.0.0.0:<port>` or `:::<port>` (seen on the classic-store run) and may be `[::]:<port>`; match on the address-independent part, `port is already allocated`.
       - `exec: "<cmd>": stat <cmd>: no such file or directory` (E8c)
       - `exec: "<cmd>": permission denied` (E8c)
       - `stat <cmd>: not a directory` (E8c). Docker adds a misleading hint about mounting a directory onto a file; ignore it.
2. `running` + `unhealthy` → ladder.
3. A **crash loop**, or `on-failure:N` exhausted → ladder. (A container still `restarting` after GRACE is re-inspected on each poll; rule 1 applies when it fails.) A container that was `created` but never started → the ladder (start at rung 1, then recreate), unless compose is holding it back on a `depends_on` condition that does not hold yet: then it is held, with no incident (v3.13 item 14).
   - **Crash loop:** at least 3 `RestartCount` increases, the first at least `CRASHLOOP_AFTER` (default 5m) ago **and the latest less than `CRASHLOOP_AFTER` ago** (three quick restarts followed by a long quiet are not a loop). `RestartCount` and `die` events are the signal, not the `restarting` status: the engine's restart manager resets its backoff after a run of 10s or more, so a container that crashes every ~15s is almost always `running` when inspected, and a rule that needs `restarting` never fires. The rule must give the same answer however often dokdosta inspects, so it counts increases between inspects rather than comparing two snapshots. The loop clears after `RECOVERY_PERIOD` (default 10m) without an increase.
   - M1 ships a simpler form: the clock starts on `restarting` or a rising `RestartCount` and clears after GRACE of a steady count ([m1-notes.md](m1-notes.md) decision 11). M2a replaces it with the rule above.
4. `exited` with no `State.Error` → apply `dokdosta.heal-exited` (`crashed` | `always` | `never`; default `crashed`):
   - **Intended exit:** with restart policy `no` or `on-failure` and exit code 0, the exit was intended. Leave it alone in `crashed` and `never` modes.
   - **`crashed`:** heal if dokdosta saw a `die` **without a preceding `kill`**, or `OOMKilled=true` (also watch the `oom` event). If dokdosta missed the events (it was down):
     - heal on a non-zero exit code other than 137/143;
     - 137 with `OOMKilled=true` is a crash, so heal;
     - 137 or 143 without OOM: notify only, don't heal.
   - **`always`:** heal any exited container.
   - **A container stopped by a host shutdown or reboot is judged by its restart policy, whatever its exit code** (v3.5). *Stopped by the host* means its `FinishedAt` falls inside a **shutdown window**: from 30 s before the previous run's `last_seen` (or before the events-stream loss) to the daemon's restore. The window is closed at the reconnect after a stream loss; for the startup case it ends when dokdosta started after a reboot (new boot id), and within one boot at most 5 minutes after `last_seen`, so a crash while dokdosta was down for hours is still a crash. Exit 137 and 143 are only the common cases: an app that handles SIGTERM may exit 1 or 0 (M0b `n-parent` exited 137; a test app that exits 1 on SIGTERM was started by dokdosta in the first M2a run on the VM, which is what this rule prevents). Then:
     - **`restart: "no"`** stays down. Docker does not restore it, and dokdosta does not either, in `crashed` and `never` modes: the shutdown is not a crash (see *Manual-stop markers*). One notification (`NotifyOnly`, deduplicated, not repeated every poll). Its `network_mode` dependents, which fail at boot with `cannot join network namespace of a non running container`, stay down with it: no ladder runs for them, and the single notification that names the parent and its dependents is M2b.
     - **`always`, `unless-stopped`, `on-failure`** are left to Docker's restore. If the restore failed, the container has `State.Error` and rule 1 handles it. Exception (v3.13 item 17): Docker does not restore an **`on-failure`** container that exited 0 or whose `MaxRetries` are spent, so it is treated like `restart: "no"` (stays down, one notification).
     - **`heal-exited=always`** starts the container (and then its dependents, the heal-the-parent-first path of rule 1).
     - Containers outside a window are judged as before (`crashed`: heal on a non-zero exit code other than 137/143, and so on).
   - **`never`:** notify only.

**Manual-stop markers:**
- A marker is set on a `kill` event outside any in-flight window, and cleared on the next `start` event. A `kill` with a reload signal (SIGHUP, SIGUSR1, SIGUSR2, SIGWINCH) sets none (v3.13 item 15). A sidecar without its own opt-in gets markers too (v3.13 item 2).
- **A daemon shutdown or reboot sends `kill` to every running container** (M0b: 12 of 12 within 20ms at a daemon restart, `kill[15]` then `kill[9]`; at a reboot the observer is cut off after `kill[15]`, before any `stop` or `die`). Taken literally, the rules above would mark every container as manually stopped, and the `start` events of the restore would then clear SUSPENDED on all of them (§3.2). **Kills seen during a daemon shutdown or reboot must not set manual-stop markers or clear SUSPENDED.** Four mechanisms, all required:
  - **(a) Quarantine on stream loss.** When the events stream ends for a reason other than dokdosta's own shutdown (EOF or an error; for dokdosta's own end see (d)), discard every marker created in the 30 s before the loss: the daemon's shutdown limit is 15 s, plus the stop timeouts. A real manual stop in that window is lost too. That container is then judged by rule 4 without evidence (137/143 without OOM: notify only), which is the safe outcome.
  - **(b) Persist the boot id.** Store `/proc/sys/kernel/random/boot_id` with the markers. If it differs at startup, drop the markers and treat the first `STARTUP_SETTLE` as a restore window. With the previous run's `last_seen` known, only the markers set at or after `last_seen` − 30 s are dropped and older ones are kept (v3.13 item 16); without it, all of them. A reboot stops dokdosta in the same second as the containers, so a marker can exist without any matching event in the next run.
  - **(c) Restore window.** After connecting to a daemon that was down, until `STARTUP_SETTLE` has passed, a `start` event is a restore: it does not clear SUSPENDED, and a `kill` in the window does not set a marker.
  - **(d) Heartbeat: dokdosta's own end.** In container mode a daemon shutdown kills dokdosta too. Its stream then ends by its own SIGTERM, which (a) excludes, and the boot id is unchanged, so (b) does not fire; the markers it saw and persisted would let the restore's `start` clear SUSPENDED. So persist `last_seen` (a heartbeat, written at most every 5 s, and on a clean stop). At startup treat the previous run's end as a stream loss: discard every marker set in the 30 s before `last_seen` (as in (a)), and if the gap between `last_seen` and now exceeds `POLL_INTERVAL`, open the restore window of (c) (the daemon may be restoring containers). The price is the same as (a): a real manual stop in the 30 s before dokdosta is itself stopped is forgotten, which is the safe outcome. In systemd mode this also covers systemd stopping dokdosta before Docker.
- **Rejected: recognising the shutdown by its burst of `kill[15]`** (two or more containers within a second, tagged as a shutdown). `docker compose stop` and `docker compose down` send the same burst, and so does stopping several containers by hand. A burst rule would label those deliberate stops as shutdowns, drop their markers, and heal the stopped containers. The stream loss in (a), the boot id in (b) and the heartbeat in (d) are the only reliable signals; do not add a burst heuristic.
- Containers that fail to start at boot have `State.Error`, so rule 1 wins. The restored containers' markers never matter again, because the restore starts them.
- A container stopped by hand before the shutdown (`unless-stopped`) stays down after the restore and keeps its marker; that case needs nothing.

**Startup sweep:** after dokdosta starts (including at host boot), and **after every reconnect that follows a lost events stream**, wait `STARTUP_SETTLE` (default 2m), then apply the rules to everything. Replay with `since=` does not replace it (see above).

**Exit-code reading guide** (for M0, docs and the dashboard):

| Code | Meaning |
|---|---|
| 0 | Clean exit |
| 126 / 127 / 128 + `State.Error`, status `created` | Failed **first** start (126 EACCES, 127 command not found / ENOTDIR, 128 anything else) |
| 126 / 127 / 128 + `State.Error`, status `exited`, `RestartCount` unchanged | Failed **boot-time restore** (the code is mapped from the error, not the previous run's) |
| any code + `State.Error`, status `exited` | Failed **restart** by the restart policy. The code is the previous run's, so don't read it; show `State.Error` |
| 137 | SIGKILL: OOM if `OOMKilled`, otherwise `docker kill` or a stop that hit its timeout |
| 143 | SIGTERM (usually a stop) |

**Fixtures:** every rule gets a unit test with scrubbed `docker inspect` fixtures. The scrubber (`scripts/scrub-fixture`):
- removes `Config.Env`;
- replaces label values except `com.docker.compose.*` and `dokdosta.*`; other label keys are kept;
- rewrites the path-valued compose labels (`working_dir`, `config_files`, `environment_file`) to `/srv/stacks/<project>[/<file>]`;
- replaces `Config.Cmd`, `Entrypoint`, `Args`, healthcheck test arguments and health-check log output with `<scrubbed>`, because a raw capture can hold secrets there;
- replaces hostnames, MACs and host mount sources with placeholders, and every IP except `0.0.0.0` and `127.0.0.1` with `192.0.2.99`. Those two stay so that strings like `Bind for 0.0.0.0:38080 failed` still match;
- keeps container IDs, so `container:<id>` references still match the parent;
- has an events mode (whitelisted attributes only, times rebased to 0) and a `--check` leak check.

### 3.4 Recreate via compose

**Compose binary lookup** (first match wins):
1. `DOKDOSTA_COMPOSE_BIN`
2. The host plugin, mounted read-only at `/host/docker-compose`. The host path is typically `/usr/libexec/docker/cli-plugins/docker-compose`; `docker info` shows it. The compose plugin runs standalone.
3. The bundled copy, at `/usr/libexec/dokdosta/docker-compose` (a different path on purpose: v3.6).
4. Under systemd, the host's plugin directories.

Log which binary and version is in use at startup, and expose it in `/api/v1/status`. Below Compose v5.2.0 (or Engine 28.1) rungs 2 and 3 are disabled (§3.6). In systemd mode, use the host's `docker compose`.

**Command:**
```
<compose> -p <project> --project-directory <working_dir> -f <config_files...> [--env-file <f>...] \
  up -d --force-recreate --no-deps --pull never --no-build <service>
```
- Inputs come from these labels: `com.docker.compose.project`, `.project.working_dir`, `.project.config_files`, `.project.environment_file` and `.service`. The project and service are validated first, and `--` goes before the service operand once the compose matrix accepts it (v3.13 item 11); labels that come from the image are not compose inputs (v3.13 item 10).
- `--pull never`: without it, `pull_policy: always` (or a missing image) would make compose pull during a heal.
- `--no-build`: a heal never builds.
- Never use `down`. It removes the project network and affects other services.

**Mounts (container mode):**
- Mount the stacks root at the **identical path**, read-only, e.g. `/opt/stacks:/opt/stacks:ro`.
- Also mount anything compose reads from outside it: `env_file:` paths with `../`, `include:`/`extends:` files, and file-based `secrets:`/`configs:`.
- `:ro` works (E6). Mounting the stacks elsewhere fails, because compose passes the host path to the daemon for relative binds: that is why the path must be identical.

**Fallbacks and restarts:**
- If compose labels or files are missing or unreadable, skip rung 2 and notify.
- Rung 1 (restart) uses only the Docker API.

### 3.5 Dependents (network_mode sidecars)

Dependents break whenever their parent is restarted or recreated, by anything: Docker's restart policy, dokdosta, or a manual `compose up`.

- **Discovery:** a dependent is any container whose `HostConfig.NetworkMode` (or IPC/PID mode) is `container:<id-or-name>` pointing at the parent. Compose resolves `service:X` to `container:<id>` at create time.
- **Parent restart** (same ID): restart dependents via the Docker API, one at a time, with a health wait between each. The dependent keeps the parent's old namespace until it is restarted (E1 part B). A non-forced `up` does not repair it (E2c).
- **Parent recreate** (new ID): recreate the dependents.
  - Command: `<compose> ... up -d --no-deps --pull never --no-build <dep...>` **without** `--force-recreate`. Compose hashes the *resolved* `container:<id>`, so the new parent ID causes a mismatch and compose recreates the dependents itself. Confirmed for a running-but-stale dependent and for one that already exited (E2b, E2d).
  - Use `--force-recreate` only if a post-check still finds a stale `NetworkMode` afterwards.
- **Remedy by staleness condition** (the two conditions are under *Staleness sweep* below):
  - *Parent ID gone* (the dependent's `NetworkMode` targets an ID that is not the parent's current ID, or no longer exists) → **recreate** the dependent as above.
  - *Same ID, parent started later* → **restart** the dependent via the API.
  - **Never API-restart a dependent whose parent ID is gone.** The restart fails (`joining network namespace of container: No such container`) and turns a running-but-disconnected dependent into an exited one (E1).
- **Finding the parent after its ID is gone (v3.11, item 2):** through the services learned for the dependent (`parents:<key>` in the store), taken from the `depends_on` label while it has one and from a live `container:` reference otherwise. Compose empties the label of a service recreated with `up --no-deps`, so the label alone is not enough from the second repair on. The namespace parent (the service the `container:` reference points at) is persisted on its own and decides the opt-in (v3.13 item 3).
- **Opt-in (v3.6, item 9):** the dependents step repairs the dependents of an opted-in parent even if they are not opted in themselves, unless labelled `dokdosta.enable=false`; their own failures need their own opt-in. It does not count toward their flap limit (item 10). The parent is the namespace parent (v3.13 item 3). A sidecar that exited on its own is recreated with its parent; one stopped by hand stays stopped (v3.13 item 2).
- **Every parent change is repaired (v3.13 item 1):** a dependents-step repair records the parent's ID and `StartedAt`; when the sidecar is stale again against a different parent, a fresh incident opens at rung 0 (restart for a same-ID parent restart, the non-forced `up` for a recreate).
- **Trigger:** a parent `start` or `create` event outside the in-flight window. When the action is dokdosta's own, it runs the dependents step explicitly.
- **Staleness sweep** (part of the poll and startup sweep). A dependent is stale if either:
  - its `NetworkMode` is `container:<id>` and `<id>` is not the parent's current ID; or
  - the parent's `State.StartedAt` is newer than the dependent's.

  A stale dependent usually stays `running` with only a loopback interface (E1), so no state-based rule notices it. Only this sweep and the parent's `start`/`create` event find it.
- **At boot** (confirmed in M0b after a daemon restart and a reboot): Docker restores a `container:` dependent after its parent, with the same network namespace, also behind a parent whose start takes longer than the 5s cap (8s; the dependent started after the parent's `start` event) and behind a parent that only becomes healthy 25s later (the daemon does not wait for readiness). A parent that is **not restored** (`restart: "no"`) or whose start fails takes its dependents down: they are attempted at once, fail with `cannot join network namespace of a non running container`, and are not retried. The "single shared 5s timer" covers a container's dependencies together, which with one `container:` parent is just a 5s cap. A dependent restored before its parent was not reproduced; the startup sweep stays as the backstop.
- **A parent left down by policy leaves its dependents down** (`restart: "no"` parent stopped by a host shutdown, `heal-exited` not `always`; §3.3 rule 4): no ladder runs for the dependents, and one notification covers the parent and them.
- **Ordering:** while a parent has an open incident or an in-flight action, suppress its dependents' ladders. Heal the parent first, wait for it to be healthy, then handle the dependents in order.
- **Reverse case:** a dependent failing on its own never triggers an action on its parent.
- **Compose behaviors to keep in mind:**
  - `depends_on: {X: {restart: true}}` only applies to explicit compose commands, not to restarts by Docker's restart policy.
  - `up --no-deps` narrows the project and therefore suppresses `depends_on.restart` (E3, with positive controls). So the explicit dependents step is needed, and there is no double restart. When dependents are in scope, compose restarts or recreates them *before* the parent is recreated.
  - Compose bug #13878 (closed): in v5.2.0, `network_mode: service:` dependents were recreated on *every* `up`. It was fixed by compose PR #13880 (29 Jun 2026), which v5.3.0 and later contain, and v5.6.0 does not recreate unchanged dependents (E2a). On v5.2.0, expect external `create` events when the maintainer runs `compose up`, and treat them as external recreates.

### 3.6 Drift guard (before rung 2, and before rung 3's `up`)

A recreate deploys whatever is on disk and in the local image store *now*. The guard runs three checks:

1. **Config drift:**
   - **Primary: the dry-run oracle (E5).** For non-reference services and for parents, run `<compose> ... --dry-run --progress json up -d --no-deps --pull never --no-build <svc>`.
     - Output is one JSON object per line: `{"id":"Container <name>","status":"Working|Done","text":"<action>"}`.
     - A `Recreate` text for the service's container means drift. The oracle is compose's own recreate decision, so it covers config drift and image drift together. It does not say why.
     - It has no side effects: container IDs were unchanged after every dry run in E5.
     - A stopped container with unchanged config shows `Starting`/`Started` and no `Recreate`, so that is not drift.
     - The JSON format is a compose implementation detail. The CI compose matrix pins it (§6).
   - **Fallback: `config --hash`,** used when the oracle's output can't be parsed. Compare `<compose> config --hash=<svc>` with the container's `com.docker.compose.config-hash` label (E4).
     - Syntax: comma-separated service names, or `'*'` for all; commas are not trimmed. Output: one `<service> <hash>` line per service, sorted.
   - **Skip this check, oracle and hash both, for services whose `network_mode`/`ipc`/`pid`/`volumes_from` references another service or container.** Compose hashes those *after* resolving the reference to `container:<id>`, but `config --hash` hashes the unresolved form, so they never match, even on a freshly created container (E4: all four reference types; compose source: `execCreateContainer` → `resolveServiceReferences` → `ServiceHash`). The oracle reports `Recreate` for a dependent after its parent was recreated, which is expected rather than drift (E5). For these services, rely on checks 2–3 and on the parent's own check. **Exception (v3.13 item 4):** on a reference holder's own failure, the oracle (not the hash) runs when every container it references is the current one of its service, provided the compose matrix test of that item passes for the release in use.
2. **Image drift:**
   - Compare the container's `com.docker.compose.image` label, the digest compose recorded and uses for its own recreate decision, with what the image reference resolves to locally now:
     ```
     docker image inspect --platform linux/<arch> --format '{{.Id}}' <ref>
     ```
     `<arch>` is the server's (`docker version --format '{{.Server.Arch}}'`). This is **one comparison that is correct on both image stores** (M0b, `test/m0/m0b/image-drift.sh`): it equals the label before a re-tag and differs after. On the containerd store the label is the platform manifest digest and `--platform … .Id` returns it; on the classic store the label is the image ID.
   - **v3.6 (approved):** the label is the platform ID only from compose v5.5.0; compose v5.2.0 on the containerd store writes the plain image ID. The container is not drifted if the label equals the platform ID **or** the plain `<ref>` `.Id`. See *Changes in v3.6*, item 3.
   - **Never use `.Descriptor.digest`**, with or without `--platform`: it is empty on the classic store, so the guard would report drift for every container and SUSPEND them all. Never compare the label with plain `<ref>` `.Id`, with `.RepoDigests` or with the container's `.Image` either: on containerd those are index digests and never equal the label, and on classic plain `.Id` happens to work, which is the trap.
   - If the inspect fails (the reference is no longer present locally, or has no image for that platform), that is a failure: skip the recreate and say why.
   - **`image inspect --platform` needs Engine 28.1 or later** (API 1.49; docker/cli added the flag after v28.0.4, and the GitHub runner's Engine 28.0.4 answered `unknown flag: --platform` in the first CI run of the `image-drift` job). There is **no fallback and no store guessing** for older Engines: see *Minimum versions per rung* below.
   - The oracle already covers image drift for the services it checks. Run this explicit comparison for the services the oracle skips and whenever the hash fallback is used.
   - Fresh Engine v29 installs default to the containerd store, and a host that has run the classic store stays on it unless `containerd-snapshotter` is set explicitly. The CI `image-drift` job runs the check on both stores.
   - Not a finding before rung 3's pull (v3.8 item 2): the pull is about to move the tag.
3. **Compose version skew** (only when running the bundled compose fallback):
   - Compare the container's `com.docker.compose.version` label (present; `5.6.0` in the fixtures) with the bundled version.
   - If both the versions and the hashes differ, report "can't verify drift (compose X on host vs Y in dokdosta)". This blocks, also in `warn`.
   - With the host plugin (v3.6, item 12): if the plugin's release differs from the one in the container's label, add "compose upgraded from X to Y since this container was created" to the drift message, as information only: it is not a block of its own.
   - This is a real risk: v5.5.0 changed image-digest reconciliation and v5.6.0 pinned the hash byte layout. M0 had only one compose version, so skew is exercised by the CI matrix (§6).

**On any failure:** skip the recreate, notify with the specific cause, and set SUSPENDED.
- If dokdosta itself moved a shared tag (via rung 3 on another service), the message says which service did it and when (v3.8 item 5).

**Strictness:** `DOKDOSTA_DRIFT_GUARD=strict|warn` (default `strict`). `warn` recreates anyway and says so, for **verified drift only** (a `config` or `image` finding); a "can't verify" finding (a check that could not be made, the bundled compose's version skew) still blocks (v3.6, item 11).

**Minimum versions per rung** (decided 17 Oct 2026, after the first CI run of the M0b `image-drift` job):
- **Rung 1 (restart, Docker API only) works from Engine v25**, the minimum for dokdosta as a whole.
- **Rungs 2 and 3 (recreate, pull) and the drift guard require Engine ≥ 28.1 and Compose ≥ v5.2.0**, the oldest compose in the CI matrix (§6). Engine 28.1 is the first with `image inspect --platform` (§3.6 check 2); the dry-run oracle's output is pinned by the compose matrix (on the runner's compose 2.38.2 it printed `null` instead of JSON lines).
- **dokdosta checks both at startup** (the Engine version from the API, the compose version of the binary chosen in §3.4). If either is below the minimum it logs **one warning** that names the versions, **disables rungs 2 and 3** for every container, and the ladder ends after rung 1 with notify + SUSPENDED, as if `dokdosta.actions=restart` were set. The same applies per container when its labels select rungs that are disabled. Status and the dashboard show the disabled rungs and the reason.
- **No store-guessing fallback.** Plain `<ref>` `.Id` equals the label on the classic store but not on the containerd store, and guessing the store is how the guard would flag or miss drift silently. Below the minimum, the guard does not run at all, and neither does the recreate it protects.

### 3.7 Pull rung (opt-in, `dokdosta.heal.pull=true`)

Runs only after restart and recreate have failed (it needs `recreate` in the ladder, v3.8 item 11) and only above the version gate, §3.6. In this order:
0. Read the service's `pull_policy`; `never` and `build` (or an unreadable policy) end the rung without running it: SUSPENDED with "pull rung does not apply (pull_policy ...)" (v3.8 item 14).
1. Run drift checks 1 and 3 (§3.6; the guard in pull mode, v3.8 item 2) **before** the pull, and again after a pull that changed the image, together with the pause, before the `up` (v3.13 item 9). After a pull that changed the image, the dry-run oracle reports `Recreate` because the image changed (E5), so checking afterwards would block every pull rung. A blocked check skips the pull, notifies, and SUSPENDS (`DriftBlocked`).
2. Read what the image reference resolves to for `linux/<arch>` (and the plain ID, for the rollback). Run `<compose> ... pull --policy always <svc>` (v3.8 item 1). Read the reference again.
3. If the platform image ID is unchanged: notify (the giving-up message says "the pull found no newer image for <ref>") and SUSPEND at once.
4. If it changed: record the move (`TagMoved`, below), run `up -d --no-deps --pull never --no-build <svc>` (not forced), check that the container was replaced (`--force-recreate` if it was not), then the dependents step (§3.5), in which a sidecar may take this move and only this one (v3.8 item 13).

The notification ("upgraded during heal", from `TagMoved`) includes:
- the old image → new image;
- the exact rollback commands: `docker image tag <old-id> <ref>`, then `<docker compose> -p ... up -d --no-deps --pull never --no-build <svc> [<sidecar>...]` (the old image stays local until pruned; `--no-build` from v3.13 item 7); the same message, as `Upgraded`, when rung 2 of a `heal.pull=true` container took a tag that someone else had moved (v3.8 item 12), only when its pull rung would apply (v3.13 item 5); its sidecars may follow that move and are named in the rollback (v3.13 item 6);
- one cuttable line saying the rollback does not last while `dokdosta.heal.pull` stays on (v3.13 item 8).

`TagMoved` records that dokdosta moved a tag: the container, the time and the old and new IDs, in the store's meta, kept until the same reference is moved again. Other stacks sharing the tag will now see image drift, and when the drift guard blocks one of them, its message says which container moved the tag and when (only while the reference still resolves to what that pull produced). A dry run never pulls and records nothing.

Private registries (M7, v3.12 items 3 to 8). Without `DOKDOSTA_REGISTRY_AUTH_DIR`, the pull runs with the compose child's environment, which reads no Docker config (an empty private `DOCKER_CONFIG`, v3.12 item 1), so a registry that needs a login refuses it (v3.8 item 15). With the variable set, **the pull command and only the pull command** runs with `DOCKER_CONFIG=<dir>`:
- the directory's `config.json` may hold only `auths`, each entry with an inline `auth`, `username`+`password` or `identitytoken`;
- any other key, or a `contexts` entry, is a startup error;
- the file is checked again before each pull;
- the credentials are secrets (the redactor knows them);
- `status`, the API and the home page show only the registry hosts.

### 3.8 Guardrails
- Opt-in label `dokdosta.enable=true`, or `DOKDOSTA_MODE=all`. A `dokdosta.*` label that comes from the image does not count (v3.13 item 10).
- Never act on itself. In container mode, resolve the own container ID from `/proc/self/mountinfo` (the hostname can be overridden).
- At startup, warn if dokdosta's own `NetworkMode` is `container:<id>`. **Never run dokdosta with `network_mode: service:<x>`**: it would become a dependent of a container it may heal.
- Rate limit, flap limit, cooldown backoff, `--dry-run` (log and notify only, with a dashboard banner).
- Events-stream reconnect: resubscribe with `since=<last event time>`; the poll re-syncs state.
- `/healthz` fails when the events stream has been down longer than a threshold, and when the Docker API has been failing for longer than 2 × `POLL_INTERVAL`; the main loop's Docker reads run under a 30 s timeout (v3.13 item 13). Use it as dokdosta's own healthcheck. On the TCP listener it says only `ok` or `not ok` (v3.9 item 12); the unix socket keeps the reason.
- Optional uptime ping: `DOKDOSTA_HEARTBEAT_URL` every `DOKDOSTA_HEARTBEAT_INTERVAL` (default 5m), only while healthy (§3.12).
- Structured JSON logs.

### 3.9 Security
- Mounting `docker.sock` is root-equivalent on the host. Say so in the README.
- Minimal image with a read-only root filesystem. Only `/data` (store) and `/run` (control socket) are writable.
- **Base image: `gcr.io/distroless/static:nonroot`** (E7). The statically linked compose plugin runs in it (and in `alpine`). The image runs as uid 65532, which cannot reach the socket by default (`permission denied while trying to connect to the docker API`), so add `group_add` for the socket's group (`stat -c %g /var/run/docker.sock` on the host). There is no shell, so the image's own healthcheck must call the binary.
- **Dashboard:**
  - Off by default, read-only, no write endpoints (so no CSRF surface).
  - Never render env vars or full inspect output; show only `com.docker.compose.*` and `dokdosta.*` labels.
  - Optional token (v3.9 items 4, 11): bearer, or the password of HTTP Basic for browsers, over TLS only. Constant-time compare; never taken from the URL. Wrong tokens: 10 per minute per IP, then 429 with backoff (v3.9 item 15; `X-Forwarded-For` is read only from `DOKDOSTA_HTTP_TRUSTED_PROXIES`, v3.10 item 5). Without a token only loopback Host names and `DOKDOSTA_HTTP_ALLOWED_HOSTS` are served (item 14).
  - Only GET and HEAD are served; strict security headers and a CSP without inline script or style (v3.9 item 5); labels, event attributes and error text are filtered at one boundary (v3.9 item 6).
  - Recommended exposure: `127.0.0.1:8484:8484`, a reverse proxy, or a Tailscale container on a **shared bridge network** forwarding with `tailscale serve`. Not `network_mode`.
- A socket proxy is roadmap (R3). Compose needs broad API access, so it adds little in v1.

### 3.10 Store
- SQLite via `modernc.org/sqlite` in `DOKDOSTA_DATA_DIR` (default `/data`), file `dokdosta.db`. A `whalenurse.db` from before the rename is renamed to it once, with its `-wal` and `-shm`, under both locks (v3.15 item 6).
- Tables: per-key state, incidents and flap history, events (`events`, schema 3, built in M5: the autoincrement ID doubles as the `since` cursor and is never reused), and notifications sent (`notified`, the dedupe ledger of §3.12; schema 2, built in M3).
- Retention: `DOKDOSTA_HISTORY_RETENTION` (default 30d) for closed incidents and events, hourly; the events of an incident that is still open are kept (v3.13 item 21); events are also capped at 100,000 rows (open incidents' events deleted last).

### 3.11 API and dashboard (read-only)
- **API:** `/api/v1/status`, `/api/v1/containers`, `/api/v1/containers/{key}`, `/api/v1/events?since=<cursor>`, `/api/v1/incidents`, plus the SSE stream `/api/v1/stream` (built in M5; v3.9 items 1 to 3). Alongside it: `/healthz` and an optional Prometheus `/metrics` (`DOKDOSTA_METRICS`).
- The dashboard uses only this API, and the hub (R1) will reuse it as the agent contract.
- **Pages:**
  - Overview: counts, healer status, compose binary and version, dry-run banner, rate-limit budget.
  - Containers: state, health, incident and rung, cooldown, flap count, badges, effective config.
  - Container detail: timeline (without the `EventAttributed` noise unless `?debug=1`), inspect summary, drift status (the guard's last verdict, in plain words), parent and dependents with their state, the last upgrade with its rollback commands (one per line, a Copy button each).
  - History.
- **Tech:** `html/template`, a little vanilla JS (or vendored htmx), assets embedded with `embed`. No Node build.
- **Hub readiness:** `host` in every record, a versioned API, monotonic per-host event IDs, UTC timestamps.

### 3.12 Notifications (M3, v3.7)

`internal/notify` is one more subscriber of the domain event stream, next to the log and the store, so what is sent cannot disagree with what is recorded.

- **What is sent.** Only the events below. Everything else (`IncidentOpened`, `ActionStarted`, `RungUnavailable`, the manual-stop and restore events ...) stays in the log and the store.

| Event | Message | Weight |
|---|---|---|
| `ActionResult` | action taken: `restarted`, `recreated`, `started`, `pulled`, or `restart failed` with the error; says why it was started and the rung. Not sent for a pull with an outcome (`unchanged`, `upgraded`, `upgrade-failed`): the two rows below say it | routine |
| `Recovered` | recovered | routine |
| `Unsuspended` | unsuspended, with the reason (v3.7 item 13) | routine |
| `Suspended`, reason `ladder` | giving up, SUSPENDED, with the cause (also "the pull found no newer image") and `dokdosta unsuspend <scope>` | important |
| `Suspended`, reason `flapping` | flapping, SUSPENDED | important |
| `Suspended`, reason `drift` | *not sent* when it directly follows its `DriftBlocked`, which says the same; a re-suspension later in the same incident is sent as giving up, the first one only (v3.13 items 18 and 19) | |
| `TagMoved`, `Upgraded` | **upgraded during heal** (v3.8): the pull rung brought a newer image (`TagMoved`), or rung 2 of a `heal.pull=true` container took a tag that someone else moved (`Upgraded`): old → new, the sidecars recreated with it, a cuttable line that the rollback does not last while `heal.pull` stays on (v3.13 item 8), and the exact rollback commands (never cut; body up to 720) | important |
| `DriftBlocked` | recreate blocked, with the guard's reason, a hint (compose upgraded or skewed, tag moved, file changed), who moved the tag if it was dokdosta's pull rung, and the unsuspend command | important |
| `DriftWarned` | recreating despite drift (`DOKDOSTA_DRIFT_GUARD=warn`) | routine |
| `NotifyOnly` | needs attention, nothing was done: a `restart: "no"` parent left down by a host shutdown, **with the dependents that stay down with it** | important |
| `NeedsRecreate` | needs a recreate that is not available (version gate, no usable compose inputs, `recreate` not in `dokdosta.actions`) | important |
| `RateLimited` | action rate limit reached, healing waits; at most once an hour per URL (v3.7 item 14) | important |
| `RungsDisabled` | the version-gate warning: recreate and pull rungs disabled, and why | important |
| `Paused`, `Resumed` | paused / resumed `<scope>` (including expiry) | routine |

- **Shape.** Title: `[dry-run] host: container headline`. Body: the container key (or `host <id>`), then the cause and the next step. At most 72 runes of title and 360 of body (720 for a message with commands to copy, whose commands are never cut: v3.8 item 6); a long container name is cut, never the headline or the key line.
- **Delivery never touches healing.** The engine publishes events under its lock, so the subscriber only queues (a full queue drops and counts). One dispatcher renders and deduplicates. **One worker and one Shoutrrr sender per URL** deliver in order, each attempt under `DOKDOSTA_NOTIFY_TIMEOUT`, with two retries (after 2 s and 10 s). A hung URL delays nothing, not even another URL, and a panic inside a service is a failed delivery (dokdosta calls the located service itself under a `recover`, because Shoutrrr's router does not). A failure is logged and counted per URL; it is never published as a domain event. On a stop the queue is delivered for up to 5 s, then what is still in flight is aborted.
- **Secrets.** The URLs hold tokens (a Discord webhook id and token are the host and the path, a Telegram bot token is the user name, ntfy and Gotify tokens are a path or a query value). A URL is only ever shown as its position and scheme, `#2 discord`. Every error text goes through a redactor built from the configured URLs that masks the URL, its user info, host, path segments (of 6 runes or more) and query values (of 4 or more, except the boolean words `true`, `false`, `1`, `0`, `yes`, `no`, `on`, `off`: v3.13 item 20), in their escaped forms too; Shoutrrr redacts its own errors since v0.22.0 as well, and this is a second layer. `config` never quotes these variables in an error or a warning. `status`, `/healthz` and `notify test` show counts and redacted reasons only.
- **Dedupe.** One message per (event type, container key, discriminator, incident); the discriminator is the action and rung for `ActionResult`, the reason for `Suspended`, the findings for the drift events. The incident is the `incident` attribute the engine puts on every event of a key with an open incident (the incident's opening time), so the same event within one incident is said once, a new incident is news again, and a restart of dokdosta does not mix them up. Events with no incident are deduplicated for 24 h. Deliberate commands (`Paused`, `Resumed`, an `unsuspend` command) are never deduplicated. `Suspended` and `Unsuspended` make each other news again, once per incident (v3.13 item 18). **The dedupe is persisted** in the store's `notified` table (§3.10) and survives a restart.
- **Rate limit.** `DOKDOSTA_NOTIFY_RATE_LIMIT` messages per hour per URL (default 20), a sliding window. Routine messages may use the first half, important ones all of it. Messages held back are counted (`status` shows them) and the next message that goes out says how many ("N earlier messages were held back by the rate limit").
- **Dry-run sends notifications**, marked `[dry-run]` in the title and in the first line of the body: that is how the maintainer checks the setup.
- **`dokdosta notify test`:** one test message to every URL, one attempt each, no dedupe, no rate limit; one line per URL, `ok (143ms)` or `FAILED <redacted reason>`; exit 1 if any failed or `DOKDOSTA_NOTIFY_URLS` is empty. It reads the environment, so `docker exec dokdosta /dokdosta notify test` tests what the daemon has, and it needs no running daemon.
- **Uptime ping.** `DOKDOSTA_HEARTBEAT_URL` (healthchecks.io, Uptime Kuma push) gets a plain GET at once and then every `DOKDOSTA_HEARTBEAT_INTERVAL`, with the same timeout, **only while the daemon is healthy** (the `/healthz` condition); otherwise the ping is skipped, so the monitor alarms by the missing ping. The URL is a secret and is redacted like the others.
- **`DOKDOSTA_NOTIFY_URLS`** holds several URLs separated by whitespace or by a comma that is followed by `scheme://`. A comma inside a URL (`tags=warning,skull`, `chats=a,b`) is part of it. A URL that Shoutrrr rejects is logged by position and scheme, shown as `INVALID` in `status` and skipped.

### 3.13 Release and supply chain (M6, v3.10)

The pipeline is described in the v3.10 list above (items 1 to 4); this section holds what to keep true.

- **Only a `v*` tag publishes.** `release.yml` is the only workflow with `packages: write`, `id-token: write`, `contents: write` and `attestations: write`, in one job (`publish`) that needs the `test` job. `ci.yml` has `contents: read` and never logs in to a registry or reaches an OIDC token. The tag must be on `main` and have a dated `CHANGELOG.md` section.
- **What a release holds:** `dokdosta_<version>_linux_{amd64,arm64}.tar.gz`, `<archive>.sbom.spdx.json`, `checksums.txt` (all four above are in it), `checksums.txt.sigstore.json`, and the image `ghcr.io/jackspiering/dokdosta` (index of two platforms, signed, with BuildKit's SBOM attestation, and a GitHub provenance attestation by digest). Tags: `vX.Y.Z` always, `vX.Y`, `vX` and `latest` only for a release that is not a pre-release.
- **Identity.** A signature is accepted only from `https://github.com/jackspiering/dokdosta/.github/workflows/release.yml@refs/tags/<tag>` with the issuer `https://token.actions.githubusercontent.com`.
- **Pinned.** Actions by commit SHA, base images by digest, the bundled compose by sha256, GoReleaser, cosign and syft by exact version; the Dockerfile syntax frontend and the buildx BuildKit image by digest (v3.13 item 22).
- **Gates (v3.13 item 22).** `govulncheck` in the `go` and release `test` jobs; the Go version of `go.mod` equals both Dockerfiles' `golang:` base; the publish job builds with an empty Go cache.
- **The maintainer's steps** (public GHCR package, branch protection, tag rules, immutable releases, the release gate, the tag itself) are in [releasing.md](releasing.md). The release gate: **no `v0.1.0` tag until dokdosta has run for a few days in dry-run on a real host, and then live on a few test containers.**

## 4. Configuration (v1)

Initial defaults; tune them after M0.

**Environment variables:**

| Variable | Default |
|---|---|
| `DOKDOSTA_NOTIFY_URLS` | — (Shoutrrr URLs, separated by whitespace or a comma before `scheme://`; secret; from M3) |
| `DOKDOSTA_NOTIFY_TIMEOUT` | `10s` (one delivery attempt, per URL; from M3) |
| `DOKDOSTA_NOTIFY_RATE_LIMIT` | `20` (messages/hour per URL; from M3) |
| `DOKDOSTA_DRY_RUN` | `false` |
| `DOKDOSTA_MODE` | `label` (or `all`) |
| `DOKDOSTA_POLL_INTERVAL` | `60s` |
| `DOKDOSTA_GRACE` | `30s` |
| `DOKDOSTA_COOLDOWN` | `30s` (base; doubles per rung, capped at 5m) |
| `DOKDOSTA_RECOVERY_PERIOD` | `10m` |
| `DOKDOSTA_FLAP_LIMIT` | `3` |
| `DOKDOSTA_FLAP_WINDOW` | `24h` |
| `DOKDOSTA_CRASHLOOP_AFTER` | `5m` |
| `DOKDOSTA_RATE_LIMIT` | `10` (actions/hour) |
| `DOKDOSTA_STARTUP_SETTLE` | `2m` |
| `DOKDOSTA_DRIFT_GUARD` | `strict` (or `warn`; from M2b) |
| `DOKDOSTA_COMPOSE_BIN` | — (see §3.4; from M2b) |
| `DOKDOSTA_HOST_ID` | hostname (persisted keys are re-keyed to it at startup, v3.13 item 12) |
| `DOKDOSTA_DATA_DIR` | `/data` |
| `DOKDOSTA_HTTP_ADDR` | empty (dashboard, API and `/healthz` off; `127.0.0.1:8484` or `:8484` in examples; from M5 it serves the dashboard and the API) |
| `DOKDOSTA_CONTROL_SOCKET` | `/run/dokdosta.sock` (unix socket for `/healthz` and, from M2a, the CLI; added in M1) |
| `DOKDOSTA_HTTP_TOKEN` | — (secret; bearer or Basic password for the dashboard and API; `/healthz` stays open; from M5) |
| `DOKDOSTA_METRICS` | `false` (serve `/metrics` on the HTTP listener; from M5) |
| `DOKDOSTA_HTTP_ALLOWED_HOSTS` | — (extra Host names served when there is **no token**, besides localhost, 127.0.0.1 and [::1]; comma or space separated; from M5) |
| `DOKDOSTA_HTTP_TRUSTED_PROXIES` | — (CIDRs or addresses, comma or space separated, of reverse proxies whose `X-Forwarded-For` the wrong-token limit believes; ignored without a token; `/0` refused; from M6) |
| `DOKDOSTA_HISTORY_RETENTION` | `30d` |
| `DOKDOSTA_REGISTRY_AUTH_DIR` | — (directory holding a `config.json` with inline `auths` only, mounted read-only; passed to the pull rung's `compose pull` as `DOCKER_CONFIG`, to no other command; holds secrets; from M7) |
| `DOKDOSTA_HEARTBEAT_URL` | — (uptime ping, secret; from M3) |
| `DOKDOSTA_HEARTBEAT_INTERVAL` | `5m` (from M3) |
| `DOKDOSTA_LOG_LEVEL` | `info` |

**Labels:**

| Label | Values / default |
|---|---|
| `dokdosta.enable` | `true` to opt in |
| `dokdosta.actions` | default `restart,recreate` |
| `dokdosta.heal-exited` | `crashed` \| `always` \| `never` |
| `dokdosta.heal.pull` | `true` \| `false`; needs `recreate` in `actions`, else the pull stays off with a warning; never runs for `pull_policy: never\|build` |
| `dokdosta.cooldown` | per-container override |
| `dokdosta.flap-limit` | per-container override |

**CLI:** `dokdosta run` (default), `pause`, `resume`, `unsuspend`, `status`, `notify test` (v3.7).

**Version minimums (fixed, not configurable):** Engine v25 for rung 1 (restart). Rungs 2 and 3 and the drift guard need Engine ≥ 28.1 and Compose ≥ v5.2.0. dokdosta checks them at startup, logs one warning if either is lower, disables rungs 2 and 3, and ends the ladder with notify + SUSPENDED after rung 1 (§3.6). `DOKDOSTA_DRIFT_GUARD=warn` does not bypass this: below the minimum there is no recreate to guard.

## 5. Architecture and dependencies

```
cmd/dokdosta/      main + subcommands
internal/docker/     interface over the moby client (events, inspect, restart, list) + fake
internal/compose/    binary resolution, command builder, hash/dry-run helpers
internal/engine/     incidents, ladder, flap limit, rules (pure, injected clock); emits domain events
internal/rules/      inspect classification (State.Error, exit codes, heal-exited), fixture-tested
internal/store/      SQLite persistence + retention
internal/notify/     Shoutrrr wrapper, templates, dedupe, heartbeat
internal/api/        /api/v1 JSON + SSE, /healthz, /metrics; the documents (the hub contract) and the output rules
internal/web/        dashboard templates + embedded assets (imports internal/api only)
internal/control/    unix-socket control server for the CLI
internal/config/     env + label parsing
```

**Domain event stream:** one stream (`IncidentOpened`, `ActionStarted`, `ActionResult`, `Suspended`, `Recovered`, `Paused`, `TagMoved` and `Upgraded` (built in M4), …). Store, notify and API/SSE all subscribe to it, so notifications and history can't disagree.

**Dependencies** (check the latest version at implementation time):
- **`github.com/moby/moby/client` + `/api`.** The client was at v0.6.2 on 8 Oct 2026. It is pre-1.0, so minor versions can break; keep it behind `internal/docker`. API negotiation is the default (`WithAPIVersionNegotiation` is deprecated), and `Events()` returns a single `EventsResult`.
- **Minimum Docker Engine v25** for rung 1 (restart). Engine v29 rejects API versions below 1.44. Rungs 2 and 3 and the drift guard require Engine ≥ 28.1 and Compose ≥ v5.2.0 (§3.6).
- **`modernc.org/sqlite`.**
- **`github.com/nicholas-fedor/shoutrrr`: vetted in M3 (18 Oct 2026), v0.22.0 (13 Oct 2026).** MIT. A maintained fork of `containrrr/shoutrrr`, whose last release is v0.8.0 (Aug 2023) and last commit May 2026; the fork has released weekly since Sep 2026 and commits daily, but is effectively one maintainer (30 contributors in all). It supports ntfy, Gotify, Discord, Telegram, Slack, Matrix, Mattermost, Rocket.Chat, Teams, Google Chat, Zulip, Signal, XMPP, Lark, WeCom, Pushover, Pushbullet, SignalGrid, IFTTT, Bark, Join, Home Assistant, MQTT, SMTP, Twilio, OpsGenie, PagerDuty, Notifiarr and a generic webhook (plus a logger). v0.22.0 has context-aware sends, a send budget per service and redacted errors and logs; dokdosta builds one router per URL with `CreateSenderWithOptions` and `SendContext` (the non-options constructors are deprecated). The root package registers every service, which costs about 8.5 MB of binary (v3.7 item 11).
- No cron library in v1. If R4 needs one, `robfig/cron` is unmaintained (last release Jan 2020); use `go-co-op/gocron/v2` or a cron-expression parser plus your own timer.

## 6. Testing

**Unit tests** (engine with a fake clock and fake Docker):
- the ladder: restart → recreate → SUSPENDED;
- recovery closes the incident;
- the flap limit suspends after N incidents in the window;
- pause and pause expiry;
- the action-in-flight window suppresses interpretation;
- a manual stop is ignored;
- shutdown kills (M0b fixtures `m0b-daemon-restart-shutdown` and `m0b-reboot-shutdown`):
  - markers created in the 30 s before an events-stream loss are discarded, and an older real manual stop survives;
  - a changed boot id drops all markers and opens a restore window;
  - the heartbeat: dokdosta killed by the shutdown itself (no stream loss) and restarted with the same boot id discards the markers set in the 30 s before `last_seen`, an older real manual stop survives, a gap over `POLL_INTERVAL` opens the restore window, and a SUSPENDED container stays SUSPENDED through it;
  - the crash-loop rule: three quick restarts followed by a quiet are not a loop, restarts continuing to within `CRASHLOOP_AFTER` are, and the verdict does not depend on the poll interval or `GRACE`;
  - a `start` inside a restore window, or after a quarantined kill, does not clear SUSPENDED, and a SUSPENDED container killed by a shutdown and restored stays SUSPENDED;
  - negative test for the rejected burst rule: the same `kill[15]` burst with the events stream still up (`docker compose stop`) keeps its markers and the containers stay stopped;
  - an app that exits 1 on SIGTERM with `restart: "no"`, stopped inside a shutdown window (after a stream loss, and after dokdosta itself was killed with the same boot id), stays down with one notification; the same app outside a window is started; `always`/`unless-stopped`/`on-failure` are left to Docker; `heal-exited=always` starts it; a window ends 5 minutes after `last_seen` within one boot and is open after a reboot;
  - a `restart: "no"` parent stopped by a shutdown and its dependents stay down with one notification, and `heal-exited=always` starts the parent and then the dependents;
- `on-failure` + exit 0 is left alone;
- oneoff containers and swarm tasks are skipped;
- OOM 137 vs stop 137;
- failed first start (`created` + 126/127/128 + `State.Error`), failed restart (`exited` + previous exit code + `State.Error`) and failed boot-time restore (`exited` + 127 + `State.Error`, `RestartCount` 0), all classified by `State.Error` alone;
- the port-conflict matcher accepts `Bind for 0.0.0.0:80`, `Bind for :::80` and `Bind for [::]:80`;
- an API error or a closed stream while the daemon restores is "not ready" and opens no incident, and the sweep runs after the reconnect;
- the startup version check: Engine below 28.1 or compose below v5.2.0 logs one warning, disables rungs 2 and 3, and an unhealthy container ends at notify + SUSPENDED after rung 1; with both at the minimum nothing is disabled;
- stale-reference classification: `No such container` → recreate; `cannot join network namespace of a non running container` → heal the parent first, then restart the dependent;
- a container still `restarting` after GRACE is re-inspected by the poll;
- dependents are suppressed while the parent's incident is open;
- notifications (M3): the template of every notified event, with host and key always present and the limits held under worst-case text; events outside the list are not sent; dedupe per incident, per key, for 24 h without an incident, never for pause and resume or an unsuspend command, and one message for a drift suspension; the dedupe surviving a restart (real store, and the schema-1 migration); `Unsuspended` and `Suspended` alternating; `RateLimited` once an hour per URL; the per-URL rate limit with the critical reserve and the "held back" note; a failing URL is retried, counted and logged redacted; a hung URL, a full queue and a stalled dispatcher never block `Handle`, a panicking service is a failed delivery, and one URL never delays another; shutdown flushes; the redactor over every URL shape (user info, path, query, host, escaped forms, an unparsable URL), and the grep of the logs, `status`, `/healthz`, the socket status and the `notify test` output for tokens; URL splitting; the uptime ping (healthy only, failures redacted); the whole ladder through the daemon to a recording sender, and through Shoutrrr's generic service to a real HTTP receiver.

**Other tests:**
- **Rules:** scrubbed `docker inspect` fixtures, including captures from M0.
- **Store:** state survives a restart; retention pruning.
- **API (built in M5):** golden JSON per endpoint (this is the future hub contract); every dashboard page renders against a real store through a real daemon; a test plants secrets (an environment variable, labels outside the allowed prefixes, notification and ping URLs, the HTTP token, an event attribute) and greps every page, endpoint, the event stream and `/metrics` (with positive controls and mutation-checked: without the redactor or the attribute allow-list it fails); every non-GET method is refused on every route, with and without the token; the token cases (bearer, Basic, wrong, prefix, query), the Host check (allowed, refused, exempt `/healthz`, ignored with a token), the wrong-token limiter (10 a minute, the doubling block up to 15 minutes, a right token refused while blocked, other clients and `/healthz` unaffected, no credentials not counted, `X-Forwarded-For` ignored, IPv6 /64, bounded table, strikes forgotten after an hour), `/healthz` saying only ok or not ok, the skipped-rung marker; SSE replay, resume, key filter, reset, shutdown and the stream bound; pages carry no inline script, handler or style; `internal/web` imports only `internal/api`.

**M6 (v3.10):** `internal/api` (the trusted-proxy rules: only from a listed peer, the right-most untrusted entry, forged left entries, chains, several header lines, malformed and empty entries, IPv4-mapped and IPv6 /64, an untrusted peer that sends the header, one guessing client behind a trusted proxy not locking out another, and the unchanged shared budget without the option; mutation-checked), `internal/config` (the list's parsing and its errors; every variable and label documented and nothing documented that does not exist; every Markdown link and anchor resolves), `cmd/dokdosta` (the warning without a token); shell self-tests of `scripts/release/release-notes.sh` and `scripts/release/verify.sh` (stubbed `cosign`, `gh` and `curl`: the exact identity, a tampered archive, a missing file, a bad signature, a bad image signature, a failed attestation, a path in `checksums.txt`) and the checks of `scripts/ci/check-dockerfiles.sh` and `scripts/ci/check-pins.sh`.

**Integration tests** (CI, real Docker, projects `dokdosta-test-*` only). The tag says which milestone owns the test:
- **M2a:** toggled healthcheck → restart, then "needs recreate (M2b)", then SUSPENDED (rung 2 does not exist yet);
- **M2a:** state survives a dokdosta restart (SUSPENDED and the flap history are still there, and the ladder does not start over);
- **M2a:** pause, resume and unsuspend over the control socket against a real daemon;
- **M2a:** dry-run performs no actions;
- **M2b (done):** toggled healthcheck → restart, then recreate, then SUSPENDED;
- **M2b:** heal never pulls (a service with `pull_policy: always`);
- **M2b:** the config-drift guard blocks a recreate after a compose edit;
- **M2b:** the image-drift guard blocks a recreate after a re-tag, and does not block an unchanged container, on both image stores (the comparator of §3.6);
- **M2b:** a ref-holding dependent skips the hash check and is recreated after its parent is;
- **M3 (done; CI run 39121767631, all six legs):** notifications reach a local webhook receiver through Shoutrrr's generic service: the ladder ends in SUSPENDED (restart, recreate and one "giving up" with the cause, a dead URL beside the working one changing nothing), a recovery, and a dry run (marked, changes nothing);
- **M5 (done; v3.9):** the image with `DOKDOSTA_HTTP_ADDR` set and published on `127.0.0.1:<random port>`: `/api/v1/status` is 401 without the token (also with a wrong one and with the token in the query) and 200 with it (bearer, and Basic for the dashboard), the security headers are there, `/healthz` and nothing else needs no token, `/metrics` is behind the token, `/healthz` says only `ok`, a foreign Host is 403 without a token, ten wrong tokens earn a 429 (and the right token is refused while blocked), every other method is 405 on the API, the control-socket paths and the dashboard, the opted-in container is listed with allowed labels only, the token is not in the event history; and a second image run without a token is open on loopback and still read-only.
- **M4 (done; v3.8 items 10 to 14):** the pull rung, against a local `registry:2`: the registry round trip (and the positive control that docker events reports a pull), no label means no pull, no newer image means SUSPENDED, a newer image means upgraded with the sidecar repaired and a notification whose rollback runs as pasted, a drift block on a shared tag names who moved it, dry-run does not pull, the version gate disables the rung; `pull_policy: never` is not pulled and `missing` is overridden; `heal.pull` without recreate keeps the rung off; rung 2 of a pull-enabled container takes a moved tag and says so (the rollback runs as pasted); a sidecar sharing the image takes its parent's moved tag (its rollback is in the message).

**CI jobs added in M6:** `workflows` (actionlint and the SHA-pin check), `release-snapshot` (GoReleaser `check` and `release --snapshot --skip=sign`: archives, checksums, SBOMs, the amd64 image run and inspected, the arm64 image built), `examples` (`compose config -q`, `systemd-analyze verify`) and `demo` (the README's demo against the image from the commit, project `dokdosta-test-demo`).

**CI matrix (built in M2b: 2 stores × 3 compose releases = 6 integration jobs):** latest Docker Engine on both the classic and the containerd image store, crossed with three compose versions. The `image-drift` job (`test/m0/m0b/image-drift.sh`) runs on both stores today.

- **From M2a** the integration tests run on Engine 29.x with compose v5.6.0 on both stores, installed the way the `image-drift` job installs them (the runner's Engine 28.0.4 has no `image inspect --platform`, and its compose 2.38.2 prints `null` for the dry-run oracle). M2a does not drive compose: it uses compose only to set up test stacks, so the other two versions add nothing there.
- **From M2b** the full compose matrix applies, because dokdosta then runs compose itself and the oracle's output and the config hash are what the matrix pins:
- **v5.6.0**, the version M0 ran on;
- **the latest release**, floating. It is v5.6.0 today, so the two jobs coincide until a newer release. While they coincide, the latest legs' shards stop early and stay green (maintainer decision, 20 Oct 2026: they would repeat the pinned leg exactly); the `scope` job of `ci.yml` resolves the release (`scripts/ci/compose-latest.sh`), and a failed lookup runs the legs in full;
- **v5.2.0**, the only release with compose bug #13878. v5.3.0 (2 Jul 2026) contains the fix, and v5.2.0 (23 Jun) is the only release before the fix (29 Jun). It exercises recreate-on-every-`up` for sidecars, config-hash and dry-run output across versions, and the version-skew check (§3.6).

**Documentation-only pull requests (maintainer decision, 20 Oct 2026).** A pull request whose every changed file is Markdown (`*.md`) or under `docs/` (`scripts/ci/changes.sh`; any doubt counts as code) runs only the build, vet and unit-test job, whose tests check the documentation; the other required checks pass as skipped (the six `integration` legs and the two `image-drift` legs pass explicitly, and only if the `scope` job succeeded). Pushes to `main` always run everything.

**Pushes to `main` and stack layers (v3.14).** Pushes to `main` share one concurrency group that cancels nothing in progress, so of a burst of merges only the running push and the newest waiting one run. A pull request into a branch other than `main` (a layer of a stack) runs only the build, vet and unit-test job, `scripts`, `workflows` and `examples`; the integration and image-drift matrices are skipped as a whole, so their required checks stay unreported and a retargeted layer cannot merge into `main` before a full run. Stacks merge top-down, and the bottom pull request into `main` runs the full matrix for the whole stack.

The image under test is built once per run (the `image` job) and loaded by the integration shards and the demo.

## 7. Milestones

### H — Repo housekeeping
- [maintainer] Rename the repo to `dokdosta` (GitHub redirects the old URL), then run `git remote set-url origin https://github.com/jackspiering/dokdosta.git` in local clones.
- [maintainer] Set the About text and topics (Appendix C).
- Replace `README.md` with Appendix A.
- Add the Appendix B lines to `.gitignore`.
- Commit this plan as `docs/PLAN.md`.
- **Done when:** the README, `.gitignore` and plan are committed, and the [maintainer] items are listed in the report.

### M0 — Diagnose and verify (no app code)

**Read-only scripts** (refuse to modify anything):
- `scripts/m0/scan.sh`: lists containers that are exited with a restart policy, or have a non-empty `State.Error`. Shows exit code, `OOMKilled`, `State.Error`, `FinishedAt` and restart policy.
- `scripts/m0/capture.sh <container> [--since 30m]`: writes into `captures/<timestamp>-<name>/`:
  - `docker inspect`;
  - `docker events --since/--until --filter container=<name> --format '{{json .}}'`;
  - `journalctl -u docker --since ...` filtered with `failed to start container|restartmanger wait error|failed to restart container|failed to prepare mount points`;
  - `docker version`, `docker compose version` and `docker info --format '{{json .}}'` (image store).
- `scripts/scrub-fixture`: the scrubber from §3.3.

**Experiments:** throwaway stacks in `test/m0/` (projects `dokdosta-m0-*` only), plus `test/m0/run.sh`. The script runs each experiment, prints evidence and cleans up with `down -v` for its own projects only.

| # | Question |
|---|---|
| E1 | After `up -d --force-recreate --no-deps <parent>`, are `network_mode: service:` dependents broken (exited, or running with no network)? |
| E2 | Does a plain `up -d --no-deps --pull never --no-build <dep>` (no force) then recreate them? |
| E3 | Does `--no-deps` suppress `depends_on: {restart: true}`? |
| E4 | `config --hash` vs label for the parent (expect a match) and for the dependent (expect a mismatch) |
| E5 | `--dry-run up -d --no-deps ...` output for: an unchanged service, a changed config, a dependent after its parent was recreated, and a re-tagged image. Is it parseable? |
| E6 | Does recreate work from a container with the stacks dir mounted `:ro` at the identical path? |
| E7 | Does the host compose plugin run inside the candidate base image (distroless/static or alpine)? |
| E8 | Hypothesis A reproduction: `restart: unless-stopped`, a bind mount with `create_host_path: false` whose source is removed after the first start, and a process that exits 1. Expect `Exited (128?)`, a non-empty `State.Error` and no retry after 2 minutes |
| E9 | Event sequences for: `docker stop`, a crash, an OOM (tiny memory limit), a restart-policy restart, a compose recreate, and daemon restart (if allowed). Capture these as fixtures |

- [maintainer] Confirm the host for E1–E9. Run `scan.sh` and `capture.sh` on the real host when a stuck container appears.
- Record the results in `docs/m0-findings.md`. Move scrubbed captures into `testdata/`.
- **Done when:** every row above has an answer with evidence, and §3.3, §3.5 and §3.6 are each marked "confirmed" or "needs change" in the findings.

### M1 — Heal MVP (restart only)
- `go mod init github.com/jackspiering/dokdosta` with the latest stable Go.
- Config parsing for the M1 subset; `internal/docker` with a fake; events stream with reconnect.
- Poll and startup sweep; rules from §3.3, with every heal action mapped to restart/start in M1. One exception: **never apply rung 1 to a dependent whose parent ID is gone** (§3.5: the restart fails and turns a running dependent into an exited one, E1). Log it as "needs recreate (M2)" and take no action. This covers a stale `NetworkMode` and the `No such container` stale-reference error.
- Skips: own container, oneoff, swarm, not opted in. Also opt-in labels, the action-in-flight window, the state key, rate limit, dry-run, the domain event stream (in memory), `/healthz` and JSON logs.
- Dockerfile: static binary, minimal image, read-only rootfs. Document socket access (group or root).
- CI: build, vet, unit tests, integration test (unhealthy container gets restarted; dry-run does nothing).
- **Done when:** the unit and integration tests pass, and `dokdosta run --dry-run` against a test stack logs the expected decisions.

### M0b — Scenarios M0 could not run (no app code; after M1, must finish before M2a)

M0 ran on a VM where the daemon could not be restarted and the journal was not readable (findings §6). M0b closes those gaps; its results and the proposed §3 changes are in [m0b-findings.md](m0b-findings.md). The scripts are in `test/m0/m0b/` (prepare, maintainer step, collect). It extends `test/m0` and `scripts/m0`; the safety rules of §0 apply unchanged, and it needs the maintainer's confirmation of the host.

- **[maintainer]** Daemon restart and host reboot (E9's last scenario) on the throwaway VM. Record:
  - the shutdown `kill` events and how the manual-stop markers (§3.3) clear on Docker's boot restart;
  - boot-time restore: `failed to start container` is logged and not retried, and `State.Error` is set;
  - the shared 5s `container:` wait (§3.5), and whether dependents come up before their parent.
- **[maintainer]** Journal evidence: run `scripts/m0/capture.sh` on a host where `journalctl -u docker` is readable, and confirm `failed to start container` and `restartmanger wait error` appear for E8 and for boot-time restore.
- **Classic image store:** switch the VM's `daemon.json` to the classic store (**[maintainer]**: needs a daemon restart), and run E5's image-drift part, ideally all of `run.sh`. Confirm what `com.docker.compose.image` holds there (§3.6). CI covers this too, so write the check so that CI can run it.
- **Engine 29.9.1** (containerd 2.4.2): upgrade the VM (**[maintainer]**: restarts the daemon) and re-run the suite. Findings §1 found no change in 29.9.1 that touches any experiment, so expect no differences.
- **Done (17 Oct 2026):** the verdicts were approved and are in this plan as v3.3. The CI `image-drift` job (it installs Engine 29.x and compose 5.6.0, because the runner's Engine 28.0.4 has no `image inspect --platform`) and the M0b self-test are in `ci.yml`.
- **Done when:** each item has an answer with scrubbed evidence in [m0b-findings.md](m0b-findings.md) (a separate file, not a section of `m0-findings.md`) and fixtures in `testdata/`, and §3.3, §3.5 and §3.6 are each marked "confirmed" or "needs change" for what M0b tested. Report anything that contradicts a plan rule before M2 starts.

### M2a — State, the full ladder, the CLI and shutdown handling (no compose)

Rung 2 does not exist yet. Where the ladder would recreate, it records "needs recreate (M2b)" as M1 does (a `RungUnavailable` event), and goes on to SUSPENDED. It still never API-restarts a dependent whose parent ID is gone.

- CI: the integration tests run on Docker Engine 29.x with compose v5.6.0 on both image stores, installed the way the `image-drift` job installs them (§6, *CI matrix*).
- The full ladder: cooldown backoff, recovery period, flap limit, SUSPENDED, PAUSED. Crash-loop detection by the §3.3 rule 3 definition (≥3 `RestartCount` increases, the first ≥ `CRASHLOOP_AFTER` ago, cleared after `RECOVERY_PERIOD` without one), independent of the inspect rate, with a test that varies the poll interval and `GRACE`. SUSPENDED clears as in §3.2.
- SQLite store; control socket and CLI (pause/resume/unsuspend/status).
- `State.Error` classification and the `heal-exited` modes; manual-stop markers.
- Shutdown handling (§3.3, *Manual-stop markers*): quarantine on stream loss, persisted boot id, restore window, the `last_seen` heartbeat for dokdosta's own end (v3.5), the sweep after every reconnect, "daemon not ready" for API errors during a restore. M1's tracker still reads a shutdown as manual stops (pinned in `rules_test.go`, `multiKeyEventVerdicts`); M2a changes that expectation on purpose. The throwaway-VM check of this (dokdosta in a container, `systemctl restart docker`) is `test/m0/m0b/dokdosta-restart/`.
- **Done when:** the M2a integration tests in §6 pass, state survives a dokdosta restart, and an unhealthy container goes restart → "needs recreate (M2b)" → SUSPENDED.

### M2b — Compose recreate, the drift guard and dependents (done; v3.6)

- `internal/compose`: binary lookup (§3.4), command builder, the dry-run oracle with its `config --hash` fallback (§3.6).
- Drift guard (§3.6), including the ref-holding skip.
- Dependents (§3.5): discovery, ordering, staleness sweep, the remedy mapping (recreate via non-forced `up` when the parent ID is gone, API restart otherwise), and the "needs recreate (M2b)" cases M1 only logs. A parent left down by policy leaves its dependents down (one notification).
- The startup version check and the per-rung minimums of §3.6 (one warning, rungs 2 and 3 disabled, ladder ends at notify + SUSPENDED after rung 1).
- Rule 1's third shape and the address-independent port matcher (M1's matcher already ignores the address); the image-drift comparator of §3.6. (The third shape is already pinned by the M0b fixtures and `rules_test.go`; this bullet stays whole because its second half is part of the drift guard.)
- CI: the full compose matrix of §6 (v5.2.0, v5.6.0, latest) on both stores.
- **Done when:** the M2b integration tests in §6 pass.

### M3 — Notifications (done; v3.7)
- Vet the Shoutrrr fork at the source: latest release, maintenance, licence. Done: §5.
- `internal/notify` as a subscriber of the event stream (§3.12): templates for action taken, recovered, giving up (with the cause), flapping, drift blocked (with the reason and the compose-upgrade hint), `NotifyOnly` (including a `restart: "no"` parent left down with its dependents), the version-gate warning, paused and resumed. Dedupe per incident, a per-URL rate limit, the optional uptime ping, `dokdosta notify test`, secrets never shown.
- Moved to M4 and done there: "upgraded during heal (with rollback command)" (§3.7), with `TagMoved`.
- **Done when:** notifications are verified against a local webhook receiver in CI (§6).

### M4 — Pull rung (done; v3.8)
- §3.7 end to end, including the `TagMoved` record, the "upgraded during heal" notification with its rollback, and the dependents step.
- **Done when:** the integration test against a local `registry:2` passes; with no newer image, the pull rung goes straight to SUSPENDED.

### M5 — Dashboard (read-only) (done; v3.9)
- §3.11: API, SSE, pages, token, optional `/metrics`, retention (v3.9).
- **Done when:** golden JSON tests pass, pages render against a fixture store, and the no-secrets-rendered test passes; and the CI integration test fetches `/api/v1/status` from the image with and without the token.

### M6 — Polish and public release (done; v3.10)
- Full README; docs (env and label reference, security, exit-code guide, deployment, upgrading, verifying and releasing). Done: v3.10 item 6.
- Example compose files (minimal, full with the host plugin, stacks mount and Tailscale on a shared bridge network, the demo) and a hardened systemd unit, tested in CI. Done: v3.10 item 7.
- GoReleaser, the release workflow, signing, SBOMs, provenance, the verify script, pinned actions and Dependabot. Done: v3.10 items 1 to 4.
- `SECURITY.md`, `CHANGELOG.md` (v0.1.0), `CONTRIBUTING.md`.
- Small candidate, built: the trusted-proxy option for the wrong-token limit (v3.10 item 5).
- Small candidate, **not built, post-v1 (R5)**: the read-only `DOCKER_CONFIG` mount for private registries (v3.10 item 8).
- **Done when:** a GoReleaser snapshot builds in CI from every commit, the examples pass their checks, and the maintainer's steps in [releasing.md](releasing.md) are documented.
- [maintainer] The release gate, the GitHub settings, the first tag, the announcement ([releasing.md](releasing.md)).

### M7 — Private registries for the pull rung (R5; done, v3.12)
- §3.7 private registries: `DOKDOSTA_REGISTRY_AUTH_DIR`, the validation (only `auths`, inline credentials, checked at startup and before each pull), the pull-only `DOCKER_CONFIG`, the redactor, the registry hosts in `status`, the API and the home page (v3.12 items 3 to 8).
- Prerequisite, done separately: the compose child never reads a Docker config (v3.12 item 1, pull request #28).
- The docs and examples of v3.12 item 10.
- **Done when:**
  - the unit tests of v3.12 item 9 pass, including the two secrets tests with planted registry credentials;
  - the integration tests against the htpasswd `registry:2` pass in CI on both image stores and the compose matrix: the positive control, the upgrade with the directory byte-identical afterwards, the refused `credsStore`, and the image run with the `:ro` mount;
  - the documentation states the risk.
- [maintainer] Review; whether and when to release it (v0.2.0, a new minor, so through the release gate in [releasing.md](releasing.md)); a check on a real host against a real private registry, if wanted.

## 8. Roadmap (post-v1)
- **R1 — Multi-server hub:**
  - One autonomous agent per host, plus `dokdosta hub`, which pulls each agent's `/api/v1` over the tailnet with per-agent tokens and a `since` cursor.
  - The hub never talks to Docker.
  - Rejected: a central controller driving remote Docker sockets.
- **R2 — Dashboard controls:** unsuspend, pause/resume, heal now. Needs auth and CSRF protection; every action goes into the history.
- **R3 — Hardening:** socket proxy, update rollback.
- **R5 — Private registries for the pull rung:** now milestone **M7** (v3.12).
- **R4 — Scheduled updates:**
  - Per-service schedule, `apply` or `notify` mode, drift guard first, dependents step, Updates page.
  - Revisit this against the Watchtower fork's feature set and the state of compose #13878 before building it.

## 9. Open questions
1. What makes the maintainer's stuck containers sit at `Exited (N)`? **Open.** Hypothesis A is reproduced (E8) and matches the moby source, but only `scripts/m0/scan.sh` and `capture.sh` on the real host can confirm it. The exit code `docker ps` shows will be the previous run's, not 128.
2. ~~`:ro` stack mounts?~~ **Answered:** they work, with the stacks root at the identical path. See [findings §5](m0-findings.md#5-answers-to-the-plans-open-questions-9) and [E6](m0-findings.md#e6-recreate-from-a-helper-container-with-the-stacks-directory-ro).
3. ~~Does the host compose plugin run in the image?~~ **Answered:** yes, in `alpine` and `distroless/static`; a non-root user needs the socket's group. See [findings §5](m0-findings.md#5-answers-to-the-plans-open-questions-9) and [E7](m0-findings.md#e7-does-the-host-compose-plugin-run-in-the-candidate-base-images).
4. ~~Should the dry-run plan replace the config-hash check?~~ **Answered:** it is the primary check for non-reference services and parents, with `config --hash` as the fallback (§3.6). See [findings §5](m0-findings.md#5-answers-to-the-plans-open-questions-9) and [E5](m0-findings.md#e5---dry-run-as-the-recreate-oracle).
5. ~~Does compose recreate dependents on a non-forced `up`?~~ **Answered:** yes after a parent recreate, no after a parent restart (E2). See [findings §5](m0-findings.md#5-answers-to-the-plans-open-questions-9).
6. Final defaults for grace, cooldown, crash-loop and rate limits (after M0).
7. ~~Confirm SQLite at the start of M2a.~~ **Answered (17 Oct 2026):** `modernc.org/sqlite` v1.60.1 (29 Sep 2026, `go 1.26.0`; pure Go, so the `CGO_ENABLED=0` build holds; about 19 MB binary). See [m2a-notes.md](m2a-notes.md).
8. ~~Image drift on Engines older than 28.1, and compose versions older than the matrix?~~ **Answered (17 Oct 2026):** rung 1 keeps Engine v25 as the minimum. Rungs 2 and 3 and the drift guard require Engine ≥ 28.1 and Compose ≥ v5.2.0. dokdosta checks both at startup; below that it logs one warning, disables rungs 2 and 3, and the ladder ends with notify + SUSPENDED after rung 1. No store-guessing fallback. See §3.6, *Minimum versions per rung*.

---

## Appendix A — README.md (milestone H)

```markdown
# whalenurse

**Compose-aware self-healing for Docker hosts.**

When an opted-in container turns unhealthy, or stops and stays stopped, whalenurse restarts it. If that doesn't help, it recreates it from the stack's own compose files, together with any `network_mode: service:` sidecars that depend on it. If that fails too, it stops trying and tells you why.

> **Status: design phase.** There is no code or release yet; nothing below works today. The design and milestones are in [docs/PLAN.md](docs/PLAN.md).

## Why

Docker doesn't restart unhealthy containers. A restart policy also gives up for good when a start fails, which is common at boot when a mount or network isn't ready yet. Existing tools restart containers, but a restart isn't enough when a container has to be recreated, and it leaves sidecars that share its network namespace broken. whalenurse targets that gap:

- **Recreates from your compose files.** It uses the project, files and env file recorded on the container, and never runs `compose down`.
- **Handles sidecars.** After their parent is restarted or recreated, it brings `network_mode` dependents back in order.
- **Never upgrades by accident.** A drift guard refuses to recreate when the compose files or the image tag changed since the container was created.

## Planned features

- Opt-in per container with the `whalenurse.enable=true` label
- Escalation: restart → recreate → optional pull and recreate → notify and suspend
- Backoff, flap limit, global rate limit, pause (maintenance) mode and dry-run
- Notifications through Shoutrrr (ntfy, Gotify, Discord, Telegram and more)
- Optional read-only dashboard and JSON API
- Single static Go binary that runs as a container or under systemd

## Security

whalenurse needs access to the Docker socket, which is root-equivalent on the host. The dashboard will be off by default and read-only; expose it only on localhost, behind a reverse proxy or on a tailnet.

## Requirements (planned)

- Docker Engine v25 or later (restart only); Engine 28.1 or later and Compose v5.2.0 or later for recreate, pull and the drift guard
- The Docker Compose plugin on the host
- Compose stacks in plain directories on the host

## License

MIT
```

## Appendix B — `.gitignore` additions (milestone H)

```gitignore
# Build output
/whalenurse
/dist/

# Local runtime state
/data/
*.db
*.sqlite*

# Raw M0 captures (docker inspect/events/journal). Scrub before moving into testdata/.
/captures/
```

## Appendix C — GitHub About and topics ([maintainer], milestone H)

- **About:** Compose-aware self-healing for Docker: restarts unhealthy or stopped containers, then recreates them from their own compose files, including network_mode sidecars. Opt-in via labels. Work in progress.
- **Topics:** `docker`, `docker-compose`, `self-healing`, `healthcheck`, `autoheal`, `homelab`, `self-hosted`, `golang`
