package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- Host header (DNS rebinding) ---------------------------------------------------------------------------

// HostAllowed reports whether a request's Host header may reach a listener that has no token (plan v3.9). A web page
// on another origin can make a browser on the same machine send requests to 127.0.0.1 under its own host name (DNS
// rebinding), and the response is then readable by that page. A browser always sends the name it was given, so only
// the loopback names and the names the operator listed are served. The port is ignored. Without a Host header
// (HTTP/1.0) there is nothing to rebind, and the request is served.
func HostAllowed(hostHeader string, allowed []string) bool {
	if hostHeader == "" {
		return true
	}
	h := strings.ToLower(hostHeader)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	h = strings.TrimSuffix(h, ".")
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	for _, a := range allowed {
		if h == a {
			return true
		}
	}
	return false
}

// NormalizeHosts lowercases the entries of WHALENURSE_HTTP_ALLOWED_HOSTS and drops a port, brackets and a trailing dot,
// so that "NAS.example.org:8484", "[fd00::5]" and "nas.example.org." all match the way HostAllowed compares.
func NormalizeHosts(in []string) []string {
	var out []string
	for _, a := range in {
		a = strings.ToLower(strings.TrimSpace(a))
		if host, _, err := net.SplitHostPort(a); err == nil {
			a = host
		}
		a = strings.TrimSuffix(strings.TrimPrefix(a, "["), "]")
		a = strings.TrimSuffix(a, ".")
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

// ---- wrong-token limiter ---------------------------------------------------------------------------------------

// Limit is the number of wrong credentials one client IP may present per Window before it is blocked.
const (
	AttemptLimit  = 10
	AttemptWindow = time.Minute
	maxBlock      = 15 * time.Minute
	maxTracked    = 4096
)

// attemptLimiter blocks a client IP that presented AttemptLimit wrong credentials within AttemptWindow. The block
// starts at one minute and doubles with every further block of the same IP (up to 15 minutes); the count of blocks is
// forgotten after an hour without a failure. While an IP is blocked every request from it is refused with 429, a right
// token included: otherwise the block would only slow the guessing down instead of stopping it.
type attemptLimiter struct {
	mu  sync.Mutex
	now func() time.Time
	ips map[string]*attempts
}

type attempts struct {
	fails   []time.Time
	strikes int
	until   time.Time // blocked until
	last    time.Time
}

func newAttemptLimiter(now func() time.Time) *attemptLimiter {
	return &attemptLimiter{now: now, ips: map[string]*attempts{}}
}

// clientKey is the client's IP (an IPv6 address is reduced to its /64, which a client controls entirely). It is the
// connection's peer address: behind a reverse proxy that is the proxy, so all clients share one budget there.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// blocked reports whether the client is blocked, and for how long.
func (l *attemptLimiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if a, ok := l.ips[key]; ok {
		if now := l.now(); now.Before(a.until) {
			return a.until.Sub(now), true
		}
	}
	return 0, false
}

// fail records a wrong credential. It reports the block it caused, if any.
func (l *attemptLimiter) fail(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a, ok := l.ips[key]
	if !ok {
		if len(l.ips) >= maxTracked {
			l.purge(now)
		}
		a = &attempts{}
		l.ips[key] = a
	}
	if a.strikes > 0 && now.Sub(a.last) > time.Hour {
		a.strikes = 0
	}
	a.last = now
	kept := a.fails[:0]
	for _, t := range a.fails {
		if now.Sub(t) < AttemptWindow {
			kept = append(kept, t)
		}
	}
	a.fails = append(kept, now)
	if len(a.fails) < AttemptLimit {
		return 0, false
	}
	a.strikes++
	d := AttemptWindow << (a.strikes - 1)
	if d > maxBlock || d <= 0 {
		d = maxBlock
	}
	a.until, a.fails = now.Add(d), nil
	return d, true
}

// purge drops the clients with nothing to remember; if the table is still full it drops an arbitrary one.
func (l *attemptLimiter) purge(now time.Time) {
	for k, a := range l.ips {
		if now.After(a.until) && now.Sub(a.last) > time.Hour {
			delete(l.ips, k)
		}
	}
	for k := range l.ips {
		if len(l.ips) < maxTracked {
			return
		}
		delete(l.ips, k)
	}
}

func retryAfter(d time.Duration) string {
	return strconv.Itoa(int((d + time.Second - 1) / time.Second))
}
