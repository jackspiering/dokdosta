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

// MetaKeyed and MetaWithKey are the meta rows that hold a state key, which the store re-keys to the current host ID at
// startup (store.Rekey, plan v3.13 item 12): a prefix followed by the key, and a prefix whose JSON value has a "key".
// A new meta record keyed by a state key must be added here.
var (
	MetaKeyed   = []string{metaParents, metaNSParent}
	MetaWithKey = []string{metaTagMoved}
)

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

// parentServices returns the services that the dependent holding these labels depends on: the label while it has any,
// otherwise what was learned for the key.
func (d *Daemon) parentServices(key string, labels map[string]string) []string {
	if l := dependsOn(labels[compose.LabelDependsOn]); len(l) > 0 {
		return l
	}
	return d.knownParents(key)
}

func (d *Daemon) knownParents(key string) []string { return d.known(d.parents, metaParents, key) }

// namespaceParents are the services that the live container: references of a dependent resolved to when dokdosta last
// saw them (plan v3.13 item 3); nil when it never saw one, as for a sidecar first seen after its parent was gone. They are
// kept per reference ("network:web", "ipc:db"), so that one reference resolving does not wipe what was learned for another.
func (d *Daemon) namespaceParents(key string) []string {
	var out []string
	for _, e := range d.known(d.nsParents, metaNSParent, key) {
		if _, svc, ok := strings.Cut(e, ":"); ok && !slices.Contains(out, svc) {
			out = append(out, svc)
		}
	}
	return out
}

// known reads a learned set from memory, then from the store's meta.
func (d *Daemon) known(mem map[string][]string, prefix, key string) []string {
	if l, ok := mem[key]; ok {
		return l
	}
	if d.o.Meta == nil {
		return nil
	}
	v, ok, _ := d.o.Meta.Meta(prefix + key) // a read error is counted and logged by the store; nothing is cached
	if !ok || v == "" {
		return nil
	}
	l := strings.Split(v, ",")
	mem[key] = l
	return l
}

// optInServices are the services whose opt-in decides whether the dependents step repairs a sidecar whose parent is gone:
// its namespace parent, and only when that was never learned the services of its depends_on (plan v3.13 item 3).
func (d *Daemon) optInServices(key string, labels map[string]string) []string {
	if l := d.namespaceParents(key); len(l) > 0 {
		return l
	}
	return d.parentServices(key, labels)
}

// liveNamespace are the "kind:service" entries for the references of c (network, ipc, pid) that resolve to a container of
// its project now; refs says which kinds hold a container: reference at all.
func (d *Daemon) liveNamespace(c docker.Container) (live []string, refs map[string]bool) {
	refs = map[string]bool{}
	project := c.Labels[docker.LabelProject]
	for _, r := range []struct{ kind, mode string }{{"network", c.NetworkMode}, {"ipc", c.IpcMode}, {"pid", c.PidMode}} {
		ref := docker.ContainerRef(r.mode)
		if ref == "" {
			continue
		}
		refs[r.kind] = true
		for _, s := range d.idx.all {
			svc := s.Labels[docker.LabelService]
			if svc == "" || s.Labels[docker.LabelProject] != project || docker.IsOneOff(s.Labels) || !docker.MatchesRef(s.ID, s.Name, ref) {
				continue
			}
			if e := r.kind + ":" + svc; !slices.Contains(live, e) {
				live = append(live, e)
			}
		}
	}
	return live, refs
}

// learnParents records the services a dependent depends on. A non-empty label is authoritative and replaces what was
// stored; a live container: reference that resolves to a container of the same project adds that container's service, and
// never shrinks a set learned from the label (depends_on can name services besides the one it shares a namespace with).
func (d *Daemon) learnParents(key string, c docker.Container) {
	delete(d.parentsGone, key)
	liveNS, refs := d.liveNamespace(c)
	var live []string
	for _, e := range liveNS {
		if _, svc, _ := strings.Cut(e, ":"); !slices.Contains(live, svc) {
			live = append(live, svc)
		}
	}
	known := d.knownParents(key)
	var next []string
	if l := dependsOn(c.Labels[compose.LabelDependsOn]); len(l) > 0 {
		next = slices.Clone(l)
	} else {
		next = slices.Clone(known)
		for _, svc := range live {
			if !slices.Contains(next, svc) {
				next = append(next, svc)
			}
		}
	}
	if len(next) > 0 && !slices.Equal(next, known) {
		d.parents[key] = next
		if d.o.Meta != nil {
			d.o.Meta.SetMeta(metaParents+key, strings.Join(next, ","))
		}
	}
	// The namespace parent is what the live references say: per reference, replaced when it resolves, kept while it does
	// not (its parent is gone, which is when it matters), and dropped when the container no longer holds that reference
	// (plan v3.13 item 3).
	oldNS := d.known(d.nsParents, metaNSParent, key)
	var nextNS []string
	for _, e := range oldNS {
		kind, _, _ := strings.Cut(e, ":")
		if refs[kind] && !slices.ContainsFunc(liveNS, func(l string) bool { return strings.HasPrefix(l, kind+":") }) {
			nextNS = append(nextNS, e)
		}
	}
	nextNS = append(nextNS, liveNS...)
	slices.Sort(nextNS)
	nextNS = slices.Compact(nextNS)
	if len(nextNS) > 0 && !slices.Equal(nextNS, oldNS) {
		d.nsParents[key] = nextNS
		if d.o.Meta != nil {
			d.o.Meta.SetMeta(metaNSParent+key, strings.Join(nextNS, ","))
		}
	}
}

// forgetParents drops what is remembered about keys that have been gone from the container list for a while.
func (d *Daemon) forgetParents(now time.Time) {
	for key, since := range d.parentsGone {
		if now.Sub(since) < parentsGoneAfter {
			continue
		}
		// Only a key that had a learned set has a row to delete (WN-040); the set may live in the store alone after a restart.
		learned, learnedNS := len(d.knownParents(key)) > 0, len(d.namespaceParents(key)) > 0
		delete(d.parents, key)
		delete(d.nsParents, key)
		delete(d.parentsGone, key)
		if d.o.Meta != nil && learned {
			d.o.Meta.DeleteMeta(metaParents + key)
		}
		if d.o.Meta != nil && learnedNS {
			d.o.Meta.DeleteMeta(metaNSParent + key)
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
