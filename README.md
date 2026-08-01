<div align="center">

<img src="https://raw.githubusercontent.com/devemberx/knit-statusline/main/docs/assets/icon.png" alt="knit-statusline" width="140">

# knit-statusline

**A status line for [Claude Code](https://claude.com/claude-code) that you knit yourself — every row, every piece of every row, is one line of config.**

[Features](#-features) · [Install](#-install) · [Configure](#-configure) · [Segments](docs/CONFIGURATION.md#segment-reference) · [Uninstall](#-uninstall)

[![CI](https://github.com/devemberx/knit-statusline/actions/workflows/ci.yml/badge.svg)](https://github.com/devemberx/knit-statusline/actions/workflows/ci.yml)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

</div>

```
Opus 4.8 │ ✍️ 42% │ acme (main*) │ ◕ high

current ●●●●○○○○○○  42% ⟳ 5:00pm
weekly  ●●○○○○○○○○  18% ⟳ jul 27, 5:00pm
```

That is the default layout. Nothing above is hardcoded: each row is a `[[lines]]`
block in a TOML file, and each piece of a row is a segment name you can move,
drop, or restyle without touching code.

## ✨ Features

- 🧶 **Compose your own rows.** Reorder segments, drop what you do not want, put
  two on one line — a config edit, not a code edit.
- 🧱 **19 built-in segments.** Model, context, directory and git, session,
  effort, rate limits, cost, tokens, lines changed, PR.
- 🔢 **Cumulative token tracking.** Fresh input, cache writes, cache reads and
  output counted separately, because they are priced separately.
- 🔌 **Anything else you want.** A `command` segment runs any shell command, with
  a timeout and a cache.
- 📦 **No runtime dependencies.** One static binary — no `jq`, no `curl`, no
  Node. Installing needs `npx`; rendering does not.
- ⏱ **Fast.** No shell pipeline — no `jq`, no `curl`. A git subprocess runs only
  when the template asks for `{git}`, and under a timeout budget so a slow
  repository cannot stall the line.
- 🛟 **Never blanks your status line.** A failing segment is dropped and the row
  still draws; a broken config falls back with a marker naming the file.

## ⚡ Install

### Prerequisites

| Needed | Why |
| --- | --- |
| [Claude Code](https://claude.com/claude-code) | What draws the status line. The installer merges one `statusLine` key into its `~/.claude/settings.json` |
| [Node](https://nodejs.org) 16+ | For `npx` alone — the status line is a single static binary and needs no runtime |

No minimum Claude Code version: a segment whose stdin field your version does not
send is dropped, so an older release renders a shorter line rather than an error.

### Install it

```sh
npx @devemberx/knit-statusline
```

Then restart Claude Code. That is the whole install.

Pick a starting layout while installing:

```sh
npx @devemberx/knit-statusline install --preset minimal
```

<details>
<summary><b>What the installer touches</b></summary>

| Path | What happens |
| --- | --- |
| `~/.claude/knit-statusline` | The binary is copied here |
| `~/.claude/statusline.toml` | A starting config is written — an existing one is kept unless you pass `--force` |
| `~/.claude/settings.json` | One `statusLine` key is merged in |
| `~/.claude/settings.json.bak` | Backup of your settings as they were before the first install or uninstall — written once, never overwritten |

Your hooks, permissions, plugins and every other setting are read, merged and
written back untouched. If `statusLine` already pointed at another tool, the
installer reports what it replaced — and leaves it alone on uninstall.

On Windows, a home directory containing a space or one of `&` `'` needs Git Bash
present — it comes with Git for Windows.

</details>

<details>
<summary><b>Install without npm</b></summary>

Download the archive for your platform from
[Releases](https://github.com/devemberx/knit-statusline/releases), unpack it, and
run the binary's own installer:

```sh
./knit-statusline install
```

Builds ship for macOS, Linux and Windows on `amd64` and `arm64` — everywhere
Claude Code runs.

</details>

<details>
<summary><b>Verify it works</b></summary>

```sh
knit-statusline preview    # render sample data — no Claude Code restart needed
knit-statusline doctor     # config problems, with line numbers, and every available field
```

`preview --sparse` shows the same layout at the start of a fresh session,
before the first API call. `preview --unknown` shows a resumed session that
has not reported anything yet — what a legitimate `…` placeholder looks like,
as opposed to a segment that drops out of the row silently.

</details>

## 🎛 Configure

Everything lives in `~/.claude/statusline.toml`, and a project can override it
with its own `.claude/statusline.toml`. Each `[[lines]]` block is one row, and
`segments` is what goes on it, in order:

```toml
[[lines]]
segments = ["model", "context", "dir", "effort"]

# A block with no segments is a deliberate blank row.
[[lines]]

[[lines]]
segments = ["limit.5h", "limit.7d"]
separator = "  "
```

Reorder the names, delete one you do not want, or move two onto the same row —
that is the whole customisation model. A row whose segments all turn out empty is
dropped along with its separators, so a missing value never leaves `│ │` behind.

Start from a preset and edit from there:

| Preset | What it is |
| --- | --- |
| `reference` | The default. Reproduces the layout shown above. |
| `minimal` | One row: model, context, directory. No subprocess, no file reads. |
| `verbose` | Everything, including cumulative tokens, cost and lines changed. |
| `api` | For API-key users: rate limit rows drop out, tokens and cost take their place. |

Each preset is written out with its comments intact, so it doubles as a worked
example.

## 📚 Documentation

- **[Configuration](docs/CONFIGURATION.md)** — the layout model, all 19 segments
  and their fields, templates and alignment, every option, presets, and
  per-project overrides.
- **[Contributing](.github/CONTRIBUTING.md)** — setup, commit format, and the
  review conventions.

Two commands answer most questions without leaving the terminal:

```sh
knit-statusline preview    # render sample data after an edit
knit-statusline doctor     # config problems, and every available field
```

## 🗑 Uninstall

```sh
knit-statusline uninstall
```

Removes the `statusLine` key from your settings and deletes the installed
binary — every other setting is left as it was. Your `statusline.toml` stays put,
so reinstalling resumes rather than starting over.

## Credits

The default layout is modelled on
[nilbuild/claude-statusline](https://github.com/nilbuild/claude-statusline) by
Kamran Ahmed. This is a rewrite rather than a fork: that implementation assembles
its output as hardcoded strings, so changing what appears means editing the
script.

Built against the Claude Code
[status line](https://code.claude.com/docs/en/statusline) and
[plugin](https://code.claude.com/docs/en/plugins-reference) references. The
contribution conventions — commit format, git hooks, PR template — are adapted
from [mcp-server-polarion](https://github.com/devemberx/mcp-server-polarion).

## 📄 License

[MIT](LICENSE)
