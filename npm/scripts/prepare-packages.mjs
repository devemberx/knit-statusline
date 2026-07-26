#!/usr/bin/env node
// Stage npm packages for a release: stamp version into every package.json and
// copy each compiled binary into its platform package.
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

// Windows carry no exec bit for chmod to set, so a package staged there ship a
// binary npm cannot run. Fail before publishing one.
if (process.platform === "win32") {
  console.error("stage on Linux or macOS: Windows drop the 0755 bit the binaries need");
  process.exit(1);
}

// Tags carry leading v; npm versions do not.
const version = rawVersion.replace(/^v/, "");
const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(npmRoot, "..");

// npm platform package keyed to its Go (goos, goarch) target.
const TARGETS = [
  { pkg: "darwin-arm64", goos: "darwin", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "darwin-x64", goos: "darwin", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "linux-arm64", goos: "linux", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "linux-x64", goos: "linux", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "win32-x64", goos: "windows", goarch: "amd64", exe: "knit-statusline.exe" },
];

const BUILD_ID = "knit-statusline";

// GoReleaser record every artifact and its real path here. Read that path:
// build-output dir carry a microarch suffix (amd64 _v1, arm64 _v8.0) whose
// default move between releases.
const artifactsPath = join(distDir, "artifacts.json");
if (!existsSync(artifactsPath)) {
  console.error(`missing ${artifactsPath}: run goreleaser before this script`);
  process.exit(1);
}
const artifacts = JSON.parse(readFileSync(artifactsPath, "utf8"));

// Build id narrow the match: a second build or a universal_binaries entry share
// goos and goarch, and first hit win silently.
function binaryPath(goos, goarch) {
  const hits = artifacts.filter(
    (a) =>
      a.type === "Binary" &&
      a.goos === goos &&
      a.goarch === goarch &&
      a.extra?.ID === BUILD_ID,
  );
  if (hits.length > 1) {
    console.error(`${hits.length} ${BUILD_ID} binaries for ${goos}/${goarch}: ambiguous`);
    process.exit(1);
  }
  return hits.length === 1 ? hits[0].path : null;
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
const launcherPath = join(launcherDir, "package.json");
const wanted = TARGETS.map(({ pkg }) => `@devemberx/knit-statusline-${pkg}`);
const declared = Object.keys(
  JSON.parse(readFileSync(launcherPath, "utf8")).optionalDependencies ?? {},
);

// Key only this loop stamp. A key TARGETS dropped keep 0.0.0 and resolve to
// nothing, a target the launcher never declared install nothing at all -- and an
// optional dependency fail quiet either way.
if (declared.length !== wanted.length || wanted.some((key) => !declared.includes(key))) {
  console.error(
    `launcher optionalDependencies do not match TARGETS: ${declared.join(", ") || "none"}`,
  );
  process.exit(1);
}

patchJSON(launcherPath, (json) => {
  json.version = version;
  for (const key of wanted) {
    // Pinned exact: a range let npm pair new launcher with stale binary.
    json.optionalDependencies[key] = version;
  }
});

// Launcher `files` list README.md and npm drop a missing entry silent, so
// package page ship blank. Copy it rather than keep a second copy in git.
const readme = join(repoRoot, "README.md");
if (!existsSync(readme)) {
  console.error(`missing ${readme}: launcher would publish without a description`);
  process.exit(1);
}
copyFileSync(readme, join(launcherDir, "README.md"));

console.log(`staged knit-statusline ${version}`);
