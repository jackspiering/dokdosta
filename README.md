<p align="center">
  <img src="docs/assets/logo.svg" alt="whalenurse logo: a whale wearing a nurse cap" width="200">
</p>

# whalenurse

**Compose-aware self-healing for Docker hosts.**

When an opted-in container turns unhealthy, or stops and stays stopped, whalenurse restarts it. If that doesn't help, it recreates it from the stack's own compose files, together with any `network_mode: service:` sidecars that depend on it. If that fails too, it stops trying and tells you why.

> **Status: M2b (compose recreate, the drift guard, dependents) is built; nothing is released yet.** `whalenurse run` restarts opted-in containers that go unhealthy or stop, recreates them from their compose files if that does not help, repairs `network_mode` sidecars, and suspends what it cannot fix. Notifications, the pull rung and the dashboard are still planned. See [docs/running.md](docs/running.md) for what works today and [docs/PLAN.md](docs/PLAN.md) for the design and milestones.

## Why

Docker doesn't restart unhealthy containers. A restart policy also gives up for good when a start fails, which is common at boot when a mount or network isn't ready yet. Existing tools restart containers, but a restart isn't enough when a container has to be recreated, and it leaves sidecars that share its network namespace broken. whalenurse targets that gap:

- **Recreates from your compose files.** It uses the project, files and env file recorded on the container, and never runs `compose down`.
- **Handles sidecars.** After their parent is restarted or recreated, it brings `network_mode` dependents back in order.
- **Never upgrades by accident.** A drift guard refuses to recreate when the compose files or the image tag changed since the container was created.

## Planned features

- Opt-in per container with the `whalenurse.enable=true` label
- Escalation: restart → recreate → optional pull and recreate → notify and suspend
- Backoff, flap limit, global rate limit, pause (maintenance) mode and dry-run
- Notifications through Shoutrrr (ntfy, Gotify, Discord, Telegram and more)
- Optional read-only dashboard and JSON API
- Single static Go binary that runs as a container or under systemd

## Recreate needs two read-only mounts

To recreate, whalenurse runs the `docker compose` plugin against the compose files recorded on the container. In a container that takes two mounts (see [examples/compose.yaml](examples/compose.yaml)):

- **the host's compose plugin**, read-only, at `/host/docker-compose`. It is used first; the image also bundles a copy as a fallback, but a container made by another compose release than the bundled one is then reported as "can't verify drift" rather than recreated.
- **the stacks root, read-only, at the identical path as on the host** (`/opt/stacks:/opt/stacks:ro`). Compose passes host paths to the daemon, so `/opt/stacks:/stacks:ro` does not work. Mount anything else compose reads from outside it too: `env_file:` paths with `../`, `include:`/`extends:` files, file-based `secrets:` and `configs:`.

Without them whalenurse still restarts, says why the recreate rung is skipped, and suspends the container after the restart.

## Security

whalenurse needs access to the Docker socket, which is root-equivalent on the host. The dashboard will be off by default and read-only; expose it only on localhost, behind a reverse proxy or on a tailnet.

## Requirements (planned)

- Docker Engine v25 or later (restart only); Engine 28.1 or later and Compose v5.2.0 or later for recreate, pull and the drift guard (checked at startup: below that, one warning and the ladder ends after the restart)
- The Docker Compose plugin on the host (mounted read-only), or the copy bundled in the image
- Compose stacks in plain directories on the host

## License

MIT
