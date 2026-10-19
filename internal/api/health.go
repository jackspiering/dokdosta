package api

import (
	"fmt"
	"net/http"
)

// Checker reports whether whalenurse is working.
type Checker interface {
	Healthy() (ok bool, detail string)
}

// HealthHandler serves /healthz on the unix socket: 200 "ok", or 503 with the reason (for example the events stream being down for too
// long). The unix socket serves it for the image's HEALTHCHECK, and the TCP listener serves it without a token so that
// a reverse proxy or a monitor can probe it; it says nothing but the health text.
func HealthHandler(c Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok, detail := c.Healthy()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintln(w, detail)
	})
}

// PublicHealthHandler is /healthz on the TCP listener, where it needs no token and no allowed Host: it says "ok" or
// "not ok" (503) and nothing else. The reason (a stream down for 5m, a silent loop) is in the token-protected status
// and on the unix socket.
func PublicHealthHandler(c Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if ok, _ := c.Healthy(); !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ok")
			return
		}
		fmt.Fprintln(w, "ok")
	})
}
