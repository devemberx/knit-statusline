package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/fixtures"
	"github.com/devemberx/knit-statusline/internal/install"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/segment"
	"github.com/devemberx/knit-statusline/internal/statusline"
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
		Home: homeDir(), Binary: binary, Preset: *preset, Force: *force,
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

	res, err := install.Uninstall(homeDir())
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
	err := flags("preview", args, func(fs *flag.FlagSet) {
		preset = fs.String("preset", "", "preview a built-in preset instead of your config")
		sparse = fs.Bool("sparse", false, "render the degraded case: no rate limits, no usage yet")
	})
	if err != nil {
		return badFlag(stdout, stderr, err)
	}

	cfg, label, err := previewConfig(*preset, stderr)
	if err != nil {
		return fail(stderr, err)
	}

	doc := fixtures.Full
	kind := "complete data"
	if *sparse {
		doc = fixtures.Sparse
		kind = "degraded data: no rate limits, no usage yet"
	}
	in, err := schema.Parse(doc)
	if err != nil {
		return fail(stderr, err)
	}

	fmt.Fprintf(stdout, "config: %s\nsample: %s\n\n", label, kind)
	fmt.Fprintln(stdout, statusline.Render(cfg, in, statusline.Options{
		Palette: render.NewPalette(),
		// Fixed instant keep reset times stable, so two previews compare.
		Now:       time.Unix(fixtures.PreviewEpoch, 0),
		CacheDir:  cacheDir(),
		ConfigDir: configDir(),
	}))
	fmt.Fprintln(stdout)

	if *sparse {
		return 0
	}
	fmt.Fprintln(stdout, "Run with --sparse to check the layout when values are missing.")
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

	res := config.Load(homeDir(), workingDir())
	for _, err := range res.Errors {
		fmt.Fprintln(stderr, "warning:", err)
	}
	for _, err := range config.Validate(res.Config, res.Origin(config.UserPath(homeDir())), segment.Known) {
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

	home := homeDir()
	project := workingDir()
	fmt.Fprintln(stdout, "knit-statusline "+version)
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "Paths")
	fmt.Fprintf(stdout, "  settings   %s%s\n", install.SettingsPath(home), existsNote(install.SettingsPath(home)))
	fmt.Fprintf(stdout, "  config     %s%s\n", config.UserPath(home), existsNote(config.UserPath(home)))
	if project != "" {
		fmt.Fprintf(stdout, "  project    %s%s\n", config.ProjectPath(project), existsNote(config.ProjectPath(project)))
	}
	fmt.Fprintf(stdout, "  cache      %s\n", cacheDir())
	fmt.Fprintln(stdout)

	res := config.Load(home, project)
	fmt.Fprintln(stdout, "Configuration")
	fmt.Fprintf(stdout, "  sources    %s\n", strings.Join(res.Sources(), " + "))
	fmt.Fprintf(stdout, "  rows       %d\n", len(res.Config.Lines))

	problems := len(res.Errors)
	for _, err := range res.Errors {
		fmt.Fprintf(stdout, "  ERROR      %v\n", err)
	}

	// Origin search every layer, so problem name file that declared it. Load
	// already hold those bytes. Guessing one file blame whichever layer merged
	// last, at its innocent rows.
	for _, e := range config.Validate(res.Config, res.Origin(config.UserPath(home)), segment.Known) {
		fmt.Fprintf(stdout, "  ERROR      %v\n", e)
		problems++
	}
	if problems == 0 {
		fmt.Fprintln(stdout, "  status     ok")
	}
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "Available segments")
	for _, name := range segment.Names() {
		def, _ := segment.Lookup(name)
		fmt.Fprintf(stdout, "  %-14s {%s}\n", name, strings.Join(def.Fields, "} {"))
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
