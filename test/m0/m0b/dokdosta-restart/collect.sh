#!/usr/bin/env bash
# M0b scenario 6 (dokdosta-restart): collect after the daemon restart. Works from a fresh session; safe to run repeatedly
# (it only reads; run it again after a minute if it says INCOMPLETE).
#   collect.sh [--watch SECS] [state dir]      (default: the newest dokdosta-restart state; waits up to 240 s)
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$here/../lib.sh"
# shellcheck source=../nurse-lib.sh
source "$here/../nurse-lib.sh"

nurse_collect "$@"
