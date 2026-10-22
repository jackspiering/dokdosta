package rules

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A long State.Error is cut on a rune, never inside one (a split rune shows as U+FFFD on the pages).
func TestTrimCutsOnRunes(t *testing.T) {
	got := trim("x" + strings.Repeat("é", 400))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 303 || !strings.HasSuffix(got, "é...") {
		t.Fatalf("%q", got)
	}
	if got := trim("  a \n b  "); got != "a b" {
		t.Fatalf("%q", got)
	}
}
