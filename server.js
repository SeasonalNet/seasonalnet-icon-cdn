// cdn-service/server.js
// Lucide icon CDN for SeasonalWeather Discord embeds.
//
// GET /icon?icon=ICONNAME&hex=RRGGBB
//   Returns a 64×64 PNG of the named Lucide icon, stroke recolored to #RRGGBB.
//   Responses are cached to disk on first render. Subsequent requests for the
//   same (icon, hex) pair are served from cache with no re-render.
//
// GET /health  → 200 OK {"status":"ok"}
//
// Designed to run on seasonalweb under systemd, proxied by nginx at
// cdn.seasonalnet.org/icon.  Not exposed directly to the internet.

"use strict";

const http    = require("http");
const fs      = require("fs");
const path    = require("path");
const crypto  = require("crypto");

// --- Lazy-load sharp so startup doesn't throw if it's missing ---
let sharp;
try {
  sharp = require("sharp");
} catch (e) {
  console.error("[cdn] FATAL: sharp is not installed. Run: npm install");
  process.exit(1);
}

// --- Lucide static icons directory ---
// lucide-static ships individual SVG files under icons/
const ICONS_DIR = path.join(__dirname, "node_modules", "lucide-static", "icons");
if (!fs.existsSync(ICONS_DIR)) {
  console.error(`[cdn] FATAL: lucide-static icons directory not found at ${ICONS_DIR}`);
  console.error("      Run: npm install");
  process.exit(1);
}

// --- Disk cache ---
const CACHE_DIR = path.join(__dirname, "cache");
if (!fs.existsSync(CACHE_DIR)) fs.mkdirSync(CACHE_DIR, { recursive: true });

const PORT     = parseInt(process.env.CDN_PORT || "3600", 10);
const SIZE     = parseInt(process.env.CDN_SIZE || "64", 10);  // px; Discord thumbnail

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

function isValidIconName(name) {
  // Lucide icon names: lowercase letters, digits, hyphens only. No path traversal.
  return /^[a-z0-9-]{1,64}$/.test(name);
}

function isValidHex(hex) {
  return /^[0-9a-fA-F]{6}$/.test(hex);
}

function normaliseHex(hex) {
  return hex.toUpperCase().replace(/^#/, "");
}

// ---------------------------------------------------------------------------
// Cache helpers
// ---------------------------------------------------------------------------

function cacheKey(icon, hex) {
  return path.join(CACHE_DIR, `${icon}-${hex.toUpperCase()}.png`);
}

// ---------------------------------------------------------------------------
// Render: read SVG → recolor currentColor → rasterize with sharp
// ---------------------------------------------------------------------------

async function renderIcon(icon, hex) {
  const svgPath = path.join(ICONS_DIR, `${icon}.svg`);

  // Check the icon exists
  if (!fs.existsSync(svgPath)) {
    return null;  // 404
  }

  let svg = fs.readFileSync(svgPath, "utf8");

  // Lucide SVGs use stroke="currentColor" and fill="none".
  // Recolor: replace every occurrence of currentColor with the target hex.
  const colorHex = `#${hex}`;
  svg = svg.replace(/currentColor/gi, colorHex);

  // Ensure the SVG has explicit width/height so sharp knows the viewport
  // (Lucide SVGs already have viewBox="0 0 24 24" but may lack w/h attrs)
  if (!/\bwidth=/.test(svg)) {
    svg = svg.replace("<svg", `<svg width="${SIZE}" height="${SIZE}"`);
  }

  // Rasterize with sharp
  const png = await sharp(Buffer.from(svg))
    .resize(SIZE, SIZE)
    .png()
    .toBuffer();

  return png;
}

// ---------------------------------------------------------------------------
// Request handler
// ---------------------------------------------------------------------------

async function handler(req, res) {
  const url = new URL(req.url, `http://localhost`);

  // Health check
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
  const hex  = normaliseHex(url.searchParams.get("hex") || "FFFFFF");

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

  const cachePath = cacheKey(icon, hex);

  // Serve from cache
  if (fs.existsSync(cachePath)) {
    const data = fs.readFileSync(cachePath);
    res.writeHead(200, {
      "Content-Type":  "image/png",
      "Cache-Control": "public, max-age=604800, immutable",  // 1 week
      "X-Cache":       "HIT",
    });
    res.end(data);
    return;
  }

  // Render
  let png;
  try {
    png = await renderIcon(icon, hex);
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

  // Write cache atomically (write to .tmp then rename)
  const tmp = cachePath + ".tmp";
  try {
    fs.writeFileSync(tmp, png);
    fs.renameSync(tmp, cachePath);
  } catch (err) {
    console.error(`[cdn] cache write error:`, err.message);
    // Non-fatal: still serve the response
    try { fs.unlinkSync(tmp); } catch (_) {}
  }

  res.writeHead(200, {
    "Content-Type":  "image/png",
    "Cache-Control": "public, max-age=604800, immutable",
    "X-Cache":       "MISS",
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

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[cdn] SeasonalNet icon CDN listening on 127.0.0.1:${PORT}`);
  console.log(`[cdn] Icons dir: ${ICONS_DIR}`);
  console.log(`[cdn] Cache dir: ${CACHE_DIR}`);
  console.log(`[cdn] Size: ${SIZE}px`);
});

server.on("error", err => {
  console.error("[cdn] server error:", err);
  process.exit(1);
});
