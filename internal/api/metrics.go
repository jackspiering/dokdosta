package api

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// metrics serves GET /metrics in the Prometheus text format (exposition 0.0.4). It is written by hand: the format is
// a dozen lines per family, and the official client would add a dependency and binary size for nothing else.
func (s *Service) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	st := s.Status()
	cs := s.Containers(false)
	m := &metricWriter{w: w}

	m.family("dokdosta_info", "gauge", "Build and mode of this dokdosta (always 1).")
	m.sample("dokdosta_info", 1, "host", st.Host, "version", st.Version, "dry_run", fmt.Sprint(st.DryRun))
	m.family("dokdosta_healthy", "gauge", "1 when the daemon's own health check passes.")
	m.sample("dokdosta_healthy", b2f(st.Healthy))
	m.family("dokdosta_events_stream_up", "gauge", "1 while the Docker events stream is connected.")
	m.sample("dokdosta_events_stream_up", b2f(st.EventsUp))
	m.family("dokdosta_recreate_enabled", "gauge", "1 when rungs 2 and 3 are enabled (the version gate passed).")
	m.sample("dokdosta_recreate_enabled", b2f(st.VersionGate.RecreateEnabled))
	m.family("dokdosta_actions_last_hour", "gauge", "Actions taken in the last hour, against the global rate limit.")
	m.sample("dokdosta_actions_last_hour", float64(st.RateLimit.Used))
	m.family("dokdosta_action_rate_limit", "gauge", "DOKDOSTA_RATE_LIMIT: actions per hour.")
	m.sample("dokdosta_action_rate_limit", float64(st.RateLimit.Limit))
	m.family("dokdosta_last_event_id", "gauge", "The highest event ID of this host.")
	m.sample("dokdosta_last_event_id", float64(st.LastEventID))
	if st.Store != nil {
		m.family("dokdosta_store_errors_total", "counter", "Store operations that failed since the process started; healing carries on from memory.")
		m.sample("dokdosta_store_errors_total", float64(st.Store.Errors))
	}

	m.family("dokdosta_containers", "gauge", "Containers by dokdosta's state; only watched containers have a state other than ok.")
	m.sample("dokdosta_containers", float64(st.Counts.Containers-st.Counts.Watched), "state", "unwatched")
	for _, p := range []struct {
		state string
		n     int
	}{{"ok", st.Counts.OK}, {"incident", st.Counts.Incident}, {"recovering", st.Counts.Recovering}, {"suspended", st.Counts.Suspended}, {"paused", st.Counts.Paused}} {
		m.sample("dokdosta_containers", float64(p.n), "state", p.state)
	}

	m.family("dokdosta_container_suspended", "gauge", "1 while the watched container is SUSPENDED.")
	for _, c := range cs {
		m.sample("dokdosta_container_suspended", b2f(c.Healer.State == "SUSPENDED"), "key", c.Key)
	}
	m.family("dokdosta_container_flaps", "gauge", "Incidents of the watched container within the flap window.")
	for _, c := range cs {
		m.sample("dokdosta_container_flaps", float64(c.Healer.Flaps), "key", c.Key)
	}

	if s.Hub != nil {
		counts := s.Hub.Counts()
		types := make([]string, 0, len(counts))
		for t := range counts {
			types = append(types, t)
		}
		sort.Strings(types)
		m.family("dokdosta_events_total", "counter", "Domain events published since the process started, by type.")
		for _, t := range types {
			m.sample("dokdosta_events_total", float64(counts[t]), "type", t)
		}
	}

	n := st.Notifications
	m.family("dokdosta_notifications_sent_total", "counter", "Notifications delivered, all URLs (never a URL).")
	m.sample("dokdosta_notifications_sent_total", float64(n.Sent))
	m.family("dokdosta_notifications_failed_total", "counter", "Notification deliveries that failed, all URLs.")
	m.sample("dokdosta_notifications_failed_total", float64(n.Failed))
	m.family("dokdosta_notifications_rate_limited_total", "counter", "Notifications held back by the per-URL rate limit, all URLs (never a URL).")
	m.sample("dokdosta_notifications_rate_limited_total", float64(n.RateLimited))
	m.family("dokdosta_notifications_dropped_total", "counter", "Notifications dropped because the queue was full, all URLs (never a URL).")
	m.sample("dokdosta_notifications_dropped_total", float64(n.Dropped))
	if s.Hub != nil {
		m.family("dokdosta_sse_refused_total", "counter", "Event streams refused with 503 because the maximum number of streams was connected.")
		m.sample("dokdosta_sse_refused_total", float64(s.Hub.Refused()))
	}
}

type metricWriter struct{ w io.Writer }

func (m *metricWriter) family(name, typ, help string) {
	fmt.Fprintf(m.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// sample writes one sample; kv are label name/value pairs.
func (m *metricWriter) sample(name string, v float64, kv ...string) {
	var b strings.Builder
	b.WriteString(name)
	for i := 0; i+1 < len(kv); i += 2 {
		if i == 0 {
			b.WriteByte('{')
		} else {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, kv[i], labelValue(kv[i+1]))
	}
	if len(kv) >= 2 {
		b.WriteByte('}')
	}
	fmt.Fprintf(m.w, "%s %g\n", b.String(), v)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func labelValue(v string) string { return labelEscaper.Replace(v) }

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
