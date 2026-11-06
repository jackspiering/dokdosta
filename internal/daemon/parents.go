package daemon

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackspiering/dokdosta/internal/compose"
	"github.com/jackspiering/dokdosta/internal/docker"
	"github.com/jackspiering/dokdosta/internal/rules"
)

// metaParents is the prefix of the store keys that remember which services a dependent hangs on.
const metaParents = "parents:"

// metaNSParent is the prefix of the store keys that remember the service(s) a dependent's network_mode, ipc or pid
// references: its namespace parent (plan v3.13 item 3), which decides whether the dependents step may repair it.
const metaNSParent = "nsparent:"

// MetaKeyed and MetaValues are the meta rows that hold a state key, which the store re-keys to the current host ID at
// startup (store.Rekey, plan v3.13 item 12): a prefix followed by the key, and a prefix whose value holds keys in a
// format of its owner's, with the owner's rewrite. A new meta record keyed by a state key must be added here;
// TestEveryMetaRowIsRekeyedOrHoldsNoKey fails for a prefix the daemon writes that is in neither.
var (
	MetaKeyed  = []string{metaParents, metaNSParent}
	MetaValues = map[string]func(value string, rekey func(string) string) (string, bool){metaTagMoved: rekeyTagMove}
)

// metaNoKey are the meta rows that hold no state key.
var metaNoKey = []string{MetaBootID, MetaLastSeen}

// parentsGoneAfter is how long a key may be absent from the container list before what is remembered about it is dropped
// (a compose recreate by hand removes the container for a moment, and the parent mapping must survive that).
const parentsGoneAfter = time.Hour

// A dependent (network_mode: service:X, or an ipc or pid reference) is tied to its parent by the compose depends_on label
// once the parent's ID is gone (plan §3.5, v3.6 decision 9). Compose writes that label from the services it has in scope,
// so a dependent recreated with `up --no-deps` (which is how dokdosta does it) comes back with an empty label (compose
// pkg/compose/create.go builds com.docker.compose.depends_on from service.DependsOn; v5.6.0 up.go narrows the project with
// types.IgnoreDependencies for --no-deps). From then on the label cannot find the parent, so the dependent would not be
// repaired when the parent is recreated the next time. dokdosta therefore remembers, per key, the services a dependent
// depends on, learned while the label or a live container: reference still shows them (plan v3.11).

// learnedParents is what dokdosta remembers about dependents between looks at them (plan v3.11 item 2, v3.13 item 3, #167):
// per state key, the services a dependent depends on and the services its container: references resolved to, kept in
// memory and in the store's meta (none in a dry run: nopMeta), and dropped an hour after the key left the container list.
// It knows nothing of the container list: the daemon tells it what a look at a container showed (learn), which keys are
// gone (absent), and asks it questions; so it is tested with a fake meta alone.
type learnedParents struct {
	meta      Meta
	parents   map[string][]string  // per key: the services a dependent depends on (meta "parents:<key>")
	nsParents map[string][]string  // per key: "kind:service" per container: reference (meta "nsparent:<key>")
	gone      map[string]time.Time // keys that were not in a list, and since when
}

func newLearnedParents(m Meta) *learnedParents {
	if m == nil {
		m = nopMeta{}
	}
	return &learnedParents{meta: m, parents: map[string][]string{}, nsParents: map[string][]string{}, gone: map[string]time.Time{}}
}

// nopMeta is the Meta of a dry run: nothing is read or kept.
type nopMeta struct{}

func (nopMeta) Meta(string) (string, bool, error) { return "", false, nil }
func (nopMeta) SetMeta(string, string)            {}
func (nopMeta) DeleteMeta(string)                 {}

// services returns the services that the dependent holding these labels depends on: the depends_on label while it has
// any, otherwise what was learned for the key. Every reader of the label goes through here (labelreads_test.go).
func (l *learnedParents) services(key string, labels map[string]string) []string {
	if s := dependsOn(labels[compose.LabelDependsOn]); len(s) > 0 {
		return s
	}
	return l.known(key)
}

func (l *learnedParents) known(key string) []string { return l.read(l.parents, metaParents, key) }

// namespace are the services that the live container: references of a dependent resolved to when dokdosta last saw them
// (plan v3.13 item 3); nil when it never saw one, as for a sidecar first seen after its parent was gone. They are kept per
// reference ("network:web", "ipc:db"), so that one reference resolving does not wipe what was learned for another.
func (l *learnedParents) namespace(key string) []string {
	var out []string
	for _, e := range l.read(l.nsParents, metaNSParent, key) {
		if _, svc, ok := strings.Cut(e, ":"); ok && !slices.Contains(out, svc) {
			out = append(out, svc)
		}
	}
	return out
}

// optIn are the services whose opt-in decides whether the dependents step repairs a sidecar whose parent is gone: its
// namespace parent, and only when that was never learned the services of its depends_on (plan v3.13 item 3).
func (l *learnedParents) optIn(key string, labels map[string]string) []string {
	if s := l.namespace(key); len(s) > 0 {
		return s
	}
	return l.services(key, labels)
}

// read reads a learned set from memory, then from the store's meta.
func (l *learnedParents) read(mem map[string][]string, prefix, key string) []string {
	if s, ok := mem[key]; ok {
		return s
	}
	v, ok, _ := l.meta.Meta(prefix + key) // a read error is counted and logged by the store; nothing is cached
	if !ok || v == "" {
		return nil
	}
	s := strings.Split(v, ",")
	mem[key] = s
	return s
}

// MetaLister is a Meta that can list its keys by prefix (internal/store). The daemon reads a learned set by key, and
// only for a key it sees; a container removed while dokdosta was down is never seen again, so without the listing its
// rows would stay for ever (#167, the follow-up of B-018).
type MetaLister interface {
	MetaKeys(prefix string) ([]string, error)
}

// load reads every learned set from the store at startup. A key that is not in the list then is scheduled by
// absentRemembered like any other, and forget drops it an hour after the first list without it; a key that is in the
// list keeps what it learned. A dry run has no store and loads nothing; a failed read is counted by the store, and those
// rows wait for the next start.
func (l *learnedParents) load() {
	ml, ok := l.meta.(MetaLister)
	if !ok {
		return
	}
	for _, set := range []struct {
		prefix string
		mem    map[string][]string
	}{{metaParents, l.parents}, {metaNSParent, l.nsParents}} {
		keys, err := ml.MetaKeys(set.prefix)
		if err != nil {
			continue
		}
		for _, k := range keys {
			l.read(set.mem, set.prefix, strings.TrimPrefix(k, set.prefix))
		}
	}
}

// learn records what a look at a dependent showed: its depends_on label, the live "kind:service" entries of its
// container: references that resolve to a container of its project (liveNS), and which kinds hold a container: reference
// at all (held). A non-empty label is authoritative and replaces what was stored; a live reference adds its service, and
// never shrinks a set learned from the label (depends_on can name services besides the one it shares a namespace with).
// The key is in the list, so it is no longer gone.
func (l *learnedParents) learn(key, dependsOnLabel string, liveNS []string, held map[string]bool) {
	delete(l.gone, key)
	var live []string
	for _, e := range liveNS {
		if _, svc, _ := strings.Cut(e, ":"); !slices.Contains(live, svc) {
			live = append(live, svc)
		}
	}
	known := l.known(key)
	var next []string
	if s := dependsOn(dependsOnLabel); len(s) > 0 {
		next = slices.Clone(s)
	} else {
		next = slices.Clone(known)
		for _, svc := range live {
			if !slices.Contains(next, svc) {
				next = append(next, svc)
			}
		}
	}
	if len(next) > 0 && !slices.Equal(next, known) {
		l.parents[key] = next
		l.meta.SetMeta(metaParents+key, strings.Join(next, ","))
	}
	// The namespace parent is what the live references say: per reference, replaced when it resolves, kept while it does
	// not (its parent is gone, which is when it matters), and dropped when the container no longer holds that reference
	// (plan v3.13 item 3).
	oldNS := l.read(l.nsParents, metaNSParent, key)
	var nextNS []string
	for _, e := range oldNS {
		kind, _, _ := strings.Cut(e, ":")
		if held[kind] && !slices.ContainsFunc(liveNS, func(s string) bool { return strings.HasPrefix(s, kind+":") }) {
			nextNS = append(nextNS, e)
		}
	}
	nextNS = append(nextNS, liveNS...)
	slices.Sort(nextNS)
	nextNS = slices.Compact(nextNS)
	switch {
	case len(nextNS) > 0 && !slices.Equal(nextNS, oldNS):
		l.nsParents[key] = nextNS
		l.meta.SetMeta(metaNSParent+key, strings.Join(nextNS, ","))
	case len(nextNS) == 0 && len(oldNS) > 0:
		// Every reference was dropped: a stale entry must not decide a later sidecar's opt-in.
		delete(l.nsParents, key)
		l.meta.DeleteMeta(metaNSParent + key)
	}
}

// absent notes that the key was not in the container list; the first time starts the clock forget runs on.
func (l *learnedParents) absent(key string, now time.Time) {
	if _, ok := l.gone[key]; !ok {
		l.gone[key] = now
	}
}

// keys are the keys something is remembered about.
func (l *learnedParents) keys() []string {
	var out []string
	for k := range l.parents {
		out = append(out, k)
	}
	for k := range l.nsParents {
		if _, ok := l.parents[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// forget drops what is remembered about keys that have been gone from the container list for parentsGoneAfter, and
// returns them, so the daemon drops what it remembers about them besides.
func (l *learnedParents) forget(now time.Time) []string {
	var out []string
	for key, since := range l.gone {
		if now.Sub(since) < parentsGoneAfter {
			continue
		}
		// Only a key that had a learned set has a row to delete (WN-040); the set may live in the store alone after a restart.
		learned, learnedNS := len(l.known(key)) > 0, len(l.namespace(key)) > 0
		delete(l.parents, key)
		delete(l.nsParents, key)
		delete(l.gone, key)
		if learned {
			l.meta.DeleteMeta(metaParents + key)
		}
		if learnedNS {
			l.meta.DeleteMeta(metaNSParent + key)
		}
		out = append(out, key)
	}
	return out
}

// ---- the daemon's side: what the container list says ----------------------------------------------

// parentServices: see learnedParents.services.
func (d *Daemon) parentServices(key string, labels map[string]string) []string {
	return d.learned.services(key, labels)
}

// liveNamespace are the "kind:service" entries for the references of c (network, ipc, pid) that resolve to a container of
// its project now; held says which kinds hold a container: reference at all.
func (d *Daemon) liveNamespace(c docker.Container) (live []string, held map[string]bool) {
	held = map[string]bool{}
	project := c.Labels[docker.LabelProject]
	for _, r := range nsRefs(c) {
		held[r.Kind] = true
		for _, s := range d.idx.all {
			svc := s.Labels[docker.LabelService]
			if svc == "" || s.Labels[docker.LabelProject] != project || docker.IsOneOff(s.Labels) || !docker.MatchesRef(s.ID, s.Name, r.Ref) {
				continue
			}
			if e := r.Kind + ":" + svc; !slices.Contains(live, e) {
				live = append(live, e)
			}
		}
	}
	return live, held
}

// learnParents tells learnedParents what this look at c showed.
func (d *Daemon) learnParents(key string, c docker.Container) {
	live, held := d.liveNamespace(c)
	d.learned.learn(key, c.Labels[compose.LabelDependsOn], live, held)
}

// absentRemembered are the keys dokdosta remembers something about outside the engine (learned parents, label warnings)
// whose container is not in the list. A container that is not opted in has no engine state, so the engine's own keys do not
// cover it; scheduling them lets process note that they are gone, which starts the clock forgetParents runs on (B-018).
func (d *Daemon) absentRemembered() []string {
	var out []string
	add := func(k string) {
		if _, ok := d.idx.byKey[k]; !ok && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for _, k := range d.learned.keys() {
		add(k)
	}
	for id := range d.warned {
		if k, _, ok := strings.Cut(id, "|"); ok { // a "|" in the host ID or a label cuts a wrong prefix: a warning logged again, never a row lost
			add(k)
		}
	}
	return out
}

// forgetParents drops what is remembered about keys that have been gone from the container list for a while.
func (d *Daemon) forgetParents(now time.Time) {
	for _, key := range d.learned.forget(now) {
		for id := range d.warned {
			if strings.HasPrefix(id, key+"|") {
				delete(d.warned, id)
			}
		}
	}
}

// parentIdentity names the parent a sidecar's repair is about, for the engine to tell a new parent change from the one it
// already repaired against (plan v3.13 item 1): per container: reference, the ID and start time of the container it
// resolves to, or that the reference is gone ("gone:<ref>": a recreated parent leaves the sidecar pointing at the ID it
// had, and a further recreate leaves it pointing at the next one). Only the sidecar's own references count, so a restart
// of some other service it depends on is no parent change. "" when a start time is not known.
func parentIdentity(parents []rules.Parent) string {
	var ids []string
	for _, p := range parents {
		switch {
		case !p.Found:
			ids = append(ids, "gone:"+p.Ref)
		case p.StartedAt.IsZero():
			return ""
		default:
			ids = append(ids, p.ID+"@"+strconv.FormatInt(p.StartedAt.UnixNano(), 10))
		}
	}
	slices.Sort(ids)
	return strings.Join(slices.Compact(ids), ";")
}

// dependsOnConditions is the raw depends_on label, for the one reader that needs the conditions and not only the names
// (waitsFor, plan v3.13 item 14). A created container that dokdosta has not recreated still has the compose-written label;
// an emptied one has no condition to wait on, so nothing holds it.
func dependsOnConditions(labels map[string]string) string {
	return labels[compose.LabelDependsOn]
}
