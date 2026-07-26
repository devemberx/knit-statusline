package config

import (
	"errors"
	"os"
	"path/filepath"
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

func errorText(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "\n")
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
	}
}

// Lines replace, never merge. No principled answer to whether a two-row
// override extend or replace a four-row base, so any project declaring rows own
// whole layout.
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

// Merge must not write through to base, else a second merge see changes from
// first one.
func TestMergeDoesNotMutateBase(t *testing.T) {
	base := mustParse(t, "[segments.context]\nwarn = 60\n")
	override := mustParse(t, "[segments.context]\nwarn = 90\n")

	Merge(base, override)
	if base.Segments["context"].Warn == nil || *base.Segments["context"].Warn != 60 {
		t.Error("Merge mutated the base config")
	}
}

func TestResolveAppliesPrecedence(t *testing.T) {
	c := mustParse(t, `
[defaults]
bar_width = 15
warn = 40

[segments.context]
warn = 70
`)

	r := c.Resolve("context", "default {pct}")
	if r.BarWidth != 15 {
		t.Errorf("bar_width = %d, want the defaults value 15", r.BarWidth)
	}
	if r.Warn != 70 {
		t.Errorf("warn = %d, want the segment value 70", r.Warn)
	}
	if r.Crit != DefaultCrit {
		t.Errorf("crit = %d, want the built-in %d", r.Crit, DefaultCrit)
	}
	if r.Template != "default {pct}" {
		t.Errorf("template = %q, want the implementation default", r.Template)
	}
}

func TestResolveUsesSegmentNameAsKind(t *testing.T) {
	c := mustParse(t, `
[segments.k8s]
type = "command"
command = "kubectl config current-context"
`)
	if got := c.Resolve("k8s", "").Kind; got != "command" {
		t.Errorf("kind = %q, want command", got)
	}
	if got := c.Resolve("model", "").Kind; got != "model" {
		t.Errorf("kind = %q, want the segment name", got)
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

func fakeKnown(kind string) ([]string, bool) {
	switch kind {
	case "model":
		return []string{"name", "id"}, true
	case "command":
		return []string{"out"}, true
	}
	return nil, false
}

func TestValidateReportsEveryProblem(t *testing.T) {
	src := `
[[lines]]
segments = ["model", "nope"]

[segments.model]
template = "{name} {not_a_field}"

[segments.broken]
type = "command"

[segments.ranged]
type = "model"
warn = 150
`
	c := mustParse(t, src)
	all := errorText(Validate(c, []byte(src), "test.toml", fakeKnown))

	for _, want := range []string{
		`unknown segment "nope"`,
		`unknown field {not_a_field}`,
		`no command`,
		`outside 0-100`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

// Message name fields that do exist. A mistyped field must not send its user
// hunting for a list.
func TestValidateListsAvailableFields(t *testing.T) {
	src := "[segments.model]\ntemplate = \"{bogus}\"\n"
	c := mustParse(t, src)
	errs := Validate(c, []byte(src), "test.toml", fakeKnown)

	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errs[0].Error(), "available: name, id") {
		t.Errorf("error should list the available fields: %v", errs[0])
	}
}

func TestValidateAcceptsAGoodConfig(t *testing.T) {
	src := "[[lines]]\nsegments = [\"model\"]\n\n[segments.model]\ntemplate = \"{name}\"\n"
	c := mustParse(t, src)
	if errs := Validate(c, []byte(src), "test.toml", fakeKnown); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

// Range alone accept a fully reversed cascade -- warn 90, high 80, crit 70 --
// and grading then run backwards in silence.
func TestValidateRejectsInvertedThresholds(t *testing.T) {
	src := `
[segments.model]
warn = 90
high = 80
crit = 70
`
	c := mustParse(t, src)
	all := errorText(Validate(c, []byte(src), "test.toml", fakeKnown))

	for _, want := range []string{
		"warn = 90 is above high = 80",
		"high = 80 is above crit = 70",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

// Inversion span layers: segment name crit alone, warn arrive from defaults,
// and only their pairing is wrong. Checking declared values alone miss it.
func TestValidateCatchesInversionAcrossDefaults(t *testing.T) {
	src := `
[defaults]
warn = 80

[segments.model]
crit = 40
`
	c := mustParse(t, src)
	all := errorText(Validate(c, []byte(src), "test.toml", fakeKnown))

	if !strings.Contains(all, "model high = 70 is above crit = 40") {
		t.Errorf("cross-layer inversion missed:\n%s", all)
	}
}

// Out-of-range value suppress its own order check. Reporting both for one typo
// bury which number needs fixing.
func TestValidateSkipsOrderWhenValueIsOutOfRange(t *testing.T) {
	src := "[segments.model]\nwarn = 150\n"
	c := mustParse(t, src)
	all := errorText(Validate(c, []byte(src), "test.toml", fakeKnown))

	if !strings.Contains(all, "outside 0-100") {
		t.Errorf("range problem not reported:\n%s", all)
	}
	if strings.Contains(all, "is above") {
		t.Errorf("order check should stay quiet for an out-of-range value:\n%s", all)
	}
}

// doctor print findings in one pass. Map iteration reorder them per run, so two
// invocations read as disagreeing about a config that never changed.
func TestValidateOrderIsStable(t *testing.T) {
	src := `
[segments.aaa]
type = "nope"

[segments.mmm]
type = "nope"

[segments.zzz]
type = "nope"
`
	c := mustParse(t, src)
	first := errorText(Validate(c, []byte(src), "test.toml", fakeKnown))
	for i := 0; i < 20; i++ {
		if got := errorText(Validate(c, []byte(src), "test.toml", fakeKnown)); got != first {
			t.Fatalf("run %d reordered:\n%s\n---\n%s", i, first, got)
		}
	}
}

// Semantic error point at a declaration, never at prose naming one. Loose
// substring search report line 1 for a fault seven lines down, and a confidently
// wrong line number cost more than none at all.
func TestLineOfMatchesDeclarationsOnly(t *testing.T) {
	src := []byte(`# comment naming dir and model
[[lines]]
segments = ["dir"]

[segments."limit.5h"]
template = "{pct}"

[defaults]
warn = 40
`)
	for _, tc := range []struct {
		tok  string
		want int
	}{
		{"dir", 3},      // quoted inside a segments list
		{"limit.5h", 5}, // quoted table key holding a dot of its own
		{"defaults", 8}, // bare table header
		{"model", 0},    // named in a comment only, so location unknown
		{"lines", 0},    // [[lines]] declare rows, never a segment
		{"nowhere", 0},
	} {
		if got := lineOf(src, tc.tok); got != tc.want {
			t.Errorf("lineOf(%q) = %d, want %d", tc.tok, got, tc.want)
		}
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

// Broken file drop its own layer and get reported, never blank row: a user with
// a typo still need to see their session.
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

func TestBlankLineDetection(t *testing.T) {
	c := mustParse(t, "[[lines]]\n[[lines]]\nsegments = [\"model\"]\n")
	if !c.Lines[0].Blank() {
		t.Error("a row with no segments is a deliberate blank")
	}
	if c.Lines[1].Blank() {
		t.Error("a row with segments is not blank")
	}
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
