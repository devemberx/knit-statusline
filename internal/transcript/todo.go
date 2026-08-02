package transcript

import (
	"bytes"
	"encoding/json"
)

// Todos count one TodoWrite call. Claude Code write no todo state file, so
// tool call in transcript is only record there is.
//
// in_progress not counted: row show a counter coloured by completion, and a
// third number nothing read is dead data.
type Todos struct {
	Done  int `json:"done"`  // entries with status == "completed"
	Total int `json:"total"` // length of todos array
}

// TodoCursor record how far one transcript consumed, plus counts from last
// TodoWrite in it.
type TodoCursor struct {
	Offset int64 `json:"offset"`
	Todos  Todos `json:"todos"`
}

// Prefilter. Line without this substring carry no TodoWrite call. Reverse not
// guaranteed -- prose name tool too -- so decode still decide. False positive
// cost one wasted decode, false negative lose list.
var todoProbe = []byte("TodoWrite")

const todoCompleted = "completed"

// Minimal shape. Line carry far more; decoding only these keep scan cheap.
type todoEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input *struct {
				Todos []struct {
					Status string `json:"status"`
				} `json:"todos"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// ScanTodos advance cur over bytes appended since last scan.
//
// Last TodoWrite win. Each call replace list rather than extend it, so summing
// would report a total no list ever held.
func ScanTodos(path string, cur TodoCursor) (TodoCursor, error) {
	offset, err := scanAppended(path, cur.Offset,
		func() { cur = TodoCursor{} },
		func(line []byte) { applyTodoLine(&cur, line) },
	)
	cur.Offset = offset
	return cur, err
}

// applyTodoLine fold one complete transcript line into cursor.
func applyTodoLine(cur *TodoCursor, line []byte) {
	if !bytes.Contains(line, todoProbe) {
		return
	}
	var e todoEntry
	// Undecodable line skipped in silence. Partial write and future record shape
	// both land here; neither justify blanking a segment.
	if json.Unmarshal(line, &e) != nil {
		return
	}
	// Sidechain carry subagent's own list. Letting it through mean a subagent
	// clobber list user is watching.
	if e.Type != "assistant" || e.IsSidechain || e.Message == nil {
		return
	}
	for _, b := range e.Message.Content {
		if b.Type != "tool_use" || b.Name != "TodoWrite" || b.Input == nil {
			continue
		}
		t := Todos{Total: len(b.Input.Todos)}
		for _, item := range b.Input.Todos {
			if item.Status == todoCompleted {
				t.Done++
			}
		}
		cur.Todos = t
	}
}
