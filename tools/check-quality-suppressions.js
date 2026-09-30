#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");

const ignoredDirectories = new Set([
  ".git",
  "node_modules",
  "vendor",
  "bin",
  "dist",
]);

const goSuppressionRules = [
  { label: "golangci-lint nolint directive", pattern: /\/\/\s*nolint(?:lint)?\b|\/\*\s*nolint(?:lint)?\b/i },
  { label: "staticcheck lint ignore directive", pattern: /\/\/\s*lint:(?:file-)?ignore\b/i },
  { label: "gosec nosec directive", pattern: /\/\/\s*#?nosec\b/i },
  { label: "ignored Go build constraint", pattern: /^\s*\/\/\s*(?:go:build\s+ignore\b|\+build\s+ignore\b)/i },
  { label: "race-disabled Go build constraint", pattern: /^\s*\/\/\s*(?:go:build|\+build)\b.*(?:^|[\s&|()])!race\b/i },
  { label: "test skip", pattern: /\bt\s*\.\s*Skip(?:f|Now)?\s*\(/ },
];

const golangciConfigRules = [
  { label: "linter disabling or exclusion setting", pattern: /^\s*(?:disable|disable-all|enable-only|exclude-rules|exclude|exclusions|skip-dirs|skip-files)\s*:/i },
  { label: "golangci-lint default disables linters", pattern: /^\s*default\s*:\s*(?:none|false|off)\b/i },
  { label: "golangci-lint excludes tests", pattern: /^\s*tests\s*:\s*false\b/i },
  { label: "golangci-lint checks only new issues", pattern: /^\s*new\s*:\s*true\b/i },
];

const automationRules = [
  {
    label: "golangci-lint suppression flag",
    pattern: /--(?:disable(?:-all)?|enable-only|exclude|skip-dirs|skip-files|new(?:-from-(?:rev|patch))?|config)\b|--issues-exit-code(?:=|\s+)0\b|--tests=false\b/i,
    requires: /golangci-lint/i,
  },
  {
    label: "filtered or vet-disabled Go test command",
    pattern: /\bgo\s+test\b.*\s-(?:run|skip|short|vet=off)(?:=|\s|$)/i,
  },
  {
    label: "Go vet analyzer disabled",
    pattern: /\bgo\s+vet\b.*\s-[\w-]+=false(?:\s|$)/i,
  },
  {
    label: "CI step allowed to continue after failure",
    pattern: /^\s*continue-on-error\s*:\s*(?!false\b)\S+/i,
  },
  {
    label: "quality command failure can be swallowed by shell handling",
    pattern: /(?:golangci-lint|go\s+(?:test|vet)|make\s+(?:ci|check|quality))[^\n]*(?:\|\||\|\s*(?:tee|cat|sed|awk)\b|;\s*(?:true|:)(?:\s|$))/i,
  },
  {
    label: "shell disables fail-fast handling",
    pattern: /^\s*set\s+\+e(?:\s|$)/,
  },
  {
    label: "Make recipe ignores a quality command failure",
    pattern: /^\s*-\s*(?:golangci-lint|go\s+(?:test|vet)|make\s+(?:ci|check|quality))/i,
  },
];

function relativeName(root, filename) {
  return path.relative(root, filename).split(path.sep).join("/");
}

function inspectText(root, filename, content, rules, findings, filter = () => true) {
  const lines = content.split(/\r?\n/);
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (!filter(line)) continue;
    for (const rule of rules) {
      if (!rule.requires || rule.requires.test(line)) {
        if (rule.pattern.test(line)) {
          findings.push({
            file: relativeName(root, filename),
            line: index + 1,
            rule: rule.label,
            source: line.trim(),
          });
          break;
        }
      }
    }
  }
}

function isAutomationFile(relativePath) {
  const name = path.posix.basename(relativePath);
  return (
    name === "Makefile" ||
    name === "package.json" ||
    name === "Dockerfile" ||
    /\.(?:sh|bash)$/i.test(name) ||
    relativePath.startsWith(".forgejo/workflows/") ||
    relativePath.startsWith(".github/workflows/")
  );
}

function findViolations(root) {
  const findings = [];
  let inspectedGoFiles = 0;
  let inspectedAutomationFiles = 0;
  let inspectedLinterConfigs = 0;

  function visit(directory) {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      if (entry.isDirectory() && ignoredDirectories.has(entry.name)) continue;
      const filename = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(filename);
        continue;
      }
      if (!entry.isFile()) continue;

      const relativePath = relativeName(root, filename);
      const content = fs.readFileSync(filename, "utf8");
      if (entry.name.endsWith(".go")) {
        inspectedGoFiles += 1;
        inspectText(root, filename, content, goSuppressionRules, findings);
      }
      if (/^\.golangci(?:\.[^.]+)?\.ya?ml$/i.test(entry.name)) {
        inspectedLinterConfigs += 1;
        inspectText(root, filename, content, golangciConfigRules, findings, (line) => !/^\s*#/u.test(line));
      }
      if (isAutomationFile(relativePath)) {
        inspectedAutomationFiles += 1;
        inspectText(root, filename, content, automationRules, findings);
      }
    }
  }

  visit(root);
  return { findings, inspectedGoFiles, inspectedAutomationFiles, inspectedLinterConfigs };
}

function main() {
  const root = path.resolve(process.argv[2] || process.cwd());
  const result = findViolations(root);
  if (result.findings.length > 0) {
    for (const finding of result.findings) {
      process.stderr.write(`${finding.file}:${finding.line}: ${finding.rule}: ${finding.source}\n`);
    }
    process.stderr.write(`Found ${result.findings.length} quality-check suppression(s).\n`);
    process.exitCode = 1;
    return;
  }
  process.stdout.write(
    `Quality suppression scan passed (${result.inspectedGoFiles} Go files, ${result.inspectedLinterConfigs} linter configs, ${result.inspectedAutomationFiles} automation files).\n`,
  );
}

if (require.main === module) main();

module.exports = { findViolations };
