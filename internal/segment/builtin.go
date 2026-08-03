package segment

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/render"
)

// context and session icon sit in field, not template literal: literal take
// Result.Base, Base is Dim for both, and SGR 2 over emoji glyph fade it past
// reading.
const (
	contextIcon = "✍️"
	sessionIcon = "⏱"

	// Fill state carry meaning without color, so NO_COLOR terminal still
	// separate thinking on from off.
	thinkingOn  = "●"
	thinkingOff = "○"
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
			// Opus release, so row name a model it cannot pin.
			family, version := c.In.Model.DisplayName, modelVersion(c.In.Model.ID)
			return Result{
				Base: render.Blue,
				Fields: render.Fields{
					"name":    render.Colored(joinModelName(family, version), render.Blue),
					"family":  render.Colored(family, render.Blue),
					"version": render.Colored(version, render.Blue),
					"id":      render.Colored(c.In.Model.ID, render.Blue),
				},
			}
		},
	})

	register("context", Def{
		Fields:          []string{"pct", "remaining", "used", "size", "bar", "icon"},
		DefaultTemplate: "{icon} {pct}%",
		Stable:          true,
		Build:           buildContext,
	})

	register("session", Def{
		Fields:          []string{"duration", "id", "name", "icon"},
		DefaultTemplate: "{icon} {duration}",
		Stable:          true,
		Build: func(c Context) Result {
			text, ok := sessionDuration(c)
			if !ok {
				return empty
			}
			f := render.Fields{
				"icon":     render.Colored(sessionIcon, render.White),
				"duration": render.Colored(text, render.White),
				"id":       render.Colored(c.In.SessionID, render.Dim),
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
		Fields:          []string{"pct", "bar", "reset", "reset_time"},
		DefaultTemplate: "current {bar} {pct:>3}%{reset}",
		Stable:          true,
		Build:           limitBuilder(fiveHour),
	})

	register("limit.7d", Def{
		Fields:          []string{"pct", "bar", "reset", "reset_time"},
		DefaultTemplate: "weekly {bar} {pct:>3}%{reset}",
		Stable:          true,
		Build:           limitBuilder(sevenDay),
	})

	register("cost", Def{
		Fields:          []string{"usd", "api_duration"},
		DefaultTemplate: "${usd}",
		Stable:          true,
		Build: func(c Context) Result {
			usd, ok := costUSD(c)
			if !ok {
				return empty
			}
			f := render.Fields{"usd": render.Colored(usd, render.White)}
			if api, ok := apiDuration(c); ok {
				f["api_duration"] = render.Colored(api, render.White)
			}
			return Result{Base: render.Dim, Fields: f}
		},
	})

	register("lines", Def{
		Fields:          []string{"added", "removed"},
		DefaultTemplate: "+{added} -{removed}",
		Stable:          true,
		Build: func(c Context) Result {
			var added, removed *int64
			if c.In.Cost != nil {
				added, removed = c.In.Cost.TotalLinesAdded, c.In.Cost.TotalLinesRemoved
			}
			// Opted-out slot drop on any absent counter, way every non-stable
			// segment do. Default template "+{added} -{removed}" leave bare
			// "-" standing otherwise.
			if !c.holdsSlot() && (added == nil || removed == nil) {
				return empty
			}
			return Result{Base: render.Dim, Fields: render.Fields{
				"added":   render.Colored(lineCount(c, added), render.Green),
				"removed": render.Colored(lineCount(c, removed), render.Red),
			}}
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
				f["url"] = render.Colored(*c.In.PR.URL, render.Dim)
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
		Fields:          []string{"state", "icon"},
		DefaultTemplate: "{icon} {state}",
		Build: func(c Context) Result {
			// Absent only on payload predating field. Claude Code send
			// thinking unconditionally, off state included, so nil mean
			// missing knowledge -- not thinking turned off.
			if c.In.Thinking == nil {
				return empty
			}
			icon, color := thinkingOff, render.Dim
			if c.In.Thinking.Enabled {
				icon, color = thinkingOn, render.Pink
			}
			return Result{Base: color, Fields: render.Fields{
				"icon":  render.Colored(icon, color),
				"state": render.Colored("thinking", color),
			}}
		},
	})
}

// joinModelName put version beside family, absent half leaving no stray space.
//
// "display_name carry family alone" is upstream behaviour, not guarantee. Send
// "Haiku 4.5" with id claude-haiku-4-5 and plain join print "Haiku 4.5 4.5".
func joinModelName(family, version string) string {
	if version == "" {
		return family
	}
	if strings.HasSuffix(family, " "+version) || family == version {
		return family
	}
	return strings.TrimSpace(family + " " + version)
}

func buildContext(c Context) Result {
	// Unknown is not zero. Before first API call and after /compact, Claude Code
	// report no usage at all.
	p, ok := c.In.ContextPercent()
	if !ok {
		return contextNoUsage(c)
	}

	t := c.Thresholds()
	f := render.Fields{
		"icon":      render.Colored(contextIcon, render.White),
		"pct":       render.Colored(pct(p), t.Color(p)),
		"remaining": render.Colored(pct(100-p), t.Color(p)),
		"bar":       render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
	}
	// Known percentage prove nothing about window size: used_percentage arrive
	// without context_window_size, and omitting both field render
	// "{used}/{size}" as bare "/" -- shape collapse slot exist to stop.
	if cw := c.In.Context; cw != nil && cw.ContextWindowSize != nil {
		f["size"] = render.Colored(count(*cw.ContextWindowSize), render.Dim)
	} else if c.holdsSlot() {
		f["size"] = render.Colored(c.Cfg.Unknown, render.Dim)
	}
	if used, ok := usedTokens(c); ok {
		f["used"] = render.Colored(count(used), render.White)
	} else if c.holdsSlot() {
		f["used"] = render.Colored(c.Cfg.Unknown, render.Dim)
	}
	return Result{Base: render.Dim, Fields: f}
}

// contextNoUsage cover two states carrying no percentage.
//
// Fresh session sent nothing, so 0% is fact. Everything else -- resume,
// /compact, transcript unreadable -- hold occupancy nobody reported, and that
// is what placeholder say.
//
// {size} split off from {used}: nothing was sent, so used is a real zero, while
// window size was never reported at all and zero would be absurd.
//
// {size} override sit outside both branches: context_window_size is static
// model configuration, not usage, so payload win over freshness either way.
func contextNoUsage(c Context) Result {
	if !c.holdsSlot() {
		return empty
	}

	u := c.Cfg.Unknown
	var f render.Fields
	if !c.Fresh {
		f = render.Fields{
			"pct":       render.Colored(u, render.Dim),
			"remaining": render.Colored(u, render.Dim),
			"used":      render.Colored(u, render.Dim),
			"size":      render.Colored(u, render.Dim),
			"bar":       render.Plain(c.Palette.Bar(0, c.Cfg.BarWidth, render.Thresholds{})),
		}
	} else {
		t := c.Thresholds()
		f = render.Fields{
			"pct":       render.Colored(pct(0), t.Color(0)),
			"remaining": render.Colored(pct(100), t.Color(0)),
			"used":      render.Colored(count(0), render.White),
			"size":      render.Colored(u, render.Dim),
			"bar":       render.Plain(c.Palette.Bar(0, c.Cfg.BarWidth, t)),
		}
	}
	f["icon"] = render.Colored(contextIcon, render.White)
	if cw := c.In.Context; cw != nil && cw.ContextWindowSize != nil {
		f["size"] = render.Colored(count(*cw.ContextWindowSize), render.Dim)
	}
	return Result{Base: render.Dim, Fields: f}
}

// usedTokens report occupied tokens, second return false = no source at all.
//
// current_usage hold counted tokens, so prefer it -- back-computing from
// percentage rounded to whole points miss by up to 1k on 200k window. Same
// three counters ContextPercent sum, output left out alongside.
func usedTokens(c Context) (int64, bool) {
	cw := c.In.Context
	if cw == nil {
		return 0, false
	}
	if u := cw.CurrentUsage; u != nil {
		return u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens, true
	}
	if cw.ContextWindowSize == nil {
		return 0, false
	}
	p, ok := c.In.ContextPercent()
	if !ok {
		return 0, false
	}
	return int64(p / 100 * float64(*cw.ContextWindowSize)), true
}

// sessionDuration pick duration text. Second return false = segment drop.
//
// No fresh-zero here, unlike cost and lines: transcript prove no tokens sent,
// not no time elapsed. total_duration_ms is wall clock, and session may idle
// minutes before first call -- "0s" there is claim probe cannot back.
func sessionDuration(c Context) (string, bool) {
	if c.In.Cost != nil && c.In.Cost.TotalDurationMS != nil {
		return duration(time.Duration(*c.In.Cost.TotalDurationMS) * time.Millisecond), true
	}
	if !c.holdsSlot() {
		return "", false
	}
	return c.Cfg.Unknown, true
}

// costUSD pick {usd} text for three states. Second return false = segment
// drop -- default template "${usd}" leave bare "$" standing otherwise.
func costUSD(c Context) (string, bool) {
	if c.In.Cost != nil && c.In.Cost.TotalCostUSD != nil {
		return fmt.Sprintf("%.2f", *c.In.Cost.TotalCostUSD), true
	}
	if !c.holdsSlot() {
		return "", false
	}
	if c.Fresh {
		return fmt.Sprintf("%.2f", 0.0), true
	}
	return c.Cfg.Unknown, true
}

// lineCount pick one counter's text, resolving off its own pointer.
//
// Requiring both send known 156 down placeholder path when its partner is nil.
// Whether Claude Code ever send one counter alone is unverified, and guessing
// it never happen cost real number when it does.
func lineCount(c Context, n *int64) string {
	if n != nil {
		return strconv.FormatInt(*n, 10)
	}
	if c.Fresh {
		return "0"
	}
	return c.Cfg.Unknown
}

// apiDuration resolve independent of {usd} -- payload win over freshness or
// placeholder same as context_window_size override in contextNoUsage.
// Second return false = field left out of Fields entirely, not blanked.
func apiDuration(c Context) (string, bool) {
	if c.In.Cost != nil && c.In.Cost.TotalAPIDurationMS != nil {
		d := time.Duration(*c.In.Cost.TotalAPIDurationMS) * time.Millisecond
		return duration(d), true
	}
	if !c.holdsSlot() {
		return "", false
	}
	if c.Fresh {
		return duration(0), true
	}
	return c.Cfg.Unknown, true
}

// Fill ramp read without color, so NO_COLOR terminal still separate max from
// high. Unknown level take empty circle, never medium's glyph.
//
// max hold Magenta, not Orange: Orange is warn threshold, and context bar sit on
// same row as effort in every preset, so orange ✦ read as rate-limit warning.
// Magenta's other users -- vim mode, git worktree marker -- stay off most rows.
// high keep Cyan though dir own it too: dir draw text, effort draw glyph.
func effortStyle(level string) (string, render.Color) {
	switch level {
	case "max":
		return "✦", render.Magenta
	case "xhigh":
		return "●", render.Aqua
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
			return limitNoWindow(c)
		}
		win := c.In.RateLimits.FiveHour
		format := clockTime
		if w == sevenDay {
			win = c.In.RateLimits.SevenDay
			// Weekly window reset days out; bare clock time read ambiguous.
			format = dateTime
		}
		// Null percentage carry no more fact than absent window, so both take
		// same branch. reset_time alone would leave "{bar} %" round nothing.
		if win == nil || win.UsedPercentage == nil {
			return limitNoWindow(c)
		}

		t := c.Thresholds()
		p := *win.UsedPercentage
		f := render.Fields{
			"pct":        render.Colored(pct(p), t.Color(p)),
			"bar":        render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
			"reset":      render.Plain(""),
			"reset_time": render.Plain(""),
		}
		// Icon sit inside field, same shape as dir's {git}. Template writing
		// "⟳ {reset_time}" leave "⟳ " pointing at nothing when reset absent.
		if win.ResetsAt != nil {
			at := format(time.Unix(*win.ResetsAt, 0))
			f["reset_time"] = render.Colored(at, render.White)
			f["reset"] = render.Plain(
				c.Palette.Wrap(" ⟳ ", render.Dim) + c.Palette.Wrap(at, render.White))
		}
		return Result{Base: render.White, Fields: f}
	}
}

// limitNoWindow render a window nobody reported.
//
// No fresh-zero state here, unlike every other stable segment: window is
// account-wide and carry across sessions, so a new session may open at 80%
// used. Zero would read as room that does not exist.
//
// Empty Thresholds leave bar uncolored: severity of unknown percentage
// is unknown too, and green bar claim otherwise.
func limitNoWindow(c Context) Result {
	if !c.holdsSlot() {
		return empty
	}
	return Result{Base: render.White, Fields: render.Fields{
		"pct":        render.Colored(c.Cfg.Unknown, render.Dim),
		"bar":        render.Plain(c.Palette.Bar(0, c.Cfg.BarWidth, render.Thresholds{})),
		"reset":      render.Plain(""),
		"reset_time": render.Plain(""),
	}}
}
