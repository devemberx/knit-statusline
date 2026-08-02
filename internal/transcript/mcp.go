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
// server awaiting OAuth never contributed any.
type MCPState struct {
	Servers   []string
	Tools     int
	Pending   int
	NeedsAuth int
}

const mcpToolPrefix = "mcp__"

// Prefilter. Whole-file scan cost nothing next to per-line decode: measured
// 1.5MB transcript carry one to three of these lines.
var deferredProbe = []byte(`"deferred_tools_delta"`)

// One deferred_tools_delta attachment.
//
// addedNames and removedNames are deltas over deferred tool set. readdedNames
// is subset of addedNames -- reconnect list both -- so applying it again change
// nothing and it stay undecoded.
//
// Two server lists are whole snapshots at that moment, not deltas. Accumulating
// them hold reconnected server as pending rest of session.
type deferredDelta struct {
	Attachment *struct {
		Type      string   `json:"type"`
		Added     []string `json:"addedNames"`
		Removed   []string `json:"removedNames"`
		Pending   []string `json:"pendingMcpServers"`
		NeedsAuth []string `json:"needsAuthMcpServers"`
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
// No cache. Cursor carry byte offset alone, and delta sit at head of file where
// incremental scan never look again; parking roster in cursor is what
// cacheVersion 3 tried and lost.
func ScanMCP(path string) (MCPState, bool) {
	f, err := os.Open(path)
	if err != nil {
		return MCPState{}, false
	}
	defer f.Close()

	tools := map[string]struct{}{}
	var state MCPState
	seen := false

	r := bufio.NewReaderSize(f, 256*1024)
	for {
		// ReadBytes grow to fit. Tool listing measured 5.7KB and grow with tool
		// count, past bufio.Scanner 64KB token ceiling.
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, deferredProbe) {
			var d deferredDelta
			if json.Unmarshal(line, &d) == nil && d.Attachment != nil &&
				d.Attachment.Type == "deferred_tools_delta" {
				seen = true
				// Remove before add: one event carry both when server drop and
				// another take its place.
				for _, name := range d.Attachment.Removed {
					delete(tools, name)
				}
				for _, name := range d.Attachment.Added {
					if strings.HasPrefix(name, mcpToolPrefix) {
						tools[name] = struct{}{}
					}
				}
				state.Pending = len(d.Attachment.Pending)
				state.NeedsAuth = len(d.Attachment.NeedsAuth)
			}
		}
		// Fragment without newline = write in progress. Partial JSON fail decode
		// and drop out here anyway.
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
