package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Options configures the TCP handler.
type Options struct {
	Health  Checker
	Service *Service     // nil: only /healthz is served
	Token   string       // empty: no authentication, and only loopback Host names (and AllowedHosts) are served
	Web     http.Handler // the dashboard: serves "/" and "/static/"; nil for the API alone
	// AllowedHosts, Now and OnBlock: see SecureOptions.
	AllowedHosts []string
	// TrustedProxies: see SecureOptions.
	TrustedProxies []netip.Prefix
	Now            func() time.Time
	OnBlock        func(client string, d time.Duration)
}

// Handler is the handler of the TCP listener (DOKDOSTA_HTTP_ADDR). It is read-only: it answers GET and HEAD and
// refuses every other method, there is no route that changes anything, and the CLI's commands live on the unix socket
// only (plan §2: no network-exposed write endpoints in v1).
func Handler(o Options) http.Handler {
	mux := http.NewServeMux()
	if o.Health != nil {
		mux.Handle("GET /healthz", PublicHealthHandler(o.Health))
	}
	if s := o.Service; s != nil {
		mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Status()) })
		mux.HandleFunc("GET /api/v1/containers", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, ContainersDoc{Host: s.Host, Containers: s.Containers(r.URL.Query().Get("all") == "1")})
		})
		mux.HandleFunc("GET /api/v1/containers/{key...}", func(w http.ResponseWriter, r *http.Request) {
			d, ok := s.Container(r.PathValue("key"))
			if !ok {
				writeError(w, http.StatusNotFound, "no such container")
				return
			}
			writeJSON(w, d)
		})
		mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
			q, err := ParseEventsQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, s.EventsPage(q))
		})
		mux.HandleFunc("GET /api/v1/incidents", func(w http.ResponseWriter, r *http.Request) {
			n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			writeJSON(w, s.Incidents(r.URL.Query().Get("key"), n))
		})
		mux.HandleFunc("GET /api/v1/stream", s.stream)
		mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "no such endpoint") })
		if s.Metrics {
			mux.HandleFunc("GET /metrics", s.metrics)
		}
	}
	if o.Web != nil {
		mux.Handle("/", o.Web)
	}
	return Secure(SecureOptions{Token: o.Token, AllowedHosts: o.AllowedHosts, TrustedProxies: o.TrustedProxies, Now: o.Now, OnBlock: o.OnBlock}, mux)
}

// Bounds of the events filter. There are fewer than 32 event types, and a key is host/project/service/number; each type
// costs work in the store's query and in the dry run's ring scan, which holds the ring's lock (and so the engine's).
const (
	maxTypes   = 64
	maxTypeLen = 64
	maxKeyLen  = 512
)

// ParseEventsQuery reads ?since=, ?limit=, ?key= and ?type= (repeatable or comma-separated). The errors never quote the
// input.
func ParseEventsQuery(r *http.Request) (EventsQuery, error) {
	v := r.URL.Query()
	q := EventsQuery{Key: v.Get("key")}
	if len(q.Key) > maxKeyLen {
		return q, fmt.Errorf("key: longer than %d bytes", maxKeyLen)
	}
	if s := v.Get("since"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return q, fmt.Errorf("since: want the numeric cursor of an earlier reply")
		}
		q.Since = n
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return q, fmt.Errorf("limit: want a positive integer")
		}
		q.Limit = n
	}
	n, seen := 0, map[string]bool{}
	for _, t := range v["type"] {
		for p := range strings.SplitSeq(t, ",") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			if n++; n > maxTypes {
				return q, fmt.Errorf("type: more than %d types", maxTypes)
			}
			if len(p) > maxTypeLen {
				return q, fmt.Errorf("type: an entry longer than %d bytes", maxTypeLen)
			}
			if !seen[p] {
				seen[p] = true
				q.Types = append(q.Types, p)
			}
		}
	}
	q.Newest = v.Get("order") == "newest"
	return q, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- security: headers, methods, token ------------------------------------------------------------------

// CSP forbids everything the pages do not need. There is no inline script or style, no form, no frame, no third-party
// origin; the pages load one stylesheet and one script from this origin and open an EventSource to it.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// SecureOptions configures Secure.
type SecureOptions struct {
	Token string
	// AllowedHosts are extra Host names served when there is no token (loopback names always are); see HostAllowed.
	AllowedHosts []string
	// TrustedProxies are the reverse proxies whose X-Forwarded-For the wrong-token limiter may believe, so that the limit
	// is per real client and not per proxy. A request from any other peer is keyed on its own address, whatever it sends.
	TrustedProxies []netip.Prefix
	// Now is the clock of the wrong-token limiter; nil is time.Now.
	Now func() time.Time
	// OnBlock is called when a client IP has been blocked for presenting too many wrong tokens (it logs, once per block).
	OnBlock func(client string, d time.Duration)
	// authorize replaces the token comparison; a test seam.
	authorize func(r *http.Request, want [sha256.Size]byte) bool
}

// Secure wraps the handler with the headers on every response, the method filter, the Host check (without a token), the
// wrong-token limiter and the token check. /healthz is the one path that is exempt from the last three.
func Secure(o SecureOptions, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(o.Token))
	now := o.Now
	if now == nil {
		now = time.Now
	}
	lim := newAttemptLimiter(now)
	authorize := o.authorize
	if authorize == nil {
		authorize = authorized
	}
	allowed := NormalizeHosts(o.AllowedHosts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "read-only: only GET and HEAD are served")
			return
		}
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if o.Token == "" {
			// No token: the only defence against a web page reading this through DNS rebinding is the Host header.
			if !HostAllowed(r.Host, allowed) {
				writeError(w, http.StatusForbidden, "this Host name is not allowed; set DOKDOSTA_HTTP_ALLOWED_HOSTS or DOKDOSTA_HTTP_TOKEN")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		client := clientKey(r, o.TrustedProxies)
		// A request that sent no credentials at all (a browser before its prompt) is not a wrong guess, so it is not counted;
		// while the client is blocked it is refused like any other.
		counted := r.Header.Get("Authorization") != ""
		var res reservation
		if counted {
			d, blocked, rs := lim.reserve(client)
			if blocked {
				tooMany(w, d)
				return
			}
			res = rs
		} else if d, blocked := lim.blocked(client); blocked {
			tooMany(w, d)
			return
		}
		if !authorize(r, want) {
			if res.blockFor > 0 {
				if o.OnBlock != nil {
					o.OnBlock(client, res.blockFor)
				}
				tooMany(w, res.blockFor)
				return
			}
			h.Add("WWW-Authenticate", `Bearer realm="dokdosta"`)
			h.Add("WWW-Authenticate", `Basic realm="dokdosta", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "a token is required")
			return
		}
		if counted {
			lim.refund(client, res)
		}
		next.ServeHTTP(w, r)
	})
}

func tooMany(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", retryAfter(d))
	writeError(w, http.StatusTooManyRequests, "too many wrong tokens from this address; try again later")
}

// authorized accepts the token as a bearer token, or as the password of HTTP Basic (any user name): a browser cannot
// send a bearer header on a page load or an EventSource, and with no POST there is no login form. The token is never
// read from the URL, where it would end up in logs and in Referer. Both comparisons are constant time.
func authorized(r *http.Request, want [sha256.Size]byte) bool {
	h := r.Header.Get("Authorization")
	scheme, cred, ok := strings.Cut(h, " ")
	if !ok {
		return false
	}
	cred = strings.TrimSpace(cred)
	switch {
	case strings.EqualFold(scheme, "Bearer"):
	case strings.EqualFold(scheme, "Basic"):
		raw, err := base64.StdEncoding.DecodeString(cred)
		if err != nil {
			return false
		}
		_, pass, found := strings.Cut(string(raw), ":")
		if !found {
			return false
		}
		cred = pass
	default:
		return false
	}
	got := sha256.Sum256([]byte(cred))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// ---- SSE ------------------------------------------------------------------------------------------------

const (
	streamKeepalive = 15 * time.Second
	streamPage      = 200
)

// stream serves GET /api/v1/stream: a server-sent events stream of the domain events. The ID of each message is the
// event's ID, so a client that reconnects with Last-Event-ID (the browser does it by itself) or ?since= continues
// where it was, from the event log. Without either, the stream starts now. ?key= narrows it to one container.
func (s *Service) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok || s.Hub == nil || s.Events == nil {
		writeError(w, http.StatusNotImplemented, "streaming is not available")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
	if r.Method == http.MethodHead {
		// The headers only. net/http discards a HEAD's body, so no write would ever fail and the loop below would hold a
		// slot until shutdown, even after the client has gone.
		w.WriteHeader(http.StatusOK)
		return
	}
	wake, cancel, ok := s.Hub.Subscribe()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "too many streams")
		return
	}
	defer cancel()

	cursor := s.Events.LastID()
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cursor = n
		}
	} else if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cursor = n
		}
	}
	key := r.URL.Query().Get("key")

	fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	fl.Flush()

	tick := time.NewTicker(streamKeepalive)
	defer tick.Stop()
	for {
		if last := s.Events.LastID(); cursor > last {
			fmt.Fprintf(w, "event: reset\ndata: {\"last_id\":%d}\n\n", last)
			cursor = 0
		}
		for {
			evs, err := s.Events.Query(engineQuery(cursor, key, streamPage))
			if err != nil || len(evs) == 0 {
				break
			}
			for _, e := range evs {
				b, _ := json.Marshal(s.EventDoc(e))
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, b)
				cursor = e.ID
			}
			if len(evs) < streamPage {
				break
			}
		}
		fl.Flush()
		select {
		case <-wake:
		case <-tick.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		case <-s.Hub.Done():
			return
		}
	}
}
