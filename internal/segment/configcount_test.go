package segment

import (
	"os"
	"path/filepath"
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
		`{"version": 2, "plugins": {"`+key+`": [{"scope": "user", "installPath": "`+install+`"}]}}`)
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
	    "`+c.In.Workspace.ProjectDir+`": {"mcpServers": {"postgres": {}}},
	    "`+other+`": {"mcpServers": {"not-this-project": {}}}
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

// Rules directory symlinked at / would walk whole filesystem every render.
func TestConfigRefusesSymlinkedRuleDirectory(t *testing.T) {
	c := configCtx(t)
	real := t.TempDir()
	writeUnder(t, real, "planted.md", "x\n")

	link := filepath.Join(c.ConfigDir, "rules")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	if got := draw(c); got != "" {
		t.Errorf("followed symlink and rendered %q, want nothing", got)
	}
}
