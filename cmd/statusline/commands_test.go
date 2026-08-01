package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/install"
	"github.com/devemberx/knit-statusline/internal/schema"
)

func parse(t *testing.T, doc string) *schema.Input {
	t.Helper()
	in, err := schema.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return in
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
	if !strings.Contains(sparse, "fresh session") {
		t.Errorf("sparse preview missing its label:\n%s", sparse)
	}
	// limit.5h and limit.7d are Stable: no rate_limits still holds their row
	// with placeholder rather than dropping it, never a fake zero or percentage.
	if !strings.Contains(sparse, "current ○○○○○○○○○○   …%") {
		t.Errorf("sparse preview dropped held rate limit slot:\n%s", sparse)
	}
	if !strings.Contains(sparse, "weekly  ○○○○○○○○○○   …%") {
		t.Errorf("sparse preview dropped held rate limit slot:\n%s", sparse)
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

// Bad flag must say which. Exit 2 with no output leave user at prompt guessing.
func TestPreviewRejectsUnknownFlag(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runPreview([]string{"--nope"}, &out, &errOut); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "nope") {
		t.Errorf("stderr should name the bad flag: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should stay clean: %q", out.String())
	}
}

// -h is no error: flag package hand it back as flag.ErrHelp.
func TestSubcommandHelpFlagExitsZero(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runPreview([]string{"-h"}, &out, &errOut); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("-h printed no usage: %q", out.String())
	}
}

// Errors belong on stderr, so stdout piped to file hold rendered row alone.
// Unknown preset name what exist too, so user need not go hunting.
func TestPreviewErrorsGoToStderr(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runPreview([]string{"--preset", "nope"}, &out, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "available:") {
		t.Errorf("stderr should list presets: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should stay clean: %q", out.String())
	}
}

// preview exist to check config edit, so it must not be quietest of three.
// Mistyped segment name cost its slot; render row carry marker, doctor carry
// prose, preview said nothing at all.
func TestPreviewReportsConfigProblems(t *testing.T) {
	root := isolate(t)
	t.Setenv("NO_COLOR", "1")
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\", \"no-such-segment\"]\n")

	var out, errOut bytes.Buffer
	if code := runPreview(nil, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown segment") {
		t.Errorf("preview stayed silent about the typo: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Opus") {
		t.Errorf("row should still draw:\n%s", out.String())
	}
}

// preview draw what this directory render, project override included. Without
// it, a layout tuned per project is unpreviewable.
func TestPreviewAppliesTheProjectOverride(t *testing.T) {
	root := isolate(t)
	t.Setenv("NO_COLOR", "1")
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\", \"version\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\"]\n")
	t.Chdir(project)

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

func TestDoctorReportsPathsAndSegments(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runDoctor(nil, &out, &errOut); code != 0 {
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
	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	if !strings.Contains(out.String(), "(not present)") {
		t.Errorf("a fresh home should report missing files:\n%s", out.String())
	}
}

// Reporting problems is this command's job, so finding some is no failure.
// Exit stay zero and text carry verdict.
func TestDoctorReportsProblemsAndStillExitsZero(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, `
[[lines]]
segments = ["model", "no-such-segment"]

[segments.model]
template = "{nope}"
`)

	var out, errOut bytes.Buffer
	if code := runDoctor(nil, &out, &errOut); code != 0 {
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
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\", \"no-such-segment\"]\n")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, config.ProjectPath(project)) {
		t.Errorf("project path absent from doctor:\n%s", got)
	}
	if !strings.Contains(got, "unknown segment") {
		t.Errorf("problem declared by the override went unreported:\n%s", got)
	}
}

// Problem must name file that declared it. Blaming last-merged layer instead
// sent user to open project override and read its innocent row 2, because both
// files name segment "model".
func TestDoctorBlamesTheFileHoldingTheMistake(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n\n[segments.model]\ntemplate = \"{bogus}\"\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\"]\n")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, config.UserPath(root)+":4") {
		t.Errorf("unknown field not located in the user config at line 4:\n%s", got)
	}
	if strings.Contains(got, config.ProjectPath(project)+":") {
		t.Errorf("clean project override blamed for the user config's mistake:\n%s", got)
	}
}

// Override own mistake stay its own, line included.
func TestDoctorLocatesTheOverridesOwnMistake(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"model\"]\n\n[segments.model]\ntemplate = \"{bogus}\"\n")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	if got := out.String(); !strings.Contains(got, config.ProjectPath(project)+":4") {
		t.Errorf("override's own unknown field not located in it:\n%s", got)
	}
}

// Builtin preset carry no file, so nothing on disk leave fallback path bare of
// line number rather than pointing at row of file nobody wrote.
func TestDoctorOnBuiltinPresetNamesNoLine(t *testing.T) {
	root := isolate(t)
	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	if got := out.String(); strings.Contains(got, config.UserPath(root)+":") {
		t.Errorf("absent config reported with a line number:\n%s", got)
	}
}

// Project layer may not run shell commands. Strip already reported, so
// "type command but no command" beside it read as second, invented mistake.
func TestDoctorReportsAStrippedCommandOnce(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")

	project := t.TempDir()
	writeProjectConfig(t, project, "[[lines]]\nsegments = [\"clock\"]\n\n[segments.clock]\ntype = \"command\"\ncommand = \"date\"\n")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, "command ignored") {
		t.Errorf("strip went unreported:\n%s", got)
	}
	if strings.Contains(got, "but no command") {
		t.Errorf("strip reported twice, second time as the user's mistake:\n%s", got)
	}
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	root := isolate(t)
	var out, errOut bytes.Buffer

	if code := runInstall(nil, &out, &errOut); code != 0 {
		t.Fatalf("install exit = %d: %s", code, out.String())
	}
	for _, want := range []string{"installed", "configured", "wrote", "Restart Claude Code"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("install output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(install.SettingsPath(root)); err != nil {
		t.Errorf("settings not written: %v", err)
	}

	out.Reset()
	if code := runUninstall(nil, &out, &errOut); code != 0 {
		t.Fatalf("uninstall exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "removed the status line") {
		t.Errorf("uninstall output = %q", out.String())
	}
}

// Existing statusline.toml is user's own work, so reinstall keep it and say so.
func TestInstallKeepsAnExistingConfig(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")

	var out, errOut bytes.Buffer
	if code := runInstall(nil, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--force") {
		t.Errorf("install should mention --force:\n%s", out.String())
	}
}

func TestInstallRejectsUnknownPreset(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runInstall([]string{"--preset", "nope"}, &out, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "available:") {
		t.Errorf("stderr should list presets: %q", errOut.String())
	}
	// Nothing installed, so nothing to report on stdout.
	if out.Len() != 0 {
		t.Errorf("stdout should stay clean: %q", out.String())
	}
}

// Foreign statusLine stay put, so claiming removal is a report user act on and
// find false. Same output cover ownership check going wrong on windows case or
// 8.3 short home.
func TestUninstallReportsAForeignStatusLineLeftAlone(t *testing.T) {
	root := isolate(t)
	writeSettings(t, root, `{"statusLine":{"type":"command","command":"/opt/other-tool"}}`)

	var out, errOut bytes.Buffer
	if code := runUninstall(nil, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	got := out.String()
	if strings.Contains(got, "removed the status line") {
		t.Errorf("claimed removal of key it left in place:\n%s", got)
	}
	if !strings.Contains(got, "/opt/other-tool") {
		t.Errorf("output should name command it left:\n%s", got)
	}

	b, err := os.ReadFile(install.SettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/opt/other-tool") {
		t.Errorf("another tool's statusLine was deleted: %s", b)
	}
}

// Uninstalling what was never installed is no failure.
func TestUninstallOnACleanHome(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer

	if code := runUninstall(nil, &out, &errOut); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "no status line was configured") {
		t.Errorf("output = %q", out.String())
	}
}

func TestDoctorReportsUnknownSetting(t *testing.T) {
	isolate(t)
	var stdout, stderr bytes.Buffer
	if code := runDoctor(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("runDoctor = %d, want 0", code)
	}
	// Resolved value, not label alone: doctor printing "unknown" beside wrong
	// text pass a Contains check on name while reporting setting nobody set.
	want := fmt.Sprintf("unknown    %q", config.DefaultUnknown)
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("doctor did not report %s:\n%s", want, stdout.String())
	}
}

func TestDoctorMarksStableSegments(t *testing.T) {
	isolate(t)
	var stdout, stderr bytes.Buffer
	runDoctor(nil, &stdout, &stderr)
	out := stdout.String()
	// Whole set, not one marker: design name seven, and marker landing on one
	// arbitrary segment satisfy a bare Contains.
	for _, name := range []string{
		"context", "cost", "limit.5h", "limit.7d", "lines", "session", "tokens",
	} {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `\s.*\(holds slot\)$`).MatchString(out) {
			t.Errorf("doctor did not mark %s as holding its slot:\n%s", name, out)
		}
	}
	if got := strings.Count(out, "(holds slot)"); got != 7 {
		t.Errorf("(holds slot) marked %d segments, want 7:\n%s", got, out)
	}
}

// --unknown must reach every stable segment, not context alone. unknown.json
// drop cost and context_window for that: sparse.json carry both populated with
// real zeros, which win on known path and hide session, cost and lines.
func TestPreviewUnknownRendersPlaceholders(t *testing.T) {
	isolate(t)
	t.Setenv("NO_COLOR", "1")
	var stdout, stderr bytes.Buffer
	if code := runPreview([]string{"--preset", "verbose", "--unknown"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runPreview = %d, want 0, stderr = %s", code, stderr.String())
	}
	u := config.DefaultUnknown
	for _, want := range []string{
		"✍️ " + u,          // context
		"⏱ " + u,           // session
		"$" + u,            // cost
		"+" + u + " -" + u, // lines
		"↑" + u + " ↓" + u, // tokens
		"current ○○○○○○○○○○   " + u + "%", // limit.5h
		"weekly ○○○○○○○○○○   " + u + "%",  // limit.7d
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("preview --unknown missing %q:\n%s", want, stdout.String())
		}
	}
	// Fresh-zero shapes belong to --sparse alone. One appearing here mean
	// fixture regained block that win on known path.
	for _, banned := range []string{"✍️ 0%", "⏱ 0s", "$0.00", "+0 -0", "↑0 ↓0"} {
		if strings.Contains(stdout.String(), banned) {
			t.Errorf("preview --unknown printed %q, zero nobody proved:\n%s", banned, stdout.String())
		}
	}
}

// Opposite row from same command: sparse.json carry cost and context_window
// populated, and probe prove session sent nothing, so every zero is fact.
func TestPreviewSparseRendersFreshZeros(t *testing.T) {
	isolate(t)
	t.Setenv("NO_COLOR", "1")
	var stdout, stderr bytes.Buffer
	if code := runPreview([]string{"--preset", "reference", "--sparse"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runPreview = %d, want 0, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0%") {
		t.Fatalf("preview --sparse printed no fresh zero: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "✍️ "+config.DefaultUnknown) {
		t.Fatalf("preview --sparse placeholdered context instead of zeroing it: %s", stdout.String())
	}
}
