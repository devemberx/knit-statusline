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

func writeSettings(t *testing.T, root, body string) string {
	t.Helper()
	path := SettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSettingsMap(t *testing.T, root string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(SettingsPath(root))
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
	root := t.TempDir()

	res, err := Install(Options{Root: root, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.ConfigWrote {
		t.Error("a fresh install should write statusline.toml")
	}

	settings := readSettingsMap(t, root)
	sl, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing: %+v", settings)
	}
	if sl["type"] != "command" {
		t.Errorf("statusLine type = %v", sl["type"])
	}

	// Recorded command must be installed copy, not npx package cache npm may
	// prune out from under it.
	if sl["command"] != CommandString(BinaryPath(root)) {
		t.Errorf("statusLine command = %v, want the installed copy %s", sl["command"], CommandString(BinaryPath(root)))
	}

	info, err := os.Stat(BinaryPath(root))
	if err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	// Windows carry no POSIX execute bit -- Go report 0o666 for every regular
	// file there. Executability come from .exe suffix, covered separately.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}

	if _, err := os.Stat(config.UserPath(root)); err != nil {
		t.Errorf("statusline.toml not written: %v", err)
	}
}

// CLAUDE_CONFIG_DIR pointed at a directory nobody created yet is first run for
// anyone who moves config root, not edge case. Every MkdirAll along path
// (copyBinary, writeJSON, config write) must fire, else this silently regress
// to working only when root already exists.
func TestInstallCreatesTheRootWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "moved-root")

	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	for _, path := range []string{BinaryPath(root), SettingsPath(root), config.UserPath(root)} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s not created: %v", path, err)
		}
	}
}

// Git Bash eat unquoted backslash: C:\Users\... command render blank row.
// Real coverage on windows runner, where TempDir hold backslashes.
func TestInstallWritesCommandWithForwardSlashes(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	sl, _ := readSettingsMap(t, root)["statusLine"].(map[string]any)
	cmd, _ := sl["command"].(string)
	if strings.ContainsRune(cmd, '\\') {
		t.Errorf("command %q holds backslashes; Git Bash eats them", cmd)
	}
	if cmd != CommandString(BinaryPath(root)) {
		t.Errorf("command = %q, want %q", cmd, CommandString(BinaryPath(root)))
	}
}

// Unquoted command split at home space. TempDir carry no space, so make one.
func TestInstallQuotesACommandPathWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "John Doe")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	sl, _ := readSettingsMap(t, root)["statusLine"].(map[string]any)
	cmd, _ := sl["command"].(string)
	want := `"` + filepath.ToSlash(BinaryPath(root)) + `"`
	if cmd != want {
		t.Errorf("command = %q, want %q", cmd, want)
	}
}

// Bare path from old install, slashed, quoted -- all must read as ours, else
// uninstall strand old entry.
func TestOwnsCommandAcrossHistoricalForms(t *testing.T) {
	root := t.TempDir()
	binary := BinaryPath(root)

	for _, cmd := range []string{
		binary,
		filepath.ToSlash(binary),
		`"` + filepath.ToSlash(binary) + `"`,
		CommandString(binary),
	} {
		if !OwnsCommand(cmd, binary) {
			t.Errorf("OwnsCommand(%q) = false, want true", cmd)
		}
	}

	for _, cmd := range []string{"", "/opt/other-tool", `"/opt/other-tool"`} {
		if OwnsCommand(cmd, binary) {
			t.Errorf("OwnsCommand(%q) = true, want false", cmd)
		}
	}
}

// Metacharacter break bash same as space: O'Brien leave quote unmatched, A&B
// split command, (1) open subshell. All legal Windows filename characters.
func TestInstallQuotesAMetacharacterPath(t *testing.T) {
	for _, dir := range []string{"O'Brien", "A&B", "backup (1)"} {
		root := filepath.Join(t.TempDir(), dir)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
			t.Fatal(err)
		}

		sl, _ := readSettingsMap(t, root)["statusLine"].(map[string]any)
		cmd, _ := sl["command"].(string)
		want := `"` + filepath.ToSlash(BinaryPath(root)) + `"`
		if cmd != want {
			t.Errorf("root %q: command = %q, want %q", dir, cmd, want)
		}
	}
}

// Double quote leave $ backtick \ " live in bash: "we$ird" expand empty
// variable, backtick open command substitution, lone \ eat next rune, " end
// quote early. Escape all four. Backslash and " never survive on windows
// (ToSlash, illegal filename rune) but stay legal in unix home names.
func TestCommandStringEscapesExpansionCharacters(t *testing.T) {
	for _, tc := range []struct{ binary, want string }{
		{`/home/we$ird/.claude/knit-statusline`, `"/home/we\$ird/.claude/knit-statusline"`},
		{"/home/back`tick/.claude/knit-statusline", "\"/home/back\\`tick/.claude/knit-statusline\""},
		{`/home/we"ird/.claude/knit-statusline`, `"/home/we\"ird/.claude/knit-statusline"`},
	} {
		if got := CommandString(tc.binary); got != tc.want {
			t.Errorf("CommandString(%q) = %q, want %q", tc.binary, got, tc.want)
		}
	}
}

// Literal backslash in unix home name must arrive escaped, not eaten. Windows
// excluded: ToSlash read same rune as separator and rewrite it.
func TestCommandStringEscapesUnixBackslash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ToSlash rewrite backslash to slash on windows")
	}
	got := CommandString(`/home/back\slash/.claude/knit-statusline`)
	if want := `"/home/back\\slash/.claude/knit-statusline"`; got != want {
		t.Errorf("CommandString = %q, want %q", got, want)
	}
}

// Mid-word tilde expand in no shell, and 8.3 short home C:\Users\RUNNER~1 is
// common. Quoting it would only break PowerShell fallback for nothing.
func TestCommandStringLeavesShortNamesBare(t *testing.T) {
	// Slashed input: ToSlash rewrite separators on windows alone, and quoting
	// decision is what this test pin.
	got := CommandString("C:/Users/RUNNER~1/.claude/knit-statusline.exe")
	if want := "C:/Users/RUNNER~1/.claude/knit-statusline.exe"; got != want {
		t.Errorf("CommandString = %q, want %q", got, want)
	}
}

// Non-ASCII home safe bare in both shells; quoting would cost PowerShell
// fallback every CJK user.
func TestCommandStringLeavesNonASCIIBare(t *testing.T) {
	got := CommandString(`C:/Users/홍길동/.claude/knit-statusline.exe`)
	if want := `C:/Users/홍길동/.claude/knit-statusline.exe`; got != want {
		t.Errorf("CommandString = %q, want %q", got, want)
	}
}

// Every form CommandString emit must read back as ours, else uninstall strand
// its own entry.
func TestOwnsCommandRoundTripsEveryEmittedForm(t *testing.T) {
	for _, binary := range []string{
		`/home/plain/.claude/knit-statusline`,
		`/home/John Doe/.claude/knit-statusline`,
		`/home/O'Brien/.claude/knit-statusline`,
		`/home/we$ird/.claude/knit-statusline`,
		"/home/back`tick/.claude/knit-statusline",
		`C:\Users\A&B\.claude\knit-statusline.exe`,
		`C:\Users\홍길동\.claude\knit-statusline.exe`,
	} {
		if !OwnsCommand(CommandString(binary), binary) {
			t.Errorf("OwnsCommand(CommandString(%q)) = false, want true", binary)
		}
	}
}

// Windows path compare case-insensitively. Exact compare strand our own key
// while deleting binary it point at.
func TestOwnsCommandFoldsWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("unix path are case-sensitive")
	}
	binary := `C:\Users\John\.claude\knit-statusline.exe`
	if !OwnsCommand(`c:/users/john/.claude/knit-statusline.exe`, binary) {
		t.Error("case difference read as another tool")
	}
}

// One binary, two strings: 8.3 short home C:\Users\RUNNER~1 on windows,
// symlinked home on unix. No rewrite reconcile those, so stat settle it.
func TestOwnsCommandMatchesSameFileByAnotherPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Install(Options{Root: target, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	if !OwnsCommand(CommandString(BinaryPath(link)), BinaryPath(target)) {
		t.Error("same binary reached through symlinked home read as another tool")
	}

	// Stat must not turn every existing file into ours.
	other := filepath.Join(root, "other-tool")
	if err := os.WriteFile(other, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if OwnsCommand(other, BinaryPath(target)) {
		t.Error("another tool on disk claimed as ours")
	}
}

// Fallback path see escaped bytes whenever exact compare miss (case, short
// name). Unescaped rune then mismatch, so unquote must reverse CommandString.
func TestUnquoteCommandReversesEscaping(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`/home/plain/knit-statusline`, `/home/plain/knit-statusline`},
		{`"/home/John Doe/knit-statusline"`, `/home/John Doe/knit-statusline`},
		{`"/home/we\$ird/knit-statusline"`, `/home/we$ird/knit-statusline`},
		{"\"/home/back\\`tick/knit-statusline\"", "/home/back`tick/knit-statusline"},
		{`"/home/we\"ird/knit-statusline"`, `/home/we"ird/knit-statusline`},
		{`"/home/back\\slash/knit-statusline"`, `/home/back\slash/knit-statusline`},
		// Backslash before anything else stay literal, bash rule.
		{`"/home/back\slash/knit-statusline"`, `/home/back\slash/knit-statusline`},
		{`""`, ``},
		{`"`, `"`},
	} {
		if got := unquoteCommand(tc.in); got != tc.want {
			t.Errorf("unquoteCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Uninstall must recognize quoted form current install write for spaced home.
func TestUninstallRemovesAQuotedCommand(t *testing.T) {
	root := filepath.Join(t.TempDir(), "John Doe")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	res, err := Uninstall(root)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.RemovedStatusLine {
		t.Error("quoted command was not recognized as ours")
	}
	if _, ok := readSettingsMap(t, root)["statusLine"]; ok {
		t.Error("statusLine was not removed")
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
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(BinaryPath(root))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Install(Options{Root: root, Binary: BinaryPath(root)}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	after, err := os.ReadFile(BinaryPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("reinstalling from its own path changed the binary (%d -> %d bytes)", len(before), len(after))
	}
}

// Upgrade write over existing dst, which is rename replaceFile handle.
func TestInstallReplacesAnOlderCopy(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	newer := filepath.Join(t.TempDir(), "knit-statusline")
	if err := os.WriteFile(newer, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(Options{Root: root, Binary: newer}); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	got, err := os.ReadFile(BinaryPath(root))
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
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BinaryPath(root)); !os.IsNotExist(err) {
		t.Errorf("installed binary should be removed, stat error = %v", err)
	}
}

// Worst damage here is dropping user's hooks, permissions or enabled plugins
// while adding one key.
func TestInstallPreservesUnrelatedSettings(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{
  "model": "opus",
  "effortLevel": "xhigh",
  "permissions": {"allow": ["Bash(ls:*)"], "defaultMode": "auto"},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "rtk hook claude"}]}]},
  "enabledPlugins": {"superpowers@official": true}
}`)

	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	settings := readSettingsMap(t, root)
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
	root := t.TempDir()
	original := `{"model":"opus"}`
	writeSettings(t, root, original)

	res, err := Install(Options{Root: root, Binary: fakeBinary(t)})
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
	root := t.TempDir()
	writeSettings(t, root, `{"statusLine":{"type":"command","command":"~/.claude/old.sh"}}`)

	res, err := Install(Options{Root: root, Binary: fakeBinary(t)})
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
	root := t.TempDir()
	custom := "[[lines]]\nsegments = [\"model\"]\n"
	if err := os.MkdirAll(filepath.Dir(config.UserPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserPath(root), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(Options{Root: root, Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.ConfigWrote {
		t.Error("an existing config should not be replaced without --force")
	}

	b, _ := os.ReadFile(config.UserPath(root))
	if string(b) != custom {
		t.Errorf("config was modified: %q", b)
	}
}

func TestInstallForceReplacesConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.UserPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserPath(root), []byte("[[lines]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(Options{Root: root, Binary: fakeBinary(t), Force: true})
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
	root := t.TempDir()
	writeSettings(t, root, `{"model": "opus",,,}`)

	_, err := Install(Options{Root: root, Binary: fakeBinary(t)})
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
	root := t.TempDir()
	writeSettings(t, root, `{"model": "opus",,,}`)

	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(BinaryPath(root)); !os.IsNotExist(err) {
		t.Errorf("binary installed despite the refusal, stat error = %v", err)
	}
	if _, err := os.Stat(config.UserPath(root)); !os.IsNotExist(err) {
		t.Errorf("config written despite the refusal, stat error = %v", err)
	}
}

func TestInstallRejectsUnknownPreset(t *testing.T) {
	_, err := Install(Options{Root: t.TempDir(), Binary: fakeBinary(t), Preset: "nope"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error should list the presets: %v", err)
	}
}

func TestUninstallRemovesOnlyTheStatusLine(t *testing.T) {
	root := t.TempDir()
	// Seed bare path old install wrote, proving old entry still read as ours.
	// Marshal keep windows backslashes escaped.
	ours, err := json.Marshal(BinaryPath(root))
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, root, `{"model":"opus","statusLine":{"type":"command","command":`+string(ours)+`}}`)

	res, err := Uninstall(root)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.ReplacedCommand != BinaryPath(root) {
		t.Errorf("ReplacedCommand = %q", res.ReplacedCommand)
	}

	settings := readSettingsMap(t, root)
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
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.UserPath(root)); err != nil {
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
	root := t.TempDir()
	writeSettings(t, root, `{"statusLine":{"type":"command","command":"/opt/other-tool"}}`)

	res, err := Uninstall(root)
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

	sl, ok := readSettingsMap(t, root)["statusLine"].(map[string]any)
	if !ok {
		t.Fatal("another tool's statusLine was removed")
	}
	if sl["type"] != "command" || sl["command"] != "/opt/other-tool" {
		t.Errorf("statusLine = %+v, want it unchanged", sl)
	}
}

func TestUninstallReportsRemovingOurOwnStatusLine(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}

	res, err := Uninstall(root)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.RemovedStatusLine {
		t.Error("RemovedStatusLine should be true once our own key is dropped")
	}
	if _, ok := readSettingsMap(t, root)["statusLine"]; ok {
		t.Error("statusLine was not removed")
	}
}

// Hand-deleting key out of settings must not strand binary in ~/.claude.
func TestUninstallRemovesTheBinaryWithoutAStatusLineKey(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	settings := readSettingsMap(t, root)
	delete(settings, "statusLine")
	edited, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, root, string(edited))

	res, err := Uninstall(root)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.RemovedStatusLine {
		t.Error("RemovedStatusLine should be false when no key is present")
	}
	if _, err := os.Stat(BinaryPath(root)); !os.IsNotExist(err) {
		t.Errorf("installed binary should be removed, stat error = %v", err)
	}
}

// Empty root resolve every path against cwd, so install would drop settings.json
// into whatever directory it ran from.
func TestInstallRejectsAnEmptyRoot(t *testing.T) {
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

// Uninstall reach os.Remove, so empty root hunt a binary under cwd.
func TestUninstallRejectsAnEmptyRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	decoy := BinaryPath(dir)
	if err := os.WriteFile(decoy, []byte("not ours\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(""); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Errorf("cwd binary touched: %v", err)
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

// Binary and settings sit directly in config root. Extra ".claude" would nest
// second copy whenever CLAUDE_CONFIG_DIR already name root.
func TestPathsSitDirectlyInTheRoot(t *testing.T) {
	root := t.TempDir()
	name := "knit-statusline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if got, want := BinaryPath(root), filepath.Join(root, name); got != want {
		t.Errorf("BinaryPath = %q, want %q", got, want)
	}
	if got, want := SettingsPath(root), filepath.Join(root, "settings.json"); got != want {
		t.Errorf("SettingsPath = %q, want %q", got, want)
	}
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{"model":"opus","permissions":{"allow":[]}}`)
	before := readSettingsMap(t, root)

	if _, err := Install(Options{Root: root, Binary: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root); err != nil {
		t.Fatal(err)
	}

	after := readSettingsMap(t, root)
	if len(before) != len(after) {
		t.Fatalf("key count changed: %v -> %v", before, after)
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Errorf("key %q lost in the round trip", k)
		}
	}
}
