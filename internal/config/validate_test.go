package config

import (
	"strings"
	"testing"
)

func errorText(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "\n")
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

func validateSrc(t *testing.T, src string) string {
	t.Helper()
	return errorText(Validate(mustParse(t, src), []byte(src), "test.toml", fakeKnown))
}

func TestValidateReportsEveryProblem(t *testing.T) {
	all := validateSrc(t, `
[[lines]]
segments = ["model", "nope"]

[segments.model]
template = "{name} {not_a_field}"

[segments.broken]
type = "command"

[segments.ranged]
type = "model"
warn = 150

[segments.scoped]
type = "model"
scope = "galaxy"
`)

	for _, want := range []string{
		`unknown segment "nope"`,
		`unknown field {not_a_field}`,
		`no command`,
		`outside 0-100`,
		`scope "galaxy"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

// Message name fields that do exist. Mistyped field must not send its user
// hunting for a list.
func TestValidateListsAvailableFields(t *testing.T) {
	all := validateSrc(t, "[segments.model]\ntemplate = \"{bogus}\"\n")
	if !strings.Contains(all, "available: name, id") {
		t.Errorf("error should list the available fields:\n%s", all)
	}
}

func TestValidateAcceptsAGoodConfig(t *testing.T) {
	src := "[[lines]]\nsegments = [\"model\"]\n\n[segments.model]\ntemplate = \"{name}\"\nscope = \"project\"\n"
	if all := validateSrc(t, src); all != "" {
		t.Errorf("unexpected errors:\n%s", all)
	}
}

// Alignment spec is not part of field name, so {pct:>3} must resolve as pct.
func TestValidateStripsAlignmentSpec(t *testing.T) {
	if all := validateSrc(t, "[segments.model]\ntemplate = \"{name:>10}\"\n"); all != "" {
		t.Errorf("alignment spec treated as part of the field name:\n%s", all)
	}
}

// render.Expand read "{{name}}" as field "{name" and drop it, so no setting is
// lost. Naming a field nobody wrote would only confuse.
func TestValidateIgnoresUnclosedBrace(t *testing.T) {
	if all := validateSrc(t, "[segments.model]\ntemplate = \"{{name}}\"\n"); all != "" {
		t.Errorf("malformed placeholder reported as an unknown field:\n%s", all)
	}
}

// Key nothing decodes reach doctor through Validate, still naming its own file.
func TestValidateReportsKeysNothingDecodes(t *testing.T) {
	all := validateSrc(t, "[segments.model]\nbar_witdh = 20\n")
	if !strings.Contains(all, `unknown key "segments.model.bar_witdh"`) {
		t.Errorf("dropped key not reported:\n%s", all)
	}
	if !strings.Contains(all, "test.toml:2") {
		t.Errorf("dropped key not located:\n%s", all)
	}
}

// Range alone accept fully reversed cascade -- warn 90, high 80, crit 70 -- and
// grading then run backwards in silence.
func TestValidateRejectsInvertedThresholds(t *testing.T) {
	all := validateSrc(t, `
[segments.model]
warn = 90
high = 80
crit = 70
`)

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
	all := validateSrc(t, `
[defaults]
warn = 80

[segments.model]
crit = 40
`)

	if !strings.Contains(all, "model high = 70 is above crit = 40") {
		t.Errorf("cross-layer inversion missed:\n%s", all)
	}
}

// Pair with both sides inherited is [defaults]' own fault. Repeating it per
// segment point every finding at block holding no threshold, and buries which
// number to fix under however many segments happen to exist.
func TestValidateReportsInheritedInversionOnce(t *testing.T) {
	all := validateSrc(t, `
[defaults]
warn = 90
high = 80
crit = 70

[segments.model]
template = "{name}"

[segments.other]
type = "model"
template = "{id}"
`)

	if strings.Contains(all, "model warn") || strings.Contains(all, "other warn") {
		t.Errorf("inherited inversion blamed on a segment:\n%s", all)
	}
	if n := strings.Count(all, "is above"); n != 2 {
		t.Errorf("got %d order findings, want 2 against defaults:\n%s", n, all)
	}
}

// Out-of-range value suppress its own order check. Reporting both for one typo
// bury which number needs fixing.
func TestValidateSkipsOrderWhenValueIsOutOfRange(t *testing.T) {
	all := validateSrc(t, "[segments.model]\nwarn = 150\n")

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

// Semantic error point at declaration, never at prose naming one. Loose
// substring search report line 1 for fault seven lines down, and confidently
// wrong line number cost more than none at all.
func TestLineOfPrefersDeclarations(t *testing.T) {
	src := []byte(`# comment naming dir and model
[[lines]]
segments = ["dir", "limit.5h"]

[segments."limit.5h"]
template = "{pct}"

[defaults]
warn = 40

[segments]
inline = { warn = 10 }
`)
	for _, tc := range []struct {
		tok  string
		want int
	}{
		{"limit.5h", 5}, // quoted table key wins over its use on line 3
		{"defaults", 8}, // bare table header
		{"inline", 12},  // inline-table declaration reads as assignment
		{"dir", 3},      // no block of its own, so use is all that exist
		{"model", 0},    // named in a comment only, so location unknown
		{"lines", 0},    // [[lines]] declare rows, never a segment
		{"nowhere", 0},
	} {
		if got := lineOf(src, tc.tok); got != tc.want {
			t.Errorf("lineOf(%q) = %d, want %d", tc.tok, got, tc.want)
		}
	}
}

func TestLineOfHandlesEmptyInput(t *testing.T) {
	if got := lineOf(nil, "dir"); got != 0 {
		t.Errorf("lineOf(nil) = %d", got)
	}
	if got := lineOf([]byte("[segments.dir]\n"), ""); got != 0 {
		t.Errorf(`lineOf(src, "") = %d`, got)
	}
}

func TestTableKey(t *testing.T) {
	for _, tc := range []struct {
		line, want string
	}{
		{"[defaults]", "defaults"},
		{"[segments.dir]", "dir"},
		{`[segments."limit.5h"]`, "limit.5h"},
		{"[[lines]]", ""},
		{"segments = [\"model\"]", ""},
		{"[unterminated", ""},
		{"", ""},
	} {
		if got := tableKey([]byte(tc.line)); got != tc.want {
			t.Errorf("tableKey(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}
