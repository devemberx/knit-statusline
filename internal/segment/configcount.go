package segment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
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
	configClaudeMDIcon = "📋"
	configRulesIcon    = "📏"
	configHooksIcon    = "🪝"
	configMCPIcon      = "🔌"
)

// Bound one settings, manifest or registry read. Nothing legitimate here reach
// a megabyte, and settings.json symlinked at /dev/zero read forever otherwise.
const maxConfigBytes = 1 << 20

// .claude.json carry per-project session metrics beside mcpServers, so machine
// with many projects outgrow settings cap. Parse run near 8ms per MiB against
// ~41ms whole row, so 4 MiB is where reading cost more than it tell.
const maxClaudeJSONBytes = 4 << 20

// Bound rules walk. Directory of symlinks pointing at each other cost depth;
// generated tree cost entries. Root followed through symlink, so entry budget
// bound rules pointed at / to fixed work instead of whole filesystem.
const (
	maxRuleDepth   = 8
	maxRuleEntries = 500
	maxRuleTotal   = 1000
)

// Manifest path may point at file pointing at file. Two hops cover every layout
// shipping; deeper is loop.
const maxHookHops = 2

// Bound ancestor walk. Deepest checkout run near 20 segments; 40 leave room and
// still bound stat calls when stdin carry pathological path.
const maxAncestorDepth = 40

// Bound import walk. Claude Code stop following at 5 hops, so hop cap match
// what load. Three budgets under it: file budget bound reads, reference budget
// bound stat calls, byte budget bound their product. Prose carrying many "@"
// spend second budget alone.
//
// 200 files times 1 MiB read 200 MiB per render, uncached: measured near 74ms
// warm on darwin/arm64, disk-bound cold. Instruction tree on real machine total
// 50 KiB across 6 files, largest 25 KiB, so 4 MiB leave 80x headroom and cost
// near 1.4ms at same rate.
const (
	maxImportHops  = 5
	maxImportFiles = 200
	maxImportRefs  = 2000
	maxImportBytes = 4 << 20
)

// Bound plugin walk, way import walk bound. Every enabled plugin cost three
// manifest reads for hooks and two for MCP at maxConfigBytes each, before list
// form send walkPluginDecl two hops deeper. installed_plugins.json carry its own
// 1 MiB cap, which still hold thousands of install paths.
//
// Path budget bound stat calls plugin declaring nothing still pay; byte budget
// bound their product. Real roster of 20 plugins spend 100 paths and under
// 100 KiB, so both leave headroom maxImportBytes keep.
const (
	maxPluginPaths = 2000
	maxPluginBytes = 4 << 20
)

// Variable Claude Code expand inside plugin paths.
const pluginRootVar = "${CLAUDE_PLUGIN_ROOT}"

// Separator between items on row, both templates.
const configSep = " · "

// managedSettings hold enterprise policy layer. It sit outside config root, at
// fixed absolute path per OS, so CLAUDE_CONFIG_DIR never move it and test
// override this variable rather than plant file at /etc.
var managedSettings = defaultManagedSettings()

// defaultManagedSettings name path MDM drop policy at.
//
// Hooks and MCP servers declared there fire for every session on that machine,
// and nothing under config root mention them.
func defaultManagedSettings() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		// ProgramData relocate on some images; literal is fallback, not truth.
		dir := os.Getenv("PROGRAMDATA")
		if dir == "" {
			dir = `C:\ProgramData`
		}
		return filepath.Join(dir, "ClaudeCode", "managed-settings.json")
	default:
		return "/etc/claude-code/managed-settings.json"
	}
}

var (
	errConfigTooBig     = errors.New("config file over size cap")
	errConfigNotRegular = errors.New("config path not regular file")
	errPluginBudget     = errors.New("plugin walk over read budget")
)

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
		{configClaudeMDIcon, "CLAUDE.md", counts.claudeMD},
		{configRulesIcon, "rules", counts.rules},
		{configHooksIcon, "hooks", counts.hooks},
		{configMCPIcon, "MCP", counts.mcp},
	}
}

// configSummary compose default text: icon and number, every zero dropped.
func configSummary(c Context, counts configCounts) string {
	return configJoin(c, counts, func(it configItem, value string) string {
		return c.Palette.Wrap(it.icon, render.Dim) + value
	})
}

// configLabeled name each count, for reader who read "📏3" as puzzle. Template
// carry no conditional, so labels written by hand keep "rules 0 · MCP 0"
// standing -- this drop them, way {summary} do.
func configLabeled(c Context, counts configCounts) string {
	return configJoin(c, counts, func(it configItem, value string) string {
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
func configJoin(c Context, counts configCounts, draw func(configItem, string) string) string {
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
	return strings.Join(out, configSep)
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

	names := readMCPNames(&counts.mcp, user, project, docs)
	scanEnabledPlugins(&counts, names.direct, user, docs)
	counts.mcp.n += names.total()

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
		paths = append(paths, ancestorClaudeMD(n, project)...)
	}

	w := importWalk{
		n:     n,
		seen:  map[string]struct{}{},
		files: maxImportFiles,
		refs:  maxImportRefs,
		bytes: maxImportBytes,
	}
	for _, p := range paths {
		w.count(p, 0)
	}
}

// importWalk carry instruction count, files already counted, and every budget
// across every root.
type importWalk struct {
	n     *configCount
	seen  map[string]struct{}
	files int
	refs  int
	bytes int64
}

// charge spend one file's size, false when budget cannot cover it.
//
// Compared before subtracting: sparse file report apparent size near int64 max,
// and three of them carry budget past int64 min, wrapping it back positive and
// reopening reads walk refused.
//
// Charge is stat size, not bytes read, so file grown between stat and read cost
// up to per-file cap over budget. Bound hold against generated tree, not
// against writer racing render.
func (w *importWalk) charge(size int64) bool {
	if size > w.bytes {
		return false
	}
	w.bytes -= size
	return true
}

// count one instruction file plus every file it import.
//
// Empty file is what fresh install leave at <config root>/CLAUDE.md, and it
// load nothing. Symlink followed on purpose: CLAUDE.md pointed at AGENTS.md is
// common, and Claude Code read through it.
//
// seen key on cleaned path, not resolved identity: file importing its own
// importer stop at second visit, and two roots naming same path load it once.
// Symlink alias and case-different name on case-insensitive volume still count
// twice -- EvalSymlinks per node buy that at one syscall each, above what
// miscount cost.
func (w *importWalk) count(path string, hop int) {
	path = filepath.Clean(path)
	if _, dup := w.seen[path]; dup {
		return
	}
	if w.refs--; w.refs < 0 {
		w.n.lost()
		return
	}

	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		w.n.lost()
		return
	case !fi.Mode().IsRegular() || fi.Size() == 0:
		return
	}

	if w.files--; w.files < 0 {
		w.n.lost()
		return
	}

	w.seen[path] = struct{}{}
	w.n.n++

	// Claude Code stop at same hop, so stopping here match what load -- number
	// stay a fact, not a truncation.
	if hop >= maxImportHops {
		return
	}

	// Size charged before scan, since scan is what read it. File budget alone
	// leave 200 files times 1 MiB readable per render.
	if !w.charge(fi.Size()) {
		w.n.lost()
		return
	}

	// One past remaining budget: walk still trip on that extra reference, so
	// limit move no count, only list length.
	refs, err := scanImports(path, w.refs+1)
	if err != nil {
		w.n.lost()
		return
	}
	for _, ref := range refs {
		w.count(ref, hop+1)
	}
}

// scanImports list files one instruction file pull in.
//
// Claude Code read "@path/to/file" as import, resolved against directory of
// file holding it, and leave same text inside fenced block or backtick span
// alone -- docs showing that syntax must not load anything.
//
// Reference that resolve to nothing cost one Stat and count nothing, so "@user"
// in prose need no rule of its own.
//
// limit stop collecting where caller budget would stop walking: 1 MiB of "@a"
// line otherwise resolve 175k path, every one a joined string, before walk
// spend its first reference.
func scanImports(path string, limit int) ([]string, error) {
	b, err := readCappedFile(path, maxConfigBytes)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	var out []string
	var fence string
	for line := range strings.Lines(string(b)) {
		trimmed := strings.TrimSpace(line)
		if m := fenceMarker(trimmed); m != "" {
			switch fence {
			case "":
				fence = m
			case m:
				fence = ""
			}
			continue
		}
		// Line carrying no "@" hold no import, and skipping it before split
		// keep prose off two allocations per line.
		if fence != "" || !strings.ContainsRune(trimmed, '@') {
			continue
		}
		for _, ref := range lineImports(trimmed) {
			if p := resolveImport(ref, dir); p != "" {
				out = append(out, p)
				if len(out) >= limit {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

// fenceMarker name marker a line open or close fence with, "" when line is no
// fence.
//
// Fence close on its own character only, so "~~~" sitting inside a "```" block
// is content. Toggling on either marker close that block early and read rest of
// it as prose.
func fenceMarker(line string) string {
	switch {
	case strings.HasPrefix(line, "```"):
		return "```"
	case strings.HasPrefix(line, "~~~"):
		return "~~~"
	}
	return ""
}

// lineImports pick references out of one line, backtick span dropped.
func lineImports(line string) []string {
	var out []string
	for i, part := range strings.Split(line, "`") {
		// Odd field sit between backticks.
		if i%2 == 1 {
			continue
		}
		for _, field := range strings.Fields(part) {
			ref, ok := strings.CutPrefix(field, "@")
			if !ok {
				continue
			}
			// Sentence punctuation ride along: "see @docs/x.md." name no file.
			if ref = strings.TrimRight(ref, ".,;:)"); ref != "" {
				out = append(out, ref)
			}
		}
	}
	return out
}

// resolveImport turn reference into path.
//
// "~" mean home even under CLAUDE_CONFIG_DIR: it is a path somebody typed in
// their own file, not a config location this program own.
func resolveImport(ref, dir string) string {
	switch {
	case ref == "~" || strings.HasPrefix(ref, "~/"):
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(ref, "~"))
	case filepath.IsAbs(ref):
		return ref
	}
	return filepath.Join(dir, ref)
}

// ancestorClaudeMD list files every directory above project may hold.
//
// Claude Code walk from working directory to filesystem root, loading each
// CLAUDE.md it pass. Monorepo keep shared instruction one level up, and
// counting project directory alone report 1 where 3 load.
//
// Depth capped: path arrive from stdin, and one carrying 400 segments cost 800
// stat calls per render. Stopping short is not proof of zero, so cap resolve
// unknown.
func ancestorClaudeMD(n *configCount, project string) []string {
	var paths []string
	dir := project
	for range maxAncestorDepth {
		parent := filepath.Dir(dir)
		if parent == dir {
			return paths
		}
		dir = parent
		paths = append(paths,
			filepath.Join(dir, "CLAUDE.md"),
			filepath.Join(dir, "CLAUDE.local.md"),
		)
	}
	n.lost()
	return paths
}

func countRules(n *configCount, user, project string) {
	var dirs []string
	if user != "" {
		dirs = append(dirs, filepath.Join(user, "rules"))
	}
	if project != "" {
		dirs = append(dirs, filepath.Join(project, ".claude", "rules"))
	}
	w := ruleWalk{n: n, left: maxRuleTotal}
	for _, d := range dirs {
		w.walk(d, 0)
	}
}

// ruleWalk carry one count plus entry budget spanning every rules root.
type ruleWalk struct {
	n    *configCount
	left int
}

// walk count .md under one directory.
//
// Root read through Stat: ~/.claude/rules symlinked into dotfiles checkout is
// normal setup, and Claude Code load rules through it. Entries below stay
// Lstat -- link planted mid-tree aim wherever it like, and depth alone bound
// nothing when each level branch 500 wide.
func (w *ruleWalk) walk(dir string, depth int) {
	if depth > maxRuleDepth {
		w.n.lost()
		return
	}

	stat := os.Lstat
	if depth == 0 {
		stat = os.Stat
	}
	fi, err := stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		w.n.lost()
		return
	case !fi.Mode().IsDir():
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		w.n.lost()
		return
	}
	if len(entries) > maxRuleEntries {
		w.n.lost()
		return
	}
	if w.left -= len(entries); w.left < 0 {
		w.n.lost()
		return
	}

	for _, e := range entries {
		switch {
		case e.Type()&fs.ModeSymlink != 0:
		case e.IsDir():
			w.walk(filepath.Join(dir, e.Name()), depth+1)
		case strings.EqualFold(filepath.Ext(e.Name()), ".md"):
			w.n.n++
		}
	}
}

type hookGroup struct {
	Hooks []json.RawMessage `json:"hooks"`
}

// settingsDoc is subset of settings.json three counts read. Same shape serve
// .mcp.json, which carry mcpServers alone.
type settingsDoc struct {
	Hooks               map[string][]hookGroup     `json:"hooks"`
	MCPServers          map[string]json.RawMessage `json:"mcpServers"`
	EnabledPlugins      map[string]bool            `json:"enabledPlugins"`
	EnabledMCPJSON      []string                   `json:"enabledMcpjsonServers"`
	DisabledMCPJSON     []string                   `json:"disabledMcpjsonServers"`
	EnableAllProjectMCP bool                       `json:"enableAllProjectMcpServers"`
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
	// Last: enterprise policy override every layer above, and enabledPlugins
	// merge later-wins.
	paths = append(paths, managedSettings)

	var docs []settingsDoc
	for _, p := range paths {
		var d settingsDoc
		switch err := readConfigJSON(p, maxConfigBytes, &d); {
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

// scanEnabledPlugins add hooks and MCP servers no settings file declare.
//
// Both live inside plugin's own directory, so counting settings.json alone
// print 0 while every prompt fire them. Roster sit in
// plugins/installed_plugins.json under user config root; enabledPlugins in
// settings say which of them run.
//
// Registry read once for both counts: two passes cost same file twice and let
// them disagree on which plugin ran. Registry charged to walk budget too --
// roster carry install path every later read start from.
func scanEnabledPlugins(counts *configCounts, servers map[string]struct{}, user string, docs []settingsDoc) {
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

	w := pluginWalk{
		hooks: &counts.hooks,
		mcp:   &counts.mcp,
		paths: maxPluginPaths,
		bytes: maxPluginBytes,
	}
	var reg installedPlugins
	switch err := w.read(filepath.Join(user, "plugins", "installed_plugins.json"), &reg); {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		counts.hooks.lost()
		counts.mcp.lost()
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
			w.countManifestHooks(inst.InstallPath)
			w.countPluginMCP(servers, inst.InstallPath)
		}
	}
}

// pluginWalk carry both counts plus every budget spanning every enabled plugin.
//
// One budget set, not one per plugin: cost bound here is product of roster
// length and per-plugin reads, and per-plugin budget bound neither.
type pluginWalk struct {
	hooks *configCount
	mcp   *configCount
	paths int
	bytes int64
}

// read decode one plugin file, spending path budget on look and byte budget on
// what came back.
//
// Both compared before subtracting, way importWalk.charge do. Exhausted budget
// answer errPluginBudget, which every caller resolve to unknown -- count that
// stop early understate what run.
//
// Bytes charged after read, not off Stat size: read already capped at
// maxConfigBytes, so overshoot bound to one file and no sparse size reach
// subtraction.
func (w *pluginWalk) read(path string, v any) error {
	if w.paths <= 0 {
		return errPluginBudget
	}
	w.paths--

	b, err := readCappedFile(path, maxConfigBytes)
	if err != nil {
		return err
	}
	if int64(len(b)) > w.bytes {
		return errPluginBudget
	}
	w.bytes -= int64(len(b))
	return json.Unmarshal(b, v)
}

// pluginDoc is two keys alone, undecoded. Manifest carry commands, agents and
// version beside them in shapes settingsDoc do not describe, and one type
// mismatch anywhere in a file fail whole decode -- plugin lost to "…" over a key
// nothing here count.
type pluginDoc struct {
	Hooks      json.RawMessage `json:"hooks"`
	MCPServers json.RawMessage `json:"mcpServers"`
}

// countManifestHooks read one plugin's hook declaration.
//
// Three layouts ship today: hooks key inside .claude-plugin/plugin.json,
// hooks/hooks.json, and hooks.json at plugin root. Search run past file that
// parse but declare nothing -- claude-plugins-official carry plugin.json
// holding name and version alone, hooks sitting in hooks/hooks.json beside it.
// Plugin declaring none anywhere is 0, not unknown.
func (w *pluginWalk) countManifestHooks(install string) {
	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("hooks", "hooks.json"),
		"hooks.json",
	} {
		var d pluginDoc
		switch err := w.read(filepath.Join(install, rel), &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			w.hooks.lost()
			return
		}

		var c int
		err := w.walkPluginDecl(d.Hooks, install, "hooks", 0, func(obj json.RawMessage) error {
			var events map[string][]hookGroup
			if err := json.Unmarshal(obj, &events); err != nil {
				return err
			}
			c += countHookCommands(events)
			return nil
		})
		if err != nil {
			w.hooks.lost()
			return
		}
		if c > 0 {
			w.hooks.n += c
			return
		}
	}
}

// countPluginMCP union server names one enabled plugin declare.
//
// Servers ship two ways: mcpServers key in .claude-plugin/plugin.json, same
// three shapes hooks take, and .mcp.json at plugin root. Both candidates read,
// not first hit alone -- plugin splitting servers across them run all of them.
//
// Plugin's own servers need no project approval: switching plugin on approve
// them already, and no prompt ask twice.
func (w *pluginWalk) countPluginMCP(seen map[string]struct{}, install string) {
	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		".mcp.json",
	} {
		var d pluginDoc
		switch err := w.read(filepath.Join(install, rel), &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			w.mcp.lost()
			return
		}

		err := w.walkPluginDecl(d.MCPServers, install, "mcpServers", 0, func(obj json.RawMessage) error {
			var servers map[string]json.RawMessage
			if err := json.Unmarshal(obj, &servers); err != nil {
				return err
			}
			for name := range servers {
				seen[name] = struct{}{}
			}
			return nil
		})
		if err != nil {
			w.mcp.lost()
			return
		}
	}
}

// walkPluginDecl resolve one manifest declaration, handing each inline object
// to fn.
//
// Manifest write declaration inline, as path to file holding it, or as list of
// such paths. Reading object form alone make every path-form plugin a type
// error, and one of them drag row's whole count to "…".
//
// key name what pointed-at file wrap its object in: hooks file carry "hooks",
// .mcp.json carry "mcpServers".
func (w *pluginWalk) walkPluginDecl(raw json.RawMessage, install, key string, hop int, fn func(json.RawMessage) error) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}

	switch raw[0] {
	case '{':
		return fn(raw)
	case '"':
		var p string
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return w.walkPluginPath(p, install, key, hop, fn)
	case '[':
		var paths []json.RawMessage
		if err := json.Unmarshal(raw, &paths); err != nil {
			return err
		}
		for _, p := range paths {
			if err := w.walkPluginDecl(p, install, key, hop, fn); err != nil {
				return err
			}
		}
	}
	// Number or bool declare nothing, and neither prove file unreadable.
	return nil
}

// countHookPath read hooks file manifest point at.
//
// Resolved path stay inside plugin: manifest reaching out of its own install
// directory is not that plugin's hook file, whatever sit at other end. Declared
// file missing is 0 -- plugin ship broken path, not unreadable bytes.
func (w *pluginWalk) walkPluginPath(p, install, key string, hop int, fn func(json.RawMessage) error) error {
	if hop >= maxHookHops || p == "" {
		return nil
	}

	p = strings.ReplaceAll(p, pluginRootVar, install)
	if !filepath.IsAbs(p) {
		p = filepath.Join(install, p)
	}
	p = filepath.Clean(p)
	rel, err := filepath.Rel(install, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}

	var d map[string]json.RawMessage
	switch err := w.read(p, &d); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	return w.walkPluginDecl(d[key], install, key, hop+1, fn)
}

// claudeJSONDoc is subset of .claude.json MCP count read. `claude mcp add`
// write here, not settings.json, and key servers per project directory.
type claudeJSONDoc struct {
	MCPServers map[string]json.RawMessage   `json:"mcpServers"`
	Projects   map[string]claudeJSONProject `json:"projects"`
}

type claudeJSONProject struct {
	MCPServers      map[string]json.RawMessage `json:"mcpServers"`
	EnabledMCPJSON  []string                   `json:"enabledMcpjsonServers"`
	DisabledMCPJSON []string                   `json:"disabledMcpjsonServers"`
	DisabledMCP     []string                   `json:"disabledMcpServers"`
}

// mcpNames keep server names apart by origin.
//
// Project .mcp.json arrive with checkout, somebody else's file, so Claude Code
// hold its servers until answered. Counting them on sight print number for
// servers that never start.
type mcpNames struct {
	// User settings, .claude.json and plugins. Run as written.
	direct map[string]struct{}
	// Project .mcp.json. Run once approved.
	project map[string]struct{}

	approved map[string]struct{}
	off      map[string]struct{}
	all      bool
}

func newMCPNames() *mcpNames {
	return &mcpNames{
		direct:   map[string]struct{}{},
		project:  map[string]struct{}{},
		approved: map[string]struct{}{},
		off:      map[string]struct{}{},
	}
}

// total count servers this session reach.
//
// Same server named twice is one server: later layer override earlier rather
// than add beside it. Summing print 3 where 2 connect.
//
// Off list carry names, not origins, so server named in two files and disabled
// in one drop from both. Rejecting a .mcp.json server and running your own by
// same name is a collision nobody arrange on purpose.
func (m *mcpNames) total() int {
	live := maps.Clone(m.direct)
	for name := range m.project {
		if _, ok := m.approved[name]; ok || m.all {
			live[name] = struct{}{}
		}
	}
	for name := range m.off {
		delete(live, name)
	}
	return len(live)
}

// readMCPNames gather server names off every file declaring them.
func readMCPNames(n *configCount, user, project string, docs []settingsDoc) *mcpNames {
	names := newMCPNames()
	for _, d := range docs {
		for name := range d.MCPServers {
			names.direct[name] = struct{}{}
		}
		addNames(names.approved, d.EnabledMCPJSON)
		addNames(names.off, d.DisabledMCPJSON)
		names.all = names.all || d.EnableAllProjectMCP
	}

	if project != "" {
		var d settingsDoc
		switch err := readConfigJSON(filepath.Join(project, ".mcp.json"), maxConfigBytes, &d); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			n.lost()
		default:
			for name := range d.MCPServers {
				names.project[name] = struct{}{}
			}
		}
	}

	readClaudeJSONServers(n, names, user, project)
	return names
}

func addNames(set map[string]struct{}, names []string) {
	for _, name := range names {
		set[name] = struct{}{}
	}
}

// readClaudeJSONServers add servers `claude mcp add` register, plus answer given
// to this project's .mcp.json prompt.
//
// CLAUDE_CONFIG_DIR move file inside config root; default install leave it
// beside, at ~/.claude.json. First hit win -- both exist only when somebody
// moved config root and left old file behind.
func readClaudeJSONServers(n *configCount, names *mcpNames, user, project string) {
	if user == "" {
		return
	}

	for _, p := range []string{
		filepath.Join(user, ".claude.json"),
		filepath.Join(filepath.Dir(user), ".claude.json"),
	} {
		var d claudeJSONDoc
		switch err := readConfigJSON(p, maxClaudeJSONBytes, &d); {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			n.lost()
			return
		}
		for name := range d.MCPServers {
			names.direct[name] = struct{}{}
		}
		// Other projects' servers never reach this session.
		proj := d.Projects[project]
		for name := range proj.MCPServers {
			names.direct[name] = struct{}{}
		}
		addNames(names.approved, proj.EnabledMCPJSON)
		addNames(names.off, proj.DisabledMCPJSON)
		addNames(names.off, proj.DisabledMCP)
		return
	}
}

// readConfigJSON decode capped regular file. Caller separate fs.ErrNotExist --
// absent file prove zero, unreadable one prove nothing.
func readConfigJSON(path string, limit int64, v any) error {
	b, err := readCappedFile(path, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// readCappedFile read regular file up to limit.
//
// Stat first keep non-regular path away from open at all: settings.json or
// CLAUDE.md as FIFO hold blocking open until some writer arrive, and render
// path carry no timeout to cut that. Blocked render print nothing, which read
// as crash. Outside unix that Stat stand as only guard before open. Symlink
// followed on purpose -- settings.json pointed into dotfiles checkout is
// normal, and no byte read here reach row.
func readCappedFile(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errConfigNotRegular
	}
	return readRegular(path, limit)
}

// readRegular open path and read it, refusing anything but regular file.
//
// Stat above and open here are two syscalls, and between them path get renamed
// onto FIFO -- Stat clear it as regular, open block on it anyway. openNonblock
// return without waiting on unix; handle's own Stat then refuse what name no
// longer describe.
//
// That Stat not redundant with O_NONBLOCK: read on nonblocking FIFO fd park in
// runtime poller on linux, and return EOF on darwin -- empty bytes counted as
// zero config.
//
// openNonblock, not openNonblockNoFollow: settings.json pointed into dotfiles
// checkout is normal here, unlike caveman flag.
func readRegular(path string, limit int64) ([]byte, error) {
	f, err := openNonblock(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errConfigNotRegular
	}

	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errConfigTooBig
	}
	return b, nil
}
