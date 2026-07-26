#!/usr/bin/env node
// Stage npm packages for a release.
//
// Stamp version into every package.json -- launcher optionalDependencies
// included, which must match exactly or npm resolve nothing -- and copy each
// compiled binary into its platform package.
//
// Usage: node npm/scripts/prepare-packages.mjs <version> <goreleaser-dist-dir>

import { chmodSync, copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const [, , rawVersion, distDir] = process.argv;
if (!rawVersion || !distDir) {
  console.error("usage: prepare-packages.mjs <version> <goreleaser-dist-dir>");
  process.exit(2);
}

// Tags carry leading v; npm versions do not.
const version = rawVersion.replace(/^v/, "");
const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(npmRoot, "..");

// npm platform package keyed to its Go (goos, goarch) target. GoReleaser suffix
// each build-output dir with microarch level (amd64 _v1, arm64 _v8.0) and bump
// that default over time, so resolve real path from artifacts.json by
// goos+goarch instead of reconstructing dir name.
const TARGETS = [
  { pkg: "darwin-arm64", goos: "darwin", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "darwin-x64", goos: "darwin", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "linux-arm64", goos: "linux", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "linux-x64", goos: "linux", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "win32-x64", goos: "windows", goarch: "amd64", exe: "knit-statusline.exe" },
];

// GoReleaser record every built artifact and its real path here, so microarch
// suffix never hardcoded.
const artifactsPath = join(distDir, "artifacts.json");
if (!existsSync(artifactsPath)) {
  console.error(`missing ${artifactsPath}: run goreleaser before this script`);
  process.exit(1);
}
const artifacts = JSON.parse(readFileSync(artifactsPath, "utf8"));

function binaryPath(goos, goarch) {
  const hit = artifacts.find(
    (a) => a.type === "Binary" && a.goos === goos && a.goarch === goarch,
  );
  return hit ? hit.path : null;
}

function patchJSON(path, mutate) {
  const parsed = JSON.parse(readFileSync(path, "utf8"));
  mutate(parsed);
  writeFileSync(path, JSON.stringify(parsed, null, 2) + "\n");
}

for (const { pkg, goos, goarch, exe } of TARGETS) {
  const packageDir = join(npmRoot, "platforms", pkg);
  patchJSON(join(packageDir, "package.json"), (json) => {
    json.version = version;
  });

  const built = binaryPath(goos, goarch);
  if (!built || !existsSync(built)) {
    console.error(`missing build output for ${goos}/${goarch} (${pkg})`);
    process.exit(1);
  }

  const dest = join(packageDir, "bin", exe);
  copyFileSync(built, dest);
  chmodSync(dest, 0o755);
  console.log(`${pkg}: ${dest}`);
}

const launcherDir = join(npmRoot, "knit-statusline");

patchJSON(join(launcherDir, "package.json"), (json) => {
  json.version = version;
  for (const { pkg } of TARGETS) {
    // Pinned exactly: a range here let npm pair new launcher with stale binary.
    json.optionalDependencies[`@devemberx/knit-statusline-${pkg}`] = version;
  }
});

// Launcher `files` list README.md, and npm silently drop a missing entry --
// package page ship blank. Copy it in rather than keep a second copy in git.
const readme = join(repoRoot, "README.md");
if (!existsSync(readme)) {
  console.error(`missing ${readme}: launcher would publish without a description`);
  process.exit(1);
}
copyFileSync(readme, join(launcherDir, "README.md"));

console.log(`staged knit-statusline ${version}`);
