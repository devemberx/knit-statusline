package segment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// mcpDelta mirror attachment Claude Code write when deferred tool set change.
//
// Server lists go out as [] rather than null: recorded attachments carry key
// with [] when nobody wait, and omit it altogether otherwise. Null is third
// shape no transcript hold, and scan read it as unreported.
func mcpDelta(t *testing.T, added, removed, pending, auth []string) string {
	t.Helper()
	orEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	b, err := json.Marshal(map[string]any{"attachment": map[string]any{
		"type":                "deferred_tools_delta",
		"addedNames":          orEmpty(added),
		"removedNames":        orEmpty(removed),
		"pendingMcpServers":   orEmpty(pending),
		"needsAuthMcpServers": orEmpty(auth),
	}})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	return string(b)
}

// mcpCtx point mcp segment at throwaway transcript holding lines.
func mcpCtx(t *testing.T, lines ...string) Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var body []byte
	for _, l := range lines {
		body = append(body, l...)
		body = append(body, '\n')
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c := ctx(t, fixtures.Full, "mcp")
	c.In.TranscriptPath = path
	return c
}

func TestMCPRendersServerCount(t *testing.T) {
	got := draw(mcpCtx(t, mcpDelta(t, []string{
		"mcp__srv_a__go", "mcp__srv_b__go", "mcp__srv_c__go",
	}, nil, nil, nil)))
	if want := "⛁ 3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// One marker hold reconnecting and unauthorised servers together: reader need
// single glance at whether row is trustworthy, not two counts to add.
func TestMCPAppendsWarningForPendingAndUnauthorised(t *testing.T) {
	got := draw(mcpCtx(t, mcpDelta(t,
		[]string{"mcp__srv_a__go"}, nil,
		[]string{"claude.ai TickTick"},
		[]string{"claude.ai Gmail", "claude.ai Notion"})))
	if want := "⛁ 1 ⚠3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Nothing to warn about, nothing in row.
func TestMCPOmitsWarningWhenAllConnected(t *testing.T) {
	got := draw(mcpCtx(t, mcpDelta(t, []string{"mcp__srv_a__go"}, nil, nil, nil)))
	if strings.Contains(got, "⚠") {
		t.Errorf("rendered %q, want no warning marker", got)
	}
}

// Reconnect blip empty roster while server still coming back. Dropping segment
// here make row shape jump mid-session, which read as crash.
func TestMCPHoldsSlotWhileServerReconnects(t *testing.T) {
	tools := []string{"mcp__srv_a__go"}
	got := draw(mcpCtx(t,
		mcpDelta(t, tools, nil, nil, nil),
		mcpDelta(t, nil, tools, []string{"claude.ai TickTick"}, nil),
	))
	if want := "⛁ 0 ⚠1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Nobody run MCP at all. Absent mean off here, same as fast_mode and thinking.
func TestMCPRendersNothingWithoutServers(t *testing.T) {
	for name, lines := range map[string][]string{
		"delta prove roster empty": {mcpDelta(t, nil, nil, nil, nil)},
		"only built-in tools":      {mcpDelta(t, []string{"WebFetch"}, nil, nil, nil)},
		"session before first delta": {
			`{"type":"user","message":{"role":"user","content":"hi"}}`,
		},
	} {
		if got := draw(mcpCtx(t, lines...)); got != "" {
			t.Errorf("%s rendered %q, want nothing", name, got)
		}
	}
}

func TestMCPRendersNothingWithoutTranscriptPath(t *testing.T) {
	c := mcpCtx(t, mcpDelta(t, []string{"mcp__srv_a__go"}, nil, nil, nil))
	c.In.TranscriptPath = ""
	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

func TestMCPExposesEveryField(t *testing.T) {
	c := mcpCtx(t, mcpDelta(t,
		[]string{"mcp__srv_a__go", "mcp__srv_a__stop", "mcp__srv_b__go"}, nil,
		[]string{"claude.ai TickTick"}, []string{"claude.ai Gmail"}))
	c.Cfg.Template = "{icon}|{count}|{warn}|{tools}|{pending}|{auth}|{servers}"
	if want := "⛁|2| ⚠2|3|1|1|srv_a srv_b"; draw(c) != want {
		t.Errorf("rendered %q, want %q", draw(c), want)
	}
}

// Server name reach row from remote server, and row reach terminal unescaped,
// where "\x1b[2J" clear screen.
func TestMCPStripsControlBytesFromServerNames(t *testing.T) {
	c := mcpCtx(t, mcpDelta(t, []string{"mcp__a\x1b[2Jb__go"}, nil, nil, nil))
	c.Cfg.Template = "{servers}"
	if got := draw(c); got != "a[2Jb" {
		t.Errorf("rendered %q, want %q", got, "a[2Jb")
	}
}
