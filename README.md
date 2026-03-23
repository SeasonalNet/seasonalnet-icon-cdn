# seasonalnet-icon-cdn

A tiny Node.js HTTP service that renders [Lucide](https://lucide.dev) icons as
colored PNGs on demand. Built for use as Discord embed thumbnails in
[SeasonalWeather](https://git.seasonalnet.org/Seasonal_Currency/SeasonalWeather),
but usable anywhere you need a quick icon CDN.

## How it works

Request an icon by name and hex color. The service colorizes the Lucide SVG,
rasterizes it to PNG via [sharp](https://sharp.pixelplumbing.com/), caches the
result to disk, and returns it with long-lived cache headers. Subsequent requests
for the same `(icon, hex)` pair are served straight from disk — no re-render.

## API

```
GET /icon?icon=ICONNAME&hex=RRGGBB
```

| Parameter | Description |
|-----------|-------------|
| `icon`    | Lucide icon name (e.g. `siren`, `bell-ring`, `snowflake`). See [lucide.dev/icons](https://lucide.dev/icons/) for the full list. |
| `hex`     | 6-character hex color, no `#` (e.g. `FF0000` for red). |

Returns a `image/png` with `Cache-Control: public, max-age=604800, immutable`.

```
GET /health
```

Returns `{"status":"ok"}` — used by nginx and monitoring.

### Example

```
https://cdn.seasonalnet.org/icon?icon=siren&hex=FF0000
```

Returns a 64×64 red siren icon PNG.

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

## Setup

Requires Node.js ≥ 18 and npm.

```bash
git clone https://git.seasonalnet.org/Seasonal_Currency/seasonalnet-icon-cdn.git
cd seasonalnet-icon-cdn
npm install
node server.js
```

Environment variables (optional):

| Variable | Default | Description |
|----------|---------|-------------|
| `CDN_PORT` | `3600` | Port to listen on (localhost only) |
| `CDN_SIZE` | `64` | Output PNG size in pixels |

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
        add_header         Cache-Control "public, max-age=604800, immutable";
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
Environment=CDN_PORT=3600
User=www-data
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Cache

Rendered PNGs are cached under `cache/` in the working directory. The cache
directory is created automatically on startup. To clear it:

```bash
rm -rf cache/
```

The `cache/` directory is gitignored.

## License

MIT
