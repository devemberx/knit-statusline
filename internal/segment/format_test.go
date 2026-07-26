package segment

import (
	"math"
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, ""}, // clock skew, not a negative session
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{75 * time.Minute, "1h15m"},
		{25 * time.Hour, "25h0m"}, // hours never roll into days
		{0, "0s"},
	} {
		if got := duration(tc.d); got != tc.want {
			t.Errorf("duration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// Reset times print in viewer's local zone, so assertions build their instant
// there too rather than assuming UTC.
func TestClockAndDateFormats(t *testing.T) {
	at := time.Date(2026, time.July, 28, 15, 0, 0, 0, time.Local)

	if got := clockTime(at); got != "3:00pm" {
		t.Errorf("clockTime = %q, want %q", got, "3:00pm")
	}
	// Weekly window reset days out, so bare clock time read ambiguous.
	if got := dateTime(at); got != "jul 28, 3:00pm" {
		t.Errorf("dateTime = %q, want %q", got, "jul 28, 3:00pm")
	}
}

func TestCountAbbreviates(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{-1, "0"}, // nothing counted is not a negative count
		{0, "0"},
		{999, "999"},
		{1000, "1k"},      // trailing ".0" dropped
		{62_100, "62.1k"}, // one decimal kept when it carry information
		{1_000_000, "1M"},
		{32_400_000, "32.4M"},
		{1_500_000_000, "1.5B"},
	} {
		if got := count(tc.n); got != tc.want {
			t.Errorf("count(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// pct round rather than truncate, matching render.Bar cell count. Disagreeing
// roundings put "10%" beside empty bar.
func TestPctRoundsHalfUp(t *testing.T) {
	for _, tc := range []struct {
		f    float64
		want string
	}{
		{0, "0"},
		{4.4, "4"},
		{4.5, "5"},
		{9.6, "10"},
		{42, "42"},
		{99.5, "100"},
		{math.NaN(), "0"}, // absent reads as zero, never as "NaN%"
	} {
		if got := pct(tc.f); got != tc.want {
			t.Errorf("pct(%v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

func TestItoaKeepsEveryDigit(t *testing.T) {
	if got := itoa(62_093); got != "62093" {
		t.Errorf("itoa = %q, want %q", got, "62093")
	}
}
