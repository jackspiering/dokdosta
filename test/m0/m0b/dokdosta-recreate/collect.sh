#!/usr/bin/env bash
# M0b scenario 7 (dokdosta-recreate): collect after the step. Works from a fresh session; safe to run repeatedly (it only reads).
#   collect.sh [--watch SECS] [state dir]      (default: the newest dokdosta-recreate state; waits up to 240 s)
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$here/../lib.sh"
# shellcheck source=../nurse-lib.sh
source "$here/../nurse-lib.sh"
# shellcheck source=../recreate-lib.sh
source "$here/../recreate-lib.sh"

recreate_collect "$@"
