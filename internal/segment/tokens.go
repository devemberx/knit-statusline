package segment

import (
	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func init() {
	register("tokens", Def{
		Fields: []string{"input", "cache_write", "cache_read", "output", "total",
			"input_raw", "output_raw"},
		DefaultTemplate: "↑{input} ↓{output}",
		Build:           buildTokens,
	})
}

// buildTokens report cumulative token usage read from transcript.
//
// stdin carry current context occupancy only -- since v2.1.132
// context_window.total_input_tokens is no running total -- so cumulative figures
// accumulate from transcript.
//
// Four counters stay apart, not interchangeable: cache read run orders of
// magnitude over fresh input and price differently, so merged "input" number
// misrepresent session more than it explain.
//
// Raw variants exist for input and output alone. Those two run small enough that
// "62.1k" hide digits somebody may want; cache figures reach tens of millions,
// where exact count inform nobody.
func buildTokens(c Context) Result {
	if c.In.TranscriptPath == "" {
		return empty
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
		return empty
	}

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"input":       render.Colored(count(totals.Input), render.White),
			"cache_write": render.Colored(count(totals.CacheWrite), render.Dim),
			"cache_read":  render.Colored(count(totals.CacheRead), render.Dim),
			"output":      render.Colored(count(totals.Output), render.White),
			"total":       render.Colored(count(totals.Total()), render.White),

			"input_raw":  render.Colored(itoa(totals.Input), render.White),
			"output_raw": render.Colored(itoa(totals.Output), render.White),
		},
	}
}
