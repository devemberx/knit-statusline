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
	return c
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
		`{"mcpServers": {"github": {}, "linear": {}}}`)
	writeUnder(t, c.In.Workspace.ProjectDir, ".mcp.json",
		`{"mcpServers": {"github": {}, "postgres": {}}}`)

	if got, want := draw(c), "🔌3"; got != want {
		t.Errorf("rendered %q, want %q", got, want)
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
