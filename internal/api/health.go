package api

import (
	"fmt"
	"net/http"
)

// Checker reports whether whalenurse is working.
type Checker interface {
	Healthy() (ok bool, detail string)
}

// HealthHandler serves /healthz: 200 "ok", or 503 with the reason (for example the events stream being down for too
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
