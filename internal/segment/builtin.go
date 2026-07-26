package segment

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("model", Def{
		Fields:          []string{"name", "family", "version", "id"},
		DefaultTemplate: "{name}",
		Build: func(c Context) Result {
			if c.In.Model.DisplayName == "" && c.In.Model.ID == "" {
				return empty
			}
			// display_name carry family alone -- "Opus" read same across every
			// Opus release, so row name a model it cannot pin. name join version
			// in here, where absent half is one branch; template joining them
			// leave stray space instead.
			family, version := c.In.Model.DisplayName, modelVersion(c.In.Model.ID)
			return Result{
				Base: render.Blue,
				Fields: render.Fields{
					"name":    render.Colored(strings.TrimSpace(family+" "+version), render.Blue),
					"family":  render.Colored(family, render.Blue),
					"version": render.Colored(version, render.Blue),
					"id":      render.Colored(c.In.Model.ID, render.Blue),
				},
			}
		},
	})

	register("context", Def{
		Fields:          []string{"pct", "remaining", "used", "size", "bar"},
		DefaultTemplate: "✍️ {pct}%",
		Build:           buildContext,
	})

	register("session", Def{
		Fields:          []string{"duration", "id", "name"},
		DefaultTemplate: "⏱ {duration}",
		Build: func(c Context) Result {
			if c.In.Cost == nil || c.In.Cost.TotalDurationMS == nil {
				return empty
			}
			d := time.Duration(*c.In.Cost.TotalDurationMS) * time.Millisecond
			f := render.Fields{
				"duration": render.Colored(duration(d), render.White),
				"id":       render.Plain(c.In.SessionID),
			}
			if c.In.SessionName != nil {
				f["name"] = render.Colored(*c.In.SessionName, render.White)
			}
			return Result{Base: render.Dim, Fields: f}
		},
	})

	register("effort", Def{
		Fields:          []string{"level", "icon"},
		DefaultTemplate: "{icon} {level}",
		Build: func(c Context) Result {
			// Absent on models without effort parameter, where showing default
			// would claim setting this session does not have.
			if c.In.Effort == nil || c.In.Effort.Level == "" {
				return empty
			}
			icon, color := effortStyle(c.In.Effort.Level)
			return Result{
				Base: color,
				Fields: render.Fields{
					"icon":  render.Colored(icon, color),
					"level": render.Colored(c.In.Effort.Level, color),
				},
			}
		},
	})

	register("limit.5h", Def{
		Fields:          []string{"pct", "bar", "reset"},
		DefaultTemplate: "current {bar} {pct:>3}% ⟳ {reset}",
		Build:           limitBuilder(fiveHour),
	})

	register("limit.7d", Def{
		Fields:          []string{"pct", "bar", "reset"},
		DefaultTemplate: "weekly {bar} {pct:>3}% ⟳ {reset}",
		Build:           limitBuilder(sevenDay),
	})

	register("cost", Def{
		Fields:          []string{"usd", "api_duration"},
		DefaultTemplate: "${usd}",
		Build: func(c Context) Result {
			if c.In.Cost == nil || c.In.Cost.TotalCostUSD == nil {
				return empty
			}
			f := render.Fields{
				"usd": render.Colored(fmt.Sprintf("%.2f", *c.In.Cost.TotalCostUSD), render.White),
			}
			if c.In.Cost.TotalAPIDurationMS != nil {
				d := time.Duration(*c.In.Cost.TotalAPIDurationMS) * time.Millisecond
				f["api_duration"] = render.Colored(duration(d), render.White)
			}
			return Result{Base: render.Dim, Fields: f}
		},
	})

	register("lines", Def{
		Fields:          []string{"added", "removed"},
		DefaultTemplate: "+{added} -{removed}",
		Build: func(c Context) Result {
			if c.In.Cost == nil || c.In.Cost.TotalLinesAdded == nil || c.In.Cost.TotalLinesRemoved == nil {
				return empty
			}
			return Result{
				Base: render.Dim,
				Fields: render.Fields{
					"added":   render.Colored(strconv.FormatInt(*c.In.Cost.TotalLinesAdded, 10), render.Green),
					"removed": render.Colored(strconv.FormatInt(*c.In.Cost.TotalLinesRemoved, 10), render.Red),
				},
			}
		},
	})

	register("version", Def{
		Fields:          []string{"version"},
		DefaultTemplate: "{version}",
		Build: func(c Context) Result {
			if c.In.Version == "" {
				return empty
			}
			return Result{Base: render.Dim, Fields: render.Fields{
				"version": render.Colored(c.In.Version, render.Dim),
			}}
		},
	})

	register("vim", Def{
		Fields:          []string{"mode"},
		DefaultTemplate: "{mode}",
		Build: func(c Context) Result {
			if c.In.Vim == nil || c.In.Vim.Mode == "" {
				return empty
			}
			return Result{Base: render.Magenta, Fields: render.Fields{
				"mode": render.Colored(c.In.Vim.Mode, render.Magenta),
			}}
		},
	})

	register("output_style", Def{
		Fields:          []string{"name"},
		DefaultTemplate: "{name}",
		Build: func(c Context) Result {
			if c.In.OutputStyle == nil || c.In.OutputStyle.Name == "" {
				return empty
			}
			return Result{Base: render.Dim, Fields: render.Fields{
				"name": render.Colored(c.In.OutputStyle.Name, render.Dim),
			}}
		},
	})

	register("repo", Def{
		Fields:          []string{"host", "owner", "name", "slug"},
		DefaultTemplate: "{slug}",
		Build: func(c Context) Result {
			r := c.In.Workspace.Repo
			if r == nil {
				return empty
			}
			return Result{Base: render.Dim, Fields: render.Fields{
				"host":  render.Colored(r.Host, render.Dim),
				"owner": render.Colored(r.Owner, render.Dim),
				"name":  render.Colored(r.Name, render.Dim),
				"slug":  render.Colored(r.Owner+"/"+r.Name, render.Dim),
			}}
		},
	})

	register("pr", Def{
		Fields:          []string{"number", "state", "url"},
		DefaultTemplate: "#{number}",
		Build: func(c Context) Result {
			if c.In.PR == nil || c.In.PR.Number == nil {
				return empty
			}
			f := render.Fields{
				"number": render.Colored(strconv.FormatInt(*c.In.PR.Number, 10), render.Cyan),
			}
			if c.In.PR.ReviewState != nil {
				f["state"] = render.Colored(*c.In.PR.ReviewState, render.Dim)
			}
			if c.In.PR.URL != nil {
				f["url"] = render.Plain(*c.In.PR.URL)
			}
			return Result{Base: render.Dim, Fields: f}
		},
	})

	register("fast_mode", Def{
		Fields:          []string{"state"},
		DefaultTemplate: "{state}",
		Build: func(c Context) Result {
			// Only worth slot when it is on; "off" marker is noise.
			if c.In.FastMode == nil || !*c.In.FastMode {
				return empty
			}
			return Result{Base: render.Yellow, Fields: render.Fields{
				"state": render.Colored("⚡fast", render.Yellow),
			}}
		},
	})

	register("thinking", Def{
		Fields:          []string{"state"},
		DefaultTemplate: "{state}",
		Build: func(c Context) Result {
			if c.In.Thinking == nil || !c.In.Thinking.Enabled {
				return empty
			}
			return Result{Base: render.Magenta, Fields: render.Fields{
				"state": render.Colored("think", render.Magenta),
			}}
		},
	})
}

func buildContext(c Context) Result {
	// Unknown is not zero. Before first API call and after /compact, Claude Code
	// report no usage at all. 0% there claim empty context where truth is
	// unreported one.
	p, ok := c.In.ContextPercent()
	if !ok {
		return empty
	}

	t := c.Thresholds()
	f := render.Fields{
		"pct":       render.Colored(pct(p), t.Color(p)),
		"remaining": render.Colored(pct(100-p), t.Color(p)),
		"bar":       render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
	}
	if cw := c.In.Context; cw != nil && cw.ContextWindowSize != nil {
		f["size"] = render.Colored(count(*cw.ContextWindowSize), render.Dim)
		f["used"] = render.Colored(count(int64(p/100*float64(*cw.ContextWindowSize))), render.White)
	}
	return Result{Base: render.Dim, Fields: f}
}

func effortStyle(level string) (string, render.Color) {
	switch level {
	case "max", "xhigh", "high":
		return "●", render.Magenta
	case "low":
		return "◔", render.Dim
	default:
		return "◑", render.Dim
	}
}

type window int

const (
	fiveHour window = iota
	sevenDay
)

// limitBuilder render one rate limit window.
//
// rate_limits reach Claude.ai subscribers only, after first API response only,
// and each window go absent on its own. Check every level before use.
func limitBuilder(w window) func(Context) Result {
	return func(c Context) Result {
		if c.In.RateLimits == nil {
			return empty
		}
		win := c.In.RateLimits.FiveHour
		format := clockTime
		if w == sevenDay {
			win = c.In.RateLimits.SevenDay
			// Weekly window reset days out; bare clock time read ambiguous.
			format = dateTime
		}
		if win == nil {
			return empty
		}

		t := c.Thresholds()
		p := win.UsedPercentage
		f := render.Fields{
			"pct": render.Colored(pct(p), t.Color(p)),
			"bar": render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
		}
		if win.ResetsAt != nil {
			f["reset"] = render.Colored(format(time.Unix(*win.ResetsAt, 0)), render.White)
		} else {
			f["reset"] = render.Plain("")
		}
		return Result{Base: render.White, Fields: f}
	}
}
