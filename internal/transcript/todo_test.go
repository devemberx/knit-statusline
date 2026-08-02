package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Line shaped like ones Claude Code write for a TodoWrite call: tool_use block
// inside assistant message, one entry per todo.
func todoLine(sidechain bool, statuses ...string) string {
	var items string
	for i, s := range statuses {
		if i > 0 {
			items += ","
		}
		items += fmt.Sprintf(`{"content":"t%d","status":%q,"activeForm":"doing t%d"}`, i, s, i)
	}
	return fmt.Sprintf(
		`{"type":"assistant","isSidechain":%t,"uuid":"u-%d","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu","name":"TodoWrite","input":{"todos":[%s]}}]}}`,
		sidechain, len(statuses), items)
}

func scanTodosOnce(t *testing.T, path string, cur TodoCursor) TodoCursor {
	t.Helper()
	out, err := ScanTodos(path, cur)
	if err != nil {
		t.Fatalf("ScanTodos: %v", err)
	}
	return out
}

// Defining behaviour: each TodoWrite replace list, never add to it.
func TestLastTodoWriteWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		todoLine(false, "completed", "pending"),
		todoLine(false, "completed", "completed", "completed", "pending"),
	})

	got := scanTodosOnce(t, path, TodoCursor{}).Todos
	if want := (Todos{Done: 3, Total: 4}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Subagent list must not clobber main one. Sidechain decide which lines count,
// same rule filesFor apply to which files open.
func TestScanTodosSkipsSidechain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		todoLine(false, "completed", "pending"),
		todoLine(true, "completed", "completed", "completed", "completed"),
	})

	got := scanTodosOnce(t, path, TodoCursor{}).Todos
	if want := (Todos{Done: 1, Total: 2}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Half-written entry and future record shape both land here. Neither justify
// blanking segment, and a later valid line must still count.
func TestScanTodosSkipsUndecodableLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		`{"type":"assistant","name":"TodoWrite",`,
		todoLine(false, "completed", "pending"),
	})

	got := scanTodosOnce(t, path, TodoCursor{}).Todos
	if want := (Todos{Done: 1, Total: 2}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Line naming tool in prose carry no tool_use block. Prefilter let it
// through; decode must drop it rather than count a zero list.
func TestScanTodosIgnoresProseMention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		todoLine(false, "completed", "pending"),
		`{"type":"user","uuid":"u-p","message":{"role":"user","content":[{"type":"text","text":"use TodoWrite here"}]}}`,
	})

	got := scanTodosOnce(t, path, TodoCursor{}).Todos
	if want := (Todos{Done: 1, Total: 2}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Incremental scan must land where a cold one does, else counts jitter between
// renders.
func TestScanTodosResumesFromOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{todoLine(false, "completed", "pending")})

	cur := scanTodosOnce(t, path, TodoCursor{})
	if cur.Offset == 0 {
		t.Fatal("first scan left offset at 0")
	}

	appendLines(t, path, []string{todoLine(false, "completed", "completed", "pending")})
	warm := scanTodosOnce(t, path, cur)

	cold := scanTodosOnce(t, path, TodoCursor{})
	if warm != cold {
		t.Errorf("warm scan = %+v, cold scan = %+v", warm, cold)
	}
}

// Appended bytes carrying no TodoWrite leave counts alone.
func TestScanTodosKeepsCountsWhenNothingNewMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{todoLine(false, "completed", "pending")})
	cur := scanTodosOnce(t, path, TodoCursor{})

	appendLines(t, path, []string{`{"type":"user","uuid":"u-x","message":{"role":"user","content":[]}}`})
	got := scanTodosOnce(t, path, cur).Todos

	if want := (Todos{Done: 1, Total: 2}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Transcripts append-only, so a shorter file is a replacement. Rescan whole
// rather than resume into middle of a different document.
func TestScanTodosRescansShrunkFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		todoLine(false, "completed", "completed", "completed", "pending"),
		todoLine(false, "completed", "completed", "completed", "pending"),
	})
	cur := scanTodosOnce(t, path, TodoCursor{})

	writeLines(t, path, []string{todoLine(false, "pending", "pending")})
	got := scanTodosOnce(t, path, cur).Todos

	if want := (Todos{Done: 0, Total: 2}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Emptied list is a real answer, not a missing one. Segment read Total==0 and
// drop, same as a session that never called tool.
func TestScanTodosReadsClearedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{
		todoLine(false, "completed", "pending"),
		`{"type":"assistant","isSidechain":false,"uuid":"u-c","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu","name":"TodoWrite","input":{"todos":[]}}]}}`,
	})

	got := scanTodosOnce(t, path, TodoCursor{}).Todos
	if want := (Todos{}); got != want {
		t.Errorf("Todos = %+v, want %+v", got, want)
	}
}

// Fragment without newline is a write in progress. Offset stay short of it, so
// line count once complete instead of half.
func TestScanTodosIgnoresUnterminatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, []string{todoLine(false, "completed", "pending")})
	cur := scanTodosOnce(t, path, TodoCursor{})

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(todoLine(false, "pending", "pending", "pending")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := scanTodosOnce(t, path, cur)
	if want := (Todos{Done: 1, Total: 2}); got.Todos != want {
		t.Errorf("Todos = %+v, want %+v", got.Todos, want)
	}
	if got.Offset != cur.Offset {
		t.Errorf("offset moved over an unterminated line: %d, was %d", got.Offset, cur.Offset)
	}
}

// Missing file is not error worth a stack: session with no transcript yet
// reach here on every render.
func TestScanTodosReportsMissingFile(t *testing.T) {
	_, err := ScanTodos(filepath.Join(t.TempDir(), "gone.jsonl"), TodoCursor{})
	if err == nil {
		t.Fatal("missing transcript returned no error")
	}
}
