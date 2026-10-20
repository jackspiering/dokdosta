#!/usr/bin/env bash
# Is the latest compose release one the integration matrix already pins? Then a "compose latest" leg would repeat that leg
# exactly, and its shards stop early; the leg's required check stays (plan section 6, *CI matrix*).
#   scripts/ci/compose-latest.sh <workflow>   print "version=<X.Y.Z>" and "pinned=true|false" (lines for $GITHUB_OUTPUT)
#   scripts/ci/compose-latest.sh --self-test
# The pinned releases are the workflow's compose matrix ("compose: [...]") without "latest"; every such line must agree, and
# a workflow without one is an error. If the latest release cannot be looked up, it prints an empty version and
# pinned=false: the legs run in full (setup-docker.sh resolves "latest" itself, and fails the leg if it cannot either).
# COMPOSE_LATEST_URL replaces the GitHub API URL (the self-test uses file:// URLs); GITHUB_TOKEN is sent when set.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

pinned_in() { # pinned_in <workflow>: the pinned compose releases, one per line
  local lines
  lines=$(sed -n 's/^[[:space:]]*compose:[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$1" | tr -d "\"' " | sort -u)
  if [ -z "$lines" ]; then echo "compose-latest: no compose matrix in $1" >&2; return 1; fi
  if [ "$(echo "$lines" | wc -l)" -ne 1 ]; then
    echo "compose-latest: the compose matrices in $1 disagree:" >&2
    echo "$lines" >&2
    return 1
  fi
  echo "$lines" | tr ',' '\n' | grep -vx 'latest' | sed 's/^v//' | grep . || { echo "compose-latest: $1 pins no compose release" >&2; return 1; }
}

latest() { # the latest compose release as X.Y.Z, or nothing
  local auth=() tag url="${COMPOSE_LATEST_URL:-https://api.github.com/repos/docker/compose/releases/latest}"
  if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}"); fi
  tag=$(curl -fsSL --retry 3 --max-time 30 "${auth[@]}" "$url" 2>/dev/null | jq -r '.tag_name // empty' 2>/dev/null) || tag=""
  tag=${tag#v}
  if [[ "$tag" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then echo "$tag"; fi
}

run() { # run <workflow>
  local pinned v
  pinned=$(pinned_in "$1") || return 1
  v=$(latest)
  if [ -z "$v" ]; then
    echo "compose-latest: could not look up the latest compose release; the latest legs run in full" >&2
    printf 'version=\npinned=false\n'
    return 0
  fi
  echo "version=$v"
  if grep -qxF "$v" <<<"$pinned"; then
    echo "compose-latest: the latest release is v$v, which the matrix pins: the latest legs would repeat it" >&2
    echo "pinned=true"
  else
    echo "compose-latest: the latest release is v$v, which the matrix does not pin: the latest legs run" >&2
    echo "pinned=false"
  fi
}

self_test() {
  local tmp fail=0 got
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  t() { # t <description> <want> <got>
    if [ "$3" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: got '$3', want '$2'"; fail=1; fi
  }
  cat >"$tmp/ci.yml" <<'YAML'
  integration-shard:
    strategy:
      matrix:
        # compose: [not, this, one]
        compose: ["5.2.0", "5.6.0", latest]
  integration:
    strategy:
      matrix:
        compose: [ "5.2.0", '5.6.0', latest ]
YAML
  sed 's/"5.2.0", "5.6.0"/"5.2.0", "5.7.0"/' "$tmp/ci.yml" >"$tmp/disagree.yml"
  echo 'jobs: {}' >"$tmp/none.yml"
  printf '        compose: [latest]\n' >"$tmp/onlylatest.yml"
  echo '{"tag_name": "v5.6.0", "name": "v5.6.0"}' >"$tmp/pinned.json"
  echo '{"tag_name": "v5.7.0"}' >"$tmp/newer.json"
  echo '{"message": "API rate limit exceeded"}' >"$tmp/limited.json"
  echo '{"tag_name": "v5.7.0-rc.1"}' >"$tmp/odd.json"

  t "the pinned releases, from both matrices" "$(printf '5.2.0\n5.6.0')" "$(pinned_in "$tmp/ci.yml")"
  refused() { # refused <description> <command>...: the command must fail
    local what=$1
    shift
    if "$@" >/dev/null 2>&1; then t "$what" refused accepted; else t "$what" refused refused; fi
  }
  refused "disagreeing matrices are refused" pinned_in "$tmp/disagree.yml"
  refused "a workflow without a compose matrix is refused" pinned_in "$tmp/none.yml"
  refused "a matrix that pins nothing is refused" pinned_in "$tmp/onlylatest.yml"
  got=$(pinned_in "$here/../../.github/workflows/ci.yml" 2>&1) || got="error: $got"
  if [ -n "$got" ] && ! grep -qx latest <<<"$got" && ! grep -q '^error' <<<"$got"; then got=ok; fi
  t "ci.yml has one compose matrix that pins releases" ok "$got"

  got=$(COMPOSE_LATEST_URL="file://$tmp/pinned.json" run "$tmp/ci.yml" 2>/dev/null)
  t "latest is pinned" "$(printf 'version=5.6.0\npinned=true')" "$got"
  got=$(COMPOSE_LATEST_URL="file://$tmp/newer.json" run "$tmp/ci.yml" 2>/dev/null)
  t "latest is newer than every pinned release" "$(printf 'version=5.7.0\npinned=false')" "$got"
  got=$(COMPOSE_LATEST_URL="file://$tmp/missing.json" run "$tmp/ci.yml" 2>/dev/null)
  t "a failed lookup runs the legs" "$(printf 'version=\npinned=false')" "$got"
  got=$(COMPOSE_LATEST_URL="file://$tmp/limited.json" run "$tmp/ci.yml" 2>/dev/null)
  t "an API error runs the legs" "$(printf 'version=\npinned=false')" "$got"
  got=$(COMPOSE_LATEST_URL="file://$tmp/odd.json" run "$tmp/ci.yml" 2>/dev/null)
  t "a tag that is not X.Y.Z runs the legs" "$(printf 'version=\npinned=false')" "$got"
  COMPOSE_LATEST_URL="file://$tmp/pinned.json" refused "a broken matrix fails the run" run "$tmp/disagree.yml"

  if [ "$fail" -ne 0 ]; then echo "FAIL compose-latest.sh self-test"; return 1; fi
  echo "ok   compose-latest.sh self-test"
}

case "${1:-}" in
  --self-test) self_test ;;
  -* | "") sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2 ;;
  *) run "$1" ;;
esac
