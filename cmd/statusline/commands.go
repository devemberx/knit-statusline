package main

import (
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
func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}

// flags parse args with usage suppressed, so a bad flag report through our own
// exit code rather than printing twice.
func flags(name string, args []string, bind func(*flag.FlagSet)) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bind(fs)
	return fs, fs.Parse(args)
}

func runInstall(args []string, stdout io.Writer) int {
	var preset *string
	var force *bool
	_, err := flags("install", args, func(fs *flag.FlagSet) {
		preset = fs.String("preset", config.DefaultPreset, "starting layout")
		force = fs.Bool("force", false, "replace an existing statusline.toml")
	})
	if err != nil {
		return 2
	}

	binary, err := os.Executable()
	if err != nil {
		return fail(stdout, fmt.Errorf("locating this binary: %w", err))
	}

	res, err := install.Install(install.Options{
		Home: homeDir(), Binary: binary, Preset: *preset, Force: *force,
	})
	if err != nil {
		return fail(stdout, err)
	}

	if res.ReplacedCommand != "" && res.ReplacedCommand != res.InstalledBinary {
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

func runUninstall(args []string, stdout io.Writer) int {
	if _, err := flags("uninstall", args, func(*flag.FlagSet) {}); err != nil {
		return 2
	}

	res, err := install.Uninstall(homeDir())
	if err != nil {
		return fail(stdout, err)
	}
	if res.ReplacedCommand == "" {
		fmt.Fprintf(stdout, "no status line was configured in %s\n", res.SettingsPath)
		return 0
	}
	if res.BackupPath != "" {
		fmt.Fprintf(stdout, "backed up %s\n", res.BackupPath)
	}
	fmt.Fprintf(stdout, "removed the status line from %s\n", res.SettingsPath)
	fmt.Fprintf(stdout, "left %s in place\n", res.ConfigPath)
	return 0
}

// runPreview render sample data, so config edit get checked with no Claude Code
// restart. Without it, iterating on a layout cost a restart per character.
func runPreview(args []string, stdout, stderr io.Writer) int {
	var preset *string
	var sparse *bool
	_, err := flags("preview", args, func(fs *flag.FlagSet) {
		preset = fs.String("preset", "", "preview a built-in preset instead of your config")
		sparse = fs.Bool("sparse", false, "render the degraded case: no rate limits, no usage yet")
	})
	if err != nil {
		return 2
	}

	cfg, label, err := previewConfig(*preset, stderr)
	if err != nil {
		return fail(stdout, err)
	}

	doc := fixtures.Full
	kind := "complete data"
	if *sparse {
		doc = fixtures.Sparse
		kind = "degraded data: no rate limits, no usage yet"
	}
	in, err := schema.Parse(doc)
	if err != nil {
		return fail(stdout, err)
	}

	fmt.Fprintf(stdout, "config: %s\nsample: %s\n\n", label, kind)
	fmt.Fprintln(stdout, statusline.Render(cfg, in, statusline.Options{
		Palette: render.NewPalette(),
		// Fixed instant keep reset times stable, so two previews compare.
		Now:      time.Unix(fixtures.PreviewEpoch, 0),
		CacheDir: cacheDir(),
	}))
	fmt.Fprintln(stdout)

	if *sparse {
		return 0
	}
	fmt.Fprintln(stdout, "Run with --sparse to check the layout when values are missing.")
	return 0
}

// previewConfig resolve what to draw. Project override included, so preview show
// what this directory render rather than user layer alone.
func previewConfig(preset string, stderr io.Writer) (*config.Config, string, error) {
	if preset != "" {
		cfg, err := config.Preset(preset)
		if err != nil {
			return nil, "", fmt.Errorf("%w; available: %v", err, config.PresetNames())
		}
		return cfg, "preset " + preset, nil
	}
	res := config.Load(homeDir(), workingDir())
	if len(res.Errors) > 0 {
		fmt.Fprintln(stderr, "warning:", res.Errors[0])
	}
	return res.Config, strings.Join(res.Sources, " + "), nil
}

// Real diagnostics live here. Row hold marker alone, so whatever need explaining
// get explained in this command.
func runDoctor(args []string, stdout io.Writer) int {
	if _, err := flags("doctor", args, func(*flag.FlagSet) {}); err != nil {
		return 2
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
	fmt.Fprintf(stdout, "  sources    %s\n", strings.Join(res.Sources, " + "))
	fmt.Fprintf(stdout, "  rows       %d\n", len(res.Config.Lines))

	problems := len(res.Errors)
	for _, err := range res.Errors {
		fmt.Fprintf(stdout, "  ERROR      %v\n", err)
	}

	path, source := lastFileSource(res)
	if path == "" {
		path = config.UserPath(home)
	}
	for _, e := range config.Validate(res.Config, source, path, segment.Known) {
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

// lastFileSource return bytes of last real file that fed config, for lineOf to
// locate against. Merge lost which layer declared what, and project override is
// layer a user edit most, so it win when both exist. Builtin preset carry no
// path, so it never qualify.
func lastFileSource(res *config.LoadResult) (string, []byte) {
	for i := len(res.Sources) - 1; i >= 0; i-- {
		path := res.Sources[i]
		if strings.HasPrefix(path, "builtin:") {
			continue
		}
		if b, err := os.ReadFile(path); err == nil {
			return path, b
		}
	}
	return "", nil
}

func existsNote(path string) string {
	if _, err := os.Stat(path); err != nil {
		return "  (not present)"
	}
	return ""
}
