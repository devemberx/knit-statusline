"""Command parsing shared by gh validation hooks."""

from __future__ import annotations

import json
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Callable

BULLET_LIMIT = 120
REQUIRED_BULLETS = 2

# argv_of None = no flag read at all. Without this, caller blame whichever flag
# it looked for first.
PARSE_ERROR = (
    "Command could not be parsed as shell words (unbalanced quote?), so no flag "
    "check can run. Put the body in a <<'EOF' heredoc, or remove the stray quote."
)

GH_API_RE = re.compile(r"\bgh\s+api\b")

# shlex know neither $() nor heredoc: it end token at first inner quote, so
# truncated body would pass. Slice payload off raw string.
HEREDOC_RE = re.compile(
    r"<<(?P<dash>-)?\s*(?:'(?P<sq>[^']+)'|\"(?P<dq>[^\"]+)\"|(?P<bare>\w+))"
)
BODY_CAT_RE = re.compile(r"(?<!\S)(?:--body|-b)[=\s]+[\"']?\$\(\s*cat\s*\Z")


def run(check: Callable[[str], list[str]], label: str) -> int:
    """PreToolUse entry. Exit 0 allow, 2 block."""
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0
    if data.get("tool_name") != "Bash":
        return 0
    cmd = (data.get("tool_input") or {}).get("command", "")
    if not isinstance(cmd, str) or not cmd:
        return 0

    errors = check(cmd)
    if not errors:
        return 0
    sys.stderr.write(f"BLOCKED by {label}:\n\n")
    for error in errors:
        sys.stderr.write(f"* {error}\n\n")
    return 2


def heredoc_spans(cmd: str) -> list[tuple[int, int, int, str]]:
    """(opener_start, payload_start, payload_end, payload) per closed heredoc.

    Unclosed or newline-less heredoc skipped: shell error on that command too,
    nothing sound to validate.
    """
    spans: list[tuple[int, int, int, str]] = []
    for opener in HEREDOC_RE.finditer(cmd):
        marker = opener.group("sq") or opener.group("dq") or opener.group("bare")
        nl = cmd.find("\n", opener.end())
        if nl == -1:
            continue
        # Bare << close at column 0 only; <<- allow leading tabs.
        indent = r"\t*" if opener.group("dash") else ""
        close = re.compile(
            rf"^{indent}{re.escape(marker)}\s*$", re.MULTILINE
        ).search(cmd, nl + 1)
        if close is None:
            continue
        spans.append((opener.start(), nl + 1, close.start(), cmd[nl + 1 : close.start()]))
    return spans


def strip_heredocs(cmd: str) -> str:
    """Heredoc payloads removed, for subcommand matching. Prose quoting gh
    command would else classify as that command.
    """
    out: list[str] = []
    prev = 0
    for _, payload_start, payload_end, _ in heredoc_spans(cmd):
        out.append(cmd[prev:payload_start])
        prev = payload_end
    out.append(cmd[prev:])
    return "".join(out)


def body_heredoc(cmd: str) -> str | None:
    for start, _, _, payload in heredoc_spans(cmd):
        if BODY_CAT_RE.search(cmd[:start]):
            return payload.rstrip("\r\n")
    return None


def argv_of(cmd: str) -> list[str] | None:
    try:
        return shlex.split(cmd)
    except ValueError:
        return None


def flag_value(
    argv: list[str], names: set[str], attached: str | None = None
) -> tuple[bool, str | None]:
    """(found, value) for `--flag v`, `--flag=v`, `-f v` and `-fv`.

    Value None when flag end command — shell error there, but flag was still
    asked for, so found stay True.
    """
    for i, arg in enumerate(argv):
        if arg in names:
            return True, argv[i + 1] if i + 1 < len(argv) else None
        for name in names:
            if name.startswith("--") and arg.startswith(f"{name}="):
                return True, arg[len(name) + 1 :]
        if (
            attached
            and not arg.startswith("--")
            and arg.startswith(attached)
            and len(arg) > len(attached)
        ):
            return True, arg[len(attached) :]
    return False, None


def read_file(path: str) -> str | None:
    try:
        return Path(path).read_text()
    except OSError:
        return None


def project_root() -> Path:
    """Repo root. Hook inherit arbitrary cwd."""
    env = os.environ.get("CLAUDE_PROJECT_DIR")
    if env:
        return Path(env)
    try:
        top = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.SubprocessError):
        return Path.cwd()
    return Path(top.stdout.strip()) if top.returncode == 0 and top.stdout.strip() else Path.cwd()


def bullets_of(text: str) -> list[str]:
    return [ln for ln in (ln.rstrip() for ln in text.splitlines()) if ln.startswith("- ")]


def bullet_errors(bullets: list[str], where: str) -> list[str]:
    errors: list[str] = []
    if len(bullets) != REQUIRED_BULLETS:
        errors.append(
            f"{where} must contain exactly {REQUIRED_BULLETS} bullets, "
            f"found {len(bullets)}. "
            "First says why the change is needed, second says what changed."
        )
    errors.extend(
        f"{where} bullet is {len(ln)} chars (limit: {BULLET_LIMIT}):\n    {ln}"
        for ln in bullets
        if len(ln) > BULLET_LIMIT
    )
    return errors
