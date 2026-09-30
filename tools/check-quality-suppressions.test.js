"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { findViolations } = require("./check-quality-suppressions.js");

function withFixture(t, files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "icon-cdn-quality-scan-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const [filename, content] of Object.entries(files)) {
    const fullPath = path.join(root, filename);
    fs.mkdirSync(path.dirname(fullPath), { recursive: true });
    fs.writeFileSync(fullPath, content);
  }
  return root;
}

test("clean Go, linter, and CI configuration passes", (t) => {
  const root = withFixture(t, {
    "internal/sample.go": "package sample\n\nfunc value() int { return 1 }\n",
    ".golangci.yml": "version: \"2\"\nlinters:\n  default: standard\n",
    Makefile: "check:\n\tgo test ./...\n\tgo vet ./...\n\tgolangci-lint run ./...\n",
    ".forgejo/workflows/ci.yml": "steps:\n  - run: make ci\n  - continue-on-error: false\n",
  });
  const result = findViolations(root);
  assert.deepEqual(result.findings, []);
  assert.equal(result.inspectedGoFiles, 1);
  assert.equal(result.inspectedLinterConfigs, 1);
  assert.equal(result.inspectedAutomationFiles, 2);
});

test("Go suppression comments, ignored build constraints, and test skips are reported", (t) => {
  const root = withFixture(t, {
    "internal/sample.go": [
      "package sample",
      "//nolint:errcheck -- don't inspect this call",
      "//lint:ignore SA1000 bypass this warning",
      "// #nosec -- ignore this finding",
      "//go:build ignore",
      "//go:build !race",
      "func TestSkipped(t *testing.T) { t.Skip(\"not today\") }",
      "",
    ].join("\n"),
  });
  const result = findViolations(root);
  assert.equal(result.findings.length, 6);
  assert.deepEqual(
    result.findings.map((finding) => finding.rule),
    [
      "golangci-lint nolint directive",
      "staticcheck lint ignore directive",
      "gosec nosec directive",
      "ignored Go build constraint",
      "race-disabled Go build constraint",
      "test skip",
    ],
  );
  assert.ok(result.findings.every((finding) => finding.file === "internal/sample.go"));
});

test("golangci-lint exclusions and disabled lint settings are reported", (t) => {
  const root = withFixture(t, {
    ".golangci.yml": [
      "version: \"2\"",
      "linters:",
      "  default: none",
      "  disable: [errcheck]",
      "run:",
      "  tests: false",
      "issues:",
      "  new: true",
      "  exclude-rules: []",
      "",
    ].join("\n"),
  });
  const result = findViolations(root);
  assert.deepEqual(
    result.findings.map((finding) => finding.rule),
    [
      "golangci-lint default disables linters",
      "linter disabling or exclusion setting",
      "golangci-lint excludes tests",
      "golangci-lint checks only new issues",
      "linter disabling or exclusion setting",
    ],
  );
});

test("automation that weakens Go or golangci-lint checks is reported", (t) => {
  const root = withFixture(t, {
    Makefile: [
      "check:",
      "\tgolangci-lint run --disable=errcheck ./...",
      "\tgo test ./... -vet=off",
      "\tgo test ./... -run=^$",
      "\tgo vet ./... -printf=false",
      "\tgo test ./... || true",
      "\t-golangci-lint run ./...",
      "\tgo test ./... | tee /tmp/go-test.log",
      "\tgo test ./...; true",
      "",
    ].join("\n"),
    "tools/check.sh": "#!/bin/sh\nset +e\ngo test ./...\n",
  });
  const result = findViolations(root);
  assert.equal(result.findings.length, 9);
  assert.ok(result.findings.some((finding) => finding.rule === "golangci-lint suppression flag"));
  assert.ok(result.findings.some((finding) => finding.rule === "filtered or vet-disabled Go test command"));
  assert.ok(result.findings.some((finding) => finding.rule === "Go vet analyzer disabled"));
  assert.ok(result.findings.some((finding) => finding.rule === "quality command failure can be swallowed by shell handling"));
  assert.ok(result.findings.some((finding) => finding.rule === "shell disables fail-fast handling"));
  assert.ok(result.findings.some((finding) => finding.rule === "Make recipe ignores a quality command failure"));
});

test("CI steps configured to continue after quality failures are reported", (t) => {
  const root = withFixture(t, {
    ".forgejo/workflows/ci.yml": "steps:\n  - run: make ci\n    continue-on-error: true\n",
  });
  const result = findViolations(root);
  assert.equal(result.findings.length, 1);
  assert.equal(result.findings[0].rule, "CI step allowed to continue after failure");
});
