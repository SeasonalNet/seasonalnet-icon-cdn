"use strict";

const fs = require("fs");
const path = require("path");

let sharp;
try {
  sharp = require("sharp");
} catch (err) {
  console.error("[cdn] FATAL: sharp is not installed. Run: pnpm install");
  process.exit(1);
}

function assertIconsDir(iconsDir) {
  if (!fs.existsSync(iconsDir)) {
    console.error(`[cdn] FATAL: lucide-static icons directory not found at ${iconsDir}`);
    console.error("      Run: pnpm install");
    process.exit(1);
  }
}

function isValidIconName(name) {
  return /^[a-z0-9-]{1,64}$/.test(name);
}

function isValidHex(hex) {
  return /^[0-9a-fA-F]{6}$/.test(hex);
}

function normaliseHex(hex) {
  return String(hex || "").toUpperCase().replace(/^#/, "");
}

function createRenderer({ iconsDir, size }) {
  assertIconsDir(iconsDir);

  async function renderIcon(icon, hex) {
    const svgPath = path.join(iconsDir, `${icon}.svg`);

    if (!fs.existsSync(svgPath)) {
      return null;
    }

    let svg = fs.readFileSync(svgPath, "utf8");

    const colorHex = `#${hex}`;
    svg = svg.replace(/currentColor/gi, colorHex);

    if (!/\bwidth=/.test(svg)) {
      svg = svg.replace("<svg", `<svg width="${size}" height="${size}"`);
    }

    return sharp(Buffer.from(svg))
      .resize(size, size)
      .png()
      .toBuffer();
  }

  return { renderIcon };
}

module.exports = {
  createRenderer,
  isValidHex,
  isValidIconName,
  normaliseHex,
};
