# M6 notes

Decisions, deviations and open questions for milestone M6 (polish and public release) of [PLAN.md](PLAN.md). The rules are in
the plan as **v3.10** (written by this build; it awaits the maintainer's review). **Nothing was tagged, released or pushed to
GHCR.**

## Upstream check (19 and 20 Oct 2026)

Versions read from the GitHub releases API and the registries on 19 Oct 2026, and the docs and source of GoReleaser at `main`
(the fields used were checked in `internal/pipe/...` of the GoReleaser source, not only in the docs).

| What | Version | Date | Pinned as |
|---|---|---|---|
| Go | 1.27.2 (`go.dev/dl`, stable; unchanged since M5) | | `go.mod`, the `golang:1.27.2` image by digest |
| GoReleaser | v2.18.3 | 9 Oct 2026 | `version: v2.18.3` in the workflows |
| `goreleaser/goreleaser-action` | v7.3.0 | 12 Oct | `a4f02ab5355a589de60d917a659ce617ed19b3de` |
| cosign | v3.1.3 | 6 Aug | `cosign-release: v3.1.3` |
| `sigstore/cosign-installer` | v4.1.2 | 7 May | `6f9f17788090df1f26f669e9d70d6ae9567deba6` |
| syft | v1.55.0 | 19 Oct | `syft-version: v1.55.0` |
| `anchore/sbom-action` (`download-syft`) | v0.24.3 | 2 Oct | `66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c` |
| `actions/attest` | v4.2.2 | 4 Aug | `1e69f48acb82d1966a394da916b4c1698aa569d6` |
| `actions/checkout` | v7.0.1 | 20 Jul | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | v7.0.0 | 16 Jul | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `docker/setup-buildx-action` | v4.4.1 | 16 Sep | `f87e5991a6d7451dcb8d9637bfbc97413f497069` |
| `docker/login-action` | v4.6.0 | 29 Jul | `dbcb813823bdd20940b903addbd779551569679f` |
| `rhysd/actionlint` | v1.7.12 | 30 Mar | `go run ...@v1.7.12` in CI |
| `tailscale/tailscale` image (example) | v1.104.1 | | `examples/full/compose.yaml` |
| `docker/compose` bundled | v5.6.0 (still the latest) | 2 Oct | unchanged |
| bop image (demo) | `ghcr.io/jackspiering/bop:0.1.0`, public (an anonymous token pulls its manifest) | | `examples/demo` |

Also checked: `actions/attest-build-provenance` v4.2.2 exists, but the GoReleaser attestation docs use `actions/attest` directly, so
that is what the workflow uses. Each SHA was resolved with `gh api repos/<r>/commits/<tag>` (the commit, not the tag object of an
annotated tag). The base-image digests (`golang:1.27.2`, `gcr.io/distroless/static:nonroot`) are those of the multi-arch indexes,
read from the registries on 19 Oct.

Facts from the GoReleaser source that the configuration rests on: `dockers_v2` images are built in the **publish** phase (a
`--skip=publish` run does not build them, so the CI snapshot runs without that skip: a snapshot builds per-platform images locally
and pushes nothing); `docker_signs` with `artifacts: all` signs `DockerImageV2` artifacts with the digest known from the push;
`sboms` with `artifacts: archive` writes one document per archive and these are among the artifacts the checksum file covers
(the snapshot build shows it, and CI asserts it); the release is **always created as a draft, uploaded, then published**, so the
repository's *immutable releases* setting works (an existing immutable release makes GoReleaser fail, it does not alter it).
`cosign sign-blob --bundle` and `verify-blob --bundle` exist in cosign v3.1.3 (checked with `--help` on a local build).

## Decisions the brief leaves open

1. **`Dockerfile.release` next to `Dockerfile`.** `dockers_v2` builds from binaries GoReleaser already compiled, so its Dockerfile
   only copies; the existing `Dockerfile` compiles from source and stays the CI and development image (the integration tests
   need an image built from the commit without GoReleaser). `scripts/ci/check-dockerfiles.sh` compares the compose release, both
   checksums and the base-image digests of the two. Both base images are now pinned by digest (a change to `Dockerfile`: the
   behaviour is the same, Dependabot's docker ecosystem updates the digest).
2. **No QEMU.** The final stage has no `RUN`, and the compose download stage is `FROM --platform=$BUILDPLATFORM` and picks the
   asset by `TARGETARCH`, so a two-platform build needs a multi-platform builder (`docker/setup-buildx-action`) and nothing
   emulated. CI builds the arm64 image once to show it (`--output type=cacheonly`).
3. **Four permissions for the publish job, not two** (the brief: "the only one with `packages: write` and `id-token: write`").
   Creating the release needs `contents: write` and the provenance needs `attestations: write`. They are in that one job only;
   the table is in [releasing.md](releasing.md#why-these-permissions). `actions/attest` has `create-storage-record: false`,
   which is the input that would otherwise ask for `artifact-metadata: write`.
4. **Provenance through `actions/attest`** (`subject-checksums` on `checksums.txt` for the archives and SBOMs, and on
   `digests.txt` for the image), not a Pro-only GoReleaser feature (GoReleaser's attestation page does the same). It is stored
   in GitHub's attestation store, not pushed to the registry, so `gh attestation verify oci://...` finds it by digest. "SBOM
   (syft)": a syft SPDX document per archive, listed in the signed checksums; for the image, `dockers_v2`'s own `sbom: true`
   (BuildKit's SBOM attestation in the index, which is syft-based).
5. **Release notes from `CHANGELOG.md`** with `scripts/release/release-notes.sh` (fails closed: a missing, undated or empty
   section stops the release), passed to GoReleaser with `--release-notes`; GoReleaser's own changelog is off. The version
   section is committed with the placeholder date `YYYY-MM-DD`, which the script refuses, so the maintainer has to set it
   before tagging: the gate cannot be skipped by accident.
6. **The tag must be on `main`** (the `test` job fails otherwise), the same rule bop uses, so a tag on a feature branch publishes
   nothing.
7. **`verify.sh` is exact, not a regexp**: it pins the certificate identity to this repository's `release.yml` at the tag. The
   release workflow runs it at the end, against what it published, so a pipeline that signed nothing fails.
8. **The demo uses the real bop image**, `ghcr.io/jackspiering/bop:0.1.0`, because it is public. `sticky=true` is what makes the
   restart rung insufficient (a restart keeps the sticky mode, a recreate does not) so the demo shows the whole ladder. The
   demo's variables go in a `.env` next to the file, because compose interpolates the whole file whenever whalenurse recreates
   one service of it: `${DOCKER_GID:?}` would otherwise fail inside whalenurse's compose child, which has no such variable. The
   `.env` is recorded on the containers (`com.docker.compose.project.environment_file`) and read through the stacks mount.
9. **The Tailscale example** uses a serve config file (`tailscale/serve.json`, `${TS_CERT_DOMAIN}` is substituted by containerboot:
   checked in `cmd/containerboot/serve.go`), `tailscale/tailscale:v1.104.1`, no published port, one shared bridge network
   (`dashboard`). A token is required in that example (the listener is not loopback-only).
10. **The systemd unit** runs as a dedicated `whalenurse` user in the `docker` group, with `NoNewPrivileges`, `ProtectSystem=strict`,
    `ProtectHome=read-only` (so the stacks under `/home` stay readable), no capabilities, `RestrictAddressFamilies`,
    `MemoryDenyWriteExecute`, `@system-service` with `SystemCallErrorNumber=EPERM` (so a syscall that Go or compose needs and the
    filter lacks fails with an error in the journal instead of killing the process), and `StateDirectory`/`RuntimeDirectory`.
    `ProcSubset=pid` is **not** set (whalenurse reads `/proc/sys/kernel/random/boot_id`). `systemd-analyze security --offline`
    scores it 1.4 OK. **The unit was verified, not run.**
11. **Trusted proxies** (the small candidate that was built): see plan v3.10 item 5. Chosen: the right-most untrusted entry of all
    `X-Forwarded-For` lines; an unparsable entry gives up on the header (the proxy's own address, the strict side); a peer
    outside the list is never believed; `/0` is a config error. Not built: `X-Real-IP`, `Forwarded` (RFC 7239) and the PROXY
    protocol; the proxy must append to `X-Forwarded-For`, which nginx, Caddy and Traefik do.
12. **Not built, post-v1 (plan R5): the read-only `DOCKER_CONFIG` mount.** It reverses a plan rule (v3.6 item 1: the compose child runs
    without `DOCKER_CONFIG`), it puts registry credentials in a container that holds the Docker socket, and it is only trustworthy
    with an integration test against an authenticated `registry:2` (htpasswd) that proves compose pulls with a read-only,
    inline-`auths` config and that nothing else in the config directory is written. That is not small. A design sketch for R5:
    `WHALENURSE_DOCKER_CONFIG=/docker-config` (the path of a read-only mount holding `config.json` with inline `auths`; credential
    helpers do not exist in the image), passed to the compose child **for the pull command only**, the guard test extended to
    accept the authenticated registry, and the risk written in `docs/security.md`.
13. **`docs/running.md` stays** as the detailed behaviour reference and now points at the new pages; its settings table is
    replaced by a link, so `configuration.md` is the one source of truth (a test keeps it complete). Its stale line "there is no
    limit on attempts" (true before M5's limiter) is fixed.
14. **actionlint found an old problem in `ci.yml`**: a comment starting "shellcheck's" was parsed as a directive and had hidden an
    SC2086 note; the comment is reworded and the intended `disable=SC2086` written. The `workflows` job now runs actionlint.
15. A broken anchor in `docs/m0-findings.md` (`#verdicts`, the heading is "4. Verdicts") was fixed because the new link test
    found it.
16. **Image annotations are scoped** (found by the first CI run of the snapshot job): a snapshot builds one image per platform and
    BuildKit refuses *index* annotations for a single-platform export ("index annotations not supported for single platform
    export"). The per-platform `manifest:` annotations are always set; the `index:` ones are templated to an empty value in a
    snapshot (GoReleaser ignores empty values) and set on a real release. Checked by rendering the buildx command of a snapshot
    with a stub `docker` on `PATH` (six `manifest:` annotations, no `index:`); the real release's index annotations are not
    rendered anywhere until a tag, which is one more reason for the `-rc` rehearsal. The labels (`org.opencontainers.image.source`
    among them) are set in both.
17. **The demo sets `WHALENURSE_STARTUP_SETTLE: 10s`** (the 2 m default is for a host that has just booted), and the CI demo job
    waits for the dashboard to list bop rather than assuming it does at once (found by the first run of the demo job, which
    got as far as "everything healthy, dashboard answers" on the image built from the commit).
18. **The demo job's second run found the recreate refused** ("compose inputs ... are not readable from here ... permission denied"):
    the test script's `mktemp -d` directory is mode 0700, so uid 65532 in the container could not read the compose file. The
    script now `chmod 0755`s it. The behaviour is the documented one (the stacks must be readable by uid 65532), and a good
    check of the message: it names the file and the fix. A directory from `git clone` is 0755; mind this if your stacks live in a
    0700 directory.
19. **Found by reading it back, not by CI** (the review pass before the hand-over): the Tailscale container had `hostname: whalenurse`,
    which Docker writes into its own `/etc/hosts`, so `serve.json`'s `http://whalenurse:8484` would have resolved to itself (the line
    is gone; `TS_HOSTNAME` names the tailnet node); the README said the demo "only touches its own project", but in label mode
    its whalenurse acts on any opted-in container on the host (reworded, with a warning not to run it beside another whalenurse);
    the systemd unit moves the control socket, so the CLI needs `WHALENURSE_CONTROL_SOCKET` set (header and `deployment.md`);
    cosign is documented as v3 or later, because v2 was not tested against v3's bundles and OCI-referrer signatures; the systemd
    compose lookup list in `deployment.md` is now exactly `HostPluginDirs` of `internal/compose/binary.go`; and the README's
    demo ends on the dashboard, as the brief asked. The Tailscale example is **checked by `compose config -q` only**; it has not
    been run against a tailnet.

## What was verified, and how

Locally (no Docker daemon was used; no integration test was run, per the working protocol):

- `go vet ./...` (also `-tags integration`), `go test -race ./...` all green; `gofmt` clean.
- **Mutation checks** of the trusted-proxy tests: walking the header left to right fails 7 tests, trusting every peer fails 5;
  removing a row from `configuration.md` or adding a bogus label row to `labels.md` fails the documentation test.
- `scripts/release/release-notes.sh --self-test` (11 cases) and `scripts/release/verify.sh --self-test` (21 cases, stubbed
  `cosign`, `gh` and `curl`), `scripts/ci/check-dockerfiles.sh` (and its failure on a changed compose version and on an
  unpinned base image), `scripts/ci/check-pins.sh` (and its failure on a tag ref), `shellcheck -x` over all scripts, `actionlint`
  over all workflows.
- **GoReleaser v2.18.3, built from source with syft v1.55.0**: `goreleaser check` passes, and `goreleaser release --snapshot
  --skip=sign,docker,publish` builds both archives, both SPDX SBOMs and a `checksums.txt` that lists all four; the amd64 binary
  prints `whalenurse 0.0.0-SNAPSHOT-<sha>` and the arm64 one is a static ELF.
- `docker compose config -q` on the three compose examples (with their variables, and without them: the message says a required variable is missing; which one it names first depends on the compose version),
  `systemd-analyze verify` on the unit (clean, pointed at a real binary; with a typo'd directive it prints a
  warning, which is why CI fails on any output) and `systemd-analyze --offline=true security` (1.4).
- `cosign sign-blob --help` / `verify-blob --help` show the `--bundle` and `--yes` flags the configuration uses.

In CI: **run 39335333170 on 7e84b87: all 18 checks green** (the commit before the final documentation edits; those touch no
code or workflow: the Tailscale example's hostname, the README demo text, the systemd unit's header comment, three doc lines and
this file), including the six integration legs (2 image stores x compose 5.2.0, 5.6.0, latest, about 15 minutes each), the two
`image-drift` legs, CodeQL, and the four jobs new in this milestone: **workflows** (24 s), **GoReleaser snapshot** (1 m 27 s: both
archives, both SBOMs, the checksums, the amd64 image run and inspected, the arm64 image built), **examples** (22 s) and **demo**
(2 m 43 s: bop broken, restarted, recreated in 26 s, the sidecar recreated behind it, the history in the API; about 50 s from the
break to a healthy stack). Three earlier runs failed and led to fixes 14, 16, 17 and 18 above (the first run's failures: the
examples job, where the runner's older compose names a different missing variable first, the snapshot's index annotations, the
demo's race and its 0700 directory). The final push only changes documentation and example text, so its run repeats the same
checks.


## Found by the release-gate rehearsal (20 Oct 2026)

Run on the maintainer's host (Engine 29.9.1, compose 5.6.0, image store containerd): whalenurse live over a stack with `bop`, a
`network_mode: service:bop` sidecar with no opt-in, and a private ntfy; a dry-run pass, then live, then a reboot.

- **Passed:** the dry-run decisions and `[dry-run]` messages; the live ladder (restart, recreate, the sidecar recreated as a stale
  dependent); the ntfy messages; a deliberate `docker stop` left alone; a host reboot (new boot id, restore window, nothing
  healed, nothing notified, all containers back).
- **Bug, fixed:** the incident the dependents step opens for a sidecar that is not opted in never closed. `rules.Classify` skips
  a non-opted-in container unless the decision is a dependents-step action, and a healthy sidecar's decision is "OK", so the
  skip kept it from `engine.Evaluate` and so from `recovering()`. The next parent recreate then found the sidecar's incident
  "rung 2 done" and suspended it without repairing it. The fix: `rules.Input.IncidentOpen`; a decision that needs no action
  passes while the incident is open. The test (`TestDependentIncidentClosesAndTheNextParentRecreateRepairsItAgain`) failed
  before the fix. It is not a plan rule change: plan §3.5 already says the dependents step repairs a sidecar every time.

## Not verified

- **The signing and attestation steps have never run.** They need the OIDC token of the publish job, which only a `v*` tag
  run has, so nothing here can exercise them: not CI, not a laptop. The configuration follows GoReleaser's and cosign's
  documentation and the flags were checked against a local cosign, and `verify.sh` runs at the end of the release and would
  fail it. **Rehearse with a `v0.1.0-rc.1` tag first** ([releasing.md](releasing.md#cutting-a-release-maintainer)); an `-rc` tag
  moves no stable tag and makes a pre-release.
- **The multi-arch image push and the GHCR package settings** (public, linked): [maintainer] steps, after the first push.
- The systemd unit has not been *started* on a host: its hardening could block something whalenurse does on a real machine
  (the filter errors with `EPERM` rather than killing, so it would show in the journal). A maintainer run on the real host is part
  of the release gate.
- The demo has been proven by the CI job only (run 39335333170); it was not run on this machine.
- `README.md`'s mermaid diagram was checked by reading, not rendered; GitHub renders it on the page.
- The screenshots in the README are those of M5 (retaken on 19 Oct 2026); the dashboard has not been seen on a real phone.

## Open questions for the maintainer

1. **The four permissions** (decision 3): fine, or would you rather split the release from the provenance into two jobs?
2. **Immutable releases and tag protection** are recommended in `releasing.md`; both are your settings to make.
3. **The rehearsal tag**: `v0.1.0-rc.1` before `v0.1.0` (recommended, because the first signing run cannot be tried earlier).
4. **Post-v1 list** (below): is R5 (private registries) the right next item, or something else?

## Post-v1 (not done in M6)

- **R5, the read-only `DOCKER_CONFIG` mount** for private registries (decision 12).
- Rehearsing the signing steps without a tag. Not possible while only the publish job may have `id-token: write`; the `-rc` tag
  is the rehearsal.
- `X-Real-IP`, RFC 7239 `Forwarded` and the PROXY protocol for the trusted-proxy option.
- A dashboard check on real phones; a screenshot gallery in the docs.
- The earlier roadmap: the multi-host hub (R1), dashboard controls (R2), hardening such as a socket proxy (R3), scheduled updates (R4).
