package segment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
// Extension point: anything builtins miss -- kubectl context, deploy status,
// ticket id -- land here with no new release.
//
// Two guards. Timeout, because status line re-run constantly and hung command
// take whole row down. Cache, because without one expensive command run on every
// keystroke-driven redraw.
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
	// Cancelled context kill direct child alone. Grandchild that inherited
	// stdout hold pipe open, and Output() read to EOF, so deadline pass
	// unenforced -- `sh -c "sleep 30"` block whole 30s under a 50ms budget.
	// WaitDelay close pipes and give up shortly after cancellation.
	cmd.WaitDelay = pipeDrain
	raw, err := cmd.Output()
	if err != nil {
		// Failed or timed-out command drop its own segment, rest of row intact.
		return empty
	}

	// First line only. Multi-line result break row.
	out := strings.TrimSpace(string(raw))
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	if out == "" {
		return empty
	}

	writeCommandCache(c, out)
	return commandResult(out)
}

// shell name interpreter for one command line. Windows ship no sh, so hardcoding
// it drop this segment there in silence -- worst failure shape, since user see
// no output and no error either.
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

	// Temp file then rename. Claude Code start render before previous one
	// finish, so two processes write here at once and reader must never see
	// half-written file.
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
	_ = os.Rename(name, commandCachePath(c))
}
