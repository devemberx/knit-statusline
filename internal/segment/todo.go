package segment

import (
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

// Icon sit in field, not template literal: literal take Result.Base, Base is
// Dim, and SGR 2 fade glyph past reading.
const todoIcon = "☑"

func init() {
	register("todo", Def{
		Fields:          []string{"icon", "ratio", "done", "total", "pending"},
		DefaultTemplate: "{icon} {ratio}",
		Build:           buildTodo,
	})
}

// buildTodo report todo list progress read from transcript.
//
// Claude Code write no todo state file. Last TodoWrite tool call in session
// transcript is whole record.
//
// Not Stable: session never touching todos have no list, and absent is
// answer -- permanent "…" there is noise, not fact withheld.
func buildTodo(c Context) Result {
	if c.In.TranscriptPath == "" {
		return empty
	}

	cur := transcript.LoadTodoCursor(c.CacheDir, c.In.TranscriptPath)
	cur, err := transcript.ScanTodos(c.In.TranscriptPath, cur)
	if err != nil {
		// Cached counts describe file this render could not read. Gone
		// transcript would otherwise pin stale number on screen for rest
		// of session.
		return empty
	}

	// Cache write failure cost one rescan next render. Not worth failing over.
	_ = transcript.SaveTodoCursor(c.CacheDir, c.In.TranscriptPath, cur)

	t := cur.Todos
	// Zero cover two states -- tool never called, and list cleared with empty
	// array. Both mean nothing to show.
	if t.Total == 0 {
		return empty
	}

	done, total := int64(t.Done), int64(t.Total)
	col := render.White
	if t.Done == t.Total {
		col = render.Green
	}

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"icon":    render.Colored(todoIcon, col),
			"ratio":   render.Colored(itoa(done)+"/"+itoa(total), col),
			"done":    render.Colored(itoa(done), col),
			"total":   render.Colored(itoa(total), col),
			"pending": render.Colored(itoa(total-done), col),
		},
	}
}
