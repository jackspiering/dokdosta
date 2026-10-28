package docker

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// Label namespaces dokdosta reads from a container.
const (
	composeLabelPrefix  = "com.docker.compose."
	dokdostaLabelPrefix = "dokdosta."
)

// ImageLabelFilter keeps the labels an image ships from steering dokdosta (plan v3.13 item 10). Docker copies an image's
// own labels (its Config.Labels) onto every container created from it, for each key the create request did not set (moby
// daemon/commit.go, merge), so a container's merged labels hold both the operator's and the image publisher's. The filter
// reads the image's labels by the container's image ID and removes, from the labels List and Inspect return:
//
//   - every com.docker.compose.* label, when one of them has the value the image carries for that key: the compose
//     identity is the image's, the container was not made by compose, and it falls back to its name and rung 1;
//   - on a compose container, every dokdosta.* label that the service's `labels:` in the compose file does not declare
//     (Declared); when the compose file cannot be read, or on any other container, every dokdosta.* label whose value is
//     the image's own value for that key.
//
// What is removed is decided once per container: its labels never change. The one decision that can change is one that
// consulted the compose file (the file is edited): it is kept for DeclaredTTL and then made again. An image that is gone
// (ErrImageNotFound), or a container with no image ID, has no image labels to compare with and keeps all of its own; the
// image is looked for again on the next read.
type ImageLabelFilter struct {
	Reader
	// Declared returns the dokdosta.* keys the compose file declares in the labels of the service of the compose
	// container with these labels. ok is false when that cannot be known (compose unavailable, the files unreadable, the
	// read failed). Nil means never known.
	Declared func(ctx context.Context, labels map[string]string) (keys map[string]bool, ok bool)
	// Warn is called once per container and set of removed keys.
	Warn func(container, reason string, keys []string)
	// Now is the clock for DeclaredTTL; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	images  map[string]map[string]string // image ID -> the labels it ships
	dropped map[string]dropped           // container ID -> the label keys removed
	warned  map[string]bool
}

// DeclaredTTL is how long a decision that read the compose file stays valid: the file can be edited after the container
// was made, in either direction.
const DeclaredTTL = 10 * time.Minute

// dropped is the keys removed from one container; until is when the decision must be made again (zero: never).
type dropped struct {
	keys  []string
	until time.Time
}

func (f *ImageLabelFilter) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// cachedDrop returns the decision kept for a container, if it is still valid.
func (f *ImageLabelFilter) cachedDrop(id string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.dropped[id]
	if !ok || (!d.until.IsZero() && !f.now().Before(d.until)) {
		return nil, false
	}
	return d.keys, true
}

var _ Reader = (*ImageLabelFilter)(nil)

// maxImagesCached bounds the cache of image labels; the images of a host are few, so a full cache is simply reset.
const maxImagesCached = 512

func hasSteering(labels map[string]string) bool {
	for k := range labels {
		if strings.HasPrefix(k, composeLabelPrefix) || strings.HasPrefix(k, dokdostaLabelPrefix) {
			return true
		}
	}
	return false
}

// imageLabels returns the labels an image ships; known is false when the image is gone.
func (f *ImageLabelFilter) imageLabels(ctx context.Context, imageID string) (labels map[string]string, known bool, err error) {
	f.mu.Lock()
	l, ok := f.images[imageID]
	f.mu.Unlock()
	if ok {
		return l, true, nil
	}
	l, err = f.Reader.ImageLabels(ctx, imageID)
	if errors.Is(err, ErrImageNotFound) {
		return nil, false, nil // not cached: it is looked for again
	}
	if err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	if f.images == nil || len(f.images) >= maxImagesCached {
		f.images = map[string]map[string]string{}
	}
	f.images[imageID] = l
	f.mu.Unlock()
	return l, true, nil
}

// resolve returns the keys to remove from the labels of one container.
func (f *ImageLabelFilter) resolve(ctx context.Context, id, name, imageID string, labels map[string]string) ([]string, error) {
	if imageID == "" || !hasSteering(labels) {
		return nil, nil
	}
	if drop, ok := f.cachedDrop(id); ok {
		return drop, nil
	}
	img, imageKnown, err := f.imageLabels(ctx, imageID)
	if err != nil {
		return nil, err
	}
	var drop []string
	var identity, steering []string
	for k, v := range labels {
		switch {
		case strings.HasPrefix(k, composeLabelPrefix):
			if iv, has := img[k]; has && iv == v {
				identity = append(identity, k)
			}
		case strings.HasPrefix(k, dokdostaLabelPrefix):
			steering = append(steering, k)
		}
	}
	slices.Sort(identity)
	slices.Sort(steering)
	definitive, consulted := imageKnown, false
	if len(identity) > 0 {
		for k := range labels {
			if strings.HasPrefix(k, composeLabelPrefix) {
				drop = append(drop, k)
			}
		}
		f.warn(name, "compose labels come from the image: the container is treated as not made by compose", identity)
	}
	isCompose := len(identity) == 0 && labels[LabelProject] != "" && labels[LabelService] != ""
	if len(steering) > 0 {
		var declared map[string]bool
		known := false
		if isCompose && f.Declared != nil {
			declared, known = f.Declared(ctx, labels)
		}
		consulted = isCompose && f.Declared != nil
		definitive = definitive && (!isCompose || known)
		var ignored []string
		for _, k := range steering {
			var from bool
			if known {
				from = !declared[k]
			} else {
				iv, has := img[k]
				from = has && iv == labels[k]
			}
			if from {
				ignored = append(ignored, k)
			}
		}
		if len(ignored) > 0 {
			drop = append(drop, ignored...)
			reason := "these labels have the value the image gives them, so they are not the operator's and are ignored"
			if known {
				reason = "the compose file does not declare these labels (they come from the image), so they are ignored"
			}
			f.warn(name, reason, ignored)
		}
	}
	slices.Sort(drop)
	if definitive {
		e := dropped{keys: drop}
		if consulted {
			e.until = f.now().Add(DeclaredTTL)
		}
		f.mu.Lock()
		if f.dropped == nil {
			f.dropped = map[string]dropped{}
		}
		f.dropped[id] = e
		f.mu.Unlock()
	}
	return drop, nil
}

func (f *ImageLabelFilter) warn(name, reason string, keys []string) {
	if f.Warn == nil {
		return
	}
	id := name + "|" + reason + "|" + strings.Join(keys, ",")
	f.mu.Lock()
	seen := f.warned[id]
	if f.warned == nil || len(f.warned) >= 4096 {
		f.warned = map[string]bool{}
	}
	f.warned[id] = true
	f.mu.Unlock()
	if !seen {
		f.Warn(name, reason, keys)
	}
}

// without returns labels minus the keys; the input map is not changed, because a Reader may hand out its own.
func without(labels map[string]string, keys []string) map[string]string {
	if len(keys) == 0 {
		return labels
	}
	out := maps.Clone(labels)
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

// List is the inner list with the image-supplied labels removed.
func (f *ImageLabelFilter) List(ctx context.Context) ([]Summary, error) {
	sums, err := f.Reader.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, len(sums))
	seen := make(map[string]bool, len(sums))
	for i, s := range sums {
		drop, err := f.resolve(ctx, s.ID, s.Name, s.ImageID, s.Labels)
		if err != nil {
			return nil, err
		}
		s.Labels = without(s.Labels, drop)
		out[i] = s
		seen[s.ID] = true
	}
	f.mu.Lock()
	for id := range f.dropped {
		if !seen[id] {
			delete(f.dropped, id)
		}
	}
	f.mu.Unlock()
	return out, nil
}

// Inspect is the inner inspect with the image-supplied labels removed.
func (f *ImageLabelFilter) Inspect(ctx context.Context, ref string) (Container, error) {
	c, err := f.Reader.Inspect(ctx, ref)
	if err != nil {
		return c, err
	}
	drop, err := f.resolve(ctx, c.ID, c.Name, c.ImageID, c.Labels)
	if err != nil {
		return Container{}, err
	}
	c.Labels = without(c.Labels, drop)
	return c, nil
}

// Event returns the event with the image-supplied labels removed from its attributes (an event carries the container's
// labels beside its own attributes). An event is only a trigger, so when the container cannot be looked up (it is
// already gone) or the image's labels cannot be read, the event is returned as it is.
func (f *ImageLabelFilter) Event(ctx context.Context, ev Event) Event {
	if !hasSteering(ev.Labels) {
		return ev
	}
	drop, ok := f.cachedDrop(ev.ID)
	if !ok {
		c, err := f.Reader.Inspect(ctx, ev.ID)
		if err != nil {
			return ev
		}
		if drop, err = f.resolve(ctx, c.ID, c.Name, c.ImageID, c.Labels); err != nil {
			return ev
		}
	}
	ev.Labels = without(ev.Labels, drop)
	return ev
}
