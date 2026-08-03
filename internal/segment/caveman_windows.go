//go:build !unix

package segment

import "os"

// openFlagFile open path for reading.
//
// Windows have no O_NOFOLLOW and no O_NONBLOCK, so os.SameFile in readIfSame
// stay only guard. Named pipe there live under \\.\pipe\, not under config
// root, so no rename put one at this path.
func openFlagFile(path string) (*os.File, error) {
	return os.Open(path)
}
