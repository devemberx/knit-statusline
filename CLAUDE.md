# CLAUDE.md

Status line for Claude Code. Migration in progress — `LICENSE`, `.githooks/`,
the PR template and the `gh` validation hooks here so far; the rest move over
from `bare-statusline`.

## Repo Conventions

Commit format enforced by `.githooks/commit-msg`, so a violation fail at commit,
not at review. Rules themselves live in `.github/CONTRIBUTING.md` — not ported
yet, the hook header already cite it. Not repeated here.

Hook opt-in per clone: `git config core.hooksPath .githooks`. Unset, it sit dead
and nothing else check.

Branch naming unenforced for now: `bare-statusline` carry a `pre-push` hook for
it, deliberately not ported yet.

PR title obey the same rule as the commit subject — squash turn that title into
that subject. `.claude/hooks/validate_pr.py` block a malformed `gh pr create`,
`validate_pr_merge.py` force `gh pr merge --squash`. Both fail closed: a body
that cannot be read, or a command `shlex` cannot split, is an error not a pass.

PR and commit artifacts in English. Closed ``` fences and `inline spans` exempt,
so pasted terminal output keep its glyphs.

Agent-driven merge carry `Co-Authored-By: Claude <noreply@anthropic.com>` in the
squash body, else git history attribute the work to the human alone.
`validate_pr_merge.py` cite this line.

`.github/PULL_REQUEST_TEMPLATE.md` drop the status line checklist section that
`bare-statusline` carry — it name Go symbols and a `preview` subcommand not
ported yet. `label-pr.yml` unported too, so a PR title pick no release-note
label here.

## Comments

- Caveman style: drop articles and filler. `# Deletion push = all-zero sha, no
  branch name to check.` Why, not what. NEVER restate self-evident code.
- One distinct fact per line. Technical terms, ids and numbers exact. No
  invented abbreviations.
- No `WARNING:`/`NOTE:` prefixes, no banner dividers, no dev narrative.
- `TODO` = `# TODO(#issue): concrete action`. No dead code. Comments sync when
  the code change.
