# Releasing

How a release is made, and the steps that only the maintainer can do (marked **[maintainer]**: GitHub settings, anything on
the real Docker host, and the decision to tag). CI on pull requests and `main` publishes nothing: **only a `v*` tag publishes,
and only the maintainer decides to make one.** The maintainer tags by hand ([Cutting a release](#cutting-a-release-maintainer)),
or tells an agent (for example an autonomous Claude Code session) to make one specific tag, which it then makes the same way.
An agent never tags on its own initiative, as a side effect of another task, or because the checks are green.

## What the pipeline does

`.github/workflows/release.yml` runs on a `v*` tag, and only that:

1. `test` job (`contents: read`): the tag looks like `v0.1.0` or `v0.2.0-rc.1`, it points at a commit **on `main`**,
   `CHANGELOG.md` has a **dated** section for it, then `go vet`, the unit tests with `-race` and the release scripts' self-tests.
2. `publish` job, which needs `test`. It is the only job in the repository with `packages: write` and `id-token: write`, plus
   `contents: write` (create the release) and `attestations: write` (store the provenance). It installs cosign v3.1.3 and syft
   v1.55.0, logs in to GHCR with the workflow's `GITHUB_TOKEN`, and runs GoReleaser v2.18.3, which
   - builds `linux/amd64` and `linux/arm64` binaries (`CGO_ENABLED=0`, `-trimpath`, stripped, the version stamped in) into
     `.tar.gz` archives with the README, LICENSE, CHANGELOG and the systemd unit, a syft SBOM per archive, and `checksums.txt`;
   - builds and pushes the multi-arch image `ghcr.io/jackspiering/whalenurse` from `Dockerfile.release` (which only copies the
     binaries; the bundled compose is downloaded and sha256-checked in a build-platform stage, so no emulation is needed) with
     the tags `vX.Y.Z`, `vX.Y`, `vX` and `latest` (a pre-release like `v0.2.0-rc.1` gets only its own tag and never moves the
     others);
   - signs `checksums.txt` (`checksums.txt.sigstore.json`) and every image digest with cosign, keyless;
   - creates the GitHub release with the section of `CHANGELOG.md` as its notes (`scripts/release/release-notes.sh`, which
     fails if the section is missing, empty or undated), marked a pre-release for `-rc` tags.
3. `actions/attest` stores build provenance for the archives and SBOMs (from `checksums.txt`) and the image (from `digests.txt`).
4. `scripts/release/verify.sh` runs against what was just published, so a pipeline that signed nothing fails loudly.

Pull requests and `main` run `ci.yml`, which includes a **GoReleaser snapshot**: the same archives, checksums, SBOMs and
image are built from the commit, with `contents: read` only, no registry login, no cosign and no OIDC. Nothing is pushed or
signed outside a tag.

Every action in every workflow is pinned to a commit SHA (`scripts/ci/check-pins.sh` fails otherwise); Dependabot updates them,
the Go modules, the Dockerfiles and the example images weekly, grouped.

Verify a release yourself: [verifying-releases.md](verifying-releases.md).

## The release gate

**No new minor or major version is tagged until it has run for a few days in dry-run on a real host, and then live on a few
test containers** (v0.1.0 went through this gate). **[maintainer]**

1. Run the image built from `main` (`docker build -t whalenurse .`, or a snapshot) on your real host with
   `WHALENURSE_DRY_RUN=true` and notifications on. Read every `IncidentOpened`, `would restart` and `DriftBlocked` for a few days,
   across at least one host reboot or daemon restart, a deploy of one of your stacks, and a night.
2. Switch to live for a few test containers (a stack you can afford to break; the [demo](../examples/demo/compose.yaml) is
   good for rehearsal). Break them on purpose; check the ladder, the sidecar repair, the notifications and `unsuspend`.
3. Fix what you found, and write it into the changelog.

Do not skip this because CI is green. CI runs on throwaway runners; what it cannot show you is your stacks.

## One-time setup before the first tag **[maintainer]**

### Repository settings

- **Branch protection on `main`** (Settings, Rules, Rulesets): require a pull request before merging, require status checks to
  pass, block force pushes and deletion. Required checks: `build, vet, unit tests`, `shellcheck and M0 self-tests`,
  `workflows (actionlint, actions pinned to SHAs)`, `GoReleaser snapshot (builds everything, publishes nothing)`,
  `examples (compose config, systemd-analyze verify)`, `demo (examples/demo, break bop and watch the ladder)`, the six
  `integration (...)` legs and the two `image-drift check (...)` legs. (The check names are the jobs' `name:` in `ci.yml`. The six `integration (...)` checks are aggregate jobs that wait for the
  three `integration shard ... ` jobs each leg is split into; require the aggregates, not the shards. On a pull request that changes only documentation most of these checks
  report "skipped", which counts as passed; the integration and image-drift legs pass only if the `scope` job said so.)
- **Tag protection for `v*`**: a tag ruleset (Settings, Rules, Rulesets, target: tags, pattern `v*`) that restricts who can create
  them to you, and blocks updates and deletions. A release tag is the trigger of the only workflow that publishes; whoever can
  push one can publish.
- **Immutable releases** (Settings, General): once published, a release's assets and tag can never change. GoReleaser creates the
  release as a draft, uploads, then publishes, so it works with this setting.
- **Actions**: workflow permissions default to read-only (the workflows ask for what they need); "Require approval for
  workflows from outside collaborators". Consider restricting allowed actions to the pinned ones.
- **Private vulnerability reporting** on (Settings, Advanced Security): [SECURITY.md](../SECURITY.md) tells reporters to use it.
- **Dependabot alerts and security updates** on; `.github/dependabot.yml` is in the repository.

### The GHCR package

The first push creates `ghcr.io/jackspiering/whalenurse` as a **private** package. After the first publish:

1. Open the package (your profile, Packages, `whalenurse`). It should already be linked to this repository: the image carries
   `org.opencontainers.image.source`, which links it. If not, "Connect repository".
2. **Make it public** (Package settings, Danger zone, Change visibility). Public packages cannot be made private again.
3. Under "Manage Actions access", make sure this repository has **write** access (it does when the package is linked).
4. Test as a stranger: `docker logout ghcr.io && docker pull ghcr.io/jackspiering/whalenurse:v0.1.0`, then
   `make verify-release TAG=v0.1.0`.

## Cutting a release **[maintainer]**

1. The pull request that prepares it merges to `main` with green CI. In `CHANGELOG.md`, replace `YYYY-MM-DD` of the version with
   today's date (the release workflow refuses an undated section) and make sure its entries are right. Check the
   links at the bottom of the file.
2. **Rehearse with a release candidate first**, because the first run of the signing steps happens on a real tag and cannot be
   tried anywhere else (the only job with `id-token: write` is the publish job). Give the changelog a `## [0.1.0-rc.1] - date`
   section, then:
   ```
   git switch main && git pull
   git tag -s v0.1.0-rc.1 -m "v0.1.0-rc.1"      # or: git tag -a (a signed tag is better)
   git push git@github.com:jackspiering/whalenurse.git v0.1.0-rc.1
   ```
   Watch the `release` workflow. A `-rc` tag publishes `v0.1.0-rc.1` only (never `latest`, `v0.1`, `v0`), as a GitHub
   pre-release. Run `make verify-release TAG=v0.1.0-rc.1`. If a step fails, fix it on `main` and tag `rc.2`: **a tag that
   was published is never reused** (immutable releases would refuse it anyway).
3. Tag the real release the same way (`v0.1.0`), push it, watch the workflow, run `make verify-release TAG=v0.1.0`, do the
   anonymous pull above.
4. Announce.

Steps 2 and 3 can be delegated: tell an agent which tag to make ("tag and push v0.1.1"). It makes that tag and no other, from
`main`, only if CI is green on the head commit and the changelog section is dated, and it stops and reports instead of fixing
anything on its own. The tag ruleset still decides who can push a `v*` tag, so the agent pushes with the maintainer's
credentials.

If the `publish` job fails after the image was pushed but before the release exists, the image tag is already on GHCR: delete
that package version (Packages, Versions) before retrying, or cut the next tag. If it fails at the final "Verify what was
published" step, the release is public but signing or provenance did not work: say so in the release notes, or yank and re-release as the
next patch.

## Why these permissions

| Permission | Job | Needed for |
|---|---|---|
| `contents: read` | `test`, and every job in `ci.yml` | checkout |
| `contents: write` | `publish` | creating the release and uploading its assets |
| `packages: write` | `publish` | pushing the image to GHCR |
| `id-token: write` | `publish` | the OIDC identity of the keyless signatures and attestations |
| `attestations: write` | `publish` | storing build provenance |

The brief for M6 named `packages: write` and `id-token: write` as the publish job's; the release and the attestations need the
other two, and no other job has any of the four.
