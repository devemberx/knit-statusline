package segment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/render"
)

// todoCtx point a Context at a transcript on disk. No fixture JSON carry
// transcript_path, so every todo test seed its own.
// Shared with segment_test.go.
func todoCtx(t *testing.T, lines string) Context {
	t.Helper()
	c := ctx(t, fixtures.Full, "todo")
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	c.In.TranscriptPath = path
	c.CacheDir = t.TempDir()
	return c
}

// todoTranscript write shipped fixture out, so registry-wide tests reach list
// without hand-writing lines.
// Shared with segment_test.go.
func todoTranscript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, fixtures.TodosJSONL, 0o644); err != nil {
		t.Fatalf("seed todo transcript: %v", err)
	}
	return path
}

const todoLineTwoOfThree = `{"type":"assistant","isSidechain":false,"uuid":"u-1","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu","name":"TodoWrite","input":{"todos":[{"content":"a","status":"completed","activeForm":"a"},{"content":"b","status":"completed","activeForm":"b"},{"content":"c","status":"in_progress","activeForm":"c"}]}}]}}
`

const todoLineAllDone = `{"type":"assistant","isSidechain":false,"uuid":"u-1","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu","name":"TodoWrite","input":{"todos":[{"content":"a","status":"completed","activeForm":"a"},{"content":"b","status":"completed","activeForm":"b"}]}}]}}
`

func TestTodoDefaultTemplate(t *testing.T) {
	if got, want := draw(todoCtx(t, todoLineTwoOfThree)), "☑ 2/3"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
}

func TestTodoFields(t *testing.T) {
	res := Build(todoCtx(t, todoLineTwoOfThree))
	if res.Empty {
		t.Fatal("todo dropped a list it could read")
	}
	for field, want := range map[string]string{
		"icon":    "☑",
		"ratio":   "2/3",
		"done":    "2",
		"total":   "3",
		"pending": "1",
	} {
		if got := res.Fields[field].Text; got != want {
			t.Errorf("{%s} = %q, want %q", field, got, want)
		}
	}
}

// Two states, no thresholds: high completion is good here, and threshold colours
// grade it as bad.
func TestTodoColorsByCompletion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		want  render.Color
	}{
		{"in progress", todoLineTwoOfThree, render.White},
		{"all done", todoLineAllDone, render.Green},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Build(todoCtx(t, tc.lines))
			if res.Empty {
				t.Fatal("todo dropped a list it could read")
			}
			for _, field := range []string{"icon", "ratio", "done", "total", "pending"} {
				if got := res.Fields[field].Color; got != tc.want {
					t.Errorf("{%s} colour = %q, want %q", field, got, tc.want)
				}
			}
		})
	}
}

// Absent, not unknown: session never touching todos would otherwise carry a
// permanent placeholder.
func TestTodoDropsWithoutSomethingToShow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
	}{
		{"no TodoWrite in transcript", `{"type":"user","uuid":"u-1","message":{"role":"user","content":[]}}` + "\n"},
		{"list cleared", `{"type":"assistant","isSidechain":false,"uuid":"u-1","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu","name":"TodoWrite","input":{"todos":[]}}]}}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := draw(todoCtx(t, tc.lines)); got != "" {
				t.Errorf("drew %q, want nothing", got)
			}
		})
	}
}

func TestTodoDropsWithoutTranscriptPath(t *testing.T) {
	c := ctx(t, fixtures.Full, "todo")
	c.CacheDir = t.TempDir()
	if got := draw(c); got != "" {
		t.Errorf("drew %q with no transcript_path, want nothing", got)
	}
}

// Gone transcript must not leave a stale count on screen forever.
func TestTodoDropsWhenTranscriptDisappears(t *testing.T) {
	c := todoCtx(t, todoLineTwoOfThree)
	if draw(c) == "" {
		t.Fatal("todo dropped a list it could read")
	}
	if err := os.Remove(c.In.TranscriptPath); err != nil {
		t.Fatal(err)
	}
	if got := draw(c); got != "" {
		t.Errorf("drew %q after the transcript vanished, want nothing", got)
	}
}

// Second render must read appended bytes only and still land where a cold one
// does. Cache file proves offset persisted.
func TestTodoReusesItsCache(t *testing.T) {
	c := todoCtx(t, todoLineTwoOfThree)
	if got, want := draw(c), "☑ 2/3"; got != want {
		t.Errorf("first draw = %q, want %q", got, want)
	}

	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "todos-") {
		t.Fatalf("cache dir hold %d entries, want one todos-*.json", len(entries))
	}

	f, err := os.OpenFile(c.In.TranscriptPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(todoLineAllDone); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if got, want := draw(c), "☑ 2/2"; got != want {
		t.Errorf("second draw = %q, want %q", got, want)
	}
}

// Unwritable cache dir cost one rescan, row still draw.
func TestTodoSurvivesUnwritableCacheDir(t *testing.T) {
	c := todoCtx(t, todoLineTwoOfThree)
	c.CacheDir = filepath.Join(c.In.TranscriptPath, "not-a-dir")
	if got, want := draw(c), "☑ 2/3"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
}
