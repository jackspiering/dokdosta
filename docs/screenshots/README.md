# Dashboard screenshots (M5)

Taken on 19 Oct 2026 from the branch `t3/m5-dashboard`, against a real demo stack (compose project `whalenurse-test-demo`, busybox containers only) managed by a real `whalenurse run` with the dashboard on `127.0.0.1` behind a token. Headless Chromium through Playwright, full page, device scale 1, HTTP Basic with the token as the password (the way a browser sends it). The host is called `demo-nas`; the paths are the demo stack's. Nothing here is personal data. Each image exists as `-light` and `-dark` (`prefers-color-scheme`), at `desktop` (1440×900 viewport) and `phone` (390×844, touch, mobile).

The demo state, as the pages show it (names are `whalenurse-test-demo-<service>-1`):

| Service | State shown |
|---|---|
| `web`, `app` | healthy |
| `app-sidecar` | healthy sidecar with `network_mode: service:app`; parent `app` |
| `api` | open incident: restarted once, waiting out the cooldown before the recreate |
| `db` | **SUSPENDED (drift)**: its compose file was edited after it was created, so the drift guard blocked the recreate |
| `cache` | **PAUSED** for 2 h (`whalenurse pause whalenurse-test-demo/cache --for 2h`) and unhealthy |
| `worker` | **upgraded during heal**: its image tag moved under it, the recreate took the new image (`heal.pull=true`), incident recovered, last upgrade with the rollback commands |

| File prefix | What it shows |
|---|---|
| `overview-*` | healer status, compose binary and version, the version gate, action budget, container counts, the pause with its expiry, "Needs attention" (`api`, `cache`, `db`) and the latest events |
| `containers-*` | the watched containers with state, health, healer badges (Incident, PAUSED with expiry, SUSPENDED: drift, ok), incident and rung progress, cooldown and flap count. On the phone the table becomes stacked cards |
| `detail-suspended-*` | `db`: the SUSPENDED banner with the cause and the `unsuspend` command, inspect summary, healer, the drift guard's verdict (blocked, with the finding), incidents, timeline, labels |
| `detail-sidecar-*` | `app-sidecar`: the parent link under "Related" |
| `detail-upgraded-*` | `worker`: "Last upgrade during heal" with the old and new image and the two rollback commands, the closed incident, the timeline |
| `history-*` | incidents (two open, one recovered) and the event list with its filter chips |
| `containers-tablet700-light.png` | **evidence of a layout problem, not a design**: the containers page at 700 px wide (a tablet in portrait), where the table is wider than the screen and the Cooldown and Flaps columns are cut off |

To regenerate: start a demo like the one above and run Playwright against it; the script is not in the repo.
