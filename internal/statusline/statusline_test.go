package statusline

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	_ "github.com/devemberx/knit-statusline/internal/segment"
)

// TestMain pin zone golden expectations are written in.
//
// Rate limit resets format in viewer's local zone, which is what user want and
// what make rendered output machine-dependent: same epoch read 8:00am in UTC and
// 5:00pm nine hours east. Without this, assertions below encode whichever zone
// their author sat in and fail everywhere else. CI run suite under a second zone
// to keep it honest.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// parseTOML build config from inline document, so each test state layout it
// exercise rather than pointing at a shared file.
func parseTOML(t *testing.T, src string) *config.Config {
	t.Helper()
	cfg, err := config.ParseBytes([]byte(src), "test.toml")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

func draw(t *testing.T, cfg *config.Config, doc []byte) string {
	t.Helper()
	in, err := schema.Parse(doc)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return Render(cfg, in, Options{
		Palette:  render.NoColor(),
		Now:      time.Unix(fixtures.PreviewEpoch, 0),
		CacheDir: t.TempDir(),
	})
}

// drawPreset draw a builtin preset with color disabled, so assertions compare
// what user read rather than escape sequences.
func drawPreset(t *testing.T, preset string, doc []byte) string {
	t.Helper()
	cfg, err := config.Preset(preset)
	if err != nil {
		t.Fatalf("preset %s: %v", preset, err)
	}
	return draw(t, cfg, doc)
}

// Reference preset must reproduce layout it is named for.
func TestReferencePresetOnFullData(t *testing.T) {
	got := drawPreset(t, "reference", fixtures.Full)
	want := strings.Join([]string{
		"Opus 4.8 │ ✍️ 42% │ acme │ ⏱ 1h15m │ ● high",
		"",
		"current ●●●●○○○○○○  42% ⟳ 8:00am",
		"weekly  ●●○○○○○○○○  18% ⟳ jul 27, 8:00am",
	}, "\n")

	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// Case reference bash implementation get wrong. Nothing here is available, so
// nothing about it may be invented: no "0%" context, no empty rate limit bars,
// and no blank rows left behind by segments that vanished.
func TestReferencePresetOnSparseData(t *testing.T) {
	got := drawPreset(t, "reference", fixtures.Sparse)
	want := "Sonnet 5 │ scratch │ ⏱ 1s"

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, forbidden := range []string{"0%", "current", "weekly", "○"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("output invents %q from missing data: %q", forbidden, got)
		}
	}
}

// Empty document is floor: no panic, no blank output.
func TestEmptyDocumentStillRenders(t *testing.T) {
	for _, preset := range []string{"minimal", "reference", "verbose", "api"} {
		got := drawPreset(t, preset, fixtures.Empty)
		if strings.Contains(got, "%") || strings.Contains(got, "│") {
			t.Errorf("%s: empty document produced content: %q", preset, got)
		}
	}
}

// Every preset must survive every fixture. Preset shipping broken is one users
// meet on install, before they ever open statusline.toml.
func TestEveryPresetRendersEveryFixture(t *testing.T) {
	for _, preset := range config.PresetNames() {
		for _, f := range []struct {
			name string
			doc  []byte
		}{
			{"full", fixtures.Full},
			{"sparse", fixtures.Sparse},
			{"empty", fixtures.Empty},
		} {
			got := drawPreset(t, preset, f.doc)
			if strings.Contains(got, "{") || strings.Contains(got, "}") {
				t.Errorf("%s/%s: unexpanded placeholder in %q", preset, f.name, got)
			}
			for _, row := range strings.Split(got, "\n") {
				if strings.HasPrefix(row, "│") || strings.HasSuffix(row, "│") {
					t.Errorf("%s/%s: bare separator at a row edge: %q", preset, f.name, row)
				}
			}
		}
	}
}

// Original request behind this project: put two rate limit windows on one row.
// It has to be a one-line config change, or customisation model has failed.
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

// Row whose segments all render empty is dropped, so absent value never leave a
// bare separator standing for information that does not exist.
func TestRowWithOnlyEmptySegmentsIsDropped(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model"]

[[lines]]
segments = ["limit.5h", "limit.7d"]
`)

	if got := draw(t, cfg, fixtures.Sparse); got != "Sonnet 5" {
		t.Errorf("got %q, want %q", got, "Sonnet 5")
	}
}

// Blank row between content is deliberate and must survive, unlike one left
// dangling at either edge.
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

// Alignment spec exist to produce padding. Trimming segment output undo every
// spec sitting at either end of a template, so "{pct:>5}%" print "42%" and
// columns stop lining up.
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

// Padding alone is not content. Segment rendering nothing but spaces still lose
// its slot, else separators fence off a column holding no text.
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

// Broken config must still draw a row, with a marker naming file. Alternative --
// empty status line -- give user nothing to act on.
func TestWarningIsAppendedToFirstRow(t *testing.T) {
	cfg, err := config.Preset("minimal")
	if err != nil {
		t.Fatal(err)
	}
	in, _ := schema.Parse(fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NoColor(), Warning: "statusline.toml:12"})
	if !strings.Contains(got, "⚠ statusline.toml:12") {
		t.Errorf("warning missing from %q", got)
	}
	if !strings.Contains(got, "Opus") {
		t.Errorf("content lost alongside the warning: %q", got)
	}
}

// Marker must reach user even when every segment came back empty, since that is
// exactly what config naming nothing renderable produce.
func TestWarningSurvivesAnEmptyLayout(t *testing.T) {
	cfg := parseTOML(t, "[[lines]]\nsegments = [\"vim\"]\n")
	in, _ := schema.Parse(fixtures.Empty)

	got := Render(cfg, in, Options{Palette: render.NoColor(), Warning: "statusline.toml:3"})
	if got != "⚠ statusline.toml:3" {
		t.Errorf("got %q", got)
	}
}

func TestFallbackAlwaysProducesText(t *testing.T) {
	p := render.NoColor()
	if got := Fallback(nil, p); got != "Claude" {
		t.Errorf("nil input fallback = %q, want Claude", got)
	}
	in, _ := schema.Parse(fixtures.Full)
	if got := Fallback(in, p); got != "Opus" {
		t.Errorf("fallback = %q, want Opus", got)
	}
	bare, _ := schema.Parse(fixtures.Empty)
	if got := Fallback(bare, p); got != "Claude" {
		t.Errorf("unnamed model fallback = %q, want Claude", got)
	}
}

// Unknown segment name is skipped rather than taking its row down with it.
func TestUnknownSegmentIsSkipped(t *testing.T) {
	cfg := parseTOML(t, `
[[lines]]
segments = ["model", "no-such-segment", "version"]
`)

	if got := draw(t, cfg, fixtures.Full); got != "Opus 4.8 │ 2.1.211" {
		t.Errorf("got %q", got)
	}
}

// Now defaulting to wall clock keep Render usable from anywhere, and a zero
// Options must not render times at Unix epoch.
func TestZeroNowDefaultsToWallClock(t *testing.T) {
	cfg, err := config.Preset("reference")
	if err != nil {
		t.Fatal(err)
	}
	in, _ := schema.Parse(fixtures.Full)

	got := Render(cfg, in, Options{Palette: render.NoColor()})
	if strings.Contains(got, "jan 1,") {
		t.Errorf("zero Now leaked the epoch into %q", got)
	}
}
