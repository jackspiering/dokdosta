package web

import (
	"go/build"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
)

// "The dashboard uses only this API": the package may render the documents of internal/api and nothing from the
// healer itself, so a page cannot show anything the API's output rules did not clean.
func TestWebImportsOnlyTheAPI(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if strings.HasPrefix(imp, "github.com/jackspiering/whalenurse/") && imp != "github.com/jackspiering/whalenurse/internal/api" {
			t.Errorf("internal/web imports %s: it may import internal/api only", imp)
		}
	}
	if len(pkg.TestImports) > 0 {
		for _, imp := range pkg.TestImports {
			if strings.Contains(imp, "internal/engine") || strings.Contains(imp, "internal/store") || strings.Contains(imp, "internal/daemon") {
				t.Errorf("its tests import %s", imp)
			}
		}
	}
}

func TestLadderTextSeparatesRunFromSkipped(t *testing.T) {
	opened := time.Now()
	c := api.ContainerDoc{
		Ladder: []string{"restart", "recreate", "pull"},
		Healer: api.HealerDoc{IncidentOpened: opened, RungsRun: 2, NextRung: "pull", SkippedRungs: []api.SkippedDoc{{Rung: "recreate", Reason: "x"}}},
	}
	if got, want := ladderPath(c), "restart ✓ → recreate skipped – not available → pull (next)"; got != want {
		t.Errorf("ladderPath = %q, want %q", got, want)
	}
	if got, want := ladder(c), "1 of 3 rungs run, 1 skipped (not available), next: pull"; got != want {
		t.Errorf("ladder = %q, want %q", got, want)
	}
	c.Healer.SkippedRungs = nil
	if got, want := ladderPath(c), "restart ✓ → recreate ✓ → pull (next)"; got != want {
		t.Errorf("ladderPath = %q, want %q", got, want)
	}
	if got := ladderPath(api.ContainerDoc{Ladder: []string{"restart", "recreate"}}); got != "restart → recreate" {
		t.Errorf("no incident: %q", got)
	}
}

func TestExpiryReadsAsTimeLeftAndTheClockTime(t *testing.T) {
	now := time.Now().UTC()
	re := regexp.MustCompile(`^<time datetime="[^"]+" title="[^"]+">(.*)</time>$`)
	text := func(t time.Time) string {
		m := re.FindStringSubmatch(string(expiry(t)))
		if m == nil {
			return "NO MATCH " + string(expiry(t))
		}
		return m[1]
	}
	if got := text(now.Add(2*time.Minute + 20*time.Second)); !regexp.MustCompile(`^2m left \(until \d\d:\d\d\)$`).MatchString(got) && !regexp.MustCompile(`^2m left \(until \d+ \w+ \d\d:\d\d\)$`).MatchString(got) {
		t.Errorf("2 minutes: %q", got)
	}
	in := now.Add(time.Hour + 57*time.Minute + 30*time.Second)
	want := "1h 57m left (until " + in.Format("15:04") + ")"
	if in.YearDay() != now.YearDay() {
		want = "1h 57m left (until " + in.Format("2 Jan 15:04") + ")"
	}
	if got := text(in); got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if got := text(now.Add(50*time.Hour + time.Minute)); !regexp.MustCompile(`^2d 2h left \(until \d+ \w{3} \d\d:\d\d\)$`).MatchString(got) {
		t.Errorf("another day carries its date: %q", got)
	}
	if got := text(now.Add(-3 * time.Minute)); got != "ended 3m ago" {
		t.Errorf("past: %q", got)
	}
	if strings.Contains(string(expiry(now.Add(time.Minute))), "until in") {
		t.Error("the old wording")
	}
	if expiry(time.Time{}) != "" {
		t.Error("zero")
	}
}

func TestMessageSaysDriftInPlainWordsAndKeepsTheRawTextUnderDetails(t *testing.T) {
	raw := "recreate skipped by the drift guard (config: compose would recreate this container (its dry-run says: Recreate, Recreated, Starting, Started): the compose file, an env file or the image changed since it was created): healthcheck reports unhealthy"
	got := string(message(raw))
	if !strings.Contains(got, "compose would recreate this container: the configuration or the image changed since it was created") {
		t.Errorf("plain sentence missing: %s", got)
	}
	plain := got[:strings.Index(got, "<details")]
	if strings.Contains(plain, "Recreated") || strings.Contains(plain, "dry-run says") {
		t.Errorf("the raw words are still in the sentence: %s", plain)
	}
	if !strings.Contains(got, `<details class="raw"><summary>details</summary><code>`) || !strings.Contains(got, "its dry-run says: Recreate, Recreated, Starting, Started") {
		t.Errorf("the raw output must stay under details: %s", got)
	}
	if got := string(message("restart ok <b>x</b>")); got != "restart ok &lt;b&gt;x&lt;/b&gt;" {
		t.Errorf("escaping: %s", got)
	}
}

func TestRelBadgeSaysWhatTheOtherEndIsDoing(t *testing.T) {
	for _, c := range []struct {
		r    api.RelationDoc
		want string
	}{
		{api.RelationDoc{State: "running", Health: "healthy", HealerState: "ok"}, `badge-ok">running, healthy<`},
		{api.RelationDoc{State: "running"}, `badge-ok">running<`},
		{api.RelationDoc{State: "exited"}, `badge-bad">exited<`},
		{api.RelationDoc{State: "running", Health: "unhealthy", HealerState: "SUSPENDED"}, `badge-bad">running, unhealthy · SUSPENDED<`},
		{api.RelationDoc{State: "running", Health: "unhealthy", HealerState: "incident"}, `badge-warn">running, unhealthy · incident<`},
		{api.RelationDoc{State: "running", HealerState: "PAUSED"}, `badge-info">running · PAUSED<`},
	} {
		if got := string(relBadge(c.r)); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %s, want %s", c.r, got, c.want)
		}
	}
	if relBadge(api.RelationDoc{}) != "" {
		t.Error("an unknown state has no badge")
	}
}

func TestInScopeMirrorsTheEnginesRule(t *testing.T) {
	for scope, want := range map[string]bool{"p": true, "p/web": true, "p/web/1": true, "p/db": false, "q": false, "p/web/1/x": false} {
		if got := inScope(scope, "h/p/web/1"); got != want {
			t.Errorf("inScope(%q) = %v", scope, got)
		}
	}
	if inScope("p", "nokey") {
		t.Error("a key without a host")
	}
}
