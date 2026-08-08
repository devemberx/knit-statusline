package segment

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

// .claude.json carry per-project session metrics beside usage block, so machine
// with many projects run past one megabyte. Parse run near 8ms per MiB against
// ~41ms whole row, so 4 MiB is where reading cost more than it tell.
const maxClaudeJSONBytes = 4 << 20

var (
	errConfigTooBig     = errors.New("config file over size cap")
	errConfigNotRegular = errors.New("config path not regular file")
)

// readConfigJSON decode capped regular file. Caller separate fs.ErrNotExist --
// absent file prove zero, unreadable one prove nothing.
func readConfigJSON(path string, limit int64, v any) error {
	b, err := readCappedFile(path, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// readCappedFile read regular file up to limit.
//
// Stat first keep non-regular path away from open at all: .claude.json as FIFO
// hold blocking open until some writer arrive, and render path carry no timeout
// to cut that. Blocked render print nothing, which read as crash. Outside unix
// that Stat stand as only guard before open. Symlink followed on purpose --
// config file pointed into dotfiles checkout is normal, and no byte read here
// reach row.
func readCappedFile(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errConfigNotRegular
	}
	return readRegular(path, limit)
}

// readRegular open path and read it, refusing anything but regular file.
//
// Stat above and open here are two syscalls, and between them path get renamed
// onto FIFO -- Stat clear it as regular, open block on it anyway. openNonblock
// return without waiting on unix; handle's own Stat then refuse what name no
// longer describe.
//
// That Stat not redundant with O_NONBLOCK: read on nonblocking FIFO fd park in
// runtime poller on linux, and return EOF on darwin -- empty bytes counted as
// zero config.
//
// openNonblock, not openNonblockNoFollow: .claude.json pointed into dotfiles
// checkout is normal here, unlike caveman flag.
func readRegular(path string, limit int64) ([]byte, error) {
	f, err := openNonblock(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errConfigNotRegular
	}

	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errConfigTooBig
	}
	return b, nil
}
