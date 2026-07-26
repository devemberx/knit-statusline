package segment

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Compact wall-clock: "1h15m", "15m", "30s". No room for "1 hour 15 minutes".
func duration(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// "3:00pm" -- reference layout style, lowercase and space-free.
func clockTime(t time.Time) string {
	return strings.ToLower(t.Format("3:04pm"))
}

// "jul 28, 3:00pm", for reset days away.
func dateTime(t time.Time) string {
	return strings.ToLower(t.Format("Jan 2, 3:04pm"))
}

// Abbreviate. Cumulative figures reach hundreds of millions, and exact digit
// count is never what reader want.
func count(n int64) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 1_000_000:
		return trimZero(float64(n)/1000) + "k"
	case n < 1_000_000_000:
		return trimZero(float64(n)/1_000_000) + "M"
	default:
		return trimZero(float64(n)/1_000_000_000) + "B"
	}
}

// One decimal, dropped when zero: "62.1k" and "5k" both read naturally.
func trimZero(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// No decimals. Sub-percent precision is noise at this size.
//
// math.Round, matching render.Bar cell count. Two roundings that disagree put
// "10%" beside empty bar.
func pct(f float64) string {
	if math.IsNaN(f) {
		return "0"
	}
	return strconv.Itoa(int(math.Round(f)))
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// modelVersion pull release number out of model id: claude-opus-4-8 give "4.8",
// claude-sonnet-5 give "5", claude-3-5-sonnet-20241022 give "3.5".
//
// Numeric runs joined with dots, in id order, so family placement never matter.
// Eight digits is date stamp -- every dated id end in one -- and dropped.
func modelVersion(id string) string {
	var parts []string
	for _, p := range strings.Split(id, "-") {
		if !digits(p) || len(p) == 8 {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ".")
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
