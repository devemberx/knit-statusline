package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/install"
	"github.com/devemberx/knit-statusline/internal/schema"
)

// parse decode one inline document, so a test state shape it exercise.
// Shared with main_test.go.
func parse(t *testing.T, doc string) *schema.Input {
	t.Helper()
	in, err := schema.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return in
}

// chdir move process into dir for one test, so workingDir() report it.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func writeProjectConfig(t *testing.T, project, body string) {
	t.Helper()
	path := config.ProjectPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// preview render sample data, so a config edit get checked with no Claude Code
// restart.
func TestPreviewRendersCompleteAndSparseData(t *testing.T) {
	isolate(t)
	t.Setenv("NO_COLOR", "1")

	var out, errOut bytes.Buffer
	if code := runPreview(nil, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	full := out.String()
	for _, want := range []string{"config:", "sample: complete data", "Opus 4.8", "current", "--sparse"} {
		if !strings.Contains(full, want) {
			t.Errorf("preview missing %q:\n%s", want, full)
		}
	}

	out.Reset()
	if code := runPreview([]string{"--sparse"}, &out, &errOut); code != 0 {
		t.Fatalf("sparse exit = %d", code)
	}
	sparse := out.String()
	if !strings.Contains(sparse, "degraded data") {
		t.Errorf("sparse preview missing its label:\n%s", sparse)
	}
	// Degraded case is where invented value would show first.
	if strings.Contains(sparse, "current ") || strings.Contains(sparse, "weekly ") {
		t.Errorf("sparse preview invented rate limits:\n%s", sparse)
	}
}

func TestPreviewDrawsANamedPreset(t *testing.T) {
	isolate(t)
	t.Setenv("NO_COLOR", "1")

	for _, name := range config.PresetNames() {
		var out, errOut bytes.Buffer
		if code := runPreview([]string{"--preset", name}, &out, &errOut); code != 0 {
			t.Errorf("preset %s exit = %d, stderr = %q", name, code, errOut.String())
		}
		if !strings.Contains(out.String(), "preset "+name) {
			t.Errorf("preset %s unlabelled:\n%s", name, out.String())
		}
	}
}

// Unknown preset must name what exist, so user need not go hunting.
func TestPreviewRejectsUnknownPreset(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runPreview([]string{"--preset", "nope"}, &out, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "available:") {
		t.Errorf("error should list presets: %q", out.String())
	}
}

func TestPreviewRejectsUnknownFlag(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runPreview([]string{"--nope"}, &out, &errOut); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

// preview draw what this directory render, project override included. Without
// it, a layout tuned per project is unpreviewable.
func TestPreviewAppliesTheProjectOverride(t *testing.T) {
	home := isolate(t)
	t.Setenv("NO_COLOR", "1")
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\", \"version\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\"]\n")
	chdir(t, project)

	var out, errOut bytes.Buffer
	if code := runPreview(nil, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, config.ProjectPath(project)) {
		t.Errorf("project override not among sources:\n%s", got)
	}
	if strings.Contains(got, "2.1.211") {
		t.Errorf("override should have dropped the version segment:\n%s", got)
	}
}

// doctor is where whatever need explaining get explained, since row hold marker
// alone.
func TestDoctorReportsPathsAndSegments(t *testing.T) {
	isolate(t)
	var out bytes.Buffer

	if code := runDoctor(nil, &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"knit-statusline", "Paths", "settings", "config", "cache",
		"Configuration", "sources", "rows", "status     ok", "Available segments",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	// Every field of every segment, so a mistyped template never send user
	// hunting elsewhere.
	for _, want := range []string{"model", "{name} {family}", "limit.5h", "command"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor omits %q:\n%s", want, got)
		}
	}
}

func TestDoctorMarksAbsentFiles(t *testing.T) {
	isolate(t)
	var out bytes.Buffer
	runDoctor(nil, &out)

	if !strings.Contains(out.String(), "(not present)") {
		t.Errorf("a fresh home should report missing files:\n%s", out.String())
	}
}

// Reporting problems is this command's job, so finding some is no failure.
// Exit stay zero and text carry verdict.
func TestDoctorReportsProblemsAndStillExitsZero(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, `
[[lines]]
segments = ["model", "no-such-segment"]

[segments.model]
template = "{nope}"
`)

	var out bytes.Buffer
	if code := runDoctor(nil, &out); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	got := out.String()
	for _, want := range []string{"unknown segment", "unknown field", "statusline.toml:"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "status     ok") {
		t.Errorf("doctor called a broken config ok:\n%s", got)
	}
}

// doctor read project override too, else a layout that only fail inside one
// project look clean from there.
func TestDoctorSeesTheProjectOverride(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\", \"no-such-segment\"]\n")
	chdir(t, project)

	var out bytes.Buffer
	runDoctor(nil, &out)

	got := out.String()
	if !strings.Contains(got, config.ProjectPath(project)) {
		t.Errorf("project path absent from doctor:\n%s", got)
	}
	if !strings.Contains(got, "unknown segment") {
		t.Errorf("problem declared by the override went unreported:\n%s", got)
	}
}

// Line numbers come from last real file that fed config, so a problem point at
// something openable. Builtin preset carry no path and never qualify.
func TestLastFileSourcePrefersTheOverride(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\"]\n")
	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"version\"]\n")

	res := config.Load(home, project)
	path, source := lastFileSource(res)
	if path != config.ProjectPath(project) {
		t.Errorf("path = %q, want the override", path)
	}
	if !strings.Contains(string(source), "version") {
		t.Errorf("source = %q, want the override's bytes", source)
	}

	// Nothing on disk leave only builtin preset, carrying no file to read.
	bare := config.Load(t.TempDir(), "")
	if path, source := lastFileSource(bare); path != "" || source != nil {
		t.Errorf("builtin preset reported as a file: %q", path)
	}
}

// install and uninstall round trip through isolated home, so no test touch
// real Claude Code settings.
func TestInstallUninstallRoundTrip(t *testing.T) {
	home := isolate(t)
	var out bytes.Buffer

	if code := runInstall(nil, &out); code != 0 {
		t.Fatalf("install exit = %d: %s", code, out.String())
	}
	for _, want := range []string{"installed", "configured", "wrote", "Restart Claude Code"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("install output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(install.SettingsPath(home)); err != nil {
		t.Errorf("settings not written: %v", err)
	}

	out.Reset()
	if code := runUninstall(nil, &out); code != 0 {
		t.Fatalf("uninstall exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "removed the status line") {
		t.Errorf("uninstall output = %q", out.String())
	}
}

// Existing statusline.toml is user's own work, so reinstall keep it and say so.
func TestInstallKeepsAnExistingConfig(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\"]\n")

	var out bytes.Buffer
	if code := runInstall(nil, &out); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--force") {
		t.Errorf("install should mention --force:\n%s", out.String())
	}
}

func TestInstallRejectsUnknownPreset(t *testing.T) {
	isolate(t)
	var out bytes.Buffer

	if code := runInstall([]string{"--preset", "nope"}, &out); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "available:") {
		t.Errorf("error should list presets: %q", out.String())
	}
}

// Uninstalling what was never installed is no failure.
func TestUninstallOnACleanHome(t *testing.T) {
	isolate(t)
	var out bytes.Buffer

	if code := runUninstall(nil, &out); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "no status line was configured") {
		t.Errorf("output = %q", out.String())
	}
}
