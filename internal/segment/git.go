package segment

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/render"
)

func init() {
	register("dir", Def{
		Fields:          []string{"name", "path", "project", "worktree", "git", "branch", "dirty"},
		DefaultTemplate: "{name}{git}",
		Build:           buildDir,
	})
}

func buildDir(c Context) Result {
	dir := c.In.Dir()
	if dir == "" {
		return empty
	}

	f := render.Fields{
		"name":    render.Colored(filepath.Base(dir), render.Cyan),
		"path":    render.Colored(dir, render.Cyan),
		"project": render.Colored(filepath.Base(c.In.Workspace.ProjectDir), render.Cyan),
	}
	if c.In.Workspace.GitWorktree != nil {
		f["worktree"] = render.Colored(*c.In.Workspace.GitWorktree, render.Magenta)
	}

	// Git cost subprocess. Pay only when template ask; minimal preset render
	// "{name}" and shell out never.
	if wantsGit(c.Cfg.Template) {
		if branch, dirty, ok := gitStatus(dir, c.budget()); ok {
			f["branch"] = render.Colored(branch, render.Green)
			f["dirty"] = render.Colored(dirty, render.Red)

			// One preformatted field. No repository = empty, where separate
			// branch and dirty fields leave "()" behind.
			git := c.Palette.Wrap(" (", render.Green) +
				c.Palette.Wrap(branch, render.Green) +
				c.Palette.Wrap(dirty, render.Red) +
				c.Palette.Wrap(")", render.Green)
			f["git"] = render.Plain(git)
		}
	}
	if _, ok := f["git"]; !ok {
		f["git"] = render.Plain("")
	}
	return Result{Base: render.Cyan, Fields: f}
}

func wantsGit(tmpl string) bool {
	return strings.Contains(tmpl, "{git") ||
		strings.Contains(tmpl, "{branch") ||
		strings.Contains(tmpl, "{dirty")
}

// gitStatus return current branch plus dirty marker.
//
// Every call share one budget. status --porcelain walk whole working tree, so on
// large repository it run seconds; unbounded, render hang and Claude Code print
// nothing, which read same as crash. Expired budget drop git fields alone and
// leave rest of row standing.
//
// --no-optional-locks stop status check writing index. Matters: this run several
// times a second beside user's own git commands.
func gitStatus(dir string, budget time.Duration) (branch, dirty string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	branch, err := gitRun(ctx, dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		// Detached HEAD still deserve identity.
		branch, err = gitRun(ctx, dir, "rev-parse", "--short", "HEAD")
		if err != nil {
			return "", "", false
		}
	}
	if branch == "" {
		return "", "", false
	}

	if st, err := gitRun(ctx, dir, "--no-optional-locks", "status", "--porcelain"); err == nil && st != "" {
		dirty = "*"
	}
	return branch, dirty, true
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Git spawn helpers -- credential, hooks, pager -- that inherit stdout and
	// outlive it. Without WaitDelay, Output() read to EOF past cancellation.
	cmd.WaitDelay = pipeDrain
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
