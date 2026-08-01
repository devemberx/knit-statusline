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
