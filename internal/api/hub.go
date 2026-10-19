package api

import (
	"sync"

	"github.com/jackspiering/whalenurse/internal/engine"
)

// MaxStreams bounds the SSE clients; one more is refused with 503.
const MaxStreams = 32

// Hub wakes the SSE clients when an event was published and counts the events by type for /metrics. It is a bus
// subscriber, so it runs inside Publish under the engine's lock: it never blocks, and what it sends is a wake-up, not
// the event. A client then reads the event log from its own cursor, so a slow client costs nothing and loses nothing
// (a client that cannot keep up simply reads a longer page). Subscribe it after the event log's own subscriber, so the
// event is queryable by the time the clients wake.
type Hub struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	counts  map[string]uint64
	closed  bool
	done    chan struct{}
}

// NewHub creates a Hub.
func NewHub() *Hub {
	return &Hub{clients: map[chan struct{}]struct{}{}, counts: map[string]uint64{}, done: make(chan struct{})}
}

// Notify is the bus subscriber.
func (h *Hub) Notify(e engine.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counts[string(e.Type)]++
	for c := range h.clients {
		select {
		case c <- struct{}{}:
		default: // already woken
		}
	}
}

// Subscribe registers a client. ok is false when MaxStreams clients are connected or the hub is closed.
func (h *Hub) Subscribe() (wake <-chan struct{}, cancel func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) >= MaxStreams {
		return nil, nil, false
	}
	c := make(chan struct{}, 1)
	h.clients[c] = struct{}{}
	return c, func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}, true
}

// Done is closed by Close: the streams end.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Close ends every stream. Register it with http.Server.RegisterOnShutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

// Clients is the number of connected streams.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Counts returns how many events of each type were published since the process started.
func (h *Hub) Counts() map[string]uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]uint64, len(h.counts))
	for k, v := range h.counts {
		out[k] = v
	}
	return out
}
