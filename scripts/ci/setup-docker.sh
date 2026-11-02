#!/usr/bin/env bash
# Install the latest Docker Engine 29.x and a pinned Docker Compose on a GitHub-hosted Ubuntu runner, then select the image
# store and restart Docker. CI only: it uses sudo, rewrites /etc/docker/daemon.json and restarts the daemon, so never run it
# on a host you care about.
#
# The runner ships Engine 28.0.4 and compose 2.38.x. `docker image inspect --platform` needs Engine 28.1 or later and the
# dry-run oracle is pinned to compose v5.x (plan section 6), so install what the plan's matrix names. Compose is installed
# from the GitHub release (checksum verified) rather than from apt, which may not carry every release of the matrix.
# The image-drift job and the integration job both call this script.
#
# Every download retries and has a time limit, and apt reads only Docker's own list: a full `apt-get update` once hung for an
# hour on the runner's Ubuntu mirror (job 122234849509, 27 Oct 2026). The restart after the image-store switch is tried
# three times and prints the daemon's journal if it still fails (it failed twice with no reason in the log, Oct 2026).
#
# usage: scripts/ci/setup-docker.sh classic|containerd [compose-version|latest]     (default compose: 5.6.0)
# It exports DOKDOSTA_COMPOSE_BIN and DOKDOSTA_COMPOSE_VERSION to later steps through $GITHUB_ENV.
set -euo pipefail

store=${1:-}
want=${2:-5.6.0}
case "$store" in
  classic | containerd) ;;
  *)
    echo "usage: $0 classic|containerd [compose-version|latest]" >&2
    exit 2
    ;;
esac

if [ "${GITHUB_ACTIONS:-}" != "true" ]; then
  echo "refusing to run outside GitHub Actions: this rewrites the Docker install and daemon.json" >&2
  exit 2
fi

# Five retries 5 s apart: GitHub's release download answered 500 for about 7 s once (job 125476857201, 2 Nov 2026), longer
# than curl's default backoff (1, 2, 4 s) waits.
fetch=(curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors --connect-timeout 20 --max-time 120)
list=/etc/apt/sources.list.d/docker-m0b.list
# Only Docker's list: the packages below all come from it, and the runner's own mirrors are where apt hung.
apt_opts=(-o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30)
sudo install -m 0755 -d /etc/apt/keyrings
sudo "${fetch[@]}" https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker-m0b.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker-m0b.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee "$list"
install_docker() {
  sudo apt-get install -y "${apt_opts[@]}" --allow-change-held-packages --allow-downgrades \
    docker-ce='5:29.*' docker-ce-cli='5:29.*' containerd.io docker-compose-plugin
}
sudo apt-get update "${apt_opts[@]}" -o Dir::Etc::sourcelist="$list" -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0
# Their dependencies are on the runner already (it ships Docker); if one is not, read every list once, with the time limits.
if ! install_docker; then
  echo "::warning::installing from Docker's list alone failed; reading every apt list and trying again"
  sudo apt-get update "${apt_opts[@]}"
  install_docker
fi
docker version --format 'engine {{.Server.Version}} (API {{.Server.APIVersion}})'

# Compose: the release asked for, replacing whatever apt installed.
auth=()
if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}"); fi
if [ "$want" = latest ]; then
  tag=$("${fetch[@]}" "${auth[@]}" https://api.github.com/repos/docker/compose/releases/latest | jq -r .tag_name)
else
  tag="v${want#v}"
fi
case "$(uname -m)" in
  x86_64) asset=docker-compose-linux-x86_64 ;;
  aarch64) asset=docker-compose-linux-aarch64 ;;
  *) echo "no compose release asset for $(uname -m)" >&2; exit 1 ;;
esac
dir=$(mktemp -d)
base="https://github.com/docker/compose/releases/download/${tag}"
"${fetch[@]}" -o "$dir/$asset" "$base/$asset"
"${fetch[@]}" -o "$dir/$asset.sha256" "$base/$asset.sha256"
(cd "$dir" && echo "$(awk '{print $1}' "$asset.sha256")  $asset" | sha256sum -c -)
plugin=/usr/libexec/docker/cli-plugins/docker-compose
sudo install -m 0755 -d "$(dirname "$plugin")"
sudo install -m 0755 "$dir/$asset" "$plugin"
rm -rf "$dir"
got=$(docker compose version --short)
if [ "$want" != latest ] && [ "${got#v}" != "${want#v}" ]; then
  echo "asked for compose $want, got $got" >&2
  exit 1
fi
echo "compose ${got} at ${plugin} (asked for ${want}, release ${tag})"
docker compose version
if [ -n "${GITHUB_ENV:-}" ]; then
  { echo "DOKDOSTA_COMPOSE_BIN=${plugin}"; echo "DOKDOSTA_COMPOSE_VERSION=${got#v}"; } >>"$GITHUB_ENV"
fi

if [ "$store" = containerd ]; then
  echo '{"features":{"containerd-snapshotter":true}}' | sudo tee /etc/docker/daemon.json
else
  echo '{"features":{"containerd-snapshotter":false},"storage-driver":"overlay2"}' | sudo tee /etc/docker/daemon.json
fi
for attempt in 1 2 3; do
  if sudo systemctl restart docker && timeout 60 sh -c 'until docker info >/dev/null 2>&1; do sleep 1; done'; then break; fi
  echo "::warning::docker did not come back after the restart (attempt $attempt of 3)"
  if [ "$attempt" -eq 3 ]; then
    sudo systemctl status docker --no-pager || true
    sudo journalctl -u docker -u containerd --no-pager -n 80 || true
    exit 1
  fi
  sudo systemctl reset-failed docker docker.socket || true
  sleep 5
done
docker info --format 'storage driver: {{.Driver}} / {{json .DriverStatus}}'
