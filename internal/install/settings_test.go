package install

import (
	"os"
	"path/filepath"
	"runtime"
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

// settings.json env block hold ANTHROPIC_API_KEY, so 0o600 original must not
// copy to world-readable .bak.
func TestBackupFileKeepsOriginalMode(t *testing.T) {
	home := t.TempDir()
	path := writeSettings(t, home, `{"env":{"ANTHROPIC_API_KEY":"sk-secret"}}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := backupFile(path)
	if err != nil {
		t.Fatalf("backupFile: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	// Windows carry no POSIX mode bits -- Go report 0o666 for every regular file.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", info.Mode().Perm())
	}
}

// Second install back up already-modified settings.json. Only first .bak hold
// pre-knit original, so it stay.
func TestBackupFileKeepsFirstBackup(t *testing.T) {
	home := t.TempDir()
	original := `{"model":"opus"}`
	path := writeSettings(t, home, original)

	first, err := backupFile(path)
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}

	writeSettings(t, home, `{"model":"sonnet"}`)
	second, err := backupFile(path)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if second != first {
		t.Errorf("backup path = %q, want %q", second, first)
	}

	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original {
		t.Errorf("backup = %q, want the pre-install original %q", b, original)
	}
}

// Temp file start 0o600, so write must hand back whatever mode user chose.
func TestWriteJSONKeepsExistingFileMode(t *testing.T) {
	home := t.TempDir()
	path := writeSettings(t, home, `{"model":"opus"}`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeJSON(path, map[string]any{"model": "sonnet"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows carry no POSIX mode bits -- Go report 0o666 for every regular file.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("settings mode = %v, want 0644", info.Mode().Perm())
	}
}

// No original mode to copy. settings.json env block hold ANTHROPIC_API_KEY, so
// file we create stay owner-only.
func TestWriteJSONCreatesOwnerOnlyFile(t *testing.T) {
	home := t.TempDir()
	path := SettingsPath(home)

	if err := writeJSON(path, map[string]any{"model": "opus"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows carry no POSIX mode bits -- Go report 0o666 for every regular file.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("settings mode = %v, want 0600", info.Mode().Perm())
	}
}

// Rename alone replace existing destination, so no remove step leave settings
// gone mid-write.
func TestReplaceFileOverwritesDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ".settings-x.tmp")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(tmp, dst); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Errorf("destination = %q, want %q", b, "new")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("source still present after rename: %v", err)
	}
}
