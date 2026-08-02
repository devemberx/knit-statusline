package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Line shaped like ones Claude Code write: one per content block, each repeating
// a full usage object for its message. Shared with cursor_test.go.
func assistantLine(msgID, model string, in, cw, cr, out int64) string {
	return fmt.Sprintf(
		`{"type":"assistant","isSidechain":false,"uuid":"u-%s","message":{"id":%q,"model":%q,"role":"assistant","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		msgID, msgID, model, in, cw, cr, out)
}

// Shared with cursor_test.go.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func scanOnce(t *testing.T, path string, cache *Cache) (Summary, *Cache) {
	t.Helper()
	return Scan(Options{TranscriptPath: path, Scope: ScopeSession}, cache)
}

// Defining behaviour of this package: repeated lines for one message count once.
// Undeduplicated, these three lines report 300 input tokens.
func TestDedupesRepeatedMessageLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_b", "claude-opus-4-8", 200, 20, 2000, 7),
	})

	got, _ := scanOnce(t, path, nil)
	want := Totals{Input: 300, CacheWrite: 30, CacheRead: 3000, Output: 12}
	if got.Totals != want {
		t.Errorf("got %+v, want %+v", got.Totals, want)
	}
}

// Failure mode that motivated persisting LastMessageID: repeated lines of one
// message straddle two scans, so a second scan must still recognise which id it
// already counted. Wrong here, totals depend on render timing and one session
// report different numbers each redraw.
func TestDedupesAcrossIncrementalBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
	})

	first, cache := scanOnce(t, path, nil)
	if first.Totals.Input != 100 {
		t.Fatalf("first scan input = %d, want 100", first.Totals.Input)
	}

	// Remaining blocks of that same message arrive after first scan.
	appendLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_b", "claude-opus-4-8", 200, 20, 2000, 7),
	})

	second, _ := scanOnce(t, path, cache)
	want := Totals{Input: 300, CacheWrite: 30, CacheRead: 3000, Output: 12}
	if second.Totals != want {
		t.Errorf("got %+v, want %+v", second.Totals, want)
	}

	// Full scan from scratch agree with incremental result.
	fresh, _ := scanOnce(t, path, nil)
	if fresh.Totals != second.Totals {
		t.Errorf("incremental %+v disagrees with full rescan %+v", second.Totals, fresh.Totals)
	}
}

// Entry carrying usage but no message.id must not overwrite dedup guard.
// Overwritten, guard hold "" and repeat below count twice: 201, not 101.
func TestIDLessEntryKeepsDedupGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 0),
		`{"type":"assistant","message":{"model":"claude-opus-4-8","usage":{"input_tokens":1}}}`,
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 0),
	})

	got, _ := scanOnce(t, path, nil)
	if got.Totals.Input != 101 {
		t.Errorf("input = %d, want 101 (id-less entry disarmed dedup)", got.Totals.Input)
	}
}

func TestIncrementalAppendDoesNotRecount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5)})

	_, cache := scanOnce(t, path, nil)
	offsetAfterFirst := cache.Files[path].Offset

	appendLines(t, path, []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7)})
	got, cache := scanOnce(t, path, cache)

	if got.Totals.Input != 300 || got.Totals.Output != 12 {
		t.Errorf("got %+v, want input 300 output 12", got.Totals)
	}
	if cache.Files[path].Offset <= offsetAfterFirst {
		t.Error("cursor did not advance past the appended bytes")
	}
}

// Shrunk transcript got replaced, not appended to. Cursor untrustworthy, so read
// it again from its start.
func TestTruncationTriggersRescan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5),
		assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7),
	})
	_, cache := scanOnce(t, path, nil)

	// Reuse msg_b, id pre-truncation cursor already saw as LastMessageID:
	// proves reset clears that id, not only Totals. Left stale, this line
	// reads as duplicate and its tokens go uncounted.
	writeLines(t, path, []string{assistantLine("msg_b", "claude-opus-4-8", 50, 0, 0, 1)})

	got, _ := scanOnce(t, path, cache)
	if got.Totals.Input != 50 || got.Totals.Output != 1 {
		t.Errorf("got %+v, want input 50 output 1", got.Totals)
	}
}

// Claude Code may be mid-write when a status line run. Leave that fragment
// unconsumed: counted once complete, never twice.
func TestPartialTrailingLineIsDeferred(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	complete := assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5)
	pending := assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7)

	if err := os.WriteFile(path, []byte(complete+"\n"+pending[:40]), 0o644); err != nil {
		t.Fatal(err)
	}

	got, cache := scanOnce(t, path, nil)
	if got.Totals.Input != 100 {
		t.Fatalf("partial line was counted: %+v", got.Totals)
	}

	// Finish interrupted line.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(pending[40:] + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, _ = scanOnce(t, path, cache)
	if got.Totals.Input != 300 || got.Totals.Output != 12 {
		t.Errorf("got %+v, want input 300 output 12", got.Totals)
	}
}

func TestSkipsSyntheticAndNonAssistantLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		`{"type":"queue-operation","operation":"enqueue"}`,
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"file-history-snapshot","snapshot":{}}`,
		assistantLine("msg_syn", syntheticModel, 999, 999, 999, 999),
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		`{"type":"assistant","message":{"id":"msg_nousage","model":"claude-opus-4-8"}}`,
	})

	got, _ := scanOnce(t, path, nil)
	want := Totals{Input: 100, CacheWrite: 10, CacheRead: 1000, Output: 5}
	if got.Totals != want {
		t.Errorf("got %+v, want %+v", got.Totals, want)
	}
}

// Malformed lines skipped in silence. Corrupt transcript degrade a number, never
// blank its status line.
func TestCorruptLinesAreSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		`{"usage": broken json here`,
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5),
		`{"usage":`,
	})

	got, _ := scanOnce(t, path, nil)
	if got.Totals.Input != 100 {
		t.Errorf("got %+v, want input 100", got.Totals)
	}
}

func TestMissingTranscriptYieldsZero(t *testing.T) {
	got, cache := scanOnce(t, filepath.Join(t.TempDir(), "absent.jsonl"), nil)
	if got.Totals != (Totals{}) {
		t.Errorf("got %+v, want zero totals", got.Totals)
	}
	if cache == nil {
		t.Error("cache should never be nil")
	}
}

// Claude Code omit transcript_path on some payloads. Empty path mean no
// transcript, never current directory.
func TestEmptyTranscriptPathYieldsZero(t *testing.T) {
	got, cache := Scan(Options{Scope: ScopeSession}, nil)
	if got.Totals != (Totals{}) {
		t.Errorf("got %+v, want zero totals", got.Totals)
	}
	if cache == nil {
		t.Fatal("cache should never be nil")
	}
	if len(cache.Files) != 0 {
		t.Errorf("cursors recorded for no transcript: %+v", cache.Files)
	}
}

// Subagent transcripts sit in separate agent-*.jsonl files, so sidechain
// inclusion come down to file selection.
func TestProjectScopeSidechainSelection(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "session.jsonl")
	writeLines(t, main, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5)})
	writeLines(t, filepath.Join(dir, "other.jsonl"), []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7)})
	writeLines(t, filepath.Join(dir, "agent-x.jsonl"), []string{assistantLine("msg_c", "claude-opus-4-8", 400, 0, 0, 9)})

	session, _ := Scan(Options{TranscriptPath: main, Scope: ScopeSession}, nil)
	if session.Totals.Input != 100 {
		t.Errorf("session scope input = %d, want 100", session.Totals.Input)
	}

	project, _ := Scan(Options{TranscriptPath: main, Scope: ScopeProject}, nil)
	if project.Totals.Input != 300 {
		t.Errorf("project scope input = %d, want 300 (agent file excluded)", project.Totals.Input)
	}

	withAgents, _ := Scan(Options{TranscriptPath: main, Scope: ScopeProject, IncludeSidechain: true}, nil)
	if withAgents.Totals.Input != 700 {
		t.Errorf("project scope with sidechain input = %d, want 700", withAgents.Totals.Input)
	}
}

// Deleted transcript lose its cursor. Retained, cursors for rotated-away files
// pile up in a cache that only ever grow.
func TestVanishedFileEvictsItsCursor(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "session.jsonl")
	gone := filepath.Join(dir, "gone.jsonl")
	writeLines(t, main, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 0)})
	writeLines(t, gone, []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 0)})

	opts := Options{TranscriptPath: main, Scope: ScopeProject}
	if first, _ := Scan(opts, nil); first.Totals.Input != 300 {
		t.Fatalf("first scan input = %d, want 300", first.Totals.Input)
	}

	_, cache := Scan(opts, nil)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	got, cache := Scan(opts, cache)
	if got.Totals.Input != 100 {
		t.Errorf("input = %d, want 100 (deleted file still counted)", got.Totals.Input)
	}
	if _, ok := cache.Files[gone]; ok {
		t.Error("cursor for deleted file survived")
	}
}

// Unreadable-but-present file keep its last cursor. Dropped instead, one EMFILE
// or permission blip dip total for a render and force cold rescan.
// Self-referential symlink give stat error other than fs.ErrNotExist.
func TestTransientErrorKeepsLastCursor(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "session.jsonl")
	flaky := filepath.Join(dir, "flaky.jsonl")
	writeLines(t, main, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 0)})
	writeLines(t, flaky, []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 0)})

	opts := Options{TranscriptPath: main, Scope: ScopeProject}
	_, cache := Scan(opts, nil)

	if err := os.Remove(flaky); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("flaky.jsonl", flaky); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := os.Stat(flaky); err == nil {
		t.Skip("self-symlink did not produce a stat error")
	}

	got, cache := Scan(opts, cache)
	if got.Totals.Input != 300 {
		t.Errorf("input = %d, want 300 (unreadable file dropped its totals)", got.Totals.Input)
	}
	if cache.Files[flaky].Totals.Input != 200 {
		t.Errorf("cursor for unreadable file lost: %+v", cache.Files[flaky])
	}
}

// IncludeSidechain stay out of CacheKey, so flipping it reuse one cache file.
// Eviction carry that: agent cursors drop when scope stop covering them and
// rescan cold when it cover them again, so totals stay right either way.
func TestSidechainFlipKeepsTotalsCorrect(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "session.jsonl")
	agent := filepath.Join(dir, "agent-x.jsonl")
	writeLines(t, main, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 0)})
	writeLines(t, agent, []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 0)})

	plain := Options{TranscriptPath: main, Scope: ScopeProject}
	withAgents := plain
	withAgents.IncludeSidechain = true

	_, cache := Scan(plain, nil)
	if _, ok := cache.Files[agent]; ok {
		t.Error("agent cursor recorded while sidechain off")
	}

	got, cache := Scan(withAgents, cache)
	if got.Totals.Input != 300 {
		t.Errorf("input = %d, want 300 with sidechain on", got.Totals.Input)
	}

	got, _ = Scan(plain, cache)
	if got.Totals.Input != 100 {
		t.Errorf("input = %d, want 100 with sidechain off again", got.Totals.Input)
	}
}

// Line shaped like attachment Claude Code write once per session. Compact
// JSON, matching what land on disk.
func skillListingLine(count int) string {
	return fmt.Sprintf(
		`{"type":"user","attachment":{"type":"skill_listing","content":"# Skills","skillCount":%d,"isInitial":true,"names":["deploy"]}}`,
		count)
}

func TestSkillListingCountReachesSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		skillListingLine(40),
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
	})

	sum, _ := scanOnce(t, path, nil)
	if !sum.Skills.Known || sum.Skills.Available != 40 {
		t.Errorf("skills = %+v, want 40 known", sum.Skills)
	}
	if sum.Totals.Input != 100 {
		t.Errorf("input = %d, want 100 -- listing line must not disturb totals", sum.Totals.Input)
	}
}

// Cursor resume past listing line, so count recomputed each scan drop to
// zero on second render.
func TestSkillCountSurvivesIncrementalScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{skillListingLine(40)})

	_, cache := scanOnce(t, path, nil)
	appendLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 7, 0, 0, 1)})

	sum, _ := scanOnce(t, path, cache)
	if !sum.Skills.Known || sum.Skills.Available != 40 {
		t.Errorf("skills after append = %+v, want 40 known", sum.Skills)
	}
}

// Resumed session write a second listing. Later value win: skill installed
// mid-session is exactly why a second line exist.
func TestLaterSkillListingWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{skillListingLine(40), skillListingLine(41)})

	sum, _ := scanOnce(t, path, nil)
	if sum.Skills.Available != 41 {
		t.Errorf("available = %d, want 41", sum.Skills.Available)
	}
}

// No listing = unknown, never a zero somebody read as "no skills installed".
func TestMissingSkillListingIsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5)})

	sum, _ := scanOnce(t, path, nil)
	if sum.Skills.Known || sum.Skills.Available != 0 {
		t.Errorf("skills = %+v, want unknown", sum.Skills)
	}
}

// Count belong to one session. Project scope sum tokens across sibling files,
// and taking a sibling's listing would report a number this session never had.
func TestProjectScopeTakesSessionFileSkillCount(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "session.jsonl")
	sibling := filepath.Join(dir, "other.jsonl")
	writeLines(t, session, []string{skillListingLine(40)})
	writeLines(t, sibling, []string{skillListingLine(7)})

	sum, _ := Scan(Options{TranscriptPath: session, Scope: ScopeProject}, nil)
	if sum.Skills.Available != 40 {
		t.Errorf("available = %d, want 40", sum.Skills.Available)
	}
}

// Skill invocation as Claude Code write it: tool_use block inside assistant
// message that also carry usage.
func skillUseLine(msgID, skill string) string {
	return fmt.Sprintf(
		`{"type":"assistant","message":{"id":%q,"model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_01","name":"Skill","input":{"skill":%q}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}`,
		msgID, skill)
}

func TestLastSkillReachesSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		skillListingLine(40),
		skillUseLine("msg_a", "superpowers:brainstorming"),
	})

	sum, _ := scanOnce(t, path, nil)
	if sum.Skills.Last != "superpowers:brainstorming" {
		t.Errorf("last = %q, want superpowers:brainstorming", sum.Skills.Last)
	}
}

func TestLastSkillTakesMostRecentInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		skillUseLine("msg_a", "superpowers:brainstorming"),
		skillUseLine("msg_b", "superpowers:writing-plans"),
	})

	sum, _ := scanOnce(t, path, nil)
	if sum.Skills.Last != "superpowers:writing-plans" {
		t.Errorf("last = %q, want superpowers:writing-plans", sum.Skills.Last)
	}
}

// Invocation sit before incremental boundary. Recomputing per scan lose it.
func TestLastSkillSurvivesIncrementalScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{skillUseLine("msg_a", "deploy")})

	_, cache := scanOnce(t, path, nil)
	appendLines(t, path, []string{assistantLine("msg_b", "claude-opus-4-8", 7, 0, 0, 1)})

	sum, _ := scanOnce(t, path, cache)
	if sum.Skills.Last != "deploy" {
		t.Errorf("last after append = %q, want deploy", sum.Skills.Last)
	}
}

// Every other tool_use share block shape. Only Skill name a skill.
// Grep pattern value "Skill" trips skillUseProbe on raw bytes same as a real
// invocation would, so decode runs -- guard rejects on name, not on probe.
func TestOtherToolUseDoesNotSetLastSkill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		`{"type":"assistant","message":{"id":"msg_a","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_01","name":"Grep","input":{"pattern":"Skill"}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}`,
	})

	sum, _ := scanOnce(t, path, nil)
	if sum.Skills.Last != "" {
		t.Errorf("last = %q, want empty", sum.Skills.Last)
	}
}

// Opt-in check against a real Claude Code transcript. Real transcripts hold
// conversation content and never get committed, so this stay skipped until a
// path arrive:
//
//	KNIT_STATUSLINE_REAL_TRANSCRIPT=/path/to/session.jsonl go test ./internal/transcript
func TestRealTranscript(t *testing.T) {
	path := os.Getenv("KNIT_STATUSLINE_REAL_TRANSCRIPT")
	if path == "" {
		t.Skip("set KNIT_STATUSLINE_REAL_TRANSCRIPT to run")
	}

	for _, scope := range []Scope{ScopeSession, ScopeProject} {
		opts := Options{TranscriptPath: path, Scope: scope}

		start := time.Now()
		cold, cache := Scan(opts, nil)
		coldTook := time.Since(start)

		start = time.Now()
		warm, _ := Scan(opts, cache)
		warmTook := time.Since(start)

		t.Logf("%-7s input=%d cacheWrite=%d cacheRead=%d output=%d skills=%+v cold=%v warm=%v",
			scope, cold.Totals.Input, cold.Totals.CacheWrite, cold.Totals.CacheRead, cold.Totals.Output,
			cold.Skills, coldTook, warmTook)

		if cold.Totals.Total() == 0 {
			t.Fatalf("%s scope produced zero tokens", scope)
		}
		// A warm scan read nothing new, so it must agree with a cold one.
		if warm != cold {
			t.Errorf("%s warm scan %+v disagrees with cold %+v", scope, warm, cold)
		}
	}
}
