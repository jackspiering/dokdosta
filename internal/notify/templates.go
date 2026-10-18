package notify

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// Message is one notification: a title and a body, both short enough for a phone push.
type Message struct {
	Title string
	Body  string
	// Critical messages (giving up, drift blocked, flapping, a parent left down, rungs disabled) have a reserve in the
	// per-URL rate limit: a storm of routine messages cannot use up the budget that "giving up" needs.
	Critical bool
}

// The size limits. A phone shows about 40 characters of a title and a few lines of a body before it cuts them off.
const (
	MaxTitle  = 72  // runes
	MaxBody   = 360 // runes, the key line included
	maxDetail = 140 // runes, one detail line
)

// DryRunMarker is put in front of every message a dry run sends, in the title and in the first line of the body (some
// services drop the title).
const DryRunMarker = "[dry-run]"

// rctx is what the templates need besides the event.
type rctx struct {
	host   string
	dryRun bool
	reason string // why the action that just ended was started (from its ActionStarted)
}

// render turns an event into a message. It reports false for the events that are not notified: everything the plan does
// not list stays in the log and the store. dedupe is the identity of the thing said, "" when it is never suppressed.
func render(e engine.Event, c rctx) (m Message, dedupe string, ok bool) {
	var (
		headline string
		detail   []string
		discr    string
		critical bool
		once     = true // false: deliberate commands (pause, resume) are never deduplicated
	)
	subject := subjectOf(e)
	scope := scopeOf(e.Key)
	attr := func(k string) string { return e.Attrs[k] }

	switch e.Type {
	case engine.ActionResult:
		verb, failed := verbOf(attr("action"))
		discr = attr("action") + "/" + attr("rung") + "/" + attr("ok")
		switch {
		case attr("dry_run") == "true":
			headline = "would " + attr("action")
		case attr("ok") == "true":
			headline = verb
		default:
			headline = failed
			detail = append(detail, "error: "+afterPrefix(e.Message, attr("action")+" failed: "))
		}
		if c.reason != "" {
			detail = append(detail, "why: "+c.reason)
		}
		if r := attr("rung"); r != "" && attr("dry_run") != "true" {
			detail = append(detail, "rung: "+r)
		}

	case engine.Recovered:
		headline = "recovered"
		detail = append(detail, e.Message)

	case engine.Suspended:
		switch attr("reason") {
		case engine.ReasonDrift:
			// DriftBlocked, published just before it, says all of this. One message, not two.
			return Message{}, "", false
		case engine.ReasonFlapping:
			headline = "flapping, SUSPENDED"
			detail = append(detail, e.Message)
		default:
			headline = "giving up, SUSPENDED"
			detail = append(detail, "cause: "+causeOf(e.Message))
		}
		discr = attr("reason")
		critical = true
		detail = append(detail, "no more actions until it recovers or you run: "+unsuspendCmd(scope))

	case engine.DriftBlocked:
		headline = "recreate blocked by the drift guard, SUSPENDED"
		findings := attr("findings")
		discr = findings
		critical = true
		detail = append(detail, "reason: "+shortFindings(findings))
		if hint := driftHint(findings, scope); hint != "" {
			detail = append(detail, hint)
		}
		detail = append(detail, "then: "+unsuspendCmd(scope))

	case engine.DriftWarned:
		headline = "recreating despite drift (WHALENURSE_DRIFT_GUARD=warn)"
		discr = attr("findings")
		detail = append(detail, "found: "+attr("findings"))

	case engine.NotifyOnly:
		headline = "needs attention, nothing was done"
		discr = attr("class") + "/" + attr("dependents")
		critical = true
		detail = append(detail, e.Message)
		if deps := attr("dependents"); deps != "" {
			detail = append(detail, "left down with it: "+strings.ReplaceAll(deps, ",", ", "))
		}

	case engine.NeedsRecreate:
		headline = "needs a recreate that is not available"
		discr = attr("class")
		critical = true
		detail = append(detail, e.Message)

	case engine.RungsDisabled:
		headline = "recreate and pull rungs are disabled"
		discr = e.Message
		critical = true
		detail = append(detail, e.Message, "the ladder ends after the restart rung")

	case engine.Paused:
		subject, headline = "", "paused "+scope
		detail = append(detail, e.Message)
		once = false

	case engine.Resumed:
		subject, headline = "", "resumed "+scope
		detail = append(detail, e.Message)
		once = false

	default:
		return Message{}, "", false
	}

	if once {
		dedupe = strings.Join([]string{string(e.Type), e.Key, discr, attr(engine.AttrIncident)}, "|")
	}
	return compose(c, e, subject, headline, detail, critical), dedupe, true
}

// compose lays the message out: the title names the host and the container, the body starts with the key (host and
// container, always) and then the details, cut to fit.
func compose(c rctx, e engine.Event, subject, headline string, detail []string, critical bool) Message {
	host := e.Host
	if host == "" {
		host = c.host
	}
	// The headline is what the push is about, so a long container name is what gets cut, not the headline.
	marker := ""
	if c.dryRun {
		marker = DryRunMarker + " "
	}
	title := marker + host + ": " + headline
	if subject != "" && subject != host {
		room := MaxTitle - utf8.RuneCountInString(marker+host+":  "+headline)
		if room < 8 {
			room = 8
		}
		title = marker + host + ": " + cut(subject, room) + " " + headline
	}
	keyLine := e.Key
	if keyLine == "" {
		keyLine = "host " + host
	}
	if c.dryRun {
		keyLine = DryRunMarker + " " + keyLine
	}
	lines := []string{keyLine}
	for _, d := range detail {
		if d = oneLine(d); d != "" {
			lines = append(lines, cut(d, maxDetail))
		}
	}
	return Message{Title: cut(title, MaxTitle), Body: fit(lines, MaxBody), Critical: critical}
}

// fit joins the lines and cuts the result to max runes. The first line (the key) is never what gets cut.
func fit(lines []string, max int) string {
	out := lines[0]
	for _, l := range lines[1:] {
		next := out + "\n" + l
		if utf8.RuneCountInString(next) > max {
			room := max - utf8.RuneCountInString(out) - 1
			if room > 8 {
				out += "\n" + cut(l, room)
			}
			break
		}
		out = next
	}
	return out
}

// cut shortens s to max runes, ending in an ellipsis when it did.
func cut(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimRight(string(r[:max-1]), " ,;:") + "…"
}

// oneLine collapses a message that may be multi-line (compose prints its errors over several) into one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// subjectOf names the container: its name, or the last part of the key.
func subjectOf(e engine.Event) string {
	if e.Container != "" {
		return e.Container
	}
	if i := strings.Index(e.Key, "/"); i >= 0 {
		return e.Key[i+1:]
	}
	return ""
}

// scopeOf is the `whalenurse pause|unsuspend` scope of a key: project/service for a compose container (the key is
// host/project/service/number), the name for any other (host/name). For a pause the key is host/scope already.
func scopeOf(key string) string {
	parts := strings.Split(key, "/")
	switch {
	case len(parts) >= 4:
		return parts[1] + "/" + parts[2]
	case len(parts) >= 2:
		return strings.Join(parts[1:], "/")
	}
	return key
}

func unsuspendCmd(scope string) string {
	if scope == "" {
		return "whalenurse unsuspend <scope>"
	}
	return "whalenurse unsuspend " + scope
}

// verbOf names an action for a message: the past tense, and the failure.
func verbOf(action string) (done, failed string) {
	switch action {
	case "restart":
		return "restarted", "restart failed"
	case "start":
		return "started", "start failed"
	case "recreate":
		return "recreated", "recreate failed"
	case "recreate-stale":
		return "recreated (its parent was recreated)", "recreate failed"
	}
	return action + " done", action + " failed"
}

func afterPrefix(s, prefix string) string {
	if _, rest, ok := strings.Cut(s, prefix); ok {
		return rest
	}
	return s
}

// causeOf is the cause in a Suspended message: "every rung ran or was unavailable: <why>". The part before the first
// colon is the ladder's own wording and the rest is what was wrong with the container.
func causeOf(msg string) string {
	if _, rest, ok := strings.Cut(msg, ": "); ok && rest != "" {
		return rest
	}
	return msg
}

var (
	reDryRunSays = regexp.MustCompile(`\s*\(its dry-run says:[^)]*\)`)
	reUpgraded   = regexp.MustCompile(`\s*\(compose upgraded from ([^ ]+) to ([^ ]+) since[^)]*\)`)
)

// shortFindings drops from the guard's text what a phone has no room for: compose's own dry-run wording, and the
// upgrade note (the hint says it again, shorter, with the versions).
func shortFindings(f string) string {
	f = reDryRunSays.ReplaceAllString(f, "")
	return reUpgraded.ReplaceAllString(f, "")
}

// driftHint is the next step for the findings of the drift guard. The guard's own text already says what it found, and for
// a compose that was upgraded since the container was created (the host plugin) or is not the release that made it (the
// bundled copy) it says so; what it cannot say is what to do about it.
func driftHint(findings, scope string) string {
	switch {
	case strings.Contains(findings, "can't verify drift (compose"):
		return "hint: whalenurse's compose is not the one that made it. Mount the host plugin at /host/docker-compose, or: docker compose up -d " + serviceOf(scope)
	case reUpgraded.MatchString(findings):
		v := reUpgraded.FindStringSubmatch(findings)
		return "hint: compose upgraded " + v[1] + " to " + v[2] + " since creation: maybe a new hash layout, not an edit. If the file is fine: docker compose up -d " + serviceOf(scope)
	case strings.Contains(findings, "the tag moved"):
		return "hint: the image tag points at another image now. Take it with: docker compose up -d " + serviceOf(scope) + ", or re-tag the old one"
	case strings.Contains(findings, "would recreate") || strings.Contains(findings, "config --hash is"):
		return "hint: the compose file, an env file or the image changed since creation. Apply it yourself: docker compose up -d " + serviceOf(scope)
	}
	return ""
}

func serviceOf(scope string) string {
	if _, svc, ok := strings.Cut(scope, "/"); ok {
		return svc
	}
	return scope
}

// String lets a Message print as its title, for test failures.
func (m Message) String() string { return fmt.Sprintf("%q / %q", m.Title, m.Body) }
