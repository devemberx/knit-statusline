package segment

import (
	"slices"
	"strings"
	"unicode"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("limit.model", Def{
		Fields:          []string{"pct", "bar", "reset", "reset_time", "model"},
		DefaultTemplate: "{model} {bar} {pct:>3}%{reset}",
		Build:           buildModelLimit,
	})
}

// Per-model weekly window never reach stdin -- usagecache.go name where number
// come from. Cache carry window two shapes and this segment read both: release
// moving from one to other otherwise drop row silently, since Def.Stable stay
// false and no placeholder mark loss.
const (
	// kind marking per-model window inside limits[]. Session and account-wide
	// weekly sit in same array under "session" and "weekly_all", scope null on
	// both.
	scopedWeeklyKind = "weekly_scoped"

	// Top-level key holding same window, percentage alone and no scope object.
	// Suffix is family word: seven_day_opus, seven_day_sonnet.
	scopedWeeklyPrefix = "seven_day_"

	// Word naming vendor, not family. Every model id open with it, and display
	// name may too, so matching on it pair any session with any window.
	vendorWord = "claude"
)

// scopedLimit is one limits[] entry /usage draw row for.
//
// Percent is 0-100 already, unlike rate_limits.*.used_percentage, which Claude
// Code scale from 0-1 header before writing payload. Pointer separate null from
// 0: window reported without number prove no room, and printed 0 claim full
// week that may be spent.
type scopedLimit struct {
	Kind     string    `json:"kind"`
	Percent  *float64  `json:"percent"`
	ResetsAt *string   `json:"resets_at"`
	Scope    *limScope `json:"scope"`
}

type limScope struct {
	Model *limScopeModel `json:"model"`
}

// limScopeModel carry display name alone. id sit beside it and read null on
// every entry seen, so name is only handle.
type limScopeModel struct {
	DisplayName string `json:"display_name"`
}

// buildModelLimit render weekly window scoped to one model.
//
// Not Stable: most accounts carry no scoped window at all, and permanent "…"
// there name limit that does not exist. Unreadable, stale and absent all drop
// for same reason -- none is fact this render measured.
func buildModelLimit(c Context) Result {
	if c.ConfigDir == "" {
		return empty
	}
	u, ok := readUsageCache(c.ConfigDir)
	if !ok || !usageFresh(u, c.Now) {
		return empty
	}

	win, ok := pickModelWindow(&u.Utilization, c)
	if !ok {
		return empty
	}

	t := c.Thresholds()
	p := clampPct(win.pct)
	f := render.Fields{
		"model":      render.Colored(win.label, render.White),
		"pct":        render.Colored(pct(p), t.Color(p)),
		"bar":        render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
		"reset":      render.Plain(""),
		"reset_time": render.Plain(""),
	}
	// Icon sit inside field, shape limit.5h and limit.7d use: template writing
	// "⟳ {reset_time}" leave "⟳ " pointing at nothing when reset absent.
	if at, ok := resetTime(win.resetsAt); ok {
		// Weekly window reset days out; bare clock time read ambiguous.
		text := dateTime(at)
		f["reset_time"] = render.Colored(text, render.White)
		f["reset"] = render.Plain(
			c.Palette.Wrap(" ⟳ ", render.Dim) + c.Palette.Wrap(text, render.White))
	}
	return Result{Base: render.White, Fields: f}
}

// modelWindow is one per-model week, flattened out of whichever shape held it.
type modelWindow struct {
	pct      float64
	resetsAt *string
	label    string
}

// pickModelWindow find this session's per-model week, reading both shapes.
//
// limits[] go first: entry there name its model in scope, so row draw name
// usage document itself print. Top-level seven_day_<family> carry percentage
// and reset alone, and label come off name session already answer to.
func pickModelWindow(u *usageUtilization, c Context) (modelWindow, bool) {
	names := modelNames(c)
	if len(names) == 0 {
		return modelWindow{}, false
	}

	var limits []scopedLimit
	if u.decode("limits", &limits) {
		if l, ok := pickScopedLimit(limits, names); ok {
			return modelWindow{*l.Percent, l.ResetsAt, l.Scope.Model.DisplayName}, true
		}
	}

	for _, n := range names {
		var w plainWindow
		if !u.decode(scopedWeeklyPrefix+n, &w) || w.Utilization == nil {
			continue
		}
		return modelWindow{*w.Utilization, w.ResetsAt, modelLabel(c, n)}, true
	}
	return modelWindow{}, false
}

// modelLabel name window for shape carrying no display name.
//
// Pin and payload display name are strings reader already met, and family word
// is what matched, so either one naming that family stand in. Bare family word
// is last resort: lowercase, but name invented here would be worse.
func modelLabel(c Context, family string) string {
	sources := []string{c.Cfg.Model}
	if c.In != nil {
		sources = append(sources, c.In.Model.DisplayName)
	}
	for _, s := range sources {
		if s != "" && familyWord(s) == family {
			return s
		}
	}
	return family
}

// pickScopedLimit find window belonging to one of names.
//
// First match win. Account carry one scoped window per model, so second hit on
// same name mean document shape this build does not know.
func pickScopedLimit(limits []scopedLimit, names []string) (scopedLimit, bool) {
	if len(names) == 0 {
		return scopedLimit{}, false
	}
	for _, l := range limits {
		if l.Kind != scopedWeeklyKind || l.Percent == nil {
			continue
		}
		if l.Scope == nil || l.Scope.Model == nil {
			continue
		}
		family := familyWord(l.Scope.Model.DisplayName)
		if family == "" {
			continue
		}
		for _, n := range names {
			if n == family {
				return l, true
			}
		}
	}
	return scopedLimit{}, false
}

// modelNames list what this segment accept as naming its model.
//
// Pinned name stand alone: window is account-wide, so Fable week keep filling
// while row get read from Opus session, and somebody pinning it want that
// number, not this session's. Unpinned, session model answer to two names --
// display name off payload, id beside it -- and usage document agree with
// whichever one release ship.
func modelNames(c Context) []string {
	// Pin naming no family -- "Claude", "5" -- still bind row away from session
	// model. Falling through print window user never asked for, under name they
	// never typed.
	if c.Cfg.Model != "" {
		pin := familyWord(c.Cfg.Model)
		if pin == "" {
			return nil
		}
		return []string{pin}
	}
	if c.In == nil {
		return nil
	}

	var out []string
	for _, name := range []string{c.In.Model.DisplayName, c.In.Model.ID} {
		if w := familyWord(name); w != "" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// familyWord reduce model name to word every source agree on.
//
// Usage document write "Fable", payload display name may gain release number,
// id carry "claude-fable-5[1m]", and pin get pasted off any of them. Vendor
// word and version part drop; first word left is overlap.
//
// Dropping "claude" is what hold windows apart: every id open with it, so
// keeping it match "Claude Opus 4.5" from Fable session and print week that
// bind another model.
func familyWord(name string) string {
	for _, part := range strings.FieldsFunc(name, nameBreak) {
		part = strings.ToLower(part)
		// Version part lead with digit: "5", "4.5", "5[1m]", "20241022".
		if part == vendorWord || (part[0] >= '0' && part[0] <= '9') {
			continue
		}
		return part
	}
	return ""
}

// nameBreak split model name. Space separate display name words, hyphen
// separate id parts, and pin arrive in either shape.
func nameBreak(r rune) bool {
	return r == '-' || r == '_' || unicode.IsSpace(r)
}
