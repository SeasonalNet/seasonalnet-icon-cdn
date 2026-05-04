# seasonalnet-icon-cdn

A tiny Node.js HTTP service that renders [Lucide](https://lucide.dev) icons as
colored PNGs on demand. Built for use as Discord embed thumbnails in
[SeasonalWeather](https://git.seasonalnet.org/Seasonal_Currency/SeasonalWeather),
but usable anywhere you need a quick icon CDN.

## How it works

Request an icon by name and hex color. The service colorizes the Lucide SVG,
rasterizes it to PNG via [sharp](https://sharp.pixelplumbing.com/), caches the
result to disk, and returns it with long-lived cache headers. Subsequent requests
for the same render-affecting inputs are served straight from disk — no
re-render.

The cache path includes the Lucide version and output size, so changing the icon
set or `render.size` does not accidentally serve stale PNGs from an older render
configuration.

## API

```
GET /icon?icon=ICONNAME&hex=RRGGBB
```

| Parameter | Description |
|-----------|-------------|
| `icon`    | Lucide icon name (e.g. `siren`, `bell-ring`, `snowflake`). See [lucide.dev/icons](https://lucide.dev/icons/) for the full list. |
| `hex`     | 6-character hex color, no `#` (e.g. `FF0000` for red). |

Returns an `image/png` with `Cache-Control: public, max-age=604800, immutable`
by default.

```
GET /health
```

Returns `{"status":"ok"}` — used by nginx and monitoring.

### Example

```
https://cdn.seasonalnet.org/icon?icon=siren&hex=FF0000
```

Returns a 64×64 red siren icon PNG with the default configuration.

## SeasonalWeather icon mapping

SeasonalWeather maps EAS event codes to Lucide icons and NWS hazard-map colors
when posting Discord embeds. Some examples:

| Event | Icon | Color |
|-------|------|-------|
| TOR — Tornado Warning | `siren` | `#FF0000` |
| SVR — Severe Thunderstorm Warning | `cloud-lightning` | `#FF8C00` |
| FFW — Flash Flood Warning | `waves` | `#8B0000` |
| WSW — Winter Storm Warning | `snowflake` | `#FF69B4` |
| HUW — Hurricane Warning | `wind` | `#DC143C` |
| RWT — Required Weekly Test | `radio` | `#C0C0C0` |
| Errors | `circle-alert` | `#E24B4A` |

The full mapping lives in `discord_log.py` in the SeasonalWeather repo.

## Layout

```text
server.js       HTTP routing and startup
config.yaml     Runtime defaults
lib/config.js   YAML loading, defaults, and environment overrides
lib/render.js   Lucide SVG lookup and Sharp PNG rendering
lib/cache.js    Disk cache paths, reads, writes, and cleanup
```

## Setup

Requires Node.js ≥ 18 and npm.

```bash
git clone https://git.seasonalnet.org/Seasonal_Currency/seasonalnet-icon-cdn.git
cd seasonalnet-icon-cdn
npm install
npm start
```

Validation helpers:

```bash
npm run check-config
npm run check-lucide
npm run check-sharp
```

## Configuration

The service loads `config.yaml` from the repository root by default. To use a
different file:

```bash
CDN_CONFIG=/etc/seasonalnet-icon-cdn/config.yaml npm start
```

Default configuration:

```yaml
server:
  host: "127.0.0.1"
  port: 3600

render:
  size: 64
  icons_dir: null

cache:
  dir: "./cache"
  namespace: "auto"
  http_max_age_seconds: 604800
  immutable: true

  cleanup:
    enabled: true
    on_startup: true
    interval_ms: 3600000
    max_age_days: 180
    max_files: 25000
    max_bytes: 104857600
    tmp_max_age_minutes: 30
```

Environment variables can override the common runtime values:

| Variable | Default | Description |
|----------|---------|-------------|
| `CDN_CONFIG` | `./config.yaml` | Config file path |
| `CDN_HOST` | `127.0.0.1` | Listen address |
| `CDN_PORT` | `3600` | Listen port |
| `CDN_SIZE` | `64` | Output PNG size in pixels |
| `CDN_ICONS_DIR` | `node_modules/lucide-static/icons` | Lucide SVG directory override |
| `CDN_CACHE_DIR` | `./cache` | Root disk cache directory |
| `CDN_CACHE_NAMESPACE` | `auto` | Cache namespace; `auto` uses the Lucide package version |
| `CDN_CACHE_HTTP_MAX_AGE_SECONDS` | `604800` | Browser/proxy max-age for icon responses |
| `CDN_CACHE_IMMUTABLE` | `true` | Include `immutable` in cache headers |
| `CDN_CACHE_CLEANUP_ENABLED` | `true` | Enable background cache cleanup |
| `CDN_CACHE_CLEAN_ON_STARTUP` | `true` | Run cleanup at service startup |
| `CDN_CACHE_CLEAN_INTERVAL_MS` | `3600000` | Cleanup interval; `0` disables scheduled cleanup |
| `CDN_CACHE_MAX_AGE_DAYS` | `180` | Delete PNGs older than this many days; use `null` to disable |
| `CDN_CACHE_MAX_FILES` | `25000` | Maximum cached PNG count; use `null` to disable |
| `CDN_CACHE_MAX_BYTES` | `104857600` | Maximum cached PNG bytes; use `null` to disable |
| `CDN_CACHE_TMP_MAX_AGE_MINUTES` | `30` | Delete abandoned `.tmp` files after this many minutes |

## Cache

Rendered PNGs are cached under a versioned directory. With the defaults, the
path looks like this:

```text
cache/lucide-0.484.0/size-64/siren-FF0000.png
```

Cache cleanup is best-effort and non-fatal. It removes abandoned `.tmp` files,
optionally removes very old PNGs, then prunes the oldest remaining PNGs if the
cache exceeds `max_files` or `max_bytes`.

This is intended as a guardrail against unbounded icon/color combinations, not
as a complicated freshness system. Icon output is deterministic, so a cached PNG
is safe to reuse until the render configuration changes.

To clear the cache manually:

```bash
rm -rf cache/
```

The `cache/` directory is gitignored.

## Deployment

The service is designed to run behind nginx as a reverse proxy, with Cloudflare
in front for edge caching. A sample nginx server block:

```nginx
server {
    listen 443 ssl;
    server_name cdn.yourdomain.org;
    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location /icon {
        proxy_pass         http://127.0.0.1:3600;
        proxy_http_version 1.1;
        proxy_set_header   Host $host;
    }

    location /health {
        proxy_pass http://127.0.0.1:3600;
        access_log off;
    }
}
```

A sample systemd unit:

```ini
[Unit]
Description=SeasonalNet Lucide icon CDN
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/node /opt/seasonalnet/cdn/server.js
WorkingDirectory=/opt/seasonalnet/cdn
Environment=CDN_CONFIG=/opt/seasonalnet/cdn/config.yaml
Environment=CDN_PORT=3600
Environment=CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn
User=www-data
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

For a stricter systemd sandbox, keep the app directory read-only and allow only
the cache path to be writable:

```ini
ReadOnlyPaths=/opt/seasonalnet/cdn
ReadWritePaths=/var/cache/seasonalnet-icon-cdn
```

## License

MIT
