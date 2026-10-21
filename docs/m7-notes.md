# M7 notes

Decisions, deviations and open questions for milestone M7 (private registries for the pull rung, R5) of [PLAN.md](PLAN.md).
The rules are in the plan as **v3.12**, approved by the maintainer on 21 Oct 2026. **Nothing was tagged or released.** The
integration tests of this milestone were **written and compiled (`go vet -tags integration`) but not run**: they need a Docker
host, and the maintainer has not confirmed one; they run in CI.

## Upstream check

- `golang.org/x/crypto` v0.58.0 is the latest tag (checked 21 Oct 2026). It was an indirect dependency; the integration tests
  use `bcrypt` to write the htpasswd file, so it is now a direct requirement. Nothing in the shipped binary imports it (the
  tests are behind the `integration` build tag).
- docker/cli v29.6.0 (compose v5.2.0's) and master agree on what M7 relies on: `config.Dir()` reads `DOCKER_CONFIG`, a config
  with `auths` that has any entry skips the credential-store detection (`ContainsAuth`), and an absent file is a default config.
- Plan v3.12 item 9 says `registry:2` "no longer ships `htpasswd`". **That was not checked** (no image was pulled to look). The
  choice of `bcrypt` in the test is independent of it: no second image and no shelling out. Treat the sentence as unverified.

## What was built

- `compose.LoadRegistryAuth` (`internal/compose/registryauth.go`): the validation of plan item 4. The top level may hold only
  `auths`; each entry needs `auth` (base64, decoding to `user:password`), `username` with `password`, or `identitytoken`; an
  empty or missing `auths`, a `contexts` entry in the directory, a file that is not a JSON object are refused. Every error
  names the problem (a key at most, cut at 40 characters) and never a value, and JSON's own errors are not passed on (they can
  quote a byte of the file). It returns the registry hosts (sorted), the secrets, and warnings (the directory or the file is
  writable by the process, by `access(2)`).
- `Command.ConfigDir`, `ExecRunner.ConfigDir`, `Compose.WithRegistryAuth`: `Compose.Pull` re-runs `LoadRegistryAuth` and, if it
  passes, sets the command's `ConfigDir` to the directory. Nothing else sets it. A file that went bad since startup is a failed
  pull with `registry credentials: ...` and compose is not run.
- `WHALENURSE_REGISTRY_AUTH_DIR` in `config`; `run` loads it **before anything else is wired** and fails with
  `WHALENURSE_REGISTRY_AUTH_DIR: <reason>`. Warnings are logged; one info line lists the registries.
- `notify.NewRedactorWith(secrets, urls...)`: the credentials go in as literal secrets (whole, query-escaped and
  path-escaped). `setupNotify` takes them, so the one redactor that the API service, the logs and the notifier share knows them.
- Shown: `status` (`pull credentials: a, b`), `GET /api/v1/status` (`pull_auth.registries`, always an array), the home page
  ("Pull credentials"), hosts only, capped at 253 characters each. The `status.json` golden gained the field.
- Tests: the loader (accepted forms, 13 refusals, no value in any error, the directory itself, the warning); only `Pull` gets
  the directory, and a file that went bad is a failed pull; the redactor; the two secrets tests (`cmd/whalenurse`,
  `internal/daemon`) plant the user, the password, the identity token and the `auth` value, each **mutation-checked** (without
  the credentials in the redactor, the daemon test reports 27 leaks and the `cmd` test 3; with them, none); the harness guard
  (`ComposeRunner` lets only a pull use a credentials directory, and only the registered one; `Guard.PushWith` takes only a
  `whalenurse-test-docker-config-*` directory under the temp directory); and the four integration tests below.
- Integration tests (`test/integration/registry_auth_test.go`): `registry:2` with htpasswd auth, as a `whalenurse-test-registry-*`
  project on `127.0.0.1:<port>`. (1) **positive control**: without the variable the pull fails with a login error, the app ends
  SUSPENDED, the local image and the container are unchanged; (2) with the directory (mode 0555, file 0444) the pull upgrades
  and recovers, the pull alone ran with the directory, and a snapshot of the directory (modes, sizes, sha256, mtimes) is
  identical afterwards; (3) the image (read-only root, uid 65532, the directory mounted `:ro`, checked with `docker inspect`):
  same, plus the password and the `auth` value are in neither the log nor `status`, and `status` names the registry and not the
  user; (4) the image refuses a file with `credsStore`, names the key, does not leak the password.

## Decisions and deviations

1. **User names are not redacted, and not shown.** The first version of this milestone also added the registry user name to the
   redactor; that was wrong and was removed before merge. On GHCR and Docker Hub the login user is usually the image
   namespace (`ghcr.io/<user>/app`), so masking it would turn every image reference, drift message and pasteable rollback
   command into `ghcr.io/***/app`. The redactor learns the `auth` value, `user:password`, the password and the identity token.
   `status`, `/api/v1/status` and the home page never show the user name (tests check this), but compose's own error text may
   echo it, and that is let through. `TestRedactorLeavesImageReferencesOfTheRegistryUserAlone` is the regression test.
2. **The environment of the compose child is built per command, not per runner**: `Command.ConfigDir` overrides the runner's
   private empty directory (#28) for one command. This keeps `ChildEnv` the one place that sets `DOCKER_CONFIG`.
3. **The writability warning uses `access(2)`**, so it fires for the in-process integration tests and for a development run
   as root, and does not fire on a `:ro` mount (EROFS). It is a warning, never an error.
4. **No warning for "credentials set but no container has `heal.pull`"**: the variable is cheap to leave set, and a container
   can opt in later.
5. **The harness logs in with a written config, not `docker login`.** Plan item 9 has the Guard accept `docker login` with a
   temporary `--config` directory. `Guard.PushWith` instead takes a `whalenurse-test-docker-config-*` directory under the temp
   directory into which the test has written the same `auths` entry, and runs `docker --config <dir> push`. No `login` verb is
   allowed through the Guard at all.
6. **The Compose-side `Guard` (integration harness) refuses a `ConfigDir` on anything but the registered pull**, so a test that
   hands credentials to the wrong command fails closed.
7. **CI durations**: the four new integration tests are not in `scripts/ci/integration-durations.txt`, so they count as 30 s
   each when the shards are packed. Measure them on the first CI run and add them.

## Uncertain

- The integration tests have not run. Likeliest places to need a fix on the first run: the wait for the registry (it expects
  401 without credentials), compose's login against an HTTP registry on 127.0.0.1 through the daemon (the Engine treats
  127.0.0.0/8 as insecure; the M4 registry relies on the same), the exact wording of the login failure in test 1 (the
  assertion accepts `unauthorized`, `no basic auth`, `denied`, `authentication`), and the time the image test needs (300 s).
- The containerd image store: the M4 tests run on both stores, and these should behave the same; not seen.
- Whether compose v5.2.0 to v5.6.0 write into `DOCKER_CONFIG` during a pull. #28's CI showed a pull passing with an uncreatable
  directory on 5.2.0, which suggests not; test 2 and 3 assert it with the snapshot, on the whole matrix.

## Open for the maintainer

1. Review the PR; run the integration tests (CI does) and read test 1's log to see the actual login failure text.
2. Release: this is a new minor (v0.2.0), so it goes through the release gate in [releasing.md](releasing.md) first. The tag is
   yours.
3. A real private registry (GHCR with a `read:packages` token, say) has not been tried; only `registry:2` with htpasswd has.
