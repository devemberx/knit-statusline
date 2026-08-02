// Package install wire knit-statusline into Claude Code settings.
//
// Plugin settings.json take only agent and subagentStatusLine, so user's own
// settings.json in config root is only way to set main status line. Same file
// hold their hooks and permissions, so every write merge, never replace.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/devemberx/knit-statusline/internal/config"
)

// BinaryPath name installed copy inside config root. Windows need .exe:
// CreateProcess refuse extensionless file, and Claude Code print nothing when
// status line command fail.
func BinaryPath(root string) string {
	name := "knit-statusline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, name)
}

// CommandString form statusLine.command for binary. Git Bash on Windows eat
// unquoted backslash: C:\Users\... arrive separator-less, row stay blank.
// Forward slashes run in every shell. Quote whole bash-unsafe set, not
// whitespace alone: O'Brien or A&B home break bash same way a space does.
// Quoted string is no command under PowerShell fallback, so bare form stay
// whenever bash allow it -- mid-word tilde (8.3 short name) and non-ASCII
// included. Double quote leave $ backtick " \ live in bash; escape those.
func CommandString(binary string) string {
	cmd := filepath.ToSlash(binary)
	if !strings.ContainsFunc(cmd, bashUnsafe) {
		return cmd
	}
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(cmd)
	return `"` + esc + `"`
}

// bashUnsafe report rune unquoted bash command word cannot carry. Safe list,
// not unsafe list: unknown punctuation quote, never slip through.
func bashUnsafe(c rune) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c > unicode.MaxASCII:
		// Non-ASCII mean nothing to bash or PowerShell.
		return false
	}
	return !strings.ContainsRune("-_./:+~", c)
}

// OwnsCommand report whether command invoke installed binary. Settings hold
// bare backslash, slashed, quoted or quoted-escaped form, so compare
// normalized, never literal.
func OwnsCommand(command, binary string) bool {
	cmd := unquoteCommand(strings.TrimSpace(command))
	if cmd == "" {
		return false
	}
	if SamePathText(cmd, binary) {
		return true
	}
	// 8.3 short home (C:\Users\RUNNER~1) and symlinked home name one binary by
	// two strings no rewrite reconcile. Stat settle it. Uninstall call this
	// before deleting binary, so file still there to stat.
	return SameFile(cmd, binary)
}

// unquoteCommand undo CommandString quoting. Bash double-quote rule: backslash
// stay literal unless it precede $ backtick " or \.
func unquoteCommand(cmd string) string {
	if len(cmd) < 2 || !strings.HasPrefix(cmd, `"`) || !strings.HasSuffix(cmd, `"`) {
		return cmd
	}
	inner := cmd[1 : len(cmd)-1]
	var b strings.Builder
	b.Grow(len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) && strings.IndexByte("$`\"\\", inner[i+1]) >= 0 {
			i++
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// SamePathText compare path as text. Windows fold case: settings may hold
// c:\users\... while os.UserHomeDir resolve C:\Users\...
//
// Exported: doctor's stray-root check reuse it as fallback when SameFile
// cannot stat one side.
func SamePathText(a, b string) bool {
	a, b = filepath.ToSlash(a), filepath.ToSlash(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// SameFile report both path name one file on disk. Stat only, never exec.
// Missing either side mean no.
//
// Exported: doctor's stray-root check reuse it for symlinked-home identity.
func SameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// Result report what ran, so command layer name specifics instead of claiming
// success generically.
type Result struct {
	SettingsPath string
	BackupPath   string
	ConfigPath   string
	ConfigWrote  bool
	// Path recorded in settings.json.
	InstalledBinary string
	// Previous statusLine command, when one was set.
	ReplacedCommand string
	// statusLine key removed. False when configured command belong to another tool.
	RemovedStatusLine bool
}

type Options struct {
	// Claude Code config root, already resolved. Empty and relative rejected.
	Root string
	// Running executable, copied into place by Install.
	Binary string
	Preset string
	// Overwrite existing statusline.toml.
	Force bool
}

// ErrRelativeRoot let caller name knob that supplied root. This package read no
// environment: relative value reach it from CLAUDE_CONFIG_DIR and from relative
// $HOME alike, so remedy is not ours to print.
var ErrRelativeRoot = errors.New("config root is relative")

// checkAbs refuse relative root. Empty root already rejected, but relative one
// resolve too -- against cwd of whichever process read it. Claude Code resolve
// CLAUDE_CONFIG_DIR against its own directory, install against user's shell,
// so one value name two roots and row point at binary nobody installed.
// Written command inherit that: settings.json record relative path Claude Code
// resolve for itself again.
//
// Render path take no such check. It must resolve root exactly as Claude Code
// do, and never-blank rule forbid refusing config Claude Code itself load.
func checkAbs(root string) error {
	if filepath.IsAbs(root) {
		return nil
	}
	return fmt.Errorf("%w: %q resolves against whichever directory reads it", ErrRelativeRoot, root)
}

// Install copy this binary beside user's settings, point statusLine at copy,
// lay down starting config.
//
// Copy because npx run binary out of a package cache npm prune at will, and
// vanished command render empty row explaining nothing.
func Install(opts Options) (*Result, error) {
	// Empty root resolve every path against cwd, dropping settings.json and a
	// binary into whatever directory this ran from.
	if opts.Root == "" {
		return nil, errors.New("no config directory")
	}
	if err := checkAbs(opts.Root); err != nil {
		return nil, err
	}
	if opts.Preset == "" {
		opts.Preset = config.DefaultPreset
	}
	preset, err := config.PresetSource(opts.Preset)
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q; available: %v", opts.Preset, config.PresetNames())
	}

	res := &Result{
		SettingsPath:    SettingsPath(opts.Root),
		ConfigPath:      config.UserPath(opts.Root),
		InstalledBinary: BinaryPath(opts.Root),
	}

	settings, err := readSettings(res.SettingsPath)
	if err != nil {
		return nil, err
	}
	res.ReplacedCommand = statusLineCommand(settings)

	if err := copyBinary(opts.Binary, res.InstalledBinary); err != nil {
		return nil, fmt.Errorf("installing the binary: %w", err)
	}

	// settings.json hold user's hooks and permissions, so keep a copy before
	// merging. Binary land first on purpose: reverse order point statusLine at
	// a binary that may never arrive.
	backup, err := backupFile(res.SettingsPath)
	if err != nil {
		return nil, err
	}
	res.BackupPath = backup

	settings["statusLine"] = map[string]any{
		"type":    "command",
		"command": CommandString(res.InstalledBinary),
	}
	if err := writeJSON(res.SettingsPath, settings); err != nil {
		return nil, err
	}

	// Existing statusline.toml is user's own work. Never overwrite without
	// --force: reinstalling usually mean updating binary path, not discarding a
	// layout.
	if _, err := os.Stat(res.ConfigPath); err == nil && !opts.Force {
		return res, nil
	}
	if err := os.MkdirAll(filepath.Dir(res.ConfigPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(res.ConfigPath, preset, 0o644); err != nil {
		return nil, err
	}
	res.ConfigWrote = true
	return res, nil
}

// Uninstall drop our statusLine key and installed binary, every other setting
// untouched. statusline.toml stay: user's own config, and removing it turn
// reinstall into fresh start instead of resumption.
func Uninstall(root string) (*Result, error) {
	// Empty root resolve every path against cwd, so os.Remove below would hunt a
	// binary in whatever directory this ran from.
	if root == "" {
		return nil, errors.New("no config directory")
	}
	if err := checkAbs(root); err != nil {
		return nil, err
	}
	res := &Result{
		SettingsPath:    SettingsPath(root),
		ConfigPath:      config.UserPath(root),
		InstalledBinary: BinaryPath(root),
	}

	settings, err := readSettings(res.SettingsPath)
	if err != nil {
		return nil, err
	}
	res.ReplacedCommand = statusLineCommand(settings)

	// Only key we installed is ours to delete. User who switched status line
	// tools by hand keep that tool's config, and missing key match nothing.
	if OwnsCommand(res.ReplacedCommand, res.InstalledBinary) {
		backup, err := backupFile(res.SettingsPath)
		if err != nil {
			return nil, err
		}
		res.BackupPath = backup

		delete(settings, "statusLine")
		if err := writeJSON(res.SettingsPath, settings); err != nil {
			return nil, err
		}
		res.RemovedStatusLine = true
	}

	// Copy is ours whatever statusLine now point at, so remove it even when key
	// was hand-deleted. May be binary running right now; fine on Unix, process
	// keep its open file. Failure leave nothing to undo.
	_ = os.Remove(res.InstalledBinary)
	return res, nil
}

// copyBinary place running executable at dst. Temp file then replace: dst may
// be binary a running Claude Code is about to invoke, and in-place write hand
// that invocation half-written file.
func copyBinary(src, dst string) error {
	if src == "" {
		return errors.New("no source binary")
	}
	// Both side absolutised, else guard miss whenever one path is relative.
	// Reinstall from ~/.claude reach here with src and dst naming same file.
	if abs, err := filepath.Abs(src); err == nil {
		src = abs
	}
	if abs, err := filepath.Abs(dst); err == nil {
		dst = abs
	}
	if src == dst {
		return nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".knit-statusline-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return err
	}
	return replaceFile(name, dst)
}
