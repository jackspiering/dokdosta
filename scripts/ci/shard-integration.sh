#!/usr/bin/env bash
# Split the integration tests across runners. The tests are real ladders against a real daemon and run one after another
# (a dokdosta in one test watches every opted-in container on the daemon, so they cannot share a daemon); the way to be
# faster is more daemons, one per shard.
#
#   scripts/ci/shard-integration.sh <index>/<shards>   print the -run regex of shard <index> (0-based) of <shards>
#   scripts/ci/shard-integration.sh --check <shards>   fail unless the shards together are exactly the tests there are
#   scripts/ci/shard-integration.sh --check-ci <file>  fail unless the "shard: [...]" matrix of the workflow is 0/n .. n-1/n
#                                                      for one n, and the n shards are exactly the tests there are
#   scripts/ci/shard-integration.sh --self-test
#
# The test list is always `go test -tags integration -list` (SHARD_TESTS_FILE replaces it, for the self-test), so a new test
# is in some shard without anyone editing a list. Tests are packed greedily, longest first, into the shard with the least
# time so far, using scripts/ci/integration-durations.txt (a test not listed counts as 30 s). The regex is anchored:
# TestImage is a prefix of TestImageServesTheDashboard..., and an unanchored -run would run those in two shards.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
durations="${SHARD_DURATIONS_FILE:-$here/integration-durations.txt}"

tests() {
  if [ -n "${SHARD_TESTS_FILE:-}" ]; then cat "$SHARD_TESTS_FILE"; else
    (cd "$here/../.." && go test -tags integration -list '^Test' ./test/integration/)
  fi | grep -E '^Test[A-Za-z0-9_]*$' | sort -u
}

# plan <shards>: "<shard> <seconds> <test>" per test
plan() {
  tests | awk -v n="$1" -v dfile="$durations" '
    BEGIN {
      while ((getline line < dfile) > 0) { if (line ~ /^#/ || line == "") continue; split(line, f, " "); d[f[1]] = f[2] }
    }
    { print (($0 in d) ? d[$0] : 30), $0 }' |
    sort -k1,1nr -k2,2 |
    awk -v n="$1" '
      { best = 0; for (i = 1; i < n; i++) if (load[i] < load[best]) best = i; load[best] += $1; print best, $1, $2 }'
}

usage() { sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2; }

shard() { # shard <shards> <index>
  local names
  names=$(plan "$1" | awk -v i="$2" '$1 == i { printf "%s%s", (c++ ? "|" : ""), $3 }')
  [ -n "$names" ] || { echo "shard-integration: shard $2 of $1 has no tests" >&2; return 1; }
  printf '^(%s)$\n' "$names"
}

check() { # check <shards>: the shards are a partition of the test list
  local n=$1 all got i
  all=$(tests)
  [ -n "$all" ] || { echo "shard-integration: no tests found" >&2; return 1; }
  for ((i = 0; i < n; i++)); do shard "$n" "$i" >/dev/null || return 1; done
  got=$(for ((i = 0; i < n; i++)); do shard "$n" "$i" | sed 's/^^(//;s/)\$$//' | tr '|' '\n'; done | sort)
  if [ "$got" != "$all" ]; then
    echo "shard-integration: the $n shards are not exactly the tests there are:" >&2
    diff <(echo "$all") <(echo "$got") >&2 || true
    return 1
  fi
  echo "$(echo "$all" | wc -l) tests in $n shards, each exactly once:"
  plan "$n" | awk '{ t[$1] += $2; c[$1]++ } END { for (i in t) printf "  shard %s: %d tests, about %d s\n", i, c[i], t[i] }' | sort
}

self_test() {
  local tmp fail=0
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  printf '%s\n' TestImage TestImageServes TestImageRecreates TestLadder TestPull TestFast1 TestFast2 TestNew ok >"$tmp/tests"
  printf 'PASS\nTestNotATest_ \n' >>"$tmp/tests" # noise from go test -list is dropped
  printf '# comment\nTestImage 9\nTestImageServes 8\nTestImageRecreates 21\nTestLadder 41\nTestPull 45\nTestFast1 0\nTestFast2 0\n\n' >"$tmp/dur"
  export SHARD_TESTS_FILE="$tmp/tests" SHARD_DURATIONS_FILE="$tmp/dur"
  ok() { if "$@"; then echo "ok   $*"; else echo "FAIL $*"; fail=1; fi; }
  bad() { if "$@" >/dev/null 2>&1; then echo "FAIL (should fail) $*"; fail=1; else echo "ok   (fails) $*"; fi; }
  ok check 1
  ok check 3
  ok check 5
  bad check 9 # more shards than the 8 tests: an empty shard is an error, not a silent no-op
  # anchored: TestImage must not select TestImageServes
  local re
  re=$(shard 3 0)
  if [[ "$re" == ^\(*\)\$ ]]; then echo "ok   anchored: $re"; else echo "FAIL not anchored: $re"; fail=1; fi
  # the two longest tests do not share a shard, and the unlisted TestNew counts 30 s rather than 0
  if [[ "$re" == *TestPull* && "$re" == *TestLadder* ]]; then echo "FAIL the two longest tests share a shard: $re"; fail=1; else echo "ok   the longest tests are apart"; fi
  # a missing duration file is not fatal: everything counts 30 s
  if SHARD_DURATIONS_FILE="$tmp/none" check 2 >/dev/null 2>&1; then echo "ok   no duration file"; else echo "FAIL no duration file"; fail=1; fi
  printf '        shard: ["0/3", "1/3", "2/3"]\n' >"$tmp/good.yml"
  ok check_ci "$tmp/good.yml"
  printf '        shard: ["0/3", "1/3"]\n' >"$tmp/missing.yml"
  bad check_ci "$tmp/missing.yml"
  printf '        shard: ["0/3", "1/4", "2/3"]\n' >"$tmp/mixed.yml"
  bad check_ci "$tmp/mixed.yml"
  printf 'nothing here\n' >"$tmp/none.yml"
  bad check_ci "$tmp/none.yml"
  ok "$0" 1/3
  bad "$0" 3/3
  bad "$0" 1
  if [ "$fail" = 0 ]; then echo "shard-integration self-test: ok"; else echo "shard-integration self-test: FAILED" >&2; return 1; fi
}

check_ci() { # check_ci <workflow>: the matrix names every shard of one n, once
  local line entries n i
  line=$(grep -E '^[[:space:]]*shard: \[' "$1" || true)
  [ "$(echo "$line" | grep -c .)" = 1 ] || { echo "shard-integration: $1 needs exactly one 'shard: [...]' matrix line" >&2; return 1; }
  entries=$(echo "$line" | grep -oE '[0-9]+/[0-9]+' | sort -t/ -k1,1n || true)
  n=$(echo "$entries" | head -n 1 | cut -d/ -f2)
  [ -n "$n" ] || { echo "shard-integration: no i/n entries in: $line" >&2; return 1; }
  for ((i = 0; i < n; i++)); do echo "$i/$n"; done | sort -t/ -k1,1n | cmp -s - <(echo "$entries") ||
    { echo "shard-integration: the matrix in $1 is not 0/$n .. $((n - 1))/$n: $(echo "$entries" | paste -sd' ')" >&2; return 1; }
  check "$n"
}

case "${1:-}" in
  --self-test) self_test ;;
  --check) [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] || usage; check "$2" ;;
  --check-ci) [ -f "${2:-}" ] || usage; check_ci "$2" ;;
  *)
    [[ "${1:-}" =~ ^([0-9]+)/([1-9][0-9]*)$ && "${BASH_REMATCH[1]}" -lt "${BASH_REMATCH[2]}" ]] || usage
    shard "${BASH_REMATCH[2]}" "${BASH_REMATCH[1]}"
    ;;
esac
