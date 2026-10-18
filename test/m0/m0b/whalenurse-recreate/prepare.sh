#!/usr/bin/env bash
# M0b scenario 7 (whalenurse-recreate, PLAN v3.6): whalenurse for real in a container, a parent that only a recreate repairs and
# its network_mode sidecar. Prints the one maintainer step (a docker exec that breaks the parent). Run collect.sh afterwards.
# Only the whalenurse-m0-b-recreate project. No daemon restart, no sudo.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$here/../lib.sh"
# shellcheck source=../nurse-lib.sh
source "$here/../nurse-lib.sh"
# shellcheck source=../recreate-lib.sh
source "$here/../recreate-lib.sh"

recreate_prepare
