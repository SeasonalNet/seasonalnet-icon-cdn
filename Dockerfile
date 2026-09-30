FROM node:22-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS lucide-assets

ENV COREPACK_HOME=/tmp/corepack
WORKDIR /build

RUN corepack enable && corepack prepare pnpm@11.17.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY tools ./tools
RUN pnpm sync-icons

FROM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS go-toolchain

FROM ubuntu:resolute@sha256:da6fc2be547864451aa253836dd926da33623312df4a9a243e35dc877c378a78 AS build

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

RUN go test ./... \
    && go vet ./... \
    && CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/icon-cdn ./cmd/icon-cdn \
    && mkdir -p /var/cache/seasonalnet-icon-cdn \
    && chown -R 10001:10001 /var/cache/seasonalnet-icon-cdn

FROM ubuntu:resolute@sha256:da6fc2be547864451aa253836dd926da33623312df4a9a243e35dc877c378a78 AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libvips42t64=8.18.0-1build1 \
    && rm -rf /var/lib/apt/lists/*

ENV CDN_CONFIG=/run/config/seasonalnet-icon-cdn.yaml \
    CDN_HOST=0.0.0.0 \
    CDN_PORT=3600 \
    CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn

COPY --from=build --chown=10001:10001 /out/icon-cdn /usr/local/bin/icon-cdn
COPY --from=build --chown=10001:10001 /var/cache/seasonalnet-icon-cdn /var/cache/seasonalnet-icon-cdn

USER 10001:10001
EXPOSE 3600

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/icon-cdn", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/icon-cdn"]
