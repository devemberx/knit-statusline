package segment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// configCtx point segment at throwaway config root and project root, so no test
// read real ~/.claude.
func configCtx(t *testing.T) Context {
	t.Helper()
	c := ctx(t, fixtures.Full, "config")
	c.ConfigDir = t.TempDir()
	project := t.TempDir()
	c.In.Workspace.ProjectDir = project
	c.In.Workspace.CurrentDir = project
	pointManagedSettings(t, filepath.Join(t.TempDir(), "managed-settings.json"))
	return c
}

// pointManagedSettings move enterprise policy path off /etc, so machine
// carrying real one do not leak its hooks into assertion here.
func pointManagedSettings(t *testing.T, path string) {
	t.Helper()
	prev := managedSettings
	managedSettings = path
	t.Cleanup(func() { managedSettings = prev })
}

// quotePath render path as JSON string, quotes included. Windows path carry
// backslash, and raw interpolation leave "D:\a" standing as invalid escape --
// whole file then read unknown, on that runner alone.
func quotePath(t *testing.T, p string) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("quote %s: %v", p, err)
	}
	return string(b)
}

func writeUnder(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// settings.json nest three deep: event, matcher group, command list. Counting
// either outer layer report 2 where 3 commands run.
const threeCommandSettings = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "validate_pr.py"},
          {"type": "command", "command": "validate_pr_merge.py"}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [{"type": "command", "command": "lint_comment_style.py"}]
      }
    ]
  }
}`

func TestConfigCountsHookCommandsNotEvents(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/settings.json", threeCommandSettings)

	if got, want := draw(c), "🪝3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Both scopes fire, so both count. Layer keeping its own settings.local.json
// too is normal, not double declaration.
func TestConfigSumsHooksAcrossScopes(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json", threeCommandSettings)
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/settings.local.json",
		`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "notify.sh"}]}]}}`)

	if got, want := draw(c), "🪝4"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// enablePlugin register plugin under installed_plugins.json and switch it on in
// user settings. Return install path hook file go under.
func enablePlugin(t *testing.T, c Context, key string, on bool) string {
	t.Helper()
	install := t.TempDir()
	writeUnder(t, c.ConfigDir, "plugins/installed_plugins.json",
		`{"version": 2, "plugins": {"`+key+`": [{"scope": "user", "installPath": `+quotePath(t, install)+`}]}}`)
	state := "false"
	if on {
		state = "true"
	}
	writeUnder(t, c.ConfigDir, "settings.json", `{"enabledPlugins": {"`+key+`": `+state+`}}`)
	return install
}

const oneHookBody = `"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "activate.js"}]}]}`

// Plugin hook never reach settings.json, so counting that file alone print 0
// while two hooks fire every prompt.
func TestConfigCountsEnabledPluginHooks(t *testing.T) {
	for name, rel := range map[string]string{
		"plugin manifest": ".claude-plugin/plugin.json",
		"hooks directory": "hooks/hooks.json",
		"plugin root":     "hooks.json",
	} {
		t.Run(name, func(t *testing.T) {
			c := configCtx(t)
			install := enablePlugin(t, c, "caveman@caveman", true)
			writeUnder(t, install, rel, `{"name": "caveman", `+oneHookBody+`}`)

			if got, want := draw(c), "🪝1"; got != want {
				t.Errorf("rendered %q, want %q", got, want)
			}
		})
	}
}

// Manifest carrying name and version but no hooks key is what
// claude-plugins-official ship, hooks sitting in hooks/hooks.json beside it.
// Stopping at first file that exist count 0 while hooks fire.
func TestConfigLooksPastManifestDeclaringNoHooks(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "superpowers@claude-plugins-official", true)
	writeUnder(t, install, ".claude-plugin/plugin.json", `{"name": "superpowers", "version": "6.2.0"}`)
	writeUnder(t, install, "hooks/hooks.json", `{`+oneHookBody+`}`)

	if got, want := draw(c), "🪝1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Installed but switched off run nothing.
func TestConfigSkipsDisabledPluginHooks(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "caveman@caveman", false)
	writeUnder(t, install, ".claude-plugin/plugin.json", `{"name": "caveman", `+oneHookBody+`}`)

	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// enablePlugins register n plugins at once and switch every one on. Return
// install path each sit at.
func enablePlugins(t *testing.T, c Context, n int) []string {
	t.Helper()
	root := t.TempDir()
	installs := make([]string, n)
	reg := map[string]any{}
	on := map[string]bool{}
	for i := range n {
		key := "p" + strconv.Itoa(i) + "@vendor"
		installs[i] = filepath.Join(root, strconv.Itoa(i))
		reg[key] = []map[string]string{{"scope": "user", "installPath": installs[i]}}
		on[key] = true
	}
	writeJSONUnder(t, c.ConfigDir, "plugins/installed_plugins.json",
		map[string]any{"version": 2, "plugins": reg})
	writeJSONUnder(t, c.ConfigDir, "settings.json", map[string]any{"enabledPlugins": on})
	return installs
}

func writeJSONUnder(t *testing.T, dir, rel string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", rel, err)
	}
	writeUnder(t, dir, rel, string(b))
}

// Plugin declaring nothing still cost five stat calls, and roster capped at
// 1 MiB alone hold thousands of them. Stopping short is no proof of zero.
func TestConfigMarksPluginsPastPathBudgetUnknown(t *testing.T) {
	c := configCtx(t)
	enablePlugins(t, c, maxPluginPaths+10)

	if got, want := draw(c), "🪝… · 🔌…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Path budget alone leave every manifest read at maxConfigBytes, so bytes carry
// own budget. One manifest serve both counts, so each plugin here read twice.
func TestConfigMarksPluginsPastByteBudgetUnknown(t *testing.T) {
	c := configCtx(t)
	pad := strings.Repeat("x", maxConfigBytes-1024)
	for _, install := range enablePlugins(t, c, int(maxPluginBytes/maxConfigBytes)+1) {
		writeUnder(t, install, ".claude-plugin/plugin.json",
			`{"pad": "`+pad+`", `+oneHookBody+`}`)
	}

	if got, want := draw(c), "🪝… · 🔌…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Machine running a handful of plugins stay far under both budgets, so normal
// roster still count exact.
func TestConfigCountsPluginsUnderBudget(t *testing.T) {
	c := configCtx(t)
	for _, install := range enablePlugins(t, c, 20) {
		writeUnder(t, install, ".claude-plugin/plugin.json", `{"name": "p", `+oneHookBody+`}`)
	}

	if got, want := draw(c), "🪝20"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Empty file load no instruction. Counting it claim guidance that does not
// exist -- state ~/.claude/CLAUDE.md sit in on fresh install.
func TestConfigSkipsEmptyClaudeMd(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "CLAUDE.md", "")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "# rules\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

func TestConfigCountsEveryClaudeMdLocation(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "CLAUDE.md", "user\n")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "project\n")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.local.md", "local\n")
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/CLAUDE.md", "nested\n")
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/CLAUDE.local.md", "nested local\n")

	if got, want := draw(c), "📋5"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Claude Code walk from working directory to filesystem root, loading every
// CLAUDE.md it pass. Monorepo keep shared instruction one level up, and
// counting project directory alone report 1 where 3 load.
func TestConfigCountsAncestorClaudeMd(t *testing.T) {
	c := configCtx(t)
	root := t.TempDir()
	project := filepath.Join(root, "team", "service")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c.In.Workspace.ProjectDir = project
	c.In.Workspace.CurrentDir = project

	writeUnder(t, root, "CLAUDE.md", "monorepo\n")
	writeUnder(t, filepath.Join(root, "team"), "CLAUDE.local.md", "team\n")
	writeUnder(t, project, "CLAUDE.md", "service\n")

	if got, want := draw(c), "📋3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Empty ancestor file load nothing, same as empty one in project.
func TestConfigSkipsEmptyAncestorClaudeMd(t *testing.T) {
	c := configCtx(t)
	root := t.TempDir()
	project := filepath.Join(root, "nested")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c.In.Workspace.ProjectDir = project
	c.In.Workspace.CurrentDir = project

	writeUnder(t, root, "CLAUDE.md", "")
	writeUnder(t, project, "CLAUDE.md", "service\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Path deeper than cap leave ancestors unwalked, and stopping short prove no
// zero.
func TestConfigMarksDeepAncestorPathUnknown(t *testing.T) {
	c := configCtx(t)
	deep := t.TempDir()
	for range maxAncestorDepth + 1 {
		deep = filepath.Join(deep, "d")
	}
	c.In.Workspace.ProjectDir = deep
	c.In.Workspace.CurrentDir = deep

	if got, want := draw(c), "📋…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// "@path" pull another file into context, so counting files named by hand
// report 1 where 3 load.
func TestConfigCountsClaudeMdImports(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "root\n@docs/style.md\nsee @docs/testing.md.\n")
	writeUnder(t, project, "docs/style.md", "style\n")
	writeUnder(t, project, "docs/testing.md", "testing\n")

	if got, want := draw(c), "📋3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Import chain run 5 hops deep, and every file on it load.
func TestConfigFollowsImportChain(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "@a.md\n")
	writeUnder(t, project, "a.md", "@b.md\n")
	writeUnder(t, project, "b.md", "leaf\n")

	if got, want := draw(c), "📋3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// File importing its own importer otherwise walk until hop cap, counting same
// two files five times over.
func TestConfigCountsCyclicImportOnce(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "@a.md\n")
	writeUnder(t, project, "a.md", "@CLAUDE.md\n")

	if got, want := draw(c), "📋2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Docs showing import syntax must not load what they document.
func TestConfigSkipsImportsInsideCode(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "write `@fenced.md` inline\n\n```\n@fenced.md\n```\n")
	writeUnder(t, project, "fenced.md", "not loaded\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Fence close on its own character only, so "~~~" inside "```" block is code
// that block hold, not a close. Toggle on either marker end block early and
// read rest as prose.
func TestConfigKeepsFenceOpenAcrossOtherMarker(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "```\n~~~\n@fenced.md\n```\n")
	writeUnder(t, project, "fenced.md", "not loaded\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Chain run 5 hops, so file sitting 6 hops out never load and never count.
func TestConfigStopsImportChainAtHopCap(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "@hop1.md\n")
	for i := 1; i <= maxImportHops; i++ {
		writeUnder(t, project, "hop"+strconv.Itoa(i)+".md", "@hop"+strconv.Itoa(i+1)+".md\n")
	}

	// Root plus 5 hops = 6. hop6.md sit past cap.
	if got, want := draw(c), "📋6"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// "~" mean home even under CLAUDE_CONFIG_DIR: reference is path somebody typed
// in their own file, not config location this program own.
func TestConfigExpandsTildeImport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// USERPROFILE set too: os.UserHomeDir read that one on Windows.
	t.Setenv("USERPROFILE", home)
	c := configCtx(t)
	writeUnder(t, home, "shared/style.md", "style\n")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "@~/shared/style.md\n")

	if got, want := draw(c), "📋2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Reference naming no file cost one Stat and count nothing, so "@mention" in
// prose need no rule of its own.
func TestConfigIgnoresImportsNamingNoFile(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "ping @devemberx about @anthropic-ai/sdk\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Generated CLAUDE.md listing more files than budget stop walk, and stopping
// prove no zero.
func TestConfigMarksImportsPastFileBudgetUnknown(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	var body strings.Builder
	for i := range maxImportFiles + 10 {
		name := "part" + strconv.Itoa(i) + ".md"
		writeUnder(t, project, filepath.Join("parts", name), "x\n")
		body.WriteString("@parts/" + name + "\n")
	}
	writeUnder(t, project, "CLAUDE.md", body.String())

	if got, want := draw(c), "📋…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// File budget alone leave 200 MiB readable per render, so bytes carry own
// budget. Few files under per-file cap still pass it.
func TestConfigMarksImportsPastByteBudgetUnknown(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	pad := strings.Repeat("x", maxConfigBytes-1) + "\n"
	var body strings.Builder
	for i := range int(maxImportBytes/maxConfigBytes) + 1 {
		name := "part" + strconv.Itoa(i) + ".md"
		writeUnder(t, project, filepath.Join("parts", name), pad)
		body.WriteString("@parts/" + name + "\n")
	}
	writeUnder(t, project, "CLAUDE.md", body.String())

	if got, want := draw(c), "📋…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Instruction tree measured on real machine total 50 KiB, so budget bound
// pathological tree alone and normal one count exact.
func TestConfigCountsImportsUnderByteBudget(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, filepath.Join("parts", "one.md"), strings.Repeat("x", 32<<10))
	writeUnder(t, project, filepath.Join("parts", "two.md"), strings.Repeat("x", 32<<10))
	writeUnder(t, project, "CLAUDE.md", "@parts/one.md\n@parts/two.md\n")

	if got, want := draw(c), "📋3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Sparse file report apparent size near int64 max, and three of them carry
// budget past int64 min. Subtracting first wrap it back positive, handing walk
// budget it already spent.
func TestImportWalkChargeRefusesSizeOverBudget(t *testing.T) {
	w := importWalk{bytes: maxImportBytes}
	for i := range 3 {
		if w.charge(1 << 62) {
			t.Fatalf("charge %d of 1<<62 accepted against %d budget", i+1, w.bytes)
		}
		if w.bytes != maxImportBytes {
			t.Fatalf("refused charge %d moved budget to %d", i+1, w.bytes)
		}
	}
	if !w.charge(1 << 10) {
		t.Errorf("1 KiB refused against %d budget", w.bytes)
	}
}

// File sitting at last hop get counted and never scanned, so its size cost no
// budget. Charging it shrink what reads that do happen may spend.
func TestConfigChargesNoBytesForLastHopFile(t *testing.T) {
	c := configCtx(t)
	project := c.In.Workspace.ProjectDir
	writeUnder(t, project, "CLAUDE.md", "@hop1.md\n")
	for i := 1; i < maxImportHops; i++ {
		writeUnder(t, project, "hop"+strconv.Itoa(i)+".md", "@hop"+strconv.Itoa(i+1)+".md\n")
	}
	leaf := "hop" + strconv.Itoa(maxImportHops) + ".md"
	writeUnder(t, project, leaf, strings.Repeat("x", maxImportBytes+1))

	// Root plus 5 hops = 6, leaf outweigh whole budget and still cost nothing.
	if got, want := draw(c), "📋6"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Prose carrying "@" past reference budget name no file, but resolving each one
// still cost a stat.
func TestConfigMarksImportsPastRefBudgetUnknown(t *testing.T) {
	c := configCtx(t)
	var body strings.Builder
	for i := range maxImportRefs + 10 {
		body.WriteString("ping @nobody" + strconv.Itoa(i) + "\n")
	}
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", body.String())

	if got, want := draw(c), "📋…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Unreadable instruction file hide its imports, and hidden import prove no
// count.
func TestConfigMarksClaudeMdPastByteCapUnknown(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", strings.Repeat("x", maxConfigBytes+1))

	if got, want := draw(c), "📋…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Reference budget bound stat calls, not list scanImports build ahead of them.
// 1 MiB of "@a.md" line resolve 175k path before walk spend its first, so limit
// stop collecting where budget would stop walking.
func TestScanImportsStopsAtLimit(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for range 100 {
		body.WriteString("@a.md\n")
	}
	writeUnder(t, dir, "CLAUDE.md", body.String())

	refs, err := scanImports(filepath.Join(dir, "CLAUDE.md"), 8)
	if err != nil {
		t.Fatalf("scanImports: %v", err)
	}
	if len(refs) != 8 {
		t.Errorf("collected %d refs, want 8", len(refs))
	}
}

func TestConfigCountsRulesRecursively(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "rules/style.md", "x\n")
	writeUnder(t, c.ConfigDir, "rules/go/errors.md", "x\n")
	writeUnder(t, c.ConfigDir, "rules/README.txt", "not a rule\n")
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/rules/pr.md", "x\n")

	if got, want := draw(c), "📏3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Same server named in two layers is one server, not two: project layer
// override user layer rather than add beside it.
func TestConfigDeduplicatesMCPServersByName(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json",
		`{"mcpServers": {"github": {}, "linear": {}},
		  "enabledMcpjsonServers": ["github", "postgres"]}`)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)

	if got, want := draw(c), "🔌3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// .mcp.json arrive with checkout, and Claude Code prompt before running any of
// it. Counting on sight print servers that never start.
func TestConfigSkipsUnapprovedProjectMCPServers(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json",
		`{"mcpServers": {"linear": {}}, "enabledMcpjsonServers": ["github"]}`)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)

	if got, want := draw(c), "🔌2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Answer live in .claude.json project block too, beside servers `claude mcp
// add` write.
func TestConfigApprovesProjectMCPServersFromClaudeJSON(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)
	writeUnder(t, c.ConfigDir, ".claude.json", `{
	  "projects": {
	    `+quotePath(t, c.In.Workspace.ProjectDir)+`: {"enabledMcpjsonServers": ["postgres"]}
	  }
	}`)

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// One switch approve whole file, so no name list exist to match against.
func TestConfigApprovesEveryProjectMCPServerAtOnce(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json", `{"enableAllProjectMcpServers": true}`)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)

	if got, want := draw(c), "🔌2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// MDM drop policy outside config root, and its hooks fire for every session on
// that machine. Counting config root alone print 0 where three run.
func TestConfigCountsManagedSettings(t *testing.T) {
	c := configCtx(t)
	managed := filepath.Join(t.TempDir(), "managed-settings.json")
	pointManagedSettings(t, managed)
	if err := os.WriteFile(managed, []byte(threeCommandSettings), 0o644); err != nil {
		t.Fatalf("write managed settings: %v", err)
	}

	if got, want := draw(c), "🪝3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Policy declare servers too, and those need no project approval.
func TestConfigCountsManagedMCPServers(t *testing.T) {
	c := configCtx(t)
	managed := filepath.Join(t.TempDir(), "managed-settings.json")
	pointManagedSettings(t, managed)
	if err := os.WriteFile(managed, []byte(`{"mcpServers": {"audit": {}}}`), 0o644); err != nil {
		t.Fatalf("write managed settings: %v", err)
	}

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Unreadable policy cost same two counts unreadable settings.json cost.
func TestConfigMarksUnreadableManagedSettingsUnknown(t *testing.T) {
	c := configCtx(t)
	managed := filepath.Join(t.TempDir(), "managed-settings.json")
	pointManagedSettings(t, managed)
	if err := os.WriteFile(managed, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("write managed settings: %v", err)
	}

	if got, want := draw(c), "🪝… · 🔌…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Plugin ship servers same two ways it ship hooks. Counting settings alone print
// 0 while plugin's own server answer tool calls.
func TestConfigCountsEnabledPluginMCPServers(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"plugin manifest": {".claude-plugin/plugin.json": `{"name": "p", "mcpServers": {"shrink": {}}}`},
		"manifest path": {
			".claude-plugin/plugin.json": `{"name": "p", "mcpServers": "./servers.json"}`,
			"servers.json":               `{"mcpServers": {"shrink": {}}}`,
		},
		"plugin root": {".mcp.json": `{"mcpServers": {"shrink": {}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			c := configCtx(t)
			install := enablePlugin(t, c, "p@m", true)
			for rel, body := range files {
				writeUnder(t, install, rel, body)
			}

			if got, want := draw(c), "🔌1"; got != want {
				t.Errorf("rendered %q, want %q", got, want)
			}
		})
	}
}

// Switching plugin on approve its servers already; no prompt ask twice.
func TestConfigCountsPluginMCPServersWithoutApproval(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "p@m", true)
	writeUnder(t, install, ".mcp.json", `{"mcpServers": {"shrink": {}}}`)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json", `{"mcpServers": {"github": {}}}`)

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Plugin switched off run nothing, servers included.
func TestConfigSkipsDisabledPluginMCPServers(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "p@m", false)
	writeUnder(t, install, ".mcp.json", `{"mcpServers": {"shrink": {}}}`)

	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// `claude mcp add` write .claude.json, not settings.json, so settings alone
// print 0 where servers connect. Global block apply everywhere; per-project
// block apply to its own key alone.
func TestConfigCountsMCPServersFromClaudeJSON(t *testing.T) {
	c := configCtx(t)
	other := filepath.Join(t.TempDir(), "elsewhere")
	writeUnder(t, filepath.Dir(c.ConfigDir), ".claude.json", `{
	  "mcpServers": {"sentry": {}},
	  "projects": {
	    `+quotePath(t, c.In.Workspace.ProjectDir)+`: {"mcpServers": {"postgres": {}}},
	    `+quotePath(t, other)+`: {"mcpServers": {"not-this-project": {}}}
	  }
	}`)

	if got, want := draw(c), "🔌2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// CLAUDE_CONFIG_DIR move whole tree, .claude.json included.
func TestConfigCountsMCPServersInsideConfigRoot(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, ".claude.json", `{"mcpServers": {"sentry": {}}}`)

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Same server in settings.json and .claude.json is one server.
func TestConfigDeduplicatesMCPServersAcrossFiles(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json", `{"mcpServers": {"github": {}}}`)
	writeUnder(t, c.ConfigDir, ".claude.json", `{"mcpServers": {"github": {}, "sentry": {}}}`)

	if got, want := draw(c), "🔌2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Zero is fact here -- directory read prove file absent -- but row saying "no
// hooks, no rules" every render is noise.
func TestConfigRendersNothingWhenEverythingIsZero(t *testing.T) {
	if got := draw(configCtx(t)); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// Counting only what parsed would print 1 while second hook fire unseen. Both
// hooks and MCP live in that file, so both lose; CLAUDE.md read elsewhere and
// keep its number.
func TestConfigMarksUnreadableSettingsUnknown(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json", "{ this is not json")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "project\n")

	if got, want := draw(c), "📋1 · 🪝… · 🔌…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// unknown = "" opt out of placeholders altogether, so a count nobody can read
// leave its item out rather than print a lie.
func TestConfigDropsUnknownCountWhenPlaceholderOptedOut(t *testing.T) {
	c := configCtx(t)
	c.Cfg.Unknown = ""
	writeUnder(t, c.ConfigDir, "settings.json", "{ this is not json")
	writeUnder(t, c.In.Workspace.ProjectDir, "CLAUDE.md", "project\n")

	if got, want := draw(c), "📋1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// preview and doctor may run before home directory resolve.
func TestConfigRendersNothingWithoutRoots(t *testing.T) {
	c := ctx(t, fixtures.Full, "config")
	c.ConfigDir = ""
	c.In.Workspace.ProjectDir = ""
	c.In.Workspace.CurrentDir = ""
	c.In.CWD = ""

	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// Icon alone read as puzzle. {labeled} spell each count out, dropping zeros
// same as {summary} -- template has no conditional, so user writing labels by
// hand carry "rules 0 · mcp 0" forever.
func TestConfigLabeledNamesEachCount(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "CLAUDE.md", "user\n")
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/settings.json", threeCommandSettings)
	c.Cfg.Template = "{labeled}"

	if got, want := draw(c), "📋 CLAUDE.md 1 · 🪝 hooks 3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

func TestConfigLabeledMarksUnknown(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "settings.json", "{ this is not json")
	c.Cfg.Template = "{labeled}"

	if got, want := draw(c), "🪝 hooks … · 🔌 MCP …"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

func TestConfigLabeledEmptyWhenEverythingIsZero(t *testing.T) {
	c := configCtx(t)
	c.Cfg.Template = "{labeled}"

	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// Counts addressable one at a time, so a row may carry hooks alone.
func TestConfigFieldsAddressableIndividually(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.ConfigDir, "CLAUDE.md", "user\n")
	writeUnder(t, c.In.Workspace.ProjectDir, ".claude/settings.json", threeCommandSettings)
	c.Cfg.Template = "{claude_md}/{rules}/{hooks}/{mcp}"

	if got, want := draw(c), "1/0/3/0"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Rules root symlinked into dotfiles checkout is normal setup, and Claude Code
// read through it. Refusing print 0 where rules load.
func TestConfigFollowsSymlinkedRulesRoot(t *testing.T) {
	c := configCtx(t)
	target := t.TempDir()
	writeUnder(t, target, "style.md", "x\n")
	writeUnder(t, target, "go/errors.md", "x\n")

	if err := os.Symlink(target, filepath.Join(c.ConfigDir, "rules")); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	if got, want := draw(c), "📏2"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// settings.json symlinked into dotfiles checkout is normal too, and config read
// follow it where caveman flag read refuse. openNonblock and
// openNonblockNoFollow now sit in one file, so handing this caller wrong one
// cost hooks and servers silently.
func TestConfigFollowsSymlinkedSettings(t *testing.T) {
	c := configCtx(t)
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte(threeCommandSettings), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	if err := os.Symlink(target, filepath.Join(c.ConfigDir, "settings.json")); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	if got, want := draw(c), "🪝3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Link planted below root walk wherever it aim, so entries stay Lstat.
func TestConfigSkipsSymlinkedRuleEntries(t *testing.T) {
	c := configCtx(t)
	target := t.TempDir()
	writeUnder(t, target, "planted.md", "x\n")
	writeUnder(t, c.ConfigDir, "rules/own.md", "x\n")

	if err := os.Symlink(target, filepath.Join(c.ConfigDir, "rules", "linked")); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	if got, want := draw(c), "📏1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Root followed through symlink mean rules pointed at / reach whole filesystem.
// Budget bound walk whatever shape tree take -- count past it is unknown, not
// number that stop where patience did.
func TestConfigMarksOversizeRuleTreeUnknown(t *testing.T) {
	c := configCtx(t)
	for d := range 3 {
		dir := filepath.Join(c.ConfigDir, "rules", strconv.Itoa(d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for i := range maxRuleEntries - 100 {
			if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(i)+".md"), []byte("x"), 0o644); err != nil {
				t.Fatalf("write rule: %v", err)
			}
		}
	}

	if got, want := draw(c), "📏…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Directory nested past depth cap stop walk, and stopping is not proof of zero.
func TestConfigMarksDeepRuleTreeUnknown(t *testing.T) {
	c := configCtx(t)
	deep := filepath.Join(c.ConfigDir, "rules")
	for range maxRuleDepth + 2 {
		deep = filepath.Join(deep, "down")
	}
	writeUnder(t, deep, "buried.md", "x\n")

	if got, want := draw(c), "📏…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Manifest declare hooks as path to file holding them, not object alone. Reading
// only object form make every such plugin a type error, and one plugin shipping
// it drag whole row's hook count to "…".
func TestConfigCountsHooksFromManifestPath(t *testing.T) {
	for name, decl := range map[string]string{
		"relative path":   `"./hooks/hooks.json"`,
		"plugin root var": `"${CLAUDE_PLUGIN_ROOT}/hooks/hooks.json"`,
		"path list":       `["./hooks/hooks.json", "./extra/more.json"]`,
	} {
		t.Run(name, func(t *testing.T) {
			c := configCtx(t)
			install := enablePlugin(t, c, "p@m", true)
			writeUnder(t, install, ".claude-plugin/plugin.json", `{"name": "p", "hooks": `+decl+`}`)
			writeUnder(t, install, "hooks/hooks.json", `{`+oneHookBody+`}`)
			writeUnder(t, install, "extra/more.json", `{`+oneHookBody+`}`)

			want := "🪝1"
			if strings.HasPrefix(decl, "[") {
				want = "🪝2"
			}
			if got := draw(c); got != want {
				t.Errorf("rendered %q, want %q", got, want)
			}
		})
	}
}

// Path reaching out of plugin directory is not that plugin's hook file, whatever
// sit at other end.
func TestConfigIgnoresManifestHookPathOutsidePlugin(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "p@m", true)
	outside := t.TempDir()
	writeUnder(t, outside, "hooks.json", `{`+oneHookBody+`}`)
	writeUnder(t, install, ".claude-plugin/plugin.json",
		`{"name": "p", "hooks": `+quotePath(t, filepath.Join(outside, "hooks.json"))+`}`)

	if got := draw(c); got != "" {
		t.Errorf("rendered %q, want nothing", got)
	}
}

// mcpServers carry path form same way hooks do. Manifest read through settings
// shape turn that into type error, losing hooks declared right beside it.
func TestConfigCountsManifestHooksBesideMCPPath(t *testing.T) {
	c := configCtx(t)
	install := enablePlugin(t, c, "p@m", true)
	writeUnder(t, install, ".claude-plugin/plugin.json",
		`{"name": "p", "mcpServers": "./.mcp.json", `+oneHookBody+`}`)

	if got, want := draw(c), "🪝1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Server switched off never connect. Counting it print number no client back.
func TestConfigSkipsDisabledMCPServers(t *testing.T) {
	c := configCtx(t)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)
	writeUnder(t, c.ConfigDir, ".claude.json", `{
	  "projects": {
	    `+quotePath(t, c.In.Workspace.ProjectDir)+`: {
	      "mcpServers": {"sentry": {}},
	      "enabledMcpjsonServers": ["github", "postgres"],
	      "disabledMcpjsonServers": ["postgres"],
	      "disabledMcpServers": ["sentry"]
	    }
	  }
	}`)

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// .claude.json hold per-project session metrics beside mcpServers, so it outgrow
// settings cap on machine carrying many projects. Same cap there print "…" for
// every one of them.
func TestConfigCountsMCPServersInLargeClaudeJSON(t *testing.T) {
	c := configCtx(t)
	pad := strings.Repeat("x", maxConfigBytes)
	writeUnder(t, filepath.Dir(c.ConfigDir), ".claude.json",
		`{"history": "`+pad+`", "mcpServers": {"sentry": {}}}`)

	if got, want := draw(c), "🔌1"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// Past its own cap, parse cost more than row's whole budget. Unknown, not
// number bought by stalling render.
func TestConfigMarksOversizeClaudeJSONUnknown(t *testing.T) {
	c := configCtx(t)
	pad := strings.Repeat("x", maxClaudeJSONBytes)
	writeUnder(t, filepath.Dir(c.ConfigDir), ".claude.json",
		`{"history": "`+pad+`", "mcpServers": {"sentry": {}}}`)

	if got, want := draw(c), "🔌…"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}
