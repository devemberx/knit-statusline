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
// config changed underneath; drop it rather than print raw brace into user's
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

// Doubled brace escape nothing: inner read as name "{n", miss lookup, and drop,
// leaving stray "}". Pinned so escape syntax land by decision, not by drift.
func TestExpandLeavesDoubledBraceUnescaped(t *testing.T) {
	got := NoColor().Expand("{{n}}", Fields{"n": Plain("42")}, "")
	if got != "}" {
		t.Errorf("got %q, want %q", got, "}")
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

// Padding measure visible text. Computed over string already carrying escape
// codes, a colored number come out short by however long those codes run -- so
// a colored field and a plain one align to same visible width.
func TestPadMeasuresVisibleWidthNotEscapes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	colored := NewPalette().Expand("{n:>5}", Fields{"n": Colored("42", Red)}, "")
	plain := NoColor().Expand("{n:>5}", Fields{"n": Plain("42")}, "")

	if !strings.Contains(colored, string(Red)) {
		t.Fatalf("colored field carry no escape, test prove nothing: %q", colored)
	}
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

// Base dress literal text only. Reference layout keep labels muted and let
// numbers carry severity, so field color survive base.
func TestExpandColorsLiteralWithBaseAndFieldWithItsOwn(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	got := NewPalette().Expand("ctx {pct}%", Fields{"pct": Colored("42", Red)}, Dim)
	want := string(Dim) + "ctx " + reset +
		string(Red) + "42" + reset +
		string(Dim) + "%" + reset
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoColorPaletteEmitsNoEscapes(t *testing.T) {
	got := NoColor().Expand("{a} {b}", Fields{"a": Colored("x", Red), "b": Colored("y", Blue)}, Dim)
	if strings.Contains(got, "\033") {
		t.Errorf("escape sequence leaked: %q", got)
	}
}
