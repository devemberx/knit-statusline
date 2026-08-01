package segment

import (
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func init() {
	register("effort", Def{
		Fields:          []string{"level", "icon"},
		DefaultTemplate: "{icon} {level}",
		Build:           buildEffort,
	})
}

// buildEffort report reasoning effort level.
//
// Own file, not builtin.go: ultracode detection open transcript off disk, and
// builtin.go hold segments that map already-parsed stdin fields alone.
func buildEffort(c Context) Result {
	// Absent on models without effort parameter, where showing default would
	// claim setting this session does not have.
	if c.In.Effort == nil || c.In.Effort.Level == "" {
		return empty
	}
	level := c.In.Effort.Level
	// Payload collapse ultracode into xhigh (claude-code#69068); transcript
	// markers carry live state. Gate on xhigh: stale enter marker after silent
	// drop (claude-code#80901 model switch, no exit written) must never upgrade
	// another level.
	if level == "xhigh" && ultracodeOn(c) {
		level = "ultracode"
	}
	icon, color := effortStyle(level)
	display := level
	if level == "ultracode" {
		// Row space tight; "ultra" read same and fit.
		display = "ultra"
	}
	return Result{
		Base: color,
		Fields: render.Fields{
			"icon":  render.Colored(icon, color),
			"level": render.Colored(display, color),
		},
	}
}

// Fill ramp read without color, so NO_COLOR terminal still separate max from
// high. Unknown level take empty circle, never medium's glyph.
func effortStyle(level string) (string, render.Color) {
	switch level {
	case "ultracode":
		// "ultracode" reach here two ways: transcript markers upgrading xhigh, and
		// claude-code#77812 builds leaking it verbatim in effort.level. Own hue,
		// not xhigh's Magenta: top tier must not read as one tier down at a glance.
		return "✺", render.Pink
	case "max":
		return "✦", render.Orange
	case "xhigh":
		return "●", render.Magenta
	case "high":
		return "◕", render.Cyan
	case "medium":
		return "◑", render.White
	case "low":
		return "◔", render.Dim
	default:
		return "○", render.Dim
	}
}

// ultracodeOn report whether session transcript's last ultracode marker is
// enter. Session scope always: markers land in session file, and project
// files' state say nothing about this session.
func ultracodeOn(c Context) bool {
	if c.In.TranscriptPath == "" {
		return false
	}
	opts := transcript.Options{
		TranscriptPath: c.In.TranscriptPath,
		Scope:          transcript.ScopeSession,
	}
	cache := transcript.LoadCache(c.CacheDir, opts)
	_, cache = transcript.Scan(opts, cache)
	// Save failure cost one rescan next render. Not worth failing over.
	_ = transcript.SaveCache(c.CacheDir, opts, cache)
	return cache.UltracodeOn(c.In.TranscriptPath)
}
