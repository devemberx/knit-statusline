//go:build !unix

package segment

import "os"

// openConfigFile open path for reading.
//
// Windows have no O_NONBLOCK. Named pipe there live under \\.\pipe\, not under
// config root or project directory, so no rename put one at these paths.
func openConfigFile(path string) (*os.File, error) {
	return os.Open(path)
}
