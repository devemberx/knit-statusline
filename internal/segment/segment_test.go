package segment

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
)

// Shared with builtin_test.go, command_test.go, git_test.go, tokens_test.go.
func input(t *testing.T, doc []byte) *schema.Input {
	t.Helper()
	in, err := schema.Parse(doc)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return in
}

// ctx build Context way statusline.Render will, resolving against empty config
// so defaults match what user get holding no statusline.toml.
// Shared with builtin_test.go, command_test.go, git_test.go, tokens_test.go.
func ctx(t *testing.T, doc []byte, kind string) Context {
	t.Helper()
	def, ok := Lookup(kind)
	if !ok {
		t.Fatalf("no segment registered as %q", kind)
	}
	c := &config.Config{Segments: map[string]*config.Segment{}}
	return Context{
		In:      input(t, doc),
		Cfg:     c.Resolve(kind, def.DefaultTemplate),
		Palette: render.NoColor(),
		Now:     time.Unix(fixtures.PreviewEpoch, 0),
	}
}

// draw expand one segment way statusline will, so assertion read text user see.
// Shared with builtin_test.go, git_test.go, tokens_test.go.
func draw(c Context) string {
	res := Build(c)
	if res.Empty {
		return ""
	}
	return c.Palette.Expand(c.Cfg.Template, res.Fields, res.Base)
}

func TestEverySegmentIsRegisteredAndSorted(t *testing.T) {
	names := Names()
	if !slices.IsSorted(names) {
		t.Errorf("Names() must be sorted for doctor output: %v", names)
	}
	for _, want := range []string{
		"caveman", "command", "context", "cost", "dir", "effort", "fast_mode",
		"limit.5h", "limit.7d", "lines", "model", "output_style", "pr", "repo",
		"session", "thinking", "tokens", "version", "vim",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("segment %q not registered", want)
		}
	}
}

// CLAUDE.md rule, checked rather than trusted: field Build populate but
// Def.Fields omit is invisible to doctor and to config validation, so user meet
// placeholder that expand to nothing. Run across all three fixtures, since which
// fields appear depend on input.
func TestProducedFieldsAreDeclared(t *testing.T) {
	for _, f := range []struct {
		name string
		doc  []byte
	}{
		{"full", fixtures.Full},
		{"sparse", fixtures.Sparse},
		{"empty", fixtures.Empty},
	} {
		for _, kind := range Names() {
			def, _ := Lookup(kind)
			for name := range Build(ctx(t, f.doc, kind)).Fields {
				if !slices.Contains(def.Fields, name) {
					t.Errorf("%s/%s: Build produce {%s}, absent from Def.Fields %v",
						f.name, kind, name, def.Fields)
				}
			}
		}
	}
}

// Default template of every segment must name only declared fields, else
// reordering a row inherit text that render blank.
func TestDefaultTemplatesNameDeclaredFieldsOnly(t *testing.T) {
	for _, kind := range Names() {
		def, _ := Lookup(kind)
		for _, ph := range templateFields(def.DefaultTemplate) {
			if !slices.Contains(def.Fields, ph) {
				t.Errorf("%s: default template use {%s}, absent from Def.Fields %v",
					kind, ph, def.Fields)
			}
		}
	}
}

// Minimal placeholder reader, enough for default templates. config.placeholders
// do this for user templates but sit unexported in its own package.
func templateFields(tmpl string) []string {
	var out []string
	for {
		open := strings.IndexByte(tmpl, '{')
		if open < 0 {
			return out
		}
		end := strings.IndexByte(tmpl[open:], '}')
		if end < 0 {
			return out
		}
		inner := tmpl[open+1 : open+end]
		tmpl = tmpl[open+end+1:]

		if i := strings.IndexByte(inner, ':'); i >= 0 {
			inner = inner[:i]
		}
		if inner != "" {
			out = append(out, inner)
		}
	}
}

// Two segments under one key mean one silently win, and which one depend on
// init order across files.
func TestRegisterRejectsDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering an existing name should panic")
		}
	}()
	register("model", Def{})
}

// registerTemp add segment for one test alone. Registry is package global, so
// test segment left behind join every later test walking Names(), and which
// tests those are depend on -shuffle ordering.
func registerTemp(t *testing.T, name string, d Def) {
	t.Helper()
	register(name, d)
	t.Cleanup(func() { delete(registry, name) })
}

// Build contain panic. Segments run subprocesses, file reads and user commands;
// one bad segment must cost its own slot, never whole row.
func TestBuildRecoversFromPanic(t *testing.T) {
	registerTemp(t, "panics-on-purpose", Def{
		Fields:          []string{"x"},
		DefaultTemplate: "{x}",
		Build:           func(Context) Result { panic("boom") },
	})

	if res := Build(ctx(t, fixtures.Empty, "panics-on-purpose")); !res.Empty {
		t.Errorf("panicking segment returned %+v, want empty", res)
	}
}

// Unknown kind reach Build when config name a segment this build lack. Skip it,
// never panic.
func TestBuildIgnoresUnknownKind(t *testing.T) {
	c := Context{Cfg: config.Resolved{Kind: "no-such-segment"}}
	if res := Build(c); !res.Empty {
		t.Errorf("unknown kind returned %+v, want empty", res)
	}
}

func TestKnownReportsDeclaredFields(t *testing.T) {
	fields, ok := Known("model")
	if !ok {
		t.Fatal("model should be known")
	}
	if !slices.Equal(fields, []string{"name", "family", "version", "id"}) {
		t.Errorf("model fields = %v", fields)
	}
	if _, ok := Known("no-such-segment"); ok {
		t.Error("unknown kind reported as known")
	}
}

// Zero budget would cancel before exec, turning a nonsense setting into every
// subprocess failing. Fall back instead.
func TestBudgetFallsBackOnNonsenseTimeout(t *testing.T) {
	for _, tc := range []struct {
		ms   int
		want time.Duration
	}{
		{0, config.DefaultTimeoutMS * time.Millisecond},
		{-5, config.DefaultTimeoutMS * time.Millisecond},
		{250, 250 * time.Millisecond},
	} {
		c := Context{Cfg: config.Resolved{TimeoutMS: tc.ms}}
		if got := c.budget(); got != tc.want {
			t.Errorf("budget(%d) = %v, want %v", tc.ms, got, tc.want)
		}
	}
}

// Thresholds travel from resolved config into render grading unchanged.
func TestThresholdsComeFromResolvedConfig(t *testing.T) {
	c := Context{Cfg: config.Resolved{Warn: 10, High: 20, Crit: 30}}
	want := render.Thresholds{Warn: 10, High: 20, Crit: 30}
	if got := c.Thresholds(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Empty document is floor every segment must survive: valid JSON, nothing
// populated. None may panic, and none may claim data it does not have.
// Stable segment skipped -- it hold its slot with placeholder rather than
// drop, covered per-segment in builtin_test.go. Read via Lookup rather than
// a hand-kept name list, so set stays correct as more segments go Stable.
func TestEverySegmentSurvivesEmptyDocument(t *testing.T) {
	for _, kind := range Names() {
		if def, ok := Lookup(kind); ok && def.Stable {
			continue
		}
		if got := draw(ctx(t, fixtures.Empty, kind)); got != "" {
			t.Errorf("%s rendered %q from an empty document", kind, got)
		}
	}
}

func TestHoldsSlotNeedsStableAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stable  bool
		unknown string
		want    bool
	}{
		{"stable with text", true, "…", true},
		{"stable opted out", true, "", false},
		{"not stable", false, "…", false},
		{"neither", false, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Context{stable: tc.stable, Cfg: config.Resolved{Unknown: tc.unknown}}
			if got := c.holdsSlot(); got != tc.want {
				t.Fatalf("holdsSlot = %v, want %v", got, tc.want)
			}
		})
	}
}

// Build own stable flag: builder read it through Context, never look
// registry up again.
func TestBuildInjectsStableFromRegistry(t *testing.T) {
	var seen bool
	registerTemp(t, "test.stable-probe", Def{
		Fields:          []string{"x"},
		DefaultTemplate: "{x}",
		Stable:          true,
		Build: func(c Context) Result {
			seen = c.stable
			return empty
		},
	})
	Build(Context{Cfg: config.Resolved{Kind: "test.stable-probe", Unknown: "…"}})
	if !seen {
		t.Fatal("Build did not inject Def.Stable into Context")
	}
}
