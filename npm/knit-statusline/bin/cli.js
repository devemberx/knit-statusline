#!/usr/bin/env node
"use strict";

// Thin launcher: only `npx @devemberx/knit-statusline` come through here. That
// install copy the binary to ~/.claude and point settings.json at it, so render
// run the binary direct -- Node startup cost several times a second is most of
// why this project is not JavaScript.

const { spawnSync } = require("node:child_process");

// npm install exactly one platform package, picked by "os" and "cpu" in each.
// Supported set read from own manifest: prepare-packages.mjs hold these keys
// against its TARGETS, so a target list repeated here drift silently -- package
// installed, launcher still call it unsupported.
const PACKAGE_PREFIX = "@devemberx/knit-statusline-";
const PLATFORM_PACKAGES = Object.keys(
  require("../package.json").optionalDependencies ?? {},
);

function fail(message) {
  console.error(`knit-statusline: ${message}`);
  process.exit(1);
}

function resolveBinary() {
  const target = `${process.platform}-${process.arch}`;
  const pkg = `${PACKAGE_PREFIX}${target}`;

  if (!PLATFORM_PACKAGES.includes(pkg)) {
    const supported = PLATFORM_PACKAGES.map((name) =>
      name.slice(PACKAGE_PREFIX.length)
    ).join(", ");
    fail(
      `no prebuilt binary for ${target}.\n` +
        `Supported: ${supported}\n` +
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
