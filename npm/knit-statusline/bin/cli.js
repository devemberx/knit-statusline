#!/usr/bin/env node
"use strict";

// Thin launcher for the knit-statusline binary.
//
// Only `npx @devemberx/knit-statusline` comes through here. That install
// copies the binary to ~/.claude and points settings.json at it, so renders
// run the binary directly -- Node's startup cost several times a second is
// most of why this project is not JavaScript.

const { spawnSync } = require("node:child_process");

// npm installs exactly one of these, selected by the "os" and "cpu" fields in
// each platform package.
const PACKAGE_BY_TARGET = {
  "darwin-arm64": "@devemberx/knit-statusline-darwin-arm64",
  "darwin-x64": "@devemberx/knit-statusline-darwin-x64",
  "linux-arm64": "@devemberx/knit-statusline-linux-arm64",
  "linux-x64": "@devemberx/knit-statusline-linux-x64",
  "win32-x64": "@devemberx/knit-statusline-win32-x64",
};

function fail(message) {
  console.error(`knit-statusline: ${message}`);
  process.exit(1);
}

function resolveBinary() {
  const target = `${process.platform}-${process.arch}`;
  const pkg = PACKAGE_BY_TARGET[target];

  if (!pkg) {
    fail(
      `no prebuilt binary for ${target}.\n` +
        `Supported: ${Object.keys(PACKAGE_BY_TARGET).join(", ")}\n` +
        `Build from source instead: go install github.com/devemberx/knit-statusline/cmd/statusline@latest`
    );
  }

  const exe =
    process.platform === "win32" ? "knit-statusline.exe" : "knit-statusline";
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch {
    // Optional dependencies fail quietly, so an absent package usually means
    // the install ran with --no-optional or behind a restricted registry.
    fail(
      `the ${pkg} package is missing.\n` +
        `It is an optional dependency, so it is skipped by --no-optional and by\n` +
        `registries that do not mirror it. Reinstall with optional dependencies\n` +
        `enabled, or install directly:  npm install ${pkg}`
    );
  }
}

// A bare `npx @devemberx/knit-statusline` means install -- nobody types it to
// render, and with no argument the binary waits on a stdin that never closes.
// A piped stdin is left alone, so a settings.json wired to npx still renders
// rather than reinstalling on every update.
const args = process.argv.slice(2);
if (args.length === 0 && process.stdin.isTTY) {
  args.push("install");
}

const result = spawnSync(resolveBinary(), args, {
  stdio: "inherit",
});

if (result.error) {
  fail(result.error.message);
}
process.exit(result.status === null ? 1 : result.status);
