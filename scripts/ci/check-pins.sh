#!/usr/bin/env bash
# Every `uses:` in the workflows must name a full commit SHA, with the exact release tag as a trailing comment:
#   - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
# A tag or branch can be moved by whoever controls the action's repository; a SHA cannot. Local actions (./...) are exempt.
# Steps must be written in block style: a `uses` key in a flow mapping ({ uses: ... }) or quoted ("uses": ...) fails, so no
# spelling of the key can hide a ref from this check.
#   scripts/ci/check-pins.sh [workflow-dir]                    offline: the form of every pin
#   scripts/ci/check-pins.sh --verify-remote [workflow-dir]    also: each SHA is the commit of the tag in its comment (needs gh
#                                                              and a token; catches a drifted comment and a fork's commit)
#   scripts/ci/check-pins.sh --self-test                       offline
set -euo pipefail

PIN='^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(/[A-Za-z0-9_./-]+)?@([0-9a-f]{40})[[:space:]]+#[[:space:]]*(v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?)([[:space:]].*)?$'

check() { # check <remote: 0|1> <workflow-dir>
  local remote=$1 dir=$2 fail=0 f line n text ref sha tag got
  local -A resolved=()
  shopt -s nullglob
  local files=("$dir"/*.yml "$dir"/*.yaml)
  [[ ${#files[@]} -gt 0 ]] || { echo "FAIL no workflow in $dir"; return 1; }
  for f in "${files[@]}"; do
    while IFS= read -r line; do
      n="${line%%:*}"
      echo "FAIL $f:$n writes a uses key in flow style or quoted; write the step in block style (- uses: owner/repo@sha # vX.Y.Z)"
      fail=1
    done < <(grep -nE "(\{|,)[[:space:]]*[\"']?uses[\"']?[[:space:]]*:|^[[:space:]-]*[\"']uses[\"'][[:space:]]*:" "$f" || true)
    while IFS= read -r line; do
      n="${line%%:*}"
      text="${line#*:}"
      ref="${text#*uses:}"
      ref="${ref#"${ref%%[![:space:]]*}"}"
      case "$ref" in ./*) continue ;; esac
      if ! [[ "$ref" =~ $PIN ]]; then
        echo "FAIL $f:$n is not pinned to a commit SHA with its release tag as a comment (# vX.Y.Z): $ref"
        fail=1
        continue
      fi
      [[ $remote -eq 1 ]] || continue
      sha="${BASH_REMATCH[4]}" tag="${BASH_REMATCH[5]}"
      local repo="${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
      if [[ -z "${resolved["$repo@$tag"]+x}" ]]; then
        # commits/refs/tags/<tag> peels an annotated tag to its commit, and names a tag only (not a branch of that name)
        resolved["$repo@$tag"]=$(gh api "repos/$repo/commits/refs/tags/$tag" --jq .sha 2>/dev/null) || resolved["$repo@$tag"]=""
      fi
      got="${resolved["$repo@$tag"]}"
      if [[ -z "$got" ]]; then
        echo "FAIL $f:$n $repo has no tag $tag (or it could not be looked up)"
        fail=1
      elif [[ "$got" != "$sha" ]]; then
        echo "FAIL $f:$n $sha is not the commit of $repo $tag ($got)"
        fail=1
      fi
    done < <(grep -n '^[[:space:]-]*uses:' "$f" || true)
  done
  if [[ $fail -eq 0 ]]; then
    if [[ $remote -eq 1 ]]; then echo "ok   every action is pinned to the commit of the tag in its comment"
    else echo "ok   every action is pinned to a commit SHA"; fi
  fi
  return "$fail"
}

self_test() {
  local tmp fail=0 here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  mkdir -p "$tmp/bin"
  # gh stub: knows actions/checkout v7.0.1 (its real commit) and sbom-action v0.24.3; anything else is an unknown tag.
  cat >"$tmp/bin/gh" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$STUB_CALLS"
case "$2" in
  repos/actions/checkout/commits/refs/tags/v7.0.1) echo 3d3c42e5aac5ba805825da76410c181273ba90b1 ;;
  repos/anchore/sbom-action/commits/refs/tags/v0.24.3) echo 66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c ;;
  *) echo "HTTP 404: Not Found" >&2; exit 1 ;;
esac
STUB
  chmod +x "$tmp/bin/gh"
  t() { # t <want: pass|fail> <description> <expected text> [--verify-remote] : the steps on stdin
    local want=$1 what=$2 needle=$3 d out rc
    shift 3
    d=$(mktemp -d "$tmp/case.XXXX")
    { printf 'on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n'; cat; } >"$d/x.yml"
    : >"$tmp/calls"
    out=$(STUB_CALLS="$tmp/calls" PATH="$tmp/bin:$PATH" "$here/check-pins.sh" "$@" "$d" 2>&1) && rc=0 || rc=$?
    if [[ $want == pass && $rc -eq 0 ]] || [[ $want == fail && $rc -ne 0 && "$out" == *"$needle"* ]]; then
      echo "ok   $what"
    else
      echo "FAIL $what (rc=$rc):"; printf '%s\n' "$out" | sed 's/^/       /'; fail=1
    fi
  }
  local good='      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1'
  t pass "a SHA pin with its tag" "" <<<"$good"
  t pass "a pin of an action in a subdirectory" "" <<<'      - uses: anchore/sbom-action/download-syft@66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c # v0.24.3'
  t pass "a named step with uses: on its own line, and a trailing note" "" <<<'      - name: checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1 (the latest)'
  t pass "a local action" "" <<<'      - uses: ./.github/actions/x'
  t fail "a tag ref" "is not pinned" <<<'      - uses: actions/checkout@v4'
  t fail "a branch ref" "is not pinned" <<<'      - uses: actions/checkout@main'
  t fail "39 hex digits" "is not pinned" <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b # v7.0.1'
  t fail "41 hex digits" "is not pinned" <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b11 # v7.0.1'
  t fail "upper-case hex" "is not pinned" <<<'      - uses: actions/checkout@3D3C42E5AAC5BA805825DA76410C181273BA90B1 # v7.0.1'
  t fail "a SHA without a comment" "is not pinned" <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1'
  t fail "a comment that is not a release tag" "is not pinned" <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # latest'
  t fail "a comment naming a major version only" "is not pinned" <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7'
  t fail "a quoted value" "is not pinned" <<<'      - uses: "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1" # v7.0.1'
  t fail "a docker:// ref" "is not pinned" <<<'      - uses: docker://alpine:3'
  t fail "a flow mapping" "block style" <<<'      - { uses: actions/checkout@main }'
  t fail "a flow mapping with the key second" "block style" <<<'      - { name: x, uses: actions/checkout@main }'
  t fail "a quoted key" "block style" <<<'      - "uses": actions/checkout@v4'
  t fail "a single-quoted key on its own line" "block style" <<<"      - name: x
        'uses': actions/checkout@v4"
  t fail "a flow sequence of steps" "block style" <<<'      [{ uses: actions/checkout@main }]'
  t fail "one bad pin among good ones" "x.yml:7 " <<<"$good
      - uses: actions/checkout@main
$good"
  t pass "--verify-remote: the SHA is the tag's commit" "" --verify-remote <<<"$good"
  t fail "--verify-remote: a SHA that is not the tag's commit" "is not the commit of actions/checkout v7.0.1" --verify-remote \
    <<<'      - uses: actions/checkout@0000000000000000000000000000000000000000 # v7.0.1'
  t fail "--verify-remote: a comment naming a tag that does not exist" "has no tag v9.9.9" --verify-remote \
    <<<'      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v9.9.9'
  t pass "--verify-remote: an action in a subdirectory is looked up in its repository" "" --verify-remote \
    <<<'      - uses: anchore/sbom-action/download-syft@66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c # v0.24.3'
  if grep -qx 'api repos/anchore/sbom-action/commits/refs/tags/v0.24.3 --jq .sha' "$tmp/calls"; then
    echo "ok   the lookup names the tag ref of the action's repository"
  else echo "FAIL gh calls: $(cat "$tmp/calls")"; fail=1; fi
  t pass "--verify-remote: one lookup per action and tag" "" --verify-remote <<<"$good
$good"
  if [[ $(wc -l <"$tmp/calls") -eq 1 ]]; then echo "ok   a repeated pin is looked up once"
  else echo "FAIL repeated lookups: $(cat "$tmp/calls")"; fail=1; fi
  t pass "no lookup without --verify-remote" "" <<<"$good"
  if [[ ! -s "$tmp/calls" ]]; then echo "ok   the offline check calls no gh"
  else echo "FAIL offline gh calls: $(cat "$tmp/calls")"; fail=1; fi
  if [[ $fail -ne 0 ]]; then echo "FAIL check-pins.sh self-test"; return 1; fi
  echo "ok   check-pins.sh self-test"
}

case "${1:-}" in
  --self-test) self_test ;;
  --verify-remote)
    command -v gh >/dev/null || { echo "check-pins: --verify-remote needs the gh CLI" >&2; exit 2; }
    check 1 "${2:-.github/workflows}"
    ;;
  -*) sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2 ;;
  *) check 0 "${1:-.github/workflows}" ;;
esac
