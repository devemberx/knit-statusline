//go:build unix

package segment

import (
	"os"
	"syscall"
)

// openConfigFile open path for reading, never blocking on it.
//
// O_NONBLOCK cover FIFO renamed onto path after Stat clear it: open of FIFO
// otherwise wait for writer that never come, and render carry no deadline to
// cut that wait. Handle's own Stat then refuse it.
//
// No O_NOFOLLOW: settings.json pointed into dotfiles checkout is normal here,
// unlike caveman flag.
func openConfigFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
