package segment

import (
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
)

// Default templates against complete data. Reordering a row inherit these, so
// most users never see anything else.
func TestDefaultTemplatesOnFullData(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{"model", "Opus 4.8"},
		{"context", "✍️ 42%"},
		{"session", "⏱ 1h15m"},
		{"effort", "● high"},
		{"cost", "$1.23"},
		{"lines", "+156 -23"},
		{"version", "2.1.211"},
		{"vim", "NORMAL"},
		{"output_style", "default"},
		{"repo", "acme/acme"},
		{"pr", "#42"},
		{"thinking", "think"},
	} {
		if got := draw(ctx(t, fixtures.Full, tc.kind)); got != tc.want {
			t.Errorf("%s rendered %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// Sparse document is non-subscriber on first render. Every segment resting on
// data that has not arrived must drop out rather than print a placeholder.
func TestSegmentsAbsentOnSparseData(t *testing.T) {
	for _, kind := range []string{
		"context",      // used_percentage null, current_usage null
		"effort",       // model carry no effort parameter
		"limit.5h",     // rate_limits reach subscribers only
		"limit.7d",     //
		"repo",         // directory outside any known repository
		"vim",          // vim mode off
		"pr",           // no pull request open
		"thinking",     //
		"output_style", //
	} {
		if got := draw(ctx(t, fixtures.Sparse, kind)); got != "" {
			t.Errorf("%s rendered %q on sparse data, want nothing", kind, got)
		}
	}
}

// Zero is a measurement, absent is not. Sparse fixture carry real zeros for
// cost and lines, and those must still print.
func TestZeroValuesStillRender(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{"cost", "$0.00"},
		{"lines", "+0 -0"},
		{"session", "⏱ 1s"},
	} {
		if got := draw(ctx(t, fixtures.Sparse, tc.kind)); got != tc.want {
			t.Errorf("%s rendered %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestModelFallsBackToIDWhenUnnamed(t *testing.T) {
	c := ctx(t, fixtures.Full, "model")
	c.In.Model.DisplayName = ""
	c.Cfg.Template = "{id}"

	if got := draw(c); got != "claude-opus-4-8" {
		t.Errorf("rendered %q, want claude-opus-4-8", got)
	}

	c.In.Model.ID = ""
	if res := Build(c); !res.Empty {
		t.Error("model with neither name nor id should be empty")
	}
}

// {name} join family and version, so absent half leave no stray space. Template
// writing "{family} {version}" cannot do that.
func TestModelNameJoinsFamilyAndVersion(t *testing.T) {
	c := ctx(t, fixtures.Full, "model")
	res := Build(c)

	for _, tc := range []struct{ field, want string }{
		{"name", "Opus 4.8"},
		{"family", "Opus"},
		{"version", "4.8"},
	} {
		if got := res.Fields[tc.field].Text; got != tc.want {
			t.Errorf("{%s} = %q, want %q", tc.field, got, tc.want)
		}
	}

	// Id carrying no version leave family standing alone.
	c.In.Model.ID = "some-vendor-model"
	if got := Build(c).Fields["name"].Text; got != "Opus" {
		t.Errorf("versionless id rendered %q, want Opus", got)
	}

	// Unnamed family leave version standing alone, never a leading space.
	c.In.Model.DisplayName = ""
	c.In.Model.ID = "claude-opus-4-8"
	if got := Build(c).Fields["name"].Text; got != "4.8" {
		t.Errorf("unnamed family rendered %q, want 4.8", got)
	}
}

// display_name carrying a version make plain join print "Opus 4.8 4.8".
func TestModelNameNeverRepeatsVersion(t *testing.T) {
	for _, tc := range []struct{ family, version, want string }{
		{"Opus", "4.8", "Opus 4.8"},
		{"Opus 4.8", "4.8", "Opus 4.8"},
		{"4.8", "4.8", "4.8"},
		{"Opus", "", "Opus"},
		{"", "4.8", "4.8"},
		{"Opus 4.8", "4.1", "Opus 4.8 4.1"}, // genuine disagreement still show
	} {
		if got := joinModelName(tc.family, tc.version); got != tc.want {
			t.Errorf("joinModelName(%q, %q) = %q, want %q",
				tc.family, tc.version, got, tc.want)
		}
	}
}

// Context percentage unknown is not zero. Before first API call and after
// /compact Claude Code report no usage at all, and 0% there claim empty context
// where truth is unreported one.
func TestContextDistinguishesUnknownFromZero(t *testing.T) {
	if res := Build(ctx(t, fixtures.Sparse, "context")); !res.Empty {
		t.Errorf("unreported usage gave %+v, want empty", res)
	}

	c := ctx(t, fixtures.Sparse, "context")
	zero := 0.0
	c.In.Context.UsedPercentage = &zero
	if got := draw(c); got != "✍️ 0%" {
		t.Errorf("reported zero rendered %q, want ✍️ 0%%", got)
	}
}

// {used} count current_usage rather than back-computing from a percentage
// already rounded to whole points. Fixture sum 8500+5000+70700 = 84.2k, where
// 42% of a 200k window would report a flat 84k.
func TestContextDerivesSizeAndUsed(t *testing.T) {
	c := ctx(t, fixtures.Full, "context")
	c.Cfg.Template = "{used}/{size} {remaining}"

	if got := draw(c); got != "84.2k/200k 58" {
		t.Errorf("rendered %q, want 84.2k/200k 58", got)
	}
}

// current_usage absent leave nothing to count, so {used} fall back rather than
// drop.
func TestContextUsedFallsBackToPercentage(t *testing.T) {
	c := ctx(t, fixtures.Full, "context")
	c.In.Context.CurrentUsage = nil
	c.Cfg.Template = "{used}"

	if got := draw(c); got != "84k" {
		t.Errorf("rendered %q, want 84k", got)
	}
}

// Bar cell count and printed percentage round alike, so neither contradict
// other on screen.
func TestContextBarAgreesWithPercentage(t *testing.T) {
	c := ctx(t, fixtures.Full, "context")
	c.Cfg.Template = "{bar} {pct}"

	got := draw(c)
	if !strings.HasPrefix(got, strings.Repeat("●", 4)+strings.Repeat("○", 6)) {
		t.Errorf("rendered %q, want 4 filled cells for 42%%", got)
	}
	if !strings.HasSuffix(got, " 42") {
		t.Errorf("rendered %q, want percentage 42", got)
	}
}

func TestEffortIconTracksLevel(t *testing.T) {
	for _, tc := range []struct{ level, want string }{
		{"max", "●"},
		{"xhigh", "●"},
		{"high", "●"},
		{"medium", "◑"},
		{"low", "◔"},
		{"whatever-ships-next", "◑"}, // unknown level still get a slot
	} {
		icon, _ := effortStyle(tc.level)
		if icon != tc.want {
			t.Errorf("effortStyle(%q) icon = %q, want %q", tc.level, icon, tc.want)
		}
	}
}

// Rate limit windows go absent one at a time, so each level need its own check.
func TestLimitWindowsAreIndependent(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.5h")
	c.In.RateLimits.FiveHour = nil
	if res := Build(c); !res.Empty {
		t.Errorf("missing five_hour gave %+v, want empty", res)
	}

	weekly := ctx(t, fixtures.Full, "limit.7d")
	weekly.In.RateLimits.FiveHour = nil
	if res := Build(weekly); res.Empty {
		t.Error("seven_day dropped along with five_hour")
	}
}

// Weekly window reset days out, so bare clock time read ambiguous. Windows
// differ in that alone.
func TestLimitWindowsFormatResetDifferently(t *testing.T) {
	five := Build(ctx(t, fixtures.Full, "limit.5h")).Fields["reset_time"].Text
	seven := Build(ctx(t, fixtures.Full, "limit.7d")).Fields["reset_time"].Text

	if strings.Contains(five, ",") {
		t.Errorf("five hour reset = %q, want bare clock time", five)
	}
	if !strings.Contains(seven, ",") {
		t.Errorf("seven day reset = %q, want a date alongside time", seven)
	}
}

// {reset} carry its own ⟳, so absent reset leave no icon pointing at nothing.
func TestLimitWithoutResetLeavesNoDanglingIcon(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.5h")
	c.In.RateLimits.FiveHour.ResetsAt = nil

	got := draw(c)
	if strings.Contains(got, "⟳") {
		t.Errorf("rendered %q, want no reset icon", got)
	}
	if got != "current ●●●●○○○○○○  42%" {
		t.Errorf("rendered %q", got)
	}

	res := Build(c)
	if res.Fields["reset"].Text != "" || res.Fields["reset_time"].Text != "" {
		t.Errorf("reset = %q, reset_time = %q; want both empty",
			res.Fields["reset"].Text, res.Fields["reset_time"].Text)
	}
}

// Present reset read exactly as before icon moved. Local zone decide time, so
// expectation build from field itself.
func TestLimitDefaultTemplateShowsReset(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.5h")
	at := Build(c).Fields["reset_time"].Text
	if at == "" {
		t.Fatal("fixture carry no reset time")
	}

	if got, want := draw(c), "current ●●●●○○○○○○  42% ⟳ "+at; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Only worth a slot when it is on; "off" marker is noise on a crowded row.
func TestFastModeShowsOnlyWhenOn(t *testing.T) {
	if res := Build(ctx(t, fixtures.Full, "fast_mode")); !res.Empty {
		t.Errorf("fast_mode false gave %+v, want empty", res)
	}

	c := ctx(t, fixtures.Full, "fast_mode")
	on := true
	c.In.FastMode = &on
	if got := draw(c); got != "⚡fast" {
		t.Errorf("rendered %q, want ⚡fast", got)
	}
}

// Severity colour ride on numbers, not on labels, so one glance at row tell
// whether anything need attention.
func TestPercentagesCarrySeverityColour(t *testing.T) {
	c := ctx(t, fixtures.Full, "context")
	c.Palette = render.NewPalette()
	c.In.Context.UsedPercentage = ptr(95.0)

	if got := Build(c).Fields["pct"].Color; got != render.Red {
		t.Errorf("95%% coloured %q, want red", got)
	}

	c.In.Context.UsedPercentage = ptr(10.0)
	if got := Build(c).Fields["pct"].Color; got != render.Green {
		t.Errorf("10%% coloured %q, want green", got)
	}
}

func ptr[T any](v T) *T { return &v }
