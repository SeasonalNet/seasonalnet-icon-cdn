FROM node:24-bookworm-slim@sha256:c86ecead9e55a5210ac1727de38a4ca840bf03dcaf34dc9749addbf383b1920c AS lucide-assets

ENV COREPACK_HOME=/tmp/corepack
WORKDIR /build

RUN corepack enable && corepack prepare pnpm@12.15.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY tools ./tools
RUN pnpm sync-icons

FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS go-toolchain

FROM ubuntu:resolute@sha256:19fcd1bf268f02b60aca3bd30bd8a6d9a11759a430e32921833e65f16d87348a AS build

COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential ca-certificates libvips-dev=8.18.0-1build1 pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY openapi.yaml ./openapi.yaml
COPY --from=lucide-assets /build/internal/cdn/assets/lucide ./internal/cdn/assets/lucide

ARG APP_VERSION=0.0.0+unknown
ARG APP_REVISION=unknown
ENV APP_VERSION=${APP_VERSION} APP_REVISION=${APP_REVISION}

RUN go test ./... \
    && go vet ./... \
    && CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.softwareVersion=${APP_VERSION} -X main.softwareRevision=${APP_REVISION}" -o /out/icon-cdn ./cmd/icon-cdn \
    && mkdir -p /var/cache/seasonalnet-icon-cdn \
    && chown -R 10001:10001 /var/cache/seasonalnet-icon-cdn

FROM ubuntu:resolute@sha256:19fcd1bf268f02b60aca3bd30bd8a6d9a11759a430e32921833e65f16d87348a AS runtime-files

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libvips42t64=8.18.0-1build1 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV CDN_CONFIG=/run/config/seasonalnet-icon-cdn.yaml \
    CDN_HOST=0.0.0.0 \
    CDN_PORT=3600 \
    CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn

COPY --from=build --chown=0:0 /out/icon-cdn /usr/local/bin/icon-cdn
COPY --from=build --chown=10001:10001 /var/cache/seasonalnet-icon-cdn /var/cache/seasonalnet-icon-cdn

COPY tools/container/assemble-rootfs.py /tmp/assemble-rootfs.py
RUN python3 /tmp/assemble-rootfs.py vips /runtime-root

FROM scratch AS runtime

ARG APP_VERSION=0.0.0+unknown
ARG APP_REVISION=unknown
ENV APP_VERSION=${APP_VERSION} APP_REVISION=${APP_REVISION}
LABEL org.opencontainers.image.version="${APP_VERSION}" org.opencontainers.image.revision="${APP_REVISION}"

COPY --from=runtime-files /runtime-root /
COPY --from=build --chown=0:0 /out/icon-cdn /usr/local/bin/icon-cdn
ENV CDN_CONFIG=/run/config/seasonalnet-icon-cdn.yaml CDN_HOST=0.0.0.0 CDN_PORT=3600 CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn
USER 10001:10001
EXPOSE 3600
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/icon-cdn", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/icon-cdn"]
