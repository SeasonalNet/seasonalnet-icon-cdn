FROM node:22-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS dependencies

ENV COREPACK_HOME=/tmp/corepack
WORKDIR /build

RUN corepack enable && corepack prepare pnpm@11.17.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile --prod

FROM node:22-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c

ENV NODE_ENV=production \
    CDN_CONFIG=/run/config/seasonalnet-icon-cdn.yaml \
    CDN_HOST=0.0.0.0 \
    CDN_PORT=3600 \
    CDN_CACHE_DIR=/tmp/seasonalnet-icon-cdn

RUN groupadd --system --gid 10001 seasonalcdn \
    && useradd --system --uid 10001 --gid 10001 --create-home --home-dir /home/seasonalcdn seasonalcdn \
    && mkdir -p /run/config

WORKDIR /app
COPY --from=dependencies --chown=10001:10001 /build/node_modules ./node_modules
COPY --chown=10001:10001 package.json server.js ./
COPY --chown=10001:10001 lib ./lib

USER 10001:10001
EXPOSE 3600

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:3600/health').then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["node", "server.js"]
