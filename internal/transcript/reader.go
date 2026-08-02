package transcript

import (
	"bufio"
	"io"
	"os"
)

// scanAppended feed apply every complete line added to path since offset, and
// report where it stopped.
//
// Shrunk file mean replacement, not append -- transcripts append-only -- so
// reset run and scan start from 0. Caller drop its own aggregate there: totals,
// dedup guard and todo counts all describe a document that is gone. Reset run
// before any line, else first lines of new file fold into old state.
//
// Error return leave offset where caller had it, so a transient EMFILE or
// permission blip resume rather than rescan cold. Exception: error after
// shrink hit, offset already 0 and reset() already fired, so that path
// rescans cold on next call regardless.
func scanAppended(path string, offset int64, reset func(), apply func(line []byte)) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return offset, err
	}
	size := info.Size()

	if size < offset {
		offset = 0
		reset()
	}
	if size == offset {
		return offset, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return offset, err
		}
	}

	r := bufio.NewReaderSize(f, 256*1024)
	consumed := offset

	for {
		// ReadBytes grow to fit. Line reach megabytes, past bufio.Scanner 64KB
		// token ceiling.
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			apply(line)
		}
		// Fragment without newline = write in progress. Offset stay short of it,
		// line counted once complete.
		if err != nil {
			break
		}
	}

	return consumed, nil
}
