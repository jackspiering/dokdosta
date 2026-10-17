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
# usage: scripts/ci/setup-docker.sh classic|containerd [compose-version|latest]     (default compose: 5.6.0)
# It exports WHALENURSE_COMPOSE_BIN and WHALENURSE_COMPOSE_VERSION to later steps through $GITHUB_ENV.
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

sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker-m0b.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker-m0b.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker-m0b.list
sudo apt-get update
sudo apt-get install -y --allow-change-held-packages --allow-downgrades docker-ce='5:29.*' docker-ce-cli='5:29.*' containerd.io docker-compose-plugin
docker version --format 'engine {{.Server.Version}} (API {{.Server.APIVersion}})'

# Compose: the release asked for, replacing whatever apt installed.
auth=()
if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}"); fi
if [ "$want" = latest ]; then
  tag=$(curl -fsSL "${auth[@]}" https://api.github.com/repos/docker/compose/releases/latest | jq -r .tag_name)
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
curl -fsSL -o "$dir/$asset" "$base/$asset"
curl -fsSL -o "$dir/$asset.sha256" "$base/$asset.sha256"
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
  { echo "WHALENURSE_COMPOSE_BIN=${plugin}"; echo "WHALENURSE_COMPOSE_VERSION=${got#v}"; } >>"$GITHUB_ENV"
fi

if [ "$store" = containerd ]; then
  echo '{"features":{"containerd-snapshotter":true}}' | sudo tee /etc/docker/daemon.json
else
  echo '{"features":{"containerd-snapshotter":false},"storage-driver":"overlay2"}' | sudo tee /etc/docker/daemon.json
fi
sudo systemctl restart docker
docker info --format 'storage driver: {{.Driver}} / {{json .DriverStatus}}'
