#!/usr/bin/env bash
# Verify a whalenurse release the way docs/verifying-releases.md describes: downloads the release's checksums file, its cosign
# bundle and its archives, then checks
#   1. the checksums file's keyless cosign signature, pinned to this repo's release workflow run for this tag,
#   2. every archive against the signed checksums (sha256sum -c, nothing skipped),
#   3. the image's keyless cosign signature, pinned the same way,
#   4. the GitHub build-provenance attestations of the archives and the image (needs the gh CLI; skipped, and said so, without it).
#   scripts/release/verify.sh v0.1.0 [--repo owner/name] [--dir DIR] [--skip-image] [--no-download]
#   scripts/release/verify.sh --self-test
# --dir keeps the downloaded files (default: a temp dir); --no-download checks the files already in --dir. Exit 0 only if
# nothing failed. It needs cosign v3 or later (releases are signed with v3: .sigstore.json bundles, image signatures as OCI referrers), sha256sum and curl.
# A && ok || bad is the test idiom in the self-test (ok and bad never fail), as in the other self-tests.
# shellcheck disable=SC2015
set -euo pipefail

ISSUER=https://token.actions.githubusercontent.com

verify() {
  local tag="" repo=jackspiering/whalenurse dir="" skip_image=0 download=1
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo) repo="${2:?--repo needs owner/name}"; shift 2 ;;
      --dir) dir="${2:?--dir needs a path}"; shift 2 ;;
      --skip-image) skip_image=1; shift ;;
      --no-download) download=0; shift ;;
      -h | --help) sed -n '2,13p' "${BASH_SOURCE[0]}"; return 0 ;;
      -*) echo "verify: unknown option $1" >&2; return 2 ;;
      *) [[ -z "$tag" ]] || { echo "verify: one tag only" >&2; return 2; }; tag="$1"; shift ;;
    esac
  done
  [[ -n "$tag" ]] || { echo "usage: $0 <tag> [--repo owner/name] [--dir DIR] [--skip-image] [--no-download]" >&2; return 2; }
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "verify: '$tag' is not a release tag like v0.1.0" >&2; return 2; }
  [[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "verify: '$repo' is not owner/name" >&2; return 2; }
  local need
  for need in cosign sha256sum; do command -v "$need" >/dev/null || { echo "verify: $need is required" >&2; return 2; }; done
  [[ $download -eq 0 ]] || command -v curl >/dev/null || { echo "verify: curl is required" >&2; return 2; }

  # The only identity that may have signed a release: this repo's release workflow, run for this tag. Not a regexp.
  local identity="https://github.com/$repo/.github/workflows/release.yml@refs/tags/$tag"
  local image="ghcr.io/${repo,,}"
  local base="https://github.com/$repo/releases/download/$tag"
  if [[ -z "$dir" ]]; then dir=$(mktemp -d); fi
  mkdir -p "$dir"
  echo "release $tag of $repo; files in $dir"
  echo "signer: $identity"

  local failed=0 skipped=()
  fail() { echo "FAIL $1"; failed=1; }
  pass() { echo "ok   $1"; }

  if [[ $download -eq 1 ]]; then
    for f in checksums.txt checksums.txt.sigstore.json; do
      curl -fsSL -o "$dir/$f" "$base/$f" || { fail "download $f"; return 1; }
    done
  fi
  [[ -s "$dir/checksums.txt" && -s "$dir/checksums.txt.sigstore.json" ]] || { fail "checksums.txt and checksums.txt.sigstore.json are needed in $dir"; return 1; }

  # 1. the signature, before anything in the file is trusted
  if cosign verify-blob --bundle "$dir/checksums.txt.sigstore.json" \
    --certificate-identity "$identity" --certificate-oidc-issuer "$ISSUER" "$dir/checksums.txt" >/dev/null 2>&1; then
    pass "checksums.txt is signed by $identity"
  else
    fail "cosign verify-blob rejected checksums.txt (not signed by $identity, or altered)"
    return 1
  fi

  # 2. every file the signed list names must be there and match (no --ignore-missing: nothing is skipped)
  local name
  while read -r _ name; do
    name="${name#\*}"
    [[ -n "$name" ]] || continue
    [[ "$name" != */* && "$name" != .* ]] || { fail "checksums.txt names '$name', which is not a plain file name"; return 1; }
    if [[ $download -eq 1 ]]; then curl -fsSL -o "$dir/$name" "$base/$name" || { fail "download $name"; return 1; }; fi
  done <"$dir/checksums.txt"
  if (cd "$dir" && sha256sum --check --strict checksums.txt >/dev/null 2>&1); then
    pass "every file in checksums.txt matches ($(grep -c . "$dir/checksums.txt") files)"
  else
    fail "sha256sum rejects a file listed in checksums.txt:"
    (cd "$dir" && sha256sum --check checksums.txt 2>&1 | grep -v ': OK$' || true)
  fi

  # 3. the image
  if [[ $skip_image -eq 0 ]]; then
    if cosign verify "$image:$tag" --certificate-identity "$identity" --certificate-oidc-issuer "$ISSUER" >/dev/null 2>&1; then
      pass "$image:$tag is signed by $identity"
    else
      fail "cosign verify rejected $image:$tag"
    fi
  else
    skipped+=("image signature (--skip-image)")
  fi

  # 4. provenance
  if command -v gh >/dev/null; then
    local archive
    for archive in "$dir"/*.tar.gz; do
      [[ -e "$archive" ]] || continue
      if gh attestation verify "$archive" --repo "$repo" --signer-workflow "$repo/.github/workflows/release.yml" >/dev/null 2>&1; then
        pass "build provenance of $(basename "$archive")"
      else
        fail "gh attestation verify rejected $(basename "$archive")"
      fi
    done
    if [[ $skip_image -eq 0 ]]; then
      if gh attestation verify "oci://$image:$tag" --repo "$repo" --signer-workflow "$repo/.github/workflows/release.yml" >/dev/null 2>&1; then
        pass "build provenance of $image:$tag"
      else
        fail "gh attestation verify rejected $image:$tag"
      fi
    fi
  else
    skipped+=("build provenance (the gh CLI is not installed)")
  fi

  local s
  for s in ${skipped[@]+"${skipped[@]}"}; do echo "skip $s"; done
  if [[ $failed -eq 0 ]]; then echo "VERIFIED $tag"; else echo "NOT VERIFIED: $tag"; fi
  return "$failed"
}

self_test() {
  local tmp fail=0 here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  tmp=$(mktemp -d)
  ok() { printf 'ok   %s\n' "$1"; }
  bad() { printf 'FAIL %s\n' "$1"; fail=1; }
  mkdir -p "$tmp/bin" "$tmp/rel"
  # Stubs: cosign records its arguments and passes unless STUB_COSIGN_FAIL names a subcommand; gh passes unless STUB_GH_FAIL;
  # curl copies from $tmp/remote.
  cat >"$tmp/bin/cosign" <<STUB
#!/usr/bin/env bash
echo "\$*" >>"$tmp/cosign.calls"
[[ "\${STUB_COSIGN_FAIL:-}" == "\$1" ]] && exit 1
exit 0
STUB
  cat >"$tmp/bin/gh" <<STUB
#!/usr/bin/env bash
echo "\$*" >>"$tmp/gh.calls"
[[ -z "\${STUB_GH_FAIL:-}" ]]
STUB
  cat >"$tmp/bin/curl" <<STUB
#!/usr/bin/env bash
out=""; url=""
while [[ \$# -gt 0 ]]; do case "\$1" in -o) out="\$2"; shift 2 ;; -*) shift ;; *) url="\$1"; shift ;; esac; done
echo "\$url" >>"$tmp/curl.calls"
cp "$tmp/remote/\${url##*/}" "\$out"
STUB
  chmod +x "$tmp/bin/"*
  # a fake release
  mkdir -p "$tmp/remote"
  printf 'archive one\n' >"$tmp/remote/whalenurse_0.1.0_linux_amd64.tar.gz"
  printf 'archive two\n' >"$tmp/remote/whalenurse_0.1.0_linux_arm64.tar.gz"
  printf '{"bundle":1}\n' >"$tmp/remote/checksums.txt.sigstore.json"
  (cd "$tmp/remote" && sha256sum whalenurse_0.1.0_linux_amd64.tar.gz whalenurse_0.1.0_linux_arm64.tar.gz >checksums.txt)

  run() { # run <env assignments...> -- <verify args>  (output in $out)
    local envs=()
    while [[ "$1" != -- ]]; do envs+=("$1"); shift; done
    shift
    : >"$tmp/cosign.calls"; : >"$tmp/gh.calls"; : >"$tmp/curl.calls"
    out=$(env "${envs[@]}" PATH="$tmp/bin:$PATH" "$here/verify.sh" "$@" 2>&1) && rc=0 || rc=$?
  }
  local out rc want="https://github.com/jackspiering/whalenurse/.github/workflows/release.yml@refs/tags/v0.1.0"

  run X=1 -- v0.1.0 --dir "$tmp/d1"
  [[ $rc -eq 0 && "$out" == *"VERIFIED v0.1.0"* ]] && ok "a good release verifies" || bad "a good release: rc=$rc $out"
  grep -qF -- "verify-blob --bundle $tmp/d1/checksums.txt.sigstore.json --certificate-identity $want --certificate-oidc-issuer https://token.actions.githubusercontent.com $tmp/d1/checksums.txt" "$tmp/cosign.calls" \
    && ok "verify-blob is pinned to the exact workflow identity and issuer" || bad "verify-blob arguments: $(cat "$tmp/cosign.calls")"
  grep -qF -- "verify ghcr.io/jackspiering/whalenurse:v0.1.0 --certificate-identity $want --certificate-oidc-issuer https://token.actions.githubusercontent.com" "$tmp/cosign.calls" \
    && ok "the image is verified against the same identity" || bad "verify image arguments"
  grep -q -- "--regexp" "$tmp/cosign.calls" && bad "a regexp identity was used" || ok "no regexp identity"
  grep -q 'attestation verify oci://ghcr.io/jackspiering/whalenurse:v0.1.0 --repo jackspiering/whalenurse --signer-workflow jackspiering/whalenurse/.github/workflows/release.yml' "$tmp/gh.calls" \
    && ok "provenance of the image is checked" || bad "gh image attestation: $(cat "$tmp/gh.calls")"
  [[ $(grep -c 'attestation verify .*tar.gz' "$tmp/gh.calls") -eq 2 ]] && ok "provenance of both archives is checked" || bad "gh archive attestations"

  run X=1 -- v0.1.0 --dir "$tmp/d1" --no-download
  [[ $rc -eq 0 ]] && [[ ! -s "$tmp/curl.calls" ]] && ok "--no-download downloads nothing" || bad "--no-download: rc=$rc"

  echo tampered >"$tmp/d1/whalenurse_0.1.0_linux_arm64.tar.gz"
  run X=1 -- v0.1.0 --dir "$tmp/d1" --no-download
  [[ $rc -ne 0 && "$out" == *"NOT VERIFIED"* && "$out" == *"arm64"* ]] && ok "an altered archive fails" || bad "altered archive: rc=$rc $out"

  run X=1 -- v0.1.0 --dir "$tmp/d1" --no-download
  rm "$tmp/d1/whalenurse_0.1.0_linux_amd64.tar.gz"
  run X=1 -- v0.1.0 --dir "$tmp/d1" --no-download
  [[ $rc -ne 0 ]] && ok "a file named in checksums.txt but missing fails (nothing is skipped)" || bad "missing archive: rc=$rc"

  run STUB_COSIGN_FAIL=verify-blob -- v0.1.0 --dir "$tmp/d2"
  [[ $rc -ne 0 && "$out" == *"not signed by"* ]] && ok "a bad checksums signature fails, and stops before the archives" || bad "verify-blob failure: rc=$rc $out"
  [[ ! -e "$tmp/d2/whalenurse_0.1.0_linux_amd64.tar.gz" ]] && ok "no archive is downloaded after a failed signature" || bad "archives were downloaded after a bad signature"

  run STUB_COSIGN_FAIL=verify -- v0.1.0 --dir "$tmp/d3"
  [[ $rc -ne 0 && "$out" == *"rejected ghcr.io"* ]] && ok "a bad image signature fails" || bad "image verify failure: rc=$rc $out"

  run STUB_GH_FAIL=1 -- v0.1.0 --dir "$tmp/d4"
  [[ $rc -ne 0 ]] && ok "a failed attestation fails" || bad "attestation failure: rc=$rc"

  run X=1 -- v0.1.0 --dir "$tmp/d5" --skip-image
  [[ $rc -eq 0 && "$out" == *"skip image signature"* ]] && ! grep -q '^verify ghcr' "$tmp/cosign.calls" && ok "--skip-image says what it skipped" || bad "--skip-image: rc=$rc $out"

  run X=1 -- v0.1.0 --dir "$tmp/d6" --repo someone/else
  grep -qF -- "--certificate-identity https://github.com/someone/else/.github/workflows/release.yml@refs/tags/v0.1.0" "$tmp/cosign.calls" && ok "--repo changes the pinned identity" || bad "--repo"

  for t in 0.1.0 v0.1 main 'v0.1.0 x' ''; do
    run X=1 -- "$t" --dir "$tmp/d7"
    [[ $rc -eq 2 ]] && ok "tag '$t' is refused" || bad "tag '$t': rc=$rc"
  done
  printf 'abc  ../../etc/passwd\n' >"$tmp/remote/checksums.txt"
  run X=1 -- v0.1.0 --dir "$tmp/d8"
  [[ $rc -ne 0 && "$out" == *"not a plain file name"* ]] && ok "a path in checksums.txt is refused" || bad "path in checksums: rc=$rc $out"

  rm -rf "$tmp"
  return "$fail"
}

if [[ "${1:-}" == "--self-test" ]]; then
  self_test
else
  verify "$@"
fi
