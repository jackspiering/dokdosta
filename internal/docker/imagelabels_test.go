package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const flImg = "sha256:aaaa"

func filterFor(f *Fake) (*ImageLabelFilter, *[][]string) {
	var warned [][]string
	return &ImageLabelFilter{Reader: f, Warn: func(_, _ string, keys []string) { warned = append(warned, keys) }}, &warned
}

func TestFilterRemovesWhatTheImageGives(t *testing.T) {
	f := NewFake()
	f.SetImageLabels(flImg, map[string]string{"dokdosta.enable": "true", "dokdosta.cooldown": "5s", "keep": "x"})
	own := map[string]string{"dokdosta.enable": "true", "dokdosta.cooldown": "9s", "keep": "x"}
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: own})
	flt, warned := filterFor(f)
	ctx := context.Background()

	c, err := flt.Inspect(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"dokdosta.cooldown": "9s", "keep": "x"}; !reflect.DeepEqual(c.Labels, want) {
		t.Fatalf("inspect: %v", c.Labels)
	}
	sums, err := flt.List(ctx)
	if err != nil || len(sums) != 1 || !reflect.DeepEqual(sums[0].Labels, c.Labels) {
		t.Fatalf("list: %v %v", sums, err)
	}
	// The map the Reader holds is not changed.
	if raw, _ := f.Inspect(ctx, "c1"); raw.Labels["dokdosta.enable"] != "true" {
		t.Fatal("the filter changed the inner reader's labels")
	}
	if len(*warned) != 1 || !reflect.DeepEqual((*warned)[0], []string{"dokdosta.enable"}) {
		t.Fatalf("warnings: %v", *warned)
	}
}

func TestFilterTreatsAComposeIdentityOfTheImageAsNoComposeAtAll(t *testing.T) {
	f := NewFake()
	img := map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.version": "5.6.0"}
	f.SetImageLabels(flImg, img)
	own := map[string]string{LabelProject: "p", LabelService: "web", LabelNumber: "1", "com.docker.compose.version": "5.6.0", "dokdosta.enable": "true", "other": "y"}
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: own})
	flt, _ := filterFor(f)
	c, _ := flt.Inspect(context.Background(), "c1")
	if want := map[string]string{"dokdosta.enable": "true", "other": "y"}; !reflect.DeepEqual(c.Labels, want) {
		t.Fatalf("%v", c.Labels)
	}
	if _, _, _, ok := ComposeKeyParts(c.Labels); ok {
		t.Fatal("still a compose container")
	}
}

// A container compose really made keeps its identity even when the image happens to carry unrelated compose labels.
func TestFilterKeepsARealComposeIdentity(t *testing.T) {
	f := NewFake()
	f.SetImageLabels(flImg, map[string]string{LabelProject: "other"})
	own := map[string]string{LabelProject: "p", LabelService: "web"}
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: own})
	flt, _ := filterFor(f)
	c, _ := flt.Inspect(context.Background(), "c1")
	if !reflect.DeepEqual(c.Labels, own) {
		t.Fatalf("%v", c.Labels)
	}
}

func TestFilterDeclaredDecidesForAComposeContainer(t *testing.T) {
	f := NewFake()
	f.SetImageLabels(flImg, map[string]string{"dokdosta.enable": "true", "dokdosta.heal.pull": "true"})
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: map[string]string{LabelProject: "p", LabelService: "web", "dokdosta.enable": "true", "dokdosta.heal.pull": "true"}})
	flt, _ := filterFor(f)
	flt.Declared = func(context.Context, map[string]string) (map[string]bool, bool) {
		return map[string]bool{"dokdosta.enable": true}, true
	}
	c, _ := flt.Inspect(context.Background(), "c1")
	if _, ok := c.Labels["dokdosta.enable"]; !ok {
		t.Error("a declared label was removed")
	}
	if _, ok := c.Labels["dokdosta.heal.pull"]; ok {
		t.Error("an undeclared label was kept")
	}
}

func TestFilterWithoutImageInfoKeepsEverything(t *testing.T) {
	f := NewFake()
	own := map[string]string{"dokdosta.enable": "true"}
	f.Put(Container{ID: "noimage", Name: "a", Labels: own})
	f.Put(Container{ID: "gone", Name: "b", ImageID: "sha256:gone", Labels: own})
	flt, _ := filterFor(f)
	for _, id := range []string{"noimage", "gone"} {
		c, err := flt.Inspect(context.Background(), id)
		if err != nil || !reflect.DeepEqual(c.Labels, own) {
			t.Fatalf("%s: %v %v", id, c.Labels, err)
		}
	}
}

type failingImages struct {
	*Fake
	err error
}

func (f failingImages) ImageLabels(context.Context, string) (map[string]string, error) {
	return nil, f.err
}

func TestFilterListFailsWhenTheImageCannotBeReadForAnotherReason(t *testing.T) {
	f := NewFake()
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: map[string]string{"dokdosta.enable": "true"}})
	boom := errors.New("api down")
	flt := &ImageLabelFilter{Reader: failingImages{f, boom}}
	if _, err := flt.List(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
	// An event is a trigger only: it passes as it came.
	ev := Event{ID: "c1", Labels: map[string]string{"dokdosta.enable": "true"}}
	if got := flt.Event(context.Background(), ev); !reflect.DeepEqual(got.Labels, ev.Labels) {
		t.Fatal(got.Labels)
	}
}

func TestFilterEventCarriesNoImageLabel(t *testing.T) {
	f := NewFake()
	f.SetImageLabels(flImg, map[string]string{"dokdosta.enable": "true"})
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: map[string]string{"dokdosta.enable": "true"}})
	flt, _ := filterFor(f)
	// A container the filter has not listed yet (a create event) is looked up.
	got := flt.Event(context.Background(), Event{Action: "create", ID: "c1", Name: "c1", Labels: map[string]string{"dokdosta.enable": "true", "image": "x", "name": "c1"}})
	if want := map[string]string{"image": "x", "name": "c1"}; !reflect.DeepEqual(got.Labels, want) {
		t.Fatalf("%v", got.Labels)
	}
	// One that is already gone passes as it came.
	got = flt.Event(context.Background(), Event{Action: "destroy", ID: "gone", Labels: map[string]string{"dokdosta.enable": "true"}})
	if got.Labels["dokdosta.enable"] != "true" {
		t.Fatal(got.Labels)
	}
}
