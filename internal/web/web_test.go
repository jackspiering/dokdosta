package web

import (
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
	"go/build"
	"strings"
	"testing"
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
