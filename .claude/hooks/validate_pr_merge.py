#!/usr/bin/env python3
"""PreToolUse hook: enforce the squash-merge convention on `gh pr merge`.

Fires on: gh pr merge, gh api .../pulls/<n>/merge

Rules from .github/CONTRIBUTING.md "Review and merge" plus .githooks/commit-msg:
  1. Squash only; --merge / --rebase rejected.
  2. No --subject / -t; PR title is the squash subject verbatim.
  3. Explicit body: exactly 2 bullets, each <= 120 chars.
  4. Body carry Co-Authored-By: Claude ... <noreply@anthropic.com>, so an
     agent-driven merge stay attributable in git history.

API route refused outright, not re-validated: it take a merge_method field
instead of flags, and nothing need to merge that way.

Exit 0 allow, exit 2 block.
"""

from __future__ import annotations

import re
import sys

from _common import (
    GH_API_RE,
    PARSE_ERROR,
    argv_of,
    body_heredoc,
    bullet_errors,
    bullets_of,
    flag_value,
    read_file,
    run,
    strip_heredocs,
)

PR_MERGE_RE = re.compile(r"\bgh\s+pr\s+merge\b")
API_MERGE_RE = re.compile(r"/pulls/\d+/merge\b")
# gh pr merge: -m/--merge, -r/--rebase, -s/--squash, -t/--subject, -b/--body.
STRATEGY_FLAGS = {"--merge", "-m", "--rebase", "-r"}
SQUASH_FLAGS = {"--squash", "-s"}
SUBJECT_FLAGS = {"--subject", "-t"}
CO_AUTHOR_RE = re.compile(
    r"^co-authored-by:\s*claude\b.*<noreply@anthropic\.com>\s*$",
    re.IGNORECASE | re.MULTILINE,
)


def check(cmd: str) -> list[str]:
    stripped = strip_heredocs(cmd)
    if GH_API_RE.search(stripped) and API_MERGE_RE.search(stripped):
        return [
            "Do not merge through `gh api`; use `gh pr merge --squash` so the "
            "squash-merge rules apply."
        ]
    if not PR_MERGE_RE.search(stripped):
        return []

    # Fail closed: unparseable command would else carry --merge past every check.
    argv = argv_of(cmd)
    if argv is None:
        return [PARSE_ERROR]

    errors: list[str] = []
    if flag_value(argv, SUBJECT_FLAGS, attached="-t")[0]:
        errors.append(
            "Drop --subject / -t; the PR title becomes the squash subject verbatim."
        )
    forbidden = sorted({a for a in argv if a in STRATEGY_FLAGS})
    if forbidden:
        errors.append(f"Remove {', '.join(forbidden)}; squash merge only.")
    if not any(a in SQUASH_FLAGS or a.startswith("--squash=") for a in argv):
        errors.append("Re-run with --squash.")

    found_body, body = extract_body(cmd)
    if not found_body:
        errors.append("Pass an explicit body via --body / -b or --body-file / -F.")
    elif body is None:
        errors.append("--body-file points at a file that could not be read.")
    else:
        errors.extend(body_errors(body))
    return errors


def extract_body(cmd: str) -> tuple[bool, str | None]:
    heredoc = body_heredoc(cmd)
    if heredoc is not None:
        return True, heredoc

    argv = argv_of(cmd)
    if argv is None:
        return False, None
    found, value = flag_value(argv, {"--body", "-b"}, attached="-b")
    if found:
        return True, value
    found, value = flag_value(argv, {"--body-file", "-F"}, attached="-F")
    if found:
        return True, read_file(value) if value is not None else None
    return False, None


def body_errors(body: str) -> list[str]:
    prose = "\n".join(
        ln for ln in body.splitlines() if not ln.lstrip().startswith("#")
    )
    errors = bullet_errors(bullets_of(prose), "The merge body")
    if not CO_AUTHOR_RE.search(body):
        errors.append(
            "Body must include: Co-Authored-By: Claude ... <noreply@anthropic.com>"
        )
    return errors


if __name__ == "__main__":
    sys.exit(run(check, ".claude/hooks/validate_pr_merge.py"))
