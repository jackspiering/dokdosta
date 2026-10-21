# Conveniences; CI runs the same commands (.github/workflows/ci.yml). Nothing here publishes anything.
.PHONY: build test vet scripts-test snapshot verify-release

build:
	CGO_ENABLED=0 go build -trimpath -o whalenurse ./cmd/whalenurse

vet:
	go vet ./... && go vet -tags integration ./...

# Unit tests: they touch no Docker.
test:
	go test -race -count=1 ./...

# The self-tests of the shell tooling (no Docker, no network).
scripts-test:
	scripts/m0/selftest.sh && scripts/scrub-fixture --self-test && test/m0/run.sh selftest && test/m0/m0b/selftest.sh
	scripts/release/release-notes.sh --self-test && scripts/release/verify.sh --self-test && scripts/ci/check-dockerfiles.sh --self-test && scripts/ci/check-dockerfiles.sh && scripts/ci/check-pins.sh --self-test && scripts/ci/check-pins.sh && scripts/ci/shard-integration.sh --self-test
	scripts/ci/changes.sh --self-test && scripts/ci/compose-latest.sh --self-test
	scripts/ci/check-workflows.sh --self-test && scripts/ci/check-workflows.sh

# Everything the release builds, into dist/, without pushing, signing or publishing (needs goreleaser and syft).
snapshot:
	goreleaser release --snapshot --clean --skip=sign,docker,publish

# Verify a published release: the cosign signatures (pinned to this repository's release workflow), the checksums of every
# archive, and the build provenance. Needs cosign; gh is optional. See docs/verifying-releases.md.
#   make verify-release TAG=v0.1.0
verify-release:
	@test -n "$(TAG)" || { echo "usage: make verify-release TAG=v0.1.0" >&2; exit 2; }
	scripts/release/verify.sh $(TAG)
