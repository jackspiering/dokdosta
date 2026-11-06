package daemon

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// listingMeta is a Meta that can list its keys, like the store.
type listingMeta struct{ memMetaSafe }

func newListingMeta() listingMeta {
	return listingMeta{memMetaSafe{mu: &sync.Mutex{}, m: map[string]string{}}}
}

func (m listingMeta) MetaKeys(prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

// learnedParents through its own interface, with a meta of its own: no daemon, no container list.
func TestLearnedParentsLearnKeepAndForget(t *testing.T) {
	m := newListingMeta()
	l := newLearnedParents(m)
	const k = "h/p/dep/1"
	held := map[string]bool{"network": true, "ipc": true}

	// The label is authoritative; a live reference adds its service.
	l.learn(k, "db:service_started:false", []string{"network:web"}, held)
	if got := l.known(k); !slices.Equal(got, []string{"db"}) {
		t.Fatalf("the label replaces: %v", got)
	}
	l.learn(k, "", []string{"network:web", "ipc:cache"}, held)
	if got := l.known(k); !slices.Equal(got, []string{"db", "web", "cache"}) {
		t.Fatalf("a live reference adds and never shrinks: %v", got)
	}
	if got := l.services(k, nil); !slices.Equal(got, []string{"db", "web", "cache"}) {
		t.Fatalf("services without a label: %v", got)
	}
	if got := l.optIn(k, nil); !slices.Equal(got, []string{"cache", "web"}) {
		t.Fatalf("the namespace parent decides the opt-in: %v", got)
	}
	// A reference that no longer resolves keeps what it said; one the container no longer holds is dropped.
	l.learn(k, "", nil, map[string]bool{"network": true})
	if got := l.namespace(k); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("ipc is no longer held, network is kept while unresolved: %v", got)
	}

	// A fresh one reads the rows back at startup.
	l2 := newLearnedParents(m)
	l2.load()
	if got := l2.known(k); !slices.Equal(got, []string{"db", "web", "cache"}) {
		t.Fatalf("not loaded: %v", got)
	}
	if !slices.Contains(l2.keys(), k) {
		t.Fatal("a loaded key is remembered, so the daemon schedules it")
	}

	// Gone for an hour: forgotten, rows and all; seen again before that: kept.
	now := time.Unix(1_000_000, 0)
	l2.absent(k, now)
	l2.absent(k, now.Add(30*time.Minute)) // the clock starts at the first absence
	if got := l2.forget(now.Add(59 * time.Minute)); len(got) != 0 {
		t.Fatalf("forgotten too early: %v", got)
	}
	if got := l2.forget(now.Add(time.Hour)); !slices.Equal(got, []string{k}) {
		t.Fatalf("not forgotten: %v", got)
	}
	if len(m.m) != 0 || len(l2.keys()) != 0 {
		t.Fatalf("rows left: %v %v", m.m, l2.keys())
	}
}

// A dry run has no store: the same module, nothing kept beyond memory.
func TestLearnedParentsWithoutAStore(t *testing.T) {
	l := newLearnedParents(nil)
	l.load()
	l.learn("k", "db", nil, nil)
	if got := l.known("k"); !slices.Equal(got, []string{"db"}) {
		t.Fatalf("%v", got)
	}
}
