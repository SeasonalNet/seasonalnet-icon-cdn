# AGENTS.md

## Project overview

`seasonalnet-icon-cdn` is a small Go HTTP service that renders pinned Lucide SVG assets as PNG or SVG responses. Lucide assets are synced from the exact `lucide-static` version in `package.json` and embedded into the Go binary.

Keep the service deterministic and easy to operate. Avoid databases, queues, Redis, frameworks, and broad abstractions.

## House rules

- Keep HTTP routing and process lifecycle in `cmd/icon-cdn/` and `internal/cdn/server.go`.
- Keep configuration, rendering, and disk-cache behavior in focused files under `internal/cdn/`.
- Preserve the public API and environment variables where practical, especially:
  - `GET /icon?icon=ICONNAME&hex=RRGGBB`
  - `GET /health`
  - `CDN_PORT`, `CDN_SIZE`, `CDN_ICONS_DIR`, and cache settings
- Keep default icon requests compatible: PNG, configured default size, white when `hex` is omitted, and the current versioned cache layout.
- Keep cache keys deterministic and include every render-affecting input.
- Cache cleanup is best-effort and must not add request-path complexity.
- Keep `openapi.yaml`, `config.example.yaml`, the Dockerfile, and CI current when behavior or configuration changes.
- Do not commit generated `internal/cdn/assets/lucide/`, cache files, `node_modules/`, secrets, or site-local `config.yaml`.
- Pin Lucide assets in `package.json`; update them with `pnpm sync-icons` and verify them with `pnpm check-lucide`.
- Do not commit, push, tag, publish, deploy, or rewrite existing work without explicit instruction.

## Validation

For bare-metal development, install Go 1.27.1+, libvips 8.14+ development files, pkg-config, a C compiler, Node.js, pnpm 11.17.0, and Python 3 with venv support. `make ci` additionally requires Docker with Buildx. Then use the shared Make targets:

```bash
make check
make build
make ci
```

`make check` enforces at least 90% statement coverage for `internal/cdn`, runs the race detector, anti-suppression finder, Go vulnerability scan, and OpenAPI 3.2 validator, and checks `gofmt`, `golangci-lint`, `go vet`, and the pinned Lucide asset set. Do not add suppressions or lower the coverage threshold to make a failing check pass.

`make ci` builds and starts the production container, then checks `/health`, `/ready`, `/icons`, `/metrics`, PNG and SVG icon requests, conditional ETags, and problem responses over HTTP. Run `make container-check` to execute this integration gate directly.

## Deployment notes

The Go renderer uses CGO and govips. The Docker builder installs pinned libvips development files; the runtime image contains the matching libvips runtime package. Node and pnpm are used only to sync the pinned Lucide asset set, which is embedded into the Go binary. Production cache storage should use a writable persistent path such as `/var/cache/seasonalnet-icon-cdn`.
