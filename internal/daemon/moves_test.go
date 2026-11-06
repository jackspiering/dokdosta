package daemon

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeResolver map[string]string // "ref|arch" -> ID

func (f fakeResolver) ImageID(_ context.Context, ref, arch string) (string, error) {
	if id, ok := f[ref+"|"+arch]; ok {
		return id, nil
	}
	return "", errors.New("no such image")
}

// The ledger through its own interface: a record survives a restart (read back lazily from the meta, under any spelling of
// the reference), and stands only while the tag still resolves to its result, for the platform or without one.
func TestMoveLedgerPersistsAndStandsOnlyWhileTheTagResolvesToIt(t *testing.T) {
	m := newListingMeta()
	newMoveLedger(m).record(tagMove{Ref: normalizeRef("nginx"), Key: "h/p/web/1", At: time.Unix(1_000_000, 0).UTC(), New: "sha256:new", NewPlain: "sha256:index"})

	l := newMoveLedger(m)
	if mv, ok := l.of("docker.io/library/nginx:latest"); !ok || mv.Key != "h/p/web/1" {
		t.Fatalf("not read back: %+v %v", mv, ok)
	}
	ctx := context.Background()
	if _, ok := l.standing(ctx, fakeResolver{"nginx|amd64": "sha256:new"}, "nginx", "amd64"); !ok {
		t.Error("the platform's ID is the move's result")
	}
	if _, ok := l.standing(ctx, fakeResolver{"nginx|amd64": "sha256:x", "nginx|": "sha256:index"}, "nginx", "amd64"); !ok {
		t.Error("the plain ID is the move's result (containerd store)")
	}
	if _, ok := l.standing(ctx, fakeResolver{"nginx|amd64": "sha256:newer", "nginx|": "sha256:newer"}, "nginx", "amd64"); ok {
		t.Error("the tag moved again: the record no longer stands")
	}
	if _, ok := l.standing(ctx, fakeResolver{}, "nginx", "amd64"); ok {
		t.Error("an image that cannot be resolved is no answer")
	}
}
