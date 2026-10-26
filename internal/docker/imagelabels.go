package docker

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Label namespaces whalenurse reads from a container.
const (
	composeLabelPrefix    = "com.docker.compose."
	whalenurseLabelPrefix = "whalenurse."
)

// ImageLabelFilter keeps the labels an image ships from steering whalenurse (plan v3.13 item 10). Docker copies an image's
// own labels (its Config.Labels) onto every container created from it, for each key the create request did not set (moby
// daemon/commit.go, merge), so a container's merged labels hold both the operator's and the image publisher's. The filter
// reads the image's labels by the container's image ID and removes, from the labels List and Inspect return:
//
//   - every com.docker.compose.* label, when one of them has the value the image carries for that key: the compose
//     identity is the image's, the container was not made by compose, and it falls back to its name and rung 1;
//   - on a compose container, every whalenurse.* label that the service's `labels:` in the compose file does not declare
//     (Declared); when the compose file cannot be read, or on any other container, every whalenurse.* label whose value is
//     the image's own value for that key.
//
// What is removed is decided once per container: its labels never change. An image that is gone (ErrImageNotFound), or a
// container with no image ID, has no image labels to compare with and keeps all of its own.
type ImageLabelFilter struct {
	Reader
	// Declared returns the whalenurse.* keys the compose file declares in the labels of the service of the compose
	// container with these labels. ok is false when that cannot be known (compose unavailable, the files unreadable, the
	// read failed). Nil means never known.
	Declared func(ctx context.Context, labels map[string]string) (keys map[string]bool, ok bool)
	// Warn is called once per container and set of removed keys.
	Warn func(container, reason string, keys []string)

	mu      sync.Mutex
	images  map[string]map[string]string // image ID -> the labels it ships
	dropped map[string][]string          // container ID -> the label keys removed
	warned  map[string]bool
}

var _ Reader = (*ImageLabelFilter)(nil)

// maxImagesCached bounds the cache of image labels; the images of a host are few, so a full cache is simply reset.
const maxImagesCached = 512

func hasSteering(labels map[string]string) bool {
	for k := range labels {
		if strings.HasPrefix(k, composeLabelPrefix) || strings.HasPrefix(k, whalenurseLabelPrefix) {
			return true
		}
	}
	return false
}

func (f *ImageLabelFilter) imageLabels(ctx context.Context, imageID string) (map[string]string, error) {
	f.mu.Lock()
	l, ok := f.images[imageID]
	f.mu.Unlock()
	if ok {
		return l, nil
	}
	l, err := f.Reader.ImageLabels(ctx, imageID)
	if errors.Is(err, ErrImageNotFound) {
		return nil, nil // not cached: the image may come back
	}
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.images == nil || len(f.images) >= maxImagesCached {
		f.images = map[string]map[string]string{}
	}
	f.images[imageID] = l
	f.mu.Unlock()
	return l, nil
}

// resolve returns the keys to remove from the labels of one container.
func (f *ImageLabelFilter) resolve(ctx context.Context, id, name, imageID string, labels map[string]string) ([]string, error) {
	if imageID == "" || !hasSteering(labels) {
		return nil, nil
	}
	f.mu.Lock()
	drop, ok := f.dropped[id]
	f.mu.Unlock()
	if ok {
		return drop, nil
	}
	img, err := f.imageLabels(ctx, imageID)
	if err != nil {
		return nil, err
	}
	var identity, steering []string
	for k, v := range labels {
		switch {
		case strings.HasPrefix(k, composeLabelPrefix):
			if iv, has := img[k]; has && iv == v {
				identity = append(identity, k)
			}
		case strings.HasPrefix(k, whalenurseLabelPrefix):
			steering = append(steering, k)
		}
	}
	slices.Sort(identity)
	slices.Sort(steering)
	definitive := true
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
		definitive = !isCompose || known
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
		f.mu.Lock()
		if f.dropped == nil {
			f.dropped = map[string][]string{}
		}
		f.dropped[id] = drop
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
	if f.warned == nil {
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
	f.mu.Lock()
	drop, ok := f.dropped[ev.ID]
	f.mu.Unlock()
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
