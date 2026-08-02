package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// skillsCtx point skills segment at throwaway transcript holding lines.
func skillsCtx(t *testing.T, lines ...string) Context {
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

	c := ctx(t, fixtures.Full, "skills")
	c.In.TranscriptPath = path
	c.CacheDir = filepath.Join(dir, "cache")
	return c
}

func listingLine(count int) string {
	return fmt.Sprintf(
		`{"type":"user","attachment":{"type":"skill_listing","content":"# Skills","skillCount":%d,"isInitial":true,"names":["deploy"]}}`,
		count)
}

func skillLine(msgID, skill string) string {
	return fmt.Sprintf(
		`{"type":"assistant","message":{"id":%q,"model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_01","name":"Skill","input":{"skill":%q}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}`,
		msgID, skill)
}

func TestBuildSkillsRendersCount(t *testing.T) {
	if got := draw(skillsCtx(t, listingLine(40))); got != "💡 40" {
		t.Errorf("got %q, want \"💡 40\"", got)
	}
}

// Absent listing is unknown, and unknown drop rather than print a 0 nobody
// proved. Segment appear at most once per session, so nothing vanish.
func TestBuildSkillsDropsWithoutListing(t *testing.T) {
	if got := draw(skillsCtx(t, assistantLine("msg_a", 100, 10, 1000, 5))); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBuildSkillsDropsWithoutTranscript(t *testing.T) {
	c := ctx(t, fixtures.Full, "skills")
	c.In.TranscriptPath = ""
	if got := draw(c); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBuildSkillsLastField(t *testing.T) {
	c := skillsCtx(t, listingLine(40), skillLine("msg_a", "superpowers:brainstorming"))
	c.Cfg.Template = "{icon} {count} {last}"
	if got := draw(c); got != "💡 40 superpowers:brainstorming" {
		t.Errorf("got %q", got)
	}
}

// Template naming {last} before any invocation must render rest, same as
// caveman's {savings} when /caveman-stats never ran.
func TestBuildSkillsLastEmptyBeforeAnyInvocation(t *testing.T) {
	c := skillsCtx(t, listingLine(40))
	c.Cfg.Template = "{icon} {count}{last}"
	if got := draw(c); got != "💡 40" {
		t.Errorf("got %q, want \"💡 40\"", got)
	}
}
