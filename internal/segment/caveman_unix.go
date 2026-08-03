//go:build unix

package segment

import (
	"os"
	"syscall"
)

// openFlagFile open path for reading, refusing symlink and never blocking.
//
// O_NOFOLLOW is what upstream caveman-config.js readFlag reach for. O_NONBLOCK
// cover FIFO renamed onto path: open of FIFO otherwise wait for writer that
// never come, and render carry no deadline to cut that wait.
//
// Regular file ignore both flag, so read below behave same as os.Open.
func openFlagFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
