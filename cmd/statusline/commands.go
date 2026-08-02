package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/install"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/segment"
	"github.com/devemberx/knit-statusline/internal/statusline"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

// Subcommands only. Render path exit non-zero never.
//
// stderr, so stdout carry rendered row alone and survive pipe.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// rootHint name knob that supplied relative root. install package read no
// environment, so only caller know value came from CLAUDE_CONFIG_DIR rather
// than from relative $HOME, and remedy differ per source.
func rootHint(err error) error {
	if errors.Is(err, install.ErrRelativeRoot) && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return fmt.Errorf("%w; set CLAUDE_CONFIG_DIR to an absolute path", err)
	}
	return err
}

// flags parse args with flag package's own output suppressed, so message and
// exit code both come from here instead of arriving twice.
func flags(name string, args []string, bind func(*flag.FlagSet)) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bind(fs)
	return fs.Parse(args)
}

// badFlag report unparseable args. Silent exit 2 leave user at prompt with no
// idea which flag was wrong.
//
// -h reach here as flag.ErrHelp. Asking for help is no error.
func badFlag(stdout, stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		usage(stdout)
		return 0
	}
	fmt.Fprintln(stderr, "error:", err)
	fmt.Fprintln(stderr, "run `knit-statusline help` for usage")
	return 2
}

func runInstall(args []string, stdout, stderr io.Writer) int {
	var preset *string
	var force *bool
	err := flags("install", args, func(fs *flag.FlagSet) {
		preset = fs.String("preset", config.DefaultPreset, "starting layout")
		force = fs.Bool("force", false, "replace an existing statusline.toml")
	})
	if err != nil {
		return badFlag(stdout, stderr, err)
	}

	binary, err := os.Executable()
	if err != nil {
		return fail(stderr, fmt.Errorf("locating this binary: %w", err))
	}

	res, err := install.Install(install.Options{
		Root: configDir(), Binary: binary, Preset: *preset, Force: *force,
	})
	if err != nil {
		return fail(stderr, rootHint(err))
	}

	// Literal compare call own slashed or quoted entry "replaced" on reinstall.
	if res.ReplacedCommand != "" && !install.OwnsCommand(res.ReplacedCommand, res.InstalledBinary) {
		fmt.Fprintf(stdout, "replaced the previous status line: %s\n", res.ReplacedCommand)
	}
	if res.BackupPath != "" {
		fmt.Fprintf(stdout, "backed up %s\n", res.BackupPath)
	}
	fmt.Fprintf(stdout, "installed %s\n", res.InstalledBinary)
	fmt.Fprintf(stdout, "configured %s\n", res.SettingsPath)

	if res.ConfigWrote {
		fmt.Fprintf(stdout, "wrote %s (preset %q)\n", res.ConfigPath, *preset)
	} else {
		fmt.Fprintf(stdout, "kept your existing %s (use --force to replace it)\n", res.ConfigPath)
	}
	fmt.Fprintln(stdout, "\nRestart Claude Code to see it. Edit the toml, then run `knit-statusline preview`.")
	return 0
}

func runUninstall(args []string, stdout, stderr io.Writer) int {
	if err := flags("uninstall", args, func(*flag.FlagSet) {}); err != nil {
		return badFlag(stdout, stderr, err)
	}

	res, err := install.Uninstall(configDir())
	if err != nil {
		return fail(stderr, rootHint(err))
	}
	if res.ReplacedCommand == "" {
		fmt.Fprintf(stdout, "no status line was configured in %s\n", res.SettingsPath)
		return 0
	}
	if res.BackupPath != "" {
		fmt.Fprintf(stdout, "backed up %s\n", res.BackupPath)
	}
	// Uninstall leave another tool's statusLine sitting there. Reporting removal
	// anyway send user hunting key that never moved.
	if res.RemovedStatusLine {
		fmt.Fprintf(stdout, "removed the status line from %s\n", res.SettingsPath)
	} else {
		fmt.Fprintf(stdout, "left the status line in %s: it runs %s, not our copy\n",
			res.SettingsPath, res.ReplacedCommand)
	}
	fmt.Fprintf(stdout, "left %s in place\n", res.ConfigPath)
	return 0
}

// runPreview render sample data, so config edit get checked with no Claude Code
// restart. Without it, iterating on a layout cost a restart per character.
func runPreview(args []string, stdout, stderr io.Writer) int {
	var preset *string
	var sparse *bool
	var unknown *bool
	err := flags("preview", args, func(fs *flag.FlagSet) {
		preset = fs.String("preset", "", "preview a built-in preset instead of your config")
		sparse = fs.Bool("sparse", false, "render the fresh-session case: zeros, no rate limits")
		unknown = fs.Bool("unknown", false, "render the unknown case: resumed session, nothing reported")
	})
	if err != nil {
		return badFlag(stdout, stderr, err)
	}

	cfg, label, err := previewConfig(*preset, stderr)
	if err != nil {
		return fail(stderr, err)
	}

	// One fixture per row: sparse carry cost and context_window populated with
	// real zeros, and those win on known path whatever freshness say, so
	// session, cost and lines would never show their placeholder at all.
	doc := fixtures.Full
	kind := "complete data"
	state := transcript.StateLive
	switch {
	case *unknown:
		doc = fixtures.Unknown
		kind = "unknown data: resumed session, nothing reported yet"
	case *sparse:
		doc = fixtures.Sparse
		kind = "fresh session: nothing sent yet, no rate limits"
		state = transcript.StateFresh
	}
	in, err := schema.Parse(doc)
	if err != nil {
		return fail(stderr, err)
	}

	// Fixture JSON carry no transcript_path, so todo have nothing to open, and
	// any edit to it would preview as silence rather than signal. Complete-data
	// run alone: --sparse and --unknown exist to draw shape values leave when
	// missing.
	//
	// Todo slot pay for failure, not preview run. Preview is what catch bad edit.
	if !*sparse && !*unknown {
		if path, err := writePreviewTranscript(cacheDir()); err == nil {
			in.TranscriptPath = path
		}
	}

	fmt.Fprintf(stdout, "config: %s\nsample: %s\n\n", label, kind)
	fmt.Fprintln(stdout, statusline.Render(cfg, in, statusline.Options{
		Palette: render.NewPalette(),
		// Fixed instant keep reset times stable, so two previews compare.
		Now:          time.Unix(fixtures.PreviewEpoch, 0),
		CacheDir:     cacheDir(),
		ConfigDir:    configDir(),
		SessionState: &state,
	}))
	fmt.Fprintln(stdout)

	if *sparse || *unknown {
		return 0
	}
	fmt.Fprintln(stdout, "Run with --sparse or --unknown to check the layout when values are missing.")
	return 0
}

// writePreviewTranscript put todo fixture on disk, so preview render segment
// that read transcript rather than stdin.
//
// Fixed name, not temp one: scan cursor key off this path, and fresh name
// per run leave cache file per run behind.
//
// Lives under cache directory because it is disposable by same rule --
// delete it and next preview write it again.
func writePreviewTranscript(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, ".preview-todos-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(fixtures.TodosJSONL); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	// Two previews may overlap same as two renders do. Rename leave one complete
	// file or other, never half-written transcript mid-scan.
	final := filepath.Join(dir, "preview-todos.jsonl")
	if err := os.Rename(tmpName, final); err != nil {
		return "", err
	}
	return final, nil
}

// previewConfig resolve what to draw. Project override included, so preview show
// what this directory render, not user layer alone.
//
// Every problem go to stderr. Mistyped segment name otherwise cost its slot in
// silence here while real row carry marker -- preview exist to catch that edit.
// stdout hold rendered row alone, so it still pipe.
func previewConfig(preset string, stderr io.Writer) (*config.Config, string, error) {
	if preset != "" {
		cfg, err := config.Preset(preset)
		if err != nil {
			return nil, "", fmt.Errorf("%w; available: %s", err, strings.Join(config.PresetNames(), ", "))
		}
		return cfg, "preset " + preset, nil
	}

	res := config.Load(configDir(), workingDir())
	for _, err := range res.Errors {
		fmt.Fprintln(stderr, "warning:", err)
	}
	for _, err := range config.Validate(res.Config, res.Origin(config.UserPath(configDir())), segment.Known) {
		fmt.Fprintln(stderr, "warning:", err)
	}
	return res.Config, strings.Join(res.Sources(), " + "), nil
}

// Real diagnostics live here. Row hold marker alone, so whatever need explaining
// get explained in this command.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	if err := flags("doctor", args, func(*flag.FlagSet) {}); err != nil {
		return badFlag(stdout, stderr, err)
	}

	root := configDir()
	project := workingDir()
	env := os.Getenv("CLAUDE_CONFIG_DIR")
	fmt.Fprintln(stdout, "knit-statusline "+version)
	fmt.Fprintln(stdout)

	// No root mean every path below join to bare filename -- "settings.json",
	// "statusline.toml" -- naming file in whatever directory doctor ran from.
	// Printing that invite user to edit stranger, and existsNote stat it too,
	// so their absent config lose its "(not present)" marker while
	// Configuration block below correctly report builtin.
	settings, userConfig, cache := noHome, noHome, noHome
	if root != "" {
		settingsPath, configPath := install.SettingsPath(root), config.UserPath(root)
		settings = settingsPath + existsNote(settingsPath)
		userConfig = configPath + existsNote(configPath)
		cache = cacheLabel(cachePath(root))
	}

	fmt.Fprintln(stdout, "Paths")
	fmt.Fprintf(stdout, "  root       %s%s%s\n", rootLabel(root), rootOrigin(env), rootNote(root))
	fmt.Fprintf(stdout, "  settings   %s\n", settings)
	fmt.Fprintf(stdout, "  config     %s\n", userConfig)
	if project != "" {
		fmt.Fprintf(stdout, "  project    %s%s\n", config.ProjectPath(project), existsNote(config.ProjectPath(project)))
	}
	fmt.Fprintf(stdout, "  cache      %s\n", cache)
	fmt.Fprintln(stdout)

	res := config.Load(root, project)
	fmt.Fprintln(stdout, "Configuration")
	fmt.Fprintf(stdout, "  sources    %s\n", strings.Join(res.Sources(), " + "))
	fmt.Fprintf(stdout, "  rows       %d\n", len(res.Config.Lines))
	// Empty name resolve no segment block, so this report [defaults] layer
	// alone. Passing "defaults" would pick up a segment somebody named that.
	fmt.Fprintf(stdout, "  unknown    %q\n", res.Config.Resolve("", "").Unknown)

	problems := len(res.Errors)
	for _, err := range res.Errors {
		fmt.Fprintf(stdout, "  ERROR      %v\n", err)
	}

	// Origin search every layer, so problem name file that declared it. Load
	// already hold those bytes. Guessing one file blame whichever layer merged
	// last, at its innocent rows.
	for _, e := range config.Validate(res.Config, res.Origin(config.UserPath(root)), segment.Known) {
		fmt.Fprintf(stdout, "  ERROR      %v\n", e)
		problems++
	}
	// Counted, not merely noted: install and uninstall refuse this value, and
	// "status ok" printed beside a root nobody can install into read as
	// nothing to fix.
	if root != "" && !filepath.IsAbs(root) {
		fmt.Fprintf(stdout, "  ERROR      config root %q is relative; it resolves against whichever\n", root)
		fmt.Fprintln(stdout, "             directory reads it, so install and uninstall refuse it and")
		fmt.Fprintln(stdout, "             rendering caches nothing")
		problems++
	}
	if problems == 0 {
		fmt.Fprintln(stdout, "  status     ok")
	}
	if legacy := strayRoot(root, env); legacy != "" {
		if found := strays(legacy, root); len(found) > 0 {
			fmt.Fprintln(stdout)
			fmt.Fprintf(stdout, "Stray files in %s\n", legacy)
			fmt.Fprintf(stdout, "  Claude Code reads %s now, so nothing below is loaded.\n", root)
			for _, s := range found {
				fmt.Fprintf(stdout, "  %-10s %s\n", s.label, s.path)
				fmt.Fprintf(stdout, "  %-10s %s\n", "", s.advice)
			}
			fmt.Fprintln(stdout, "  Run `knit-statusline install` to populate the new root.")
			fmt.Fprintln(stdout, "  Then unset CLAUDE_CONFIG_DIR and run `knit-statusline uninstall` to clear")
			fmt.Fprintln(stdout, "  the old one -- it drops our binary and our statusLine key, nothing else.")
		}
	}
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "Available segments")
	for _, name := range segment.Names() {
		def, _ := segment.Lookup(name)
		// Slot marker answer question a "…" in row raise: placeholder or
		// breakage. Doctor read no payload, so live state belong to preview.
		note := ""
		if def.Stable {
			note = "  (holds slot)"
		}
		fmt.Fprintf(stdout, "  %-14s {%s}%s\n", name, strings.Join(def.Fields, "} {"), note)
	}

	// Reporting problems is job. Finding some is no command failure, so exit
	// stay zero and text carry verdict.
	return 0
}

func existsNote(path string) string {
	if _, err := os.Stat(path); err != nil {
		return "  (not present)"
	}
	return ""
}

// noHome stand for every path doctor cannot name. One wording across all four
// lines: four blanks, or four bare filenames, both read as doctor breaking
// rather than as home missing.
const noHome = "(no home directory)"

// cacheLabel keep cache line printable when root exist but cachePath resolve
// nothing. Blank value read as doctor breaking rather than as caching off.
// Reason belong to ERROR below: relative root is only one user can act on.
func cacheLabel(dir string) string {
	if dir == "" {
		return "(disabled)"
	}
	return dir
}

// rootLabel keep root line printable when no home exist. Blank value read as
// missing output rather than missing home.
func rootLabel(root string) string {
	if root == "" {
		return noHome
	}
	return root
}

// rootNote skip presence marker when no root. existsNote("") stat empty path,
// fail, and print "(not present)" beside "(no home directory)" -- two answers
// to one question, second one about file nobody named.
func rootNote(root string) string {
	if root == "" {
		return ""
	}
	return existsNote(root)
}

// rootOrigin say which directory supplied root. Moved root and default one
// otherwise print alike, and user cannot tell doctor read wrong one. Relative
// value called out on same line: path printed beside it resolve against cwd of
// whoever read it, so it name no fixed directory at all.
func rootOrigin(env string) string {
	if env == "" {
		return ""
	}
	if !filepath.IsAbs(env) {
		return "  (CLAUDE_CONFIG_DIR, relative)"
	}
	return "  (CLAUDE_CONFIG_DIR)"
}

// stray name one file left in old root, with what to do about it. Advice
// belong per file, not per block: blanket "delete these" reach settings.json,
// which hold user's hooks, permissions and enabled plugins -- config this
// program never owned and install itself only ever merge into.
type stray struct {
	label  string
	path   string
	advice string
}

// strays list what survive in old root, in migration order: user's own layout
// first, our leavings after. Absent file drop out, so block stay silent for
// anyone who moved root before ever installing.
func strays(legacy, root string) []stray {
	candidates := []stray{
		{"config", config.UserPath(legacy), configAdvice(root)},
		{"binary", install.BinaryPath(legacy), "Our old copy."},
		{"settings", install.SettingsPath(legacy), "Your hooks, permissions and plugins. Deleting it loses them."},
	}
	var found []stray
	for _, s := range candidates {
		if _, err := os.Stat(s.path); err == nil {
			found = append(found, s)
		}
	}
	return found
}

// configAdvice say copy only into root holding no statusline.toml yet.
// install itself refuse to overwrite one without --force, so unconditional
// "copy it over" advise exactly what install decline to do -- and stray file is
// by definition abandoned one, so that copy revert live layout to stale.
func configAdvice(root string) string {
	dst := config.UserPath(root)
	if _, err := os.Stat(dst); err == nil {
		return "Your old layout. " + dst + " already holds one -- merge, do not copy over it."
	}
	return "Your layout. Copy it to " + dst + "."
}

// strayRoot name old ~/.claude when CLAUDE_CONFIG_DIR moved root elsewhere.
// Empty when variable unset, home unknown, relative, or both path name one
// directory -- majority never set it and must see no extra output.
//
// Relative root silence whole block: its remedy is running install, which
// refuse that same value, and its copy destination resolve against cwd. ERROR
// above already name only fix there is.
//
// Identity by stat, not text: filepath.Clean normalise separator and dot
// segment alone. Symlinked home, macOS /tmp vs /private/tmp, and Windows case
// all name one directory two strings Clean cannot fold. SameFile settle it
// when both side stat; SamePathText (case-folded on Windows) fall back when
// legacy never existed -- common case, and missing directory leaves only text
// to compare.
func strayRoot(root, env string) string {
	if env == "" || !filepath.IsAbs(env) {
		return ""
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	legacy := filepath.Join(home, ".claude")
	if install.SameFile(legacy, root) || install.SamePathText(legacy, root) {
		return ""
	}
	return legacy
}
