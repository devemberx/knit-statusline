//go:build unix

package segment

import (
	"os"
	"syscall"
)

// openCavemanFile open path for reading, refusing symlink and never blocking.
//
// O_NOFOLLOW is what upstream caveman-config.js readFlag reach for. It refuse
// at open, so target carrying side effect on open -- /dev/watchdog arm reboot
// timer -- never get touched; os.SameFile judge only once open return.
//
// O_NONBLOCK cover FIFO renamed onto path, and device node waiting on carrier:
// open otherwise wait for writer or carrier that never come, and render carry
// no deadline to cut that wait.
//
// Regular file ignore both flag, so read below behave same as os.Open.
func openCavemanFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
