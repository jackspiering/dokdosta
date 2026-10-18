package notify

import (
	"net/url"
	"sort"
	"strings"
)

// mask replaces every secret in a redacted string.
const mask = "***"

// minSecret is the shortest fragment the redactor replaces. Shorter ones ("a", "yes") would mangle ordinary text and are no
// token; a token that short would not protect anything either.
const minSecret = 4

// minPathSecret is the shortest path segment the redactor replaces. A path segment of Shoutrrr's own error text is
// ordinary words ("hook" in "generic webhook"), and real tokens in a path (Discord, Slack, Gotify, a private ntfy topic)
// are longer.
const minPathSecret = 6

// Redactor removes the pieces of the configured URLs from text. Notification URLs hold tokens (a Discord webhook id and
// token are the host and path, a Telegram bot token is the user name, ntfy and Gotify tokens are a path or a query
// value), and the errors of an HTTP client quote the URL it called. Shoutrrr redacts its own errors too (v0.22.0), but
// that is one layer, and whalenurse logs, counts and prints these errors in several places, so every one of them goes
// through here.
type Redactor struct {
	fragments []string // longest first, so a URL goes before the pieces inside it
}

// NewRedactor builds a Redactor for the given raw URLs (notification URLs, the uptime ping URL).
func NewRedactor(raw ...string) *Redactor {
	seen := map[string]bool{}
	add := func(s string) {
		if len(s) < minSecret || seen[s] {
			return
		}
		seen[s] = true
	}
	for _, r := range raw {
		for _, f := range fragmentsOf(r) {
			add(f)
		}
	}
	r := &Redactor{}
	for f := range seen {
		r.fragments = append(r.fragments, f)
	}
	sort.Slice(r.fragments, func(i, j int) bool {
		if len(r.fragments[i]) != len(r.fragments[j]) {
			return len(r.fragments[i]) > len(r.fragments[j])
		}
		return r.fragments[i] < r.fragments[j]
	})
	return r
}

// fragmentsOf lists everything in a URL that must not reach a log: the URL itself, its parts, and their escaped forms.
// The scheme is kept, it is what names the service.
func fragmentsOf(raw string) []string {
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		out = append(out, s, url.QueryEscape(s), url.PathEscape(s))
	}
	add(raw)
	// Everything after the scheme: Shoutrrr rewrites generic://host/path to http(s)://host/path in the errors of its
	// HTTP client, so the remainder is what shows up, behind another scheme.
	if _, rest, ok := strings.Cut(raw, "://"); ok {
		add(rest)
		for _, scheme := range []string{"http://", "https://"} {
			add(scheme + rest)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		// A URL that does not even parse is still redacted whole, and by its pieces.
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return strings.ContainsRune("/?&=@:#", r) }) {
			add(part)
		}
		return out
	}
	if u.User != nil {
		add(u.User.Username())
		if pw, ok := u.User.Password(); ok {
			add(pw)
		}
	}
	add(u.Host)
	add(u.Hostname())
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		if len(seg) < minPathSecret {
			continue
		}
		if s, err := url.PathUnescape(seg); err == nil {
			add(s)
		}
		add(seg)
	}
	if p := strings.Trim(u.Path, "/"); len(p) >= minPathSecret {
		add(p)
	}
	for _, vals := range u.Query() {
		for _, v := range vals {
			add(v)
			// A value with commas (tags=a,b, chats=1,2) is leaked piece by piece as well.
			for _, piece := range strings.Split(v, ",") {
				add(piece)
			}
		}
	}
	add(u.RawQuery)
	return out
}

// Redact returns s with every fragment of the configured URLs replaced by ***.
func (r *Redactor) Redact(s string) string {
	if r == nil {
		return s
	}
	for _, f := range r.fragments {
		if strings.Contains(s, f) {
			s = strings.ReplaceAll(s, f, mask)
		}
	}
	return s
}

// Err returns the redacted text of err, or "" for nil.
func (r *Redactor) Err(err error) string {
	if err == nil {
		return ""
	}
	return r.Redact(err.Error())
}

// schemeOf names a URL's service for logs and status, and nothing else about it: "ntfy", "generic", "generic+https". A
// string without a scheme is "invalid".
func schemeOf(raw string) string {
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok || scheme == "" || strings.ContainsAny(scheme, "/?@ ") {
		return "invalid"
	}
	return strings.ToLower(scheme)
}
