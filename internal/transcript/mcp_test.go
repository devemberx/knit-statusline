package transcript

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Line shaped like attachment Claude Code write when deferred tool set change.
// addedNames/removedNames are deltas; two server lists are whole snapshots
// taken at that moment.
func deltaLine(t *testing.T, added, removed, pending, auth []string) string {
	t.Helper()
	att := map[string]any{
		"type":                "deferred_tools_delta",
		"addedNames":          added,
		"removedNames":        removed,
		"readdedNames":        []string{},
		"pendingMcpServers":   pending,
		"needsAuthMcpServers": auth,
	}
	b, err := json.Marshal(map[string]any{"type": "system", "attachment": att})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	return string(b)
}

func scanMCPFile(t *testing.T, lines []string) (MCPState, bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path, lines)
	return ScanMCP(path)
}

// Defining behaviour: roster come from mcp__<server>__<tool> names, one entry
// per distinct server however many tools it carry.
func TestScanMCPCountsDistinctServers(t *testing.T) {
	got, ok := scanMCPFile(t, []string{
		deltaLine(t, []string{
			"mcp__claude_ai_TickTick__list_projects",
			"mcp__claude_ai_TickTick__create_task",
			"mcp__claude_ai_Booking_com__attractions_search",
		}, nil, nil, nil),
	})
	if !ok {
		t.Fatal("ScanMCP reported unknown, want state")
	}
	want := []string{"claude_ai_Booking_com", "claude_ai_TickTick"}
	if strings.Join(got.Servers, ",") != strings.Join(want, ",") {
		t.Errorf("Servers = %v, want %v", got.Servers, want)
	}
	if got.Tools != 3 {
		t.Errorf("Tools = %d, want 3", got.Tools)
	}
}

// Built-in tools ride same delta. Counting them report server count nobody
// configured.
func TestScanMCPIgnoresNonMCPTools(t *testing.T) {
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, []string{"WebFetch", "TodoWrite", "mcp__srv__go"}, nil, nil, nil),
	})
	if len(got.Servers) != 1 || got.Servers[0] != "srv" {
		t.Errorf("Servers = %v, want [srv]", got.Servers)
	}
	if got.Tools != 1 {
		t.Errorf("Tools = %d, want 1", got.Tools)
	}
}

// Server name run to first "__" past prefix; tool half carry "__" of its own.
func TestScanMCPSplitsServerAtFirstSeparator(t *testing.T) {
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, []string{"mcp__srv__list__all__items"}, nil, nil, nil),
	})
	if len(got.Servers) != 1 || got.Servers[0] != "srv" {
		t.Errorf("Servers = %v, want [srv]", got.Servers)
	}
}

// Disconnect then reconnect, as measured live: removal empty roster, following
// addition refill it. Order inside one event is remove then add.
func TestScanMCPAppliesDeltasInOrder(t *testing.T) {
	tools := []string{"mcp__srv__a", "mcp__srv__b"}
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, tools, nil, nil, nil),
		deltaLine(t, nil, tools, []string{"srv"}, nil),
		deltaLine(t, tools, nil, nil, nil),
	})
	if len(got.Servers) != 1 || got.Tools != 2 {
		t.Errorf("Servers = %v Tools = %d, want [srv] and 2", got.Servers, got.Tools)
	}
}

// Mid-blip state: tools gone, server listed as reconnecting.
func TestScanMCPReportsRosterEmptyWhilePending(t *testing.T) {
	tools := []string{"mcp__srv__a"}
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, tools, nil, nil, nil),
		deltaLine(t, nil, tools, []string{"claude.ai TickTick"}, nil),
	})
	if len(got.Servers) != 0 {
		t.Errorf("Servers = %v, want none", got.Servers)
	}
	if got.Pending != 1 {
		t.Errorf("Pending = %d, want 1", got.Pending)
	}
}

// Two server lists are snapshots, not deltas: accumulating them hold
// reconnected server as pending rest of session.
func TestScanMCPTakesLastServerListSnapshot(t *testing.T) {
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, nil, nil, nil, []string{"claude.ai Gmail", "claude.ai Notion"}),
		deltaLine(t, nil, nil, []string{"claude.ai TickTick"}, []string{"claude.ai Gmail", "claude.ai Notion"}),
		deltaLine(t, nil, nil, nil, []string{"claude.ai Gmail"}),
	})
	if got.Pending != 0 {
		t.Errorf("Pending = %d, want 0", got.Pending)
	}
	if got.NeedsAuth != 1 {
		t.Errorf("NeedsAuth = %d, want 1", got.NeedsAuth)
	}
}

// No attachment reached yet -- session younger than first delta, which sit some
// 16KB in. Unknown, never zero: fresh session claiming no servers contradict
// roster it is about to print.
func TestScanMCPReportsUnknownWithoutDelta(t *testing.T) {
	if _, ok := scanMCPFile(t, []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
	}); ok {
		t.Error("ScanMCP reported state, want unknown")
	}
}

func TestScanMCPReportsUnknownForMissingFile(t *testing.T) {
	if _, ok := ScanMCP(filepath.Join(t.TempDir(), "absent.jsonl")); ok {
		t.Error("ScanMCP reported state for missing file, want unknown")
	}
}

// Attachment listing every deferred tool measured 5.7KB and grow with tool
// count. bufio.Scanner cap token at 64KB and fail whole scan past it.
func TestScanMCPReadsLinePastScannerLimit(t *testing.T) {
	filler := make([]string, 0, 3000)
	for i := range 3000 {
		filler = append(filler, "pad__"+strings.Repeat("x", 20)+string(rune('a'+i%26)))
	}
	line := deltaLine(t, append(filler, "mcp__srv__a"), nil, nil, nil)
	if len(line) < 64*1024 {
		t.Fatalf("test line is %d bytes, need over 64KB to exercise limit", len(line))
	}
	got, ok := scanMCPFile(t, []string{line})
	if !ok || len(got.Servers) != 1 {
		t.Errorf("Servers = %v ok = %v, want [srv] and true", got.Servers, ok)
	}
}

// Corrupt line sit beside good ones after crash mid-write.
func TestScanMCPSkipsUnparsableLines(t *testing.T) {
	got, ok := scanMCPFile(t, []string{
		`{"attachment":{"type":"deferred_tools_delta", TRUNCATED`,
		deltaLine(t, []string{"mcp__srv__a"}, nil, nil, nil),
	})
	if !ok || len(got.Servers) != 1 {
		t.Errorf("Servers = %v ok = %v, want [srv] and true", got.Servers, ok)
	}
}
