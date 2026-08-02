package segment

import (
	"strconv"
	"strings"

	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func init() {
	register("mcp", Def{
		Fields:          []string{"icon", "count", "warn", "tools", "auth", "pending", "servers"},
		DefaultTemplate: "{icon} {count}{warn}",
		Build:           buildMCP,
	})
}

// mcpIcon stay single-width: emoji plug would read closer but cost column
// width that vary by terminal, and row alignment is what this segment sit in.
const mcpIcon = "⛁"

// buildMCP report MCP servers attached to this session.
//
// stdin payload carry no MCP field at all, so roster come from transcript
// deferred_tools_delta attachments -- see transcript.ScanMCP.
//
// Segment drop unless something is attached, pending or unauthorised. Nothing
// mean off here, same as fast_mode and thinking; person running no MCP never
// asked for slot saying so. Unknown drop for same reason, and Def.Stable stay
// false so no placeholder claim server that may not exist.
//
// Reconnect is exception: roster empty while server come back, and dropping
// there make row shape jump mid-session. Pending count hold slot instead.
func buildMCP(c Context) Result {
	if c.In.TranscriptPath == "" {
		return empty
	}
	state, ok := transcript.ScanMCP(c.In.TranscriptPath)
	if !ok {
		return empty
	}

	trouble := state.Pending + state.NeedsAuth
	if len(state.Servers) == 0 && trouble == 0 {
		return empty
	}

	// Names arrive from remote server and row reach terminal unescaped, where
	// "\x1b[2J" clear screen.
	names := make([]string, 0, len(state.Servers))
	for _, s := range state.Servers {
		names = append(names, sanitize(s))
	}

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"icon":    render.Colored(mcpIcon, render.Dim),
			"count":   render.Colored(strconv.Itoa(len(state.Servers)), render.White),
			"warn":    render.Plain(mcpWarnGroup(c, trouble)),
			"tools":   render.Colored(strconv.Itoa(state.Tools), render.White),
			"pending": render.Colored(strconv.Itoa(state.Pending), render.Yellow),
			"auth":    render.Colored(strconv.Itoa(state.NeedsAuth), render.Yellow),
			"servers": render.Colored(strings.Join(names, " "), render.Dim),
		},
	}
}

// mcpWarnGroup render " ⚠3": servers reconnecting plus servers awaiting OAuth.
//
// One number, not two: both mean same thing to reader -- server named in row is
// not answering. Separator sit inside field, same shape as tokens' {cache}, so
// healthy session drop marker and its gap together.
func mcpWarnGroup(c Context, trouble int) string {
	if trouble == 0 {
		return ""
	}
	return " " + c.Palette.Wrap("⚠", render.Dim) +
		c.Palette.Wrap(strconv.Itoa(trouble), render.Yellow)
}
