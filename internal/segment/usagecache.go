package segment

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// Usage block is only copy on this machine of numbers /usage draw its rows
// from. Claude Code fetch GET /api/oauth/usage and cache result under
// cachedUsageUtilization in .claude.json. Payload carry account-wide five_hour
// and seven_day alone, built from anthropic-ratelimit-unified-{5h,7d}-utilization
// response headers, and no header name model or extra usage.
//
// Two segments read block, so reader take own file rather than sit inside
// either one.

// Claude Code refresh cache every 5 minutes while session send, and discard it
// past one hour. Older percentage name window that may have reset since.
const usageCacheTTL = time.Hour

type usageCacheDoc struct {
	Usage *cachedUsage `json:"cachedUsageUtilization"`
}

type cachedUsage struct {
	FetchedAtMS int64            `json:"fetchedAtMs"`
	Utilization usageUtilization `json:"utilization"`
}

// usageUtilization hold block undecoded, keyed by name.
//
// Keys are open set -- five_hour, seven_day, seven_day_opus, seven_day_sonnet,
// limits, extra_usage, spend, plus whatever ship next. Struct field per key
// turn every release into build change, and one type mismatch anywhere fail
// whole decode, dropping keys that parse fine beside it.
type usageUtilization struct {
	raw map[string]json.RawMessage
}

func (u *usageUtilization) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &u.raw)
}

// decode read one entry into v, false when key absent or shape disagree.
//
// Both answer same: neither prove what window hold, and caller resolve either
// to dropped row.
func (u *usageUtilization) decode(key string, v any) bool {
	raw, ok := u.raw[key]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// plainWindow is shape five_hour, seven_day and seven_day_<family> share.
//
// Pointer separate null from 0: window reported without number prove no room,
// and printed 0 claim full week that may be spent. Key present holding null is
// normal -- account carrying no such window still list name.
type plainWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// readUsageCache decode cachedUsageUtilization off .claude.json.
//
// CLAUDE_CONFIG_DIR move file inside config root; default install leave it
// beside, at ~/.claude.json.
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
