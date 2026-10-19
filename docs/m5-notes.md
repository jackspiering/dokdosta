# M5 notes

Decisions, deviations and open questions for milestone M5 (the read-only dashboard and API) of [PLAN.md](PLAN.md). The rules that changed are in the plan as **v3.9** (proposed, awaiting the maintainer's review). Where the plan was ambiguous this file says what was chosen and why, so the maintainer can overrule it.

## Upstream check (19 Oct 2026)

No new dependency. The Prometheus text format is written by hand (about 100 lines; the official client would be a module plus its transitive tree for twelve families), `net/http` (Go 1.22+ patterns, `{key...}` wildcards, `http.FileServerFS`), `html/template`, `embed`, `crypto/subtle` and `go/build` (a test) are the standard library. `omitzero` (Go 1.24+) keeps zero times out of the JSON; `encoding/json`'s `omitempty` would have printed `0001-01-01T00:00:00Z`. Go, moby client, sqlite and shoutrrr are unchanged since M4.

## Decisions the plan leaves open

1. **Events had no store.** The plan said the events table's ID "doubles as the `since` cursor" (§3.10), but M2 to M4 never built it: the bus counter lived in memory and restarted at 1. M5 adds the table (schema 3), seeds the bus from it, and makes retention apply to it (v3.9 item 1). `AUTOINCREMENT` (not plain `INTEGER PRIMARY KEY`, which can reuse the ID of a deleted newest row) keeps `sqlite_sequence` as the high-water mark, so retention cannot make an ID come back. A cap of 100,000 rows bounds a flapping container.
2. **A cursor ahead of the host is a reset**, not an empty page: a hub holding cursor 500 for a host whose store was wiped would otherwise wait forever. The reply carries `reset: true` and starts over; the SSE stream sends a `reset` event. A dry run restarts at 1 every time, so it resets by design.
3. **Token: bearer and Basic.** The brief said bearer. A browser cannot set `Authorization` on a navigation or an `EventSource`, and a login form would need a POST (refused by the read-only rule) and a cookie. HTTP Basic with the token as the password is native to browsers, needs no write endpoint and no state. The token is still never taken from the URL. **Maintainer: say if you want bearer only** (then the dashboard behind a token needs a proxy that injects the header).
4. **`/healthz` stays open** on the TCP listener, as since M1, even with a token. It reveals only the health text. A reverse proxy or an uptime monitor can use it.
5. **`/metrics` behind `WHALENURSE_METRICS`** (default off), not always on: "optional" read as opt-in, consistent with the dashboard being off by default. Cardinality: two per-container families (`suspended`, `flaps`) for the watched containers only.
6. **Containers page lists the watched containers** (opted in, or a sidecar of an opted-in parent: what the last classification did not skip), with `?all=1` for the rest. The API does the same (`?all=1`). Counts on the overview say how many are not watched. Unwatched containers are shown with their labels filtered like the others.
7. **The drift status is the guard's last verdict** (from the event log), not a live check (v3.9 item 8). If the container was never recreated, the page says "nothing recorded". A live "would a recreate be blocked now" would need a compose dry-run per page view.
8. **Daemon read side.** The loop's index is single-goroutine, so the API reads a separate `view` (the last container list, and per key the last inspect and the rules' decision), written by the loop and read under an `RWMutex`. The page is therefore as fresh as the last poll (60 s) or event-driven look; the state column comes from the list (fresher), the restart count, exit code and start error from the last inspect of the same container ID.
9. **Server-side rendering** instead of the page fetching `/api/v1` from JavaScript: it works without JavaScript, has no inline data, and the CSP stays strict. "Uses only this API" is kept by structure (`internal/web` imports `internal/api` only; a test checks), not by HTTP loopback.
10. **Cut `State.Error`, show it.** It is the reason a container is down and is already in the event messages. It can name a host path (a bind source). Cut at 600 runes and redacted.
11. **Event attribute allow-list** (v3.9 item 6b) is a list of the 34 keys the engine and the daemon emit today, written by reading every `map[string]string{...}` and `attrs[...]` in `internal/engine` and `internal/daemon`. A new attribute has to be added to `allowedAttrs` to reach the API. The values of allowed keys are redacted but not otherwise inspected (the secrets test therefore plants its attribute secret under a key that is not allowed).
12. **Event streams do not hold events.** The hub only wakes clients; each client reads the log from its cursor, so the engine's lock is never held for I/O and a stalled browser tab costs nothing. At most 32 streams. In a dry run the log is a 5,000-event ring: a client that was slower than 5,000 events misses some (the stored mode has no such loss).
13. **Basic-auth challenge.** A 401 sends two `WWW-Authenticate` lines (Bearer, Basic) so that both a script and a browser know what to do.
14. **The API reads through its own SQLite connection.** The store's writer is a single connection and `RecordEvent` and `SaveKey` run under the engine's lock, so a page view on that connection could make the healing loop wait. `Query`, `LastID` and `History` use a second read-only connection (`mode=ro`, `query_only`; WAL lets it run beside the writer, and a test holds a write transaction open to prove a read does not wait). If it cannot be opened the reads share the writer's connection and a warning says so.
15. **HTTP server settings**: header timeout 5 s, read timeout 15 s, idle 2 min, no write timeout (the stream). Shutdown is graceful for 3 s (`RegisterOnShutdown(hub.Close)` ends the streams), then closed.

## What was verified, and how

- `go vet`, `go test -race ./...` (all packages), `go vet -tags integration ./...`; the integration tests were **not** run locally (CI runs them on the image, per the working protocol). **CI run 39251862224, all six integration legs (2 image stores × compose 5.2.0, 5.6.0, latest) green on dc56591**, `TestImageServesTheDashboardBehindTheToken` and `TestImageServesTheDashboardWithoutATokenOnLoopback` passing (not skipped).
- Golden JSON for ten API calls (`go test ./internal/api -update` rewrites them), checked by test for `host` on every record and `Z` on every time.
- The secrets test runs a real incident through a daemon on a fake Docker, a real store, the API and the pages, plants ten secrets (environment variable of the process, two labels, the notification URL's user info, path and query, the ping URL, the HTTP token, an event attribute under a non-allowed key, a leaky error text from a failed restart), and greps 30 responses and the SSE replay. Its positive controls require the container names, the `***` mask, the allowed `whalenurse.*` label and `NeedsRecreate` to be present. **Mutation check:** without the redactor it reports 36 leaks, without the attribute allow-list 12.
- Every non-GET method (POST, PUT, PATCH, DELETE, OPTIONS, TRACE, CONNECT, PROPFIND) is 405 on every route including the control socket's paths, with and without the token, and none answers with CORS headers.
- Pages: the inline `<script>`, `on*=`, `style=` and `javascript:` greps, and "no third-party origin" on every page; the import rule of `internal/web`.
- `node --check` on `app.js`.
- A stream survives a server `ReadTimeout` shorter than the stream (a regression test with a 300 ms timeout and an event after 1.2 s): `net/http` does not cancel a body-less GET's request at that deadline, so no extra code was needed.
- The attribute allow-list is complete for everything the daemon tests emit: the daemon test harness fails any event whose attribute key `api.AttrAllowed` would drop (the dashboard test's planted keys excepted), across all the M1 to M5 scenarios.

## Not verified

- **The pages were not looked at in a browser.** The preview tool was off for this thread, and the headless Chromium on the box lacks `libasound`/`libgbm`. The templates, the CSS (light/dark by `prefers-color-scheme`; a 40rem breakpoint that turns the tables into stacked cards) and `app.js` were reviewed by reading and tested by fetching the HTML, not by rendering. **[maintainer]** please open the dashboard once on a desktop and a phone, in both colour schemes, and tell me what looks wrong.
- `app.js` live refresh (EventSource plus the 15 s refetch) was not exercised in a browser; the server side of the stream is covered by Go tests.

## Open questions for the maintainer

1. Basic auth in addition to bearer (decision 3): keep?
2. **DNS rebinding.** A tokenless listener on `127.0.0.1` can be read by a web page in a browser on the same machine through DNS rebinding (a Host header check would stop it, but breaks reverse proxies with other host names unless an allow-list variable is added). The docs say to set a token. Add `WHALENURSE_HTTP_ALLOWED_HOSTS`? Not done.
3. **No attempt limit** on the token. A long random token makes it moot; a per-IP throttle is a candidate for M6.
4. `/healthz` open with a token (decision 4): keep?
5. `/metrics` default off (decision 5): keep?
6. The ladder on the detail page draws `restart ✓ → recreate ✓` for a rung that was skipped as unavailable (the engine counts it as run). The incident events say which. A separate "skipped" marker would need the engine to record it; not done.

## Candidates for M6

A per-IP throttle on a wrong token; `WHALENURSE_HTTP_ALLOWED_HOSTS`; a "skipped" mark on the ladder; a README screenshot once the pages have been seen.
