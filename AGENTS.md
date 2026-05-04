# AGENTS.md

## Project overview

`seasonalnet-icon-cdn` is a very small Node.js service that renders Lucide SVG icons as colored PNGs for SeasonalWeather and related SeasonalNet clients.

The service should stay boring, deterministic, and easy to operate. Avoid adding databases, queues, Redis, build systems, frameworks, or broad abstractions unless there is a clear operational need.

## House rules

- Keep `server.js` focused on HTTP routing, startup, and process-level error handling.
- Put focused implementation details under `lib/`.
  - `lib/config.js` owns configuration loading and environment overrides.
  - `lib/render.js` owns Lucide SVG validation and Sharp PNG rendering.
  - `lib/cache.js` owns disk cache reads, writes, paths, and cleanup.
- Preserve the public API unless explicitly requested:
  - `GET /icon?icon=ICONNAME&hex=RRGGBB`
  - `GET /health`
- Preserve compatibility with the existing environment variables where practical, especially `CDN_PORT` and `CDN_SIZE`.
- Keep cache behavior deterministic. Cache entries should be keyed by render-affecting inputs such as Lucide version, icon size, icon name, and color.
- Do not turn cache cleanup into request-path complexity. Cleanup should be best-effort and non-fatal.
- Prefer small, dependency-light changes. New dependencies should have a direct purpose.
- Do not commit generated cache files, `node_modules/`, secrets, or local deployment artifacts.

## Validation commands

Run these before committing changes when the environment has dependencies installed:

```bash
npm install
npm run check-config
npm run check-lucide
npm run check-sharp
node --check server.js
node --check lib/config.js
node --check lib/render.js
node --check lib/cache.js
```

For a quick runtime smoke test:

```bash
npm start
curl -f http://127.0.0.1:3600/health
curl -f 'http://127.0.0.1:3600/icon?icon=siren&hex=FF0000' -o /tmp/siren.png
```

## Deployment notes

The default cache directory is `./cache`, but production deployments should prefer an explicit writable cache path such as `/var/cache/seasonalnet-icon-cdn` and grant only that path write access in systemd.
