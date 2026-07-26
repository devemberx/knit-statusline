package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// isolate point homeDir() at a scratch directory, so no test read or write real
// ~/.claude. USERPROFILE set too: os.UserHomeDir read that one on Windows.
// Shared with commands_test.go.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeUserConfig lay down ~/.claude/statusline.toml inside isolated home.
// Shared with commands_test.go.
func writeUserConfig(t *testing.T, home, body string) {
	t.Helper()
	path := config.UserPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func drawStdin(t *testing.T, doc []byte) string {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	renderFromStdin(bytes.NewReader(doc), &out)
	return out.String()
}

// Hot path contract: never panic, never print nothing. Claude Code print
// whatever reach stdout, so a crash show as empty row explaining nothing.
func TestRenderSurvivesHostileInput(t *testing.T) {
	isolate(t)
	for _, doc := range []string{
		"", "{}", "null", "[]", "not json", `{"model":`,
		`{"context_window":{"current_usage":null}}`,
		`{"model":{"display_name":"Opus"},"cost":null,"rate_limits":null}`,
		`{"context_window":{"context_window_size":0,"current_usage":{"input_tokens":5}}}`,
	} {
		got := drawStdin(t, []byte(doc))
		if strings.TrimSpace(got) == "" {
			t.Errorf("input %q rendered nothing", doc)
		}
		if !strings.HasSuffix(got, "\n") {
			t.Errorf("input %q rendered without a trailing newline: %q", doc, got)
		}
	}
}

// Unreadable or empty stdin still name something. "Claude" is last identity
// left when no document arrived at all.
func TestRenderFallsBackWhenNothingParses(t *testing.T) {
	isolate(t)
	for _, doc := range []string{"", "not json"} {
		if got := drawStdin(t, []byte(doc)); strings.TrimSpace(got) != "Claude" {
			t.Errorf("input %q gave %q, want Claude", doc, got)
		}
	}
}

// Valid document with nothing populated: model name is whatever identity remain.
func TestRenderFallsBackToModelName(t *testing.T) {
	isolate(t)
	got := drawStdin(t, []byte(`{"model":{"display_name":"Opus","id":"claude-opus-4-8"}}`))
	if strings.TrimSpace(got) != "Opus 4.8" {
		t.Errorf("got %q, want Opus 4.8", got)
	}
}

func TestRenderDrawsTheDefaultPreset(t *testing.T) {
	isolate(t)
	got := drawStdin(t, fixtures.Full)
	for _, want := range []string{"Opus 4.8", "42%", "acme", "current", "weekly"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered row missing %q:\n%s", want, got)
		}
	}
}

// Segment name this build lack cost only its own slot, so unmarked it vanish and
// user get no hint which file to open.
func TestRenderMarksAnUnknownSegment(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\", \"no-such-segment\"]\n")

	got := drawStdin(t, fixtures.Full)
	if !strings.Contains(got, "⚠ statusline.toml") {
		t.Errorf("unknown segment went unmarked:\n%s", got)
	}
	if !strings.Contains(got, "Opus") {
		t.Errorf("marker cost the row its content:\n%s", got)
	}
}

// Template field that does not exist render blank, so it need marking too.
func TestRenderMarksAnUnknownTemplateField(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\"]\n\n[segments.model]\ntemplate = \"{nope}\"\n")

	if got := drawStdin(t, fixtures.Full); !strings.Contains(got, "⚠ statusline.toml") {
		t.Errorf("unknown field went unmarked:\n%s", got)
	}
}

// Broken TOML drop its layer and mark file, never blank row.
func TestRenderMarksABrokenConfig(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "this is not toml\n")

	got := drawStdin(t, fixtures.Full)
	if !strings.Contains(got, "⚠ statusline.toml") {
		t.Errorf("broken config went unmarked:\n%s", got)
	}
	if !strings.Contains(got, "Opus") {
		t.Errorf("builtin preset should still draw:\n%s", got)
	}
}

// Good config draw clean. Marker on every render would train user to ignore it.
func TestRenderLeavesAGoodConfigUnmarked(t *testing.T) {
	home := isolate(t)
	writeUserConfig(t, home, "[[lines]]\nsegments = [\"model\", \"version\"]\n")

	if got := drawStdin(t, fixtures.Full); strings.Contains(got, "⚠") {
		t.Errorf("clean config was marked:\n%s", got)
	}
}

// Project override apply where session launched, not where cwd wandered to.
func TestProjectDirPrefersProjectOverCurrent(t *testing.T) {
	in := parse(t, `{"cwd":"/tmp/elsewhere","workspace":{"current_dir":"/tmp/current","project_dir":"/tmp/project"}}`)
	if got := projectDir(in); got != "/tmp/project" {
		t.Errorf("got %q, want /tmp/project", got)
	}

	in = parse(t, `{"cwd":"/tmp/elsewhere","workspace":{"current_dir":"/tmp/current"}}`)
	if got := projectDir(in); got != "/tmp/current" {
		t.Errorf("got %q, want /tmp/current", got)
	}

	in = parse(t, `{"cwd":"/tmp/elsewhere"}`)
	if got := projectDir(in); got != "/tmp/elsewhere" {
		t.Errorf("got %q, want /tmp/elsewhere", got)
	}
}

func TestDispatchVersionAndHelp(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var out bytes.Buffer
		if code := dispatch(args, &out, &out); code != 0 {
			t.Errorf("%v exited %d", args, code)
		}
		if !strings.Contains(out.String(), "knit-statusline") {
			t.Errorf("%v printed %q", args, out.String())
		}
	}

	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		var out bytes.Buffer
		if code := dispatch(args, &out, &out); code != 0 {
			t.Errorf("%v exited %d", args, code)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%v printed no usage: %q", args, out.String())
		}
	}
}

// Unknown subcommand is user error, so exit 2 and usage on stderr. Render path
// never reach here -- it take no arguments at all.
func TestDispatchRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := dispatch([]string{"frobnicate"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should stay clean: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// Usage list every preset that exist, so --preset never name one this build lack.
func TestUsageListsEveryPreset(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	for _, name := range config.PresetNames() {
		if !strings.Contains(out.String(), name) {
			t.Errorf("usage omits preset %q:\n%s", name, out.String())
		}
	}
}
