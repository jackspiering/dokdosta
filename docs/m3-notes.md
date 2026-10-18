# M3 notes

Decisions, deviations and open questions for milestone M3 (notifications) of [PLAN.md](PLAN.md). The rules that changed are in the plan as **v3.7** (proposed with this milestone, awaiting review; §3.12 is the new section). Where the plan was ambiguous this file says what was chosen and why, so the maintainer can overrule it.

## Upstream check (18 Oct 2026): Shoutrrr

| Item | Result | Source |
|---|---|---|
| Module | `github.com/nicholas-fedor/shoutrrr`, **v0.22.0** (13 Oct 2026), the latest; pulled in with `go get` | proxy.golang.org, GitHub releases |
| Licence | **MIT** (`LICENSE.md`, "Copyright (c) 2026 Nicholas Fedor"); the parent `containrrr/shoutrrr` is MIT too | repo |
| Relationship | a fork of `containrrr/shoutrrr` (created Feb 2025, not archived, 185 stars, 4 open issues) | GitHub API |
| Maintenance | releases v0.19.0 (2 Sep), v0.20.0 (8 Sep), v0.21.0 (15 Sep), v0.21.1 (22 Sep), v0.21.2 (6 Oct), v0.22.0 (13 Oct); commits on the day of the check. **Effectively one maintainer** (30 contributors in all, renovate does the dependency bumps). The original: last release v0.8.0 (Aug 2023), last commit 9 May 2026, so the fork is the one to use | GitHub API |
| Go | `go 1.27`, toolchain 1.27.2: the same as this module | go.mod |
| Services | ntfy, Gotify, Discord, Telegram, Slack, Matrix, Mattermost, Rocket.Chat, Teams, Google Chat, Zulip, Signal, XMPP, Lark, WeCom, Pushover, Pushbullet, SignalGrid, IFTTT, Bark, Join, Home Assistant, MQTT, SMTP, Twilio, OpsGenie, PagerDuty, Notifiarr, `generic` (a webhook, with a JSON template), a logger | `pkg/services/` at v0.22.0 |
| What v0.22.0 changed for us | context-aware sends (`SendContext`), a send budget per service (10 s default) and `SenderOptions{Timeout}`, errors and logs without service URLs or credentials, `Send` results in URL order as `*types.TargetError`, the older constructors deprecated (`CreateSender`, `router.New` ...) | v0.22.0 release notes |
| Cost | the module pulls MQTT (paho), XMPP (mellium) and websocket code through the root package's service registry: **19.6 MB → 28.0 MB** for the `CGO_ENABLED=0 -trimpath` binary (+8.5 MB), 39 modules in the build. `modernc.org/sqlite` is still the larger part. Not worth a fork for v1 | measured at 51c0c82 and here |

How whalenurse uses it: one `shoutrrr.CreateSenderWithOptions(types.SenderOptions{Timeout: …}, url)` per URL (one router per URL, so a failure is that URL's and delivery cannot hold up another) and `SendContext` with a deadline and the title as the `title` param. The generic service's plain template posts only the message, so a webhook receiver that wants the title should use `template=json` (`generic://host/path?template=json&disabletls=yes`, which the integration tests use).

## Decisions the plan leaves open

1. **What is notified.** The task's list plus three that the engine already emits and a person would want: `DriftWarned` (§3.6 says `warn` "says so"), `NeedsRecreate` (a recreate that cannot run: otherwise the user never learns that healing is not happening), and `RungsDisabled` (that is the version-gate warning). Left in the log and the store: `IncidentOpened`, `ActionStarted`, `Unsuspended` (the `Recovered` that follows is the news), `RateLimited` (whalenurse's own action budget; add it if you want to hear about host-wide storms), `RungUnavailable`, `WaitingForParent`, `ActionRefused`, `DependentPostCheck`, the manual-stop and restore events.
2. **Dedupe needs an incident id.** Events carried none, and rebuilding incident state inside `notify` breaks after a restart (no `IncidentOpened` arrives for an incident that is still open). So `engine.emit` adds an `incident` attribute (the opening time, RFC 3339 nano) to every event of a key with an open incident. The identity of a message is (event type, key, discriminator, incident). Events with no incident are deduplicated for 24 h (`NotifyOnly`, `NeedsRecreate`, the version-gate warning), so a `restart: "no"` parent that stays down is not announced again by every poll, but is after a day.
3. **Dedupe is not persisted.** §3.10 lists a "notifications sent" table; I did not build it. Cost: after a restart of whalenurse the engine re-emits `NotifyOnly` once for a parent still down (it also forgets its own advisory state), and so a second message arrives. `Suspended` is not re-emitted for a persisted SUSPENDED, so the case that matters most is fine. Open question 1.
4. **`DriftBlocked` and `Suspended(drift)` are one message.** The engine publishes both; the first carries the findings. A test pins exactly one.
5. **Rate limit shape.** A sliding window of one hour per URL, `WHALENURSE_NOTIFY_RATE_LIMIT` (default 20). Routine messages may use half of it, important ones all of it, so that a storm of "restarted" cannot swallow "giving up". Held-back messages are counted, shown in `status`, and the next message that goes out says how many ("N earlier messages were held back"). I did not build a separate summary message. Not in the task: the 2:1 reserve and the default of 20 are mine.
6. **Retries.** A failed delivery is retried twice (after 2 s and 10 s) before it counts as failed, because a push service that hiccups should not lose "giving up". The retries run in that URL's worker, so they delay only that URL's later messages. Shutdown gives the queue 5 s, then aborts what is in flight.
7. **Message shape.** Title `host: container headline`, body: the key line, then cause and next step (`whalenurse unsuspend project/service`). 72 and 360 runes. The long container name is what is cut, never the headline. A body of 300 runes was too tight for the drift guard's real text (the compose-upgrade note alone is 150), so the template shortens the guard's wording (compose's own dry-run text, the upgrade parenthetical) and says the upgrade in the hint, with the versions.
8. **The compose-upgrade hint** is text in the drift message ("compose upgraded 5.2.0 to 5.6.0 since creation: maybe a new hash layout, not an edit. If the file is fine: docker compose up -d web"), matched on the guard's own wording (`compose upgraded from X to Y`, `can't verify drift (compose`, `the tag moved`, `would recreate`, `config --hash is`). If someone rewords the guard's text the hint silently disappears; the templates test uses the real wording, and the integration tests do not cover it.
9. **The redactor.** Built from the configured URLs: the URL, what follows the scheme (Shoutrrr rewrites `generic://` to `http(s)://` in client errors), the user info, host, path segments of 6 runes or more, the whole path, query values of 4 runes or more (and comma-separated pieces of them), each also URL-escaped. 4 and 6 are a compromise: a path of `hook` is not masked, because it would turn "generic webhook" into "generic web***", at the price that a real secret that short in a path is not masked either (tokens are longer). Verified by mutation: with the redactor disabled the token test, the redaction tests and the ping test fail.
10. **Invalid URLs do not stop whalenurse.** A URL Shoutrrr rejects is logged at error (position and scheme, redacted reason), listed as `INVALID` in `status`, and skipped. `notify test` reports it as FAILED. Healing carrying on matters more than a typo in a notification URL; the cost is that a typo is only seen if someone looks at the log or runs `notify test`.
11. **`notify test` runs in the CLI process** from the environment, not through the control socket: it needs no daemon, so it works before the first start (`docker run --env-file … whalenurse notify test`) and under `docker exec` (the container's env). It sends one attempt per URL, sequentially, in `[dry-run]` marking when `WHALENURSE_DRY_RUN=true` is in that environment (note: `run --dry-run` as a flag is not visible to a separate `notify test` process).
12. **The uptime ping** is a plain GET, not Shoutrrr, with the same timeout and redactor. It pings at once and then every `WHALENURSE_HEARTBEAT_INTERVAL`, only while `Healthy()` is true. It is called the uptime ping in code, because "heartbeat" already means `last_seen`. It also pings in a dry run.
13. **`WHALENURSE_NOTIFY_URLS` splitting.** Whitespace, a newline, or a comma followed by `scheme://`; a comma elsewhere belongs to the URL (ntfy `tags=a,b`, Telegram `chats=a,b`). A URL with a space in it cannot be given (percent-encode it).
14. **New variables** (plan §4): `WHALENURSE_NOTIFY_TIMEOUT` (10s), `WHALENURSE_NOTIFY_RATE_LIMIT` (20), `WHALENURSE_HEARTBEAT_INTERVAL` (5m). The "accepted with a warning, ignored until M3" entries for `NOTIFY_URLS` and `HEARTBEAT_URL` are gone.

## What M3 built

- `internal/notify`: `Notifier` (queue, dispatcher, one worker and sender per URL, dedupe, rate limit, retries, counters, shutdown flush), `render` and the templates, `Redactor`, `NewTargets` (Shoutrrr), `SendTest`/`PrintTest`, `Pinger`.
- `internal/engine`: the `incident` attribute (`AttrIncident`) on events of a key with an open incident.
- `internal/config`: `NotifyURLs` (`SplitURLs`), `NotifyTimeout`, `NotifyRateLimit`, `PingURL`, `PingInterval`.
- `internal/daemon`: `Options.NotifyStatus`, `Status.Notify`.
- `cmd/whalenurse`: `notify test`, `setupNotify`, the uptime ping and the flush on stop, notification state in `status`.
- Docs: plan v3.7 and `CLAUDE.md`, `running.md` (Notifications), `examples/compose.yaml`, README status line.

## Tests

- Unit (no Docker, no process; `go test -race ./...`): templates for every notified event (host and key present, limits under worst-case text, dry-run marker in title and body, drift hints), events outside the list are not sent, a drift suspension is one message; dedupe per incident, per key, 24 h without an incident, never for pause and resume, once per process for the version-gate warning; the rate limit with the critical reserve, per URL, and the "held back" note; retries, counting and redacted logging; `Handle` with a hung URL, a full queue and a stalled dispatcher; one URL never delaying another; the per-attempt timeout; shutdown flush and abort; invalid targets; redaction over six URL shapes and five kinds of error text; URL splitting (with `tags=warning,skull`); the uptime ping; **the whole ladder through the real daemon** (restart, recreate, one giving-up with the cause; a recovery; a dry run marked and doing nothing; a hung URL not delaying healing); **Shoutrrr's generic service against a real HTTP receiver** (JSON title and message, 401, 500, connection refused); and **the token grep** (`cmd/whalenurse`): tokens in the user info, path and query of URLs that fail by 401, 500, connection refused, an unknown service and a non-URL, plus the ping URL; the logs, `status` (text and JSON), `/healthz`, the socket status and the `notify test` output must hold none of them nor any whole URL. Mutation-checked: disabling the redactor fails the token test, the redaction tests and the ping test; making `Handle` block fails its test.
- Integration (CI only, behind the guard, `whalenurse-test-*` only, loopback receiver): the ladder ending in SUSPENDED (restart, recreate, exactly one giving-up with the cause and the unsuspend command, a dead URL beside the working one, no more messages afterwards, no URL in the status), a recovery (restart, recreate, recovered, no SUSPENDED), a dry run (every message marked, container untouched). The existing `TestImage` and the rest are unchanged.

## Not verified

- **The integration tests were not run locally** (the plan forbids it; CI is their only run). They compile and vet (`go vet -tags integration ./...`). The run that counts is the last one on the PR, which the PR description links.
- **A real push service.** Only the generic service was exercised (a loopback webhook). ntfy, Gotify, Discord and Telegram were not sent to: that needs real accounts. `notify test` is the check for the maintainer's own URLs ([maintainer]).
- **TLS from the image.** The loopback tests use plain HTTP. HTTPS needs the CA bundle in `gcr.io/distroless/static`, which ships one, but no test here makes an HTTPS request from the built image.
- **Behaviour across a whalenurse restart** of the notification dedupe (in memory, open question 1).
- **The `incident` attribute** is covered by engine tests, and all existing tests pass unchanged, but no test restarts whalenurse mid-incident and checks that the notifier says nothing twice.

## Open questions for the maintainer

1. **Persist the dedupe?** (§3.10's "notifications sent" table.) Without it a restart can repeat a `NotifyOnly` (a `restart: "no"` parent still down) once. I judged it not worth a schema change in M3; say if you want it, with M5's history page it would give "what was sent when" for free.
2. **Notify `RateLimited`?** Whalenurse's own action budget running out usually means a host-wide outage, which is when you want to hear about it. It is one `Critical` template away.
3. **The 20 an hour per URL default and the 2:1 reserve** are mine. A host with many opted-in containers failing at once (a NAS losing its disks) will hit them; the held-back count is shown in the next message and in `status`.
4. **Should `Unsuspended` be notified?** A container that was SUSPENDED and then recovers by hand gets `Recovered` only after `RECOVERY_PERIOD` (10 min). Today nothing says "it is back" in between.
5. **Binary size** (+8.5 MB, 28 MB). Accepted; the alternative is a fork or vendoring a handful of services.

## [maintainer]

- Review plan v3.7 (it is proposed, not approved).
- Run `docker exec whalenurse /whalenurse notify test` against your real ntfy/Gotify/Discord/Telegram URLs and tell me which, if any, misbehave. Keep the URLs out of anything you paste.
