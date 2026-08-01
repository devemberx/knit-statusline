package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

// assistantLine mirror what Claude Code write: one JSONL line per content block,
// each repeating whole usage object for its message.
func assistantLine(msgID string, in, cw, cr, out int64) string {
	return fmt.Sprintf(
		`{"type":"assistant","message":{"id":%q,"model":"claude-opus-4-8","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		msgID, in, cw, cr, out)
}

// tokensCtx point tokens segment at throwaway transcript holding lines.
func tokensCtx(t *testing.T, lines ...string) Context {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	var body []byte
	for _, l := range lines {
		body = append(body, l...)
		body = append(body, '\n')
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	c := ctx(t, fixtures.Full, "tokens")
	c.In.TranscriptPath = path
	c.CacheDir = filepath.Join(dir, "cache")
	return c
}

// Fixture transcript_path point nowhere on purpose, so this stand in for every
// session before its first assistant reply. tokens Stable now: ctx() default
// to live, unprovable freshness, so absent transcript hold placeholder rather
// than drop.
func TestBuildTokensRendersPlaceholderWithoutATranscript(t *testing.T) {
	c := ctx(t, fixtures.Full, "tokens")
	c.In.TranscriptPath = ""
	if got := draw(c); got != "↑… ↓…" {
		t.Errorf("got %q, want placeholder", got)
	}

	if got := draw(ctx(t, fixtures.Full, "tokens")); got != "↑… ↓…" {
		t.Errorf("absent transcript file gave %q, want placeholder", got)
	}
}

// Zero counted is absent, not zero: printing "↑0 ↓0" claim measurement nobody
// took. Stable slot hold placeholder instead of dropping.
func TestBuildTokensRendersPlaceholderWhenNothingCounted(t *testing.T) {
	c := tokensCtx(t, `{"type":"user","message":{"role":"user","content":"hi"}}`)
	if got := draw(c); got != "↑… ↓…" {
		t.Errorf("got %q, want placeholder", got)
	}
}

func TestBuildTokensRendersAbbreviatedCounts(t *testing.T) {
	c := tokensCtx(t,
		assistantLine("msg_a", 62_093, 1_200_000, 32_400_000, 231_000),
		assistantLine("msg_a", 62_093, 1_200_000, 32_400_000, 231_000),
	)

	res := Build(c)
	if res.Empty {
		t.Fatal("populated transcript rendered empty")
	}
	for _, tc := range []struct{ field, want string }{
		{"input", "62.1k"},
		{"cache_write", "1.2M"},
		{"cache_read", "32.4M"},
		{"output", "231k"},
		{"total", "33.9M"},
	} {
		if got := res.Fields[tc.field].Text; got != tc.want {
			t.Errorf("{%s} = %q, want %q", tc.field, got, tc.want)
		}
	}
}

// Default row carry both sets, not io alone.
func TestBuildTokensDefaultTemplateShowsBothGroups(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 62_093, 1_200_000, 32_400_000, 231_000))

	if got := draw(c); got != "↑62.1k ↓231k  cache ↑1.2M ↓32.4M" {
		t.Errorf("rendered %q", got)
	}
}

// Session touching no cache drop whole group, its separator included.
func TestBuildTokensCacheGroupVanishesWithoutCacheTraffic(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 100, 0, 0, 400))

	if got := draw(c); got != "↑100 ↓400" {
		t.Errorf("rendered %q, want no cache group and no trailing gap", got)
	}
}

// Two groups differ in color, so eye split them before reading labels.
func TestBuildTokensGroupsCarryDistinctColour(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 62_093, 1_200_000, 32_400_000, 231_000))
	c.Palette = render.NewPalette()
	t.Setenv("NO_COLOR", "")

	res := Build(c)
	io, cache := res.Fields["io"].Text, res.Fields["cache"].Text
	if !strings.Contains(io, string(render.White)) {
		t.Errorf("io group carry no white: %q", io)
	}
	if !strings.Contains(cache, string(render.Cyan)) {
		t.Errorf("cache group carry no cyan: %q", cache)
	}
	if strings.Contains(io, string(render.Cyan)) {
		t.Errorf("io group borrowed cache colour: %q", io)
	}
}

func TestBuildTokensReportsCacheHitRate(t *testing.T) {
	// 100 fresh + 200 written + 700 read = 1000 in, 700 of it cached.
	c := tokensCtx(t, assistantLine("msg_a", 100, 200, 700, 50))

	if got := Build(c).Fields["cache_hit"].Text; got != "70" {
		t.Errorf("{cache_hit} = %q, want 70", got)
	}
}

// Dividing by zero input yield NaN, which only pct's own guard hide.
func TestCacheHitWithoutInput(t *testing.T) {
	if got := cacheHit(transcript.Totals{Output: 400}); got != 0 {
		t.Errorf("cacheHit with no input = %v, want 0", got)
	}
}

// Raw variants exist for input and output alone. Those two run small enough that
// "62.1k" hide digits somebody may want; cache figures reach tens of millions,
// where exact count inform nobody.
func TestBuildTokensCarriesRawInputAndOutputOnly(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 62_093, 5_000, 70_700, 1_200))

	res := Build(c)
	if got := res.Fields["input_raw"].Text; got != "62093" {
		t.Errorf("{input_raw} = %q, want 62093", got)
	}
	if got := res.Fields["output_raw"].Text; got != "1200" {
		t.Errorf("{output_raw} = %q, want 1200", got)
	}
	for _, gone := range []string{"cache_write_raw", "cache_read_raw", "total_raw"} {
		if _, ok := res.Fields[gone]; ok {
			t.Errorf("{%s} should not exist", gone)
		}
	}
}

// Four counters stay apart. Cache read run orders of magnitude over fresh input
// and price differently, so merged figure misrepresent session.
func TestBuildTokensKeepsCountersApart(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 100, 200, 300, 400))

	res := Build(c)
	for _, tc := range []struct{ field, want string }{
		{"input", "100"},
		{"cache_write", "200"},
		{"cache_read", "300"},
		{"output", "400"},
		{"total", "1k"},
	} {
		if got := res.Fields[tc.field].Text; got != tc.want {
			t.Errorf("{%s} = %q, want %q", tc.field, got, tc.want)
		}
	}
}

// scope = "project" widen selection to sibling transcripts. Unmapped, this
// segment would count one session while config promise whole project.
func TestBuildTokensHonoursProjectScope(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 100, 0, 0, 0))
	sibling := filepath.Join(filepath.Dir(c.In.TranscriptPath), "other.jsonl")
	if err := os.WriteFile(sibling, []byte(assistantLine("msg_b", 200, 0, 0, 0)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Build(c).Fields["input"].Text; got != "100" {
		t.Errorf("session scope {input} = %q, want 100", got)
	}

	c.Cfg.Scope = config.ScopeProject
	if got := Build(c).Fields["input"].Text; got != "300" {
		t.Errorf("project scope {input} = %q, want 300", got)
	}
}

// Second render reuse cursor rather than rescanning. Totals must not drift when
// nothing was appended.
func TestBuildTokensAgreeAcrossRenders(t *testing.T) {
	c := tokensCtx(t, assistantLine("msg_a", 100, 10, 1000, 5))

	first := Build(c).Fields["total"].Text
	second := Build(c).Fields["total"].Text
	if first != second {
		t.Errorf("second render gave {total} = %q, first gave %q", second, first)
	}
}

func TestTokensFreshRendersZero(t *testing.T) {
	res := buildTokens(ctxFor("tokens", &schema.Input{}, true))
	if res.Empty {
		t.Fatal("fresh tokens returned empty")
	}
	if got := res.Fields["input"].Text; got != "0" {
		t.Fatalf("input = %q, want %q", got, "0")
	}
	if got := res.Fields["io"].Text; got != "↑0 ↓0" {
		t.Fatalf("io = %q, want %q", got, "↑0 ↓0")
	}
}

// Cache group stay hidden in both placeholder states: absent traffic is not a
// value, and gap before it belong to group.
func TestTokensPlaceholderHidesCacheGroup(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		res := buildTokens(ctxFor("tokens", &schema.Input{}, fresh))
		if got := res.Fields["cache"].Text; got != "" {
			t.Fatalf("fresh=%v cache = %q, want empty", fresh, got)
		}
	}
}

func TestTokensLiveWithoutUsageRendersUnknown(t *testing.T) {
	res := buildTokens(ctxFor("tokens", &schema.Input{}, false))
	if res.Empty {
		t.Fatal("live tokens returned empty")
	}
	if got := res.Fields["io"].Text; got != "↑… ↓…" {
		t.Fatalf("io = %q, want %q", got, "↑… ↓…")
	}
	if got := res.Fields["total"].Text; got != config.DefaultUnknown {
		t.Fatalf("total = %q, want %q", got, config.DefaultUnknown)
	}
}

func TestTokensOptedOutDrops(t *testing.T) {
	c := ctxFor("tokens", &schema.Input{}, true)
	c.Cfg.Unknown = ""
	if !buildTokens(c).Empty {
		t.Fatal("unknown = \"\" did not drop the segment")
	}
}
