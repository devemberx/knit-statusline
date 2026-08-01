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
		return fail(stderr, err)
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
		return fail(stderr, err)
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
	fmt.Fprintln(stdout, "knit-statusline "+version)
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "Paths")
	fmt.Fprintf(stdout, "  root       %s%s%s\n", rootLabel(root), rootOrigin(), existsNote(root))
	fmt.Fprintf(stdout, "  settings   %s%s\n", install.SettingsPath(root), existsNote(install.SettingsPath(root)))
	fmt.Fprintf(stdout, "  config     %s%s\n", config.UserPath(root), existsNote(config.UserPath(root)))
	if project != "" {
		fmt.Fprintf(stdout, "  project    %s%s\n", config.ProjectPath(project), existsNote(config.ProjectPath(project)))
	}
	fmt.Fprintf(stdout, "  cache      %s\n", cacheDir())
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
	if problems == 0 {
		fmt.Fprintln(stdout, "  status     ok")
	}
	if legacy := strayRoot(root); legacy != "" {
		strays := [][2]string{
			{"config", config.UserPath(legacy)},
			{"binary", install.BinaryPath(legacy)},
			{"settings", install.SettingsPath(legacy)},
		}
		var found [][2]string
		for _, s := range strays {
			if _, err := os.Stat(s[1]); err == nil {
				found = append(found, s)
			}
		}
		if len(found) > 0 {
			fmt.Fprintln(stdout)
			fmt.Fprintf(stdout, "Stray files in %s\n", legacy)
			fmt.Fprintf(stdout, "  Claude Code reads %s now, so nothing below is loaded.\n", root)
			for _, s := range found {
				fmt.Fprintf(stdout, "  %-10s %s\n", s[0], s[1])
			}
			fmt.Fprintln(stdout, "  Run `knit-statusline install` to set up the new root, then delete these.")
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

// rootLabel keep root line printable when no home exist. Blank value read as
// missing output rather than missing home.
func rootLabel(root string) string {
	if root == "" {
		return "(no home directory)"
	}
	return root
}

// rootOrigin say which directory supplied root. Moved root and default one
// otherwise print alike, and user cannot tell doctor read wrong one.
func rootOrigin() string {
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		return ""
	}
	return "  (CLAUDE_CONFIG_DIR)"
}

// strayRoot name old ~/.claude when CLAUDE_CONFIG_DIR moved root elsewhere.
// Empty when variable unset, home unknown, or both path name one directory --
// majority never set it and must see no extra output.
func strayRoot(root string) string {
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		return ""
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	legacy := filepath.Join(home, ".claude")
	if filepath.Clean(legacy) == filepath.Clean(root) {
		return ""
	}
	return legacy
}
