package statusline

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/segment"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func ptr[T any](v T) *T { return &v }

// Pin zone golden expectations written in.
//
// Rate limit reset format in viewer's local zone: same epoch read 8:00am UTC,
// 5:00pm nine hours east. CI rerun suite under second zone.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// Inline document = test state layout it exercise, no shared file.
func parseTOML(t *testing.T, src string) *config.Config {
	t.Helper()
	cfg, err := config.ParseBytes([]byte(src), "test.toml")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

func parseInput(t *testing.T, doc []byte) *schema.Input {
	t.Helper()
	in, err := schema.Parse(doc)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return in
}

// Empty config root by default: segment reading file beside settings.json find
// nothing, so unrelated golden row stay put wherever suite run.
func draw(t *testing.T, cfg *config.Config, doc []byte) string {
	t.Helper()
	return drawIn(t, cfg, doc, t.TempDir())
}

func drawIn(t *testing.T, cfg *config.Config, doc []byte, configDir string) string {
	t.Helper()
	return Render(cfg, parseInput(t, doc), Options{
		Palette:   render.NoColor(),
		Now:       time.Unix(fixtures.PreviewEpoch, 0),
		CacheDir:  t.TempDir(),
		ConfigDir: configDir,
	})
}

// Color off: assertions compare what user read, not escape sequences.
func drawPreset(t *testing.T, preset string, doc []byte) string {
	t.Helper()
	cfg, err := config.Preset(preset)
	if err != nil {
		t.Fatalf("preset %s: %v", preset, err)
	}
	return draw(t, cfg, doc)
}

func TestReferencePresetOnFullData(t *testing.T) {
	got := drawPreset(t, "reference", fixtures.Full)
	want := strings.Join([]string{
		"Opus 4.8 │ ✍️ 42% │ acme │ ⏱ 1h15m │ ◕ high",
		"",
		"current ●●●●○○○○○○  42% ⟳ 8:00am",
		"weekly  ●●○○○○○○○○  18% ⟳ jul 27, 8:00am",
	}, "\n")

	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// Row preset's own header comment promise. Pinned here so comment and render
// cannot drift apart.
func TestMinimalPresetOnFullData(t *testing.T) {
	got := drawPreset(t, "minimal", fixtures.Full)
	want := "Opus 4.8 │ ✍️ 42% │ acme"

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Case reference bash implementation get wrong: no blank rows left by
// vanished segments. context differs from limit.5h/limit.7d here: Sparse's
// transcript_path name file that does not exist, so probe prove session fresh
// and 0% is fact, not invention. Rate limit windows carry no such proof --
// account-wide state survive across sessions, so absent rate_limits still owe
// a row, held at placeholder rather than a zero that would lie about room left.
func TestReferencePresetOnSparseData(t *testing.T) {
	got := drawPreset(t, "reference", fixtures.Sparse)
	want := strings.Join([]string{
		"Sonnet 5 │ ✍️ 0% │ scratch │ ⏱ 1s",
		"",
		"current ○○○○○○○○○○   …%",
		"weekly  ○○○○○○○○○○   …%",
	}, "\n")

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Every non-stable segment come back empty. context, session, limit.5h and
// limit.7d differ: fixtures.Empty carry no transcript_path, freshness
// unprovable rather than proven, so each Stable slot hold placeholder instead
// of dropping or printing bare zero. Printing Fallback past that remains
// caller's job.
func TestEmptyDocumentRendersOnlyStableSlots(t *testing.T) {
	for _, tc := range []struct{ preset, want string }{
		{"minimal", "✍️ …%"},
		{"reference", "✍️ …% │ ⏱ …\n\ncurrent ○○○○○○○○○○   …%\nweekly  ○○○○○○○○○○   …%"},
		{"verbose", "✍️ …% │ ⏱ …\ncurrent ○○○○○○○○○○   …%  weekly ○○○○○○○○○○   …%\n↑… ↓… │ +… -… │ $…"},
		{"api", "✍️ …% │ ⏱ …\n↑… ↓… │ +… -… │ $…"},
	} {
		if got := drawPreset(t, tc.preset, fixtures.Empty); got != tc.want {
			t.Errorf("%s: empty document produced %q, want %q", tc.preset, got, tc.want)
		}
	}
}

// render.Expand drop unknown placeholder silently. Preset naming field that does
// not exist render nothing and leave no brace behind for
// TestEveryPresetRendersEveryFixture to catch.
func TestEveryPresetValidates(t *testing.T) {
	for _, name := range config.PresetNames() {
		cfg, err := config.Preset(name)
		if err != nil {
			t.Fatalf("preset %s: %v", name, err)
		}
		src, err := config.PresetSource(name)
		if err != nil {
			t.Fatalf("preset source %s: %v", name, err)
		}
		for _, e := range config.Validate(cfg, config.FileOrigin("preset:"+name, src), segment.Known) {
			t.Errorf("%s: %v", name, e)
		}
	}
}

// Broken preset is one users meet on install, before they open statusline.toml.
//
// Separators read off config: verbose second row use "  ", so hardcoded "│"
// check that row for nothing.
func TestEveryPresetRendersEveryFixture(t *testing.T) {
	for _, preset := range config.PresetNames() {
		cfg, err := config.Preset(preset)
		if err != nil {
			t.Fatalf("preset %s: %v", preset, err)
		}

		var seps []string
		for _, line := range cfg.Lines {
			if sep := strings.TrimSpace(cfg.Separator(line)); sep != "" {
				seps = append(seps, sep)
			}
		}

		for _, f := range []struct {
			name string
			doc  []byte
		}{
			{"full", fixtures.Full},
			{"sparse", fixtures.Sparse},
			{"unknown", fixtures.Unknown},
			{"empty", fixtures.Empty},
		} {
			got := draw(t, cfg, f.doc)
			if strings.Contains(got, "{") || strings.Contains(got, "}") {
				t.Errorf("%s/%s: unexpanded placeholder in %q", preset, f.name, got)
			}
			for _, row := range strings.Split(got, "\n") {
				row = strings.TrimSpace(row)
				for _, sep := range seps {
					if strings.HasPrefix(row, sep) || strings.HasSuffix(row, sep) {
						t.Errorf("%s/%s: bare separator %q at a row edge: %q", preset, f.name, sep, row)
					}
				}
			}
		}
	}
}

// Original request behind this project: two rate limit windows on one row. Must
// be one-line config change, or customisation model has failed.
func TestTwoLimitsOnOneRow(t *testing.T) {
	cfg, err := config.Preset("reference")
	if err != nil {
		t.Fatal(err)
	}
	override := parseTOML(t, `
[[lines]]
segments = ["limit.5h", "limit.7d"]
separator = "  "
`)

	got := draw(t, config.Merge(cfg, override), fixtures.Full)
	if strings.Count(got, "\n") != 0 {
		t.Errorf("expected a single row, got:\n%s", got)
	}
	if !strings.Contains(got, "current") || !strings.Contains(got, "weekly") {
		t.Errorf("both windows should appear: %q", got)
	}
}

// Absent value leave no bare separator standing for information that does not
// exist.
// pr and vim stand in for "genuinely empty on Sparse" -- limit.5h/limit.7d no
// longer qualify: both are Stable, so absent rate_limits hold their row at a
// placeholder rather than dropping it.
func TestRowWithOnlyEmptySegmentsIsDropped(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model"]

[[lines]]
segments = ["pr", "vim"]
`)

	if got := draw(t, cfg, fixtures.Sparse); got != "Sonnet 5" {
		t.Errorf("got %q, want %q", got, "Sonnet 5")
	}
}

// Blank between content deliberate. Blank at either edge is not.
func TestBlankRowsTrimmedAtEdgesOnly(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
[[lines]]
segments = ["model"]
[[lines]]
[[lines]]
segments = ["version"]
[[lines]]
`)

	want := "Opus 4.8\n\n2.1.211"
	if got := draw(t, cfg, fixtures.Full); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Both pr and vim empty in Sparse, so their row drop and leave two blanks
// adjacent. Run of two or more collapse to one. limit.5h/limit.7d no longer
// serve here -- both Stable, so absent rate_limits hold their row instead of
// dropping it.
func TestBlankRunCollapses(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model"]

[[lines]]

[[lines]]
segments = ["pr", "vim"]

[[lines]]

[[lines]]
segments = ["version"]
`)

	want := "Sonnet 5\n\n2.1.211"
	if got := draw(t, cfg, fixtures.Sparse); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Alignment spec exist to produce padding. Trimming segment output undo every
// spec sitting at either end of template, so "{pct:>5}%" print "42%" and columns
// stop lining up.
func TestAlignmentPaddingSurvives(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["limit.5h"]

[segments."limit.5h"]
template = "{pct:>5}%"
`)

	if got := draw(t, cfg, fixtures.Full); got != "   42%" {
		t.Errorf("got %q, want %q", got, "   42%")
	}
}

// Padding alone is not content. Segment rendering only spaces lose its slot,
// else separators fence off column holding no text.
func TestWhitespaceOnlySegmentIsDropped(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model", "blank"]

[segments.blank]
type = "version"
template = "  {version:>3}  "
`)

	if got := draw(t, cfg, fixtures.Sparse); !strings.Contains(got, "2.1.211") {
		t.Errorf("version should still render: %q", got)
	}

	empty := parseTOML(t, `
[[lines]]
segments = ["model", "blank"]

[segments.blank]
type = "vim"
template = "   {mode}   "
`)
	got := draw(t, empty, fixtures.Sparse)
	if got != "Sonnet 5" {
		t.Errorf("got %q, want the model alone", got)
	}
}

// Broken config still draw row, with marker naming file. Empty status line give
// user nothing to act on.
func TestWarningIsAppendedToFirstRow(t *testing.T) {
	cfg, err := config.Preset("minimal")
	if err != nil {
		t.Fatal(err)
	}
	in := parseInput(t, fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NoColor(), Warning: "statusline.toml:12"})
	if !strings.Contains(got, "⚠ statusline.toml:12") {
		t.Errorf("warning missing from %q", got)
	}
	if !strings.Contains(got, "Opus") {
		t.Errorf("content lost alongside the warning: %q", got)
	}
}

// Config naming nothing renderable still owe user marker.
func TestWarningSurvivesAnEmptyLayout(t *testing.T) {
	cfg := parseTOML(t, "[[lines]]\nsegments = [\"vim\"]\n")
	in := parseInput(t, fixtures.Empty)

	got := Render(cfg, in, Options{Palette: render.NoColor(), Warning: "statusline.toml:3"})
	if got != "⚠ statusline.toml:3" {
		t.Errorf("got %q", got)
	}
}

// Marker land on first row surviving collapse, never on leading blank.
func TestWarningSkipsLeadingBlankRow(t *testing.T) {
	cfg := parseTOML(t, "[[lines]]\n[[lines]]\nsegments = [\"model\"]\n")
	in := parseInput(t, fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NoColor(), Warning: "statusline.toml:4"})
	want := "Opus 4.8 ⚠ statusline.toml:4"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Every other test run color off, so nothing else reach escape path through
// Render.
func TestColorReachesTheWarning(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	cfg, err := config.Preset("minimal")
	if err != nil {
		t.Fatal(err)
	}
	in := parseInput(t, fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NewPalette(), Warning: "statusline.toml:9"})
	want := "\033[38;2;230;200;0m⚠ statusline.toml:9\033[0m"
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want it to contain %q", got, want)
	}
}

// Same name on two rows otherwise shell out twice per redraw.
//
// fixtures.Empty carry empty cwd, so command run in test's own directory. Other
// fixtures point at paths that do not exist and command fail to start.
func TestSegmentRunsOnceAcrossRows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh is no cmd builtin")
	}
	counter := filepath.Join(t.TempDir(), "runs")
	cfg := parseTOML(t, fmt.Sprintf(`
[[lines]]
segments = ["probe"]

[[lines]]
segments = ["probe"]

[segments.probe]
type = "command"
command = "printf x >> %s; printf probe"
`, counter))

	if got := draw(t, cfg, fixtures.Empty); got != "probe\nprobe" {
		t.Errorf("got %q, want %q", got, "probe\nprobe")
	}

	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("segment ran %d times, want 1", len(runs))
	}
}

// Nil config otherwise panic inside Render, and panic print nothing at all.
// segment.Build recover per segment, Render itself does not.
func TestNilConfigRendersNothing(t *testing.T) {
	got := Render(nil, parseInput(t, fixtures.Full), Options{Palette: render.NoColor()})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFallbackAlwaysProducesText(t *testing.T) {
	p := render.NoColor()
	if got := Fallback(nil, p); got != "Claude" {
		t.Errorf("nil input fallback = %q, want Claude", got)
	}
	in := parseInput(t, fixtures.Full)
	if got := Fallback(in, p); got != "Opus" {
		t.Errorf("fallback = %q, want Opus", got)
	}
	bare := parseInput(t, fixtures.Empty)
	if got := Fallback(bare, p); got != "Claude" {
		t.Errorf("unnamed model fallback = %q, want Claude", got)
	}
}

// Unknown name lose own slot, not whole row.
func TestUnknownSegmentIsSkipped(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model", "no-such-segment", "version"]
`)

	if got := draw(t, cfg, fixtures.Full); got != "Opus 4.8 │ 2.1.211" {
		t.Errorf("got %q", got)
	}
}

// Now default to wall clock, so zero Options must not render times at Unix
// epoch.
func TestZeroNowDefaultsToWallClock(t *testing.T) {
	cfg, err := config.Preset("reference")
	if err != nil {
		t.Fatal(err)
	}
	in := parseInput(t, fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NoColor()})
	if strings.Contains(got, "jan 1,") {
		t.Errorf("zero Now leaked the epoch into %q", got)
	}
}

// Whole path: Options carry config root, segment find flag, row show level.
func TestCavemanSegmentReachesRow(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, ".caveman-active"), []byte("ultra"), 0o644); err != nil {
		t.Fatalf("write flag: %v", err)
	}

	cfg := parseTOML(t, `
[[lines]]
segments = ["model", "caveman"]
`)
	if got, want := drawIn(t, cfg, fixtures.Full, configDir), "Opus 4.8 │ 🦴 ultra"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}

	// Plugin not installed: segment and its separator both go.
	if got, want := drawIn(t, cfg, fixtures.Full, t.TempDir()), "Opus 4.8"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Override exist so preview and tests pin a state without a transcript on disk.
func TestSessionFreshHonoursOverride(t *testing.T) {
	fresh, live := transcript.StateFresh, transcript.StateLive

	if !sessionFresh(&schema.Input{}, Options{SessionState: &fresh}) {
		t.Fatal("StateFresh override did not resolve fresh")
	}
	if sessionFresh(&schema.Input{}, Options{SessionState: &live}) {
		t.Fatal("StateLive override resolved fresh")
	}
}

// Override beat payload: fixture naming transcript path that happens to
// exist must not flip pinned state.
func TestSessionFreshOverrideBeatsProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	live := transcript.StateLive
	in := &schema.Input{TranscriptPath: path}

	if sessionFresh(in, Options{SessionState: &live}) {
		t.Fatal("probe overrode the pinned state")
	}
	if !sessionFresh(in, Options{}) {
		t.Fatal("empty transcript did not probe fresh")
	}
}

// Nil input probe nothing: live is answer that print no number.
func TestSessionFreshNilInputIsLive(t *testing.T) {
	if sessionFresh(nil, Options{}) {
		t.Fatal("nil input resolved fresh")
	}
}

// Pinned state must reach segments, not merely resolve inside Render.
func TestRenderSessionStateOverrideReachesSegments(t *testing.T) {
	fresh := transcript.StateFresh
	cfg := &config.Config{Lines: []config.Line{{Segments: []string{"session"}}}}

	got := Render(cfg, &schema.Input{}, Options{
		Palette:      render.NoColor(),
		SessionState: &fresh,
	})
	if !strings.Contains(got, "0s") {
		t.Fatalf("Render = %q, want a fresh-zero duration", got)
	}
}

// Row shape must not change as usage arrives. Two payloads differing only in
// usage-derived fields render same rows and same segment count per row, so no
// slot appear or vanish mid-session.
//
// Only stable segments take part: pr and repo legitimately come and go.
func TestRowShapeSurvivesMissingUsage(t *testing.T) {
	cfg := &config.Config{Lines: []config.Line{
		{Segments: []string{"model", "context", "session", "cost", "lines"}},
		{Segments: []string{"tokens"}},
		{Segments: []string{"limit.5h"}},
		{Segments: []string{"limit.7d"}},
	}}

	usd, ms, added, removed := 1.23, int64(4_500_000), int64(156), int64(23)
	p := 42.0
	known := &schema.Input{
		Model:   schema.Model{DisplayName: "Opus 5"},
		Context: &schema.ContextWin{UsedPercentage: &p},
		Cost: &schema.Cost{
			TotalCostUSD: &usd, TotalDurationMS: &ms,
			TotalLinesAdded: &added, TotalLinesRemoved: &removed,
		},
		RateLimits: &schema.RateLimits{
			FiveHour: &schema.Window{UsedPercentage: ptr(42.0)},
			SevenDay: &schema.Window{UsedPercentage: ptr(18.0)},
		},
	}
	bare := &schema.Input{Model: schema.Model{DisplayName: "Opus 5"}}

	live := transcript.StateLive
	opts := Options{Palette: render.NoColor(), SessionState: &live}

	shape := func(in *schema.Input) []int {
		var out []int
		for _, row := range strings.Split(Render(cfg, in, opts), "\n") {
			out = append(out, strings.Count(row, config.DefaultSeparator)+1)
		}
		return out
	}

	got, want := shape(bare), shape(known)
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d segment count = %d, want %d", i+1, got[i], want[i])
		}
	}
}

// Same contract at other end: a fresh session fill slots with zeros rather
// than dropping them.
func TestRowShapeSurvivesFreshSession(t *testing.T) {
	cfg := &config.Config{Lines: []config.Line{
		{Segments: []string{"model", "context", "session", "cost", "lines"}},
	}}
	fresh := transcript.StateFresh
	in := &schema.Input{Model: schema.Model{DisplayName: "Opus 5"}}

	got := Render(cfg, in, Options{Palette: render.NoColor(), SessionState: &fresh})
	for _, want := range []string{"Opus 5", "0%", "0s", "$0.00", "+0 -0"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render = %q, missing %q", got, want)
		}
	}
}
