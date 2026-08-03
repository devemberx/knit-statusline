//go:build !unix

package segment

import "os"

// openNonblock open path for reading.
//
// No O_NONBLOCK outside unix. Windows named pipe live under \\.\pipe\, not under
// config root or project directory, so no rename put one at path either caller
// read.
//
// File name carry no GOOS suffix on purpose: open_windows.go would take implicit
// windows-only constraint on top of !unix, leaving plan9 and wasm with no open
// at all.
func openNonblock(path string) (*os.File, error) {
	return os.Open(path)
}

// openNonblockNoFollow is openNonblock outside unix: no O_NOFOLLOW here either.
//
// Caveman flag still refuse symlink one guard later -- os.Lstat name it, and
// os.SameFile reject opened handle whose id differ.
func openNonblockNoFollow(path string) (*os.File, error) {
	return openNonblock(path)
}
