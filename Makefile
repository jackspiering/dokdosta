# Conveniences; CI runs the same commands (.github/workflows/ci.yml). Nothing here publishes anything.
.PHONY: check build test vet scripts-test shellcheck snapshot verify-release

# What CI's go and scripts jobs check, in their order, without Docker: run it before every push.
# A formatting slip or an untidy go.mod found here costs seconds; found by CI it costs a full run (about 120 runner-minutes).
# go mod tidy rewrites go.mod/go.sum when they are not tidy, so commit what it changed. Not here: govulncheck and the
# pins' --verify-remote (network), actionlint (go run, CI's workflows job), and the integration tests (CI only).
check:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "needs gofmt:"; echo "$$unformatted"; exit 1; fi
	go mod tidy && git diff --exit-code go.mod go.sum
	$(MAKE) vet build
	scripts/ci/shard-integration.sh --check-ci .github/workflows/ci.yml
	$(MAKE) test scripts-test shellcheck

build:
	CGO_ENABLED=0 go build -trimpath -o dokdosta ./cmd/dokdosta

vet:
	go vet ./... && go vet -tags integration ./...

# Unit tests: they touch no Docker.
test:
	go test -race -count=1 ./...

# The self-tests of the shell tooling (no Docker, no network).
scripts-test:
	scripts/scrub-fixture --self-test
	scripts/release/release-notes.sh --self-test && scripts/release/verify.sh --self-test && scripts/ci/check-dockerfiles.sh --self-test && scripts/ci/check-dockerfiles.sh && scripts/ci/check-pins.sh --self-test && scripts/ci/check-pins.sh && scripts/ci/shard-integration.sh --self-test
	scripts/ci/changes.sh --self-test && scripts/ci/compose-latest.sh --self-test
	scripts/ci/check-workflows.sh --self-test && scripts/ci/check-workflows.sh

# CI's scripts job runs the same.
shellcheck:
	shellcheck -x $$( { grep -rIl --exclude-dir=testdata -E '^#!.*(ba)?sh' scripts test; find scripts test -name '*.sh'; } | sort -u)

# Everything the release builds, into dist/, without pushing, signing or publishing (needs goreleaser and syft).
snapshot:
	goreleaser release --snapshot --clean --skip=sign,docker,publish

# Verify a published release: the cosign signatures (pinned to this repository's release workflow), the checksums of every
# archive, and the build provenance. Needs cosign; gh is optional (without it the provenance check is skipped), but if it is
# installed it must be logged in (gh auth login, or GH_TOKEN). See docs/verifying-releases.md.
#   make verify-release TAG=v0.3.0
#   make verify-release TAG=v0.2.0 REPO=jackspiering/whalenurse   (the releases from before the rename, plan v3.15)
verify-release:
	@test -n "$(TAG)" || { echo "usage: make verify-release TAG=v0.3.0 [REPO=owner/name]" >&2; exit 2; }
	scripts/release/verify.sh $(TAG) $(if $(REPO),--repo $(REPO))
