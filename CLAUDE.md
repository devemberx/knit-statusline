# CLAUDE.md

Status line for Claude Code. Migration in progress — `LICENSE`, `.githooks/`,
`.github/PULL_REQUEST_TEMPLATE.md` and `.claude/hooks/` here so far; the rest
move over from `bare-statusline`.

Contribution rules — branch names, commit format, PR and merge — belong in
`.github/CONTRIBUTING.md`, not ported yet. Hook headers cite it. Not repeated
here.

## Comments

- Caveman style: drop articles and filler. `# Deletion push = all-zero sha, no
  branch name to check.` Why, not what. NEVER restate self-evident code.
- One distinct fact per line. Technical terms, ids and numbers exact. No
  invented abbreviations.
- No `WARNING:`/`NOTE:` prefixes, no banner dividers, no dev narrative.
- `TODO` = `# TODO(#issue): concrete action`. No dead code. Comments sync when
  the code change.
