package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/config"
)

// fakeBinary stand in for running executable. Install copy it, so path must be
// real file.
func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knit-statusline")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSettings(t *testing.T, home, body string) string {
	t.Helper()
	path := SettingsPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSettingsMap(t *testing.T, home string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(SettingsPath(home))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings is not valid JSON: %v", err)
	}
	return m
}

func TestInstallCreatesSettingsAndConfig(t *testing.T) {
	home := t.TempDir()

	res, err := Install(Options{Home: home, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.ConfigWrote {
		t.Error("a fresh install should write statusline.toml")
	}

	settings := readSettingsMap(t, home)
	sl, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing: %+v", settings)
	}
	if sl["type"] != "command" {
		t.Errorf("statusLine type = %v", sl["type"])
	}

	// Recorded command must be installed copy, not npx package cache npm may
	// prune out from under it.
	if sl["command"] != BinaryPath(home) {
		t.Errorf("statusLine command = %v, want the installed copy %s", sl["command"], BinaryPath(home))
	}

	info, err := os.Stat(BinaryPath(home))
	if err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	// Windows carry no POSIX execute bit -- Go report 0o666 for every regular
	// file there. Executability come from .exe suffix, covered separately.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}

	if _, err := os.Stat(config.UserPath(home)); err != nil {
		t.Errorf("statusline.toml not written: %v", err)
	}
}

// CreateProcess refuse to run extensionless file, so a Windows install without
// .exe leave Claude Code printing nothing and no error saying why.
func TestBinaryPathCarriesWindowsExtension(t *testing.T) {
	got := filepath.Base(BinaryPath(t.TempDir()))

	want := "knit-statusline"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got != want {
		t.Errorf("BinaryPath base = %q, want %q on %s", got, want, runtime.GOOS)
	}
}

// Reinstalling from installed location must not truncate binary by copying it
// onto itself.
func TestInstallIsIdempotentFromItsOwnLocation(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(BinaryPath(home))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Install(Options{Home: home, Binary: BinaryPath(home)}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	after, err := os.ReadFile(BinaryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("reinstalling from its own path changed the binary (%d -> %d bytes)", len(before), len(after))
	}
}

// Upgrade write over existing dst, which is rename replaceFile handle.
func TestInstallReplacesAnOlderCopy(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	newer := filepath.Join(t.TempDir(), "knit-statusline")
	if err := os.WriteFile(newer, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(Options{Home: home, Binary: newer}); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	got, err := os.ReadFile(BinaryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "exit 1") {
		t.Errorf("installed binary = %q, want the newer copy", got)
	}
}

func TestCopyBinaryRejectsEmptySource(t *testing.T) {
	if err := copyBinary("", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("empty source should be an error")
	}
}

func TestUninstallRemovesTheInstalledBinary(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BinaryPath(home)); !os.IsNotExist(err) {
		t.Errorf("installed binary should be removed, stat error = %v", err)
	}
}

// Worst damage here is dropping user's hooks, permissions or enabled plugins
// while adding one key.
func TestInstallPreservesUnrelatedSettings(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{
  "model": "opus",
  "effortLevel": "xhigh",
  "permissions": {"allow": ["Bash(ls:*)"], "defaultMode": "auto"},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "rtk hook claude"}]}]},
  "enabledPlugins": {"superpowers@official": true}
}`)

	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	settings := readSettingsMap(t, home)
	for _, key := range []string{"model", "effortLevel", "permissions", "hooks", "enabledPlugins"} {
		if _, ok := settings[key]; !ok {
			t.Errorf("install dropped %q", key)
		}
	}

	hooks, _ := json.Marshal(settings["hooks"])
	if !strings.Contains(string(hooks), "rtk hook claude") {
		t.Errorf("hook contents altered: %s", hooks)
	}
}

func TestInstallBacksUpBeforeWriting(t *testing.T) {
	home := t.TempDir()
	original := `{"model":"opus"}`
	writeSettings(t, home, original)

	res, err := Install(Options{Home: home, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("no backup was made")
	}

	b, err := os.ReadFile(res.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(b) != original {
		t.Errorf("backup = %q, want the original contents", b)
	}
}

func TestInstallReportsAReplacedStatusLine(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"statusLine":{"type":"command","command":"~/.claude/old.sh"}}`)

	res, err := Install(Options{Home: home, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.ReplacedCommand != "~/.claude/old.sh" {
		t.Errorf("ReplacedCommand = %q", res.ReplacedCommand)
	}
}

// Existing statusline.toml is user's own work. Reinstalling to update a binary
// path must not throw their layout away.
func TestInstallKeepsAnExistingConfig(t *testing.T) {
	home := t.TempDir()
	custom := "[[lines]]\nsegments = [\"model\"]\n"
	if err := os.MkdirAll(filepath.Dir(config.UserPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserPath(home), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(Options{Home: home, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.ConfigWrote {
		t.Error("an existing config should not be replaced without --force")
	}

	b, _ := os.ReadFile(config.UserPath(home))
	if string(b) != custom {
		t.Errorf("config was modified: %q", b)
	}
}

func TestInstallForceReplacesConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.UserPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserPath(home), []byte("[[lines]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(Options{Home: home, Binary: fakeBinary(t), Force: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.ConfigWrote {
		t.Error("--force should replace the config")
	}
}

// Overwriting a settings file we cannot parse would discard whatever it hold, so
// install refuse and say so.
func TestInstallRefusesMalformedSettings(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"model": "opus",,,}`)

	_, err := Install(Options{Home: home, Binary: fakeBinary(t)})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should explain the problem: %v", err)
	}
}

// Refusal must happen before anything is written, else typo leave half install
// behind.
func TestInstallRefusesMalformedSettingsBeforeTouchingDisk(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"model": "opus",,,}`)

	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(BinaryPath(home)); !os.IsNotExist(err) {
		t.Errorf("binary installed despite the refusal, stat error = %v", err)
	}
	if _, err := os.Stat(config.UserPath(home)); !os.IsNotExist(err) {
		t.Errorf("config written despite the refusal, stat error = %v", err)
	}
}

func TestInstallRejectsUnknownPreset(t *testing.T) {
	_, err := Install(Options{Home: t.TempDir(), Binary: fakeBinary(t), Preset: "nope"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error should list the presets: %v", err)
	}
}

func TestUninstallRemovesOnlyTheStatusLine(t *testing.T) {
	home := t.TempDir()
	// Uninstall touch statusLine only when command is our own installed copy,
	// so seed exactly that. Marshal keep Windows backslashes escaped.
	ours, err := json.Marshal(BinaryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, home, `{"model":"opus","statusLine":{"type":"command","command":`+string(ours)+`}}`)

	res, err := Uninstall(home)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.ReplacedCommand != BinaryPath(home) {
		t.Errorf("ReplacedCommand = %q", res.ReplacedCommand)
	}

	settings := readSettingsMap(t, home)
	if _, ok := settings["statusLine"]; ok {
		t.Error("statusLine was not removed")
	}
	if settings["model"] != "opus" {
		t.Error("uninstall dropped an unrelated setting")
	}
}

// Config is user's, not ours; removing it turn a reinstall into fresh start
// rather than resumption.
func TestUninstallLeavesTheConfigInPlace(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.UserPath(home)); err != nil {
		t.Errorf("statusline.toml should survive uninstall: %v", err)
	}
}

func TestUninstallOnACleanSystemIsNotAnError(t *testing.T) {
	res, err := Uninstall(t.TempDir())
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.ReplacedCommand != "" {
		t.Errorf("nothing should have been reported as removed: %+v", res)
	}
}

// User who switched status line tools by hand still own that tool's config.
func TestUninstallLeavesAForeignStatusLine(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"statusLine":{"type":"command","command":"/opt/other-tool"}}`)

	res, err := Uninstall(home)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.RemovedStatusLine {
		t.Error("RemovedStatusLine should be false for another tool's command")
	}
	if res.ReplacedCommand != "/opt/other-tool" {
		t.Errorf("ReplacedCommand = %q, want the foreign command reported", res.ReplacedCommand)
	}
	if res.BackupPath != "" {
		t.Errorf("BackupPath = %q, want no write at all", res.BackupPath)
	}

	sl, ok := readSettingsMap(t, home)["statusLine"].(map[string]any)
	if !ok {
		t.Fatal("another tool's statusLine was removed")
	}
	if sl["type"] != "command" || sl["command"] != "/opt/other-tool" {
		t.Errorf("statusLine = %+v, want it unchanged", sl)
	}
}

func TestUninstallReportsRemovingOurOwnStatusLine(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	res, err := Uninstall(home)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.RemovedStatusLine {
		t.Error("RemovedStatusLine should be true once our own key is dropped")
	}
	if _, ok := readSettingsMap(t, home)["statusLine"]; ok {
		t.Error("statusLine was not removed")
	}
}

// Hand-deleting key out of settings must not strand binary in ~/.claude.
func TestUninstallRemovesTheBinaryWithoutAStatusLineKey(t *testing.T) {
	home := t.TempDir()
	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	settings := readSettingsMap(t, home)
	delete(settings, "statusLine")
	edited, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, home, string(edited))

	res, err := Uninstall(home)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.RemovedStatusLine {
		t.Error("RemovedStatusLine should be false when no key is present")
	}
	if _, err := os.Stat(BinaryPath(home)); !os.IsNotExist(err) {
		t.Errorf("installed binary should be removed, stat error = %v", err)
	}
}

// Empty home resolve every path against cwd, so install would drop a .claude
// into whatever directory it ran from.
func TestInstallRejectsAnEmptyHome(t *testing.T) {
	binary := fakeBinary(t)
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := Install(Options{Binary: binary}); err == nil {
		t.Fatal("expected an error")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("working directory written to: %v", entries)
	}
}

// Uninstall reach os.Remove, so empty home hunt a .claude under cwd.
func TestUninstallRejectsAnEmptyHome(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	decoy := BinaryPath(dir)
	if err := os.WriteFile(decoy, []byte("not ours\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(""); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Errorf("cwd .claude touched: %v", err)
	}
}

// Self-copy guard compare src against dst, so both side need absolutising.
func TestCopyBinaryRelativeSelfCopyIsANoOp(t *testing.T) {
	t.Chdir(t.TempDir())
	body := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile("knit-statusline", []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat("knit-statusline")
	if err != nil {
		t.Fatal(err)
	}

	if err := copyBinary(filepath.Join(wd, "knit-statusline"), "knit-statusline"); err != nil {
		t.Fatalf("copyBinary: %v", err)
	}

	after, err := os.Stat("knit-statusline")
	if err != nil {
		t.Fatalf("binary gone: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("self copy replaced the file instead of doing nothing")
	}
	got, err := os.ReadFile("knit-statusline")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("binary = %q, want it untouched", got)
	}
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"model":"opus","permissions":{"allow":[]}}`)
	before := readSettingsMap(t, home)

	if _, err := Install(Options{Home: home, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(home); err != nil {
		t.Fatal(err)
	}

	after := readSettingsMap(t, home)
	if len(before) != len(after) {
		t.Fatalf("key count changed: %v -> %v", before, after)
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Errorf("key %q lost in the round trip", k)
		}
	}
}
