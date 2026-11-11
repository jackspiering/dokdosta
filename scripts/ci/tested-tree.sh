#!/usr/bin/env bash
# Did a pull request's run already test this tree? A push to main whose exact tree a full pull-request run tested green
# runs only the go job (plan section 6, v3.18 item 5).
#   scripts/ci/tested-tree.sh name <tree>                  the name of a tree's pointer artifact
#   scripts/ci/tested-tree.sh lookup <owner/repo> <tree>   print the id of a run that tested the tree green, or nothing
#   scripts/ci/tested-tree.sh --self-test
# The pointer is an artifact that the tested-tree job of ci.yml uploads at the start of a full pull-request run into main,
# named after the Git tree of the merge commit the run checked out. It proves nothing by itself: lookup takes the runs
# that uploaded one for this tree and accepts a run only when the API says all of this about it: a pull_request run of
# ci.yml in this repository, from a branch of this repository (not a fork), completed, conclusion success. The pointer
# must not be expired or older than 7 days. A run that has not finished is asked again for about a minute: the merge can
# be seconds ahead of the run's own end. Any doubt prints nothing, and the push runs in full; lookup fails only on a
# usage error.
# GITHUB_TOKEN is sent when set (it needs actions: read). TESTED_TREE_WAIT is the seconds between two looks at an
# unfinished run (10) and TESTED_TREE_TRIES the number of looks (7). TESTED_TREE_FIXTURES replaces the GitHub API with a
# directory of files, for the self-test.
set -euo pipefail

workflow=.github/workflows/ci.yml
max_age=$((7 * 24 * 3600))
wait=${TESTED_TREE_WAIT:-10}
tries=${TESTED_TREE_TRIES:-7}

usage() { sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2; }
say() { echo "tested-tree: $*" >&2; }

artifact_name() { echo "tested-tree-$1"; }

fetch() { # fetch <path under the API>: the JSON document, or fail
  local path=$1 auth=() file n
  if [ -n "${TESTED_TREE_FIXTURES:-}" ]; then
    file="$TESTED_TREE_FIXTURES/$(tr -c 'A-Za-z0-9.\n-' '_' <<<"$path")"
    if [ -d "$file" ]; then # answers that change: 1.json for the first look, 2.json for the second, ...
      n=$(($(cat "$file/looks" 2>/dev/null || echo 0) + 1))
      echo "$n" >"$file/looks"
      cat "$file/$n.json"
    else
      cat "$file.json"
    fi 2>/dev/null
    return
  fi
  if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}"); fi
  curl -fsSL --retry 3 --max-time 30 -H "Accept: application/vnd.github+json" "${auth[@]}" "https://api.github.com/$path"
}

green() { # green <owner/repo> <run id>: a finished, green pull-request run of ci.yml from a branch of this repository
  local repo=$1 id=$2 run verdict try
  for ((try = 1; try <= tries; try++)); do
    run=$(fetch "repos/$repo/actions/runs/$id") || { say "run $id: could not be read"; return 1; }
    verdict=$(jq -r --arg repo "$repo" --arg wf "$workflow" --arg id "$id" '
      if (.id | tostring) != $id then "not the run that was asked for"
      elif .event != "pull_request" then "not a pull request run (\(.event))"
      elif .path != $wf then "not a run of \($wf) (\(.path))"
      elif .repository.full_name != $repo or .head_repository.full_name != $repo then "from another repository (\(.head_repository.full_name))"
      elif .status != "completed" then "running"
      elif .conclusion != "success" then "not green (\(.conclusion))"
      else "green" end' <<<"$run" 2>/dev/null) || verdict="could not be read"
    case "$verdict" in
      green)
        say "run $id tested this tree green"
        return 0
        ;;
      running)
        if [ "$try" -lt "$tries" ]; then
          say "run $id has not finished (look $try of $tries)"
          sleep "$wait"
        fi
        ;;
      *)
        say "run $id: $verdict"
        return 1
        ;;
    esac
  done
  say "run $id had not finished after $tries looks"
  return 1
}

lookup() { # lookup <owner/repo> <tree>
  local repo=$1 tree=$2 name arts ids id
  name=$(artifact_name "$tree")
  arts=$(fetch "repos/$repo/actions/artifacts?name=$name&per_page=100") || { say "could not list the artifacts named $name"; return 0; }
  # The newest runs first, five at most: one tree has several pointers only after a re-run or a second pull request.
  ids=$(jq -r --arg n "$name" --argjson max "$max_age" '
    [.artifacts[]? | select(.name == $n and .expired == false and ((.created_at | fromdateiso8601) > (now - $max)))
      | .workflow_run.id | numbers] | unique | reverse | .[:5] | .[]' <<<"$arts" 2>/dev/null) || ids=""
  if [ -z "$ids" ]; then
    say "no pull request's run left a pointer for tree $tree in the last 7 days"
    return 0
  fi
  for id in $ids; do
    if green "$repo" "$id"; then
      echo "$id"
      return 0
    fi
  done
  return 0
}

self_test() {
  local tmp fail=0 repo=o/r tree now old n=0
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  tree=$(printf 'a%.0s' {1..40})
  now=$(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ)
  old=$(date -u -d '-8 days' +%Y-%m-%dT%H:%M:%SZ)
  t() { # t <description> <want> <got>
    if [ "$3" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: got '$3', want '$2'"; fail=1; fi
  }
  key() { tr -c 'A-Za-z0-9.\n-' '_' <<<"$1"; }
  # pointers <dir> <created_at> <expired> <run id>...: the artifact list of the tree
  pointers() {
    local dir=$1 at=$2 expired=$3 id items=""
    shift 3
    for id in "$@"; do
      items+="${items:+,}{\"name\":\"tested-tree-$tree\",\"expired\":$expired,\"created_at\":\"$at\",\"workflow_run\":{\"id\":$id}}"
    done
    echo "{\"artifacts\":[$items]}" >"$dir/$(key "repos/$repo/actions/artifacts?name=tested-tree-$tree&per_page=100").json"
  }
  # run <id> <event> <path> <head repository> <status> <conclusion>: a run document
  run() { echo "{\"id\":$1,\"event\":\"$2\",\"path\":\"$3\",\"repository\":{\"full_name\":\"$repo\"},\"head_repository\":{\"full_name\":\"$4\"},\"status\":\"$5\",\"conclusion\":$6}"; }
  # one <description> <want> <created_at> <expired> <run document of run 7>
  one() {
    local d
    n=$((n + 1))
    d="$tmp/$n"
    mkdir -p "$d"
    pointers "$d" "$3" "$4" 7
    echo "$5" >"$d/$(key "repos/$repo/actions/runs/7").json"
    t "$1" "$2" "$(TESTED_TREE_FIXTURES="$d" TESTED_TREE_WAIT=0 TESTED_TREE_TRIES=2 lookup "$repo" "$tree" 2>/dev/null)"
  }
  local ok_run
  ok_run=$(run 7 pull_request "$workflow" "$repo" completed '"success"')

  t "the pointer's name" "tested-tree-$tree" "$(artifact_name "$tree")"
  one "a green pull-request run of this repository" 7 "$now" false "$ok_run"
  one "an expired pointer" "" "$now" true "$ok_run"
  one "a pointer older than 7 days" "" "$old" false "$ok_run"
  one "a run from a fork" "" "$now" false "$(run 7 pull_request "$workflow" fork/r completed '"success"')"
  one "a run that is not a pull request's" "" "$now" false "$(run 7 push "$workflow" "$repo" completed '"success"')"
  one "a run of another workflow" "" "$now" false "$(run 7 pull_request .github/workflows/other.yml "$repo" completed '"success"')"
  one "a failed run" "" "$now" false "$(run 7 pull_request "$workflow" "$repo" completed '"failure"')"
  one "a cancelled run" "" "$now" false "$(run 7 pull_request "$workflow" "$repo" completed '"cancelled"')"
  one "a run that never finishes" "" "$now" false "$(run 7 pull_request "$workflow" "$repo" in_progress null)"
  one "another run's document" "" "$now" false "$(run 8 pull_request "$workflow" "$repo" completed '"success"')"
  one "a run document that is not JSON" "" "$now" false "<html>"

  # no pointer at all, and an API that does not answer
  mkdir -p "$tmp/none" "$tmp/down"
  echo '{"artifacts":[]}' >"$tmp/none/$(key "repos/$repo/actions/artifacts?name=tested-tree-$tree&per_page=100").json"
  t "no pointer" "" "$(TESTED_TREE_FIXTURES="$tmp/none" lookup "$repo" "$tree" 2>/dev/null)"
  t "an API that does not answer" "" "$(TESTED_TREE_FIXTURES="$tmp/down" lookup "$repo" "$tree" 2>/dev/null)"
  if TESTED_TREE_FIXTURES="$tmp/down" lookup "$repo" "$tree" >/dev/null 2>&1; then echo "ok   a failed lookup is not an error"; else echo "FAIL a failed lookup is an error"; fail=1; fi

  # a run that finishes while lookup waits
  local d="$tmp/late" runs
  runs="$d/$(key "repos/$repo/actions/runs/7")"
  mkdir -p "$runs"
  pointers "$d" "$now" false 7
  run 7 pull_request "$workflow" "$repo" in_progress null >"$runs/1.json"
  echo "$ok_run" >"$runs/2.json"
  t "a run that finishes on the second look" 7 "$(TESTED_TREE_FIXTURES="$d" TESTED_TREE_WAIT=0 lookup "$repo" "$tree" 2>/dev/null)"

  # two runs for one tree: the newer failed, the older is green
  d="$tmp/two"
  mkdir -p "$d"
  pointers "$d" "$now" false 7 9
  echo "$ok_run" >"$d/$(key "repos/$repo/actions/runs/7").json"
  run 9 pull_request "$workflow" "$repo" completed '"failure"' >"$d/$(key "repos/$repo/actions/runs/9").json"
  t "the newer run failed, the older is green" 7 "$(TESTED_TREE_FIXTURES="$d" lookup "$repo" "$tree" 2>/dev/null)"

  # usage: a tree that is not a hash, a repository that is not owner/name
  local here
  here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
  if "$here/tested-tree.sh" lookup "$repo" 'x; rm -rf /' >/dev/null 2>&1; then echo "FAIL a tree that is not a hash is accepted"; fail=1; else echo "ok   a tree that is not a hash is refused"; fi
  if "$here/tested-tree.sh" lookup 'o/r/../x' "$tree" >/dev/null 2>&1; then echo "FAIL a bad repository is accepted"; fail=1; else echo "ok   a bad repository is refused"; fi
  if "$here/tested-tree.sh" name main >/dev/null 2>&1; then echo "FAIL name takes something that is not a hash"; fail=1; else echo "ok   name refuses what is not a hash"; fi

  if [ "$fail" -ne 0 ]; then echo "FAIL tested-tree.sh self-test"; return 1; fi
  echo "ok   tested-tree.sh self-test"
}

is_tree() { [[ "$1" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]]; }

case "${1:-}" in
  --self-test) self_test ;;
  name)
    if [ $# -ne 2 ] || ! is_tree "$2"; then usage; fi
    artifact_name "$2"
    ;;
  lookup)
    if [ $# -ne 3 ] || ! [[ "$2" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || ! is_tree "$3"; then usage; fi
    lookup "$2" "$3"
    ;;
  *) usage ;;
esac
