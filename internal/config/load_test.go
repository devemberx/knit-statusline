package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Config {
	t.Helper()
	c, err := ParseBytes([]byte(src), "test.toml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

func writeConfig(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEveryPresetParses(t *testing.T) {
	names := PresetNames()
	if len(names) == 0 {
		t.Fatal("no presets are embedded")
	}
	for _, name := range names {
		c, err := Preset(name)
		if err != nil {
			t.Errorf("preset %s: %v", name, err)
			continue
		}
		if len(c.Lines) == 0 {
			t.Errorf("preset %s declares no rows", name)
		}
		if len(c.unknown) != 0 {
			t.Errorf("preset %s carries keys nothing decodes: %v", name, c.unknown)
		}
	}
	if !slices.Contains(names, DefaultPreset) {
		t.Errorf("presets %v omit the fallback %q", names, DefaultPreset)
	}
}

func TestUnknownPresetIsNamedNotPanicked(t *testing.T) {
	if _, err := Preset("nope"); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("err = %v, want the rejected name", err)
	}
}

// Installer write preset out verbatim, so comments must survive round trip.
func TestPresetSourceKeepsComments(t *testing.T) {
	b, err := PresetSource(DefaultPreset)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "#") {
		t.Error("preset source lost its comments")
	}
}

// Syntax error name its line. "Your config is broken" is not actionable;
// "line 4" is.
func TestSyntaxErrorReportsLine(t *testing.T) {
	_, err := ParseBytes([]byte("[[lines]]\nsegments = [\"model\"]\n\nthis is not toml\n"), "bad.toml")
	if err == nil {
		t.Fatal("expected an error")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("got %T, want *config.Error", err)
	}
	if ce.Line != 4 {
		t.Errorf("line = %d, want 4", ce.Line)
	}
	if !strings.Contains(ce.Short(), "bad.toml:4") {
		t.Errorf("Short() = %q", ce.Short())
	}
}

// Row is narrow, so Short drop directory and keep line.
func TestErrorRendering(t *testing.T) {
	located := &Error{File: "/home/u/.claude/statusline.toml", Line: 7, Msg: "boom"}
	if got := located.Error(); got != "/home/u/.claude/statusline.toml:7: boom" {
		t.Errorf("Error() = %q", got)
	}
	if got := located.Short(); got != "statusline.toml:7" {
		t.Errorf("Short() = %q", got)
	}

	whole := &Error{File: "/home/u/.claude/statusline.toml", Msg: "boom"}
	if got := whole.Error(); got != "/home/u/.claude/statusline.toml: boom" {
		t.Errorf("Error() = %q", got)
	}
	if got := whole.Short(); got != "statusline.toml" {
		t.Errorf("Short() = %q", got)
	}
}

// Misspelled key decode into nothing and parse clean, so its setting vanish in
// silence. Captured at parse, where file name and line still known.
func TestParseRecordsKeysNothingDecodes(t *testing.T) {
	c := mustParse(t, "[segments.model]\nbar_witdh = 20\n")

	if len(c.unknown) != 1 {
		t.Fatalf("unknown = %v, want one entry", c.unknown)
	}
	got := c.unknown[0]
	if !strings.Contains(got.Msg, "segments.model.bar_witdh") {
		t.Errorf("msg = %q, want the full key path", got.Msg)
	}
	if got.Line != 2 {
		t.Errorf("line = %d, want 2", got.Line)
	}
	if got.File != "test.toml" {
		t.Errorf("file = %q", got.File)
	}
}

func TestParseAcceptsEveryDeclaredKey(t *testing.T) {
	c := mustParse(t, `
[defaults]
separator = " | "
bar_width = 10
warn = 50
high = 70
crit = 90

[[lines]]
segments = ["model"]
separator = "  "

[segments.model]
type = "command"
template = "{out}"
warn = 10
high = 20
crit = 30
bar_width = 5
scope = "project"
include_sidechain = true
command = "echo hi"
timeout_ms = 250
cache_ms = 5000
`)
	if len(c.unknown) != 0 {
		t.Errorf("declared keys reported as unknown: %v", c.unknown)
	}
}

// ToSlash so one expectation read on every runner: Windows join with "\".
func TestPathsSitUnderDotClaude(t *testing.T) {
	if got := filepath.ToSlash(UserPath("/home/u")); got != "/home/u/.claude/statusline.toml" {
		t.Errorf("UserPath = %q", got)
	}
	if got := filepath.ToSlash(ProjectPath("/w/acme")); got != "/w/acme/.claude/statusline.toml" {
		t.Errorf("ProjectPath = %q", got)
	}
}

// Lines replace, never merge. No principled answer to whether two-row override
// extend or replace four-row base, so any project declaring rows own whole
// layout.
func TestMergeReplacesLinesWholesale(t *testing.T) {
	base := mustParse(t, `
[[lines]]
segments = ["model", "context"]
[[lines]]
segments = ["limit.5h"]
[[lines]]
segments = ["limit.7d"]
`)
	override := mustParse(t, `
[[lines]]
segments = ["limit.5h", "limit.7d"]
`)

	got := Merge(base, override)
	if len(got.Lines) != 1 {
		t.Fatalf("got %d rows, want 1", len(got.Lines))
	}
	if len(got.Lines[0].Segments) != 2 {
		t.Errorf("got segments %v", got.Lines[0].Segments)
	}
}

func TestMergeKeepsBaseLinesWhenOverrideDeclaresNone(t *testing.T) {
	base := mustParse(t, "[[lines]]\nsegments = [\"model\"]\n")
	override := mustParse(t, "[segments.model]\ntemplate = \"{id}\"\n")

	got := Merge(base, override)
	if len(got.Lines) != 1 || got.Lines[0].Segments[0] != "model" {
		t.Errorf("base layout lost: %+v", got.Lines)
	}
	if got.Segments["model"].Template == nil || *got.Segments["model"].Template != "{id}" {
		t.Error("override template not applied")
	}
}

// Segment settings merge per key, so one field of one segment get adjusted
// without restating a layout.
func TestMergeSegmentsPerKey(t *testing.T) {
	base := mustParse(t, `
[segments.context]
template = "ctx {pct}%"
bar_width = 20
warn = 60
`)
	override := mustParse(t, `
[segments.context]
warn = 80
`)

	got := Merge(base, override)
	seg := got.Segments["context"]

	if seg.Template == nil || *seg.Template != "ctx {pct}%" {
		t.Error("template should survive an override that does not mention it")
	}
	if seg.BarWidth == nil || *seg.BarWidth != 20 {
		t.Error("bar_width should survive")
	}
	if seg.Warn == nil || *seg.Warn != 80 {
		t.Error("warn should be overridden")
	}
}

func TestMergeDefaultsPerKey(t *testing.T) {
	base := mustParse(t, "[defaults]\nseparator = \" | \"\nwarn = 40\n")
	override := mustParse(t, "[defaults]\nwarn = 55\n")

	got := Merge(base, override)
	if got.Defaults.Separator == nil || *got.Defaults.Separator != " | " {
		t.Error("separator should survive an override that does not mention it")
	}
	if got.Defaults.Warn == nil || *got.Defaults.Warn != 55 {
		t.Error("warn should be overridden")
	}
}

func TestMergeAddsSegmentsAbsentFromBase(t *testing.T) {
	base := mustParse(t, "[segments.model]\ntemplate = \"{name}\"\n")
	override := mustParse(t, "[segments.k8s]\ntype = \"command\"\ncommand = \"kubectl\"\n")

	got := Merge(base, override)
	if got.Segments["model"] == nil || got.Segments["k8s"] == nil {
		t.Fatalf("merged segments = %v", got.Segments)
	}
	if base.Segments["k8s"] != nil {
		t.Error("Merge wrote a new segment back into the base")
	}
}

// Merge must not write through to base, else second merge see changes from
// first one.
func TestMergeDoesNotMutateBase(t *testing.T) {
	base := mustParse(t, "[segments.context]\nwarn = 60\n[[lines]]\nsegments = [\"model\"]\n")
	override := mustParse(t, "[segments.context]\nwarn = 90\n")

	got := Merge(base, override)
	if base.Segments["context"].Warn == nil || *base.Segments["context"].Warn != 60 {
		t.Error("Merge mutated the base config")
	}

	got.Lines = append(got.Lines, Line{Segments: []string{"dir"}})
	if len(base.Lines) != 1 {
		t.Error("appending to the merged layout reached back into the base")
	}
}

// Both layers report their own dropped keys, and merged config still name file
// each one came from.
func TestMergeCarriesUnknownKeysFromBothLayers(t *testing.T) {
	base, err := ParseBytes([]byte("[segments.model]\nbar_witdh = 20\n"), "user.toml")
	if err != nil {
		t.Fatal(err)
	}
	override, err := ParseBytes([]byte("[segments.model]\ncache_mss = 5\n"), "project.toml")
	if err != nil {
		t.Fatal(err)
	}

	got := Merge(base, override)
	if len(got.unknown) != 2 {
		t.Fatalf("unknown = %v, want both layers", got.unknown)
	}
	if got.unknown[0].File != "user.toml" || got.unknown[1].File != "project.toml" {
		t.Errorf("files = %q, %q", got.unknown[0].File, got.unknown[1].File)
	}
}

func TestLoadFallsBackToPresetWhenNothingExists(t *testing.T) {
	res := Load(t.TempDir(), "")
	if res.Config == nil || len(res.Config.Lines) == 0 {
		t.Fatal("Load must always yield a usable config")
	}
	if len(res.Sources) != 1 || !strings.HasPrefix(res.Sources[0], "builtin:") {
		t.Errorf("sources = %v, want the built-in preset", res.Sources)
	}
	if len(res.Errors) != 0 {
		t.Errorf("a missing config is not an error: %v", res.Errors)
	}
}

// Empty home mean no home directory to read. UserPath would yield relative
// ".claude/statusline.toml" and pick up whatever directory process sit in.
func TestLoadSkipsUserLayerWithoutHome(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, filepath.Join(dir, ".claude", "statusline.toml"),
		"[[lines]]\nsegments = [\"nothing_from_cwd\"]\n")
	t.Chdir(dir)

	res := Load("", "")
	if len(res.Sources) != 1 || !strings.HasPrefix(res.Sources[0], "builtin:") {
		t.Errorf("sources = %v, want the built-in preset only", res.Sources)
	}
	if res.Config.Lines[0].Segments[0] == "nothing_from_cwd" {
		t.Error("Load read a config relative to the working directory")
	}
}

func TestLoadAppliesProjectOverride(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	writeConfig(t, UserPath(home), "[[lines]]\nsegments = [\"model\", \"context\"]\n")
	writeConfig(t, ProjectPath(project), "[[lines]]\nsegments = [\"model\"]\n")

	res := Load(home, project)
	if len(res.Config.Lines) != 1 || len(res.Config.Lines[0].Segments) != 1 {
		t.Errorf("project override not applied: %+v", res.Config.Lines)
	}
	if len(res.Sources) != 2 {
		t.Errorf("sources = %v, want both files", res.Sources)
	}
}

// Broken file drop its own layer and get reported, never blank row: user with
// typo still need to see their session.
func TestLoadSurvivesABrokenUserConfig(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, UserPath(home), "this is not toml\n")

	res := Load(home, "")
	if len(res.Errors) == 0 {
		t.Error("the parse failure should be reported")
	}
	if res.Config == nil || len(res.Config.Lines) == 0 {
		t.Fatal("rendering must continue from the built-in preset")
	}
}

func TestLoadSurvivesABrokenProjectOverride(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	writeConfig(t, UserPath(home), "[[lines]]\nsegments = [\"model\"]\n")
	writeConfig(t, ProjectPath(project), "{{{\n")

	res := Load(home, project)
	if len(res.Errors) == 0 {
		t.Error("the parse failure should be reported")
	}
	if len(res.Config.Lines) != 1 || res.Config.Lines[0].Segments[0] != "model" {
		t.Error("the valid user config should still apply")
	}
}
