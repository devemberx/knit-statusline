// Package transcript accumulate token usage from Claude Code transcripts.
//
// Claude Code write one JSONL line per content block, each repeating a whole
// usage object for its message. Naive sum over-count: measured session, 483
// assistant entries over 220 distinct messages, input 62,093 read as 195,836.
// Dedup on message.id is this package's correctness requirement.
//
// Repeats always contiguous -- eight large transcripts, zero non-contiguous.
// One remembered id enough. Id persist in cursor too, else a message straddling
// one incremental boundary double-count and totals jitter between renders.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entries Claude Code generate locally. Carry usage, bill nothing.
const syntheticModel = "<synthetic>"

// Totals hold four counters apart. Cache read run orders of magnitude over
// fresh input and price differently; one "input" figure mislead.
type Totals struct {
	Input      int64 `json:"input"`
	CacheWrite int64 `json:"cacheWrite"`
	CacheRead  int64 `json:"cacheRead"`
	Output     int64 `json:"output"`
}

func (t *Totals) Add(other Totals) {
	t.Input += other.Input
	t.CacheWrite += other.CacheWrite
	t.CacheRead += other.CacheRead
	t.Output += other.Output
}

func (t Totals) Total() int64 {
	return t.Input + t.CacheWrite + t.CacheRead + t.Output
}

// Minimal shape. Transcript line carry far more; decoding these fields only
// keep scan cheap.
type entry struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens         int64 `json:"input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Prefilter. Line without this substring carry no message.usage, skip undecoded.
// Reverse not guaranteed -- message content hold that text too -- but a false
// positive cost one wasted decode, a false negative lose tokens.
var usageProbe = []byte(`"usage"`)

// FileCursor record how far one transcript file consumed, plus its totals.
type FileCursor struct {
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
	// Id of last counted message. Carried across runs = dedup survive one
	// incremental boundary.
	LastMessageID string `json:"lastMessageID"`
	Totals        Totals `json:"totals"`
}

// scanFile advance cur over bytes appended since last scan.
//
// Shrunk file rescanned whole: transcripts append-only, so a smaller size mean
// that file got replaced.
func scanFile(path string, cur FileCursor) (FileCursor, error) {
	info, err := os.Stat(path)
	if err != nil {
		return cur, err
	}
	size := info.Size()

	if size < cur.Offset {
		cur = FileCursor{}
	}
	if size == cur.Offset {
		cur.Size = size
		return cur, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return cur, err
	}
	defer f.Close()

	if cur.Offset > 0 {
		if _, err := f.Seek(cur.Offset, io.SeekStart); err != nil {
			return cur, err
		}
	}

	r := bufio.NewReaderSize(f, 256*1024)
	consumed := cur.Offset

	for {
		// ReadBytes grow to fit. Transcript line reach megabytes, past
		// bufio.Scanner 64KB token ceiling.
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			applyLine(&cur, line)
		}
		// Fragment without newline = write in progress. Offset stay short of it
		// so that line get read once complete.
		if err != nil {
			break
		}
	}

	cur.Offset = consumed
	cur.Size = size
	return cur, nil
}

// applyLine fold one complete transcript line into cursor.
func applyLine(cur *FileCursor, line []byte) {
	if !bytes.Contains(line, usageProbe) {
		return
	}
	var e entry
	// Undecodable line skipped in silence. Partial write and future record
	// shape both land here; neither justify blanking a status line.
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}
	if e.Type != "assistant" || e.Message == nil || e.Message.Usage == nil {
		return
	}
	if e.Message.Model == syntheticModel {
		return
	}
	if e.Message.ID != "" && e.Message.ID == cur.LastMessageID {
		return
	}
	cur.LastMessageID = e.Message.ID
	cur.Totals.Add(Totals{
		Input:      e.Message.Usage.InputTokens,
		CacheWrite: e.Message.Usage.CacheCreationTokens,
		CacheRead:  e.Message.Usage.CacheReadTokens,
		Output:     e.Message.Usage.OutputTokens,
	})
}

// Scope select which transcript files feed a total.
type Scope string

const (
	// Running session transcript only.
	ScopeSession Scope = "session"
	// Every transcript recorded for this project.
	ScopeProject Scope = "project"
)

// filesFor resolve a scope to files it cover.
//
// Subagent transcripts sit in sibling agent-*.jsonl, not as flagged lines in
// main one. Sidechain = which files to open, not which lines to keep.
func filesFor(transcriptPath string, scope Scope, includeSidechain bool) ([]string, error) {
	if scope != ScopeProject {
		return []string{transcriptPath}, nil
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(transcriptPath), "*.jsonl"))
	if err != nil {
		return nil, err
	}
	files := matches[:0]
	for _, m := range matches {
		if !includeSidechain && strings.HasPrefix(filepath.Base(m), "agent-") {
			continue
		}
		files = append(files, m)
	}
	// Glob order already lexical. Sort make cache key and scan order explicit,
	// not incidental.
	sort.Strings(files)
	return files, nil
}

type Options struct {
	TranscriptPath   string
	Scope            Scope
	IncludeSidechain bool
}

// Scan return totals for a scope, reusing cache to skip bytes already counted.
// Caller persist returned cache.
//
// Unreadable file skipped, never fatal: a transcript rotate or vanish between
// renders, and one missing file must not blank its segment.
func Scan(opts Options, cache *Cache) (Totals, *Cache) {
	if cache == nil {
		cache = NewCache()
	}
	if opts.TranscriptPath == "" {
		return Totals{}, cache
	}

	files, err := filesFor(opts.TranscriptPath, opts.Scope, opts.IncludeSidechain)
	if err != nil {
		return Totals{}, cache
	}

	var total Totals
	live := make(map[string]FileCursor, len(files))
	for _, path := range files {
		cur, err := scanFile(path, cache.Files[path])
		if err != nil {
			continue
		}
		live[path] = cur
		total.Add(cur.Totals)
	}

	// Replace, not merge. Cursors for vanished files else accumulate forever.
	cache.Files = live
	return total, cache
}
