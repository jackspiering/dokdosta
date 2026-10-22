#!/usr/bin/env bash
# Rules about what the workflows' steps say, beyond the SHA pins (check-pins.sh):
#   - the GoReleaser release step of release.yml names the tag it releases: GORELEASER_CURRENT_TAG: ${{ github.ref_name }}.
#     Without it GoReleaser takes the first of `git tag --points-at HEAD`, and an -rc tag on the same commit sorts first, so
#     the final release would be built and published as the -rc. No other workflow sets it.
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
  shopt -s nullglob
  for f in "$dir"/*.yml "$dir"/*.yaml; do
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
    if [[ -n "${CI_YML:-}" ]]; then printf '%s\n' "$CI_YML" >"$d/ci.yml"; fi
    out=$("$here/check-workflows.sh" "$d" 2>&1) && rc=0 || rc=$?
    if [[ $want == pass && $rc -eq 0 ]] || [[ $want == fail && $rc -ne 0 && "$out" == *"$needle"* ]]; then
      echo "ok   $what"
    else
      echo "FAIL $what (rc=$rc):"; printf '%s\n' "$out" | sed 's/^/       /'; fail=1
    fi
  }
  # shellcheck disable=SC2016 # the fixtures are workflow text: ${{ ... }} is not shell
  local good='jobs:
  publish:
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - name: GoReleaser
        uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          version: v2.18.3
          args: release --clean --release-notes=notes.md
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GORELEASER_CURRENT_TAG: ${{ github.ref_name }}
      - name: after
        run: echo GORELEASER_CURRENT_TAG'

  t pass "the release step names the tag" "" <<<"$good"
  t fail "the release step without GORELEASER_CURRENT_TAG" "must set GORELEASER_CURRENT_TAG" <<<"$(grep -v 'GORELEASER_CURRENT_TAG: ' <<<"$good")"
  t fail "GORELEASER_CURRENT_TAG with another value" "must set GORELEASER_CURRENT_TAG" <<<"${good/github.ref_name/github.sha}"
  t fail "GORELEASER_CURRENT_TAG only in the next step does not count" "must set GORELEASER_CURRENT_TAG" <<<"$(grep -v 'GORELEASER_CURRENT_TAG: ' <<<"$good")
      - name: later
        env:
          GORELEASER_CURRENT_TAG: \${{ github.ref_name }}
        run: true"
  t pass "steps indented at the level of steps: too" "" <<<"$(sed 's/^      -/    -/; s/^        /      /' <<<"$good")"
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
