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
// npm reject non-semver only at publish, seven manifests already stamped by then.
// Build metadata rejected too: npm publish strip it with a warning, so registry
// version diverge from tag and pins.
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$/.test(version)) {
  console.error(`not a semver version: ${rawVersion}`);
  process.exit(1);
}

const npmRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(npmRoot, "..");

// npm pack always include README, LICENSE and package.json; `files` cannot drop
// them. Missing file drop silent instead: package page publish blank, tarball
// carry no license text. Both copied in at stage time -- single source in git.
for (const name of ["README.md", "LICENSE"]) {
  if (!existsSync(join(repoRoot, name))) {
    console.error(`missing ${join(repoRoot, name)}: packages would publish without it`);
    process.exit(1);
  }
}

// npm platform package keyed to its Go (goos, goarch) target.
const TARGETS = [
  { pkg: "darwin-arm64", goos: "darwin", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "darwin-x64", goos: "darwin", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "linux-arm64", goos: "linux", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "linux-x64", goos: "linux", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "win32-arm64", goos: "windows", goarch: "arm64", exe: "knit-statusline.exe" },
  { pkg: "win32-x64", goos: "windows", goarch: "amd64", exe: "knit-statusline.exe" },
];

const BUILD_ID = "knit-statusline";

// GoReleaser record every artifact and its real path here. Read that path:
// build-output dir carry a microarch suffix (amd64 _v1, arm64 _v8.0) whose
// default move between releases.
const dist = resolve(distDir);
const artifactsPath = join(dist, "artifacts.json");
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
  // artifacts.json path relative to goreleaser cwd = dist parent, not to this
  // script's cwd. resolve pass an absolute path through untouched.
  return hits.length === 1 ? resolve(dirname(dist), hits[0].path) : null;
}

function patchJSON(path, mutate) {
  const parsed = JSON.parse(readFileSync(path, "utf8"));
  mutate(parsed);
  writeFileSync(path, JSON.stringify(parsed, null, 2) + "\n");
}

// Every guard fire before the first stamp: exit mid-loop else leave the tree
// half-stamped.
const binaries = new Map();
for (const { pkg, goos, goarch } of TARGETS) {
  const built = binaryPath(goos, goarch);
  if (!built || !existsSync(built)) {
    console.error(`missing build output for ${goos}/${goarch} (${pkg})`);
    process.exit(1);
  }
  binaries.set(pkg, built);
}

const launcherDir = join(npmRoot, "knit-statusline");
const launcherPath = join(launcherDir, "package.json");
const wanted = TARGETS.map(({ pkg }) => `@devemberx/knit-statusline-${pkg}`);
const declared = Object.keys(
  JSON.parse(readFileSync(launcherPath, "utf8")).optionalDependencies ?? {},
);

// Key only the loop below stamp. A key TARGETS dropped keep 0.0.0 and resolve
// to nothing, a target the launcher never declared install nothing at all --
// and an optional dependency fail quiet either way.
if (declared.length !== wanted.length || wanted.some((key) => !declared.includes(key))) {
  console.error(
    `launcher optionalDependencies do not match TARGETS: ${declared.join(", ") || "none"}`,
  );
  process.exit(1);
}

for (const { pkg, exe } of TARGETS) {
  const packageDir = join(npmRoot, "platforms", pkg);
  patchJSON(join(packageDir, "package.json"), (json) => {
    json.version = version;
  });

  const dest = join(packageDir, "bin", exe);
  copyFileSync(binaries.get(pkg), dest);
  chmodSync(dest, 0o755);
  copyFileSync(join(repoRoot, "LICENSE"), join(packageDir, "LICENSE"));
  console.log(`${pkg}: ${dest}`);
}

patchJSON(launcherPath, (json) => {
  json.version = version;
  for (const key of wanted) {
    // Pinned exact: a range let npm pair new launcher with stale binary.
    json.optionalDependencies[key] = version;
  }
});

copyFileSync(join(repoRoot, "README.md"), join(launcherDir, "README.md"));
copyFileSync(join(repoRoot, "LICENSE"), join(launcherDir, "LICENSE"));

console.log(`staged knit-statusline ${version}`);
