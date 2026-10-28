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

// Compose writes project, service and version into every image it builds, so a container compose makes from such an image
// carries the same three values: that is not the image's identity (plan v3.16 item 1). A one-off container (`compose run`)
// is created with number -1, for which compose writes no container-number (compose pkg/compose/create.go and run.go,
// v5.2.0 and v5.6.1), so the realistic fixture of a one-off has config-hash and oneoff=True and no container-number.
func composeBuiltImage(oneoff bool) (img, own map[string]string) {
	img = map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.version": "5.6.0"}
	own = map[string]string{
		LabelProject: "p", LabelService: "web", "com.docker.compose.version": "5.6.0",
		"com.docker.compose.config-hash": "abc", LabelOneOff: "False",
		"com.docker.compose.project.working_dir": "/srv/p", "com.docker.compose.project.config_files": "/srv/p/compose.yaml",
		"dokdosta.enable": "true",
	}
	if oneoff {
		own[LabelOneOff] = "True"
	} else {
		own[LabelNumber] = "1"
	}
	return img, own
}

func inspectFiltered(t *testing.T, img, own map[string]string) map[string]string {
	t.Helper()
	f := NewFake()
	f.SetImageLabels(flImg, img)
	f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: own})
	flt, _ := filterFor(f)
	flt.Declared = func(context.Context, map[string]string) (map[string]bool, bool) {
		return map[string]bool{"dokdosta.enable": true}, true
	}
	c, err := flt.Inspect(context.Background(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	return c.Labels
}

func TestFilterKeepsTheComposeIdentityOfAContainerComposeCreatedFromAComposeBuiltImage(t *testing.T) {
	img, own := composeBuiltImage(false)
	got := inspectFiltered(t, img, own)
	if !reflect.DeepEqual(got, own) {
		t.Fatalf("%v", got)
	}
	if _, _, _, ok := ComposeKeyParts(got); !ok {
		t.Fatal("no longer a compose container")
	}
}

// A real `docker compose run` container from a compose-built image has no container-number and keeps oneoff=True, which
// is what rule 0 (SkipOneOff) reads.
func TestFilterKeepsOneOffLabel(t *testing.T) {
	img, own := composeBuiltImage(true)
	if _, has := own[LabelNumber]; has {
		t.Fatal("the fixture of a one-off must not carry a container-number")
	}
	got := inspectFiltered(t, img, own)
	if !reflect.DeepEqual(got, own) {
		t.Fatalf("%v", got)
	}
	if !IsOneOff(got) {
		t.Fatalf("the oneoff label is gone: %v", got)
	}
}

// compose writes project.environment_file only when --env-file was given, so an image that carries it alone, under a
// container compose made without env files, is the image's label: it must not reach compose's --env-file.
func TestFilterDropsAnEnvironmentFileOnlyTheImagePlants(t *testing.T) {
	for _, oneoff := range []bool{false, true} {
		img, own := composeBuiltImage(oneoff)
		const envFile = "com.docker.compose.project.environment_file"
		img[envFile] = "/x"
		own[envFile] = "/x"
		got := inspectFiltered(t, img, own)
		if want := map[string]string{"dokdosta.enable": "true"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("one-off %v: %v", oneoff, got)
		}
		if _, _, _, ok := ComposeKeyParts(got); ok {
			t.Fatal("still a compose container")
		}
	}
}

// An identity of the image is still recognised when compose cannot have made the container, or the image holds a value
// compose writes at create time.
func TestFilterStillDropsAnIdentityOfTheImage(t *testing.T) {
	for name, tc := range map[string]struct{ img, own map[string]string }{
		"docker run of an image that plants project and service": {
			img: map[string]string{LabelProject: "p", LabelService: "web"},
			own: map[string]string{LabelProject: "p", LabelService: "web", "dokdosta.enable": "true", "other": "y"},
		},
		"no container-number on a container that is not a one-off": {
			img: map[string]string{LabelProject: "p", LabelService: "web"},
			own: map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.config-hash": "abc", LabelOneOff: "False", "dokdosta.enable": "true", "other": "y"},
		},
		"config-hash equal to the image's": {
			img: map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.config-hash": "abc"},
			own: map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.config-hash": "abc", LabelNumber: "1", LabelOneOff: "False", "dokdosta.enable": "true", "other": "y"},
		},
		"project.config_files equal to the image's": {
			img: map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.project.config_files": "/srv/p/compose.yaml"},
			own: map[string]string{LabelProject: "p", LabelService: "web", "com.docker.compose.config-hash": "abc", LabelNumber: "1", LabelOneOff: "False", "com.docker.compose.project.config_files": "/srv/p/compose.yaml", "dokdosta.enable": "true", "other": "y"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := NewFake()
			f.SetImageLabels(flImg, tc.img)
			f.Put(Container{ID: "c1", Name: "c1", ImageID: flImg, Labels: tc.own})
			flt, _ := filterFor(f)
			c, _ := flt.Inspect(context.Background(), "c1")
			if want := map[string]string{"dokdosta.enable": "true", "other": "y"}; !reflect.DeepEqual(c.Labels, want) {
				t.Fatalf("%v", c.Labels)
			}
			if _, _, _, ok := ComposeKeyParts(c.Labels); ok {
				t.Fatal("still a compose container")
			}
		})
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
