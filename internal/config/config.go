// Package config parses the DOKDOSTA_* environment and the dokdosta.*
// container labels. Variables that belong to later milestones are accepted
// with a warning so a full config file does not break an earlier deployment.
package config

import (
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mode selects which containers are in scope.
type Mode string

const (
	ModeLabel Mode = "label" // only containers with dokdosta.enable=true
	ModeAll   Mode = "all"   // everything except dokdosta.enable=false
)

// Global is the daemon-wide configuration.
type Global struct {
	HostID         string
	Mode           Mode
	DryRun         bool
	PollInterval   time.Duration
	Grace          time.Duration
	Cooldown       time.Duration // base; doubles per rung, capped at CooldownMax
	CrashloopAfter time.Duration
	RateLimit      int // actions per hour
	StartupSettle  time.Duration
	LogLevel       slog.Level
	HTTPAddr       string // empty: no TCP listener
	HTTPToken      string // DOKDOSTA_HTTP_TOKEN: when set, the dashboard and the API need it. A secret: never logged or shown.
	Metrics        bool   // DOKDOSTA_METRICS: serve /metrics on the HTTP listener
	// HTTPAllowedHosts are the Host names served besides localhost, 127.0.0.1 and [::1] when there is no token
	// (DOKDOSTA_HTTP_ALLOWED_HOSTS: a DNS rebinding defence). Ignored when a token is set.
	HTTPAllowedHosts []string
	// HTTPTrustedProxies are the reverse proxies (DOKDOSTA_HTTP_TRUSTED_PROXIES, CIDRs or single addresses) whose
	// X-Forwarded-For the wrong-token limiter believes. Empty: the peer address is the client. Only matters with a token.
	HTTPTrustedProxies []netip.Prefix
	ControlSocket      string // unix socket for /healthz and the CLI (pause, resume, unsuspend, status)

	RecoveryPeriod   time.Duration // an incident closes after this long recovered; a crash loop clears after it
	FlapLimit        int           // more than this many incidents within FlapWindow suspends the container
	FlapWindow       time.Duration
	DataDir          string        // the SQLite store lives here
	HistoryRetention time.Duration // closed incidents older than this are pruned

	DriftGuard DriftGuard // strict (default): a recreate that would deploy a change is skipped and SUSPENDS; warn: recreate anyway and say so
	ComposeBin string     // DOKDOSTA_COMPOSE_BIN: the compose binary to use, ahead of the host plugin and the bundled copy

	// Notifications (M3). The URLs hold tokens: never log them, never show them in status.
	NotifyURLs      []string      // DOKDOSTA_NOTIFY_URLS, Shoutrrr URLs
	NotifyTimeout   time.Duration // one delivery attempt, per URL
	NotifyRateLimit int           // messages per hour per URL; the second half of that budget is kept for the important ones
	PingURL         string        // DOKDOSTA_HEARTBEAT_URL: pinged while dokdosta is healthy (healthchecks.io, Uptime Kuma push)
	PingInterval    time.Duration // DOKDOSTA_HEARTBEAT_INTERVAL

	// RegistryAuthDir is DOKDOSTA_REGISTRY_AUTH_DIR (M7): a directory with a config.json of inline "auths", given to
	// `compose pull` as DOCKER_CONFIG and to no other command. The path is not a secret; the file is.
	RegistryAuthDir string
}

// DriftGuard is DOKDOSTA_DRIFT_GUARD.
type DriftGuard string

const (
	DriftStrict DriftGuard = "strict"
	DriftWarn   DriftGuard = "warn"
)

// CooldownMax caps the cooldown backoff.
const CooldownMax = 5 * time.Minute

// Defaults returns the plan's §4 defaults for the M1 subset.
func Defaults() Global {
	host, _ := os.Hostname()
	return Global{
		HostID:         host,
		Mode:           ModeLabel,
		PollInterval:   60 * time.Second,
		Grace:          30 * time.Second,
		Cooldown:       30 * time.Second,
		CrashloopAfter: 5 * time.Minute,
		RateLimit:      10,
		StartupSettle:  2 * time.Minute,
		LogLevel:       slog.LevelInfo,
		ControlSocket:  "/run/dokdosta.sock",

		RecoveryPeriod:   10 * time.Minute,
		FlapLimit:        3,
		FlapWindow:       24 * time.Hour,
		DataDir:          "/data",
		HistoryRetention: 30 * 24 * time.Hour,
		DriftGuard:       DriftStrict,

		NotifyTimeout:   10 * time.Second,
		NotifyRateLimit: 20,
		PingInterval:    5 * time.Minute,
	}
}

// LegacyEnvPrefix is the prefix of the variables from before the rename (plan v3.15 item 4). A variable with it is a
// startup error, so a lost token, notification URL or mode does not go unnoticed; the old names are not aliases.
const LegacyEnvPrefix = "WHALENURSE_"

// later lists variables defined in the plan that nothing acts on yet.
var later = map[string]string{}

// FromEnv parses environ ("KEY=value" strings, as os.Environ returns). It
// returns warnings for ignored or unknown DOKDOSTA_* variables, and an error
// for a value that is present but invalid.
func FromEnv(environ []string) (Global, []string, error) {
	g := Defaults()
	env := map[string]string{}
	var legacy []string
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		switch {
		case ok && strings.HasPrefix(k, "DOKDOSTA_"):
			env[k] = v
		case ok && strings.HasPrefix(k, LegacyEnvPrefix):
			legacy = append(legacy, k)
		}
	}
	var warns []string
	var errs []string
	if len(legacy) > 0 {
		// Only the names: the values may be secrets (the token, the notification URLs, the uptime ping).
		sort.Strings(legacy)
		errs = append(errs, fmt.Sprintf("%s set: whalenurse was renamed dokdosta, rename them to DOKDOSTA_* (docs/upgrading.md)",
			strings.Join(legacy, ", ")))
	}
	take := func(k string) (string, bool) {
		v, ok := env[k]
		delete(env, k)
		v = strings.TrimSpace(v)
		return v, ok && v != ""
	}
	dur := func(k string, dst *time.Duration, allowZero bool) {
		if v, ok := take(k); ok {
			d, err := ParseDuration(v)
			switch {
			case err != nil:
				errs = append(errs, fmt.Sprintf("%s=%q: %v", k, v, err))
			case d < 0 || (d == 0 && !allowZero):
				errs = append(errs, fmt.Sprintf("%s=%q: must be positive", k, v))
			default:
				*dst = d
			}
		}
	}

	if v, ok := take("DOKDOSTA_HOST_ID"); ok {
		if strings.Contains(v, "/") {
			// The state key is host/project/service/n; a slash in the host would shift every part of it, and pause
			// and unsuspend scopes would match nothing.
			errs = append(errs, fmt.Sprintf("DOKDOSTA_HOST_ID=%q: must not contain a '/'", v))
		} else {
			g.HostID = v
		}
	}
	if g.HostID == "" {
		g.HostID = "unknown"
	}
	if v, ok := take("DOKDOSTA_MODE"); ok {
		switch Mode(strings.ToLower(v)) {
		case ModeLabel, ModeAll:
			g.Mode = Mode(strings.ToLower(v))
		default:
			errs = append(errs, fmt.Sprintf("DOKDOSTA_MODE=%q: want label or all", v))
		}
	}
	if v, ok := take("DOKDOSTA_DRY_RUN"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_DRY_RUN=%q: %v", v, err))
		}
		g.DryRun = b
	}
	dur("DOKDOSTA_POLL_INTERVAL", &g.PollInterval, false)
	dur("DOKDOSTA_GRACE", &g.Grace, true)
	dur("DOKDOSTA_COOLDOWN", &g.Cooldown, true)
	dur("DOKDOSTA_CRASHLOOP_AFTER", &g.CrashloopAfter, false)
	dur("DOKDOSTA_STARTUP_SETTLE", &g.StartupSettle, true)
	dur("DOKDOSTA_RECOVERY_PERIOD", &g.RecoveryPeriod, false)
	dur("DOKDOSTA_FLAP_WINDOW", &g.FlapWindow, false)
	dur("DOKDOSTA_HISTORY_RETENTION", &g.HistoryRetention, false)
	if v, ok := take("DOKDOSTA_FLAP_LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_FLAP_LIMIT=%q: want a positive integer", v))
		} else {
			g.FlapLimit = n
		}
	}
	if v, ok := take("DOKDOSTA_DATA_DIR"); ok {
		g.DataDir = v
	}
	if v, ok := take("DOKDOSTA_RATE_LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_RATE_LIMIT=%q: want a positive integer", v))
		} else {
			g.RateLimit = n
		}
	}
	if v, ok := take("DOKDOSTA_LOG_LEVEL"); ok {
		if err := g.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_LOG_LEVEL=%q: %v", v, err))
		}
	}
	if v, ok := take("DOKDOSTA_HTTP_ADDR"); ok {
		g.HTTPAddr = v
	}
	// The token is a secret: an error about it never quotes the value.
	if v, ok := take("DOKDOSTA_HTTP_TOKEN"); ok {
		// The examples' placeholders ("replace-with-...") pass the length check and the compose ":?" check, and would
		// leave the dashboard behind a token that is published in the repository.
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "replace-with") {
			errs = append(errs, "DOKDOSTA_HTTP_TOKEN is still the placeholder from the example env file; generate one with openssl rand -hex 24")
		} else {
			g.HTTPToken = v
		}
	}
	if v, ok := take("DOKDOSTA_HTTP_ALLOWED_HOSTS"); ok {
		g.HTTPAllowedHosts = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	}
	if v, ok := take("DOKDOSTA_HTTP_TRUSTED_PROXIES"); ok {
		ps, err := ParseProxies(v)
		if err != nil {
			errs = append(errs, "DOKDOSTA_HTTP_TRUSTED_PROXIES: "+err.Error())
		}
		g.HTTPTrustedProxies = ps
	}
	if v, ok := take("DOKDOSTA_METRICS"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_METRICS=%q: %v", v, err))
		}
		g.Metrics = b
	}
	if v, ok := take("DOKDOSTA_CONTROL_SOCKET"); ok {
		g.ControlSocket = v
	}
	if v, ok := take("DOKDOSTA_DRIFT_GUARD"); ok {
		switch DriftGuard(strings.ToLower(v)) {
		case DriftStrict, DriftWarn:
			g.DriftGuard = DriftGuard(strings.ToLower(v))
		default:
			errs = append(errs, fmt.Sprintf("DOKDOSTA_DRIFT_GUARD=%q: want strict or warn", v))
		}
	}
	if v, ok := take("DOKDOSTA_COMPOSE_BIN"); ok {
		// The probe Stats the path in the working directory but compose runs from "/", so a relative path is refused here.
		if !filepath.IsAbs(v) {
			errs = append(errs, "DOKDOSTA_COMPOSE_BIN must be an absolute path")
		} else {
			g.ComposeBin = v
		}
	}
	// The notification variables hold tokens, so an error about them never quotes the value.
	if v, ok := take("DOKDOSTA_NOTIFY_URLS"); ok {
		g.NotifyURLs = SplitURLs(v)
	}
	if v, ok := take("DOKDOSTA_HEARTBEAT_URL"); ok {
		g.PingURL = v
	}
	if v, ok := take("DOKDOSTA_REGISTRY_AUTH_DIR"); ok {
		g.RegistryAuthDir = v
	}
	dur("DOKDOSTA_NOTIFY_TIMEOUT", &g.NotifyTimeout, false)
	dur("DOKDOSTA_HEARTBEAT_INTERVAL", &g.PingInterval, false)
	if v, ok := take("DOKDOSTA_NOTIFY_RATE_LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Sprintf("DOKDOSTA_NOTIFY_RATE_LIMIT=%q: want a positive integer", v))
		} else {
			g.NotifyRateLimit = n
		}
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if ms, ok := later[k]; ok {
			warns = append(warns, fmt.Sprintf("%s is set but not used until %s", k, ms))
		} else {
			warns = append(warns, fmt.Sprintf("%s is not a known variable", k))
		}
	}
	if len(errs) > 0 {
		return g, warns, fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
	}
	return g, warns, nil
}

// CooldownFor returns the cooldown after rung n (0-based): base, doubling per
// rung, capped at CooldownMax. A positive override replaces the base.
func CooldownFor(base, override time.Duration, rung int) time.Duration {
	if override > 0 {
		base = override
	}
	d := base
	for i := 0; i < rung && d < CooldownMax; i++ {
		d *= 2
	}
	return min(d, CooldownMax)
}

// ParseDuration is time.ParseDuration plus a day suffix ("30d", "2d12h"), which the
// plan's defaults use (DOKDOSTA_HISTORY_RETENTION=30d).
func ParseDuration(v string) (time.Duration, error) {
	if d, rest, ok := strings.Cut(v, "d"); ok && d != "" && strings.Trim(d, "0123456789") == "" {
		days, err := strconv.Atoi(d)
		if err != nil {
			return 0, err
		}
		var tail time.Duration
		if rest != "" {
			if tail, err = time.ParseDuration(rest); err != nil {
				return 0, err
			}
		}
		// 213504d wraps a Duration to about 25 minutes; refuse what does not fit instead.
		if days > int(math.MaxInt64/int64(24*time.Hour)) {
			return 0, fmt.Errorf("%q is too long", v)
		}
		total := time.Duration(days) * 24 * time.Hour
		if tail > 0 && total > math.MaxInt64-tail {
			return 0, fmt.Errorf("%q is too long", v)
		}
		return total + tail, nil
	}
	return time.ParseDuration(v)
}

// ParseProxies parses a comma or space separated list of CIDRs or single addresses (a single address is a /32 or /128). It
// refuses a prefix of length 0: every client would then be a trusted proxy and could write its own address.
func ParseProxies(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		p, err := netip.ParsePrefix(f)
		if err != nil {
			a, aerr := netip.ParseAddr(f)
			if aerr != nil {
				return nil, fmt.Errorf("%q is not a CIDR such as 172.18.0.0/16 or an address", f)
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		if p.Addr().Is4In6() {
			// ::ffff:10.0.0.0/104 is the IPv4 range 10.0.0.0/8, and requests are matched after unmapping.
			a := p.Addr().Unmap()
			if p.Bits() < 96 {
				return nil, fmt.Errorf("%q: an IPv4-mapped prefix needs at least 96 bits", f)
			}
			p = netip.PrefixFrom(a, p.Bits()-96)
		}
		if p.Bits() == 0 {
			return nil, fmt.Errorf("%q trusts every address, so any client could forge its own; list the proxy's network", f)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
