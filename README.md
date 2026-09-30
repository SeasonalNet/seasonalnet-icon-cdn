# seasonalnet-icon-cdn

A small Go HTTP service that renders Lucide icons for Discord embeds and other clients.

## API

### Render an icon

```http
GET /icon?icon=siren&hex=FF0000
```

The default response is a 64×64 PNG with `Cache-Control: public, max-age=604800, immutable`. `hex` is six hexadecimal digits; an optional leading `#` is accepted. If omitted or empty, the color defaults to white.

Optional render variants:

```text
/icon?icon=siren&hex=FF0000&size=128&format=png
/icon?icon=siren&hex=FF0000&format=svg
```

Explicit sizes are bounded to `16`, `24`, `32`, `48`, `64`, `96`, and `128`. Omitting `size` uses `CDN_SIZE` or `render.size`. Formats are `png` (default) and `svg`. Cache entries vary by Lucide version, size, icon, color, and format.

Successful responses include an `ETag`, `X-Cache` (`HIT`, `MISS`, or `COALESCED`), and cache headers. `HEAD` returns the same headers as `GET` without an image body. `If-None-Match` returns `304 Not Modified` when the representation matches.

### Other endpoints

| Endpoint | Response |
|---|---|
| `GET /health` | `{"status":"ok"}` liveness check; preserved for existing probes. |
| `GET /ready` | Readiness status. |
| `GET /icons` | Sorted icon names and the embedded Lucide version. |
| `GET /metrics` | Prometheus text metrics for requests, cache behavior, renders, and errors. |

Errors use `application/problem+json` following RFC 9457. The `code` extension is stable for client handling; `detail` is explanatory text. The API description is in [openapi.yaml](openapi.yaml) and targets OpenAPI 3.2.1.

Example invalid-input response:

```json
{
  "type": "about:blank",
  "title": "Invalid hex color",
  "status": 400,
  "detail": "Expected six hexadecimal characters, with an optional leading #.",
  "code": "invalid_hex_color"
}
```

## Rendering compatibility

The Node and Go implementations both use libvips for SVG rasterization and PNG encoding. The Go service uses govips and requires libvips 8.14 or newer, pkg-config, and a C compiler. The container pins Ubuntu Resolute and libvips 8.18.0 in both build and runtime stages. A full comparison of all 2,118 embedded icons at all seven supported sizes found identical decoded RGBA pixels to the original Sharp/libvips 8.18.7 pipeline. PNG files are not byte-identical because the encoder metadata and compressed stream differ.

## Build and run

Requirements for bare-metal development: Go 1.27.1+, libvips 8.14+ development files, pkg-config, a C compiler, Node.js, pnpm 11.17.0, and Python 3 with venv support for OpenAPI validation. `make ci` also requires Docker with Buildx. The Docker build installs the pinned libvips build and runtime packages in its own stages.

```bash
make dev
```

`make dev` installs the pinned asset tooling, syncs Lucide files, and runs the service. The generated `internal/cdn/assets/lucide/` directory is ignored by Git; `go:embed` includes those files in the built binary. Common manual operations:

```bash
make setup          # install pnpm dependencies and sync Lucide files
make build          # build bin/icon-cdn
make check          # run formatting, lint, vet, tests, race, coverage, vulnerability, and API checks
make ci             # quality gates, container HTTP smoke checks, dependency policy, and build
make container      # build the local Docker image
make container-check # build the image and smoke-test its HTTP endpoints
make openapi-check  # validate openapi.yaml against OpenAPI 3.2
make vuln           # scan Go code for reachable known vulnerabilities
```

The Dockerfile performs the asset sync and Go build in separate stages. It installs the pinned libvips development package only in the build stage and the matching runtime package in the final Ubuntu image; Node and the compiler are absent from the runtime image.

## Configuration

The service reads `config.yaml` from its working directory by default. Set `CDN_CONFIG` to use another file. Missing configuration falls back to these defaults:

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

Environment overrides:

| Variable | Default | Description |
|---|---:|---|
| `CDN_CONFIG` | `./config.yaml` | Configuration path; relative paths use the working directory. |
| `CDN_HOST` | `127.0.0.1` | Listen address. |
| `CDN_PORT` | `3600` | Listen port. |
| `CDN_SIZE` | `64` | Default rendered size. |
| `CDN_ICONS_DIR` | embedded assets | Optional external SVG directory. |
| `CDN_CACHE_DIR` | `./cache` | Cache root. |
| `CDN_CACHE_NAMESPACE` | `auto` | Cache namespace; `auto` uses the embedded Lucide version. |
| `CDN_CACHE_HTTP_MAX_AGE_SECONDS` | `604800` | HTTP max-age for rendered icons. |
| `CDN_CACHE_IMMUTABLE` | `true` | Include `immutable` in image cache headers. |
| `CDN_CACHE_CLEANUP_ENABLED` | `true` | Enable cache cleanup. |
| `CDN_CACHE_CLEAN_ON_STARTUP` | `true` | Run best-effort cleanup at startup. |
| `CDN_CACHE_CLEAN_INTERVAL_MS` | `3600000` | Cleanup interval; `0` disables the timer. |
| `CDN_CACHE_MAX_AGE_DAYS` | `180` | Delete older PNG entries; `null` disables age pruning. |
| `CDN_CACHE_MAX_FILES` | `25000` | Maximum number of cached PNGs; `null` disables this limit. |
| `CDN_CACHE_MAX_BYTES` | `104857600` | Maximum PNG cache bytes; `null` disables this limit. |
| `CDN_CACHE_TMP_MAX_AGE_MINUTES` | `30` | Remove older abandoned atomic-write temp files. |

Cache entries are stored under a versioned directory, for example:

```text
cache/lucide-1.48.0/size-64/siren-FF0000.png
```

Cleanup removes stale PNGs and old `.tmp` files, then prunes the oldest remaining PNGs if count or byte limits are exceeded. It is best-effort and never prevents a rendered response from being served.

## Deployment

The service is intended to run behind nginx with Cloudflare edge caching. The container listens on port `3600`, runs as UID/GID `10001`, and uses `/var/cache/seasonalnet-icon-cdn` for its writable cache. Mount the site-local config at `/run/config/seasonalnet-icon-cdn.yaml` and persist the cache path.

Example systemd unit:

```ini
[Unit]
Description=SeasonalNet Lucide icon CDN
After=network.target

[Service]
Type=simple
ExecStart=/opt/seasonalnet/icon-cdn
WorkingDirectory=/opt/seasonalnet
Environment=CDN_CONFIG=/etc/seasonalnet-icon-cdn/config.yaml
Environment=CDN_PORT=3600
Environment=CDN_CACHE_DIR=/var/cache/seasonalnet-icon-cdn
User=www-data
Restart=on-failure
ReadOnlyPaths=/opt/seasonalnet
ReadWritePaths=/var/cache/seasonalnet-icon-cdn

[Install]
WantedBy=multi-user.target
```

## Quality baseline

`make check` and `make ci` share the Go formatting, golangci-lint, vet, unit, race, coverage, Go vulnerability, OpenAPI specification, and Lucide asset checks. They also run a suppression finder that rejects Go `nolint`/ignore directives, skipped tests, disabled or excluded golangci-lint rules, and CI commands that skip or ignore Go quality checks. Its own fixtures are tested as part of the gate. The `internal/cdn` package must maintain at least 90% statement coverage; the current unit suite is above that threshold. CI also checks pinned pnpm dependencies, JavaScript syntax, and the built container's health, metadata, metrics, PNG, SVG, caching, and problem responses. `COVERAGE_MIN` can be raised for local experiments, but the Makefile rejects values below the checked-in baseline.

Focused targets are available for iteration: `make test`, `make test-race`, `make coverage`, `make lint`, `make vet`, `make vuln`, `make openapi-check`, `make fmt`, `make anti-quality`, `make test-anti-quality`, `make check-lucide`, and `make container-check`. `make build` creates the ignored `bin/icon-cdn` executable. API failure behavior is covered by tests for invalid paths and parameters, unsupported methods, missing icons, malformed SVGs, cache read/write failures, conditional requests, and service recovery after an individual render failure.

## License

`seasonalnet-icon-cdn` is licensed under the GNU AGPLv3 license. See [LICENSE](./LICENSE).
