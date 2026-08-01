// Package render turn segment output into text Claude Code print.
package render

import (
	"math"
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
	Pink    Color = "\033[38;2;255;60;175m"
	Dim     Color = "\033[2m"

	reset = "\033[0m"
)

// Palette apply or suppress color across one render.
type Palette struct{ enabled bool }

// NewPalette honour NO_COLOR per no-color.org: any non-empty value disable
// color, empty value leave it on. Nothing detect pipe -- Claude Code capture
// this output either way.
func NewPalette() Palette {
	return Palette{enabled: os.Getenv("NO_COLOR") == ""}
}

// NoColor emit no escapes. Golden tests compare plain text; preview use color.
func NoColor() Palette { return Palette{enabled: false} }

// Wrap apply c to s. Empty color or disabled palette return s unchanged, never
// bare reset.
func (p Palette) Wrap(s string, c Color) string {
	if !p.enabled || c == "" || s == "" {
		return s
	}
	return string(c) + s + reset
}

// Thresholds map percentage to severity color.
type Thresholds struct{ Warn, High, Crit int }

// Color grade percentage: green under Warn, then orange, yellow, red at or over
// Crit. Same escalation as reference statusline, so one glance at bar tell
// whether anything need attention.
//
// Zero value grade nothing, matching empty Color meaning elsewhere. Every field
// zero otherwise send 0% down Crit branch, so caller who forget to wire config
// paint whole row red.
func (t Thresholds) Color(pct float64) Color {
	if t == (Thresholds{}) {
		return ""
	}
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

// Bar render progress bar, clamping out-of-range percentage instead of drawing
// negative or oversized one.
//
// Filled part carry severity color, remainder same hue dimmed: bar read at
// glance even in crowded row.
//
// Cell count round to nearest cell, never truncate. Truncation put 9.6% on
// screen as "10%" against empty bar, which read as nothing used at all. Bar hold
// width steps against 100 for percentage, so two still disagree below half cell.
//
// NaN clamp to 0. It pass both range test, and int(NaN) is platform-defined --
// minInt64 on amd64 -- which reach strings.Repeat as negative count and panic.
func (p Palette) Bar(pct float64, width int, t Thresholds) string {
	if width <= 0 {
		return ""
	}
	switch {
	case math.IsNaN(pct), pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}

	filled := int(math.Round(pct * float64(width) / 100))
	on := strings.Repeat(barFilled, filled)
	off := strings.Repeat(barEmpty, width-filled)

	c := t.Color(pct)
	if !p.enabled || c == "" {
		return on + off
	}
	// Dim layer over severity color instead of replacing it: "\033[2m" set faint
	// and carry no foreground of its own. Reset between halves drop hue and
	// leave empty cells default grey, untied to their own bar.
	return string(c) + on + string(Dim) + off + reset
}
