// Package install wire knit-statusline into Claude Code settings.
//
// Plugin settings.json take only agent and subagentStatusLine, so user's own
// ~/.claude/settings.json is only way to set main status line. Same file hold
// their hooks and permissions, so every write merge, never replace.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/devemberx/knit-statusline/internal/config"
)

// BinaryPath name installed copy. Windows need .exe: CreateProcess refuse
// extensionless file, and Claude Code print nothing when status line command
// fail.
func BinaryPath(home string) string {
	name := "knit-statusline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, ".claude", name)
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
	Home string
	// Running executable, copied into place by Install.
	Binary string
	Preset string
	// Overwrite existing statusline.toml.
	Force bool
}

// Install copy this binary beside user's settings, point statusLine at copy,
// lay down starting config.
//
// Copy because npx run binary out of a package cache npm prune at will, and
// vanished command render empty row explaining nothing.
func Install(opts Options) (*Result, error) {
	// Empty home resolve every path against cwd, dropping a .claude into
	// whatever directory this ran from.
	if opts.Home == "" {
		return nil, errors.New("no home directory")
	}
	if opts.Preset == "" {
		opts.Preset = config.DefaultPreset
	}
	preset, err := config.PresetSource(opts.Preset)
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q; available: %v", opts.Preset, config.PresetNames())
	}

	res := &Result{
		SettingsPath:    SettingsPath(opts.Home),
		ConfigPath:      config.UserPath(opts.Home),
		InstalledBinary: BinaryPath(opts.Home),
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
		"command": res.InstalledBinary,
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
func Uninstall(home string) (*Result, error) {
	// Empty home resolve every path against cwd, so os.Remove below would hunt a
	// .claude in whatever directory this ran from.
	if home == "" {
		return nil, errors.New("no home directory")
	}
	res := &Result{
		SettingsPath:    SettingsPath(home),
		ConfigPath:      config.UserPath(home),
		InstalledBinary: BinaryPath(home),
	}

	settings, err := readSettings(res.SettingsPath)
	if err != nil {
		return nil, err
	}
	res.ReplacedCommand = statusLineCommand(settings)

	// Only key we installed is ours to delete. User who switched status line
	// tools by hand keep that tool's config, and missing key match nothing.
	if res.ReplacedCommand == res.InstalledBinary {
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
