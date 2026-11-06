package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Drift checks (plan §3.6).
const (
	CheckConfig     = "config"     // compose would recreate the container: the compose file, an env file, or the image changed
	CheckImage      = "image"      // the image behind the reference is not the one the container was created from
	CheckSkew       = "skew"       // the bundled compose differs from the one that made the container, so drift cannot be told from a hash change
	CheckUnverified = "unverified" // a check could not run
)

// Finding is one reason the recreate should not go ahead. Cause is the text people read; what the notifications and the
// dashboard decide on is the structure (Check, Upgrade), carried to them by Attrs, never parsed back from Cause.
type Finding struct {
	Check string
	Cause string
	// Upgrade is set on a config finding when the compose in use is a later release than the one that made the container
	// (the host plugin was upgraded): the change may be the hash layout rather than an edit.
	Upgrade *Upgrade
}

// Upgrade is a compose release change since the container was created.
type Upgrade struct{ From, To string }

func (f Finding) String() string { return f.Check + ": " + f.Cause }

// Verified reports whether the finding is real drift that was established (config or image). The others (a check that
// could not be made, a version skew that makes drift unverifiable) are "can't verify": DOKDOSTA_DRIFT_GUARD=warn does
// not override them (plan v3.6).
func (f Finding) Verified() bool { return f.Check == CheckConfig || f.Check == CheckImage }

// Overridable reports whether DOKDOSTA_DRIFT_GUARD=warn lets an action go ahead despite these findings: only when every
// one is verified drift (plan v3.6). None is not a question warn answers.
func Overridable(fs []Finding) bool {
	for _, f := range fs {
		if !f.Verified() {
			return false
		}
	}
	return len(fs) > 0
}

// Attribute keys of the guard's structured facts on a DriftBlocked, DriftWarned or Suspended event.
const (
	AttrChecks      = "checks"       // the checks that found something, in order, comma-separated: config,image,skew,unverified
	AttrComposeFrom = "compose_from" // a config finding's compose upgrade: the release that made the container
	AttrComposeTo   = "compose_to"   // and the one in use
)

// Attrs returns the findings' structure as event attributes, so a reader decides on it rather than on the wording.
func Attrs(fs []Finding) map[string]string {
	if len(fs) == 0 {
		return nil
	}
	var checks []string
	seen := map[string]bool{}
	out := map[string]string{}
	for _, f := range fs {
		if !seen[f.Check] {
			seen[f.Check] = true
			checks = append(checks, f.Check)
		}
		if f.Upgrade != nil && out[AttrComposeFrom] == "" {
			out[AttrComposeFrom], out[AttrComposeTo] = f.Upgrade.From, f.Upgrade.To
		}
	}
	out[AttrChecks] = strings.Join(checks, ",")
	return out
}

// ImageResolver returns what an image reference resolves to for linux/<arch> (internal/docker implements it).
type ImageResolver interface {
	ImageID(ctx context.Context, ref, arch string) (string, error)
}

// GuardInput describes the container about to be recreated.
type GuardInput struct {
	Project       Project
	ContainerName string
	Labels        map[string]string
	ImageRef      string // Config.Image
	Arch          string // the Engine's, as `docker version --format '{{.Server.Arch}}'`
	// RefHolder: network_mode, ipc, pid or volumes_from names another service or container. Compose hashes those after
	// resolving them, so neither the oracle nor `config --hash` can be trusted for it (E4, E5) and check 1 is skipped.
	RefHolder bool
	// RefsCurrent: on the holder's own failure, every container it references is the current, running container of its
	// service. Compose's resolved hash then matches the one it recorded, so the dry-run oracle can be trusted for a
	// reference holder too: a Recreate means config drift (plan v3.13 item 4). The hash stays skipped (E4: it hashes the
	// unresolved form), and so does the oracle in image-move mode, where a Recreate cannot be told from a moved tag.
	RefsCurrent bool
	// AllowImageMove: the check before rung 3's pull, before rung 2's recreate of a container that opted in to the pull
	// rung, and before a sidecar takes a tag its parent's pull just moved (plan §3.7, v3.8). Checks 1 and 3 run; check 2
	// is not a finding, because the caller deploys the moved image on purpose. Compose's dry-run says "Recreate" for a
	// moved tag as well and cannot say why, so a Recreate is a config finding unless `config --hash` still matches the
	// label and the explicit comparison shows the image moved: then it is the image alone. Config drift still blocks.
	AllowImageMove bool
}

// Guard runs the three drift checks of plan §3.6 before a recreate.
type Guard struct {
	Compose Reader
	Images  ImageResolver
}

// Check returns the findings; none means the recreate may go ahead. It never changes anything.
func (g Guard) Check(ctx context.Context, in GuardInput) []Finding {
	if in.AllowImageMove {
		return g.checkBeforePull(ctx, in)
	}
	var out []Finding

	// Check 1: config drift. The oracle is compose's own decision and covers config and image drift together;
	// `config --hash` is the fallback when its output cannot be read. Skipped for reference holders.
	var configDrift string
	oracleAnswered := false
	explicitImage := in.RefHolder // where check 1 cannot be used, the explicit image comparison is the check
	if !in.RefHolder {
		var f *Finding
		configDrift, f, explicitImage = g.configCheck(ctx, in)
		if f != nil {
			out = append(out, *f)
		}
	} else if in.RefsCurrent && g.trustsOracleForRefHolders() {
		configDrift, oracleAnswered = g.refHolderOracle(ctx, in)
	}

	// Check 2: image drift, the explicit comparison. Mandatory where the oracle is not used or not trusted; when the
	// oracle found drift it only says why.
	if explicitImage || configDrift != "" {
		f, drift := g.imageCheck(ctx, in)
		switch {
		case f != nil && explicitImage:
			out = append(out, *f)
		case drift != "":
			out = append(out, Finding{Check: CheckImage, Cause: drift})
			configDrift = "" // the image finding explains compose's decision
		}
	}

	// Check 3: version skew, only for the bundled compose. With another compose release than the one that made the
	// container, a change compose sees may be the hash layout that moved (v5.5.0 changed image-digest reconciliation,
	// v5.6.0 pinned the hash byte layout) rather than the config, so say that drift cannot be verified.
	if configDrift != "" {
		if msg := g.skew(in); msg != "" {
			out = append(out, Finding{Check: CheckSkew, Cause: msg})
		} else {
			out = append(out, g.configFinding(in, configDrift))
		}
	}
	if !oracleAnswered {
		// A reference holder whose check 1 was not made: check 3 stands alone for it. Once the oracle has answered, skew
		// qualifies its Recreate as it does for any service, and an answer of "nothing to change" is not second-guessed.
		out = append(out, g.refHolderSkew(in)...)
	}
	return out
}

// refHolderOracle is check 1 for a reference holder whose references are current: the dry-run oracle only. It returns the
// drift message (empty for none) and whether the oracle answered. An oracle that fails or cannot be read leaves the check
// unmade, as it has always been for a reference holder (no `config --hash`).
func (g Guard) refHolderOracle(ctx context.Context, in GuardInput) (drift string, answered bool) {
	o, err := g.Compose.DryRunUp(ctx, in.Project, in.ContainerName)
	if err != nil {
		return "", false
	}
	if !o.WouldRecreate() {
		return "", true
	}
	return fmt.Sprintf("compose would recreate this container (its dry-run says: %s): the compose file, an env file or the image changed since it was created", strings.Join(o.Texts(), ", ")), true
}

// refHolderOracleExcluded lists the compose releases for which the CI matrix showed that the oracle does not agree for a
// reference holder with current references (it says Recreate for an unchanged one, or none after an edit). The CI matrix
// decides (TestOracleForAReferenceHolderWithCurrentReferences): a release for which it fails is added here with the reason
// in docs/m2b-notes.md (v3.13 item 4). For a release listed here the check stays skipped.
//
// 5.2.0: the CI run of the test showed a Recreate for an unchanged holder of every kind (network_mode, ipc, pid,
// volumes_from) with the parent current, on both image stores. 5.6.0 and the latest release (5.6.1) agreed for all four.
var refHolderOracleExcluded = map[string]bool{"5.2.0": true}

// RefHolderOracleExcluded reports whether the check stays skipped for this compose release (the integration test reads it).
func RefHolderOracleExcluded(v Version) bool { return refHolderOracleExcluded[v.String()] }

// trustsOracleForRefHolders reports whether the compose in use is one for which the oracle may be asked about a reference
// holder: a known release that is not excluded.
func (g Guard) trustsOracleForRefHolders() bool {
	v := g.Compose.Binary().Version
	return !v.IsZero() && !RefHolderOracleExcluded(v)
}

// refHolderSkew is check 3 for a reference holder, which has no check 1 to qualify it: the recreate always happens (the
// parent it resolves changed, or its own rung 2 forces one), so a container made by another compose release than the
// bundled one cannot be verified (plan §3.6 check 3; m2b-notes decision 7).
func (g Guard) refHolderSkew(in GuardInput) []Finding {
	if !in.RefHolder {
		return nil
	}
	if msg := g.skew(in); msg != "" {
		return []Finding{{Check: CheckSkew, Cause: msg}}
	}
	return nil
}

// checkBeforePull is Check for rung 3: the config drift check and the version-skew check, not the image check.
func (g Guard) checkBeforePull(ctx context.Context, in GuardInput) []Finding {
	var out []Finding
	var configDrift string
	if !in.RefHolder {
		var f *Finding
		configDrift, f, _ = g.configCheck(ctx, in)
		if f != nil {
			out = append(out, *f)
		}
		if configDrift != "" && g.onlyTheImageMoved(ctx, in) {
			configDrift = ""
		}
	}
	if configDrift != "" {
		if msg := g.skew(in); msg != "" {
			out = append(out, Finding{Check: CheckSkew, Cause: msg})
		} else {
			out = append(out, g.configFinding(in, configDrift))
		}
	}
	out = append(out, g.refHolderSkew(in)...)
	return out
}

// onlyTheImageMoved reports whether the compose config is unchanged (its hash equals the label) while the image the
// reference resolves to is not the one the container was created from. Anything it cannot establish is "no".
func (g Guard) onlyTheImageMoved(ctx context.Context, in GuardInput) bool {
	f, drift := g.imageCheck(ctx, in)
	if f != nil || drift == "" {
		return false
	}
	want := in.Labels[LabelConfigHash]
	hash, err := g.Compose.ConfigHash(ctx, in.Project)
	return err == nil && want != "" && hash == want
}

// configCheck is check 1. It returns the drift message (empty for none), a finding when the check could not be made, and
// whether the explicit image comparison is now required (the hash fallback covers config only).
func (g Guard) configCheck(ctx context.Context, in GuardInput) (drift string, f *Finding, needImage bool) {
	o, err := g.Compose.DryRunUp(ctx, in.Project, in.ContainerName)
	if err == nil {
		if o.WouldRecreate() {
			return fmt.Sprintf("compose would recreate this container (its dry-run says: %s): the compose file, an env file or the image changed since it was created", strings.Join(o.Texts(), ", ")), nil, false
		}
		return "", nil, false
	}
	if !errors.Is(err, ErrUnparseable) {
		err = fmt.Errorf("dry-run failed: %w", err)
	}
	hash, herr := g.Compose.ConfigHash(ctx, in.Project)
	want := in.Labels[LabelConfigHash]
	switch {
	case herr != nil:
		return "", &Finding{Check: CheckUnverified, Cause: fmt.Sprintf("cannot check the compose config for drift: %v; config --hash: %v", err, herr)}, true
	case want == "":
		return "", &Finding{Check: CheckUnverified, Cause: "cannot check the compose config for drift: the container has no " + LabelConfigHash + " label"}, true
	case hash != want:
		return fmt.Sprintf("config --hash is %s, the container was created with %s: the compose file or an env file changed", hash, want), nil, true
	}
	return "", nil, true
}

// skew reports the version-skew message, or "" when the check does not apply.
func (g Guard) skew(in GuardInput) string {
	b := g.Compose.Binary()
	if b.Source != SourceBundled {
		return ""
	}
	have := strings.TrimSpace(in.Labels[LabelVersion])
	if have == "" || SameRelease(have, b.Version) {
		return ""
	}
	if in.RefHolder {
		return fmt.Sprintf("can't verify drift (compose %s on host vs %s in dokdosta): the container was made by another compose release than the bundled one, and its own compose config is not checked", have, b.Version)
	}
	return fmt.Sprintf("can't verify drift (compose %s on host vs %s in dokdosta): the container was made by another compose release than the bundled one, and compose sees a change", have, b.Version)
}

// configFinding is the config-drift finding, with the upgrade note when the compose in use is not the release that made
// the container (the host plugin was upgraded since): the change may be the hash layout rather than an edit. The note is
// not a block of its own (plan v3.6); the bundled compose is handled by skew.
func (g Guard) configFinding(in GuardInput, drift string) Finding {
	f := Finding{Check: CheckConfig, Cause: drift}
	b := g.Compose.Binary()
	have := strings.TrimSpace(in.Labels[LabelVersion])
	if have == "" || b.Version.IsZero() || SameRelease(have, b.Version) {
		return f
	}
	v, _ := ParseVersion(have)
	f.Upgrade = &Upgrade{From: v.String(), To: b.Version.String()}
	f.Cause += fmt.Sprintf(" (compose upgraded from %s to %s since this container was created, so this may be a changed hash layout rather than an edit)", v, b.Version)
	return f
}

// imageCheck compares the container's com.docker.compose.image label with what its image reference resolves to now. It
// returns a finding when the comparison cannot be made, or the drift message when the tag moved.
func (g Guard) imageCheck(ctx context.Context, in GuardInput) (*Finding, string) {
	want := in.Labels[LabelImage]
	switch {
	case in.ImageRef == "":
		return &Finding{Check: CheckUnverified, Cause: "cannot check the image for drift: the container has no image reference"}, ""
	case want == "":
		return &Finding{Check: CheckUnverified, Cause: "cannot check the image for drift: the container has no " + LabelImage + " label"}, ""
	}
	got, err := g.Images.ImageID(ctx, in.ImageRef, in.Arch)
	if err != nil {
		// A reference that is gone locally is a failure whatever the cause: the recreate would need a pull.
		return &Finding{Check: CheckUnverified, Cause: fmt.Sprintf("image %s cannot be resolved for linux/%s: %v", in.ImageRef, in.Arch, err)}, ""
	}
	if got == want {
		return nil, ""
	}
	// Compose before v5.5.0 recorded the image (index) ID in the label, not the platform manifest digest, on the containerd
	// store (found by the v5.2.0 leg of the CI matrix, plan v3.6). Either is the image the container was created from; a
	// moved tag changes both, so accepting the plain ID too hides no drift.
	if plain, err := g.Images.ImageID(ctx, in.ImageRef, ""); err == nil && plain == want {
		return nil, ""
	}
	return nil, fmt.Sprintf("image %s now resolves to %s, but this container was created from %s: the tag moved", in.ImageRef, short(got), short(want))
}

func short(s string) string {
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
