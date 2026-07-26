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

// readSettings load into generic map, so every key we do not understand survive
// round trip untouched.
//
// Missing file = empty settings. Malformed JSON = error: alternative silently
// discard whatever user had.
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

// statusLineCommand read command already configured, or empty. Every level
// checked: neighbouring key may hold any shape, and this run before we own it.
func statusLineCommand(settings map[string]any) string {
	existing, ok := settings["statusLine"].(map[string]any)
	if !ok {
		return ""
	}
	cmd, _ := existing["command"].(string)
	return cmd
}

// backupFile copy path to path.bak. Missing original need no backup.
func backupFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backup := path + ".bak"
	if err := os.WriteFile(backup, b, 0o644); err != nil {
		return "", err
	}
	return backup, nil
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
	// Windows refuse rename onto existing file, where Unix replace silently.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(name, path)
}
