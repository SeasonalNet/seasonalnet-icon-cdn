"use strict";

const assert = require("node:assert/strict");

const baseURL = process.env.CDN_SMOKE_BASE_URL;
if (!baseURL) throw new Error("CDN_SMOKE_BASE_URL is required");

function endpoint(path) {
  return new URL(path, baseURL).toString();
}

function request(path, options = {}) {
  return fetch(endpoint(path), {
    signal: AbortSignal.timeout(10000),
    ...options,
  });
}

async function waitForService() {
  const deadline = Date.now() + 30000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(endpoint("/health"), {
        signal: AbortSignal.timeout(1500),
      });
      if (response.ok) {
        const body = await response.json();
        assert.deepEqual(body, { status: "ok" });
        return;
      }
      lastError = new Error(`/health returned HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 400));
  }
  throw new Error(`service did not become healthy within 30s: ${lastError}`);
}

async function problem(path, status, code) {
  const response = await request(path);
  assert.equal(response.status, status, `${path} status`);
  assert.match(response.headers.get("content-type") || "", /^application\/problem\+json\b/);
  const body = await response.json();
  assert.equal(body.type, "about:blank");
  assert.equal(typeof body.title, "string");
  assert.equal(body.status, status);
  assert.equal(body.code, code);
  assert.equal(typeof body.detail, "string");
}

async function main() {
  await waitForService();

  const ready = await request("/ready");
  assert.equal(ready.status, 200);
  assert.match(ready.headers.get("content-type") || "", /^application\/json\b/);
  assert.deepEqual(await ready.json(), { status: "ready" });

  const iconsResponse = await request("/icons");
  assert.equal(iconsResponse.status, 200);
  assert.match(iconsResponse.headers.get("content-type") || "", /^application\/json\b/);
  const manifest = await iconsResponse.json();
  assert.deepEqual(Object.keys(manifest).sort(), ["icons", "lucide_version"]);
  assert.equal(typeof manifest.lucide_version, "string");
  assert.ok(Array.isArray(manifest.icons));
  assert.match(manifest.lucide_version, /^\d+\.\d+\.\d+$/);
  assert.ok(manifest.icons.length > 1000);
  assert.ok(manifest.icons.includes("siren"));
  assert.deepEqual(manifest.icons, [...manifest.icons].sort());

  const metricsResponse = await request("/metrics");
  assert.equal(metricsResponse.status, 200);
  assert.match(metricsResponse.headers.get("content-type") || "", /^text\/plain\b/);
  assert.match(await metricsResponse.text(), /seasonalnet_icon_cdn_requests_total/);

  const iconPath = "/icon?icon=siren&hex=FF0000";
  const png = await request(iconPath);
  assert.equal(png.status, 200);
  assert.match(png.headers.get("content-type") || "", /^image\/png\b/);
  assert.match(png.headers.get("cache-control") || "", /immutable/);
  assert.ok(["MISS", "COALESCED"].includes(png.headers.get("x-cache")));
  const etag = png.headers.get("etag");
  assert.ok(etag, "PNG response has an ETag");
  const pngBytes = Buffer.from(await png.arrayBuffer());
  assert.deepEqual(pngBytes.subarray(0, 8), Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));

  const conditional = await request(iconPath, {
    headers: { "If-None-Match": etag },
  });
  assert.equal(conditional.status, 304);

  const head = await request(iconPath, { method: "HEAD" });
  assert.equal(head.status, 200);
  assert.equal(head.headers.get("content-length"), String(pngBytes.length));
  assert.equal((await head.arrayBuffer()).byteLength, 0);

  const svg = await request("/icon?icon=siren&hex=00ff00&size=16&format=svg");
  assert.equal(svg.status, 200);
  assert.match(svg.headers.get("content-type") || "", /^image\/svg\+xml\b/);
  const svgText = await svg.text();
  assert.match(svgText, /<svg\b/);
  assert.match(svgText, /width="16"/);
  assert.match(svgText, /#00FF00/);

  await problem("/icon?icon=siren&hex=not-hex", 400, "invalid_hex_color");
  await problem("/icon?icon=not-a-real-icon", 404, "icon_not_found");
  await problem("/missing", 404, "not_found");

  process.stdout.write("container HTTP smoke checks passed\n");
}

main().catch((error) => {
  process.stderr.write(`${error.stack || error}\n`);
  process.exitCode = 1;
});
