package render

import (
	"strings"
	"testing"
)

func TestExpandSubstitutesFields(t *testing.T) {
	p := NoColor()
	got := p.Expand("ctx {pct}% of {size}", Fields{
		"pct":  Plain("42"),
		"size": Plain("200k"),
	}, "")

	if got != "ctx 42% of 200k" {
		t.Errorf("got %q", got)
	}
}

// Validate report unknown placeholders up front. One reaching render time mean
// config changed underneath; drop it rather than print a raw brace into user's
// terminal.
func TestExpandDropsUnknownPlaceholders(t *testing.T) {
	got := NoColor().Expand("a{missing}b", Fields{}, "")
	if got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}
}

func TestExpandTreatsUnterminatedBraceAsText(t *testing.T) {
	got := NoColor().Expand("100% {oops", Fields{}, "")
	if got != "100% {oops" {
		t.Errorf("got %q", got)
	}
}

func TestPadAlignment(t *testing.T) {
	p := NoColor()
	for _, tc := range []struct{ tmpl, want string }{
		{"{n:>5}", "   42"},
		{"{n:<5}", "42   "},
		{"{n:5}", "   42"}, // a bare width right-aligns, as a number should
		{"{n:>1}", "42"},   // never truncate; a clipped number is a wrong number
		{"{n:>bad}", "42"}, // unparsable spec ignored rather than fatal
		{"{n}", "42"},
	} {
		if got := p.Expand(tc.tmpl, Fields{"n": Plain("42")}, ""); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}

// Padding measure visible text. Computed over a string already carrying escape
// codes, a colored number come out short by however long those codes run -- so
// a colored field and a plain one align to same visible width.
func TestPadMeasuresVisibleWidthNotEscapes(t *testing.T) {
	colored := NewPalette().Expand("{n:>5}", Fields{"n": Colored("42", Red)}, "")
	plain := NoColor().Expand("{n:>5}", Fields{"n": Plain("42")}, "")

	if got := stripEscapes(colored); got != "   42" {
		t.Errorf("colored field visible text = %q, want %q", got, "   42")
	}
	if plain != "   42" {
		t.Errorf("plain field = %q, want %q", plain, "   42")
	}
	if stripEscapes(colored) != plain {
		t.Errorf("colored %q and plain %q do not align", stripEscapes(colored), plain)
	}
}

// Strip ANSI SGR sequences, leaving what terminal display.
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
	if got := p.Bar(50, 0, Thresholds{}); got != "" {
		t.Errorf("zero width should render nothing: %q", got)
	}
}

// Cell count round, matching percentage printed beside it. Truncation put 9.6%
// on screen as "10%" against empty bar, which read as nothing used at all.
func TestBarRoundsToMatchPrintedPercentage(t *testing.T) {
	p := NoColor()
	for _, tc := range []struct {
		pct    float64
		filled int
	}{
		{9.6, 1},  // print as "10%", so one cell
		{4, 0},    // print as "4%", still no cell
		{5, 1},    // half a cell round up
		{95, 10},  // print as "95%", round to full
		{100, 10}, // never over-fill
	} {
		if got := strings.Count(p.Bar(tc.pct, 10, Thresholds{}), "●"); got != tc.filled {
			t.Errorf("Bar(%v) filled %d cells, want %d", tc.pct, got, tc.filled)
		}
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

func TestNoColorPaletteEmitsNoEscapes(t *testing.T) {
	got := NoColor().Expand("{a} {b}", Fields{"a": Colored("x", Red), "b": Colored("y", Blue)}, Dim)
	if strings.Contains(got, "\033") {
		t.Errorf("escape sequence leaked: %q", got)
	}
}

func TestWrapLeavesEmptyStringsAlone(t *testing.T) {
	if got := NewPalette().Wrap("", Red); got != "" {
		t.Errorf("wrapping an empty string produced %q", got)
	}
}
