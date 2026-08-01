// Package transcript accumulate token usage from Claude Code transcripts.
//
// One JSONL line per content block, each repeating whole usage object for its
// message. Naive sum over-count 3x: measured session, 483 entries over 220
// messages, input 62,093 read as 195,836. Dedup on message.id required.
//
// Repeats contiguous across eight large transcripts, so one remembered id
// enough. Id persist in cursor, else message straddling incremental boundary
// double-count and totals jitter between renders.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entries Claude Code generate locally. Carry usage, bill nothing.
const syntheticModel = "<synthetic>"

// Totals keep four counters apart: cache read run orders of magnitude over
// fresh input and price differently. One merged "input" figure mislead.
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

// Minimal shape. Line carry far more; decoding only these keep scan cheap.
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

// countable report whether decoded entry contribute to Totals.
//
// Probe share it: two copies of this filter drift, and probe disagreeing with
// counter mark session live that show zero tokens.
func (e entry) countable() bool {
	return e.Type == "assistant" && e.Message != nil &&
		e.Message.Usage != nil && e.Message.Model != syntheticModel
}

// Prefilter. Line without this substring carry no message.usage. Reverse not
// guaranteed -- content hold that text too -- but false positive cost one
// wasted decode, false negative lose tokens.
var usageProbe = []byte(`"usage"`)

// FileCursor record how far one transcript file consumed, plus its totals.
type FileCursor struct {
	Offset int64 `json:"offset"`
	// Last counted message. Persisted = dedup survive incremental boundary.
	LastMessageID string `json:"lastMessageID"`
	Totals        Totals `json:"totals"`
}

// scanFile advance cur over bytes appended since last scan. Shrunk file mean
// replacement, not append -- transcripts append-only -- so rescan whole.
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
		// ReadBytes grow to fit. Line reach megabytes, past bufio.Scanner 64KB
		// token ceiling.
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			applyLine(&cur, line)
		}
		// Fragment without newline = write in progress. Offset stay short of it,
		// line counted once complete.
		if err != nil {
			break
		}
	}

	cur.Offset = consumed
	return cur, nil
}

// applyLine fold one complete transcript line into cursor.
func applyLine(cur *FileCursor, line []byte) {
	if !bytes.Contains(line, usageProbe) {
		return
	}
	var e entry
	// Undecodable line skipped in silence. Partial write and future record shape
	// both land here; neither justify blanking a status line.
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}
	if !e.countable() {
		return
	}
	// Id-less entry leave guard alone. Overwriting with "" disarm dedup, so next
	// repeat of preceding id count twice.
	if e.Message.ID != "" {
		if e.Message.ID == cur.LastMessageID {
			return
		}
		cur.LastMessageID = e.Message.ID
	}
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

// filesFor resolve scope to files it cover. Subagent transcripts sit in sibling
// agent-*.jsonl, so sidechain decide which files open, not which lines keep.
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
	// Glob order already lexical. Sort make scan order explicit, not incidental.
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
	cursors := make(map[string]FileCursor, len(files))
	for _, path := range files {
		prev, cached := cache.Files[path]
		cur, err := scanFile(path, prev)
		if err != nil {
			// Gone file drop out. Every other error transient -- EMFILE, EIO,
			// permission blip -- so hold last cursor: total stay put and next
			// render resume instead of rescanning cold.
			if cached && !errors.Is(err, fs.ErrNotExist) {
				cursors[path] = prev
				total.Add(prev.Totals)
			}
			continue
		}
		cursors[path] = cur
		total.Add(cur.Totals)
	}

	// Replace, not merge. Cursors for vanished files else accumulate forever.
	cache.Files = cursors
	return total, cache
}
