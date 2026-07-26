package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// readSettings load into generic map, so key we do not understand survive round
// trip. Missing file = empty settings. Malformed JSON = error, never empty:
// empty discard every hook and permission user had.
func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}

	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%w); fix or move it, then try again", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

// statusLineCommand check every level: statusLine hold any shape user typed,
// and this run before we own it.
func statusLineCommand(settings map[string]any) string {
	existing, ok := settings["statusLine"].(map[string]any)
	if !ok {
		return ""
	}
	cmd, _ := existing["command"].(string)
	return cmd
}

// backupFile copy path to path.bak. Missing original need no backup.
//
// Existing .bak stay: second install would otherwise back up its own output,
// losing last pre-knit copy.
//
// Backup inherit original mode. settings.json env block hold
// ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN, so widening 0o600 leak credentials.
func backupFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backup := path + ".bak"
	if _, err := os.Stat(backup); err == nil {
		return backup, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	perm := info.Mode().Perm()
	if err := os.WriteFile(backup, b, perm); err != nil {
		return "", err
	}
	// WriteFile honour perm only when it create, and umask clear bits it do.
	if err := os.Chmod(backup, perm); err != nil {
		return "", err
	}
	return backup, nil
}

// replaceFile move tmp onto dst. Rename alone replace existing dst everywhere:
// Windows os.Rename call MoveFileEx with MOVEFILE_REPLACE_EXISTING.
//
// Remove only after rename fail, for dst locked or otherwise unreplaceable.
// Unconditional remove would open a window where dst does not exist at all.
func replaceFile(tmp, dst string) error {
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, dst)
}

// writeJSON write atomically, so interrupted install leave Claude Code no
// truncated settings.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// CreateTemp make 0o600. Existing settings.json keep its own mode, commonly
	// 0o644; file we create keep 0o600, since env block hold ANTHROPIC_API_KEY.
	if info, err := os.Stat(path); err == nil {
		if err := os.Chmod(name, info.Mode().Perm()); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return replaceFile(name, path)
}
