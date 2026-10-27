package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The depends_on label is emptied by `up --no-deps`, so every reader goes through parentServices (plan v3.11 item 2).
func TestDependsOnLabelIsReadOnlyInParentsGo(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "parents.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "LabelDependsOn") || strings.Contains(string(b), "compose.depends_on") {
			t.Errorf("%s reads the depends_on label: go through Daemon.parentServices", f)
		}
	}
}
