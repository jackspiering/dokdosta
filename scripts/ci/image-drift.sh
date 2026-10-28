#!/usr/bin/env bash
# Image-drift check of plan §3.6 on whatever image store the daemon uses (from M0b scenario 5; CI only).
#   scripts/ci/image-drift.sh --out DIR
#
# It creates one container from a re-taggable image, then moves the tag to another image, and at each point prints
# every candidate comparator next to the `com.docker.compose.image` label that compose recorded:
#   a comparator is USABLE if it equals the label before the re-tag and differs from it after.
# It also records what `compose --dry-run --progress json up` says (the oracle): no `Recreate` before, `Recreate` after.
# Exit 0 only if at least one comparator is usable and the oracle behaves.
#
# Docker safety (fails closed): it runs only in GitHub Actions, acts only on the compose project dokdosta-test-imgdrift
# and the tag dokdosta-test/imgdrift:latest, pulls only busybox:1.37 and alpine:3.22, and only on the local socket.
set -euo pipefail

die() { printf 'image-drift: REFUSED: %s\n' "$*" >&2; exit 3; }

[[ "${GITHUB_ACTIONS-}" == true ]] || die "runs only in GitHub Actions (it changes images and containers)"
out=""
while (($#)); do
  case "$1" in
    --out) out="${2:?--out DIR}"; shift 2 ;;
    *) die "usage: image-drift.sh --out DIR" ;;
  esac
done
[[ -n "$out" ]] || die "usage: image-drift.sh --out DIR"
for t in jq docker; do command -v "$t" >/dev/null || die "$t is required"; done

# Environment that could point compose or docker at something else.
unset COMPOSE_PROJECT_NAME COMPOSE_FILE COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_PATH_SEPARATOR DOCKER_CONTEXT DOCKER_CONFIG
case "${DOCKER_HOST-}" in
  "" | unix:///var/run/docker.sock | unix:///run/docker.sock) unset DOCKER_HOST ;;
  *) die "DOCKER_HOST=$DOCKER_HOST: only the local socket is allowed" ;;
esac
[[ "$(docker context show 2>/dev/null)" == default ]] || die "the docker context is not 'default'"

readonly P=dokdosta-test-imgdrift ref=dokdosta-test/imgdrift:latest
[[ "$P" =~ ^dokdosta-test-[a-z0-9][a-z0-9_-]*$ ]] || die "project '$P' is not a dokdosta-test-* project"
[[ -z "$(docker ps -aq --filter "label=com.docker.compose.project=$P")" ]] || die "project $P already exists"

umask 077
dir="$out/stack"
mkdir -p "$dir"
cat >"$dir/compose.yaml" <<YAML
services:
  tagged:
    image: $ref
    command: ["sh", "-c", "while true; do sleep 1; done"]
    stop_grace_period: 1s
YAML
dc() { docker compose -p "$P" --project-directory "$dir" -f "$dir/compose.yaml" "$@"; }
pull() { case "$1" in busybox:1.37 | alpine:3.22) docker pull -q "$1" >/dev/null ;; *) die "pull of '$1' not allowed" ;; esac; }
tag() { docker tag "$1" "$ref"; }
cleanup() { docker compose -p "$P" down -v --timeout 2 --remove-orphans >/dev/null 2>&1 || true; docker rmi "$ref" >/dev/null 2>&1 || true; }
trap cleanup EXIT

sum="$out/summary.txt"
: >"$sum"
say() { printf '%s\n' "$*" | tee -a "$sum"; }

for b in busybox:1.37 alpine:3.22; do docker image inspect "$b" >/dev/null 2>&1 || pull "$b"; done
store="$(docker info --format '{{.Driver}}')"
arch=$(docker version --format '{{.Server.Arch}}')
tag busybox:1.37
dc up -d --pull never --no-build >"$out/up.txt" 2>&1 || { cat "$out/up.txt"; die "compose up failed"; }
c=$(docker ps -aq --no-trunc --filter "label=com.docker.compose.project=$P" \
  --filter "label=com.docker.compose.service=tagged" --filter 'label=com.docker.compose.oneoff=False' | head -1)
[[ -n "$c" ]] || die "no container for $P/tagged"

short() { printf '%.19s' "$1"; }
# Each comparator prints the value to compare with the label, or "ERR: <message>".
cmp_ids=(ref.Id ref.RepoDigest ref.Descriptor.digest ref.Descriptor.digest@platform ref.Id@platform)
val() {
  case "$1" in
    ref.Id) docker image inspect --format '{{.Id}}' "$ref" 2>&1 ;;
    ref.RepoDigest) docker image inspect --format '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' "$ref" 2>&1 | sed 's/.*@//' ;;
    ref.Descriptor.digest) docker image inspect --format '{{if .Descriptor}}{{.Descriptor.digest}}{{end}}' "$ref" 2>&1 ;;
    ref.Descriptor.digest@platform) docker image inspect --platform "linux/$arch" --format '{{if .Descriptor}}{{.Descriptor.digest}}{{end}}' "$ref" 2>&1 ;;
    ref.Id@platform) docker image inspect --platform "linux/$arch" --format '{{.Id}}' "$ref" 2>&1 ;;
  esac | head -1 | sed 's/^Error.*/ERR: &/' | cut -c1-120
}
oracle() { # texts compose prints for the container, sorted unique
  dc --dry-run --progress json up -d --no-deps --pull never --no-build tagged 2>&1 |
    jq -R -r 'fromjson? | select(.id | test("-tagged-1$")) | .text' | sort -u | paste -sd' ' -
}

label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.image"}}' "$c")
say "# image-drift: store=$store driver=$(docker info --format '{{json .DriverStatus}}') engine=$(docker version --format '{{.Server.Version}}') compose=$(docker compose version --short) arch=$arch"
say "container .Image        = $(short "$(docker inspect --format '{{.Image}}' "$c")")"
say "container .Config.Image = $(docker inspect --format '{{.Config.Image}}' "$c")"
say "label com.docker.compose.image = $(short "$label")"
declare -A before after
for k in "${cmp_ids[@]}"; do before[$k]=$(val "$k" || true); done
before[container.Image@ref.Id]=$([[ "$(docker inspect --format '{{.Image}}' "$c")" == "$(docker image inspect --format '{{.Id}}' "$ref")" ]] && echo equal || echo differs)
oracle_before=$(oracle || true)
say "before the re-tag:"
for k in "${cmp_ids[@]}"; do say "  $(printf '%-34s' "$k") $(short "${before[$k]}")   $([[ "${before[$k]}" == "$label" ]] && echo '== label' || echo '!= label')"; done
say "  $(printf '%-34s' 'container.Image == ref.Id') ${before[container.Image@ref.Id]}"
say "  oracle: $oracle_before"

tag alpine:3.22
for k in "${cmp_ids[@]}"; do after[$k]=$(val "$k" || true); done
after[container.Image@ref.Id]=$([[ "$(docker inspect --format '{{.Image}}' "$c")" == "$(docker image inspect --format '{{.Id}}' "$ref")" ]] && echo equal || echo differs)
oracle_after=$(oracle || true)
say "after moving the tag to alpine:"
for k in "${cmp_ids[@]}"; do say "  $(printf '%-34s' "$k") $(short "${after[$k]}")   $([[ "${after[$k]}" == "$label" ]] && echo '== label' || echo '!= label')"; done
say "  $(printf '%-34s' 'container.Image == ref.Id') ${after[container.Image@ref.Id]}"
say "  oracle: $oracle_after"

usable=()
for k in "${cmp_ids[@]}"; do
  [[ "${before[$k]}" != ERR* && -n "${before[$k]}" && "${before[$k]}" == "$label" && "${after[$k]}" != "$label" ]] && usable+=("$k")
done
[[ "${before[container.Image@ref.Id]}" == equal && "${after[container.Image@ref.Id]}" == differs ]] && usable+=("container.Image==ref.Id")
say ""
say "usable comparators (equal to the label before, different after): ${usable[*]:-none}"
oracle_ok=no
if [[ " $oracle_before " != *" Recreate "* && " $oracle_after " == *" Recreate "* ]]; then oracle_ok=yes; fi
say "dry-run oracle: no Recreate before, Recreate after: $oracle_ok"

jq -n --arg store "$store" --arg driver "$(docker info --format '{{json .DriverStatus}}')" --arg engine "$(docker version --format '{{.Server.Version}}')" \
  --arg compose "$(docker compose version --short)" --arg label "$label" --arg ob "$oracle_before" --arg oa "$oracle_after" \
  --argjson usable "$(printf '%s\n' "${usable[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')" --arg oracle_ok "$oracle_ok" \
  '{store: $store, driver: $driver, engine: $engine, compose: $compose, label: $label, oracle_before: $ob, oracle_after: $oa, usable: $usable, oracle_ok: ($oracle_ok == "yes")}' >"$out/verdict.json"
say "verdict: $out/verdict.json"
((${#usable[@]} > 0)) && [[ "$oracle_ok" == yes ]]
