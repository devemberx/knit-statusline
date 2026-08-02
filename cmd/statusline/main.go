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
  --sparse         render the fresh-session case: zeros, no rate limits
  --unknown        render the unknown case: resumed session, nothing reported

Configuration lives in statusline.toml inside the Claude Code config root --
$CLAUDE_CONFIG_DIR when set, otherwise ~/.claude -- with an optional
per-project override at <project>/.claude/statusline.toml. The doctor
subcommand prints the root it resolved.
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

	root := configDir()
	res := config.Load(root, projectDir(in))

	out := statusline.Render(res.Config, in, statusline.Options{
		Palette:   palette,
		Now:       time.Now(),
		CacheDir:  cacheDir(),
		ConfigDir: configDir(),
		Warning:   marker(res, root),
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
func marker(res *config.LoadResult, root string) string {
	if len(res.Errors) > 0 {
		return short(res.Errors[0])
	}
	origin := res.Origin(config.UserPath(root))
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

// cacheDir hold transcript cursor and command output. Sit under config root,
// not $HOME: split root read config from one directory and cache to another.
// Empty root give empty path, which segment and cursor already treat as
// "no cache" rather than a relative directory under cwd.
//
// Relative root give empty path too. Render honour such root because Claude
// Code do, but cache write under it land in directory Claude Code started in --
// one statusline-cache/ per project user open, none of them shared. Cache
// disposable, so losing it cost a rescan; scattering directories through user's
// projects cost more.
func cacheDir() string {
	root := configDir()
	if root == "" || !filepath.IsAbs(root) {
		return ""
	}
	return filepath.Join(root, "statusline-cache")
}

// configDir locate Claude Code config root. settings.json, statusline.toml, our
// binary and our cache all live under it.
//
// CLAUDE_CONFIG_DIR relocate whole root, so reading $HOME directly write files
// Claude Code never open.
//
// Empty home give empty root, never ".claude": filepath.Join on empty string
// yield relative path, and install and Load guard on empty exactly to stop
// dropping a config directory into whatever directory command ran from.
func configDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h := homeDir()
	if h == "" {
		return ""
	}
	return filepath.Join(h, ".claude")
}
