# Dashboard screenshots (M5)

Retaken on 19 Oct 2026 (after the last review fixes; `detail-sidecar-*` is from the round before, it shows none of the changed texts) from the branch `t3/m5-dashboard`, against a real demo stack (compose project `whalenurse-test-demo`, busybox containers only) managed by a real `whalenurse run` with the dashboard on `127.0.0.1` behind a token. Headless Chromium through Playwright, full page, device scale 1, HTTP Basic with the token as the password (the way a browser sends it). The host is called `demo-nas`; the paths are the demo stack's. Nothing here is personal data. Each image exists as `-light` and `-dark` (`prefers-color-scheme`), at `desktop` (1440×900 viewport) and `phone` (390×844, touch, mobile); the three pages that matter most (`overview`, `containers`, `detail-upgraded`) also at `tablet` (768×1024), because the tables turn into cards up to 900 px.

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
| `containers-*` | the watched containers with state, health, healer badges (Incident, PAUSED with "1h 57m left (until 19:05)", SUSPENDED: drift, ok), incident and rung progress, cooldown and flap count. Up to 900 px the table becomes cards with a label and its value on one line, without the fields that say "none" |
| `detail-suspended-*` | `db`: the SUSPENDED banner with the cause and the `unsuspend` command, inspect summary, healer, the plain-language SUSPENDED sentence (the raw text under "details"), the ladder with the blocked rung (`recreate ✗ blocked (drift)`), the drift guard's verdict, incidents, a short `Suspended` row next to the `DriftBlocked` row, labels |
| `detail-sidecar-*` | `app-sidecar`: the parent under "Related", with a badge for its state |
| `detail-upgraded-*` | `worker`: "Last upgrade during heal" with the old and new image and the two rollback commands (one per line, the Copy button beside each, never over the text), the closed incident, the timeline |
| `history-*` | incidents (two open, one recovered) and the event list with its filter chips; the pause reads "for 2h, until 19:05" |
| `*-tablet-*` | the same pages at 768 px: cards, no horizontal scroll |

Also checked with the same demo state, headless Chromium, light: 27 widths from 320 to 1440 px (including 640/641 and 899/900/901) on 9 pages, none with a page-wide horizontal scroll or an element outside the viewport; every internal link of 27 pages returns 200; the two Copy buttons put exactly the command in the clipboard and do not exist without JavaScript; no console errors or CSP violations; lowest text contrast 5.2:1 (light) and 6.1:1 (dark); no token, user name or `/home` path in any page.

To regenerate: start a demo like the one above and run Playwright against it; the script is not in the repo.
