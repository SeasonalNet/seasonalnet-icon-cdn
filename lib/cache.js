"use strict";

const fs = require("fs");
const fsp = require("fs/promises");
const path = require("path");
const crypto = require("crypto");

function safeCacheName(icon, hex) {
  return `${icon}-${hex.toUpperCase()}.png`;
}

function buildCacheControl(config) {
  const parts = ["public", `max-age=${config.cache.http_max_age_seconds}`];
  if (config.cache.immutable) parts.push("immutable");
  return parts.join(", ");
}

async function pathExists(filePath) {
  try {
    await fsp.access(filePath, fs.constants.F_OK);
    return true;
  } catch (_) {
    return false;
  }
}

async function walkFiles(rootDir) {
  const files = [];

  async function walk(dir) {
    let entries;
    try {
      entries = await fsp.readdir(dir, { withFileTypes: true });
    } catch (err) {
      if (err.code === "ENOENT") return;
      throw err;
    }

    for (const entry of entries) {
      const entryPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        await walk(entryPath);
      } else if (entry.isFile()) {
        files.push(entryPath);
      }
    }
  }

  await walk(rootDir);
  return files;
}

async function removeFile(filePath) {
  try {
    await fsp.unlink(filePath);
    return true;
  } catch (err) {
    if (err.code === "ENOENT") return false;
    throw err;
  }
}

async function removeEmptyDirs(rootDir) {
  async function walk(dir) {
    let entries;
    try {
      entries = await fsp.readdir(dir, { withFileTypes: true });
    } catch (err) {
      if (err.code === "ENOENT") return;
      throw err;
    }

    for (const entry of entries) {
      if (entry.isDirectory()) {
        await walk(path.join(dir, entry.name));
      }
    }

    if (dir !== rootDir) {
      try {
        await fsp.rmdir(dir);
      } catch (_) {
        // Directory was not empty or disappeared. Both are fine for cleanup.
      }
    }
  }

  await walk(rootDir);
}

class DiskCache {
  constructor(config) {
    this.config = config;
    this.rootDir = config.cache.dir;
    this.activeDir = config.cache.versioned_dir;
    this.cacheControl = buildCacheControl(config);
    this.cleanupRunning = false;
  }

  async ensure() {
    await fsp.mkdir(this.activeDir, { recursive: true });
  }

  getPath(icon, hex) {
    return path.join(this.activeDir, safeCacheName(icon, hex));
  }

  async read(icon, hex) {
    const cachePath = this.getPath(icon, hex);
    try {
      return await fsp.readFile(cachePath);
    } catch (err) {
      if (err.code === "ENOENT") return null;
      throw err;
    }
  }

  async write(icon, hex, data) {
    const cachePath = this.getPath(icon, hex);
    await fsp.mkdir(path.dirname(cachePath), { recursive: true });

    const tmpPath = `${cachePath}.${process.pid}.${Date.now()}.${crypto.randomBytes(6).toString("hex")}.tmp`;
    try {
      await fsp.writeFile(tmpPath, data);
      await fsp.rename(tmpPath, cachePath);
    } catch (err) {
      try { await fsp.unlink(tmpPath); } catch (_) {}
      throw err;
    }
  }

  async cleanup(reason = "scheduled") {
    const cleanup = this.config.cache.cleanup;
    if (!cleanup.enabled || this.cleanupRunning) return null;

    this.cleanupRunning = true;
    const now = Date.now();
    const stats = {
      reason,
      scanned: 0,
      removed_tmp: 0,
      removed_stale: 0,
      removed_over_limit: 0,
      remaining_files: 0,
      remaining_bytes: 0,
    };

    try {
      if (!(await pathExists(this.rootDir))) return stats;

      const files = await walkFiles(this.rootDir);
      const pngFiles = [];
      const maxAgeMs = cleanup.max_age_days === null ? null : cleanup.max_age_days * 24 * 60 * 60 * 1000;
      const tmpMaxAgeMs = cleanup.tmp_max_age_minutes * 60 * 1000;

      for (const filePath of files) {
        stats.scanned += 1;

        let stat;
        try {
          stat = await fsp.stat(filePath);
        } catch (err) {
          if (err.code === "ENOENT") continue;
          throw err;
        }

        if (filePath.endsWith(".tmp")) {
          if (now - stat.mtimeMs >= tmpMaxAgeMs && await removeFile(filePath)) {
            stats.removed_tmp += 1;
          }
          continue;
        }

        if (!filePath.endsWith(".png")) continue;

        if (maxAgeMs !== null && now - stat.mtimeMs >= maxAgeMs) {
          if (await removeFile(filePath)) {
            stats.removed_stale += 1;
          }
          continue;
        }

        pngFiles.push({ path: filePath, size: stat.size, mtimeMs: stat.mtimeMs });
      }

      pngFiles.sort((a, b) => a.mtimeMs - b.mtimeMs);
      let totalBytes = pngFiles.reduce((sum, file) => sum + file.size, 0);
      let totalFiles = pngFiles.length;

      const maxFiles = cleanup.max_files;
      const maxBytes = cleanup.max_bytes;
      for (const file of pngFiles) {
        const overFileLimit = maxFiles !== null && totalFiles > maxFiles;
        const overByteLimit = maxBytes !== null && totalBytes > maxBytes;
        if (!overFileLimit && !overByteLimit) break;

        if (await removeFile(file.path)) {
          totalFiles -= 1;
          totalBytes -= file.size;
          stats.removed_over_limit += 1;
        }
      }

      stats.remaining_files = totalFiles;
      stats.remaining_bytes = Math.max(0, totalBytes);

      await removeEmptyDirs(this.rootDir);
      return stats;
    } finally {
      this.cleanupRunning = false;
    }
  }

  startCleanupTimer(logger = console) {
    const cleanup = this.config.cache.cleanup;
    if (!cleanup.enabled || cleanup.interval_ms <= 0) return null;

    const timer = setInterval(() => {
      this.cleanup("scheduled")
        .then(stats => {
          if (!stats) return;
          const removed = stats.removed_tmp + stats.removed_stale + stats.removed_over_limit;
          if (removed > 0) {
            logger.log(
              `[cdn] cache cleanup removed ${removed} files ` +
              `(tmp=${stats.removed_tmp}, stale=${stats.removed_stale}, over_limit=${stats.removed_over_limit})`,
            );
          }
        })
        .catch(err => logger.error("[cdn] cache cleanup error:", err.message));
    }, cleanup.interval_ms);

    if (typeof timer.unref === "function") timer.unref();
    return timer;
  }
}

module.exports = {
  DiskCache,
  buildCacheControl,
  safeCacheName,
};
