#!/usr/bin/env node
"use strict";

// Thin launcher: only `npx @devemberx/knit-statusline` come through here. That
// install copy the binary to ~/.claude and point settings.json at it, so render
// run the binary direct -- Node startup cost several times a second is most of
// why this project is not JavaScript.

const { spawnSync } = require("node:child_process");

// npm install exactly one, picked by "os" and "cpu" in each platform package.
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
    // Optional dependency fail quiet, so absent package mean --no-optional or a
    // registry that mirror nothing.
    fail(
      `the ${pkg} package is missing.\n` +
        `It is an optional dependency, so it is skipped by --no-optional and by\n` +
        `registries that do not mirror it. Reinstall with optional dependencies\n` +
        `enabled, or install directly:  npm install ${pkg}`
    );
  }
}

// Bare `npx @devemberx/knit-statusline` mean install: nobody type it to render,
// and with no argument the binary wait on a stdin that never close. Piped stdin
// left alone, so settings.json wired to npx render instead of reinstalling.
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
