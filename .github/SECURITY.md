# Security Policy

## Supported versions

Only the latest release receives fixes. It is a single static binary with no
runtime dependencies, so upgrading is replacing one file.

| Version | Supported |
| --- | --- |
| latest release | ✅ |
| anything older | ❌ |

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: **Security → Report a
vulnerability** on this repository. That keeps the report private until a fix
ships. Please do not open a public issue for anything exploitable.

Include the version (`knit-statusline version`), your platform, your
`statusline.toml` if it is involved, and the smallest input that reproduces the
problem.

Expect an acknowledgement within a week. This is a small project maintained in
spare time, so a fix follows when there is one to ship — you will be told either
way, and credited in the release notes unless you would rather not be.

## What this program actually does

Worth knowing before you file, because most of the surface is smaller than it
looks:

- It reads one JSON object on stdin, from Claude Code.
- It reads the transcript file that object names — Claude Code puts those under
  `~/.claude/projects/` — and writes a disposable cache under
  `~/.claude/statusline-cache/`.
- `install` copies the binary to `~/.claude/` and merges a `statusLine` key into
  `~/.claude/settings.json`. `uninstall` removes the binary, and removes that key
  only while it still points at our copy.
- Before either writes `settings.json` it copies the file to `settings.json.bak`.
  An existing `.bak` is left alone, so it keeps the file as it stood before the
  first install rather than tracking the newest edit.
- **It makes no network requests.** Ever. Not for updates, not for telemetry.

## Not vulnerabilities

- **`statusline.toml` runs shell commands.** The `command` segment executes what
  the config tells it to. That is the feature. Anyone who can write your
  `statusline.toml` can already run code as you.
- **Transcript contents reaching the terminal.** Segment output is drawn from
  your own session; the status line does not send it anywhere.
- **The status line staying silent about an error.** Deliberate. The render path
  drops a failing segment rather than blanking the row. Run `doctor` for the
  full diagnostic.

Vulnerabilities do include: a crafted transcript or stdin payload that causes
code execution, a path that writes outside `~/.claude` or the project directory,
anything that leaks file contents off the machine, and anything in the release
pipeline that could ship an artifact we did not build.

## Release integrity

The npm packages are published straight from the tagged workflow run over OIDC,
with no long-lived publish token in the repository. npm records
[provenance](https://docs.npmjs.com/generating-provenance-statements) for each
one, so you can see which workflow run and which commit built it:

```sh
npm view @devemberx/knit-statusline
```

The GitHub release archives ship a `checksums.txt` beside them. That catches a
corrupted download, not a rewritten release — whoever can replace an archive can
replace the checksum file in the same edit. If you need more than that, install
from npm or build the tag yourself.
