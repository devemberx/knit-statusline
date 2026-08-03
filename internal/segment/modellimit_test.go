package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
)

// usageFetchedAt is instant fixture cache claim it was written at. One hour
// before PreviewEpoch would sit exactly on TTL edge, so tests state own age.
var usageFetchedAt = time.Unix(fixtures.PreviewEpoch, 0)

// scopedLimitJSON build one limits[] entry. Percent and scope written raw, so
// test reach null on either.
func scopedLimitJSON(kind, percent, scope, resetsAt string) string {
	return fmt.Sprintf(
		`{"kind":%q,"group":"weekly","percent":%s,"severity":"critical","resets_at":%q,"scope":%s,"is_active":true}`,
		kind, percent, resetsAt, scope)
}

func modelScope(name string) string {
	return fmt.Sprintf(`{"model":{"id":null,"display_name":%q},"surface":null}`, name)
}

// writeUsageCache plant .claude.json carrying cachedUsageUtilization, way
// Claude Code write it.
func writeUsageCache(t *testing.T, dir string, fetchedAt time.Time, limits ...string) {
	t.Helper()
	doc := fmt.Sprintf(`{
  "installMethod": "native",
  "cachedUsageUtilization": {
    "fetchedAtMs": %d,
    "accountUuid": "1317b16d-ddb0-44d4-afa6-2ebc38e3620b",
    "utilization": {
      "five_hour": {"utilization": 87, "resets_at": "2025-07-23T12:50:00.809792+00:00"},
      "seven_day": {"utilization": 89, "resets_at": "2025-07-28T21:00:00.809814+00:00"},
      "seven_day_opus": null,
      "limits": [%s]
    }
  }
}`, fetchedAt.UnixMilli(), joinJSON(limits))
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
}

func joinJSON(items []string) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ","
		}
		out += it
	}
	return out
}

// modelLimitCtx build Context statusline.Render way, with config root pointing
// at seeded directory.
func modelLimitCtx(t *testing.T, doc []byte, root string, age time.Duration) Context {
	t.Helper()
	c := ctx(t, doc, "limit.model")
	c.ConfigDir = root
	c.Now = usageFetchedAt.Add(age)
	return c
}

// Weekly window scoped to session model is whole reason segment exist: number
// live in no payload field, so nothing else on row can report it.
func TestModelLimitReadsScopedWindowForSessionModel(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("session", "87", "null", "2025-07-23T12:50:00.809792+00:00"),
		scopedLimitJSON("weekly_all", "89", "null", "2025-07-28T21:00:00.809814+00:00"),
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if res.Empty {
		t.Fatal("scoped window for session model dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q", got, "42")
	}
	if got := res.Fields["model"].Text; got != "Opus" {
		t.Errorf("model = %q, want %q", got, "Opus")
	}
	if got := res.Fields["reset_time"].Text; got != "jul 28, 8:59pm" {
		t.Errorf("reset_time = %q, want %q", got, "jul 28, 8:59pm")
	}
}

// Unscoped rows sit in same array. Reading them here print account-wide weekly
// under model name limit.7d already carry.
func TestModelLimitIgnoresUnscopedWindows(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("session", "87", "null", "2025-07-23T12:50:00.809792+00:00"),
		scopedLimitJSON("weekly_all", "89", "null", "2025-07-28T21:00:00.809814+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("unscoped windows rendered %+v, want empty", res.Fields)
	}
}

// Window belong to another model: account carry Fable week while session run
// Opus, and printing it under this session claim limit that does not bind here.
func TestModelLimitSkipsOtherModelsWindow(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "100", modelScope("Fable"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("other model's window rendered %+v, want empty", res.Fields)
	}
}

// model = "Fable" pin row to account-wide Fable week, readable from any
// session. Window keep filling while user sit in Opus.
func TestModelLimitHonoursPinnedModel(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "100", modelScope("Fable"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.Cfg.Model = "Fable"
	res := Build(c)
	if res.Empty {
		t.Fatal("pinned model dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "100" {
		t.Errorf("pct = %q, want %q", got, "100")
	}
	if got := res.Fields["model"].Text; got != "Fable" {
		t.Errorf("model = %q, want %q", got, "Fable")
	}
}

// Pin name model, not session's. Pinned Fable with no Fable window must drop
// rather than fall through to whatever window session model own.
func TestModelLimitPinDoesNotFallBackToSessionModel(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.Cfg.Model = "Fable"
	if res := Build(c); !res.Empty {
		t.Errorf("pinned Fable rendered Opus window %+v, want empty", res.Fields)
	}
}

// Three names for one model: usage document write "Fable", payload display name
// may carry release number, id carry claude-fable-5. Match on family word cover
// all three.
func TestModelLimitMatchesModelByIDFamily(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "63", modelScope("Fable"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-fable-5[1m]", DisplayName: "Fable 5"}
	res := Build(c)
	if res.Empty {
		t.Fatal("id family match dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "63" {
		t.Errorf("pct = %q, want %q", got, "63")
	}
}

// Claude Code discard this cache past one hour, and window may have reset in
// meantime. Stale percentage state fact no render measured.
func TestModelLimitDropsStaleCache(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, usageCacheTTL+time.Second)); !res.Empty {
		t.Errorf("cache one hour old rendered %+v, want empty", res.Fields)
	}
	// Edge itself still readable: Claude Code cut past TTL, not at it.
	if res := Build(modelLimitCtx(t, fixtures.Full, root, usageCacheTTL)); res.Empty {
		t.Error("cache exactly at TTL dropped, want rendered")
	}
}

// Stamp ahead of clock leave no age to reason about: clock moved, or file came
// off another machine.
func TestModelLimitDropsFutureStamp(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, -time.Minute)); !res.Empty {
		t.Errorf("cache stamped in future rendered %+v, want empty", res.Fields)
	}
}

// null percent prove no room. Printing 0 claim full week that may be spent.
func TestModelLimitDropsNullPercent(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "null", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("null percent rendered %+v, want empty", res.Fields)
	}
}

// Unparseable reset cost reset alone. Percentage is number row exist for.
func TestModelLimitKeepsPercentWhenResetUnparseable(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "not a timestamp"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if res.Empty {
		t.Fatal("unparseable reset dropped whole segment")
	}
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q", got, "42")
	}
	if got := res.Fields["reset"].Text; got != "" {
		t.Errorf("reset = %q, want empty", got)
	}
}

// Default install leave .claude.json beside config root, not inside it, so
// probe run both. CLAUDE_CONFIG_DIR case is directory-inside one above.
func TestModelLimitReadsClaudeJSONBesideRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	writeUsageCache(t, home, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); res.Empty {
		t.Error(".claude.json beside root dropped segment")
	}
}

// Half-written file catch render mid-rewrite: Claude Code touch .claude.json
// constantly. Drop, never panic.
func TestModelLimitSurvivesBrokenJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"cachedUsageUtil`), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("broken JSON rendered %+v, want empty", res.Fields)
	}
}

// No config root mean no file to read. Joining empty root give relative
// ".claude.json", read out of whatever directory Claude Code started in.
func TestModelLimitDropsWithoutConfigRoot(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.model")
	c.Now = usageFetchedAt
	if res := Build(c); !res.Empty {
		t.Errorf("empty config root rendered %+v, want empty", res.Fields)
	}
}

// Percentage past 100 run bar off own width. Document report 100 on spent
// window today, nothing promise it stop there.
func TestModelLimitClampsPercent(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "137", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.Palette = render.NoColor()
	res := Build(c)
	if res.Empty {
		t.Fatal("over-full window dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "100" {
		t.Errorf("pct = %q, want %q", got, "100")
	}
	if got := len([]rune(res.Fields["bar"].Text)); got != config.DefaultBarWidth {
		t.Errorf("bar width = %d, want %d", got, config.DefaultBarWidth)
	}
}
