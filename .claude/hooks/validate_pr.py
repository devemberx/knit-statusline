#!/usr/bin/env python3
"""PreToolUse hook: block gh PR commands that break repo convention.

Fires on: gh pr create|edit|comment, gh api .../pulls...

Squash build commit from PR title + ## Changes bullets, so rules mirror
.githooks/commit-msg:
  1. Prose English. Closed ``` fences and `inline spans` exempt — pasted
     terminal output carry glyphs by design.
  2. Every ## Type of Change checkbox from template kept.
  3. ## Changes: exactly 2 bullets, each <= 120 chars.
  4. Title "type(scope)!: summary", lowercase, no period, <= 50 chars.

Fail closed: unreadable body, or create with no body, is error not pass.

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
    project_root,
    read_file,
    run,
    strip_heredocs,
)

NON_ASCII_RE = re.compile(r"[^\x00-\x7F]")
# Formatting, not language.
TYPOGRAPHIC_RE = re.compile("[–—‘’“”•…←→↔⇒≠≤≥±·×÷]")
EMOJI_RE = re.compile(
    "[\U0001f000-\U0001faff"
    "\U00002600-\U000027bf"
    "\U00002b00-\U00002bff"
    "\U0000fe00-\U0000fe0f"
    "\U0000200d]"
)
# Closed pairs only: unclosed fence must not exempt rest of body.
FENCE_RE = re.compile(r"^```[\s\S]*?^```", re.MULTILINE)
INLINE_CODE_RE = re.compile(r"`[^`\n]*`")
# GitHub render [X] same as [x].
CHECKBOX_RE = re.compile(r"^- \[[ xX]\] (.+)$", re.MULTILINE)
TYPE_HEADER_RE = re.compile(r"^##\s+Type of Change\s*$", re.IGNORECASE | re.MULTILINE)
CHANGES_HEADER_RE = re.compile(r"^##\s+Changes\s*$", re.IGNORECASE | re.MULTILINE)
NEXT_SECTION_RE = re.compile(r"^##\s", re.MULTILINE)
TEMPLATE_PATH = ".github/PULL_REQUEST_TEMPLATE.md"

# Same rule as .githooks/commit-msg: type allowlist, scope free-form kebab,
# trailing "!" breaking. Scope allowlist here would diverge silent.
TITLE_TYPES = ("feat", "fix", "docs", "refactor", "perf", "test", "ci", "chore")
TITLE_RE = re.compile(
    rf"^(?:{'|'.join(TITLE_TYPES)})(?:\([a-z][a-z0-9-]*\))?!?: [a-z]"
)
TITLE_LIMIT = 50

PR_CREATE_RE = re.compile(r"\bgh\s+pr\s+create\b")
PR_EDIT_RE = re.compile(r"\bgh\s+pr\s+edit\b")
PR_COMMENT_RE = re.compile(r"\bgh\s+pr\s+comment\b")
WEB_FLAG_RE = re.compile(r"(?<!\S)(?:--web|-w)(?!\S)")
# Creation POST to /pulls with no trailing id, so bare segment count too.
API_PULLS_RE = re.compile(r"/pulls(?:/|\b)")
# Sub-resources carry no PR title or body; /merge belong to validate_pr_merge.
API_SUBRESOURCE_RE = re.compile(r"/pulls/\d+/\w+")
API_FIELD_FLAGS = {"-F", "-f", "--field", "--raw-field"}

NON_ASCII_ERROR = (
    "Body contains non-ASCII characters (other than emoji and typographic "
    "punctuation). Per repo convention PR and commit artifacts must be in "
    "English. Only closed ``` fences and `inline spans` are exempt."
)


def check(cmd: str) -> list[str]:
    kind = classify(strip_heredocs(cmd))
    if kind is None:
        return []

    # shlex failure lose every flag. Say so, else passed --title read absent.
    parsed = argv_of(cmd) is not None
    found_body, body = extract_body(cmd, kind)

    errors: list[str] = []
    if not parsed:
        errors.append(PARSE_ERROR)
    if found_body and body is None:
        errors.append("--body-file / -F points at a file that could not be read.")
    if body is not None and has_disallowed_non_ascii(body):
        errors.append(NON_ASCII_ERROR)
    if kind == "comment":
        return errors

    title = extract_title(cmd, kind)
    if title is not None:
        errors.extend(title_errors(title))
    elif kind == "create" and parsed:
        errors.append("Pass an explicit --title; it becomes the squash commit subject.")

    if kind == "create" and not found_body and parsed:
        errors.append(
            "Pass an explicit --body / --body-file. --fill and the editor "
            "cannot produce the template that the squash commit body comes from."
        )
    if body is not None:
        errors.extend(template_errors(body))
        errors.extend(changes_errors(body))
    return errors


def classify(cmd: str) -> str | None:
    if PR_CREATE_RE.search(cmd):
        # --web hand body to browser form, out of reach here.
        return None if WEB_FLAG_RE.search(cmd) else "create"
    if PR_EDIT_RE.search(cmd):
        return "edit"
    if PR_COMMENT_RE.search(cmd):
        return "comment"
    if (
        GH_API_RE.search(cmd)
        and API_PULLS_RE.search(cmd)
        and not API_SUBRESOURCE_RE.search(cmd)
    ):
        return "api"
    return None


def extract_body(cmd: str, kind: str) -> tuple[bool, str | None]:
    heredoc = body_heredoc(cmd)
    if heredoc is not None:
        return True, heredoc

    argv = argv_of(cmd)
    if argv is None:
        return False, None
    if kind == "api":
        return _api_field(argv, "body")

    found, value = flag_value(argv, {"--body", "-b"}, attached="-b")
    if found:
        return True, value
    found, value = flag_value(argv, {"--body-file", "-F"}, attached="-F")
    if found:
        return True, read_file(value) if value is not None else None
    return False, None


def extract_title(cmd: str, kind: str) -> str | None:
    argv = argv_of(cmd)
    if argv is None:
        return None
    if kind == "api":
        # gh api -t = --template; title arrive as title= field.
        return _api_field(argv, "title")[1]
    return flag_value(argv, {"--title", "-t"}, attached="-t")[1]


def _api_field(argv: list[str], name: str) -> tuple[bool, str | None]:
    prefix = f"{name}="
    for i, arg in enumerate(argv):
        if arg not in API_FIELD_FLAGS or i + 1 >= len(argv):
            continue
        value = argv[i + 1]
        if not value.startswith(prefix):
            continue
        value = value[len(prefix) :]
        return True, read_file(value[1:]) if value.startswith("@") else value
    return False, None


def has_disallowed_non_ascii(body: str) -> bool:
    prose = INLINE_CODE_RE.sub("", FENCE_RE.sub("", body))
    return bool(NON_ASCII_RE.search(TYPOGRAPHIC_RE.sub("", EMOJI_RE.sub("", prose))))


def _section(text: str, header: re.Pattern[str]) -> str | None:
    match = header.search(text)
    if match is None:
        return None
    nxt = NEXT_SECTION_RE.search(text, match.end())
    return text[match.end() : nxt.start()] if nxt else text[match.end() :]


def title_errors(title: str) -> list[str]:
    errors: list[str] = []
    if not TITLE_RE.match(title):
        errors.append(
            "Title must be a conventional-commit subject 'type(scope)!: summary' "
            "with a lowercase summary. "
            f"type one of: {', '.join(TITLE_TYPES)}. "
            "scope optional, lowercase kebab, omit it for a change that spans "
            "the repo. Trailing '!' mark a breaking change. "
            "Same rule as .githooks/commit-msg."
        )
    if title.endswith("."):
        errors.append(
            "Title must not end with a period; it becomes the squash commit subject."
        )
    if len(title) > TITLE_LIMIT:
        errors.append(
            f"Title is {len(title)} chars (limit: {TITLE_LIMIT}); it becomes the "
            "squash commit subject."
        )
    return errors


def template_errors(body: str) -> list[str]:
    """Checkboxes body dropped from template's Type of Change block.

    Only that block mandatory — every other template section is free prose.
    """
    template = read_file(str(project_root() / TEMPLATE_PATH))
    if template is None:
        return [f"Could not read {TEMPLATE_PATH}; the checkbox check cannot run."]
    section = _section(template, TYPE_HEADER_RE)
    if section is None:
        return [f"{TEMPLATE_PATH} has no '## Type of Change' section."]

    missing = sorted(set(CHECKBOX_RE.findall(section)) - set(CHECKBOX_RE.findall(body)))
    if not missing:
        return []
    listed = "\n".join(f"    - [ ] {m}" for m in missing)
    return [
        f"Body is missing template checkboxes. Do NOT delete unchecked options.\n{listed}"
    ]


def changes_errors(body: str) -> list[str]:
    section = _section(body, CHANGES_HEADER_RE)
    if section is None:
        return [
            f"Body is missing the required '## Changes' section (see {TEMPLATE_PATH})."
        ]
    return bullet_errors(bullets_of(section), "The ## Changes section")


if __name__ == "__main__":
    sys.exit(run(check, ".claude/hooks/validate_pr.py"))
