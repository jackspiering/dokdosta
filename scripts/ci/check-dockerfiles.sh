#!/usr/bin/env bash
# Dockerfile (compiles from source; the CI and development image) and Dockerfile.release (copies GoReleaser's binaries; the
# published image) must ship the same image: the same bundled compose release and checksums, the same Go toolchain image for
# the download stage and the same distroless base, each pinned by digest. Dependabot bumps both; this fails if only one moved.
# A `# syntax=` directive names an image that BuildKit runs as the Dockerfile frontend, so it is either absent from both
# files (BuildKit's built-in frontend) or the same in both and pinned by digest.
#   scripts/ci/check-dockerfiles.sh [dir]
#   scripts/ci/check-dockerfiles.sh --self-test
# The first argument is the directory with the two files (default: the repository root).
set -euo pipefail

check() {
  local dir a b fail va vb name image
  dir="${1:-.}"
  a="$dir/Dockerfile" b="$dir/Dockerfile.release"
  [[ -f "$a" && -f "$b" ]] || { echo "check-dockerfiles: $a and $b are needed" >&2; return 2; }
  fail=0

  syntax() { # syntax <file>: the image of a "# syntax=" parser directive, or nothing
    sed -n 's/^#[[:space:]]*syntax[[:space:]]*=[[:space:]]*\([^[:space:]]*\).*/\1/Ip' "$1" | head -n 1
  }
  va=$(syntax "$a") vb=$(syntax "$b")
  if [[ -z "$va" && -z "$vb" ]]; then
    echo "ok   no # syntax= directive (BuildKit's built-in Dockerfile frontend)"
  elif [[ "$va" != "$vb" ]]; then
    echo "FAIL # syntax= differs: '${va:-none}' in $a, '${vb:-none}' in $b"
    fail=1
  elif ! [[ "$va" =~ @sha256:[0-9a-f]{64}$ ]]; then
    echo "FAIL # syntax=$va is not pinned by digest (BuildKit runs it as the frontend)"
    fail=1
  else
    echo "ok   # syntax=$va"
  fi

  arg() { # arg <file> <name>: the default of "ARG <name>=<value>"
    sed -n "s/^ARG $2=//p" "$1" | head -n 1
  }
  for name in COMPOSE_VERSION COMPOSE_SHA256_AMD64 COMPOSE_SHA256_ARM64; do
    va=$(arg "$a" "$name") vb=$(arg "$b" "$name")
    if [[ -z "$va" || -z "$vb" ]]; then
      echo "FAIL ARG $name is missing from $([[ -z "$va" ]] && echo "$a" || echo "$b")"
      fail=1
    elif [[ "$va" != "$vb" ]]; then
      echo "FAIL ARG $name differs: $va in $a, $vb in $b"
      fail=1
    else
      echo "ok   ARG $name = $va"
    fi
  done

  base() { # base <file> <image name>: the full reference of the first FROM of that image, platform flag removed
    sed -n "s|^FROM \(--platform=[^ ]* \)\{0,1\}\($2[^ ]*\).*|\2|p" "$1" | head -n 1
  }
  for image in golang: gcr.io/distroless/static:; do
    va=$(base "$a" "$image") vb=$(base "$b" "$image")
    if [[ -z "$va" || -z "$vb" ]]; then
      echo "FAIL no FROM $image in one of the files"
      fail=1
    elif [[ "$va" != "$vb" ]]; then
      echo "FAIL FROM $image differs: $va in $a, $vb in $b"
      fail=1
    elif [[ "$va" != *@sha256:* ]]; then
      echo "FAIL FROM $va is not pinned by digest"
      fail=1
    else
      echo "ok   FROM $va"
    fi
  done
  return "$fail"
}

self_test() {
  local tmp fail=0 here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  # shellcheck disable=SC2016 # Dockerfile text: $BUILDPLATFORM is not shell
  local body='FROM --platform=$BUILDPLATFORM golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS fetch
ARG COMPOSE_VERSION=5.6.0
ARG COMPOSE_SHA256_AMD64=40343e21ca777173e69cff5dbafeb37c6f81f3b0d57d9e597f036e95eb63e76a
ARG COMPOSE_SHA256_ARM64=733ec76717ceb59052a9609b9dadfb523b2df8eab57a54212872d10a58078ea2
FROM gcr.io/distroless/static:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087'
  local pinned='# syntax=docker/dockerfile:1.20@sha256:0000000000000000000000000000000000000000000000000000000000000001'
  t() { # t <want: pass|fail> <description> <expected text> <first line of Dockerfile> <first line of Dockerfile.release> [sed for both] [sed for Dockerfile.release]
    local want=$1 what=$2 needle=$3 d out rc
    d=$(mktemp -d "$tmp/case.XXXX")
    printf '%s\n%s\n' "$4" "$body" | sed "${6:-}" >"$d/Dockerfile"
    printf '%s\n%s\n' "$5" "$body" | sed "${6:-}" | sed "${7:-}" >"$d/Dockerfile.release"
    out=$("$here/check-dockerfiles.sh" "$d" 2>&1) && rc=0 || rc=$?
    if [[ $want == pass && $rc -eq 0 ]] || [[ $want == fail && $rc -ne 0 && "$out" == *"$needle"* ]]; then
      echo "ok   $what"
    else
      echo "FAIL $what (rc=$rc):"; printf '%s\n' "$out" | sed 's/^/       /'; fail=1
    fi
  }
  t pass "no syntax directive in either file" "" "" ""
  t pass "the same digest-pinned syntax directive in both" "" "$pinned" "$pinned"
  t fail "an unpinned syntax directive in both" "is not pinned by digest" "# syntax=docker/dockerfile:1" "# syntax=docker/dockerfile:1"
  t fail "a tag-pinned syntax directive in both" "is not pinned by digest" "# syntax=docker/dockerfile:1.20" "# syntax=docker/dockerfile:1.20"
  t fail "upper-case SYNTAX counts too" "is not pinned by digest" "# SYNTAX=docker/dockerfile:1" "# SYNTAX=docker/dockerfile:1"
  t fail "a syntax directive in one file only" "# syntax= differs" "# syntax=docker/dockerfile:1" ""
  t fail "two different pinned directives" "# syntax= differs" "$pinned" "${pinned%1}2"
  t fail "a compose version that differs" "ARG COMPOSE_VERSION differs" "" "" "" "/^ARG COMPOSE_VERSION/s/5.6.0/5.6.1/"
  t fail "an unpinned base image" "is not pinned by digest" "" "" "s/@sha256:5167[0-9a-f]*//"
  if [[ $fail -ne 0 ]]; then echo "FAIL check-dockerfiles.sh self-test"; return 1; fi
  echo "ok   check-dockerfiles.sh self-test"
}

case "${1:-}" in
  --self-test) self_test ;;
  -*) sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2 ;;
  *) check "${1:-.}" ;;
esac
