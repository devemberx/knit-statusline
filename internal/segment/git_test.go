package segment

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devemberx/knit-statusline/internal/fixtures"
)

// repo build throwaway repository. Identity passed per command: CI runner carry
// no global git config, and commit fail outright without one.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.email=test@example.com",
			"-c", "user.name=test",
			"-c", "commit.gpgsign=false",
		}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-m", "first")
	return dir
}

func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// Git cost subprocess, so template decide whether to pay. Minimal preset render
// "{name}" and must shell out never.
func TestWantsGit(t *testing.T) {
	for _, tc := range []struct {
		tmpl string
		want bool
	}{
		{"{name}", false},
		{"{name}{git}", true},
		{"{name} ({branch})", true},
		{"{branch}{dirty}", true},
		{"{path}", false},
		{"{project}", false}, // no git token inside, whatever its letters spell
	} {
		if got := wantsGit(tc.tmpl); got != tc.want {
			t.Errorf("wantsGit(%q) = %v, want %v", tc.tmpl, got, tc.want)
		}
	}
}

func TestGitStatusReportsBranchAndDirtiness(t *testing.T) {
	dir := repo(t)

	branch, dirty, ok := gitStatus(dir, time.Second)
	if !ok {
		t.Fatal("gitStatus failed on a real repository")
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	if dirty != "" {
		t.Errorf("clean tree reported dirty marker %q", dirty)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, dirty, _ := gitStatus(dir, time.Second); dirty != "*" {
		t.Errorf("modified tree dirty = %q, want *", dirty)
	}
}

// Detached HEAD still deserve identity, so short sha stand in for branch name.
func TestGitStatusNamesDetachedHead(t *testing.T) {
	dir := repo(t)
	sha := gitAt(t, dir, "rev-parse", "--short", "HEAD")
	if out, err := exec.Command("git", "-C", dir, "checkout", "--detach", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("detach: %v\n%s", err, out)
	}

	branch, _, ok := gitStatus(dir, time.Second)
	if !ok {
		t.Fatal("detached HEAD should still resolve")
	}
	if branch != sha {
		t.Errorf("branch = %q, want short sha %q", branch, sha)
	}
}

func TestGitStatusFailsOutsideRepository(t *testing.T) {
	if _, _, ok := gitStatus(t.TempDir(), time.Second); ok {
		t.Error("plain directory reported as a repository")
	}
}

// status --porcelain walk whole working tree, so on large repository it run
// seconds. Unbounded, render hang and Claude Code print nothing -- which read
// same as crash. Expired budget must drop git fields and return, not block.
func TestGitStatusHonoursBudget(t *testing.T) {
	dir := repo(t)

	start := time.Now()
	_, _, ok := gitStatus(dir, time.Nanosecond)
	took := time.Since(start)

	if ok {
		t.Error("expired budget still reported success")
	}
	if took > 2*time.Second {
		t.Errorf("gitStatus took %v despite a 1ns budget", took)
	}
}

// No repository = empty {git}, where separate {branch} and {dirty} leave "()"
// standing for a repository that is not there.
func TestBuildDirLeavesNoEmptyParens(t *testing.T) {
	c := ctx(t, fixtures.Full, "dir")
	c.In.Workspace.CurrentDir = t.TempDir()

	got := draw(c)
	if strings.Contains(got, "(") || strings.Contains(got, ")") {
		t.Errorf("rendered %q outside a repository, want no parens", got)
	}
	if got != filepath.Base(c.In.Workspace.CurrentDir) {
		t.Errorf("rendered %q, want the bare directory name", got)
	}
}

func TestBuildDirRendersBranchInsideRepository(t *testing.T) {
	dir := repo(t)
	c := ctx(t, fixtures.Full, "dir")
	c.In.Workspace.CurrentDir = dir

	if got := draw(c); got != filepath.Base(dir)+" (main)" {
		t.Errorf("rendered %q, want %q", got, filepath.Base(dir)+" (main)")
	}
}

// Template naming no git field must not shell out at all. Absent branch field
// stand in for that: it populate only after gitStatus run.
func TestBuildDirSkipsGitWhenTemplateDoesNotAsk(t *testing.T) {
	c := ctx(t, fixtures.Full, "dir")
	c.In.Workspace.CurrentDir = repo(t)
	c.Cfg.Template = "{name}"

	res := Build(c)
	if _, ok := res.Fields["branch"]; ok {
		t.Error("branch resolved for a template that never ask for it")
	}
}

func TestBuildDirCarriesWorktreeName(t *testing.T) {
	res := Build(ctx(t, fixtures.Full, "dir"))
	if got := res.Fields["worktree"].Text; got != "feat-auth" {
		t.Errorf("worktree = %q, want feat-auth", got)
	}
}

func TestBuildDirIsEmptyWithoutADirectory(t *testing.T) {
	c := ctx(t, fixtures.Empty, "dir")
	if res := Build(c); !res.Empty {
		t.Errorf("got %+v, want empty", res)
	}
}
