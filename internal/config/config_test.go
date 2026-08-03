package config

import "testing"

func ptr[T any](v T) *T { return &v }

func TestKindFallsBackToDeclaringName(t *testing.T) {
	var absent *Segment
	if got := absent.Kind("model"); got != "model" {
		t.Errorf("nil segment kind = %q, want the declaring name", got)
	}
	if got := (&Segment{}).Kind("model"); got != "model" {
		t.Errorf("empty type kind = %q, want the declaring name", got)
	}
	if got := (&Segment{Type: ptr("")}).Kind("k8s"); got != "k8s" {
		t.Errorf("blank type kind = %q, want the declaring name", got)
	}
	if got := (&Segment{Type: ptr("command")}).Kind("k8s"); got != "command" {
		t.Errorf("kind = %q, want command", got)
	}
}

// Row with no segments is deliberate blank. Row whose segments all render empty
// is dropped elsewhere, so two cases stay apart.
func TestBlankIsAboutDeclaredSegments(t *testing.T) {
	if !(Line{}).Blank() {
		t.Error("a row with no segments is a deliberate blank")
	}
	if (Line{Segments: []string{"model"}}).Blank() {
		t.Error("a row with segments is not blank")
	}
}

func TestSeparatorPrecedence(t *testing.T) {
	c := &Config{Defaults: Defaults{Separator: ptr(" - ")}}

	if got := c.Separator(Line{Separator: ptr(" | ")}); got != " | " {
		t.Errorf("separator = %q, want the row override", got)
	}
	if got := c.Separator(Line{}); got != " - " {
		t.Errorf("separator = %q, want the defaults value", got)
	}
	if got := (&Config{}).Separator(Line{}); got != DefaultSeparator {
		t.Errorf("separator = %q, want the built-in %q", got, DefaultSeparator)
	}
}

// Segment beat defaults beat builtin, per key. Threshold left unset at both
// upper layers must still land on its builtin rather than zero.
func TestResolveThresholdPrecedence(t *testing.T) {
	c := &Config{
		Defaults: Defaults{BarWidth: ptr(15), Warn: ptr(40), High: ptr(60)},
		Segments: map[string]*Segment{
			"context": {Warn: ptr(70)},
		},
	}

	r := c.Resolve("context", "default {pct}")
	for _, tc := range []struct {
		label     string
		got, want int
	}{
		{"warn", r.Warn, 70},
		{"high", r.High, 60},
		{"crit", r.Crit, DefaultCrit},
		{"bar_width", r.BarWidth, 15},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.label, tc.got, tc.want)
		}
	}
	if r.Template != "default {pct}" {
		t.Errorf("template = %q, want the implementation default", r.Template)
	}
	if r.Name != "context" || r.Kind != "context" {
		t.Errorf("name/kind = %q/%q, want context/context", r.Name, r.Kind)
	}
}

// Most segments carry no [segments.NAME] block at all, so resolving one must
// not dereference nil.
func TestResolveUndeclaredSegment(t *testing.T) {
	r := (&Config{}).Resolve("model", "{name}")

	if r.Kind != "model" || r.Template != "{name}" {
		t.Errorf("kind/template = %q/%q", r.Kind, r.Template)
	}
	if r.Warn != DefaultWarn || r.High != DefaultHigh || r.Crit != DefaultCrit {
		t.Errorf("thresholds = %d/%d/%d, want the built-ins", r.Warn, r.High, r.Crit)
	}
	if r.BarWidth != DefaultBarWidth {
		t.Errorf("bar_width = %d, want %d", r.BarWidth, DefaultBarWidth)
	}
	if r.Scope != DefaultScope || r.TimeoutMS != DefaultTimeoutMS {
		t.Errorf("scope/timeout = %q/%d", r.Scope, r.TimeoutMS)
	}
	if r.IncludeSidechain || r.Model != "" || r.Command != "" || r.CacheMS != 0 {
		t.Errorf("optional fields should stay zero: %+v", r)
	}
}

// Non-threshold overrides ride same precedence, 0 included: nil pointer is what
// separate unset from deliberate zero.
func TestResolveCarriesEveryOverride(t *testing.T) {
	c := &Config{Segments: map[string]*Segment{
		"k8s": {
			Type:             ptr("command"),
			Template:         ptr("{out}"),
			Scope:            ptr(ScopeProject),
			IncludeSidechain: ptr(true),
			Model:            ptr("Fable"),
			Command:          ptr("kubectl config current-context"),
			TimeoutMS:        ptr(0),
			CacheMS:          ptr(5000),
		},
	}}

	r := c.Resolve("k8s", "{ignored}")
	if r.Kind != "command" || r.Name != "k8s" {
		t.Errorf("kind/name = %q/%q, want command/k8s", r.Kind, r.Name)
	}
	if r.Template != "{out}" {
		t.Errorf("template = %q", r.Template)
	}
	if r.Scope != ScopeProject {
		t.Errorf("scope = %q, want %q", r.Scope, ScopeProject)
	}
	if !r.IncludeSidechain {
		t.Error("include_sidechain override lost")
	}
	if r.Model != "Fable" {
		t.Errorf("model = %q, want %q", r.Model, "Fable")
	}
	if r.Command != "kubectl config current-context" {
		t.Errorf("command = %q", r.Command)
	}
	if r.TimeoutMS != 0 {
		t.Errorf("timeout_ms = %d, want an explicit 0 to survive", r.TimeoutMS)
	}
	if r.CacheMS != 5000 {
		t.Errorf("cache_ms = %d", r.CacheMS)
	}
}

func TestResolveUnknownDefault(t *testing.T) {
	c := &Config{}
	if got := c.Resolve("context", "{pct}").Unknown; got != DefaultUnknown {
		t.Fatalf("Unknown = %q, want %q", got, DefaultUnknown)
	}
}

func TestResolveUnknownFromDefaults(t *testing.T) {
	want := "?"
	c := &Config{Defaults: Defaults{Unknown: &want}}
	if got := c.Resolve("context", "{pct}").Unknown; got != want {
		t.Fatalf("Unknown = %q, want %q", got, want)
	}
}

func TestResolveUnknownSegmentOverridesDefaults(t *testing.T) {
	global, seg := "?", "~"
	c := &Config{
		Defaults: Defaults{Unknown: &global},
		Segments: map[string]*Segment{"cost": {Unknown: &seg}},
	}
	if got := c.Resolve("cost", "${usd}").Unknown; got != seg {
		t.Fatalf("Unknown = %q, want %q", got, seg)
	}
}

// Empty string is a setting, not absent one: it is how a segment opt out.
func TestResolveUnknownEmptyStringSurvivesResolution(t *testing.T) {
	off := ""
	c := &Config{Segments: map[string]*Segment{"cost": {Unknown: &off}}}
	if got := c.Resolve("cost", "${usd}").Unknown; got != "" {
		t.Fatalf("Unknown = %q, want empty", got)
	}
}
