// cdn-service/server.js
// Lucide icon CDN for SeasonalWeather Discord embeds.
//
// GET /icon?icon=ICONNAME&hex=RRGGBB
//   Returns a PNG of the named Lucide icon, stroke recolored to #RRGGBB.
//   Responses are cached to disk on first render. Subsequent requests for the
//   same render-affecting inputs are served from cache with no re-render.
//
// GET /health  → 200 OK {"status":"ok"}
//
// Designed to run on seasonalweb under systemd, proxied by nginx at
// cdn.seasonalnet.org/icon. Not exposed directly to the internet.

"use strict";

const http = require("http");

const { loadConfig } = require("./lib/config");
const { DiskCache } = require("./lib/cache");
const {
  createRenderer,
  isValidHex,
  isValidIconName,
  normaliseHex,
} = require("./lib/render");

let config;
try {
  config = loadConfig({ appRoot: __dirname });
} catch (err) {
  console.error("[cdn] FATAL: config error:", err.message);
  process.exit(1);
}

const cache = new DiskCache(config);
const renderer = createRenderer({
  iconsDir: config.render.icons_dir,
  size: config.render.size,
});

// ---------------------------------------------------------------------------
// Request handler
// ---------------------------------------------------------------------------

async function handler(req, res) {
  const url = new URL(req.url, `http://${config.server.host}`);

  if (url.pathname === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }

  if (url.pathname !== "/icon") {
    res.writeHead(404);
    res.end("Not found");
    return;
  }

  const icon = (url.searchParams.get("icon") || "").toLowerCase().trim();
  const hex = normaliseHex(url.searchParams.get("hex") || "FFFFFF");

  if (!isValidIconName(icon)) {
    res.writeHead(400);
    res.end("Invalid icon name");
    return;
  }
  if (!isValidHex(hex)) {
    res.writeHead(400);
    res.end("Invalid hex color (expect 6 hex chars, no #)");
    return;
  }

  let cached;
  try {
    cached = await cache.read(icon, hex);
  } catch (err) {
    console.error(`[cdn] cache read error icon=${icon} hex=${hex}:`, err.message);
  }

  if (cached) {
    res.writeHead(200, {
      "Content-Type": "image/png",
      "Cache-Control": cache.cacheControl,
      "X-Cache": "HIT",
    });
    res.end(cached);
    return;
  }

  let png;
  try {
    png = await renderer.renderIcon(icon, hex);
  } catch (err) {
    console.error(`[cdn] render error icon=${icon} hex=${hex}:`, err.message);
    res.writeHead(500);
    res.end("Render error");
    return;
  }

  if (!png) {
    res.writeHead(404);
    res.end(`Icon not found: ${icon}`);
    return;
  }

  try {
    await cache.write(icon, hex, png);
  } catch (err) {
    console.error("[cdn] cache write error:", err.message);
    // Non-fatal: still serve the response.
  }

  res.writeHead(200, {
    "Content-Type": "image/png",
    "Cache-Control": cache.cacheControl,
    "X-Cache": "MISS",
  });
  res.end(png);
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

const server = http.createServer((req, res) => {
  handler(req, res).catch(err => {
    console.error("[cdn] unhandled error:", err);
    if (!res.headersSent) {
      res.writeHead(500);
      res.end("Internal server error");
    }
  });
});

async function start() {
  await cache.ensure();

  if (config.cache.cleanup.enabled && config.cache.cleanup.on_startup) {
    cache.cleanup("startup")
      .then(stats => {
        if (!stats) return;
        const removed = stats.removed_tmp + stats.removed_stale + stats.removed_over_limit;
        if (removed > 0) {
          console.log(
            `[cdn] startup cache cleanup removed ${removed} files ` +
            `(tmp=${stats.removed_tmp}, stale=${stats.removed_stale}, over_limit=${stats.removed_over_limit})`,
          );
        }
      })
      .catch(err => console.error("[cdn] startup cache cleanup error:", err.message));
  }

  cache.startCleanupTimer(console);

  server.listen(config.server.port, config.server.host, () => {
    console.log(`[cdn] SeasonalNet icon CDN listening on ${config.server.host}:${config.server.port}`);
    console.log(`[cdn] Config: ${config.config_path}`);
    console.log(`[cdn] Icons dir: ${config.render.icons_dir}`);
    console.log(`[cdn] Cache dir: ${config.cache.versioned_dir}`);
    console.log(`[cdn] Size: ${config.render.size}px`);
  });
}

server.on("error", err => {
  console.error("[cdn] server error:", err);
  process.exit(1);
});

start().catch(err => {
  console.error("[cdn] startup error:", err);
  process.exit(1);
});
