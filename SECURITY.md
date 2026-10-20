# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem. Report it privately with GitHub's private vulnerability reporting:

**https://github.com/jackspiering/whalenurse/security/advisories/new** (the repository's "Security" tab, "Report a vulnerability").

Include what you found, how to reproduce it (a compose file and the whalenurse version are usually enough), and what you think
the impact is. You will get an acknowledgement as soon as the maintainer can send one, and a fix or an explanation after
that; this is a one-person project, so please be patient. Coordinated disclosure: a fix is released before the advisory is
published, and you are credited unless you prefer not to be.

## What is in scope

- The `whalenurse` binary and image: anything that lets someone act on containers whalenurse should not touch, read what the
  dashboard and API should never show (environment variables, secrets, notification URLs, the token, foreign labels), bypass
  the token, the Host check or the read-only rule of the HTTP listener, or run a command through the compose invocation.
- The release pipeline: a way to publish, sign or alter a release without the maintainer.

## What is not

- That mounting `docker.sock` is root-equivalent. It is, and [docs/security.md](docs/security.md) says so.
- A dashboard exposed without a token on an untrusted network, against the documentation.
- Vulnerabilities in Docker, Compose or the other dependencies themselves (report those upstream; do tell us if whalenurse
  needs a bump).

## Supported versions

The latest release. Before v1.0, fixes go into the next release and the changelog says so.

## Verifying what you run

Releases are signed and carry provenance: [docs/verifying-releases.md](docs/verifying-releases.md).
