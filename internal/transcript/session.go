package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
)

// State say whether session already produced API usage.
type State int

const (
	// StateFresh: session sent nothing yet, so zero is a fact.
	StateFresh State = iota
	// StateLive: usage exist, or freshness unprovable.
	StateLive
)

// SessionState classify one session transcript.
//
// Every error resolve StateLive. Wrong StateFresh print zero where truth is
// unknown, and a lying number cost more than a missing one.
//
// Scan stop at first countable entry, so live transcript read only its opening
// bytes however large it grew.
func SessionState(path string) State {
	if path == "" {
		return StateLive
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return StateFresh
		}
		return StateLive
	}
	defer f.Close()

	// Same 256KB reader scanFile use: transcript line reach megabytes, past
	// bufio.Scanner 64KB token ceiling.
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && live(line) {
			return StateLive
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return StateFresh
			}
			return StateLive
		}
	}
}

// live report whether one line prove session already produced usage.
//
// Undecodable line still carrying "usage" count as proof: half-written entry and
// future record shape both land there, and neither justify claiming fresh.
func live(line []byte) bool {
	if !bytes.Contains(line, usageProbe) {
		return false
	}
	var e entry
	if json.Unmarshal(line, &e) != nil {
		return true
	}
	return e.countable()
}
