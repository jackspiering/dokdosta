FROM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/whalenurse ./cmd/whalenurse

# The bundled docker compose plugin: the fallback when the host's plugin is not mounted (plan section 3.4). It is a static
# binary and runs standalone. The checksums are those of the release assets; to bundle another release, pass all three.
ARG COMPOSE_VERSION=5.6.0
ARG COMPOSE_SHA256_AMD64=40343e21ca777173e69cff5dbafeb37c6f81f3b0d57d9e597f036e95eb63e76a
ARG COMPOSE_SHA256_ARM64=733ec76717ceb59052a9609b9dadfb523b2df8eab57a54212872d10a58078ea2
ARG TARGETARCH
RUN arch="${TARGETARCH:-$(dpkg --print-architecture)}" && \
    case "$arch" in \
      amd64) asset=x86_64; sum="$COMPOSE_SHA256_AMD64" ;; \
      arm64) asset=aarch64; sum="$COMPOSE_SHA256_ARM64" ;; \
      *) echo "no bundled compose for $arch" >&2; exit 1 ;; \
    esac && \
    curl -fsSL -o /out/docker-compose "https://github.com/docker/compose/releases/download/v${COMPOSE_VERSION}/docker-compose-linux-${asset}" && \
    echo "${sum}  /out/docker-compose" | sha256sum -c - && \
    chmod 0755 /out/docker-compose
# The state store lives in /data. A fresh named volume copies the ownership of the image's directory, so make it
# the nonroot user's; otherwise the volume is root-owned and the store cannot open.
RUN mkdir -p /out/data

# distroless/static has no shell, so the healthcheck below calls the binary.
# The image runs as uid 65532, which cannot read the Docker socket: give it the
# socket's group at run time with `group_add` (see docs/running.md).
# To recreate, mount the host's compose plugin read-only at /host/docker-compose (used first) and the stacks root
# read-only at the identical host path; without the plugin the bundled copy above is used.
FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/whalenurse /whalenurse
COPY --from=build --chmod=0755 /out/docker-compose /usr/libexec/whalenurse/docker-compose
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
USER 65532:65532
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/whalenurse", "healthcheck"]
ENTRYPOINT ["/whalenurse"]
CMD ["run"]
