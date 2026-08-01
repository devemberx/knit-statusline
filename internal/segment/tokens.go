package segment

import (
	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func init() {
	register("tokens", Def{
		Fields: []string{"io", "cache", "input", "cache_write", "cache_read",
			"output", "total", "cache_hit", "input_raw", "output_raw"},
		DefaultTemplate: "{io}{cache}",
		Stable:          true,
		Build:           buildTokens,
	})
}

// buildTokens report cumulative token usage read from transcript.
//
// stdin carry current context occupancy only -- since v2.1.132
// context_window.total_input_tokens is no running total.
//
// Four counters stay apart: cache read run orders of magnitude over fresh input
// and price differently, so merged "input" number misrepresent session.
//
// {io} and {cache} group them, following dir's {git}. Four bare numbers under
// one set of ↑↓ read as one list, where ↑ mean "sent" in one half and "written"
// in other.
//
// Raw variants for input and output alone: those run small enough that "62.1k"
// hide digits somebody may want, cache figures reach tens of millions where
// exact count inform nobody.
func buildTokens(c Context) Result {
	if c.In.TranscriptPath == "" {
		return tokensNoUsage(c)
	}

	scope := transcript.ScopeSession
	if c.Cfg.Scope == config.ScopeProject {
		scope = transcript.ScopeProject
	}
	opts := transcript.Options{
		TranscriptPath:   c.In.TranscriptPath,
		Scope:            scope,
		IncludeSidechain: c.Cfg.IncludeSidechain,
	}

	cache := transcript.LoadCache(c.CacheDir, opts)
	totals, cache := transcript.Scan(opts, cache)

	// Cache write failure cost one rescan next render. Not worth failing over.
	_ = transcript.SaveCache(c.CacheDir, opts, cache)

	if totals.Total() == 0 {
		return tokensNoUsage(c)
	}

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"io":    render.Plain(ioGroup(c, totals)),
			"cache": render.Plain(cacheGroup(c, totals)),

			"input":       render.Colored(count(totals.Input), render.White),
			"cache_write": render.Colored(count(totals.CacheWrite), render.Cyan),
			"cache_read":  render.Colored(count(totals.CacheRead), render.Cyan),
			"output":      render.Colored(count(totals.Output), render.White),
			"total":       render.Colored(count(totals.Total()), render.White),
			"cache_hit":   render.Colored(pct(cacheHit(totals)), render.Cyan),

			"input_raw":  render.Colored(itoa(totals.Input), render.White),
			"output_raw": render.Colored(itoa(totals.Output), render.White),
		},
	}
}

// ioGroup render fresh traffic: "↑62.1k ↓231k". Arrow dim, number bright --
// reference layout mute labels and let values carry weight.
func ioGroup(c Context, t transcript.Totals) string {
	return c.Palette.Wrap("↑", render.Dim) +
		c.Palette.Wrap(count(t.Input), render.White) +
		" " +
		c.Palette.Wrap("↓", render.Dim) +
		c.Palette.Wrap(count(t.Output), render.White)
}

// cacheGroup render cache traffic: "  cache ↑1.2M ↓32.4M".
//
// Cyan against io's white, so eye split two groups before reading labels.
// Separator sit inside field, same shape as dir's {git}: session touching no
// cache drop whole group, gap included.
func cacheGroup(c Context, t transcript.Totals) string {
	if t.CacheWrite == 0 && t.CacheRead == 0 {
		return ""
	}
	return "  " +
		c.Palette.Wrap("cache ", render.Dim) +
		c.Palette.Wrap("↑", render.Dim) +
		c.Palette.Wrap(count(t.CacheWrite), render.Cyan) +
		" " +
		c.Palette.Wrap("↓", render.Dim) +
		c.Palette.Wrap(count(t.CacheRead), render.Cyan)
}

// tokensNoUsage cover states with nothing counted.
//
// Fresh session sent nothing, so every counter is a real zero. Otherwise
// transcript was unreadable or holds only synthetic entries, and no number is
// known. scope = "project" reach here on neither: other transcripts in
// project carry usage, so totals come back known even at first render.
func tokensNoUsage(c Context) Result {
	if !c.holdsSlot() {
		return empty
	}
	text, col := c.Cfg.Unknown, render.Dim
	if c.Fresh {
		text, col = "0", render.White
	}

	// Cache group hidden, same shape as session that touched no cache: gap
	// live inside field, so it go with it.
	io := c.Palette.Wrap("↑", render.Dim) + c.Palette.Wrap(text, col) +
		" " + c.Palette.Wrap("↓", render.Dim) + c.Palette.Wrap(text, col)

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"io":    render.Plain(io),
			"cache": render.Plain(""),

			"input":       render.Colored(text, col),
			"cache_write": render.Colored(text, col),
			"cache_read":  render.Colored(text, col),
			"output":      render.Colored(text, col),
			"total":       render.Colored(text, col),
			"cache_hit":   render.Colored(text, col),

			"input_raw":  render.Colored(text, col),
			"output_raw": render.Colored(text, col),
		},
	}
}

// cacheHit report share of input tokens served from cache. Raw cache_read reach
// tens of millions and land as noise; percentage answer what it was read for.
func cacheHit(t transcript.Totals) float64 {
	in := t.Input + t.CacheWrite + t.CacheRead
	if in <= 0 {
		return 0
	}
	return float64(t.CacheRead) * 100 / float64(in)
}
