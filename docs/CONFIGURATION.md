# Configuration

Everything lives in `~/.claude/statusline.toml`. A project can override it with
its own `.claude/statusline.toml`.

After any edit:

```sh
knit-statusline preview            # render sample data
knit-statusline preview --sparse   # how a fresh session looks: zeros, no rate limits
knit-statusline preview --unknown  # how a resumed session looks before it reports anything
knit-statusline doctor             # problems, with line numbers
```

Neither needs a Claude Code restart.

---

## The layout model

Each `[[lines]]` block is one row, and `segments` is what goes on it, in order.

```toml
[[lines]]
segments = ["model", "context", "dir", "session", "effort"]

# A block with no segments is a deliberate blank row.
[[lines]]

[[lines]]
segments = ["limit.5h"]

[[lines]]
segments = ["limit.7d"]
```

That is the `reference` preset, and it produces:

```
Opus 4.8 │ ✍️ 42% │ acme (main*) │ ⏱ 1h15m │ ◕ high

current ●●●●○○○○○○  42% ⟳ 5:00pm
weekly  ●●○○○○○○○○  18% ⟳ jul 27, 5:00pm
```

A blank row between content is kept. A row whose segments all turn out empty is
dropped, along with its separators — so a value that does not exist never leaves
`│ │` behind. Blank rows at the very top or bottom are trimmed, and a run of
consecutive blank rows collapses to one.

---

## Common edits

### Put the two rate limit windows on one row

```toml
[[lines]]
segments = ["limit.5h", "limit.7d"]
separator = "  "
```

### Drop something

Delete the name from `segments`. That is the whole operation.

### Track cumulative tokens

```toml
[[lines]]
segments = ["tokens"]

[segments.tokens]
template = "{io}{cache}"
scope = "session"          # or "project"
```

These are not the same numbers Claude Code reports on stdin. Those describe what
is currently in the context window; these are read from the transcript and are
cumulative for the session. The transcript is scanned incrementally, and the last
counted message id is remembered, so a re-scan does not count a message twice.

`{io}` and `{cache}` are preformatted groups — fresh traffic in white, cache
traffic in cyan, each with its own label and arrows. A session that touched no
cache drops `{cache}` and the gap before it. To lay the numbers out yourself:
`{input}` `{output}` `{cache_write}` `{cache_read}` `{total}` `{cache_hit}`, plus
`{input_raw}` and `{output_raw}` for exact digits.

### Add something of your own

```toml
[[lines]]
segments = ["model", "k8s"]

[segments.k8s]
type = "command"
command = "kubectl config current-context"
cache_ms = 5000
timeout_ms = 1000
```

The command runs in the session's working directory, through `sh -c` — `cmd /c`
on Windows. Only its first line is used. A command that fails or times out drops
its own segment and leaves the rest of the row intact.

`cache_ms` reuses the previous output for that long, which is what keeps a slow
command off the render path. It applies to `command` segments only.

### Change a threshold or a bar

```toml
[defaults]
bar_width = 12
warn = 40
high = 65
crit = 85

[segments.context]
crit = 95           # this segment only
```

### Choose what an unknown value shows

Seven segments — `context`, `session`, `cost`, `lines`, `tokens`, `limit.5h`,
`limit.7d` — hold their slot from the first render instead of dropping out until
Claude Code has something to report. A status line that grows a segment
mid-session reads as a bug as easily as a crash does, so the slot is there from
the start either way:

- The transcript proves the session has sent nothing yet, so the value really is
  zero: `✍️ 0%`, `↑0 ↓0`.
- The value is missing for any other reason — a resumed session, the gap after
  `/compact`, an unreadable transcript: `✍️ …%`, `↑… ↓…`.

Three segments never show the zero: `limit.5h` and `limit.7d` because both
windows are account-wide and carry across sessions — a brand-new session can
still open at 80% used — and `session` because its duration is wall-clock time:
a session may sit idle for minutes before its first call, so an empty
transcript proves nothing about how long it has been open.

```toml
[defaults]
unknown = "…"

[segments."limit.5h"]
unknown = "?"
```

`…` is U+2026. It is one column wide in most terminals, but East Asian
Ambiguous: a CJK-locale terminal that draws it two columns wide will shift
alignment specs such as `{pct:>3}` by one column. Set `unknown = "?"` there.

Setting `unknown = ""` opts a segment out entirely — no placeholder and no
fresh zeros either, the segment drops out on a missing value the way every
other segment already does:

```toml
[segments."limit.7d"]
unknown = ""
```

`preview --sparse` and `preview --unknown`, shown above, are how you check both
shapes without waiting for a real session to reach them.

---

## Templates

A segment's `template` is its text, with `{field}` placeholders:

```toml
[segments."limit.5h"]
template = "current {bar} {pct:>3}%{reset}"
```

`{pct:>3}` right-aligns to width 3; `{pct:<3}` left-aligns; a bare `{pct:3}`
right-aligns. Width is counted in visible characters, so colored values and bars
line up the way they look. Nothing is ever truncated — a clipped number is a
wrong number.

Some fields are preformatted and carry their own punctuation: `{reset}` above
already begins with ` ⟳ `, and `{git}` is a whole ` (branch*)`. That is what lets
them vanish cleanly when the value is absent, instead of leaving a stray `⟳ ` or
`()` behind. Write the pieces yourself — `⟳ {reset_time}`, `({branch})` — only if
you accept that orphan.

Every segment has a default template, so a row that only reorders names needs no
`[segments.*]` blocks at all. `doctor` lists the fields each segment offers, and
an unknown field name is reported there rather than silently rendering blank.

---

## Presets

```sh
knit-statusline install --preset minimal
knit-statusline preview --preset verbose
```

| Preset | What it is |
| --- | --- |
| `reference` | The default. Reproduces the layout shown above. |
| `minimal` | One row: model, context, directory. No subprocess, no file reads. |
| `verbose` | Everything, including cumulative tokens, cost and lines changed. |
| `api` | For API-key users: rate limit rows drop out, tokens and cost take their place. |

Installing writes the preset to `~/.claude/statusline.toml` with its comments
intact, so it doubles as a worked example. An existing config is never replaced
without `--force`.

---

## Project overrides

A `.claude/statusline.toml` inside a project is merged over your global one. The
project directory is `workspace.project_dir` — where Claude Code was launched —
so the override does not come and go as you `cd` around during a session.

Merge rules:

- **`[[lines]]` replaces the whole layout.** A project that declares any rows
  owns all of them. There is no principled answer to whether a two-row override
  extends or replaces a four-row base, so it replaces.
- **`[segments.*]` merges key by key.** You can change one field of one segment
  without restating a layout.
- **`[defaults]` merges key by key.**
- **`command` is dropped from a project file.** A project config is repository
  content, so honouring it would mean cloning a repository and opening it runs
  whatever that file says, on the first render and with no prompt. The trust
  boundary sits at `$HOME`: the key is ignored, `doctor` reports each one, and
  every other setting in the file still applies.

So this project file changes only the threshold, keeping the global layout:

```toml
[segments.context]
crit = 95
```

And this one takes over the layout entirely:

```toml
[[lines]]
segments = ["model", "dir", "limit.5h", "limit.7d"]
```

---

## Segment reference

`knit-statusline doctor` prints this for the version you have installed.

| Segment | Fields | Notes |
| --- | --- | --- |
| `model` | `name` `family` `version` `id` | `{name}` is `Opus 4.8`; `{family}` and `{version}` are its halves, and `{version}` is empty when the id carries no release number |
| `context` | `pct` `remaining` `used` `size` `bar` | Absent before the first API call and after `/compact` |
| `dir` | `name` `path` `project` `worktree` `git` `branch` `dirty` | `{git}` is a preformatted ` (branch*)`, empty outside a repo |
| `session` | `duration` `id` `name` | |
| `effort` | `level` `icon` | Absent on models without an effort parameter; `{icon}` tracks the level — see [Colors](#colors) |
| `limit.5h` | `pct` `bar` `reset` `reset_time` | Claude.ai subscribers only, after the first response |
| `limit.7d` | `pct` `bar` `reset` `reset_time` | Same, and absent independently of `limit.5h` |
| `tokens` | `io` `cache` `cache_hit` `input` `cache_write` `cache_read` `output` `total` `input_raw` `output_raw` | Read from the transcript, so the totals are cumulative |
| `cost` | `usd` `api_duration` | Client-side estimate; resets on `/clear` |
| `lines` | `added` `removed` | |
| `repo` | `host` `owner` `name` `slug` | Needs an `origin` remote |
| `pr` | `number` `state` `url` | |
| `version` | `version` | Claude Code version |
| `vim` | `mode` | Only when vim mode is on |
| `output_style` | `name` | |
| `fast_mode` | `state` | Only rendered when enabled |
| `thinking` | `state` | Only rendered when enabled |
| `caveman` | `mode` `icon` `savings` | Only while the [caveman](https://github.com/juliusbrussee/caveman) plugin is active; `{savings}` needs `/caveman-stats` and is out of the default template |
| `command` | `out` | Your own shell command |

Abbreviated counts read as `62.1k`, `1.2M`, `364.9M`. The `_raw` fields give
exact digits.

---

## Segment options

| Key | Applies to | Meaning | Default |
| --- | --- | --- | --- |
| `template` | any | The text, with `{field}` placeholders | the segment's own |
| `type` | any | The implementation, when it differs from the key name — how `command` segments are declared | the key name |
| `warn` `high` `crit` | anything with a percentage | Color thresholds, 0–100, in that order | `50` `70` `90` |
| `bar_width` | `context`, `limit.*` | Bar width in characters | `10` |
| `scope` | `tokens` | `"session"` or `"project"` | `"session"` |
| `include_sidechain` | `tokens` | Count subagent transcripts too | `false` |
| `command` | `type = "command"` | What to run | — |
| `timeout_ms` | `type = "command"`, `dir` | How long to wait — bounds the command, or the git call for `dir` | `1000` |
| `cache_ms` | `type = "command"` | Reuse the previous output for this long | `0` |
| `unknown` | `context`, `session`, `cost`, `lines`, `tokens`, `limit.5h`, `limit.7d` | Text shown while a value is unknown rather than dropping the segment; `""` opts back out | `…` |

Set fallbacks for `separator`, `bar_width`, `warn`, `high`, `crit` and `unknown`
under `[defaults]`. A per-line `separator` overrides the default for that row;
the built-in separator is `" │ "`.

`warn` must not exceed `high`, nor `high` exceed `crit`, and the check runs
against effective values — set `warn = 95` alone and it is compared against the
`high` you inherited, so `doctor` reports it rather than painting a row that
never leaves red.

---

## Colors

Thresholds escalate green → orange → yellow → red as a percentage climbs past
`warn`, `high` and `crit`. Filled bar segments carry that color and the
remainder is dimmed.

The `effort` segment has its own scale. Claude Code defines five levels, and each
gets a distinct glyph and color:

| Level | Icon | Color |
| --- | --- | --- |
| `low` | `◔` | dim |
| `medium` | `◑` | white |
| `high` | `◕` | cyan |
| `xhigh` | `●` | magenta |
| `max` | `✦` | orange |

Ultracode is a sixth entry Claude Code does not report as a level — it arrives as
plain `xhigh`, and the segment derives it from the session transcript instead. It
renders as `✺ ultra` in pink, and falls back to `● xhigh` whenever detection
fails.

A level the binary does not recognise renders `○` in dim — its own slot, so it
never reads as `medium`. The glyphs form a fill ramp, so the levels stay
distinguishable with color switched off.

Setting the `NO_COLOR` environment variable to anything non-empty disables color
entirely.

---

## Where things live

| Path | What |
| --- | --- |
| `~/.claude/statusline.toml` | Your configuration |
| `<project>/.claude/statusline.toml` | Per-project override |
| `~/.claude/settings.json` | Where the `statusLine` key points at the binary |
| `~/.claude/settings.json.bak` | Backup of your settings as they were before the first install or uninstall — written once, never overwritten |
| `~/.claude/knit-statusline` | The installed binary |
| `~/.claude/statusline-cache/` | Transcript cursors and command output caches |

The cache is disposable. Deleting it costs one slower render.
