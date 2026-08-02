package segment

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("config", Def{
		Fields:          []string{"claude_md", "rules", "hooks", "mcp", "summary", "labeled"},
		DefaultTemplate: "{summary}",
		Build:           buildConfigCount,
	})
}

// U+1FA9D hook is Emoji 13.0, later than every other icon here and than
// caveman.go's bone. Fishing pole U+1F3A3 sit in 6.0 and cover more fonts, but
// read as fishing, not as hook -- glyph nobody decode buy no coverage.
const (
	claudeMDIcon = "📋"
	rulesIcon    = "📏"
	hooksIcon    = "🪝"
	mcpIcon      = "🔌"
)

// Bound one settings, manifest or registry read. Nothing legitimate here reach
// a megabyte, and settings.json symlinked at /dev/zero read forever otherwise.
const maxConfigBytes = 1 << 20

// Bound rules walk. Directory of symlinks pointing at each other cost depth;
// generated tree cost entries. Neither belong on render path.
const (
	maxRuleDepth   = 8
	maxRuleEntries = 500
)

var errConfigTooBig = errors.New("config file over size cap")

// configCount hold one category. ok=false mean some source failed to read, so
// n understate what run -- three facts kept apart, never folded into zero.
type configCount struct {
	n  int
	ok bool
}

func (c *configCount) lost() { c.ok = false }

type configCounts struct {
	claudeMD configCount
	rules    configCount
	hooks    configCount
	mcp      configCount
}

func buildConfigCount(c Context) Result {
	user, project := c.ConfigDir, projectRoot(c)
	if user == "" && project == "" {
		return empty
	}

	counts := scanConfig(user, project)
	f := render.Fields{
		"claude_md": configField(c, counts.claudeMD),
		"rules":     configField(c, counts.rules),
		"hooks":     configField(c, counts.hooks),
		"mcp":       configField(c, counts.mcp),
	}
	f["summary"] = render.Plain(configSummary(c, counts))
	f["labeled"] = render.Plain(configLabeled(c, counts))
	return Result{Base: render.Dim, Fields: f}
}

// projectRoot pick scope project-level config sit under. workspace.project_dir
// is repository root; current_dir follow user into subdirectory, where
// .claude/settings.json do not live.
func projectRoot(c Context) string {
	if c.In == nil {
		return ""
	}
	if c.In.Workspace.ProjectDir != "" {
		return c.In.Workspace.ProjectDir
	}
	return c.In.Dir()
}

// configField render one count on its own, for template naming {hooks} alone.
// Unknown print placeholder rather than number nobody can back.
func configField(c Context, n configCount) render.Field {
	if !n.ok {
		return render.Colored(c.Cfg.Unknown, render.Dim)
	}
	return render.Colored(strconv.Itoa(n.n), render.White)
}

type configItem struct {
	icon  string
	label string
	n     configCount
}

func configItems(counts configCounts) []configItem {
	return []configItem{
		{claudeMDIcon, "CLAUDE.md", counts.claudeMD},
		{rulesIcon, "rules", counts.rules},
		{hooksIcon, "hooks", counts.hooks},
		{mcpIcon, "MCP", counts.mcp},
	}
}

// configSummary compose default text: icon and number, every zero dropped.
func configSummary(c Context, counts configCounts) string {
	return configJoin(c, counts, " · ", func(it configItem, value string) string {
		return c.Palette.Wrap(it.icon, render.Dim) + value
	})
}

// configLabeled name each count, for reader who read "📏3" as puzzle. Template
// carry no conditional, so labels written by hand keep "rules 0 · MCP 0"
// standing -- this drop them, way {summary} do.
func configLabeled(c Context, counts configCounts) string {
	return configJoin(c, counts, " · ", func(it configItem, value string) string {
		return c.Palette.Wrap(it.icon+" "+it.label+" ", render.Dim) + value
	})
}

// configJoin compose items worth drawing, draw deciding their shape.
//
// Counts stay static across a session, so item vanishing read as project
// difference, never as crash mid-render. Row carrying "📏0 🔌0" spend width on
// features this project does not use.
//
// unknown = "" opt out of placeholders, so unreadable count leave its item out
// -- number that understate is worse than no number.
func configJoin(c Context, counts configCounts, sep string, draw func(configItem, string) string) string {
	var out []string
	for _, it := range configItems(counts) {
		switch {
		case !it.n.ok:
			if c.Cfg.Unknown == "" {
				continue
			}
			out = append(out, draw(it, c.Palette.Wrap(c.Cfg.Unknown, render.Dim)))
		case it.n.n > 0:
			out = append(out, draw(it, c.Palette.Wrap(strconv.Itoa(it.n.n), render.White)))
		}
	}
	return strings.Join(out, sep)
}

// scanConfig read every layer Claude Code merge, user scope then project scope.
func scanConfig(user, project string) configCounts {
	counts := configCounts{
		claudeMD: configCount{ok: true},
		rules:    configCount{ok: true},
		hooks:    configCount{ok: true},
		mcp:      configCount{ok: true},
	}

	countClaudeMD(&counts.claudeMD, user, project)
	countRules(&counts.rules, user, project)

	docs := readSettings(&counts, user, project)
	for _, d := range docs {
		counts.hooks.n += countHookCommands(d.Hooks)
	}
	countPluginHooks(&counts.hooks, user, docs)
	countMCPServers(&counts.mcp, user, project, docs)

	return counts
}

// countClaudeMD count files carrying instruction.
//
// Empty file is what a fresh install leave at ~/.claude/CLAUDE.md, and it load
// nothing. Symlink followed on purpose: CLAUDE.md pointed at AGENTS.md is
// common, and Claude Code read through it. Size alone leave this path, so
// following leak no bytes.
func countClaudeMD(n *configCount, user, project string) {
	var paths []string
	if user != "" {
		paths = append(paths, filepath.Join(user, "CLAUDE.md"))
	}
	if project != "" {
		paths = append(paths,
			filepath.Join(project, "CLAUDE.md"),
			filepath.Join(project, "CLAUDE.local.md"),
			filepath.Join(project, ".claude", "CLAUDE.md"),
			filepath.Join(project, ".claude", "CLAUDE.local.md"),
		)
	}

	for _, p := range paths {
		fi, err := os.Stat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			n.lost()
		case fi.Mode().IsRegular() && fi.Size() > 0:
			n.n++
		}
	}
}

func countRules(n *configCount, user, project string) {
	var dirs []string
	if user != "" {
		dirs = append(dirs, filepath.Join(user, "rules"))
	}
	if project != "" {
		dirs = append(dirs, filepath.Join(project, ".claude", "rules"))
	}
	for _, d := range dirs {
		walkRules(n, d, 0)
	}
}

// walkRules count .md under one directory.
//
// Lstat, not Stat: rules symlinked at / walk whole filesystem every render.
// Same reason entries that are symlinks get skipped rather than followed.
func walkRules(n *configCount, dir string, depth int) {
	if depth > maxRuleDepth {
		n.lost()
		return
	}

	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		n.lost()
		return
	case !fi.Mode().IsDir():
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		n.lost()
		return
	}
	if len(entries) > maxRuleEntries {
		n.lost()
		return
	}

	for _, e := range entries {
		switch {
		case e.Type()&fs.ModeSymlink != 0:
		case e.IsDir():
			walkRules(n, filepath.Join(dir, e.Name()), depth+1)
		case strings.EqualFold(filepath.Ext(e.Name()), ".md"):
			n.n++
		}
	}
}

type hookGroup struct {
	Hooks []json.RawMessage `json:"hooks"`
}

// settingsDoc is subset of settings.json three counts read. Same shape serve
// .mcp.json and plugin manifests, which carry one key each.
type settingsDoc struct {
	Hooks          map[string][]hookGroup     `json:"hooks"`
	MCPServers     map[string]json.RawMessage `json:"mcpServers"`
	EnabledPlugins map[string]bool            `json:"enabledPlugins"`
}

// readSettings parse every settings layer once.
//
// One unreadable file cost hooks and MCP together: both counts live in it, and
// dropped bytes prove nothing either way. Plugin roster travel out through
// EnabledPlugins, so plugin hooks lose same way.
func readSettings(counts *configCounts, user, project string) []settingsDoc {
	var paths []string
	if user != "" {
		paths = append(paths,
			filepath.Join(user, "settings.json"),
			filepath.Join(user, "settings.local.json"),
		)
	}
	if project != "" {
		paths = append(paths,
			filepath.Join(project, ".claude", "settings.json"),
			filepath.Join(project, ".claude", "settings.local.json"),
		)
	}

	var docs []settingsDoc
	for _, p := range paths {
		var d settingsDoc
		switch err := readConfigJSON(p, &d); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			counts.hooks.lost()
			counts.mcp.lost()
		default:
			docs = append(docs, d)
		}
	}
	return docs
}

// countHookCommands count innermost layer.
//
// settings.json nest event, matcher group, command list. Counting either outer
// layer report 2 where 3 commands run, and command count is what user ask for.
func countHookCommands(hooks map[string][]hookGroup) int {
	var n int
	for _, groups := range hooks {
		for _, g := range groups {
			n += len(g.Hooks)
		}
	}
	return n
}

type installedPlugins struct {
	Plugins map[string][]struct {
		InstallPath string `json:"installPath"`
	} `json:"plugins"`
}

// countPluginHooks add hooks no settings file declare.
//
// Plugin hook live inside plugin's own directory, so counting settings.json
// alone print 0 while every prompt fire them. Roster sit in
// plugins/installed_plugins.json under user config root; enabledPlugins in
// settings say which of them run.
func countPluginHooks(n *configCount, user string, docs []settingsDoc) {
	if user == "" {
		return
	}

	enabled := map[string]bool{}
	for _, d := range docs {
		for key, on := range d.EnabledPlugins {
			enabled[key] = on
		}
	}
	if len(enabled) == 0 {
		return
	}

	var reg installedPlugins
	switch err := readConfigJSON(filepath.Join(user, "plugins", "installed_plugins.json"), &reg); {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		n.lost()
		return
	}

	for key, on := range enabled {
		if !on {
			continue
		}
		for _, inst := range reg.Plugins[key] {
			if inst.InstallPath == "" {
				continue
			}
			countManifestHooks(n, inst.InstallPath)
		}
	}
}

// countManifestHooks read one plugin's hook declaration.
//
// Three layouts ship today: hooks key inside .claude-plugin/plugin.json,
// hooks/hooks.json, and hooks.json at plugin root. Search run past file that
// parse but declare nothing -- claude-plugins-official carry plugin.json
// holding name and version alone, hooks sitting in hooks/hooks.json beside it.
// Plugin declaring none anywhere is 0, not unknown.
func countManifestHooks(n *configCount, install string) {
	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("hooks", "hooks.json"),
		"hooks.json",
	} {
		var d settingsDoc
		switch err := readConfigJSON(filepath.Join(install, rel), &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			n.lost()
			return
		}
		if c := countHookCommands(d.Hooks); c > 0 {
			n.n += c
			return
		}
	}
}

// claudeJSONDoc is subset of .claude.json MCP count read. `claude mcp add`
// write here, not settings.json, and key servers per project directory.
type claudeJSONDoc struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
	Projects   map[string]struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	} `json:"projects"`
}

// countMCPServers union names across every file declaring servers.
//
// Same server named twice is one server: later layer override earlier rather
// than add beside it. Summing print 3 where 2 connect.
func countMCPServers(n *configCount, user, project string, docs []settingsDoc) {
	seen := map[string]struct{}{}
	for _, d := range docs {
		for name := range d.MCPServers {
			seen[name] = struct{}{}
		}
	}

	if project != "" {
		var d settingsDoc
		switch err := readConfigJSON(filepath.Join(project, ".mcp.json"), &d); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			n.lost()
		default:
			for name := range d.MCPServers {
				seen[name] = struct{}{}
			}
		}
	}

	countClaudeJSONServers(n, seen, user, project)
	n.n += len(seen)
}

// countClaudeJSONServers add servers `claude mcp add` register.
//
// CLAUDE_CONFIG_DIR move file inside config root; default install leave it
// beside, at ~/.claude.json. First hit win -- both exist only when somebody
// moved config root and left old file behind.
func countClaudeJSONServers(n *configCount, seen map[string]struct{}, user, project string) {
	if user == "" {
		return
	}

	for _, p := range []string{
		filepath.Join(user, ".claude.json"),
		filepath.Join(filepath.Dir(user), ".claude.json"),
	} {
		var d claudeJSONDoc
		switch err := readConfigJSON(p, &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			n.lost()
			return
		}
		for name := range d.MCPServers {
			seen[name] = struct{}{}
		}
		// Other projects' servers never reach this session.
		for name := range d.Projects[project].MCPServers {
			seen[name] = struct{}{}
		}
		return
	}
}

// readConfigJSON decode capped file. Caller separate fs.ErrNotExist -- absent
// file prove zero, unreadable one prove nothing.
func readConfigJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxConfigBytes {
		return errConfigTooBig
	}
	return json.Unmarshal(b, v)
}
