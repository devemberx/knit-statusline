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
//
// Empty list, never null: recorded attachments carry key with [] when nobody
// wait, and leave it out altogether otherwise. Marshalling nil slice would
// write null, third shape no transcript hold.
func deltaLine(t *testing.T, added, removed, pending, auth []string) string {
	t.Helper()
	att := map[string]any{
		"type":                "deferred_tools_delta",
		"addedNames":          orEmpty(added),
		"removedNames":        orEmpty(removed),
		"readdedNames":        []string{},
		"pendingMcpServers":   orEmpty(pending),
		"needsAuthMcpServers": orEmpty(auth),
	}
	return marshalDelta(t, att)
}

func marshalDelta(t *testing.T, att map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": "system", "attachment": att})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	return string(b)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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

// Recorded attachments leave needsAuthMcpServers out entirely -- measured in
// three sessions, one of them reporting three such servers on later delta.
// Absent list is no proof of zero, and reading it as one drop ⚠ off row while
// servers still wait for OAuth.
func TestScanMCPKeepsCountsWhenServerListAbsent(t *testing.T) {
	full := deltaLine(t, []string{"mcp__srv__a"}, nil,
		[]string{"claude.ai TickTick"},
		[]string{"claude.ai Gmail", "claude.ai Notion"})
	// Same attachment minus both server lists.
	bare := marshalDelta(t, map[string]any{
		"type":         "deferred_tools_delta",
		"addedNames":   []string{},
		"removedNames": []string{},
		"readdedNames": []string{},
	})

	got, ok := scanMCPFile(t, []string{full, bare})
	if !ok {
		t.Fatal("ScanMCP reported unknown, want state")
	}
	if got.Pending != 1 {
		t.Errorf("Pending = %d, want 1", got.Pending)
	}
	if got.NeedsAuth != 2 {
		t.Errorf("NeedsAuth = %d, want 2", got.NeedsAuth)
	}
}

// readdedNames measured empty in every recorded session, its names repeating in
// addedNames. Folding it in cost nothing and outlive writer that stop repeating
// them, which would else strand reconnected server at zero.
func TestScanMCPFoldsReaddedNames(t *testing.T) {
	tools := []string{"mcp__srv__a"}
	readd := marshalDelta(t, map[string]any{
		"type":         "deferred_tools_delta",
		"addedNames":   []string{},
		"removedNames": []string{},
		"readdedNames": tools,
	})
	got, _ := scanMCPFile(t, []string{
		deltaLine(t, tools, nil, nil, nil),
		deltaLine(t, nil, tools, []string{"srv"}, nil),
		readd,
	})
	if len(got.Servers) != 1 || got.Tools != 1 {
		t.Errorf("Servers = %v Tools = %d, want [srv] and 1", got.Servers, got.Tools)
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

// Line past mcpReadBuffer arrive in pieces and get stitched back together.
// Longest recorded transcript line ran 2.7MB, so this path carry real traffic
// beyond this test.
//
// Delta name sit in first piece and tool name in last, so scan that decode
// pieces separately report no server and fail here.
func TestScanMCPReadsLinePastReadBuffer(t *testing.T) {
	filler := make([]string, 0, 12000)
	for i := range 12000 {
		filler = append(filler, "pad__"+strings.Repeat("x", 20)+string(rune('a'+i%26)))
	}
	line := deltaLine(t, append(filler, "mcp__srv__a"), nil, nil, nil)
	if len(line) <= mcpReadBuffer {
		t.Fatalf("test line is %d bytes, need over %d to overflow buffer",
			len(line), mcpReadBuffer)
	}
	got, ok := scanMCPFile(t, []string{line, deltaLine(t, nil, nil, nil, nil)})
	if !ok || len(got.Servers) != 1 || got.Servers[0] != "srv" {
		t.Errorf("Servers = %v ok = %v, want [srv] and true", got.Servers, ok)
	}
}

// Oversized line stop at its newline. Recorded transcript carry 2.7MB line with
// ordinary lines behind it, and reader losing that boundary glue two together
// and break both.
func TestScanMCPKeepsLinesApartPastReadBuffer(t *testing.T) {
	pad := make([]string, 0, 12000)
	for i := range 12000 {
		pad = append(pad, "pad__"+strings.Repeat("x", 20)+string(rune('a'+i%26)))
	}
	long := deltaLine(t, append(pad, "mcp__first__a"), nil, nil, nil)
	if len(long) <= mcpReadBuffer {
		t.Fatalf("first line is %d bytes, need over %d", len(long), mcpReadBuffer)
	}
	got, ok := scanMCPFile(t, []string{
		long,
		deltaLine(t, []string{"mcp__second__a"}, nil, nil, nil),
	})
	want := []string{"first", "second"}
	if !ok || strings.Join(got.Servers, ",") != strings.Join(want, ",") {
		t.Errorf("Servers = %v ok = %v, want %v and true", got.Servers, ok, want)
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
