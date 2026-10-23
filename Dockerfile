FROM node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d AS lucide-assets

ENV COREPACK_HOME=/tmp/corepack
WORKDIR /build

RUN corepack enable && corepack prepare pnpm@11.17.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY tools ./tools
RUN pnpm sync-icons

FROM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS go-toolchain

FROM ubuntu:resolute@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944 AS build

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

FROM ubuntu:resolute@sha256:c098726dd9b9733f3e885f8de0ef375206a311b0a317eadc40d347b1115fb944 AS runtime-files

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
COPY --from=runtime-files /runtime-root /
COPY --from=build --chown=0:0 /out/icon-cdn /usr/local/bin/icon-cdn
ENV CDN_CONFIG=/run/config/seasonalnet-icon-cdn.yaml CDN_HOST=0.0.0.0 CDN_PORT=3600 CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn
USER 10001:10001
EXPOSE 3600
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/icon-cdn", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/icon-cdn"]
