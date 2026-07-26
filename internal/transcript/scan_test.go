package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Transcript line shaped like ones Claude Code write: one per content block,
// each repeating a full usage object for its message.
func assistantLine(msgID, model string, in, cw, cr, out int64) string {
	return fmt.Sprintf(
		`{"type":"assistant","isSidechain":false,"uuid":"u-%s","message":{"id":%q,"model":%q,"role":"assistant","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`,
		msgID, msgID, model, in, cw, cr, out)
}

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

func scanOnce(t *testing.T, path string, cache *Cache) (Totals, *Cache) {
	t.Helper()
	return Scan(Options{TranscriptPath: path, Scope: ScopeSession}, cache)
}

// Defining behaviour of this package: repeated lines for one message count
// once. Undeduplicated, these three lines report 300 input tokens.
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
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
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
	if first.Input != 100 {
		t.Fatalf("first scan input = %d, want 100", first.Input)
	}

	// Remaining blocks of that same message arrive after first scan.
	appendLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5),
		assistantLine("msg_b", "claude-opus-4-8", 200, 20, 2000, 7),
	})

	second, _ := scanOnce(t, path, cache)
	want := Totals{Input: 300, CacheWrite: 30, CacheRead: 3000, Output: 12}
	if second != want {
		t.Errorf("got %+v, want %+v", second, want)
	}

	// Full scan from scratch agree with incremental result.
	fresh, _ := scanOnce(t, path, nil)
	if fresh != second {
		t.Errorf("incremental %+v disagrees with full rescan %+v", second, fresh)
	}
}

func TestIncrementalAppendDoesNotRecount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5)})

	_, cache := scanOnce(t, path, nil)
	offsetAfterFirst := cache.Files[path].Offset

	appendLines(t, path, []string{assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7)})
	got, cache := scanOnce(t, path, cache)

	if got.Input != 300 || got.Output != 12 {
		t.Errorf("got %+v, want input 300 output 12", got)
	}
	if cache.Files[path].Offset <= offsetAfterFirst {
		t.Error("cursor did not advance past the appended bytes")
	}
}

// Shrunk transcript got replaced, not appended to. Cursor untrustworthy, so
// read it again from its start.
func TestTruncationTriggersRescan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5),
		assistantLine("msg_b", "claude-opus-4-8", 200, 0, 0, 7),
	})
	_, cache := scanOnce(t, path, nil)

	writeLines(t, path, []string{assistantLine("msg_c", "claude-opus-4-8", 50, 0, 0, 1)})

	got, _ := scanOnce(t, path, cache)
	if got.Input != 50 || got.Output != 1 {
		t.Errorf("got %+v, want input 50 output 1", got)
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
	if got.Input != 100 {
		t.Fatalf("partial line was counted: %+v", got)
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
	if got.Input != 300 || got.Output != 12 {
		t.Errorf("got %+v, want input 300 output 12", got)
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
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Malformed lines skipped in silence. Corrupt transcript degrade a number,
// never blank its status line.
func TestCorruptLinesAreSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		`{"usage": broken json here`,
		assistantLine("msg_a", "claude-opus-4-8", 100, 0, 0, 5),
		`{"usage":`,
	})

	got, _ := scanOnce(t, path, nil)
	if got.Input != 100 {
		t.Errorf("got %+v, want input 100", got)
	}
}

func TestMissingTranscriptYieldsZero(t *testing.T) {
	got, cache := scanOnce(t, filepath.Join(t.TempDir(), "absent.jsonl"), nil)
	if got != (Totals{}) {
		t.Errorf("got %+v, want zero totals", got)
	}
	if cache == nil {
		t.Error("cache should never be nil")
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
	if session.Input != 100 {
		t.Errorf("session scope input = %d, want 100", session.Input)
	}

	project, _ := Scan(Options{TranscriptPath: main, Scope: ScopeProject}, nil)
	if project.Input != 300 {
		t.Errorf("project scope input = %d, want 300 (agent file excluded)", project.Input)
	}

	withAgents, _ := Scan(Options{TranscriptPath: main, Scope: ScopeProject, IncludeSidechain: true}, nil)
	if withAgents.Input != 700 {
		t.Errorf("project scope with sidechain input = %d, want 700", withAgents.Input)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeLines(t, path, []string{assistantLine("msg_a", "claude-opus-4-8", 100, 10, 1000, 5)})

	opts := Options{TranscriptPath: path, Scope: ScopeSession}
	_, cache := Scan(opts, nil)

	cacheDir := filepath.Join(dir, "cache")
	if err := SaveCache(cacheDir, opts, cache); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded := LoadCache(cacheDir, opts)
	if loaded.Files[path] != cache.Files[path] {
		t.Errorf("round trip lost state: %+v vs %+v", loaded.Files[path], cache.Files[path])
	}

	// No temp file survive one atomic write.
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestLoadCacheToleratesGarbage(t *testing.T) {
	dir := t.TempDir()
	opts := Options{TranscriptPath: "/x/y.jsonl", Scope: ScopeSession}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CacheKey(opts)), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := LoadCache(dir, opts)
	if c == nil || len(c.Files) != 0 || c.Version != cacheVersion {
		t.Errorf("corrupt cache should yield a fresh one, got %+v", c)
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

		t.Logf("%-7s input=%d cacheWrite=%d cacheRead=%d output=%d  cold=%v warm=%v",
			scope, cold.Input, cold.CacheWrite, cold.CacheRead, cold.Output, coldTook, warmTook)

		if cold.Total() == 0 {
			t.Fatalf("%s scope produced zero tokens", scope)
		}
		// A warm scan read nothing new, so it must agree with a cold one.
		if warm != cold {
			t.Errorf("%s warm scan %+v disagrees with cold %+v", scope, warm, cold)
		}
	}
}
