#!/usr/bin/env bash
# Print the release notes of one version: CHANGELOG.md's "## [X.Y.Z] - YYYY-MM-DD" section, without its heading.
#   scripts/release/release-notes.sh v0.1.0 [CHANGELOG.md]
#   scripts/release/release-notes.sh --self-test
# It fails closed, so a tag can never publish a release with empty or made-up notes: no such section, a date that is not a
# YYYY-MM-DD date (the placeholder the maintainer replaces before tagging, docs/releasing.md), or an empty body is an error.
# The release workflow passes the output to `goreleaser release --release-notes`.
# A && ok || bad is the test idiom here (ok and bad never fail), as in the other self-tests.
# shellcheck disable=SC2015
set -euo pipefail

notes() { # notes <version without v> <file>
  local version="$1" file="$2"
  [[ -f "$file" ]] || { echo "release-notes: $file not found" >&2; return 1; }
  local heading
  # The heading is "## [version] - date"; the version is matched literally.
  heading=$(awk -v want="## [$version]" 'index($0, want) == 1 { print; exit }' "$file")
  [[ -n "$heading" ]] || { echo "release-notes: $file has no section '## [$version] - YYYY-MM-DD'" >&2; return 1; }
  if ! [[ "$heading" =~ ^##\ \[[^]]+\]\ -\ [0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
    echo "release-notes: the heading '$heading' needs a release date as '## [$version] - YYYY-MM-DD' (set it before tagging)" >&2
    return 1
  fi
  local body
  body=$(awk -v want="$heading" '
    $0 == want { on = 1; next }
    on && /^## / { exit }
    on && /^\[[^]]+\]: / { next }  # the link references at the end of the file
    on { print }
  ' "$file")
  # Trim leading and trailing blank lines.
  body=$(printf '%s\n' "$body" | awk 'NF { found = 1 } found { print }' | awk '{ lines[NR] = $0 } END { n = NR; while (n > 0 && lines[n] ~ /^[[:space:]]*$/) n--; for (i = 1; i <= n; i++) print lines[i] }')
  [[ -n "${body//[[:space:]]/}" ]] || { echo "release-notes: the section '$heading' is empty" >&2; return 1; }
  printf '%s\n' "$body"
}

self_test() {
  local tmp fail=0
  tmp=$(mktemp -d)
  ok() { printf 'ok   %s\n' "$1"; }
  bad() { printf 'FAIL %s\n' "$1"; fail=1; }
  cat >"$tmp/CHANGELOG.md" <<'MD'
# Changelog

## [Unreleased]

## [0.2.0] - 2026-11-01

### Added

- Two.

## [0.1.1] - YYYY-MM-DD

- Not dated yet.

## [0.1.0-rc.1] - 2026-10-30

- A release candidate.

## [0.1.0] - 2026-10-25

### Added

- One.
- Another, with `code` and a [link](https://example.com).

## [0.0.9] - 2026-10-01

## [0.0.8] - 2026-09-01

- Eight.

[Unreleased]: https://example.com/compare
[0.1.0]: https://example.com/releases/v0.1.0
MD
  local out
  out=$(notes 0.1.0 "$tmp/CHANGELOG.md") && [[ "$out" == $'### Added\n\n- One.\n- Another, with `code` and a [link](https://example.com).' ]] && ok "the section of 0.1.0, nothing of its neighbours" || { bad "the section of 0.1.0: $out"; }
  out=$(notes 0.2.0 "$tmp/CHANGELOG.md") && [[ "$out" == $'### Added\n\n- Two.' ]] && ok "the newest section" || bad "the newest section: $out"
  out=$(notes 0.0.8 "$tmp/CHANGELOG.md") && [[ "$out" == '- Eight.' ]] && ok "the last section stops before the link references" || bad "the last section: $out"
  out=$(notes 0.1.0-rc.1 "$tmp/CHANGELOG.md") && [[ "$out" == '- A release candidate.' ]] && ok "a pre-release version" || bad "a pre-release: $out"
  notes 0.1.1 "$tmp/CHANGELOG.md" >/dev/null 2>&1 && bad "an undated section was accepted" || ok "an undated section is refused"
  notes 0.0.9 "$tmp/CHANGELOG.md" >/dev/null 2>&1 && bad "an empty section was accepted" || ok "an empty section is refused"
  notes 0.3.0 "$tmp/CHANGELOG.md" >/dev/null 2>&1 && bad "a missing section was accepted" || ok "a missing section is refused"
  notes 0.1 "$tmp/CHANGELOG.md" >/dev/null 2>&1 && bad "0.1 matched 0.1.0" || ok "a version is matched exactly, not as a prefix"
  notes Unreleased "$tmp/CHANGELOG.md" >/dev/null 2>&1 && bad "Unreleased was accepted" || ok "Unreleased is refused"
  notes 0.1.0 "$tmp/missing.md" >/dev/null 2>&1 && bad "a missing file was accepted" || ok "a missing file is refused"
  rm -rf "$tmp"
  return "$fail"
}

main() {
  if [[ "${1:-}" == "--self-test" ]]; then
    self_test
    return
  fi
  [[ $# -ge 1 && $# -le 2 ]] || { echo "usage: $0 <tag> [CHANGELOG.md] | --self-test" >&2; return 2; }
  local tag="$1" file="${2:-CHANGELOG.md}"
  notes "${tag#v}" "$file"
}

main "$@"
