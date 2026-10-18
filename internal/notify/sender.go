package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// shoutrrrSender is one Shoutrrr service behind one URL. Each URL gets a router of its own, so a failing URL is reported
// as that URL and its delivery cannot hold up another.
type shoutrrrSender struct {
	r *router.ServiceRouter
}

// NewTargets builds a Target for every URL, in order. A URL that Shoutrrr rejects becomes a target with Err set (redacted),
// so the daemon starts and heals all the same and the status says what is wrong.
func NewTargets(urls []string, red *Redactor, timeout time.Duration) []Target {
	out := make([]Target, 0, len(urls))
	for i, u := range urls {
		t := Target{Label: Label(i, u)}
		r, err := shoutrrr.CreateSenderWithOptions(types.SenderOptions{Timeout: timeout}, u)
		if err != nil {
			t.Err = red.Err(err)
			if t.Err == "" {
				t.Err = "invalid URL"
			}
		} else {
			t.Sender = &shoutrrrSender{r: r}
		}
		out = append(out, t)
	}
	return out
}

// Send delivers the message; the title goes in as Shoutrrr's title param (a service that has no title ignores it, and the
// body carries the host and the key on its own).
func (s *shoutrrrSender) Send(ctx context.Context, m Message) error {
	errs := s.r.SendContext(ctx, m.Body, &types.Params{"title": m.Title})
	var failed []error
	for _, err := range errs {
		if err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("send: %w", errors.Join(failed...))
}

func (s *shoutrrrSender) Close() error { return s.r.Close() }
