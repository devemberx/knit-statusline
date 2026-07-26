// Package install wire knit-statusline into Claude Code settings.
//
// Plugin cannot configure main status line -- plugin settings.json take only
// agent and subagentStatusLine -- so user's own ~/.claude/settings.json is only
// path in. That file also hold their hooks, permissions and enabled plugins, so
// every write here merge, never replace.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/devemberx/knit-statusline/internal/config"
)

// BinaryPath name installed copy.
//
// Windows need .exe: CreateProcess refuse to run extensionless file, and Claude
// Code print nothing when its status line command fail, so user meet a blank row
// with no error naming what went wrong.
func BinaryPath(home string) string {
	name := "knit-statusline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, ".claude", name)
}

// Result report what install or uninstall did, so command tell user instead of
// claiming success generically.
type Result struct {
	SettingsPath string
	BackupPath   string
	ConfigPath   string
	ConfigWrote  bool
	// Path recorded in settings.json.
	InstalledBinary string
	// Previous statusLine command, when one was set.
	ReplacedCommand string
}

type Options struct {
	Home string
	// Running executable, copied into place by Install.
	Binary string
	Preset string
	// Overwrite existing statusline.toml.
	Force bool
}

// Install copy this binary beside user's Claude Code settings, point status line
// at copy, lay down starting config.
//
// Copy is not incidental. npx run binary out of a package cache npm prune at
// will, and status line whose command vanish render empty row explaining
// nothing. Copy under ~/.claude survive a cleared cache.
func Install(opts Options) (*Result, error) {
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

	// Back up before touching anything. File hold user's hooks and permissions,
	// so a bad write here cost far more than a status line.
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

// Uninstall drop statusLine key and installed binary, every other setting
// untouched.
//
// statusline.toml stay: it is user's config. Removing it turn reinstall into
// fresh start instead of resumption.
func Uninstall(home string) (*Result, error) {
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
	if _, ok := settings["statusLine"]; !ok {
		return res, nil
	}

	backup, err := backupFile(res.SettingsPath)
	if err != nil {
		return nil, err
	}
	res.BackupPath = backup

	delete(settings, "statusLine")
	if err := writeJSON(res.SettingsPath, settings); err != nil {
		return nil, err
	}

	// Remove copy this tool made. May be binary running right now; fine on Unix,
	// process keep its open file. Failure not worth reporting once setting
	// already gone.
	_ = os.Remove(res.InstalledBinary)
	return res, nil
}

// copyBinary place running executable at dst.
//
// Temp file then rename: dst may be binary a running Claude Code is about to
// invoke. In-place write hand that invocation a half-written file; rename swap
// it whole. Copy onto itself no-op -- what reinstall from ~/.claude do.
func copyBinary(src, dst string) error {
	if src == "" {
		return errors.New("no source binary")
	}
	if abs, err := filepath.Abs(src); err == nil {
		src = abs
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
	// Windows refuse rename onto existing file, where Unix replace silently.
	// Reinstall hit this every time.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(name, dst)
}
