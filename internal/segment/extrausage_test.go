package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// writeExtraUsage plant .claude.json carrying utilization.extra_usage alone.
// Keys beside block left out: decode read two of them and must not need rest.
func writeExtraUsage(t *testing.T, dir string, fetchedAt time.Time, block string) {
	t.Helper()
	doc := fmt.Sprintf(`{
  "installMethod": "native",
  "cachedUsageUtilization": {
    "fetchedAtMs": %d,
    "utilization": {
      "five_hour": {"utilization": 87, "resets_at": "2025-07-23T12:50:00.809792+00:00"},
      "extra_usage": %s,
      "limits": []
    }
  }
}`, fetchedAt.UnixMilli(), block)
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
}

func extraCtx(t *testing.T, root string, age time.Duration) Context {
	t.Helper()
	c := ctx(t, fixtures.Full, "limit.extra")
	c.ConfigDir = root
	c.Now = usageFetchedAt.Add(age)
	return c
}

// Extra usage is last /usage row carrying number no payload field hold, so
// segment exist for same reason limit.model does.
func TestExtraUsageRendersEnabledWindow(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt,
		`{"is_enabled": true, "monthly_limit": null, "used_credits": null, "utilization": 18,
		  "currency": null, "decimal_places": null, "user_disabled": false}`)

	res := Build(extraCtx(t, root, time.Minute))
	if res.Empty {
		t.Fatal("enabled extra usage dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "18" {
		t.Errorf("pct = %q, want %q", got, "18")
	}
}

// Default template stand in same column shape as limit.5h and limit.7d, so
// three rows stack without per-segment template.
func TestExtraUsageDefaultTemplateNamesRow(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": true, "utilization": 18}`)

	if got, want := draw(extraCtx(t, root, time.Minute)), "extra ●●○○○○○○○○  18%"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Every observed account read is_enabled false, so this is shape segment meet
// most. Off is absent, never "…" -- placeholder name limit nobody bought.
func TestExtraUsageDropsWhenDisabled(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt,
		`{"is_enabled": false, "utilization": null, "user_disabled": true}`)

	if res := Build(extraCtx(t, root, time.Minute)); !res.Empty {
		t.Errorf("disabled extra usage rendered %+v, want empty", res.Fields)
	}
}

// Flag off while number linger describe window that ran before somebody
// switched it off. Printing it claim credit this account no longer spend.
func TestExtraUsageDropsDisabledWindowCarryingNumber(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": false, "utilization": 42}`)

	if res := Build(extraCtx(t, root, time.Minute)); !res.Empty {
		t.Errorf("stale number under off flag rendered %+v, want empty", res.Fields)
	}
}

// Enabled block reporting no number prove no room either way, and printed 0
// claim full month that may be spent.
func TestExtraUsageDropsNullUtilization(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": true, "utilization": null}`)

	if res := Build(extraCtx(t, root, time.Minute)); !res.Empty {
		t.Errorf("null utilization rendered %+v, want empty", res.Fields)
	}
}

// Account never offered extra usage carry no block at all.
func TestExtraUsageDropsWhenBlockAbsent(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt)

	if res := Build(extraCtx(t, root, time.Minute)); !res.Empty {
		t.Errorf("absent block rendered %+v, want empty", res.Fields)
	}
}

// Cache past TTL name window that may have reset since, same rule limit.model
// hold. Both segment read one block, so staleness must not answer twice.
func TestExtraUsageDropsStaleCache(t *testing.T) {
	root := t.TempDir()
	writeExtraUsage(t, root, usageFetchedAt, `{"is_enabled": true, "utilization": 18}`)

	if res := Build(extraCtx(t, root, usageCacheTTL+time.Minute)); !res.Empty {
		t.Errorf("stale cache rendered %+v, want empty", res.Fields)
	}
}

// Relative root reach render path, and empty one leave filepath.Join naming
// cwd. Segment read nothing rather than open file beside whatever directory
// Claude Code happen to run from.
func TestExtraUsageDropsWithoutConfigRoot(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.extra")
	c.Now = usageFetchedAt.Add(time.Minute)

	if res := Build(c); !res.Empty {
		t.Errorf("empty config root rendered %+v, want empty", res.Fields)
	}
}
