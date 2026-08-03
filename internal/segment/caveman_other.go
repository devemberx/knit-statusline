//go:build !unix && !windows

package segment

import (
	"errors"
	"os"
)

var errCavemanUnsupported = errors.New("caveman flag read unsupported on this platform")

// openCavemanFile refuse flag read off unix and windows.
//
// O_NOFOLLOW and O_NONBLOCK have no stand-in on plan9 or wasm, and os.Open
// alone grant symlink follow and blocking open nobody reasoned about. Refusal
// keep that guard without costing those GOOS whole package: segment drop, row
// still draw.
func openCavemanFile(_ string) (*os.File, error) {
	return nil, errCavemanUnsupported
}
