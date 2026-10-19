package web

import (
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
)

var funcs = template.FuncMap{
	"badge":         badge,
	"timeTag":       timeTag,
	"containerPath": containerPath,
	"ladder":        ladder,
	"ladderPath":    ladderPath,
	"cooling":       func(t time.Time) bool { return !t.IsZero() && t.After(time.Now()) },
	"short":         short,
	"expiry":        expiry,
	"message":       message,
	"relBadge":      relBadge,
	"ifelse": func(c bool, a, b string) string {
		if c {
			return a
		}
		return b
	},
}

// badge is a coloured label. The class is one of a fixed set and the text is escaped by html/template; the colour is
// never the only signal, the text says the same.
func badge(kind, text string) template.HTML {
	switch kind {
	case "ok", "warn", "bad", "info":
	default:
		kind = "info"
	}
	return template.HTML(`<span class="badge badge-` + kind + `">` + template.HTMLEscapeString(text) + `</span>`)
}

// timeTag renders a UTC time with its age, machine-readable in datetime.
func timeTag(t time.Time) template.HTML {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	return template.HTML(fmt.Sprintf(`<time datetime="%s" title="%s">%s</time>`,
		t.Format(time.RFC3339), t.Format("2006-01-02 15:04:05 UTC"), template.HTMLEscapeString(relative(t))))
}

func relative(t time.Time) string {
	d := time.Since(t)
	future := d < 0
	if future {
		d = -d
	}
	var s string
	switch {
	case d < 5*time.Second:
		return "now"
	case d < time.Minute:
		s = fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		s = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		s = fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return t.Format("2006-01-02 15:04 UTC")
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}

// containerPath is the dashboard path of a state key: the key's slashes stay, each part is escaped.
func containerPath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/containers/" + strings.Join(parts, "/")
}

// skippedSet is the rungs of the open incident that were skipped.
func skippedSet(c api.ContainerDoc) map[string]bool {
	m := map[string]bool{}
	for _, k := range c.Healer.SkippedRungs {
		m[k.Rung] = true
	}
	return m
}

// ladder says how far the incident has got: "1 of 2 rungs run, 1 skipped (not available), next: recreate".
func ladder(c api.ContainerDoc) string {
	if c.Healer.IncidentOpened.IsZero() {
		return ""
	}
	n := len(c.Ladder)
	if n == 0 {
		return ""
	}
	skip := skippedSet(c)
	passed, skipped := 0, 0
	for i, r := range c.Ladder {
		if i >= c.Healer.RungsRun {
			break
		}
		if skip[r] {
			skipped++
		} else {
			passed++
		}
	}
	s := fmt.Sprintf("%d of %d rungs run", passed, n)
	if skipped > 0 {
		s += fmt.Sprintf(", %d skipped (not available)", skipped)
	}
	if c.Healer.NextRung != "" {
		s += ", next: " + c.Healer.NextRung
	}
	return s
}

// ladderPath draws the ladder with what happened to each rung: "restart ✓ → recreate skipped – not available → pull".
func ladderPath(c api.ContainerDoc) string {
	skip := skippedSet(c)
	var parts []string
	for i, r := range c.Ladder {
		switch {
		case c.Healer.IncidentOpened.IsZero():
			parts = append(parts, r)
		case i < c.Healer.RungsRun && skip[r]:
			parts = append(parts, r+" skipped – not available")
		case i < c.Healer.RungsRun:
			parts = append(parts, r+" ✓")
		case i == c.Healer.RungsRun && c.Healer.NextRung != "":
			parts = append(parts, r+" (next)")
		default:
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, " → ")
}

// short cuts an image ID to 12 hex digits.
func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// humanDur writes a duration the way the pages do: 45s, 12m, 1h 57m, 3d 4h.
func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// expiry writes the end of a cooldown or a pause as "2m left (until 14:32)". The clock time is UTC, like every time on
// the pages; a time on another day carries its date. A time that has passed says so.
func expiry(t time.Time) template.HTML {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	now := time.Now().UTC()
	var text string
	if d := t.Sub(now); d <= 0 {
		text = "ended " + humanDur(-d) + " ago"
	} else {
		clock := t.Format("15:04")
		if t.YearDay() != now.YearDay() || t.Year() != now.Year() {
			clock = t.Format("2 Jan 15:04")
		}
		text = fmt.Sprintf("%s left (until %s)", humanDur(d), clock)
	}
	return template.HTML(fmt.Sprintf(`<time datetime="%s" title="%s">%s</time>`, t.Format(time.RFC3339), t.Format("2006-01-02 15:04:05 UTC"), template.HTMLEscapeString(text)))
}

// dryRunSays matches the part of the drift guard's config finding that quotes compose's dry-run output.
var dryRunSays = regexp.MustCompile(` \(its dry-run says: [^)]*\): the compose file, an env file or the image changed since it was created`)

// message writes a message of the event log for people: compose's raw dry-run words ("Recreate, Recreated, Starting,
// Started") become a plain sentence, and the original text stays one click away under "details".
func message(s string) template.HTML {
	plain := dryRunSays.ReplaceAllString(s, ": the configuration or the image changed since it was created")
	if plain == s {
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(template.HTMLEscapeString(plain) +
		` <details class="raw"><summary>details</summary><code>` + template.HTMLEscapeString(s) + `</code></details>`)
}

// relBadge says in one badge what the other end of a network_mode link is doing: "running, healthy", "exited",
// "running, unhealthy · SUSPENDED".
func relBadge(r api.RelationDoc) template.HTML {
	if r.State == "" {
		return ""
	}
	text, kind := r.State, "ok"
	if r.State != "running" {
		kind = "bad"
	}
	if r.Health != "" {
		text += ", " + r.Health
		if r.Health == "unhealthy" && kind == "ok" {
			kind = "warn"
		}
	}
	switch r.HealerState {
	case "", "ok":
	case "SUSPENDED":
		text, kind = text+" · SUSPENDED", "bad"
	case "incident":
		text += " · incident"
		if kind == "ok" {
			kind = "warn"
		}
	default:
		text += " · " + r.HealerState
		if kind == "ok" {
			kind = "info"
		}
	}
	return badge(kind, text)
}
