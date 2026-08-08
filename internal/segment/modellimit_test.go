package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// Claude Code write it. Top-level per-model key read null, which is what every
// observed account carry.
func writeUsageCache(t *testing.T, dir string, fetchedAt time.Time, limits ...string) {
	t.Helper()
	writeUsageCacheOpus(t, dir, fetchedAt, "null", limits...)
}

// writeUsageCacheOpus plant same cache with seven_day_opus written out, second
// shape cache carry per-model week in.
func writeUsageCacheOpus(t *testing.T, dir string, fetchedAt time.Time, opus string, limits ...string) {
	t.Helper()
	doc := fmt.Sprintf(`{
  "installMethod": "native",
  "cachedUsageUtilization": {
    "fetchedAtMs": %d,
    "accountUuid": "1317b16d-ddb0-44d4-afa6-2ebc38e3620b",
    "utilization": {
      "five_hour": {"utilization": 87, "resets_at": "2025-07-23T12:50:00.809792+00:00"},
      "seven_day": {"utilization": 89, "resets_at": "2025-07-28T21:00:00.809814+00:00"},
      "seven_day_opus": %s,
      "limits": [%s]
    }
  }
}`, fetchedAt.UnixMilli(), opus, strings.Join(limits, ","))
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
}

// wantReset render stamp way row must show it: local zone, spelled out here
// rather than taken off dateTime, so production formatter answer to something.
//
// Literal expectation would state UTC wall clock and pass in every zone,
// hiding exactly what this guard exist for.
func wantReset(t *testing.T, stamp string) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("parse %q: %v", stamp, err)
	}
	return strings.ToLower(at.Local().Format("Jan 2, 3:04pm"))
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
	if want := wantReset(t, "2025-07-28T20:59:59.810071+00:00"); res.Fields["reset_time"].Text != want {
		t.Errorf("reset_time = %q, want %q", res.Fields["reset_time"].Text, want)
	}
}

// Usage document stamp reset as RFC 3339 carrying "+00:00"; payload stamp it as
// Unix seconds, which time.Unix hand back in local zone. Segments sharing row
// must land on one clock -- unmarked UTC beside local time read as two resets,
// and preset put limit.7d and limit.model side by side.
func TestModelLimitResetAgreesWithAccountWideWindow(t *testing.T) {
	const stamp = "2025-07-28T20:59:59.810071+00:00"
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("parse stamp: %v", err)
	}

	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), stamp),
	)
	scoped := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if scoped.Empty {
		t.Fatal("scoped window dropped")
	}

	sec, used := at.Unix(), 42.0
	c := ctx(t, fixtures.Full, "limit.7d")
	c.In.RateLimits = &schema.RateLimits{
		SevenDay: &schema.Window{UsedPercentage: &used, ResetsAt: &sec},
	}
	account := Build(c)

	if got, want := scoped.Fields["reset_time"].Text, account.Fields["reset_time"].Text; got != want {
		t.Errorf("one instant, two clocks: limit.model %q, limit.7d %q", got, want)
	}
}

// Cache write per-model week two ways and limits[] is not guaranteed to be one
// of them. Reading array alone drop row with no marker at all -- Def.Stable
// stay false -- so release moving shape look like account that never had window.
func TestModelLimitReadsTopLevelWindowWhenArrayEmpty(t *testing.T) {
	root := t.TempDir()
	writeUsageCacheOpus(t, root, usageFetchedAt,
		`{"utilization": 42, "resets_at": "2025-07-28T20:59:59.810071+00:00"}`,
		scopedLimitJSON("weekly_all", "89", "null", "2025-07-28T21:00:00.809814+00:00"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if res.Empty {
		t.Fatal("top-level per-model window dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q", got, "42")
	}
	// Shape carry no display name. Label come off payload model this session
	// already run under -- bare family word would print lowercase "opus".
	if got := res.Fields["model"].Text; got != "Opus" {
		t.Errorf("model = %q, want %q", got, "Opus")
	}
	if want := wantReset(t, "2025-07-28T20:59:59.810071+00:00"); res.Fields["reset_time"].Text != want {
		t.Errorf("reset_time = %q, want %q", res.Fields["reset_time"].Text, want)
	}
}

// Both shapes populated on one account mean cache mid-migration. Array entry
// win: it name its own model, so row print name usage document print.
func TestModelLimitPrefersScopedArrayOverTopLevel(t *testing.T) {
	root := t.TempDir()
	writeUsageCacheOpus(t, root, usageFetchedAt,
		`{"utilization": 11, "resets_at": "2025-07-28T20:59:59.810071+00:00"}`,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q -- top-level shape beat limits[]", got, "42")
	}
	if got := res.Fields["model"].Text; got != "Opus" {
		t.Errorf("model = %q, want %q", got, "Opus")
	}
}

// Key present holding null is what every observed account carry, and shape
// reporting no number prove no window.
func TestModelLimitIgnoresNullTopLevelWindow(t *testing.T) {
	root := t.TempDir()
	writeUsageCacheOpus(t, root, usageFetchedAt, `{"utilization": null, "resets_at": null}`)

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("null utilization rendered %+v, want empty", res.Fields)
	}
}

// Top-level key name family, so window belonging to another model must not
// answer this session. Sonnet week under Opus session bind nothing here.
func TestModelLimitSkipsTopLevelWindowOfOtherModel(t *testing.T) {
	root := t.TempDir()
	writeUsageCacheOpus(t, root, usageFetchedAt, "null")
	// seven_day_sonnet planted beside null opus key: family word decide, not
	// presence of any per-model key.
	doc, err := os.ReadFile(filepath.Join(root, ".claude.json"))
	if err != nil {
		t.Fatalf("read seeded cache: %v", err)
	}
	swapped := strings.Replace(string(doc),
		`"seven_day_opus": null`,
		`"seven_day_sonnet": {"utilization": 42, "resets_at": null}`, 1)
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(swapped), 0o644); err != nil {
		t.Fatalf("write swapped cache: %v", err)
	}

	if res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute)); !res.Empty {
		t.Errorf("Sonnet window rendered under Opus session: %+v", res.Fields)
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

	// Display name blank leave id as only handle, so this exercise id path
	// rather than pass on display name that already match.
	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-fable-5[1m]"}
	res := Build(c)
	if res.Empty {
		t.Fatal("id family match dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "63" {
		t.Errorf("pct = %q, want %q", got, "63")
	}
}

// Dated id put two numbers ahead of family: claude-3-5-sonnet-20241022 give
// "3" to anything reading first part after vendor word.
func TestModelLimitMatchesDatedModelID(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "29", modelScope("Sonnet"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-3-5-sonnet-20241022"}
	res := Build(c)
	if res.Empty {
		t.Fatal("dated id dropped its own model's window")
	}
	if got := res.Fields["pct"].Text; got != "29" {
		t.Errorf("pct = %q, want %q", got, "29")
	}
}

// Pin naming no family bind row nowhere. Falling through to session model print
// window user never asked for, under name they never typed.
func TestModelLimitPinNamingNoFamilyDrops(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	for _, pin := range []string{"Claude", "claude", "5", "4.5", "-"} {
		c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
		c.Cfg.Model = pin
		if res := Build(c); !res.Empty {
			t.Errorf("pin %q rendered %+v, want empty", pin, res.Fields)
		}
	}
}

// Trailing slash on CLAUDE_CONFIG_DIR leave filepath.Dir pointing at root
// itself, collapsing both probe onto one file and losing beside-root copy.
func TestModelLimitReadsBesideTrailingSlashRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	writeUsageCache(t, home, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	if res := Build(modelLimitCtx(t, fixtures.Full, root+string(filepath.Separator), time.Minute)); res.Empty {
		t.Error("trailing-slash root dropped segment")
	}
}

// Vendor word name no family. Display name carrying it must still match its own
// model, so stripping it cost no window.
func TestModelLimitMatchesVendorPrefixedDisplayName(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "63", modelScope("Claude Fable 5"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-fable-5[1m]", DisplayName: "Fable 5"}
	res := Build(c)
	if res.Empty {
		t.Fatal("vendor-prefixed display name dropped its own model's window")
	}
	if got := res.Fields["pct"].Text; got != "63" {
		t.Errorf("pct = %q, want %q", got, "63")
	}
}

// Every model id start "claude-", so vendor word left in name pool match
// "Claude Opus 4.5" from Fable session and print another model's week.
func TestModelLimitVendorWordMatchesNoModel(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Claude Opus 4.5"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-fable-5[1m]", DisplayName: "Fable 5"}
	if res := Build(c); !res.Empty {
		t.Errorf("Opus window rendered %+v under a Fable session, want empty", res.Fields)
	}
}

// Release number distinguish no window: /usage scope its row by family, and
// session id carry release number usage document need not repeat.
func TestModelLimitMatchesAcrossReleaseNumbers(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "51", modelScope("Opus 4.5"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-opus-5", DisplayName: "Opus 5"}
	res := Build(c)
	if res.Empty {
		t.Fatal("release number split one family into two windows")
	}
	if got := res.Fields["pct"].Text; got != "51" {
		t.Errorf("pct = %q, want %q", got, "51")
	}
}

// Account hold one scoped window per model. Walk must reach session's own
// rather than stop at whichever entry sit first.
func TestModelLimitPicksOwnWindowAmongSeveral(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "12", modelScope("Claude Opus 4.5"), "2025-07-28T20:59:59.810071+00:00"),
		scopedLimitJSON("weekly_scoped", "34", modelScope("Sonnet 4.5"), "2025-07-28T20:59:59.810071+00:00"),
		scopedLimitJSON("weekly_scoped", "56", modelScope("Fable"), "2025-07-28T20:59:59.810071+00:00"),
	)

	c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
	c.In.Model = schema.Model{ID: "claude-fable-5[1m]", DisplayName: "Fable 5"}
	res := Build(c)
	if res.Empty {
		t.Fatal("session's own window dropped while two others sat ahead of it")
	}
	if got := res.Fields["pct"].Text; got != "56" {
		t.Errorf("pct = %q, want %q", got, "56")
	}
	if got := res.Fields["model"].Text; got != "Fable" {
		t.Errorf("model = %q, want %q", got, "Fable")
	}
}

// Model id is what /model and payload show, so pin get pasted from it. Silent
// drop there leave user no error to read.
func TestModelLimitPinAcceptsModelID(t *testing.T) {
	root := t.TempDir()
	writeUsageCache(t, root, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "77", modelScope("Fable"), "2025-07-28T20:59:59.810071+00:00"),
	)

	for _, pin := range []string{"Fable", "fable", "Fable 5", "Claude Fable 5", "claude-fable-5[1m]"} {
		c := modelLimitCtx(t, fixtures.Full, root, time.Minute)
		c.In.Model = schema.Model{ID: "claude-opus-5", DisplayName: "Opus 5"}
		c.Cfg.Model = pin
		res := Build(c)
		if res.Empty {
			t.Errorf("pin %q dropped segment", pin)
			continue
		}
		if got := res.Fields["pct"].Text; got != "77" {
			t.Errorf("pin %q: pct = %q, want %q", pin, got, "77")
		}
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

// Moved config root leave two .claude.json, and only one hold usage block.
// Stopping at first file that merely parse report no window while live copy sit
// one directory up.
func TestModelLimitFallsPastFileWithoutUsageBlock(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte(`{"installMethod":"native"}`), 0o644); err != nil {
		t.Fatalf("write inner .claude.json: %v", err)
	}
	writeUsageCache(t, home, usageFetchedAt,
		scopedLimitJSON("weekly_scoped", "42", modelScope("Opus"), "2025-07-28T20:59:59.810071+00:00"),
	)

	res := Build(modelLimitCtx(t, fixtures.Full, root, time.Minute))
	if res.Empty {
		t.Fatal("usage block one directory up dropped segment")
	}
	if got := res.Fields["pct"].Text; got != "42" {
		t.Errorf("pct = %q, want %q", got, "42")
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
