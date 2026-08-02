package segment

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("caveman", Def{
		Fields:          []string{"mode", "icon", "savings"},
		DefaultTemplate: "{icon} {mode}",
		Build:           buildCaveman,
	})
}

// Bone read as prehistoric at one character. Moai (U+1F5FF) is Easter Island,
// wrong era. Mammoth and rock are Unicode 13.0, thinner font coverage.
const cavemanIcon = "🦴"

// Flag caveman UserPromptSubmit hook write, under Claude Code config root.
const cavemanFlagFile = ".caveman-active"

// Pre-rendered string /caveman-stats write. Absent until that command run once.
const cavemanSavingsFile = ".caveman-statusline-suffix"

// Upstream caveman-statusline.sh read same 64 bytes. Cap bound row width and
// bound read: os.ReadFile would load planted gigabyte file whole.
const cavemanMaxBytes = 64

// Modes upstream whitelist. "off" left out on purpose -- it mean inactive, same
// row as missing file. Anything else render nothing rather than echo bytes
// somebody else planted.
var cavemanModes = []string{
	"lite", "full", "ultra",
	"wenyan-lite", "wenyan", "wenyan-full", "wenyan-ultra",
	"commit", "review", "compress",
}

func buildCaveman(c Context) Result {
	if c.ConfigDir == "" {
		return empty
	}
	raw, ok := readCavemanFile(filepath.Join(c.ConfigDir, cavemanFlagFile))
	if !ok {
		return empty
	}
	mode := cavemanMode(raw)
	if mode == "" {
		return empty
	}
	f := render.Fields{
		"icon": render.Colored(cavemanIcon, render.Orange),
		"mode": render.Colored(mode, render.Orange),
		// Template naming savings must not break when /caveman-stats never ran.
		"savings": render.Plain(""),
	}
	// Leading space sit inside field, same as limit's {reset}. Template writing
	// "{mode}{savings}" leave trailing space otherwise.
	if s := cavemanSavings(c.ConfigDir); s != "" {
		f["savings"] = render.Colored(" "+s, render.Dim)
	}
	return Result{Base: render.Orange, Fields: f}
}

// readCavemanFile read at most cavemanMaxBytes from regular file.
//
// Lstat before open: flag symlinked at ~/.ssh/id_rsa otherwise print that
// file's bytes every render.
func readCavemanFile(path string) (string, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return readSameFile(path, fi)
}

// readSameFile open path, refusing unless opened file is one fi describe.
//
// Lstat and Open are two syscalls, and between them path get renamed onto
// symlink -- Lstat clear it as regular, Open follow it anyway. Handle's own
// Stat close that window: identity checked on opened handle, not on name.
// Upstream caveman-config.js readFlag reach for O_NOFOLLOW instead;
// Windows have no such flag, os.SameFile run everywhere.
//
// Longer than cap = reject whole, not truncate -- nothing legitimate write past
// 64 bytes here.
func readSameFile(path string, fi os.FileInfo) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	opened, err := f.Stat()
	if err != nil || !os.SameFile(fi, opened) {
		return "", false
	}

	b, err := io.ReadAll(io.LimitReader(f, cavemanMaxBytes+1))
	if err != nil || len(b) > cavemanMaxBytes {
		return "", false
	}
	return string(b), true
}

// cavemanMode fold case and drop every rune outside [a-z0-9-].
//
// Row reach terminal unescaped, where "\x1b[2J" clear screen and
// "\x1b]0;...\a" retitle window. command.go sanitize guard command output;
// this path never pass through it.
func cavemanMode(raw string) string {
	mode := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		}
		return -1
	}, raw)

	if !slices.Contains(cavemanModes, mode) {
		return ""
	}
	return mode
}

// cavemanSavings read pre-rendered token count.
//
// Content free-form -- "⛏ 12.4k" carry pictograph and digits -- so mode
// whitelist cannot guard it and sanitize drop control bytes instead. Missing
// file cost this field alone, never mode beside it.
func cavemanSavings(dir string) string {
	raw, ok := readCavemanFile(filepath.Join(dir, cavemanSavingsFile))
	if !ok {
		return ""
	}
	return strings.TrimSpace(sanitize(raw))
}
