#!/usr/bin/env bash
# Rules about what the workflows' steps say, beyond the SHA pins (check-pins.sh):
#   - the GoReleaser release step of release.yml names the tag it releases: GORELEASER_CURRENT_TAG: ${{ github.ref_name }}.
#     Without it GoReleaser takes the first of `git tag --points-at HEAD`, and an -rc tag on the same commit sorts first, so
#     the final release would be built and published as the -rc. No other workflow sets it.
#   - release.yml restores nothing another run saved: every actions/setup-go step has `cache: false` (its default caches
#     GOMODCACHE and GOCACHE, and a tag run restores the caches main's CI saved; go build trusts GOCACHE's compiled packages,
#     go mod verify does not look at them), every docker/setup-buildx-action step has `cache-binary: false`, and there is no
#     actions/cache step and no type=gha build cache.
#   - ci.yml and release.yml both run govulncheck at a pinned version (go run golang.org/x/vuln/cmd/govulncheck@vX.Y.Z), the
#     same one: a release must not ship a Go or module vulnerability that the code reaches.
#   - every docker/setup-buildx-action step, in every workflow, runs a BuildKit image pinned by digest
#     (`driver-opts: image=moby/buildkit:vX.Y.Z@sha256:...`), the same one everywhere: without it the builder is the floating
#     moby/buildkit:buildx-stable-1, which builds the image that is signed. Dependabot does not update it (docs/releasing.md).
#   - every actions/checkout step, in every workflow, has `persist-credentials: false`: no later step needs the token in
#     .git/config, so it is not left there.
#   scripts/ci/check-workflows.sh [workflow-dir]
#   scripts/ci/check-workflows.sh --self-test
# The steps must be written in block style (check-pins.sh refuses the other forms). A rule that finds none of the steps it is
# about fails: a renamed or moved step must not pass by being invisible.
set -euo pipefail

steps() { # steps <file>: every step of every job, NUL-separated; each starts with its line number and a newline
  awk '
    function indent(s) { match(s, /[^ ]/); return RSTART - 1 }
    function flush() { if (buf != "") printf "%s%c", buf, 0; buf = "" }
    {
      if ($0 ~ /^[[:space:]]*(#.*)?$/) { if (buf != "") buf = buf "\n" $0; next }
      n = indent($0)
      if (insteps && item < 0 && n >= sind && $0 ~ /^ *- /) item = n
      if (insteps && item >= 0 && n == item && $0 ~ /^ *- /) { flush(); buf = NR "\n" $0; next }
      if (insteps && item >= 0 && n > item) { buf = buf "\n" $0; next }
      if (insteps && (item >= 0 || n <= sind)) { flush(); insteps = 0 }
      if ($0 ~ /^ *steps:[[:space:]]*$/) { insteps = 1; sind = n; item = -1 }
    }
    END { flush() }
  ' "$1"
}

check() { # check <workflow-dir>
  local dir="$1" fail=0 f step n text release="$1/release.yml" found=0
  [[ -f "$release" ]] || { echo "FAIL $release is missing"; return 1; }
  while IFS= read -r -d '' step; do
    n="${step%%$'\n'*}" text="${step#*$'\n'}"
    grep -Eq '^[[:space:]-]*uses:[[:space:]]*goreleaser/goreleaser-action@' <<<"$text" || continue
    grep -Eq '^[[:space:]]*args:.*(^|[[:space:]])release([[:space:]]|$)' <<<"$text" || continue
    found=1
    if grep -Eq '^[[:space:]]*GORELEASER_CURRENT_TAG:[[:space:]]*\$\{\{[[:space:]]*github\.ref_name[[:space:]]*\}\}[[:space:]]*$' <<<"$text"; then
      echo "ok   $release:$n GoReleaser releases the tag that triggered the run (GORELEASER_CURRENT_TAG)"
    else
      echo "FAIL $release:$n the GoReleaser step must set GORELEASER_CURRENT_TAG: \${{ github.ref_name }} in its env"
      fail=1
    fi
  done < <(steps "$release")
  if [[ $found -eq 0 ]]; then echo "FAIL $release has no GoReleaser release step"; fail=1; fi

  local setup_go=0
  while IFS= read -r -d '' step; do
    n="${step%%$'\n'*}" text="${step#*$'\n'}"
    if grep -Eq '^[[:space:]-]*uses:[[:space:]]*actions/setup-go@' <<<"$text"; then
      setup_go=1
      if grep -Eq '^[[:space:]]*cache:[[:space:]]*false[[:space:]]*(#.*)?$' <<<"$text"; then
        echo "ok   $release:$n setup-go restores no cache"
      else
        echo "FAIL $release:$n actions/setup-go must have cache: false (a tag run would restore the Go cache main's CI saved)"
        fail=1
      fi
    fi
    if grep -Eq '^[[:space:]-]*uses:[[:space:]]*docker/setup-buildx-action@' <<<"$text" &&
      ! grep -Eq '^[[:space:]]*cache-binary:[[:space:]]*false[[:space:]]*(#.*)?$' <<<"$text"; then
      echo "FAIL $release:$n docker/setup-buildx-action must have cache-binary: false"
      fail=1
    fi
  done < <(steps "$release")
  if [[ $setup_go -eq 0 ]]; then echo "FAIL $release has no actions/setup-go step"; fail=1; fi
  if n=$(grep -nE '^[[:space:]-]*uses:[[:space:]]*actions/cache(/[a-z]+)?@' "$release"); then
    echo "FAIL $release:${n%%:*} uses actions/cache; the release builds from nothing another run saved"
    fail=1
  fi
  if n=$(grep -nE 'type=gha' "$release"); then
    echo "FAIL $release:${n%%:*} uses the GitHub Actions build cache (type=gha)"
    fail=1
  fi
  local vuln="" v wf
  for wf in "$dir/ci.yml" "$release"; do
    v=$(grep -Eo 'golang\.org/x/vuln/cmd/govulncheck@[^[:space:]]+' "$wf" 2>/dev/null | sort -u || true)
    if ! [[ "$v" =~ ^golang\.org/x/vuln/cmd/govulncheck@v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "FAIL $wf must run govulncheck at one pinned version (go run golang.org/x/vuln/cmd/govulncheck@vX.Y.Z); found: '${v//$'\n'/ }'"
      fail=1
    elif [[ -n "$vuln" && "$v" != "$vuln" ]]; then
      echo "FAIL $wf runs ${v#*@} of govulncheck, the other workflow ${vuln#*@}"
      fail=1
    else
      vuln="$v"
      echo "ok   $wf runs ${v##*/}"
    fi
  done

  shopt -s nullglob
  local builder="" img
  for f in "$dir"/*.yml "$dir"/*.yaml; do
    while IFS= read -r -d '' step; do
      n="${step%%$'\n'*}" text="${step#*$'\n'}"
      if grep -Eq '^[[:space:]-]*uses:[[:space:]]*docker/setup-buildx-action@' <<<"$text"; then
        img=$(sed -n 's/^[[:space:]]*\(driver-opts:[[:space:]]*\)\{0,1\}image=\([^[:space:]]*\)[[:space:]]*$/\2/p' <<<"$text" | head -n 1)
        if ! grep -Eq '^[[:space:]]*driver-opts:' <<<"$text" || ! [[ "$img" =~ ^moby/buildkit:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$ ]]; then
          echo "FAIL $f:$n docker/setup-buildx-action must set driver-opts: image=moby/buildkit:vX.Y.Z@sha256:<digest> (got '${img:-none}')"
          fail=1
        elif [[ -n "$builder" && "$img" != "$builder" ]]; then
          echo "FAIL $f:$n runs BuildKit $img, another step runs $builder"
          fail=1
        else
          builder="$img"
          echo "ok   $f:$n BuildKit $img"
        fi
      fi
      if grep -Eq '^[[:space:]-]*uses:[[:space:]]*actions/checkout@' <<<"$text" &&
        ! grep -Eq '^[[:space:]]*persist-credentials:[[:space:]]*false[[:space:]]*(#.*)?$' <<<"$text"; then
        echo "FAIL $f:$n actions/checkout must have persist-credentials: false"
        fail=1
      fi
    done < <(steps "$f")
    [[ "$f" == "$release" ]] && continue
    if n=$(grep -n 'GORELEASER_CURRENT_TAG' "$f"); then
      echo "FAIL $f:${n%%:*} sets GORELEASER_CURRENT_TAG; only the release workflow may"
      fail=1
    fi
  done
  return "$fail"
}

self_test() {
  local tmp fail=0 here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  t() { # t <want: pass|fail> <description> <expected text in the output, or ""> : the release.yml on stdin
    local want=$1 what=$2 needle=$3 d out rc
    d=$(mktemp -d "$tmp/case.XXXX")
    cat >"$d/release.yml"
    printf '%s\n%s\n' "${CI_YML:-jobs:}" "$vuln_ci" >"$d/ci.yml"
    out=$("$here/check-workflows.sh" "$d" 2>&1) && rc=0 || rc=$?
    if [[ $want == pass && $rc -eq 0 ]] || [[ $want == fail && $rc -ne 0 && "$out" == *"$needle"* ]]; then
      echo "ok   $what"
    else
      echo "FAIL $what (rc=$rc):"; printf '%s\n' "$out" | sed 's/^/       /'; fail=1
    fi
  }
  # appended to every ci.yml fixture: a job that runs govulncheck
  local vuln_ci='  vuln:
    steps:
      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...'
  # shellcheck disable=SC2016 # the fixtures are workflow text: ${{ ... }} is not shell
  local good='jobs:
  publish:
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          cache: false
      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1
        with:
          cache-binary: false
          driver-opts: |
            image=moby/buildkit:v0.33.1@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea
      - name: GoReleaser
        uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          version: v2.18.3
          args: release --clean --release-notes=notes.md
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GORELEASER_CURRENT_TAG: ${{ github.ref_name }}
      - name: after
        run: echo GORELEASER_CURRENT_TAG
      - name: govulncheck
        run: go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...'

  t pass "the release step names the tag" "" <<<"$good"
  t fail "the release step without GORELEASER_CURRENT_TAG" "must set GORELEASER_CURRENT_TAG" <<<"$(grep -v 'GORELEASER_CURRENT_TAG: ' <<<"$good")"
  t fail "GORELEASER_CURRENT_TAG with another value" "must set GORELEASER_CURRENT_TAG" <<<"${good/github.ref_name/github.sha}"
  t fail "GORELEASER_CURRENT_TAG only in the next step does not count" "must set GORELEASER_CURRENT_TAG" <<<"$(grep -v 'GORELEASER_CURRENT_TAG: ' <<<"$good")
      - name: later
        env:
          GORELEASER_CURRENT_TAG: \${{ github.ref_name }}
        run: true"
  t pass "steps indented at the level of steps: too" "" <<<"$(sed 's/^      -/    -/; s/^        /      /' <<<"$good")"
  t fail "setup-go without cache: false" "must have cache: false" <<<"$(grep -v 'cache: false' <<<"$good")"
  t fail "setup-go with cache: true" "must have cache: false" <<<"${good/cache: false/cache: true}"
  t fail "cache: false on the next step does not count" "must have cache: false" <<<"$(sed '/^          cache: false/d; s/^          cache-binary: false/          cache-binary: false\n          cache: false/' <<<"$good")"
  t fail "a second setup-go without cache: false" "must have cache: false" <<<"$good
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0"
  t fail "no setup-go step at all" "has no actions/setup-go step" <<<"$(sed '/setup-go@/,/cache: false/d' <<<"$good")"
  t fail "setup-buildx-action without cache-binary: false" "must have cache-binary: false" <<<"$(grep -v 'cache-binary' <<<"$good")"
  t fail "an actions/cache step" "uses actions/cache" <<<"$good
      - uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830 # v4.3.0"
  t fail "an actions/cache/restore step" "uses actions/cache" <<<"$good
      - uses: actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830 # v4.3.0"
  t fail "a type=gha build cache" "type=gha" <<<"$good
      - run: docker buildx build --cache-from type=gha ."
  local buildx_ci='jobs:
  a:
    steps:
      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1
        with:
          driver-opts: image=moby/buildkit:v0.33.1@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea'
  CI_YML="$buildx_ci" t pass "the same pinned BuildKit in both workflows (block and plain driver-opts)" "" <<<"$good"
  CI_YML="${buildx_ci/cec9f/aaaaa}" t fail "another BuildKit digest in the other workflow" "another step runs" <<<"$good"
  CI_YML="${buildx_ci/@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea/}" t fail "a BuildKit pinned by tag only" "must set driver-opts" <<<"$good"
  CI_YML="${buildx_ci/v0.33.1/buildx-stable-1}" t fail "a floating BuildKit tag with a digest" "must set driver-opts" <<<"$good"
  t fail "setup-buildx-action without driver-opts" "must set driver-opts" <<<"$(grep -v 'driver-opts\|image=moby' <<<"$good")"
  t fail "an image= that is not under driver-opts" "must set driver-opts" <<<"$(grep -v 'driver-opts' <<<"$good")"
  t fail "release.yml without govulncheck" "release.yml must run govulncheck" <<<"$(grep -v govulncheck <<<"$good")"
  t fail "govulncheck@latest" "must run govulncheck at one pinned version" <<<"${good/govulncheck@v1.9.0/govulncheck@latest}"
  vuln_ci="${vuln_ci/v1.9.0/v1.8.0}" t fail "the two workflows run different govulncheck versions" "the other workflow v1.8.0" <<<"$good"
  vuln_ci='' t fail "ci.yml without govulncheck" "ci.yml must run govulncheck" <<<"$good"
  t fail "a checkout that keeps the credentials" "must have persist-credentials: false" <<<"$(grep -v 'persist-credentials' <<<"$good")"
  CI_YML='jobs:
  a:
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0' t fail "a checkout in another workflow that keeps the credentials" "ci.yml:4 actions/checkout must have persist-credentials" <<<"$good"
  t fail "no GoReleaser release step at all" "has no GoReleaser release step" <<<'jobs:
  publish:
    steps:
      - uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          args: check'
  CI_YML='jobs:
  snap:
    steps:
      - uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          args: release --snapshot --clean --skip=sign
        env:
          GORELEASER_CURRENT_TAG: v1.2.3' t fail "another workflow sets GORELEASER_CURRENT_TAG" "only the release workflow may" <<<"$good"
  if [[ $fail -ne 0 ]]; then echo "FAIL check-workflows.sh self-test"; return 1; fi
  echo "ok   check-workflows.sh self-test"
}

case "${1:-}" in
  --self-test) self_test ;;
  -*) sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2 ;;
  *) check "${1:-.github/workflows}" ;;
esac
