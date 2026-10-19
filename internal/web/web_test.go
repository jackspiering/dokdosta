package web

import (
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
