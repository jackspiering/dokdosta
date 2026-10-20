#!/usr/bin/env bash
# Is a change documentation only? Then CI skips the jobs documentation cannot affect (plan section 6, *CI matrix*).
#   git diff --name-only <base> <head> | scripts/ci/changes.sh     print "docs" or "code"
#   scripts/ci/changes.sh --self-test
# Documentation is a Markdown file anywhere (*.md) and anything under docs/ (Markdown, the logo, the screenshots). The build,
# vet and unit-test job always runs: its tests check the documentation (TestMarkdownLinksResolve,
# TestEveryVariableAndLabelIsDocumented). Fails closed: an empty list, or one file that is not documentation, is "code".
set -euo pipefail

classify() {
  local f n=0 kind=docs
  # Read every line (no early return): the producer of the list must not die of SIGPIPE under pipefail.
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    n=$((n + 1))
    case "$f" in
      *.md | docs/*) ;;
      *) kind=code ;;
    esac
  done
  if [ "$n" -eq 0 ]; then kind=code; fi
  echo "$kind"
}

self_test() {
  local fail=0
  t() { # t <want> <description> <file>...
    local want=$1 what=$2 got
    shift 2
    got=$(if [ $# -gt 0 ]; then printf '%s\n' "$@"; fi | classify)
    if [ "$got" = "$want" ]; then echo "ok   $what"; else echo "FAIL $what: got $got, want $want"; fail=1; fi
  }
  t docs "the README alone" README.md
  t docs "the changelog, the plan and CLAUDE.md" CHANGELOG.md docs/PLAN.md CLAUDE.md
  t docs "a screenshot and the logo under docs/" docs/screenshots/overview-desktop-dark.png docs/assets/logo.svg
  t docs "an example's README" examples/demo/README.md
  t docs "a path with a space" "docs/a b.md"
  t code "no files at all"
  t code "an empty line only" ""
  t code "Go code" internal/engine/engine.go
  t code "documentation and one Go file" README.md internal/engine/engine.go docs/running.md
  t code "the workflow" .github/workflows/ci.yml
  t code "a Dockerfile" Dockerfile
  t code "a directory that only starts with docs" docsite/index.html
  t code "a name that only contains .md" README.md.orig
  t code "an example's compose file" examples/demo/compose.yaml
  if [ "$fail" -ne 0 ]; then echo "FAIL changes.sh self-test"; return 1; fi
  echo "ok   changes.sh self-test"
}

case "${1:-}" in
  --self-test) self_test ;;
  "") classify ;;
  *) sed -n '2,/^set -/p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2 ;;
esac
