"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const packageRoot = path.dirname(require.resolve("lucide-static/package.json"));
const sourceIcons = path.join(packageRoot, "icons");
const target = path.join(root, "internal", "cdn", "assets", "lucide");
const targetIcons = path.join(target, "icons");
const version = require(path.join(packageRoot, "package.json")).version;

function digestDirectory(directory) {
  return fs.readdirSync(directory)
    .filter((name) => name.endsWith(".svg"))
    .sort()
    .map((name) => `${name}:${crypto.createHash("sha256").update(fs.readFileSync(path.join(directory, name))).digest("hex")}`)
    .join("\n");
}

if (process.argv.includes("--check")) {
  const versionFile = path.join(target, "VERSION");
  if (!fs.existsSync(versionFile) || fs.readFileSync(versionFile, "utf8").trim() !== version) {
    throw new Error(`embedded Lucide assets are missing or stale; run pnpm sync-icons (expected ${version})`);
  }
  if (digestDirectory(sourceIcons) !== digestDirectory(targetIcons)) {
    throw new Error("embedded Lucide SVG assets do not match the pinned package; run pnpm sync-icons");
  }
  console.log(`Lucide ${version}: ${fs.readdirSync(sourceIcons).filter((name) => name.endsWith(".svg")).length} icons verified`);
} else {
  fs.rmSync(target, { recursive: true, force: true });
  fs.mkdirSync(target, { recursive: true });
  fs.cpSync(sourceIcons, targetIcons, { recursive: true });
  fs.writeFileSync(path.join(target, "VERSION"), `${version}\n`);
  const license = path.join(packageRoot, "LICENSE");
  if (fs.existsSync(license)) fs.copyFileSync(license, path.join(target, "LICENSE"));
  console.log(`Synced Lucide ${version} assets into ${path.relative(root, target)}`);
}
