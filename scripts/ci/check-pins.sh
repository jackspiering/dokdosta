#!/usr/bin/env bash
# Every `uses:` in the workflows must name a full commit SHA, with the version as a trailing comment:
#   - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
# A tag or branch can be moved by whoever controls the action's repository; a SHA cannot. Local actions (./...) are exempt.
#   scripts/ci/check-pins.sh [workflow-dir]
set -euo pipefail
dir="${1:-.github/workflows}"
fail=0
shopt -s nullglob
for f in "$dir"/*.yml "$dir"/*.yaml; do
  while IFS= read -r line; do
    n="${line%%:*}"
    text="${line#*:}"
    ref="${text#*uses:}"
    ref="${ref#"${ref%%[![:space:]]*}"}"
    case "$ref" in ./*) continue ;; esac
    if ! [[ "$ref" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}[[:space:]]+#[[:space:]]*v?[0-9][^[:space:]]*([[:space:]].*)?$ ]]; then
      echo "FAIL $f:$n is not pinned to a commit SHA with a version comment: $ref"
      fail=1
    fi
  done < <(grep -n '^[[:space:]-]*uses:' "$f" || true)
done
[[ $fail -eq 0 ]] && echo "ok   every action is pinned to a commit SHA"
exit "$fail"
