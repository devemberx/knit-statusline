//go:build unix

package segment

import (
	"os"
	"syscall"
)

// openNonblock open path for reading without waiting on it.
//
// O_NONBLOCK cover FIFO renamed onto path after Stat clear it as regular, and
// device node beside it: open otherwise wait for writer or carrier that never
// come, and render carry no deadline to cut that wait. Regular file ignore it,
// so read behave same as os.Open.
//
// Caller still check opened handle's own Stat. Read on nonblocking FIFO fd park
// in runtime poller on linux and return EOF on darwin, so flag buy return from
// open, never refusal of what open land on.
func openNonblock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

// openNonblockNoFollow add symlink refusal to openNonblock.
//
// O_NOFOLLOW refuse at open, so target carrying side effect on open --
// /dev/watchdog arm reboot timer -- never get touched, where os.SameFile judge
// only once open return. Upstream caveman-config.js readFlag reach for it too.
func openNonblockNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
