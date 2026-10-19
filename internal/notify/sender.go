package notify

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// shoutrrrSender is one Shoutrrr service behind one URL.
//
// It locates and initialises the service through Shoutrrr's router, but sends by calling the service itself, in the
// caller's goroutine, under a recover. The router's own Send runs each service in a goroutine of its own with no recover
// (v0.22.0, pkg/router/router.go sendToService), so a panic inside a service (its release notes include "stop custom url
// panic") would take the whole process down, and a dead whalenurse heals nothing. A panic here is a failed delivery.
type shoutrrrSender struct {
	svc types.Service
}

// NewTargets builds a Target for every URL, in order. A URL that Shoutrrr rejects becomes a target with Err set (redacted),
// so the daemon starts and heals all the same and the status says what is wrong.
func NewTargets(urls []string, red *Redactor, timeout time.Duration) []Target {
	out := make([]Target, 0, len(urls))
	for i, u := range urls {
		t := Target{Label: Label(i, u)}
		s, err := newShoutrrrSender(u, timeout)
		if err != nil {
			t.Err = red.Err(err)
			if t.Err == "" {
				t.Err = "invalid URL"
			}
		} else {
			t.Sender = s
		}
		out = append(out, t)
	}
	return out
}

func newShoutrrrSender(rawURL string, timeout time.Duration) (s Sender, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("creating sender: panic: %v", r)
		}
	}()
	r, err := router.NewWithOptions(nil, types.SenderOptions{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("creating sender: %w", err)
	}
	svc, err := r.Locate(rawURL)
	if err != nil {
		return nil, fmt.Errorf("creating sender: %w", err)
	}
	return &shoutrrrSender{svc: svc}, nil
}

// Send delivers the message; the title goes in as Shoutrrr's title param (a service that has no title ignores it, and the
// body carries the host and the key on its own). The context carries the timeout.
func (s *shoutrrrSender) Send(ctx context.Context, m Message) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in the %s service: %v", s.svc.GetID(), r)
		}
	}()
	params := types.Params{"title": m.Title} // fresh each time: services write to it
	if cs, ok := s.svc.(types.ContextSender); ok {
		if err := cs.SendContext(ctx, m.Body, &params); err != nil {
			return fmt.Errorf("send: %w", err)
		}
		return nil
	}
	// A service without a context cannot be cut short: wait for it in a goroutine, up to the deadline.
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic in the %s service: %v", s.svc.GetID(), r)
			}
		}()
		done <- s.svc.Send(m.Body, &params)
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("send: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("send: %w", ctx.Err())
	}
}

func (s *shoutrrrSender) Close() error {
	if c, ok := s.svc.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
