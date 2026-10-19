# Verifying a release

Every release of whalenurse is built by one workflow, [`release.yml`](../.github/workflows/release.yml), from a `v*` tag on `main`.
That workflow signs what it publishes **keylessly** (cosign with the run's GitHub OIDC identity, recorded in the public
Sigstore transparency log) and records **build provenance** (GitHub artifact attestations). There is no signing key to steal or
to lose: what you check is that the signature was made by *that workflow, for that tag, in this repository*.

| What | Signed / attested by | Where |
|---|---|---|
| `checksums.txt` (sha256 of every archive and SBOM) | cosign: `checksums.txt.sigstore.json` (a bundle: signature, certificate, log entry) | the GitHub release |
| the archives `whalenurse_<version>_linux_{amd64,arm64}.tar.gz` | build provenance (`gh attestation`), and the checksums above | the GitHub release |
| the image `ghcr.io/jackspiering/whalenurse:<tag>` (the multi-arch index) | cosign signature in the registry; build provenance | GHCR |
| SBOMs | archives: SPDX JSON by syft, `*.sbom.spdx.json`, listed in the signed checksums. Image: the BuildKit SBOM attestation in the image index | the release / GHCR |

## One command

You need [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) (v2 or later; releases are signed with v3) and
`sha256sum`; the [GitHub CLI](https://cli.github.com/) is optional, and without it the script says that the provenance check
was skipped.

```
git clone https://github.com/jackspiering/whalenurse && cd whalenurse
make verify-release TAG=v0.1.0
# or: scripts/release/verify.sh v0.1.0 [--skip-image] [--dir where-to-keep-the-files]
```

It downloads `checksums.txt` and its bundle, and checks, in this order, stopping where it must:

1. **the signature of `checksums.txt`**, pinned to the identity
   `https://github.com/jackspiering/whalenurse/.github/workflows/release.yml@refs/tags/<tag>` and the issuer
   `https://token.actions.githubusercontent.com`. The identity is exact, not a pattern: a signature from another workflow, another
   tag or a fork is rejected.
2. **every file listed** in the signed checksums (downloaded, then `sha256sum --check --strict`: nothing is skipped, a missing
   file is an error).
3. **the image's signature**, pinned to the same identity.
4. **the build provenance** of both archives and the image (`gh attestation verify`), pinned to the release workflow.

It prints `VERIFIED v0.1.0` and exits 0, or `NOT VERIFIED` and exits non-zero. Its own tests run in CI with stubbed tools, so the
paths that must fail (a tampered archive, a bad signature) are exercised on every change. The release workflow runs the same
script against what it has just published.

## By hand

```
TAG=v0.1.0
ID="https://github.com/jackspiering/whalenurse/.github/workflows/release.yml@refs/tags/$TAG"
ISSUER=https://token.actions.githubusercontent.com

# the archives
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity "$ID" --certificate-oidc-issuer "$ISSUER" checksums.txt
sha256sum --check --ignore-missing checksums.txt          # the archives you downloaded
gh attestation verify whalenurse_0.1.0_linux_amd64.tar.gz --repo jackspiering/whalenurse

# the image
cosign verify ghcr.io/jackspiering/whalenurse:$TAG \
  --certificate-identity "$ID" --certificate-oidc-issuer "$ISSUER"
gh attestation verify oci://ghcr.io/jackspiering/whalenurse:$TAG --repo jackspiering/whalenurse
```

A tag can be moved; a digest cannot. Resolve the digest you verified and run exactly that:

```
docker buildx imagetools inspect ghcr.io/jackspiering/whalenurse:$TAG      # Digest: sha256:...
cosign verify ghcr.io/jackspiering/whalenurse@sha256:... --certificate-identity "$ID" --certificate-oidc-issuer "$ISSUER"
```
then use `image: ghcr.io/jackspiering/whalenurse:v0.1.0@sha256:...` in your compose file.

To read the image's SBOM:
`docker buildx imagetools inspect ghcr.io/jackspiering/whalenurse:$TAG --format '{{ json .SBOM }}'`.

## What this proves, and what it does not

It proves that the files are the ones the repository's release workflow produced for that tag, unaltered since. It does not
prove the code is free of bugs or that the workflow was not itself changed maliciously: read the workflow and the tag's
commit if you need that, and note that `main` is protected and release tags are restricted to the maintainer
([releasing.md](releasing.md)).
