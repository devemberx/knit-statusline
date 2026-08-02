package segment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// cavemanCtx point segment at throwaway config root. Flag content written when
// non-empty; empty string mean no file at all.
func cavemanCtx(t *testing.T, flag string) Context {
	t.Helper()
	c := ctx(t, fixtures.Full, "caveman")
	c.ConfigDir = t.TempDir()
	if flag != "" {
		writeCavemanFlag(t, c.ConfigDir, flag)
	}
	return c
}

func writeCavemanFlag(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".caveman-active"), []byte(content), 0o644); err != nil {
		t.Fatalf("write flag: %v", err)
	}
}

func TestCavemanRendersActiveMode(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{
		{"full", "🦴 full"},
		{"lite", "🦴 lite"},
		{"ultra", "🦴 ultra"},
		{"wenyan-full", "🦴 wenyan-full"},
		{"compress", "🦴 compress"},
		// Hook write no trailing newline. Editor save one.
		{"ULTRA\n", "🦴 ultra"},
		{"  full  ", "🦴 full"},
	} {
		if got := draw(cavemanCtx(t, tc.flag)); got != tc.want {
			t.Errorf("flag %q rendered %q, want %q", tc.flag, got, tc.want)
		}
	}
}

// Plugin absent, deactivated, or flag holding something nobody wrote on
// purpose. Every one of these is "caveman not running", and a row that say so
// out loud is noise.
func TestCavemanRendersNothing(t *testing.T) {
	for name, flag := range map[string]string{
		"deactivated":   "off",
		"empty file":    "\n",
		"unknown mode":  "verbose",
		"escape bytes":  "\x1b[2Jroot",
		"over 64 bytes": strings.Repeat("a", 70),
	} {
		if got := draw(cavemanCtx(t, flag)); got != "" {
			t.Errorf("%s rendered %q, want nothing", name, got)
		}
	}

	t.Run("no flag file", func(t *testing.T) {
		if got := draw(cavemanCtx(t, "")); got != "" {
			t.Errorf("rendered %q, want nothing", got)
		}
	})

	// preview and doctor may run before home directory resolve.
	t.Run("no config dir", func(t *testing.T) {
		c := ctx(t, fixtures.Full, "caveman")
		c.ConfigDir = ""
		if got := draw(c); got != "" {
			t.Errorf("rendered %q, want nothing", got)
		}
	})
}

// Flag pointed at ~/.ssh/id_rsa would print that file on every keystroke.
func TestCavemanRefusesSymlink(t *testing.T) {
	c := ctx(t, fixtures.Full, "caveman")
	c.ConfigDir = t.TempDir()

	target := filepath.Join(t.TempDir(), "real-flag")
	if err := os.WriteFile(target, []byte("full"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(c.ConfigDir, ".caveman-active")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	if got := draw(c); got != "" {
		t.Errorf("followed symlink and rendered %q, want nothing", got)
	}
}

// Lstat and Open are two syscalls. Whoever write config root swap flag for
// symlink in between, and Open land on target Lstat never saw. Handing
// readSameFile FileInfo of another file stand in for that window, microseconds
// wide and never lost on purpose.
func TestCavemanRefusesFileSwappedAfterStat(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, ".caveman-active")
	if err := os.WriteFile(flag, []byte("full"), 0o644); err != nil {
		t.Fatalf("write flag: %v", err)
	}
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("ultra"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}

	fi, err := os.Lstat(other)
	if err != nil {
		t.Fatalf("lstat other: %v", err)
	}
	if got, ok := readSameFile(flag, fi); ok {
		t.Errorf("read %q under another file's stat and got %q, want refusal", flag, got)
	}

	fi, err = os.Lstat(flag)
	if err != nil {
		t.Fatalf("lstat flag: %v", err)
	}
	if got, ok := readSameFile(flag, fi); !ok || got != "full" {
		t.Errorf("read own file gave %q, %v; want %q, true", got, ok, "full")
	}
}

func TestCavemanRefusesDirectory(t *testing.T) {
	c := ctx(t, fixtures.Full, "caveman")
	c.ConfigDir = t.TempDir()
	if err := os.Mkdir(filepath.Join(c.ConfigDir, ".caveman-active"), 0o755); err != nil {
		t.Fatalf("mkdir flag: %v", err)
	}
	if got := draw(c); got != "" {
		t.Errorf("read directory and rendered %q, want nothing", got)
	}
}

// Icon droppable without rewriting whole line.
func TestCavemanModeFieldAlone(t *testing.T) {
	c := cavemanCtx(t, "ultra")
	c.Cfg.Template = "{mode}"
	if got := draw(c); got != "ultra" {
		t.Errorf("rendered %q, want %q", got, "ultra")
	}
}

func writeCavemanSavings(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(dir, ".caveman-statusline-suffix")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write savings: %v", err)
	}
}

// Savings out of default template. Reader asking for it write it in.
func TestCavemanSavingsOnlyWhenTemplateAskForIt(t *testing.T) {
	c := cavemanCtx(t, "full")
	writeCavemanSavings(t, c.ConfigDir, "⛏ 12.4k")

	if got := draw(c); got != "🦴 full" {
		t.Errorf("default template rendered %q, want %q", got, "🦴 full")
	}

	c.Cfg.Template = "{icon} {mode}{savings}"
	if got, want := draw(c), "🦴 full ⛏ 12.4k"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// /caveman-stats never ran. Template naming savings must not leave stray space.
func TestCavemanSavingsAbsent(t *testing.T) {
	c := cavemanCtx(t, "full")
	c.Cfg.Template = "{icon} {mode}{savings}"
	if got, want := draw(c), "🦴 full"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Savings string is free-form, so mode whitelist cannot guard it. Control byte
// removal is what stand between planted file and escape sequence.
//
// ESC dropped, "[2J" survive as printable text -- same trade command.go
// sanitize make. Sequence broken is what matter; residue draw no screen clear.
func TestCavemanSavingsStripsControlBytes(t *testing.T) {
	c := cavemanCtx(t, "full")
	writeCavemanSavings(t, c.ConfigDir, "\x1b[2J⛏ 12.4k\r\n")
	c.Cfg.Template = "{icon} {mode}{savings}"

	got := draw(c)
	for _, r := range []rune{0x1b, '\r', '\n'} {
		if strings.ContainsRune(got, r) {
			t.Errorf("rendered %q, still carry %q", got, r)
		}
	}
	if !strings.Contains(got, "⛏ 12.4k") {
		t.Errorf("rendered %q, savings text gone", got)
	}
}

func TestCavemanSavingsRefusesSymlink(t *testing.T) {
	c := cavemanCtx(t, "full")

	target := filepath.Join(t.TempDir(), "real-savings")
	if err := os.WriteFile(target, []byte("⛏ 12.4k"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(c.ConfigDir, ".caveman-statusline-suffix")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	// Mode still render: savings is garnish, its failure cost only itself.
	c.Cfg.Template = "{icon} {mode}{savings}"
	if got, want := draw(c), "🦴 full"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// No mode = no segment, savings file or not.
func TestCavemanSavingsWithoutMode(t *testing.T) {
	c := cavemanCtx(t, "off")
	writeCavemanSavings(t, c.ConfigDir, "⛏ 12.4k")
	c.Cfg.Template = "{icon} {mode}{savings}"
	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}
