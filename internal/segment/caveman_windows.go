//go:build windows

package segment

import "os"

// openCavemanFile open path for reading.
//
// Windows have no O_NOFOLLOW and no O_NONBLOCK, so os.SameFile in readIfSame
// stay only guard. Named pipe there live under \\.\pipe\, not under config
// root, so no rename put one at this path.
//
// Constraint say windows, not !unix: file name already pin GOOS, so !unix here
// read as cover for plan9 and wasm it never give. caveman_other.go answer those
// with refusal instead, weaker guard nobody reasoned about being worse.
func openCavemanFile(path string) (*os.File, error) {
	return os.Open(path)
}
