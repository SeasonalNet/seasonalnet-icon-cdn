FROM node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 AS lucide-assets

ENV COREPACK_HOME=/tmp/corepack
WORKDIR /build

RUN corepack enable && corepack prepare pnpm@11.17.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY tools ./tools
RUN pnpm sync-icons

FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS go-toolchain

FROM ubuntu:resolute@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7 AS build

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

FROM ubuntu:resolute@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7 AS runtime

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
