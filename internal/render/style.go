// Package render turn segment output into text Claude Code print.
package render

import (
	"os"
	"strings"
)

// Color is ANSI escape prefix. Empty Color = leave text alone, so no-color
// render hand out empty Colors instead of branching at every call site.
type Color string

// Reference palette, 24-bit foreground escapes.
const (
	Blue    Color = "\033[38;2;0;153;255m"
	Orange  Color = "\033[38;2;255;176;85m"
	Green   Color = "\033[38;2;0;175;80m"
	Cyan    Color = "\033[38;2;86;182;194m"
	Red     Color = "\033[38;2;255;85;85m"
	Yellow  Color = "\033[38;2;230;200;0m"
	White   Color = "\033[38;2;220;220;220m"
	Magenta Color = "\033[38;2;180;140;255m"
	Dim     Color = "\033[2m"

	reset = "\033[0m"
)

// Palette apply or suppress color for a whole render.
type Palette struct{ enabled bool }

// NewPalette honour NO_COLOR: any non-empty value disable color, so piping
// status line somewhere give clean text.
func NewPalette() Palette {
	return Palette{enabled: os.Getenv("NO_COLOR") == ""}
}

// NoColor emit no escapes. Golden tests compare plain text; preview use color.
func NoColor() Palette { return Palette{enabled: false} }

// Wrap apply c to s. Empty color or disabled palette return s unchanged, never
// a bare reset.
func (p Palette) Wrap(s string, c Color) string {
	if !p.enabled || c == "" || s == "" {
		return s
	}
	return string(c) + s + reset
}

// Thresholds map a percentage to a severity color.
type Thresholds struct{ Warn, High, Crit int }

// Color grade a percentage: green under warn, then orange, yellow, red at or
// over crit. Same escalation as reference statusline, so one glance at bar tell
// whether anything need attention.
func (t Thresholds) Color(pct float64) Color {
	switch {
	case pct >= float64(t.Crit):
		return Red
	case pct >= float64(t.High):
		return Yellow
	case pct >= float64(t.Warn):
		return Orange
	default:
		return Green
	}
}

const (
	barFilled = "●"
	barEmpty  = "○"
)

// Bar render a progress bar, clamping out-of-range percentages instead of
// drawing a negative or oversized one.
//
// Filled part carry severity color, remainder dimmed: bar read at a glance even
// in a crowded row.
//
// Cell count round, never truncate. Bar print beside a percentage rounded same
// way, and truncation put 9.6% on screen as "10%" against empty bar -- which
// read as nothing used at all.
func (p Palette) Bar(pct float64, width int, t Thresholds) string {
	if width <= 0 {
		return ""
	}
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}

	filled := int(pct*float64(width)/100 + 0.5)
	if filled > width {
		filled = width
	}

	return p.Wrap(strings.Repeat(barFilled, filled), t.Color(pct)) +
		p.Wrap(strings.Repeat(barEmpty, width-filled), Dim)
}
