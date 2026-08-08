package segment

import "github.com/devemberx/knit-statusline/internal/render"

func init() {
	register("limit.extra", Def{
		Fields:          []string{"pct", "bar"},
		DefaultTemplate: "extra {bar} {pct:>3}%",
		Build:           buildExtraUsage,
	})
}

// extraUsageDoc is two keys off utilization.extra_usage.
//
// Block carry monthly_limit, used_credits, currency, decimal_places and
// spend_limit_reached beside them, and utilization.spend restate same money
// third way. None read here: every key is undocumented, money need currency and
// exponent read right to print at all, and percentage already stand on scale
// limit.5h and limit.7d share.
type extraUsageDoc struct {
	IsEnabled   bool     `json:"is_enabled"`
	Utilization *float64 `json:"utilization"`
}

// buildExtraUsage render extra usage window /usage draw its credit row from.
//
// Not Stable, way limit.model is not: most accounts leave extra usage off, and
// permanent "…" there name limit nobody bought. Off, unreadable and stale all
// drop -- none is fact this render measured.
func buildExtraUsage(c Context) Result {
	if c.ConfigDir == "" {
		return empty
	}
	u, ok := readUsageCache(c.ConfigDir)
	if !ok || !usageFresh(u, c.Now) {
		return empty
	}

	var d extraUsageDoc
	if !u.Utilization.decode("extra_usage", &d) {
		return empty
	}
	// Flag decide, not presence of number: account switched off may still carry
	// percentage from while it ran, and that window buy nothing now.
	if !d.IsEnabled || d.Utilization == nil {
		return empty
	}

	t := c.Thresholds()
	p := clampPct(*d.Utilization)
	return Result{Base: render.White, Fields: render.Fields{
		"pct": render.Colored(pct(p), t.Color(p)),
		"bar": render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
	}}
}
