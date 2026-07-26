package render

import (
	"math"
	"strings"
	"testing"
)

// Strip ANSI SGR sequences, leaving what terminal display. Shared with
// template_test.go.
func stripEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestNewPaletteHonoursNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := NewPalette().Wrap("x", Red); got != "x" {
		t.Errorf("NO_COLOR=1 = %q, want no escapes", got)
	}
	// no-color.org count presence, not truth: empty value keep color on.
	t.Setenv("NO_COLOR", "")
	if got := NewPalette().Wrap("x", Red); got != string(Red)+"x"+reset {
		t.Errorf("NO_COLOR= = %q, want color", got)
	}
}

func TestWrapLeavesEmptyStringsAlone(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if got := NewPalette().Wrap("", Red); got != "" {
		t.Errorf("wrapping an empty string produced %q", got)
	}
}

// Bar width counted in runes, so a bar of ● and ○ pad as it look, not as it
// encode.
func TestBarWidthIsCountedInRunes(t *testing.T) {
	got := NoColor().Bar(50, 10, Thresholds{})
	if strings.Count(got, "●") != 5 || strings.Count(got, "○") != 5 {
		t.Errorf("got %q, want 5 filled and 5 empty", got)
	}
}

func TestBarClampsOutOfRange(t *testing.T) {
	p := NoColor()
	if got := p.Bar(-20, 10, Thresholds{}); strings.Count(got, "○") != 10 {
		t.Errorf("negative percentage should render empty: %q", got)
	}
	if got := p.Bar(250, 10, Thresholds{}); strings.Count(got, "●") != 10 {
		t.Errorf("over-100 percentage should render full: %q", got)
	}
	if got := p.Bar(math.Inf(-1), 10, Thresholds{}); strings.Count(got, "○") != 10 {
		t.Errorf("-Inf should render empty: %q", got)
	}
	if got := p.Bar(math.Inf(1), 10, Thresholds{}); strings.Count(got, "●") != 10 {
		t.Errorf("+Inf should render full: %q", got)
	}
	if got := p.Bar(50, 0, Thresholds{}); got != "" {
		t.Errorf("zero width should render nothing: %q", got)
	}
}

// NaN pass both range test, so unclamped it reach int(NaN) -- minInt64 on amd64
// -- and panic strings.Repeat with negative count. Percentage no caller can
// compute draw empty.
func TestBarTreatsNaNAsZero(t *testing.T) {
	got := NoColor().Bar(math.NaN(), 10, Thresholds{})
	if strings.Count(got, "○") != 10 {
		t.Errorf("NaN percentage = %q, want empty bar", got)
	}
}

// Cell count round to nearest cell. Truncation put 9.6% on screen as "10%"
// against empty bar, which read as nothing used at all.
func TestBarRoundsToNearestCell(t *testing.T) {
	p := NoColor()
	for _, tc := range []struct {
		pct    float64
		filled int
	}{
		{9.6, 1},  // 0.96 cell round up
		{4, 0},    // 0.4 cell round down
		{5, 1},    // half cell round up
		{95, 10},  // 9.5 cell round up to full
		{100, 10}, // never over-fill
	} {
		if got := strings.Count(p.Bar(tc.pct, 10, Thresholds{}), "●"); got != tc.filled {
			t.Errorf("Bar(%v) filled %d cells, want %d", tc.pct, got, tc.filled)
		}
	}
}

func TestBarColorsFilledBySeverityAndRemainderDim(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	got := NewPalette().Bar(50, 2, Thresholds{Warn: 50, High: 70, Crit: 90})
	want := string(Orange) + barFilled + reset + string(Dim) + barEmpty + reset
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestThresholdEscalation(t *testing.T) {
	th := Thresholds{Warn: 50, High: 70, Crit: 90}
	for _, tc := range []struct {
		pct  float64
		want Color
	}{
		{10, Green}, {49, Green},
		{50, Orange}, {69, Orange},
		{70, Yellow}, {89, Yellow},
		{90, Red}, {100, Red},
	} {
		if got := th.Color(tc.pct); got != tc.want {
			t.Errorf("Color(%v) = %q, want %q", tc.pct, got, tc.want)
		}
	}
}

// Unset Thresholds grade nothing. Every field zero otherwise put 0% past Crit,
// so config nobody wired paint whole row red.
func TestThresholdZeroValueGradesNothing(t *testing.T) {
	for _, pct := range []float64{0, 50, 100} {
		if got := (Thresholds{}).Color(pct); got != "" {
			t.Errorf("Color(%v) = %q, want no color", pct, got)
		}
	}
}
