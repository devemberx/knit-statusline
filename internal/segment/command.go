package segment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("command", Def{
		Fields:          []string{"out"},
		DefaultTemplate: "{out}",
		Build:           buildCommand,
	})
}

// buildCommand run user-supplied shell command.
//
// Extension point: kubectl context, deploy status, ticket id -- anything
// builtins miss land here with no new release.
//
// Timeout because status line re-run constantly and hung command take whole row
// down. Cache because expensive command otherwise run every redraw.
func buildCommand(c Context) Result {
	if c.Cfg.Command == "" {
		return empty
	}

	if out, ok := readCommandCache(c); ok {
		return commandResult(out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.budget())
	defer cancel()

	name, args := shell(c.Cfg.Command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.In.Dir()
	// Cancelled context kill direct child alone. Grandchild inherit stdout and
	// hold pipe open, so read run to EOF past deadline -- `sh -c "sleep 30"`
	// block whole 30s under 50ms budget. WaitDelay close pipes after cancel.
	cmd.WaitDelay = pipeDrain

	// Timeout cap how long command run, never how much it print. `cat
	// /dev/urandom` fill memory well inside 1s.
	var buf strings.Builder
	lw := &limitWriter{w: &buf, left: maxCommandBytes}
	cmd.Stdout = lw
	// Cap reached is success: exec close pipe, command die of SIGPIPE, Run
	// report that exit instead of write error.
	if err := cmd.Run(); err != nil && !lw.full {
		// Failed or timed-out command drop its own segment, rest of row intact.
		return empty
	}

	out := sanitize(firstLine(buf.String()))
	if out == "" {
		return empty
	}

	writeCommandCache(c, out)
	return commandResult(out)
}

// Everything past first line drop anyway, and 4 KiB far past one terminal row.
const maxCommandBytes = 4 << 10

var errCommandOutputFull = errors.New("command output limit reached")

// full mark cap reached, since caller read that as success and exec surface it
// as SIGPIPE exit.
type limitWriter struct {
	w    io.Writer
	left int
	full bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.left <= 0 {
		l.full = true
		return 0, errCommandOutputFull
	}
	if len(p) > l.left {
		n, _ := l.w.Write(p[:l.left])
		l.left = 0
		l.full = true
		return n, errCommandOutputFull
	}
	n, err := l.w.Write(p)
	l.left -= n
	return n, err
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sanitize drop control characters. Output reach terminal unescaped, where
// "\x1b[2J" clear screen, "\x1b]0;...\a" retitle window, "\r" overwrite drawn row.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 0x1b || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// Windows ship no sh. Hardcoding it drop segment there in silence -- no output,
// no error, worst failure shape.
func shell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

func commandResult(out string) Result {
	return Result{
		Base:   render.White,
		Fields: render.Fields{"out": render.Colored(out, render.White)},
	}
}

type commandCache struct {
	ExpiresAt int64  `json:"expiresAt"`
	Output    string `json:"output"`
}

// Key on segment name plus command text: editing command invalidate its cache
// instead of serving previous one's output.
func commandCachePath(c Context) string {
	h := sha256.Sum256([]byte(c.Cfg.Name + "\x00" + c.Cfg.Command))
	return filepath.Join(c.CacheDir, "cmd-"+hex.EncodeToString(h[:8])+".json")
}

func readCommandCache(c Context) (string, bool) {
	if c.Cfg.CacheMS <= 0 || c.CacheDir == "" {
		return "", false
	}
	b, err := os.ReadFile(commandCachePath(c))
	if err != nil {
		return "", false
	}
	var cc commandCache
	if err := json.Unmarshal(b, &cc); err != nil {
		return "", false
	}
	if c.Now.UnixMilli() >= cc.ExpiresAt {
		return "", false
	}
	return cc.Output, true
}

func writeCommandCache(c Context, out string) {
	if c.Cfg.CacheMS <= 0 || c.CacheDir == "" {
		return
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(commandCache{
		ExpiresAt: c.Now.UnixMilli() + int64(c.Cfg.CacheMS),
		Output:    out,
	})
	if err != nil {
		return
	}

	// Temp then rename. Claude Code start render before previous finish, so two
	// process write here at once and reader must never see half-written file.
	tmp, err := os.CreateTemp(c.CacheDir, ".cmd-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(name, commandCachePath(c)); err != nil {
		return
	}
	sweepCommandCache(c)
}

// Entry go stale two ways: editing command move its key, dropping segment
// strand its file. Neither delete anything.
const commandCacheTTL = 7 * 24 * time.Hour

// sweepCommandCache delete cmd-*.json untouched for commandCacheTTL.
//
// Run after write, so cached render pay nothing. Live entry get mtime refreshed
// by that same write. Errors ignored -- cache disposable.
func sweepCommandCache(c Context) {
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		return
	}
	cutoff := c.Now.Add(-commandCacheTTL)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cmd-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(c.CacheDir, name))
	}
}
