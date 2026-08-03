//go:build !unix

package segment

import "os"

// openConfigFile open path for reading.
//
// No O_NONBLOCK outside unix. Windows named pipe live under \\.\pipe\, not
// under config root or project directory, so no rename put one at these paths.
//
// File name carry no GOOS suffix on purpose: configcount_windows.go would take
// implicit windows-only constraint on top of !unix, leaving plan9 and wasm with
// no openConfigFile at all.
func openConfigFile(path string) (*os.File, error) {
	return os.Open(path)
}
