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
		"knit-statusline", "Paths", "  root       ", "settings", "config", "cache",
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

// Relative CLAUDE_CONFIG_DIR name no fixed directory: it resolve against cwd
// of whichever process read it, and Claude Code's differ from shell's. doctor
// must say so and count it, else "status ok" sit beside root install refuse.
func TestDoctorFlagsARelativeConfigRoot(t *testing.T) {
	isolate(t)
	t.Chdir(t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "myconf")

	var out, errOut bytes.Buffer
	if code := runDoctor(nil, &out, &errOut); code != 0 {
		t.Fatalf("doctor exited %d: %s", code, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, "(CLAUDE_CONFIG_DIR, relative)") {
		t.Errorf("root line does not mark the value relative:\n%s", got)
	}
	if !strings.Contains(got, `config root "myconf" is relative`) {
		t.Errorf("doctor does not report the relative root:\n%s", got)
	}
	if strings.Contains(got, "status     ok") {
		t.Errorf("doctor called a root install refuses ok:\n%s", got)
	}
}

// Stray block tell user run install and copy files into new root. Relative
// root make both impossible: install refuse that value, and destination
// resolve against cwd. Two instructions contradicting each other read as
// doctor confused rather than as one thing to fix.
func TestDoctorSkipsTheStrayBlockOnARelativeRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := filepath.Join(home, ".claude")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config.UserPath(legacy), install.SettingsPath(legacy), install.BinaryPath(legacy)} {
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "myconf")

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if strings.Contains(got, "Stray files") {
		t.Errorf("stray block fired on a relative root:\n%s", got)
	}
	// Silence hold only while ERROR still name what to fix.
	if !strings.Contains(got, `config root "myconf" is relative`) {
		t.Errorf("doctor went quiet without reporting the relative root:\n%s", got)
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

// Refusal must reach command, not package alone, and land before anything
// written.
func TestInstallRefusesARelativeConfigRoot(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "myconf")

	var out, errOut bytes.Buffer
	if code := runInstall(nil, &out, &errOut); code == 0 {
		t.Fatalf("install exited 0 with a relative root:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "relative") {
		t.Errorf("stderr does not say the root is relative:\n%s", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "myconf")); !os.IsNotExist(err) {
		t.Errorf("install created a directory under cwd, stat error = %v", err)
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

// Moved root and default root print identical Paths block without this line, so
// user cannot tell which directory doctor read.
func TestDoctorNamesTheConfigRootVariable(t *testing.T) {
	isolate(t)
	moved := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", moved)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, moved) {
		t.Errorf("doctor omits the moved root %q:\n%s", moved, got)
	}
	if !strings.Contains(got, "(CLAUDE_CONFIG_DIR)") {
		t.Errorf("doctor does not say where the root came from:\n%s", got)
	}
	// isolate never create ~/.claude, so old root hold nothing of ours. Block
	// firing here send user to run uninstall against directory with no install
	// in it. Pin both guards: Stat filter in strays, len(found) in runDoctor.
	if strings.Contains(got, "Stray files") {
		t.Errorf("stray block fired with nothing left in the old root:\n%s", got)
	}
}

// File left in old root is read by nobody. Five causes make a segment vanish and
// all look alike, so doctor name what it found rather than stay silent.
func TestDoctorListsStrayFilesInTheOldRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := filepath.Join(home, ".claude")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := config.UserPath(legacy)
	if err := os.WriteFile(orphan, []byte("[[lines]]\nsegments = [\"model\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", moved)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, "Stray files in "+legacy) {
		t.Errorf("doctor omits the stray block:\n%s", got)
	}
	if !strings.Contains(got, orphan) {
		t.Errorf("doctor omits the stray config %q:\n%s", orphan, got)
	}
	// statusline.toml is user's layout. Naming destination turn report into
	// migration step; "it is over there" alone leave them to guess.
	if !strings.Contains(got, "Copy it to "+config.UserPath(moved)) {
		t.Errorf("doctor does not name where to copy the stray config:\n%s", got)
	}
}

// Old settings.json hold user's hooks, permissions and enabled plugins --
// config this program never owned, and install itself only ever merge into.
// Blanket "delete these" cost them all of that, and no reinstall bring it back.
func TestDoctorNeverTellsUserToDeleteStraySettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := filepath.Join(home, ".claude")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := install.SettingsPath(legacy)
	if err := os.WriteFile(settings, []byte(`{"hooks":{},"permissions":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, settings) {
		t.Fatalf("doctor omits the stray settings.json:\n%s", got)
	}
	if !strings.Contains(got, "Deleting it loses them") {
		t.Errorf("doctor does not say what deleting settings.json costs:\n%s", got)
	}
	if strings.Contains(got, "delete these") {
		t.Errorf("doctor still tells the user to delete every stray file:\n%s", got)
	}
	// uninstall drop our binary and our statusLine key, nothing else -- only
	// safe way to clear old root wholesale.
	if !strings.Contains(got, "knit-statusline uninstall") {
		t.Errorf("doctor does not point at uninstall to clear the old root:\n%s", got)
	}
}

// New root already carrying statusline.toml is user who moved root long ago and
// tuned layout there. Stray one is abandoned copy, so "copy it over" revert live
// layout to stale -- and install refuse that same overwrite without --force.
func TestDoctorSaysMergeWhenNewRootAlreadyHasAConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := filepath.Join(home, ".claude")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserPath(legacy), []byte("[[lines]]\nsegments = [\"model\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := t.TempDir()
	if err := os.WriteFile(config.UserPath(moved), []byte("[[lines]]\nsegments = [\"version\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", moved)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	got := out.String()
	if strings.Contains(got, "Copy it to "+config.UserPath(moved)) {
		t.Errorf("doctor told the user to copy over a live config:\n%s", got)
	}
	if !strings.Contains(got, "merge, do not copy over it") {
		t.Errorf("doctor does not say to merge into the existing config:\n%s", got)
	}
}

// isolate pins CLAUDE_CONFIG_DIR to default ~/.claude itself -- same
// directory named two ways, textually equal this time. Exercises strayRoot's
// identity check, not early return covering majority who never set variable
// at all.
func TestDoctorSkipsStrayBlockOnTheDefaultRoot(t *testing.T) {
	root := isolate(t)
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	if got := out.String(); strings.Contains(got, "Stray files") {
		t.Errorf("stray block fired on the default root:\n%s", got)
	}
}

// Symlinked home name legacy root and CLAUDE_CONFIG_DIR by two different
// strings for one directory. Text compare alone misreads live config as stray
// and tells user delete it -- reproduces reviewer's report exactly.
func TestDoctorSkipsStrayBlockAcrossASymlinkedHome(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("HOME", link)
	t.Setenv("USERPROFILE", link)

	root := filepath.Join(target, ".claude")
	writeUserConfig(t, root, "[[lines]]\nsegments = [\"model\"]\n")
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	var out, errOut bytes.Buffer
	runDoctor(nil, &out, &errOut)

	if got := out.String(); strings.Contains(got, "Stray files") {
		t.Errorf("stray block fired across a symlinked home:\n%s", got)
	}
}
