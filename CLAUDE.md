# CLAUDE.md

Status line for Claude Code. Migration in progress — `LICENSE` and `.githooks/`
here so far; the rest move over from `bare-statusline`.

## Repo Conventions

Commit format enforced by `.githooks/commit-msg`, so a violation fail at commit,
not at review. Rules themselves live in `.github/CONTRIBUTING.md` — not ported
yet, the hook header already cite it. Not repeated here.

Hook opt-in per clone: `git config core.hooksPath .githooks`. Unset, it sit dead
and nothing else check.

Branch naming unenforced for now: `bare-statusline` carry a `pre-push` hook for
it, deliberately not ported yet.

## Comments

- Caveman style: drop articles and filler. `# Deletion push = all-zero sha, no
  branch name to check.` Why, not what. NEVER restate self-evident code.
- One distinct fact per line. Technical terms, ids and numbers exact. No
  invented abbreviations.
- No `WARNING:`/`NOTE:` prefixes, no banner dividers, no dev narrative.
- `TODO` = `# TODO(#issue): concrete action`. No dead code. Comments sync when
  the code change.
