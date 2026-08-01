package schema

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

func load(t *testing.T, b []byte) *Input {
	t.Helper()
	in, err := Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return in
}

// Every fixture decode without error, empty document included. A status line
// that panic on unexpected input leave user a blank row and no way to tell what
// went wrong.
func TestParseFixtures(t *testing.T) {
	for name, b := range map[string][]byte{
		"full": fixtures.Full, "sparse": fixtures.Sparse, "empty": fixtures.Empty,
	} {
		if in := load(t, b); in == nil {
			t.Fatalf("%s decoded to nil", name)
		}
	}
}

func TestFullFixtureFields(t *testing.T) {
	in := load(t, fixtures.Full)

	if in.Model.DisplayName != "Opus" {
		t.Errorf("model = %q, want Opus", in.Model.DisplayName)
	}
	if in.Dir() != "/home/dev/project/acme" {
		t.Errorf("Dir() = %q", in.Dir())
	}
	if in.RateLimits == nil || in.RateLimits.FiveHour == nil {
		t.Fatal("five_hour rate limit missing")
	}
	if got := in.RateLimits.FiveHour.UsedPercentage; got == nil || *got != 42.4 {
		t.Errorf("five_hour used = %v, want 42.4", got)
	}
	if in.Effort == nil || in.Effort.Level != "high" {
		t.Errorf("effort = %+v", in.Effort)
	}

	pct, ok := in.ContextPercent()
	if !ok || pct != 42 {
		t.Errorf("ContextPercent() = %v, %v; want 42, true", pct, ok)
	}
}

// Sparse fixture stand in for a non-subscriber on first render. Each assertion
// match one absence Claude Code docs call out, and each is a case reference bash
// implementation mishandle.
func TestSparseFixtureAbsences(t *testing.T) {
	in := load(t, fixtures.Sparse)

	if in.RateLimits != nil {
		t.Error("rate_limits should be absent for non-subscribers")
	}
	if in.Effort != nil {
		t.Error("effort should be absent when the model does not support it")
	}
	if in.Workspace.Repo != nil {
		t.Error("repo should be absent outside a configured origin remote")
	}
	if in.Vim != nil || in.SessionName != nil {
		t.Error("vim and session_name should be absent")
	}
	if in.Context == nil || in.Context.CurrentUsage != nil {
		t.Error("current_usage should be null before the first API call")
	}

	// Key distinction: percentage unknown, not zero. 0% here claim empty context
	// where truth is unreported one.
	if _, ok := in.ContextPercent(); ok {
		t.Error("ContextPercent() should report unknown when both sources are null")
	}
}

// Unknown fixture exist for absences sparse cannot carry: cost and
// context_window both gone, so no stable segment find a known value and each
// reach its placeholder. Regaining either block silently hide three of seven.
func TestUnknownFixtureAbsences(t *testing.T) {
	in := load(t, fixtures.Unknown)

	if in.Cost != nil {
		t.Error("cost should be absent, else session, cost and lines take known path")
	}
	if in.Context != nil {
		t.Error("context_window should be absent")
	}
	if in.RateLimits != nil {
		t.Error("rate_limits should be absent")
	}
	if _, ok := in.ContextPercent(); ok {
		t.Error("ContextPercent() should report unknown")
	}
}

func TestEmptyDocumentIsSafe(t *testing.T) {
	in := load(t, fixtures.Empty)

	if in.Model.DisplayName != "" || in.Dir() != "" {
		t.Errorf("empty document should yield zero values, got %+v", in)
	}
	if _, ok := in.ContextPercent(); ok {
		t.Error("ContextPercent() should be unknown for an empty document")
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Error("Parse should return an error for malformed JSON")
	}
}

// Fixtures are only written spec of this contract. Strict decode fail as soon
// as one carry key no struct field map, so ported field cannot go missing in
// silence.
func TestFixturesHaveNoUnmappedFields(t *testing.T) {
	for name, b := range map[string][]byte{
		"full": fixtures.Full, "sparse": fixtures.Sparse, "empty": fixtures.Empty,
	} {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		var in Input
		if err := dec.Decode(&in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Empty fixture leave both dir sources blank, so it never separate branches.
func TestDirFallsBackToCWD(t *testing.T) {
	in := load(t, []byte(`{"cwd":"/home/dev/project/acme"}`))
	if in.Dir() != "/home/dev/project/acme" {
		t.Errorf("Dir() = %q, want /home/dev/project/acme", in.Dir())
	}
}

// used_percentage null with current_usage populated is how every session start,
// and hold only arithmetic in this package. No fixture reach it: full short
// circuit on used_percentage, sparse and empty stop at nil guard.
func TestContextPercentDerived(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want float64
		ok   bool
	}{
		{
			// 8500 + 5000 + 70700 = 84200 of 200000. output_tokens stay out.
			name: "input plus cache tokens over window size",
			doc:  `{"context_window":{"context_window_size":200000,"used_percentage":null,"current_usage":{"input_tokens":8500,"output_tokens":1200,"cache_creation_input_tokens":5000,"cache_read_input_tokens":70700}}}`,
			want: 42.1,
			ok:   true,
		},
		{
			name: "usage past window size clamp at 100",
			doc:  `{"context_window":{"context_window_size":200000,"current_usage":{"input_tokens":300000}}}`,
			want: 100,
			ok:   true,
		},
		{
			name: "used_percentage past 100 clamp too",
			doc:  `{"context_window":{"context_window_size":200000,"used_percentage":142}}`,
			want: 100,
			ok:   true,
		},
		{
			name: "used_percentage win over current_usage",
			doc:  `{"context_window":{"context_window_size":200000,"used_percentage":10,"current_usage":{"input_tokens":84200}}}`,
			want: 10,
			ok:   true,
		},
		{
			// Divide by zero yield +Inf, printed straight into row.
			name: "window size zero is unknown",
			doc:  `{"context_window":{"context_window_size":0,"current_usage":{"input_tokens":8500}}}`,
		},
		{
			name: "window size absent is unknown",
			doc:  `{"context_window":{"current_usage":{"input_tokens":8500}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := load(t, []byte(tc.doc)).ContextPercent()
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			diff := got - tc.want
			if math.Abs(diff) > 1e-9 {
				t.Errorf("pct = %v, want %v", got, tc.want)
			}
		})
	}
}

// encoding/json reject bare NaN, so only hand-built Input reach this.
// Unknown, not zero: NaN carry no honest percentage to print.
func TestContextPercentUnknownOnNaN(t *testing.T) {
	nan := math.NaN()
	in := &Input{Context: &ContextWin{UsedPercentage: &nan}}

	if got, ok := in.ContextPercent(); ok {
		t.Errorf("pct = %v, ok = true; want ok = false", got)
	}
}

// Future Claude Code release add fields. Decode ignore them, never fail, else
// upgrade silently blank status line.
func TestUnknownFieldsIgnored(t *testing.T) {
	in, err := Parse([]byte(`{"model":{"display_name":"Opus"},"brand_new_field":{"nested":1}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Model.DisplayName != "Opus" {
		t.Errorf("model = %q", in.Model.DisplayName)
	}
}
