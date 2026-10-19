package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func prefixes(t *testing.T, in ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, s := range in {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func keyOf(remote string, trusted []netip.Prefix, xff ...string) string {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = remote
	for _, l := range xff {
		req.Header.Add("X-Forwarded-For", l)
	}
	return clientKey(req, trusted)
}

func TestClientKeyUsesForwardedForOnlyFromATrustedProxy(t *testing.T) {
	proxies := prefixes(t, "10.0.0.0/24", "fd00::/8")
	for _, tc := range []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"a trusted proxy reports the client", "10.0.0.5:1111", []string{"203.0.113.9"}, "203.0.113.9"},
		{"an untrusted peer that sends the header is keyed on itself", "198.51.100.7:1", []string{"203.0.113.9"}, "198.51.100.7"},
		{"no header from a trusted proxy keeps the proxy", "10.0.0.5:1", nil, "10.0.0.5"},
		{"the right-most untrusted entry, not what the client put on the left", "10.0.0.5:1", []string{"1.2.3.4, 203.0.113.9"}, "203.0.113.9"},
		{"a client that forges the header is overridden by the proxy's append", "10.0.0.5:1", []string{"10.0.0.7, 192.0.2.1, 203.0.113.9"}, "203.0.113.9"},
		{"a chain of trusted proxies is walked through", "10.0.0.5:1", []string{"203.0.113.9, 10.0.0.9, 10.0.0.8"}, "203.0.113.9"},
		{"several header lines are one list", "10.0.0.5:1", []string{"1.2.3.4", "203.0.113.9, 10.0.0.9"}, "203.0.113.9"},
		{"only trusted proxies in the header keeps the proxy", "10.0.0.5:1", []string{"10.0.0.9, 10.0.0.8"}, "10.0.0.5"},
		{"an entry that is not an address keeps the proxy", "10.0.0.5:1", []string{"203.0.113.9, unknown"}, "10.0.0.5"},
		{"an empty entry keeps the proxy", "10.0.0.5:1", []string{"203.0.113.9,"}, "10.0.0.5"},
		{"an IPv4-mapped IPv6 entry is the IPv4 address", "10.0.0.5:1", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"an IPv4-mapped IPv6 peer is trusted as IPv4", "[::ffff:10.0.0.5]:1", []string{"203.0.113.9"}, "203.0.113.9"},
		{"a forwarded IPv6 client is reduced to its /64", "10.0.0.5:1", []string{"2001:db8:1:2:aaaa:bbbb:cccc:dddd"}, "2001:db8:1:2::/64"},
		{"an IPv6 trusted proxy", "[fd00::1]:1", []string{"203.0.113.9"}, "203.0.113.9"},
		{"an IPv6 peer is reduced to its /64", "[2001:db8:1:2:aaaa::1]:1", nil, "2001:db8:1:2::/64"},
		{"a peer address that cannot be parsed is returned as is", "pipe", []string{"203.0.113.9"}, "pipe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := keyOf(tc.remote, proxies, tc.xff...); got != tc.want {
				t.Fatalf("clientKey = %q, want %q", got, tc.want)
			}
		})
	}
	// With no trusted proxies the header is never read: this is the behaviour of M5.
	if got := keyOf("10.0.0.5:1", nil, "203.0.113.9"); got != "10.0.0.5" {
		t.Fatalf("no trusted proxies: %q", got)
	}
}

// proxied is a handler behind a reverse proxy at 10.0.0.5 that appends the address it sees to X-Forwarded-For.
func proxied(clk *clockT, trusted []netip.Prefix) http.Handler {
	return Handler(Options{Health: okChecker{}, Service: fixtureService(), Token: limTok, Now: clk.now, TrustedProxies: trusted})
}

func viaProxy(t *testing.T, h http.Handler, client, forged, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.RemoteAddr = "10.0.0.5:4242"
	xff := client
	if forged != "" {
		xff = forged + ", " + client // what a proxy does: it appends the address it received the request from
	}
	req.Header.Set("X-Forwarded-For", xff)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWithTrustedProxiesOneGuessingClientDoesNotLockOutTheOthers(t *testing.T) {
	clk := &clockT{t: t0}
	h := proxied(clk, prefixes(t, "10.0.0.0/24"))
	for i := 0; i < AttemptLimit; i++ {
		// The guesser rotates the forged part of the header on every try; it gains nothing.
		viaProxy(t, h, "203.0.113.9", "198.51.100."+string(rune('1'+i)), "Bearer wrong")
	}
	if rec := viaProxy(t, h, "203.0.113.9", "", "Bearer "+limTok); rec.Code != 429 {
		t.Fatalf("the guesser, with the right token: %d", rec.Code)
	}
	if rec := viaProxy(t, h, "203.0.113.9", "198.51.100.77", "Bearer "+limTok); rec.Code != 429 {
		t.Fatalf("the guesser with yet another forged entry: %d", rec.Code)
	}
	if rec := viaProxy(t, h, "203.0.113.200", "", "Bearer "+limTok); rec.Code != 200 {
		t.Fatalf("another user behind the same proxy is locked out: %d", rec.Code)
	}
}

func TestWithoutTrustedProxiesTheProxyIsOneClientAsBefore(t *testing.T) {
	clk := &clockT{t: t0}
	h := proxied(clk, nil)
	for i := 0; i < AttemptLimit; i++ {
		viaProxy(t, h, "203.0.113.9", "", "Bearer wrong")
	}
	if rec := viaProxy(t, h, "203.0.113.200", "", "Bearer "+limTok); rec.Code != 429 {
		t.Fatalf("without the option all users share the proxy's budget: %d", rec.Code)
	}
}

func TestAnUntrustedPeerCannotBorrowAnotherClientsAddress(t *testing.T) {
	clk := &clockT{t: t0}
	h := proxied(clk, prefixes(t, "10.0.0.0/24"))
	// A real user is blocked...
	for i := 0; i < AttemptLimit; i++ {
		viaProxy(t, h, "203.0.113.9", "", "Bearer wrong")
	}
	// ...and an attacker reaching the listener directly cannot make the victim's address its own, or frame a victim.
	for i := 0; i < AttemptLimit; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		req.RemoteAddr = "198.51.100.66:1"
		req.Header.Set("X-Forwarded-For", "203.0.113.200")
		req.Header.Set("Authorization", "Bearer wrong")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if rec := viaProxy(t, h, "203.0.113.200", "", "Bearer "+limTok); rec.Code != 200 {
		t.Fatalf("an untrusted peer framed a victim: %d", rec.Code)
	}
}
