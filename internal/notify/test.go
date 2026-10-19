package notify

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// TestMessage is what `whalenurse notify test` sends.
func TestMessage(host string, dryRun bool) Message {
	return compose(rctx{host: host, dryRun: dryRun}, engine.Event{Host: host}, "", "test notification", []string{"sent by: whalenurse notify test", "If you can read this, notifications work."}, nil, false)
}

// TestResult is the outcome for one URL.
type TestResult struct {
	URL      string // "#2 discord"
	OK       bool
	Err      string // redacted
	Duration time.Duration
}

// SendTest sends one message to every target, one attempt each, and reports each. It bypasses the dedupe, the rate limit
// and the retries: the point is to see what a single delivery does. Targets run one after the other, so a slow URL
// shows up as its own duration.
func SendTest(ctx context.Context, targets []Target, red *Redactor, timeout time.Duration, m Message) []TestResult {
	out := make([]TestResult, 0, len(targets))
	for _, t := range targets {
		r := TestResult{URL: t.Label}
		if t.Sender == nil {
			r.Err = "invalid URL: " + t.Err
			out = append(out, r)
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		err := t.Sender.Send(cctx, m)
		cancel()
		r.Duration = time.Since(start).Round(time.Millisecond)
		if err != nil {
			r.Err = red.Err(err)
		} else {
			r.OK = true
		}
		out = append(out, r)
	}
	return out
}

// PrintTest writes one line per URL and returns how many failed.
func PrintTest(w io.Writer, res []TestResult) (failed int) {
	for _, r := range res {
		if r.OK {
			fmt.Fprintf(w, "%-14s ok      (%s)\n", r.URL, r.Duration)
			continue
		}
		failed++
		fmt.Fprintf(w, "%-14s FAILED  %s\n", r.URL, r.Err)
	}
	return failed
}
