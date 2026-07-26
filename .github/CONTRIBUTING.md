# Contributing to knit-statusline

Thanks for taking the time. This is a status line for Claude Code: it reads one JSON
object on stdin and prints one row.

This guide walks the contributor's path: **pick something to work on → set up → make
the change → open a pull request.** Comment style and the rules for the code itself
live in [CLAUDE.md](../CLAUDE.md).

> **Migration complete.** The program moved over from `bare-statusline` piece by
> piece: every Go package, `cmd/statusline`, the release pipeline, `SECURITY.md` and
> the `pre-push` hook are all here. Nothing is waiting on a port any more.

---

## Ways to contribute

- **Report a bug** — open an [issue](https://github.com/devemberx/knit-statusline/issues/new).
  Include your `statusline.toml`, your platform, and what the row printed.
- **Propose a segment** — new data worth a slot on the row. Open an issue first and
  describe the data before writing code.
- **Improve the docs** — this guide and `CLAUDE.md` are the only docs so far. Gaps in
  either are bugs. Small docs PRs need no prior discussion.
- **Fix the port** — the code came over from `bare-statusline` package by package.
  Anything that behaves differently from the original is a bug worth an issue.

Report a vulnerability privately through GitHub's security advisory form, **not** a
public issue. [SECURITY.md](SECURITY.md) says what counts as one and what does not.

---

## Development setup

### Prerequisites

- `git` and **Go 1.26** — the version in `go.mod`, which is where CI reads it from
  too. `go test ./...` is the whole suite, and `go build ./cmd/statusline` gives you
  a binary to try `preview` and `doctor` against.

### Get the code

1. **Fork** this repository (the **Fork** button on GitHub).
2. **Clone** your fork.

> Collaborators with write access may skip the fork and branch directly in this repo.

Enable the local hooks — they check your commit message and your branch name before
you get as far as a review comment:

```sh
git config core.hooksPath .githooks
```

This is opt-in **per clone**. Leave it unset and nothing checks your commits or
branch names locally; the same rules still apply at review, so turning it on saves
you a rewrite.

---

## Development workflow

1. **Branch off the latest `main`.** Use the `<type>/<short-kebab-summary>` form:

   | Prefix      | For                                        | Example                      |
   | ----------- | ------------------------------------------ | ---------------------------- |
   | `feat/`     | new segment or user-visible behavior       | `feat/token-tracking`        |
   | `fix/`      | bug fix on existing behavior               | `fix/render-empty-segments`  |
   | `perf/`     | same behavior, less work                   | `perf/cache-transcript-scan` |
   | `refactor/` | internal restructuring, no behavior change | `refactor/segment-registry`  |
   | `test/`     | tests and fixtures only                    | `test/sparse-fixture`        |
   | `docs/`     | documentation only                         | `docs/contributing`          |
   | `chore/`    | deps, tooling, housekeeping                | `chore/pr-harness`           |
   | `ci/`       | GitHub Actions / release pipeline          | `ci/hostile-input-check`     |

   One topic per branch; split unrelated work apart. `.githooks/pre-push` checks the
   name and refuses a direct push to `main`, if you enabled the hooks below. Its type
   list is the table above plus `perf`, and the two have to stay in step.

2. **Make your change.** Follow [CLAUDE.md](../CLAUDE.md) for comment style: caveman
   phrasing, why rather than what, one fact per line.

3. **Push to your fork** and open a pull request.

---

## Commit messages

We **squash-merge** PRs, so the final commit is built from your **PR title** plus the
**Changes** bullets — that is what has to follow the format. Your branch's "wip"
commits vanish on squash.

```
type(scope)!: summary       ← imperative, lowercase, no period, ≤50 chars

- why the change is needed  ← two bullets, ≤120 chars each
- what changed
```

- **type**: `feat` `fix` `docs` `refactor` `perf` `test` `ci` `chore`
- **scope**: optional, lowercase kebab (`render`, `label-pr`). Deliberately not an
  allowlist — a fixed list needs editing on every rename and drifts out of step with
  the workflow that reads PR titles. Omit the scope for a change that spans the repo.
- **`!`**: optional, marks a breaking change (`feat!:`, `feat(render)!:`).

`.githooks/commit-msg` checks all of this, if you enabled it above.

---

## Pull requests

- **Keep it small.** Small, focused PRs get reviewed fast; large ones sit in the queue.
  One concern per PR.
- **Open it against `devemberx/knit-statusline:main`**, from your branch.
- **Use the [pull request template](PULL_REQUEST_TEMPLATE.md)** (auto-loaded). Fill
  every section — Summary, Type of Change, Changes, Testing.
- **Flip `[ ]` to `[x]`** in Type of Change; do **not** delete the options you did not
  check.
- **Make the title a commit subject.** It becomes the squash subject verbatim, so it
  follows the [commit format](#commit-messages) — same 50-char limit, no period.
- **Make `## Changes` exactly two bullets**, ≤120 chars each: why, then what. They
  become the squash commit body verbatim.
- **Write PR text in English.** Titles, bodies and comments end up in `git log`.
  Fenced code blocks and `inline spans` are exempt, so pasted terminal output keeps
  its glyphs.
- **Link issues** with `Closes #<n>` or `Refs #<n>` in the Summary.
- **No real paths or session content.** Transcripts hold conversation text; never
  commit one as a fixture, and scrub paths out of pasted output.

CI runs on every PR: the test suite on Linux, macOS and Windows runners, the same
tests again under `TZ=Asia/Seoul`, `golangci-lint`, a `go mod tidy` drift check, a
five-target cross-compile matrix, `govulncheck`, and a `zizmor` audit of the
workflows. The `gate` job aggregates them into one required check. Say in **Testing**
what you checked beyond that.

Release-note labels are stamped from the PR title, and re-stamped every time the title
is edited — so do not apply `feature`, `bug`, `breaking` or the rest by hand; the next
edit drops them. `skip-changelog` is never touched, apply that one manually.

### Review and merge

- At least one approving review is required; resolve review threads before merge.
- Merge strategy is **squash and merge**. Do not pass `--subject` to `gh pr merge` —
  the PR title is the subject already.
- Give the merge an explicit body: the same two bullets, and
  `Co-Authored-By: Claude <noreply@anthropic.com>` when an agent did the work, so git
  history attributes it.
- Force-pushing your own branch is fine (e.g. to clean up history before merge).

---

## Working with Claude Code

Two `PreToolUse` hooks in `.claude/hooks/` check the rules above during a Claude Code
session: `validate_pr.py` blocks a `gh pr create` whose title, template checkboxes or
`## Changes` bullets are off, and `validate_pr_merge.py` blocks any merge that is not
`--squash` with an explicit body. Both fail closed — a body they cannot read is an
error, not a pass.

They are conveniences, not the source of truth. Contributing with a plain `gh` CLI or
the GitHub web form hits no hook, and the rules in this guide still apply.

---

## AI-assisted contributions

Using an AI assistant to help write code or docs is welcome. The same bar applies to
every line:

- **You are the author.** Understand, and be able to explain, everything you submit.
  Review and test the output before opening a PR.
- **Stay focused.** Do not let a tool expand the diff with unrelated refactors.
- **Bug fixes start with a failing test** that passes after your change.

---

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](../LICENSE).
