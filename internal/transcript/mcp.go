package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
)

// MCPState is live MCP roster for one session.
//
// Three counts stay apart because they answer different questions and no two
// share a namespace: Servers come from tool names Claude Code sanitize
// ("claude_ai_Booking_com"), while Pending and NeedsAuth count display names
// ("claude.ai Booking.com"). Merging them by string match nothing.
//
// Disjoint by construction: reconnecting server had its tools removed, and
// server awaiting OAuth never contributed any. Measured across 280 recorded
// sessions -- no server land in Servers and either count at once.
type MCPState struct {
	Servers   []string
	Tools     int
	Pending   int
	NeedsAuth int
}

const mcpToolPrefix = "mcp__"

// Reader window, matching scan.go. Longer line still read whole, ReadBytes
// stitching pieces together.
const mcpReadBuffer = 256 * 1024

// Prefilter. Whole-file scan cost nothing next to per-line decode: measured
// 1.5MB transcript carry one to three of these lines.
var deferredProbe = []byte(`"deferred_tools_delta"`)

// One deferred_tools_delta attachment.
//
// addedNames and removedNames are deltas over deferred tool set. readdedNames
// measured empty in every recorded session, its names repeating in addedNames,
// but set insert is idempotent so folding it in cost nothing and outlive writer
// that stop repeating them.
//
// Two server lists are whole snapshots at that moment, not deltas. Accumulating
// them hold reconnected server as pending rest of session.
//
// Server lists are pointers: measured transcripts carry deltas leaving
// needsAuthMcpServers out entirely, and absent list is no proof of zero. Nil
// mean unreported, empty list mean nobody waiting.
//
// Holding last count instead of zeroing trade under-report for over-report. ⚠
// linger one delta past reconnect that omit list, where zeroing drop ⚠ while
// servers still wait. Stale marker self-correct on next delta carrying list;
// missing marker never correct itself. Every recorded reconnect carry list.
type deferredDelta struct {
	Attachment *struct {
		Type      string    `json:"type"`
		Added     []string  `json:"addedNames"`
		Readded   []string  `json:"readdedNames"`
		Removed   []string  `json:"removedNames"`
		Pending   *[]string `json:"pendingMcpServers"`
		NeedsAuth *[]string `json:"needsAuthMcpServers"`
	} `json:"attachment"`
}

// ScanMCP replay every deferred_tools_delta in one session transcript.
//
// Second return is false when no delta was found: unreadable transcript, or
// session younger than first attachment, which sit some 16KB in. Zero servers
// is fact only once delta prove roster empty.
//
// Session scope always. MCP connection belong to session process, so summing
// sibling transcripts under scope = "project" would report servers of sessions
// that already exited.
//
// No cache. Delta sit at head of file where incremental scan never look again,
// so cursor is only place roster could live -- and roster is tool set plus two
// server lists, not one number. Skill listing park in cursor at cacheVersion 4
// because it is one int; MCPState would grow every cursor by every tool name
// session ever attached, sibling transcripts under scope = "project" included.
//
// Whole file pass through every render, so cost track transcript size: measured
// on M1 Pro with warm page cache, 1.4MB in 1.1ms, 3.5MB in 3.2ms. Reading only
// appended bytes would need that roster cached beside cursor.
func ScanMCP(path string) (MCPState, bool) {
	f, err := os.Open(path)
	if err != nil {
		return MCPState{}, false
	}
	defer f.Close()

	tools := map[string]struct{}{}
	var state MCPState
	seen := false

	r := bufio.NewReaderSize(f, mcpReadBuffer)
	for {
		// ReadBytes grow to fit. Longest recorded transcript line ran 2.7MB, far
		// past bufio.Scanner 64KB token ceiling.
		line, err := r.ReadBytes('\n')
		if applyMCPDelta(line, tools, &state) {
			seen = true
		}
		// Fragment without newline = write in progress. Partial JSON fail decode
		// and drop out in applyMCPDelta anyway.
		if err != nil {
			break
		}
	}
	if !seen {
		return MCPState{}, false
	}

	state.Tools = len(tools)
	servers := map[string]struct{}{}
	for name := range tools {
		if s := serverOf(name); s != "" {
			servers[s] = struct{}{}
		}
	}
	state.Servers = make([]string, 0, len(servers))
	for s := range servers {
		state.Servers = append(state.Servers, s)
	}
	// Sorted so row stay put between renders -- map order alone reshuffle
	// {servers} every draw.
	slices.Sort(state.Servers)
	return state, true
}

// applyMCPDelta fold one transcript line into roster, reporting whether line
// was a delta at all. Caller need that apart from roster contents: transcript
// carrying no delta is unknown, delta proving roster empty is zero.
func applyMCPDelta(line []byte, tools map[string]struct{}, state *MCPState) bool {
	if !bytes.Contains(line, deferredProbe) {
		return false
	}
	var d deferredDelta
	// Undecodable line skipped in silence. Partial write and future record shape
	// both land here; neither justify blanking a status line.
	if json.Unmarshal(line, &d) != nil || d.Attachment == nil ||
		d.Attachment.Type != "deferred_tools_delta" {
		return false
	}
	// Remove before add: one event carry both when server drop and another take
	// its place.
	for _, name := range d.Attachment.Removed {
		delete(tools, name)
	}
	for _, name := range slices.Concat(d.Attachment.Added, d.Attachment.Readded) {
		if strings.HasPrefix(name, mcpToolPrefix) {
			tools[name] = struct{}{}
		}
	}
	// Unreported list leave count alone. Reading absent needsAuthMcpServers as 0
	// drop ⚠ off row while servers still wait for OAuth.
	if d.Attachment.Pending != nil {
		state.Pending = len(*d.Attachment.Pending)
	}
	if d.Attachment.NeedsAuth != nil {
		state.NeedsAuth = len(*d.Attachment.NeedsAuth)
	}
	return true
}

// serverOf pull server half out of mcp__<server>__<tool>. Tool half carry "__"
// of its own, so split take first separator past prefix, not last.
func serverOf(tool string) string {
	rest, ok := strings.CutPrefix(tool, mcpToolPrefix)
	if !ok {
		return ""
	}
	server, _, ok := strings.Cut(rest, "__")
	if !ok {
		return ""
	}
	return server
}
