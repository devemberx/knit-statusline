package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Missing settings.json is normal: user may never have opened one. Install must
// create rather than refuse.
func TestReadSettingsToleratesAbsentAndEmptyFiles(t *testing.T) {
	home := t.TempDir()
	got, err := readSettings(SettingsPath(home))
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing file gave %+v, want empty settings", got)
	}

	writeSettings(t, home, "")
	if got, err = readSettings(SettingsPath(home)); err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty file gave %+v, want empty settings", got)
	}
}

// Literal null decode to nil map. Writing into that panic, so it become empty.
func TestReadSettingsTurnsNullIntoEmptySettings(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, "null")

	got, err := readSettings(SettingsPath(home))
	if err != nil {
		t.Fatalf("null: %v", err)
	}
	if got == nil {
		t.Fatal("null decoded to a nil map, which no write can use")
	}
	got["statusLine"] = "probe"
}

// Malformed JSON is error, never empty settings: treating it as empty discard
// every hook and permission user had.
func TestReadSettingsRefusesMalformedJSON(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"model": "opus",,,}`)

	_, err := readSettings(SettingsPath(home))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should name the problem: %v", err)
	}
	if !strings.Contains(err.Error(), "fix or move it") {
		t.Errorf("error should say what to do: %v", err)
	}
}

// Neighbouring key hold any shape user typed, and this run before we own it.
func TestStatusLineCommandChecksEveryLevel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]any
		want     string
	}{
		{"absent", map[string]any{}, ""},
		{"wrong type", map[string]any{"statusLine": "old.sh"}, ""},
		{"no command", map[string]any{"statusLine": map[string]any{"type": "command"}}, ""},
		{"command not a string", map[string]any{"statusLine": map[string]any{"command": 42}}, ""},
		{"present", map[string]any{"statusLine": map[string]any{"command": "/opt/x"}}, "/opt/x"},
	} {
		if got := statusLineCommand(tc.settings); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Interrupted write must leave Claude Code no truncated settings, so temp file
// then rename. Leftover temp mean rename never happened.
func TestWriteJSONIsAtomicAndLeavesNoTemp(t *testing.T) {
	home := t.TempDir()
	path := SettingsPath(home)

	if err := writeJSON(path, map[string]any{"model": "opus"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if got := readSettingsMap(t, home)["model"]; got != "opus" {
		t.Errorf("model = %v", got)
	}

	// Second write replace first. Windows refuse rename onto existing file.
	if err := writeJSON(path, map[string]any{"model": "sonnet"}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := readSettingsMap(t, home)["model"]; got != "sonnet" {
		t.Errorf("model = %v, want the second write", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

// File end with newline, so it read cleanly beside hand-edited settings.
func TestWriteJSONIndentsAndEndsWithNewline(t *testing.T) {
	home := t.TempDir()
	if err := writeJSON(SettingsPath(home), map[string]any{"a": map[string]any{"b": 1}}); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(SettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("settings = %q, want a trailing newline", b)
	}
	if !strings.Contains(string(b), "\n  ") {
		t.Errorf("settings = %q, want two-space indent", b)
	}
}

func TestBackupFileCopiesContents(t *testing.T) {
	home := t.TempDir()
	original := `{"model":"opus"}`
	path := writeSettings(t, home, original)

	backup, err := backupFile(path)
	if err != nil {
		t.Fatalf("backupFile: %v", err)
	}
	if backup != path+".bak" {
		t.Errorf("backup path = %q", backup)
	}

	b, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original {
		t.Errorf("backup = %q, want %q", b, original)
	}
}

// Nothing to back up is no failure: a first install find no settings at all.
func TestBackupFileSkipsMissingOriginal(t *testing.T) {
	backup, err := backupFile(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("backupFile: %v", err)
	}
	if backup != "" {
		t.Errorf("backup path = %q, want empty", backup)
	}
}
