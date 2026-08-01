// Command knit-statusline render a configurable Claude Code status line.
//
// No arguments: read JSON document on stdin, print rows. That is form Claude
// Code invoke. Subcommands exist for humans: install, uninstall, preview,
// doctor.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/segment"
	"github.com/devemberx/knit-statusline/internal/statusline"
)

// Overwritten at build time by release pipeline.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		renderFromStdin(os.Stdin, os.Stdout)
		return
	}
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch run one subcommand. Writers passed in so tests read output without
// spawning a process.
func dispatch(args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "install":
		return runInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runUninstall(args[1:], stdout, stderr)
	case "preview":
		return runPreview(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "knit-statusline "+version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `knit-statusline %s -- a configurable status line for Claude Code

Usage:
  knit-statusline                 render from the JSON document on stdin
  knit-statusline install         configure Claude Code to use this status line
  knit-statusline uninstall       remove that configuration
  knit-statusline preview         render sample data, to check an edit
  knit-statusline doctor          report config problems and available segments

Install flags:
  --preset NAME    starting layout: %s (default %q)
  --force          replace an existing statusline.toml

Preview flags:
  --preset NAME    preview a built-in preset instead of your config
  --sparse         render the degraded case: no rate limits, no usage yet

Configuration lives in ~/.claude/statusline.toml, with an optional
per-project override at <project>/.claude/statusline.toml.
`, version, strings.Join(config.PresetNames(), ", "), config.DefaultPreset)
}

// Hot path: Claude Code run it on every status update.
//
// Never exit non-zero, never panic. Claude Code print whatever reach stdout, so
// crash show as empty row explaining nothing -- worse than a wrong number.
func renderFromStdin(stdin io.Reader, stdout io.Writer) {
	palette := render.NewPalette()

	var in *schema.Input
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(stdout, statusline.Fallback(in, palette))
		}
	}()

	raw, err := io.ReadAll(stdin)
	if err != nil || len(raw) == 0 {
		fmt.Fprintln(stdout, statusline.Fallback(nil, palette))
		return
	}

	in, err = schema.Parse(raw)
	if err != nil {
		fmt.Fprintln(stdout, statusline.Fallback(nil, palette))
		return
	}

	home := homeDir()
	res := config.Load(home, projectDir(in))

	out := statusline.Render(res.Config, in, statusline.Options{
		Palette:   palette,
		Now:       time.Now(),
		CacheDir:  cacheDir(),
		ConfigDir: configDir(),
		Warning:   marker(res, home),
	})

	// Every segment empty: valid document with nothing populated yet, or layout
	// of segments that do not apply here. Printing nothing read as crash, so
	// fall back to whatever identity remain.
	if out == "" {
		out = statusline.Fallback(in, palette)
	}
	fmt.Fprintln(stdout, out)
}

// marker name file needing attention, or empty.
//
// Semantic error -- segment name this build lack, template field that does not
// exist -- cost only its own segment, so unmarked that segment vanish and user
// get no hint which file to open. Parse failure already dropped a layer and Load
// recorded it, so those come first.
//
// Load hand over bytes it read, so locating problem cost no second read on path
// that run every redraw. Short() trim to "statusline.toml:7"; doctor hold full
// prose.
func marker(res *config.LoadResult, home string) string {
	if len(res.Errors) > 0 {
		return short(res.Errors[0])
	}
	origin := res.Origin(config.UserPath(home))
	if errs := config.Validate(res.Config, origin, segment.Known); len(errs) > 0 {
		return short(errs[0])
	}
	return ""
}

func short(err error) string {
	var ce *config.Error
	if errors.As(err, &ce) {
		return ce.Short()
	}
	return "config"
}

// Where per-project override is looked for. workspace.project_dir is launch
// directory, stable choice: cwd move mid-session and make override come and go.
func projectDir(in *schema.Input) string {
	if in.Workspace.ProjectDir != "" {
		return in.Workspace.ProjectDir
	}
	return in.Dir()
}

// workingDir stand in for project_dir when no stdin document exist. preview and
// doctor run from a shell, so cwd is what user mean by "this project".
func workingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

func cacheDir() string {
	return filepath.Join(homeDir(), ".claude", "statusline-cache")
}

// configDir locate Claude Code config root.
//
// CLAUDE_CONFIG_DIR is what caveman hook read when it write flag, so segment
// must read same variable or look in wrong directory for people who move it.
func configDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".claude")
}
