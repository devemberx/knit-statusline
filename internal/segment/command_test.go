package segment

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// commandCtx configure command segment, since its settings come from user
// config rather than from stdin.
func commandCtx(t *testing.T, command string) Context {
	t.Helper()
	c := ctx(t, fixtures.Full, "command")
	c.Cfg.Name = "probe"
	c.Cfg.Command = command
	c.CacheDir = t.TempDir()
	// Fixture cwd never exist on disk. Command inherit it otherwise and fail
	// before running.
	c.In.Workspace.CurrentDir = t.TempDir()
	return c
}

// echo exist as shell builtin on both sh and cmd, so one command line serve
// every runner.
func echoOf(s string) string { return "echo " + s }

func TestBuildCommandIsEmptyWithoutACommand(t *testing.T) {
	if res := Build(commandCtx(t, "")); !res.Empty {
		t.Errorf("got %+v, want empty", res)
	}
}

func TestBuildCommandRendersOutput(t *testing.T) {
	if got := draw(commandCtx(t, echoOf("staging"))); got != "staging" {
		t.Errorf("rendered %q, want staging", got)
	}
}

// Multi-line result break row, so only first line reach template.
func TestBuildCommandKeepsFirstLineOnly(t *testing.T) {
	c := commandCtx(t, "printf 'one\\ntwo\\n'")
	if runtime.GOOS == "windows" {
		t.Skip("printf is no cmd builtin")
	}
	if got := draw(c); got != "one" {
		t.Errorf("rendered %q, want one", got)
	}
}

// Failed command drop its own segment, rest of row intact. Exit status alone
// must not reach user as error text.
func TestBuildCommandIsEmptyOnFailure(t *testing.T) {
	if res := Build(commandCtx(t, "exit 7")); !res.Empty {
		t.Errorf("got %+v, want empty", res)
	}
}

func TestBuildCommandIsEmptyOnBlankOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Bare echo print "ECHO is on." there, so silence need another command.
		t.Skip("no portable way to print nothing from cmd")
	}
	if res := Build(commandCtx(t, "echo")); !res.Empty {
		t.Errorf("got %+v, want empty", res)
	}
}

// Status line re-run constantly, so hung command must lose its slot rather than
// take whole row down with it.
func TestBuildCommandHonoursTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep is no cmd builtin")
	}
	c := commandCtx(t, "sleep 30")
	c.Cfg.TimeoutMS = 50

	start := time.Now()
	res := Build(c)
	took := time.Since(start)

	if !res.Empty {
		t.Errorf("timed-out command returned %+v, want empty", res)
	}
	if took > 5*time.Second {
		t.Errorf("Build took %v despite a 50ms timeout", took)
	}
}

// Without cache, expensive command run on every keystroke-driven redraw.
func TestCommandCacheServesStoredOutput(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	c.Cfg.CacheMS = 60_000

	if got := draw(c); got != "first" {
		t.Fatalf("first render = %q, want first", got)
	}

	if out, ok := readCommandCache(c); !ok || out != "first" {
		t.Errorf("readCommandCache = %q, %v; want first, true", out, ok)
	}
}

func TestCommandCacheExpires(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	c.Cfg.CacheMS = 1_000

	if got := draw(c); got != "first" {
		t.Fatalf("first render = %q", got)
	}

	c.Now = c.Now.Add(2 * time.Second)
	if _, ok := readCommandCache(c); ok {
		t.Error("cache served an entry past its expiry")
	}
}

// Editing command must invalidate its cache rather than serve previous one's
// output under a name that no longer produce it.
func TestCommandCacheKeyTracksCommandText(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	c.Cfg.CacheMS = 60_000
	if got := draw(c); got != "first" {
		t.Fatalf("first render = %q", got)
	}

	edited := c
	edited.Cfg.Command = echoOf("second")
	if _, ok := readCommandCache(edited); ok {
		t.Error("edited command served the previous command's cache")
	}
	if got := draw(edited); got != "second" {
		t.Errorf("rendered %q, want second", got)
	}
}

// Cache disabled by default: writing one unasked leave files behind for every
// command segment anybody ever configure.
func TestCommandCacheStaysOffWhenUnset(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	if got := draw(c); got != "first" {
		t.Fatalf("first render = %q", got)
	}

	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("cache_ms unset still wrote %d file(s)", len(entries))
	}
}

// Two renders overlap, so reader must see one complete file or none. Temp file
// left behind mean rename never happened.
func TestCommandCacheWritesAtomically(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	c.Cfg.CacheMS = 60_000
	draw(c)

	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(commandCachePath(c)); err != nil {
		t.Errorf("cache file missing: %v", err)
	}
}

func TestCommandCacheToleratesGarbage(t *testing.T) {
	c := commandCtx(t, echoOf("first"))
	c.Cfg.CacheMS = 60_000
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandCachePath(c), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := readCommandCache(c); ok {
		t.Error("corrupt cache reported as usable")
	}
	if got := draw(c); got != "first" {
		t.Errorf("rendered %q, want first -- corrupt cache should rebuild", got)
	}
}

// Cache key hash segment name too, so two command segments never collide.
func TestCommandCachePathSeparatesSegments(t *testing.T) {
	a := commandCtx(t, echoOf("x"))
	b := a
	b.Cfg.Name = "other"

	if filepath.Base(commandCachePath(a)) == filepath.Base(commandCachePath(b)) {
		t.Error("two segment names share one cache file")
	}
}

// Windows ship no sh. Hardcoding it drop this segment there in silence, which is
// worst failure shape: no output and no error either.
func TestShellPicksInterpreterPerPlatform(t *testing.T) {
	name, args := shell("echo hi")
	if runtime.GOOS == "windows" {
		if name != "cmd" || args[0] != "/c" {
			t.Errorf("windows shell = %q %v, want cmd /c", name, args)
		}
		return
	}
	if name != "sh" || args[0] != "-c" {
		t.Errorf("unix shell = %q %v, want sh -c", name, args)
	}
}
