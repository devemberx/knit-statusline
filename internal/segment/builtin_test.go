package segment

import (
	"math"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
)

// Default templates against complete data. Reordering a row inherit these, so
// most users never see anything else.
func TestDefaultTemplatesOnFullData(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{"model", "Opus 4.8"},
		{"context", "✍️ 42%"},
		{"session", "⏱ 1h15m"},
		{"effort", "◕ high"},
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
// context, limit.5h and limit.7d excluded: all three are Stable now, holding
// their slot instead of dropping -- see TestContextLiveWithoutUsageRendersUnknown
// and TestLimitNeverRendersZeroWhenFresh.
func TestSegmentsAbsentOnSparseData(t *testing.T) {
	for _, kind := range []string{
		"effort",       // model carry no effort parameter
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
// /compact Claude Code report no usage at all, and 0% there claim occupancy
// nobody reported. context is Stable, so it hold its slot with placeholder
// rather than drop -- ctx() default to live, unprovable freshness.
func TestContextDistinguishesUnknownFromZero(t *testing.T) {
	if got := draw(ctx(t, fixtures.Sparse, "context")); got != "✍️ …%" {
		t.Errorf("unreported usage rendered %q, want placeholder", got)
	}

	c := ctx(t, fixtures.Sparse, "context")
	zero := 0.0
	c.In.Context.UsedPercentage = &zero
	if got := draw(c); got != "✍️ 0%" {
		t.Errorf("reported zero rendered %q, want ✍️ 0%%", got)
	}
}

// usedTokens back-compute int64(p/100*size), and int64(NaN) is minInt64 on
// amd64. No guard there, so NaN fall to placeholder like any unknown
// percentage, never garbage.
func TestContextNaNFallsBackToPlaceholder(t *testing.T) {
	c := ctx(t, fixtures.Sparse, "context")
	nan := math.NaN()
	c.In.Context.UsedPercentage = &nan

	if got := draw(c); got != "✍️ …%" {
		t.Errorf("NaN percentage rendered %q, want placeholder", got)
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

// Five levels Claude Code emit, each own glyph and color. Unknown level get own
// slot too.
func TestEffortStyleSeparatesEveryLevel(t *testing.T) {
	levels := []struct {
		level string
		icon  string
		color render.Color
	}{
		{"low", "◔", render.Dim},
		{"medium", "◑", render.White},
		{"high", "◕", render.Cyan},
		{"xhigh", "●", render.Magenta},
		{"max", "✦", render.Orange},
	}

	icons := map[string]string{}
	colors := map[render.Color]string{}
	for _, tc := range levels {
		icon, color := effortStyle(tc.level)
		if icon != tc.icon || color != tc.color {
			t.Errorf("effortStyle(%q) = %q %q, want %q %q",
				tc.level, icon, color, tc.icon, tc.color)
		}
		// Merge two levels back into one style and this fail, so no release
		// quietly stop telling them apart.
		if prev, dup := icons[icon]; dup {
			t.Errorf("levels %q and %q share icon %q", prev, tc.level, icon)
		}
		if prev, dup := colors[color]; dup {
			t.Errorf("levels %q and %q share color %q", prev, tc.level, color)
		}
		icons[icon], colors[color] = tc.level, tc.level
	}

	// Unknown level take own slot, never medium's -- absent knowledge and
	// known-medium are different facts. Dim shared with low deliberately, so
	// only glyph must stay unique.
	icon, color := effortStyle("whatever-ships-next")
	if icon != "○" || color != render.Dim {
		t.Errorf("effortStyle(unknown) = %q %q, want %q %q", icon, color, "○", render.Dim)
	}
	if prev, dup := icons[icon]; dup {
		t.Errorf("level %q took unknown's icon %q", prev, icon)
	}
}

// Rate limit windows go absent one at a time, so each level need its own check.
// five_hour going nil no longer empties segment -- limit.5h is Stable, so it
// holds its slot with placeholder instead of dropping.
func TestLimitWindowsAreIndependent(t *testing.T) {
	c := ctx(t, fixtures.Full, "limit.5h")
	c.In.RateLimits.FiveHour = nil
	res := Build(c)
	if res.Empty {
		t.Fatal("missing five_hour dropped the segment, want held placeholder")
	}
	if got := res.Fields["pct"].Text; got != config.DefaultUnknown {
		t.Errorf("pct = %q, want %q", got, config.DefaultUnknown)
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

func TestContextIconIsWhiteField(t *testing.T) {
	f := Build(ctx(t, fixtures.Full, "context")).Fields["icon"]
	if f.Text != contextIcon {
		t.Errorf("context {icon} text = %q, want %q", f.Text, contextIcon)
	}
	if f.Color != render.White {
		t.Errorf("context {icon} color = %q, want White %q", f.Color, render.White)
	}
}

// Three build paths, three separate field maps. Icon missing from one expand to
// nothing and shrink slot mid-session, which read as crash.
//
// wantPct pin which path each case land on: fixtures.Unknown gaining context
// block would fold every case onto known-percentage path unnoticed.
func TestContextIconSurvivesEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		doc     []byte
		fresh   bool
		wantPct string
	}{
		{"known", fixtures.Full, false, "42"},
		{"fresh zero", fixtures.Unknown, true, "0"},
		{"unknown", fixtures.Unknown, false, config.DefaultUnknown},
	} {
		c := ctx(t, tc.doc, "context")
		c.Fresh = tc.fresh
		res := Build(c)
		if res.Empty {
			t.Fatalf("%s: context dropped its slot", tc.name)
		}
		if got := res.Fields["pct"].Text; got != tc.wantPct {
			t.Fatalf("%s: fixture no longer exercise its path: {pct} = %q, want %q",
				tc.name, got, tc.wantPct)
		}
		if got := res.Fields["icon"].Text; got != contextIcon {
			t.Errorf("%s: context {icon} = %q, want %q", tc.name, got, contextIcon)
		}
	}
}

// End-to-end over live palette, since field colour alone prove nothing about
// what Expand emit. NO_COLOR set empty so NewPalette enable regardless of
// environment running test.
func TestContextIconEscapesDim(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	c := ctx(t, fixtures.Full, "context")
	c.Palette = render.NewPalette()
	out := draw(c)
	if strings.Contains(out, "\033[2m"+contextIcon) {
		t.Errorf("context icon still under dim: %q", out)
	}
	if !strings.Contains(out, string(render.White)+contextIcon) {
		t.Errorf("context icon not wrapped in White: %q", out)
	}
}

func ptr[T any](v T) *T { return &v }

// ctxFor build Context for one segment name with registry's own default
// template and no-color palette.
func ctxFor(name string, in *schema.Input, fresh bool) Context {
	def, _ := Lookup(name)
	return Context{
		In:      in,
		Cfg:     config.Resolved{Kind: name, Name: name, Template: def.DefaultTemplate, Unknown: config.DefaultUnknown, BarWidth: 10},
		Palette: render.NoColor(),
		Fresh:   fresh,
		stable:  def.Stable,
	}
}

func TestContextFreshRendersRealZero(t *testing.T) {
	res := buildContext(ctxFor("context", &schema.Input{}, true))
	if res.Empty {
		t.Fatal("fresh context returned empty")
	}
	if got := res.Fields["pct"].Text; got != "0" {
		t.Fatalf("pct = %q, want %q", got, "0")
	}
	if got := res.Fields["used"].Text; got != "0" {
		t.Fatalf("used = %q, want %q", got, "0")
	}
}

// Window size was never reported, so zero would be absurd there while used
// stay a real zero. State resolve per field.
func TestContextFreshSizeStaysUnknown(t *testing.T) {
	res := buildContext(ctxFor("context", &schema.Input{}, true))
	if got := res.Fields["size"].Text; got != config.DefaultUnknown {
		t.Fatalf("size = %q, want %q", got, config.DefaultUnknown)
	}
}

func TestContextLiveWithoutUsageRendersUnknown(t *testing.T) {
	res := buildContext(ctxFor("context", &schema.Input{}, false))
	if res.Empty {
		t.Fatal("live context returned empty")
	}
	if got := res.Fields["pct"].Text; got != config.DefaultUnknown {
		t.Fatalf("pct = %q, want %q", got, config.DefaultUnknown)
	}
}

// context_window_size is static model configuration, not usage, so it win over
// freshness on a live session same as it does on a fresh one -- {pct} still
// carry no fact, {size} does.
func TestContextLiveWithSizeShowsRealWindow(t *testing.T) {
	in := &schema.Input{Context: &schema.ContextWin{ContextWindowSize: ptr(int64(200000))}}
	res := buildContext(ctxFor("context", in, false))
	if got := res.Fields["size"].Text; got != "200k" {
		t.Fatalf("size = %q, want %q", got, "200k")
	}
	if got := res.Fields["pct"].Text; got != config.DefaultUnknown {
		t.Fatalf("pct = %q, want %q", got, config.DefaultUnknown)
	}
}

// Payload beat freshness: a fresh session reporting occupancy show it.
func TestContextKnownBeatsFresh(t *testing.T) {
	p := 42.0
	in := &schema.Input{Context: &schema.ContextWin{UsedPercentage: &p}}
	res := buildContext(ctxFor("context", in, true))
	if got := res.Fields["pct"].Text; got != "42" {
		t.Fatalf("pct = %q, want %q", got, "42")
	}
}

func TestContextOptedOutDropsWhenAbsent(t *testing.T) {
	c := ctxFor("context", &schema.Input{}, false)
	c.Cfg.Unknown = ""
	if !buildContext(c).Empty {
		t.Fatal("unknown = \"\" did not drop the segment")
	}
}

func TestContextOptedOutDropsWhenFresh(t *testing.T) {
	c := ctxFor("context", &schema.Input{}, true)
	c.Cfg.Unknown = ""
	if !buildContext(c).Empty {
		t.Fatal("unknown = \"\" did not drop the fresh segment")
	}
}

func buildNamed(t *testing.T, name string, in *schema.Input, fresh bool) Result {
	t.Helper()
	def, _ := Lookup(name)
	return def.Build(ctxFor(name, in, fresh))
}

// Fresh carry no zero here, unlike cost and lines: transcript prove no tokens
// sent, not no time elapsed. total_duration_ms is wall clock, and session may
// idle minutes before first call -- "0s" there is claim probe cannot back.
func TestSessionThreeStates(t *testing.T) {
	ms := int64(4_500_000)
	known := &schema.Input{Cost: &schema.Cost{TotalDurationMS: &ms}}

	for _, tc := range []struct {
		name  string
		in    *schema.Input
		fresh bool
		want  string
	}{
		{"known", known, false, "1h15m"},
		{"fresh", &schema.Input{}, true, config.DefaultUnknown},
		{"unknown", &schema.Input{}, false, config.DefaultUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := buildNamed(t, "session", tc.in, tc.fresh)
			if res.Empty {
				t.Fatal("session returned empty")
			}
			if got := res.Fields["duration"].Text; got != tc.want {
				t.Fatalf("duration = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCostThreeStates(t *testing.T) {
	usd := 1.23
	known := &schema.Input{Cost: &schema.Cost{TotalCostUSD: &usd}}

	for _, tc := range []struct {
		name  string
		in    *schema.Input
		fresh bool
		want  string
	}{
		{"known", known, false, "1.23"},
		{"fresh", &schema.Input{}, true, "0.00"},
		{"unknown", &schema.Input{}, false, config.DefaultUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := buildNamed(t, "cost", tc.in, tc.fresh)
			if res.Empty {
				t.Fatal("cost returned empty")
			}
			if got := res.Fields["usd"].Text; got != tc.want {
				t.Fatalf("usd = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCostFreshApiDurationIsZero(t *testing.T) {
	res := buildNamed(t, "cost", &schema.Input{}, true)
	if got := res.Fields["api_duration"].Text; got != "0s" {
		t.Fatalf("api_duration = %q, want %q", got, "0s")
	}
}

// api_duration resolve off its own pointer, same as {size} in contextNoUsage.
// total_cost_usd absent must never blank or zero a real api duration payload
// carry -- neither placeholder nor fresh-zero may win over it.
func TestCostAPIDurationIndependentOfUSD(t *testing.T) {
	ms := int64(233_000)
	apiOnly := &schema.Input{Cost: &schema.Cost{TotalAPIDurationMS: &ms}}

	for _, tc := range []struct {
		name  string
		fresh bool
	}{
		{"live", false},
		{"fresh", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := buildNamed(t, "cost", apiOnly, tc.fresh)
			if res.Empty {
				t.Fatal("cost returned empty")
			}
			if got := res.Fields["api_duration"].Text; got != "3m" {
				t.Fatalf("api_duration = %q, want %q", got, "3m")
			}
		})
	}
}

// {usd} unresolved and slot not held must drop whole segment. Building
// Fields with api_duration alone would leave default template "${usd}"
// render bare "$" -- worse than segment absent.
func TestCostOptedOutDropsWithOnlyAPIDuration(t *testing.T) {
	ms := int64(233_000)
	def, _ := Lookup("cost")
	c := ctxFor("cost", &schema.Input{Cost: &schema.Cost{TotalAPIDurationMS: &ms}}, false)
	c.Cfg.Unknown = ""
	if !def.Build(c).Empty {
		t.Fatal("api_duration alone with unknown = \"\" did not drop the segment")
	}
}

func TestLinesThreeStates(t *testing.T) {
	added, removed := int64(156), int64(23)
	known := &schema.Input{Cost: &schema.Cost{TotalLinesAdded: &added, TotalLinesRemoved: &removed}}

	for _, tc := range []struct {
		name        string
		in          *schema.Input
		fresh       bool
		add, remove string
	}{
		{"known", known, false, "156", "23"},
		{"fresh", &schema.Input{}, true, "0", "0"},
		{"unknown", &schema.Input{}, false, config.DefaultUnknown, config.DefaultUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := buildNamed(t, "lines", tc.in, tc.fresh)
			if res.Empty {
				t.Fatal("lines returned empty")
			}
			if got := res.Fields["added"].Text; got != tc.add {
				t.Fatalf("added = %q, want %q", got, tc.add)
			}
			if got := res.Fields["removed"].Text; got != tc.remove {
				t.Fatalf("removed = %q, want %q", got, tc.remove)
			}
		})
	}
}

// Each counter resolve off its own pointer, same as api_duration. Dragging
// known 156 down to placeholder because its partner is nil hide number payload
// carried.
func TestLinesResolveCountersIndependently(t *testing.T) {
	added := int64(156)
	in := &schema.Input{Cost: &schema.Cost{TotalLinesAdded: &added}}

	live := buildNamed(t, "lines", in, false)
	if got := live.Fields["added"].Text; got != "156" {
		t.Fatalf("live added = %q, want %q", got, "156")
	}
	if got := live.Fields["removed"].Text; got != config.DefaultUnknown {
		t.Fatalf("live removed = %q, want %q", got, config.DefaultUnknown)
	}

	fresh := buildNamed(t, "lines", in, true)
	if got := fresh.Fields["added"].Text; got != "156" {
		t.Fatalf("fresh added = %q, want %q", got, "156")
	}
	if got := fresh.Fields["removed"].Text; got != "0" {
		t.Fatalf("fresh removed = %q, want %q", got, "0")
	}
}

// Opted-out slot drop rather than render "+156 -" off half a payload.
func TestLinesOptedOutDropsOnHalfAPayload(t *testing.T) {
	added := int64(156)
	def, _ := Lookup("lines")
	c := ctxFor("lines", &schema.Input{Cost: &schema.Cost{TotalLinesAdded: &added}}, false)
	c.Cfg.Unknown = ""
	if !def.Build(c).Empty {
		t.Fatal("one counter with unknown = \"\" did not drop the segment")
	}
}

// Percentage arrive without context_window_size, and known branch used to omit
// both field entirely -- template "{used}/{size}" then render bare "/", exact
// shape collapse stable slot exist to stop.
func TestContextKnownPctWithoutSizeHoldsBothFields(t *testing.T) {
	in := &schema.Input{Context: &schema.ContextWin{UsedPercentage: ptr(42.0)}}
	c := ctxFor("context", in, false)
	c.Cfg.Template = "{used}/{size}"
	if got := draw(c); got != config.DefaultUnknown+"/"+config.DefaultUnknown {
		t.Fatalf("got %q, want %q", got, config.DefaultUnknown+"/"+config.DefaultUnknown)
	}
}

// current_usage carry counted tokens whether or not window size arrive, so
// {used} stay real number there.
func TestContextUsedComesFromCurrentUsageWithoutSize(t *testing.T) {
	in := &schema.Input{Context: &schema.ContextWin{
		UsedPercentage: ptr(42.0),
		CurrentUsage:   &schema.Usage{InputTokens: 1000, CacheReadTokens: 83_000},
	}}
	res := buildContext(ctxFor("context", in, false))
	if got := res.Fields["used"].Text; got != "84k" {
		t.Fatalf("used = %q, want %q", got, "84k")
	}
	if got := res.Fields["size"].Text; got != config.DefaultUnknown {
		t.Fatalf("size = %q, want %q", got, config.DefaultUnknown)
	}
}

func TestOptedOutDropsSessionCostLines(t *testing.T) {
	for _, name := range []string{"session", "cost", "lines"} {
		t.Run(name, func(t *testing.T) {
			def, _ := Lookup(name)
			c := ctxFor(name, &schema.Input{}, true)
			c.Cfg.Unknown = ""
			if !def.Build(c).Empty {
				t.Fatal("unknown = \"\" did not drop the segment")
			}
		})
	}
}

// Account-wide window survive across sessions, so a new session may open at 80%
// used. Zero there invite spending a window that is nearly gone.
func TestLimitNeverRendersZeroWhenFresh(t *testing.T) {
	for _, name := range []string{"limit.5h", "limit.7d"} {
		t.Run(name, func(t *testing.T) {
			res := buildNamed(t, name, &schema.Input{}, true)
			if res.Empty {
				t.Fatal("limit returned empty")
			}
			if got := res.Fields["pct"].Text; got != config.DefaultUnknown {
				t.Fatalf("pct = %q, want %q", got, config.DefaultUnknown)
			}
		})
	}
}

func TestLimitUnknownRendersEmptyBarAndNoReset(t *testing.T) {
	res := buildNamed(t, "limit.5h", &schema.Input{}, false)
	if got := res.Fields["bar"].Text; got != "○○○○○○○○○○" {
		t.Fatalf("bar = %q, want ten empty cells", got)
	}
	if got := res.Fields["reset"].Text; got != "" {
		t.Fatalf("reset = %q, want empty", got)
	}
	if got := res.Fields["reset_time"].Text; got != "" {
		t.Fatalf("reset_time = %q, want empty", got)
	}
}

// Window present with used_percentage null is shape payload send. Non-pointer
// field decode it to 0, printing "0%" for account that may sit at 80% used.
// Parse from JSON rather than struct literal: struct tag is what break.
func TestLimitNullPercentageRendersPlaceholder(t *testing.T) {
	in := input(t, []byte(
		`{"rate_limits":{"five_hour":{"used_percentage":null,"resets_at":1753257600}}}`))
	res := buildNamed(t, "limit.5h", in, false)
	if res.Empty {
		t.Fatal("limit returned empty")
	}
	if got := res.Fields["pct"].Text; got != config.DefaultUnknown {
		t.Fatalf("pct = %q, want %q", got, config.DefaultUnknown)
	}
	// Reset time is known, but percentage is not: template pairing them must
	// not read "0% until 5pm".
	if got := res.Fields["reset_time"].Text; got != "" {
		t.Fatalf("reset_time = %q, want empty beside unknown percentage", got)
	}
}

// Reported 0 is measurement, not absence. Pointer field open path where real
// zero drown in placeholder, so exact "0%" render is pinned. Parse from JSON:
// struct literal cannot regress decode path.
func TestLimitReportedZeroRendersZero(t *testing.T) {
	in := input(t, []byte(
		`{"rate_limits":{"five_hour":{"used_percentage":0,"resets_at":1753257600}}}`))
	res := buildNamed(t, "limit.5h", in, false)
	if res.Empty {
		t.Fatal("limit returned empty")
	}
	if got := res.Fields["pct"].Text; got != "0" {
		t.Fatalf("pct = %q, want %q", got, "0")
	}
	// Known zero take known path whole: reset time render beside it, unlike
	// null percentage where it stay blank.
	if got := res.Fields["reset_time"].Text; got == "" {
		t.Fatal("reset_time empty beside a reported zero")
	}
}

// One window absent while other report: only absent one placeholder.
func TestLimitWindowsResolveIndependently(t *testing.T) {
	in := &schema.Input{RateLimits: &schema.RateLimits{
		FiveHour: &schema.Window{UsedPercentage: ptr(42.0)},
	}}
	if got := buildNamed(t, "limit.5h", in, false).Fields["pct"].Text; got != "42" {
		t.Fatalf("limit.5h pct = %q, want %q", got, "42")
	}
	if got := buildNamed(t, "limit.7d", in, false).Fields["pct"].Text; got != config.DefaultUnknown {
		t.Fatalf("limit.7d pct = %q, want %q", got, config.DefaultUnknown)
	}
}

func TestLimitOptedOutDrops(t *testing.T) {
	def, _ := Lookup("limit.5h")
	c := ctxFor("limit.5h", &schema.Input{}, false)
	c.Cfg.Unknown = ""
	if !def.Build(c).Empty {
		t.Fatal("unknown = \"\" did not drop the segment")
	}
}

// Every segment design name stable, no other. Last registration land in this
// task, so whole set is pinned here.
func TestStableSegmentSet(t *testing.T) {
	want := map[string]bool{
		"context": true, "session": true, "cost": true, "lines": true,
		"tokens": true, "limit.5h": true, "limit.7d": true,
	}
	for _, name := range Names() {
		def, _ := Lookup(name)
		if def.Stable != want[name] {
			t.Errorf("%s Stable = %v, want %v", name, def.Stable, want[name])
		}
	}
}

func TestSessionIconIsWhiteField(t *testing.T) {
	f := Build(ctx(t, fixtures.Full, "session")).Fields["icon"]
	if f.Text != sessionIcon {
		t.Errorf("session {icon} text = %q, want %q", f.Text, sessionIcon)
	}
	if f.Color != render.White {
		t.Errorf("session {icon} color = %q, want White %q", f.Color, render.White)
	}
}

// Unknown branch is where slot width matter, so pin icon on that path too.
func TestSessionIconSurvivesUnknownDuration(t *testing.T) {
	c := ctx(t, fixtures.Unknown, "session")
	res := Build(c)
	if res.Empty {
		t.Fatal("session dropped its slot on unknown duration")
	}
	if got := res.Fields["duration"].Text; got != config.DefaultUnknown {
		t.Fatalf("fixture no longer exercise unknown path: {duration} = %q", got)
	}
	if got := res.Fields["icon"].Text; got != sessionIcon {
		t.Errorf("session {icon} = %q, want %q", got, sessionIcon)
	}
}

func TestSessionIconEscapesDim(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	c := ctx(t, fixtures.Full, "session")
	c.Palette = render.NewPalette()
	out := draw(c)
	if strings.Contains(out, "\033[2m"+sessionIcon) {
		t.Errorf("session icon still under dim: %q", out)
	}
	if !strings.Contains(out, string(render.White)+sessionIcon) {
		t.Errorf("session icon not wrapped in White: %q", out)
	}
}
