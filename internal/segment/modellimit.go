package segment

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"
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

// Per-model weekly window never reach stdin. Claude Code build rate_limits out
// of anthropic-ratelimit-unified-{5h,7d}-utilization response headers, and no
// header name a model, so payload carry account-wide windows alone. Only copy
// on this machine is cachedUsageUtilization in .claude.json, written from
// GET /api/oauth/usage -- numbers /usage draw its "Current week (Fable)" row
// from.
const (
	// Claude Code refresh cache every 5 minutes while session send, and discard
	// it past one hour. Older percentage name window that may have reset since.
	usageCacheTTL = time.Hour

	// kind marking per-model window. Session and account-wide weekly sit in
	// same array under "session" and "weekly_all", scope null on both.
	scopedWeeklyKind = "weekly_scoped"

	// Word naming vendor, not family. Every model id open with it, and display
	// name may too, so matching on it pair any session with any window.
	vendorWord = "claude"
)

// usageCacheDoc is subset of .claude.json this segment read. configcount.go
// open same file for MCP servers and decode own shape: two share no key, and
// one type mismatch anywhere fail whole decode.
type usageCacheDoc struct {
	Usage *cachedUsage `json:"cachedUsageUtilization"`
}

type cachedUsage struct {
	FetchedAtMS int64            `json:"fetchedAtMs"`
	Utilization usageUtilization `json:"utilization"`
}

type usageUtilization struct {
	Limits []scopedLimit `json:"limits"`
}

// scopedLimit is one window /usage draw row for.
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

	lim, ok := pickScopedLimit(u.Utilization.Limits, modelNames(c))
	if !ok {
		return empty
	}

	t := c.Thresholds()
	p := clampPct(*lim.Percent)
	f := render.Fields{
		"model":      render.Colored(lim.Scope.Model.DisplayName, render.White),
		"pct":        render.Colored(pct(p), t.Color(p)),
		"bar":        render.Plain(c.Palette.Bar(p, c.Cfg.BarWidth, t)),
		"reset":      render.Plain(""),
		"reset_time": render.Plain(""),
	}
	// Icon sit inside field, shape limit.5h and limit.7d use: template writing
	// "⟳ {reset_time}" leave "⟳ " pointing at nothing when reset absent.
	if at, ok := resetTime(lim.ResetsAt); ok {
		// Weekly window reset days out; bare clock time read ambiguous.
		text := dateTime(at)
		f["reset_time"] = render.Colored(text, render.White)
		f["reset"] = render.Plain(
			c.Palette.Wrap(" ⟳ ", render.Dim) + c.Palette.Wrap(text, render.White))
	}
	return Result{Base: render.White, Fields: f}
}

// readUsageCache decode cachedUsageUtilization off .claude.json.
//
// CLAUDE_CONFIG_DIR move file inside config root; default install leave it
// beside, at ~/.claude.json. Same probe configcount.go run.
//
// Search walk past file that parse but carry no usage block: both files exist
// only when somebody moved root, and either one may be copy Claude Code write
// its fetch to. Leftover from before move answer stale and drop on TTL, so
// walking cost no wrong number.
func readUsageCache(root string) (*cachedUsage, bool) {
	// Clean before Dir: CLAUDE_CONFIG_DIR written with trailing slash leave
	// Dir() pointing at root itself, and both probe collapse onto one file.
	for _, p := range []string{
		filepath.Join(root, ".claude.json"),
		filepath.Join(filepath.Dir(filepath.Clean(root)), ".claude.json"),
	} {
		var d usageCacheDoc
		switch err := readConfigJSON(p, maxClaudeJSONBytes, &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, false
		}
		if d.Usage != nil {
			return d.Usage, true
		}
	}
	return nil, false
}

// usageFresh reject cache too old to speak for current window, matching what
// Claude Code itself refuse to read.
//
// Future stamp rejected too: clock moved, or file came off another machine, and
// neither leave age this can reason about.
func usageFresh(u *cachedUsage, now time.Time) bool {
	if u.FetchedAtMS <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(u.FetchedAtMS))
	return age >= 0 && age <= usageCacheTTL
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

// resetTime parse RFC 3339 stamp usage document write. Unparseable value drop
// reset alone: percentage is number row exist for.
//
// Local() is what put this clock on same face as limit.5h and limit.7d: their
// stamp arrive as Unix seconds and time.Unix hand back Local, while time.Parse
// keep "+00:00" written in document. Seoul reader otherwise get "jul 28,
// 8:59pm" beside "jul 29, 5:59am" for one instant, neither marked UTC.
func resetTime(s *string) (time.Time, bool) {
	if s == nil || *s == "" {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return time.Time{}, false
	}
	return at.Local(), true
}

// clampPct hold percentage in 0-100. Document report 100 on spent window and
// nothing forbid reporting past that; bar drawn from larger number run off own
// width.
func clampPct(p float64) float64 {
	switch {
	case p < 0:
		return 0
	case p > 100:
		return 100
	default:
		return p
	}
}
