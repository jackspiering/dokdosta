package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Options configures the TCP handler.
type Options struct {
	Health  Checker
	Service *Service     // nil: only /healthz is served
	Token   string       // empty: no authentication (the listener should then be loopback or behind a proxy)
	Web     http.Handler // the dashboard: serves "/" and "/static/"; nil for the API alone
}

// Handler is the handler of the TCP listener (WHALENURSE_HTTP_ADDR). It is read-only: it answers GET and HEAD and
// refuses every other method, there is no route that changes anything, and the CLI's commands live on the unix socket
// only (plan §2: no network-exposed write endpoints in v1).
func Handler(o Options) http.Handler {
	mux := http.NewServeMux()
	if o.Health != nil {
		mux.Handle("GET /healthz", HealthHandler(o.Health))
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
	return Secure(o.Token, mux)
}

// ParseEventsQuery reads ?since=, ?limit=, ?key= and ?type= (repeatable or comma-separated).
func ParseEventsQuery(r *http.Request) (EventsQuery, error) {
	v := r.URL.Query()
	q := EventsQuery{Key: v.Get("key")}
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
	for _, t := range v["type"] {
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
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

// Secure wraps the handler with the headers on every response, the method filter and the token check. /healthz is
// the one path that needs no token.
func Secure(token string, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
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
		if token != "" && r.URL.Path != "/healthz" && !authorized(r, want) {
			h.Add("WWW-Authenticate", `Bearer realm="whalenurse"`)
			h.Add("WWW-Authenticate", `Basic realm="whalenurse", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "a token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
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

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
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
