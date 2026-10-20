#!/usr/bin/env bash
# Dockerfile (compiles from source; the CI and development image) and Dockerfile.release (copies GoReleaser's binaries; the
# published image) must ship the same image: the same bundled compose release and checksums, the same Go toolchain image for
# the download stage and the same distroless base, each pinned by digest. Dependabot bumps both; this fails if only one moved.
#   scripts/ci/check-dockerfiles.sh [dir]
# The first argument is the directory with the two files (default: the repository root).
set -euo pipefail
dir="${1:-.}"
a="$dir/Dockerfile" b="$dir/Dockerfile.release"
[[ -f "$a" && -f "$b" ]] || { echo "check-dockerfiles: $a and $b are needed" >&2; exit 2; }
fail=0

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
exit "$fail"
