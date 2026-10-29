package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/dokdosta/internal/config"
	"github.com/jackspiering/dokdosta/internal/engine"
)

func TestParseForAcceptsTheDefaultAndIndefinite(t *testing.T) {
	if d, err := ParseFor("", config.ParseDuration); err != nil || d != engine.DefaultPause {
		t.Fatalf("empty: %v %v", d, err)
	}
	if d, err := ParseFor("0", config.ParseDuration); err != nil || d != 0 {
		t.Fatalf("0 must mean indefinite: %v %v", d, err)
	}
}

func TestParseForCapsThePauseAtOneHundredYears(t *testing.T) {
	if d, err := ParseFor("36500d", config.ParseDuration); err != nil || d != 36500*24*time.Hour {
		t.Fatalf("100 years must be accepted: %v %v", d, err)
	}
	// Past 2262 the stored UnixNano expiry overflows, so the pause would expire at the next restart.
	_, err := ParseFor("90000d", config.ParseDuration)
	if err == nil {
		t.Fatal(`"90000d" accepted`)
	}
	want := `--for "90000d": longer than 100 years; use --for 0 for no expiry`
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
}

func TestParseForKeepsTheOldErrorForGarbage(t *testing.T) {
	_, err := ParseFor("soon", config.ParseDuration)
	if err == nil || !strings.Contains(err.Error(), "want a duration such as 2h or 1d, or 0 for no expiry") {
		t.Fatalf("%v", err)
	}
}
