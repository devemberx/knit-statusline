package schema

import (
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
// that panic on unexpected input leave the user a blank row and no way to tell
// what went wrong.
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
	if got := in.RateLimits.FiveHour.UsedPercentage; got != 42.4 {
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

// Sparse fixture stand in for a non-subscriber on the first render. Each
// assertion match an absence the Claude Code docs call out, and each one is a
// case the reference bash implementation mishandle.
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

	// Key distinction: percentage unknown, not zero. 0% here claim an empty
	// context where the truth is an unreported one.
	if _, ok := in.ContextPercent(); ok {
		t.Error("ContextPercent() should report unknown when both sources are null")
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

// A future Claude Code release add fields. Decode ignore them, never fail, else
// an upgrade silently blank the status line.
func TestUnknownFieldsIgnored(t *testing.T) {
	in, err := Parse([]byte(`{"model":{"display_name":"Opus"},"brand_new_field":{"nested":1}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Model.DisplayName != "Opus" {
		t.Errorf("model = %q", in.Model.DisplayName)
	}
}
