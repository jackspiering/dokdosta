package web

import (
	"fmt"
	"html/template"
	"net/url"
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

// ladder says how far the incident has got: "restart done, next: recreate".
func ladder(c api.ContainerDoc) string {
	if c.Healer.IncidentOpened.IsZero() {
		return ""
	}
	n := len(c.Ladder)
	switch {
	case n == 0:
		return ""
	case c.Healer.NextRung == "":
		return fmt.Sprintf("all %d rungs run", n)
	default:
		return fmt.Sprintf("%d of %d rungs run, next: %s", c.Healer.RungsRun, n, c.Healer.NextRung)
	}
}

// ladderPath draws the ladder with the rungs that ran: "restart ✓ → recreate (next) → pull".
func ladderPath(c api.ContainerDoc) string {
	var parts []string
	for i, r := range c.Ladder {
		switch {
		case c.Healer.IncidentOpened.IsZero():
			parts = append(parts, r)
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
