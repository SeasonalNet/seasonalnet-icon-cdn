"use strict";

const fs = require("fs");
const path = require("path");

let YAML;
try {
  YAML = require("yaml");
} catch (err) {
  console.error("[cdn] FATAL: yaml is not installed. Run: npm install");
  process.exit(1);
}

const DEFAULT_CONFIG = {
  server: {
    host: "127.0.0.1",
    port: 3600,
  },
  render: {
    size: 64,
    icons_dir: null,
  },
  cache: {
    dir: "./cache",
    namespace: "auto",
    http_max_age_seconds: 604800,
    immutable: true,
    cleanup: {
      enabled: true,
      on_startup: true,
      interval_ms: 3600000,
      max_age_days: 180,
      max_files: 25000,
      max_bytes: 104857600,
      tmp_max_age_minutes: 30,
    },
  },
};

function isPlainObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function deepMerge(base, override) {
  const merged = { ...base };
  for (const [key, value] of Object.entries(override || {})) {
    if (isPlainObject(value) && isPlainObject(base[key])) {
      merged[key] = deepMerge(base[key], value);
    } else if (value !== undefined) {
      merged[key] = value;
    }
  }
  return merged;
}

function parseInteger(value, name, { min = 0, allowNull = false } = {}) {
  if (value === null && allowNull) return null;

  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || String(value).trim() === "") {
    throw new Error(`${name} must be an integer`);
  }
  if (parsed < min) {
    throw new Error(`${name} must be >= ${min}`);
  }
  return parsed;
}

function parseBoolean(value, name) {
  if (typeof value === "boolean") return value;
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase();
    if (["1", "true", "yes", "on"].includes(normalized)) return true;
    if (["0", "false", "no", "off"].includes(normalized)) return false;
  }
  throw new Error(`${name} must be a boolean`);
}

function applyEnvOverrides(config, env) {
  const next = deepMerge(DEFAULT_CONFIG, config);

  if (env.CDN_HOST !== undefined) next.server.host = env.CDN_HOST;
  if (env.CDN_PORT !== undefined) next.server.port = parseInteger(env.CDN_PORT, "CDN_PORT", { min: 1 });
  if (env.CDN_SIZE !== undefined) next.render.size = parseInteger(env.CDN_SIZE, "CDN_SIZE", { min: 1 });
  if (env.CDN_ICONS_DIR !== undefined) next.render.icons_dir = env.CDN_ICONS_DIR;

  if (env.CDN_CACHE_DIR !== undefined) next.cache.dir = env.CDN_CACHE_DIR;
  if (env.CDN_CACHE_NAMESPACE !== undefined) next.cache.namespace = env.CDN_CACHE_NAMESPACE;
  if (env.CDN_CACHE_HTTP_MAX_AGE_SECONDS !== undefined) {
    next.cache.http_max_age_seconds = parseInteger(
      env.CDN_CACHE_HTTP_MAX_AGE_SECONDS,
      "CDN_CACHE_HTTP_MAX_AGE_SECONDS",
      { min: 0 },
    );
  }
  if (env.CDN_CACHE_IMMUTABLE !== undefined) {
    next.cache.immutable = parseBoolean(env.CDN_CACHE_IMMUTABLE, "CDN_CACHE_IMMUTABLE");
  }

  const cleanup = next.cache.cleanup;
  if (env.CDN_CACHE_CLEANUP_ENABLED !== undefined) {
    cleanup.enabled = parseBoolean(env.CDN_CACHE_CLEANUP_ENABLED, "CDN_CACHE_CLEANUP_ENABLED");
  }
  if (env.CDN_CACHE_CLEAN_ON_STARTUP !== undefined) {
    cleanup.on_startup = parseBoolean(env.CDN_CACHE_CLEAN_ON_STARTUP, "CDN_CACHE_CLEAN_ON_STARTUP");
  }
  if (env.CDN_CACHE_CLEAN_INTERVAL_MS !== undefined) {
    cleanup.interval_ms = parseInteger(env.CDN_CACHE_CLEAN_INTERVAL_MS, "CDN_CACHE_CLEAN_INTERVAL_MS", { min: 0 });
  }
  if (env.CDN_CACHE_MAX_AGE_DAYS !== undefined) {
    cleanup.max_age_days = env.CDN_CACHE_MAX_AGE_DAYS.trim().toLowerCase() === "null"
      ? null
      : parseInteger(env.CDN_CACHE_MAX_AGE_DAYS, "CDN_CACHE_MAX_AGE_DAYS", { min: 0 });
  }
  if (env.CDN_CACHE_MAX_FILES !== undefined) {
    cleanup.max_files = env.CDN_CACHE_MAX_FILES.trim().toLowerCase() === "null"
      ? null
      : parseInteger(env.CDN_CACHE_MAX_FILES, "CDN_CACHE_MAX_FILES", { min: 0 });
  }
  if (env.CDN_CACHE_MAX_BYTES !== undefined) {
    cleanup.max_bytes = env.CDN_CACHE_MAX_BYTES.trim().toLowerCase() === "null"
      ? null
      : parseInteger(env.CDN_CACHE_MAX_BYTES, "CDN_CACHE_MAX_BYTES", { min: 0 });
  }
  if (env.CDN_CACHE_TMP_MAX_AGE_MINUTES !== undefined) {
    cleanup.tmp_max_age_minutes = parseInteger(
      env.CDN_CACHE_TMP_MAX_AGE_MINUTES,
      "CDN_CACHE_TMP_MAX_AGE_MINUTES",
      { min: 0 },
    );
  }

  return next;
}

function resolveMaybeRelativePath(value, baseDir) {
  if (!value) return value;
  return path.isAbsolute(value) ? value : path.resolve(baseDir, value);
}

function readPackageJson(filePath) {
  try {
    return JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch (_) {
    return null;
  }
}

function sanitizeNamespace(value) {
  return String(value)
    .trim()
    .replace(/^\^/, "")
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "") || "default";
}

function detectLucideVersion(appRoot) {
  const installedPackage = readPackageJson(path.join(appRoot, "node_modules", "lucide-static", "package.json"));
  if (installedPackage?.version) return installedPackage.version;

  const projectPackage = readPackageJson(path.join(appRoot, "package.json"));
  const declared = projectPackage?.dependencies?.["lucide-static"];
  if (declared) return declared;

  return "unknown";
}

function validateConfig(config) {
  config.server.port = parseInteger(config.server.port, "server.port", { min: 1 });
  config.render.size = parseInteger(config.render.size, "render.size", { min: 1 });
  config.cache.http_max_age_seconds = parseInteger(
    config.cache.http_max_age_seconds,
    "cache.http_max_age_seconds",
    { min: 0 },
  );

  config.cache.immutable = parseBoolean(config.cache.immutable, "cache.immutable");
  config.cache.cleanup.enabled = parseBoolean(config.cache.cleanup.enabled, "cache.cleanup.enabled");
  config.cache.cleanup.on_startup = parseBoolean(config.cache.cleanup.on_startup, "cache.cleanup.on_startup");
  config.cache.cleanup.interval_ms = parseInteger(config.cache.cleanup.interval_ms, "cache.cleanup.interval_ms", { min: 0 });
  config.cache.cleanup.max_age_days = parseInteger(config.cache.cleanup.max_age_days, "cache.cleanup.max_age_days", {
    min: 0,
    allowNull: true,
  });
  config.cache.cleanup.max_files = parseInteger(config.cache.cleanup.max_files, "cache.cleanup.max_files", {
    min: 0,
    allowNull: true,
  });
  config.cache.cleanup.max_bytes = parseInteger(config.cache.cleanup.max_bytes, "cache.cleanup.max_bytes", {
    min: 0,
    allowNull: true,
  });
  config.cache.cleanup.tmp_max_age_minutes = parseInteger(
    config.cache.cleanup.tmp_max_age_minutes,
    "cache.cleanup.tmp_max_age_minutes",
    { min: 0 },
  );

  return config;
}

function loadYamlConfig(configPath) {
  const exists = fs.existsSync(configPath);
  if (!exists) {
    return { exists, data: {} };
  }

  try {
    const parsed = YAML.parse(fs.readFileSync(configPath, "utf8"));
    return { exists, data: parsed || {} };
  } catch (err) {
    throw new Error(`failed to parse ${configPath}: ${err.message}`);
  }
}

function loadConfig({ appRoot, env = process.env } = {}) {
  if (!appRoot) throw new Error("appRoot is required");

  const configPath = env.CDN_CONFIG
    ? resolveMaybeRelativePath(env.CDN_CONFIG, appRoot)
    : path.join(appRoot, "config.yaml");
  const configDir = path.dirname(configPath);

  const fileConfig = loadYamlConfig(configPath);
  const merged = applyEnvOverrides(fileConfig.data, env);
  const config = validateConfig(merged);

  config.config_path = configPath;
  config.config_exists = fileConfig.exists;
  config.render.icons_dir = resolveMaybeRelativePath(
    config.render.icons_dir || path.join("node_modules", "lucide-static", "icons"),
    appRoot,
  );
  config.cache.dir = resolveMaybeRelativePath(config.cache.dir, configDir);

  const lucideVersion = detectLucideVersion(appRoot);
  const namespace = config.cache.namespace === "auto"
    ? `lucide-${lucideVersion}`
    : config.cache.namespace;
  config.cache.resolved_namespace = sanitizeNamespace(namespace);
  config.cache.versioned_dir = path.join(
    config.cache.dir,
    config.cache.resolved_namespace,
    `size-${config.render.size}`,
  );

  return config;
}

module.exports = {
  DEFAULT_CONFIG,
  loadConfig,
  sanitizeNamespace,
};
