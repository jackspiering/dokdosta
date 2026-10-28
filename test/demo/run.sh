#!/usr/bin/env bash
# CI-only check of the README's "Try it in 2 minutes" path: examples/demo, started the way the README says, with the image
# under test. It breaks bop for real (unhealthy, sticky) and waits for the whole ladder: a restart that does not help, a
# recreate that does, the sidecar recreated behind its parent, and the dashboard saying so.
#   GITHUB_ACTIONS=true test/demo/run.sh <image>
# SAFETY (fails closed): it refuses to run outside GitHub Actions (a throwaway runner), and it acts only on the compose
# project dokdosta-test-demo (always passed with -p; the demo file's own name is overridden).
set -euo pipefail

[[ "${GITHUB_ACTIONS:-}" == "true" ]] || { echo "REFUSED: test/demo/run.sh runs only in GitHub Actions" >&2; exit 2; }
image="${1:?usage: test/demo/run.sh <image>}"
P=dokdosta-test-demo
[[ "$P" =~ ^dokdosta-test-[a-z0-9][a-z0-9_-]*$ ]] || { echo "REFUSED: project $P" >&2; exit 2; }
[[ -z "$(docker ps -aq --filter "label=com.docker.compose.project=$P")" ]] || { echo "project $P already exists" >&2; exit 2; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/dokdosta-demo.XXXXXX")"
# mktemp makes the directory mode 0700, and dokdosta (uid 65532) must be able to read the stack it recreates from.
chmod 0755 "$dir"
cp "$root/examples/demo/compose.yaml" "$dir/compose.yaml"
cd "$dir"
{
  echo "DOCKER_GID=$(stat -c %g /var/run/docker.sock)"
  echo "DEMO_DIR=$dir"
  echo "DOKDOSTA_IMAGE=$image"
  [[ -z "${DOKDOSTA_COMPOSE_BIN:-}" ]] || echo "COMPOSE_PLUGIN=$DOKDOSTA_COMPOSE_BIN"
} >.env
dc() { docker compose -p "$P" "$@"; }
cleanup() {
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "---- dokdosta logs"; dc logs --no-color dokdosta 2>&1 | tail -n 80 || true
    echo "---- containers"; dc ps -a 2>&1 || true
  fi
  dc down -v --remove-orphans >/dev/null 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
# wait <seconds> <description> <command...>: poll until the command succeeds.
wait_for() {
  local limit="$1" what="$2" i
  shift 2
  for ((i = 0; i < limit; i++)); do
    if "$@" >/dev/null 2>&1; then echo "ok: $what (${i}s)"; return 0; fi
    sleep 1
  done
  echo "TIMEOUT after ${limit}s: $what" >&2
  return 1
}
cid() { dc ps -aq "$1"; }
health() { docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$(cid "$1")"; }
is_healthy() { [[ "$(health "$1")" == healthy ]]; }
api() { curl -fsS "http://127.0.0.1:8484$1"; }
events() { api '/api/v1/events?limit=500'; }

step "start the demo"
dc config -q
dc up -d
wait_for 120 "bop is healthy" is_healthy bop
wait_for 60 "the sidecar is healthy" is_healthy bop-sidecar
wait_for 90 "dokdosta is healthy" is_healthy dokdosta
wait_for 30 "the dashboard answers on localhost" curl -fsS -o /dev/null http://127.0.0.1:8484/
api /api/v1/status | jq -e '.host == "demo"' >/dev/null
listed() { curl -fsS http://127.0.0.1:8484/containers | grep -q bop; }
wait_for 60 "the dashboard lists bop" listed

step "break bop: unhealthy, and sticky so that a restart does not fix it"
old_bop=$(cid bop)
old_side=$(cid bop-sidecar)
dc exec -T bop bop unhealthy sticky=true
wait_for 60 "bop is unhealthy" bash -c "[[ \"\$(docker inspect -f '{{.State.Health.Status}}' $old_bop)\" == unhealthy ]]"

step "dokdosta restarts it, then recreates it, and the sidecar follows"
recreated() { [[ "$(cid bop)" != "$old_bop" ]]; }
wait_for 240 "bop was recreated (new container id)" recreated
wait_for 120 "the new bop is healthy" is_healthy bop
sidecar_follows() {
  [[ "$(cid bop-sidecar)" != "$old_side" ]] && is_healthy bop-sidecar &&
    [[ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$(cid bop-sidecar)")" == "container:$(cid bop)" ]]
}
wait_for 180 "the sidecar was recreated behind its parent and is healthy" sidecar_follows

step "the event history says what happened"
ev=$(events)
echo "$ev" | jq -e '[.events[] | select(.type == "ActionResult" and (.message | startswith("restart")))] | length >= 1' >/dev/null || { echo "$ev" | jq -r '.events[] | "\(.type) \(.message)"'; echo "no restart in the history" >&2; exit 1; }
echo "$ev" | jq -e '[.events[] | select(.type == "ActionResult" and (.message | startswith("recreate")))] | length >= 1' >/dev/null || { echo "$ev" | jq -r '.events[] | "\(.type) \(.message)"'; echo "no recreate in the history" >&2; exit 1; }
echo "$ev" | jq -e '[.events[] | select(.type == "Suspended")] | length == 0' >/dev/null || { echo "the demo ended SUSPENDED" >&2; exit 1; }
echo "$ev" | jq -r '.events[] | select(.type == "IncidentOpened" or .type == "ActionResult" or .type == "Recovered") | "\(.type) \(.message)"'
echo
echo "DEMO OK"
