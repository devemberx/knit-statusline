// Hermetic run of prepare-packages.mjs: whole npm/ tree copied into a temp
// repo, fake dist with artifacts.json, script spawned as a child. Script
// derive npmRoot from its own path, so the copy keep the real tree untouched.
//
// Run: node --test npm/scripts/

import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const realNpmDir = join(dirname(fileURLToPath(import.meta.url)), "..");

// Script refuse win32 outright (no exec bit), so every test would fail there.
const skip = process.platform === "win32";

// Mirror of TARGETS in prepare-packages.mjs.
const GO_TARGETS = [
  { pkg: "darwin-arm64", goos: "darwin", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "darwin-x64", goos: "darwin", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "linux-arm64", goos: "linux", goarch: "arm64", exe: "knit-statusline" },
  { pkg: "linux-x64", goos: "linux", goarch: "amd64", exe: "knit-statusline" },
  { pkg: "win32-arm64", goos: "windows", goarch: "arm64", exe: "knit-statusline.exe" },
  { pkg: "win32-x64", goos: "windows", goarch: "amd64", exe: "knit-statusline.exe" },
];

function makeRepo(t) {
  const root = mkdtempSync(join(tmpdir(), "knit-stage-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));

  cpSync(join(realNpmDir), join(root, "npm"), { recursive: true });
  writeFileSync(join(root, "README.md"), "# readme fixture\n");
  writeFileSync(join(root, "LICENSE"), "license fixture\n");

  const artifacts = [];
  for (const { goos, goarch, exe } of GO_TARGETS) {
    const dir = join(root, "dist", `knit-statusline_${goos}_${goarch}`);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, exe), `binary ${goos}/${goarch}\n`);
    artifacts.push({
      name: exe,
      // Relative to goreleaser cwd = dist parent, exactly as goreleaser write it.
      path: `dist/knit-statusline_${goos}_${goarch}/${exe}`,
      goos,
      goarch,
      type: "Binary",
      extra: { ID: "knit-statusline" },
    });
  }
  // Non-Binary rows sit beside binaries in real artifacts.json; filter must
  // pass over them.
  artifacts.push({ name: "checksums.txt", path: "dist/checksums.txt", type: "Checksum" });
  artifacts.push({
    name: "knit-statusline_1.2.3_linux_amd64.tar.gz",
    path: "dist/knit-statusline_1.2.3_linux_amd64.tar.gz",
    goos: "linux",
    goarch: "amd64",
    type: "Archive",
    extra: { ID: "default" },
  });
  // Binary from another build id on a claimed target: filter dropping the ID
  // clause turn this into an ambiguous pair.
  artifacts.push({
    name: "other-tool",
    path: "dist/other-tool_linux_amd64/other-tool",
    goos: "linux",
    goarch: "amd64",
    type: "Binary",
    extra: { ID: "other-tool" },
  });
  writeArtifacts(root, artifacts);
  return root;
}

function writeArtifacts(root, artifacts) {
  writeFileSync(join(root, "dist", "artifacts.json"), JSON.stringify(artifacts, null, 2));
}

function readArtifacts(root) {
  return JSON.parse(readFileSync(join(root, "dist", "artifacts.json"), "utf8"));
}

function run(root, args, cwd = root) {
  return spawnSync(
    process.execPath,
    [join(root, "npm", "scripts", "prepare-packages.mjs"), ...args],
    { cwd, encoding: "utf8" },
  );
}

function readManifest(root, ...parts) {
  return JSON.parse(readFileSync(join(root, "npm", ...parts, "package.json"), "utf8"));
}

// Fixture copy the real npm/ tree, so baselines read from the copy rather than
// assume a literal placeholder.
function platformVersions(root) {
  return Object.fromEntries(
    GO_TARGETS.map(({ pkg }) => [pkg, readManifest(root, "platforms", pkg).version]),
  );
}

test("stages every package from a v-prefixed tag", { skip }, (t) => {
  const root = makeRepo(t);
  const result = run(root, ["v1.2.3", "dist"]);
  assert.equal(result.status, 0, result.stderr);

  for (const { pkg, goos, goarch, exe } of GO_TARGETS) {
    assert.equal(readManifest(root, "platforms", pkg).version, "1.2.3");
    const bin = join(root, "npm", "platforms", pkg, "bin", exe);
    assert.equal(readFileSync(bin, "utf8"), `binary ${goos}/${goarch}\n`);
    assert.equal(statSync(bin).mode & 0o777, 0o755);
    assert.equal(
      readFileSync(join(root, "npm", "platforms", pkg, "LICENSE"), "utf8"),
      "license fixture\n",
    );
  }

  const launcher = readManifest(root, "knit-statusline");
  assert.equal(launcher.version, "1.2.3");
  for (const value of Object.values(launcher.optionalDependencies)) {
    assert.equal(value, "1.2.3");
  }
  assert.equal(
    readFileSync(join(root, "npm", "knit-statusline", "README.md"), "utf8"),
    "# readme fixture\n",
  );
  assert.equal(
    readFileSync(join(root, "npm", "knit-statusline", "LICENSE"), "utf8"),
    "license fixture\n",
  );
});

test("accepts a prerelease version", { skip }, (t) => {
  const root = makeRepo(t);
  const result = run(root, ["v1.2.3-rc.1", "dist"]);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readManifest(root, "knit-statusline").version, "1.2.3-rc.1");
});

test("rejects a version that is not semver", { skip }, (t) => {
  const root = makeRepo(t);
  const launcherBefore = readManifest(root, "knit-statusline").version;
  // Build metadata in the reject list: npm publish strip it with a warning, so
  // registry version diverge from tag and pins.
  for (const bad of ["vfoo", "v1.2", "1.2.3.4", "v1.2.3 ", "v1.2.3+abc"]) {
    const result = run(root, [bad, "dist"]);
    assert.equal(result.status, 1, `${bad}: ${result.stderr}`);
    assert.match(result.stderr, /version/);
  }
  // Nothing staged on reject.
  assert.equal(readManifest(root, "knit-statusline").version, launcherBefore);
});

test("resolves artifact paths when run from another directory", { skip }, (t) => {
  const root = makeRepo(t);
  const elsewhere = join(root, "elsewhere");
  mkdirSync(elsewhere);
  const result = run(root, ["v2.0.0", join(root, "dist")], elsewhere);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readManifest(root, "platforms", "linux-x64").version, "2.0.0");
});

test("fails when artifacts.json is missing", { skip }, (t) => {
  const root = makeRepo(t);
  rmSync(join(root, "dist", "artifacts.json"));
  const result = run(root, ["v1.2.3", "dist"]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /run goreleaser before/);
});

test("fails when a target has no build output", { skip }, (t) => {
  const root = makeRepo(t);
  writeArtifacts(
    root,
    readArtifacts(root).filter((a) => !(a.type === "Binary" && a.goarch === "arm64" && a.goos === "linux")),
  );
  const before = platformVersions(root);
  const result = run(root, ["v1.2.3", "dist"]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /missing build output for linux\/arm64/);
  // Guard fire before any stamp: failed run else leave half-stamped tree.
  assert.deepEqual(platformVersions(root), before);
});

test("fails on two binaries for one target", { skip }, (t) => {
  const root = makeRepo(t);
  const artifacts = readArtifacts(root);
  artifacts.push(artifacts.find((a) => a.type === "Binary" && a.goos === "linux" && a.goarch === "amd64"));
  writeArtifacts(root, artifacts);
  const result = run(root, ["v1.2.3", "dist"]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /ambiguous/);
});

test("fails when launcher optionalDependencies drift from TARGETS", { skip }, (t) => {
  for (const mutate of [
    (deps) => delete deps["@devemberx/knit-statusline-linux-x64"],
    (deps) => (deps["@devemberx/knit-statusline-freebsd-x64"] = "0.0.0"),
    // Same length, different key: membership check alone catch this one.
    (deps) => {
      delete deps["@devemberx/knit-statusline-linux-x64"];
      deps["@devemberx/knit-statusline-freebsd-x64"] = "0.0.0";
    },
  ]) {
    const root = makeRepo(t);
    const launcherPath = join(root, "npm", "knit-statusline", "package.json");
    const manifest = JSON.parse(readFileSync(launcherPath, "utf8"));
    mutate(manifest.optionalDependencies);
    writeFileSync(launcherPath, JSON.stringify(manifest, null, 2) + "\n");
    const before = platformVersions(root);
    const result = run(root, ["v1.2.3", "dist"]);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /optionalDependencies do not match/);
    // Drift guard fire before platform stamping too.
    assert.deepEqual(platformVersions(root), before);
  }
});

test("fails when the repo README or LICENSE is missing", { skip }, (t) => {
  for (const file of ["README.md", "LICENSE"]) {
    const root = makeRepo(t);
    rmSync(join(root, file));
    const result = run(root, ["v1.2.3", "dist"]);
    assert.equal(result.status, 1, `${file}: staged without it`);
    assert.match(result.stderr, new RegExp(file.replace(".", "\\.")));
  }
});
