# CLAUDE.md

Go status line for Claude Code. Read one JSON object on stdin, print one row.
Single static binary, no runtime deps, no network.

Migration from `bare-statusline` done — every Go package, `cmd/statusline`, the
release pipeline (`.goreleaser.yaml`, `npm/`, `publish.yml`), `SECURITY.md` and
`.githooks/pre-push` all land here.

## Releasing

Tag is the version; nothing in the tree records it. `publish.yml` fire on `v*`,
wait for approval in the `release` environment, then GoReleaser cut a draft
release and seven npm packages publish over OIDC — no token anywhere. Draft flip
public only after all seven land. Procedure lives in `.claude/skills/deploy/`.

## Commands

```bash
go build -o knit-statusline ./cmd/statusline
go test -race ./...
gofmt -l . && go vet ./...            # gofmt must print nothing
./knit-statusline preview             # complete sample data
./knit-statusline preview --sparse    # degraded case — check both
./knit-statusline doctor              # config problems + every available field
```

## Architecture

- `cmd/statusline/` — `main.go` render path plus subcommand switch, `commands.go`
  bodies. No arg = render from stdin.
- `internal/schema/` — Claude Code payload as a typed tree. Every field pointer
  or zero-tolerant; `Parse` never hard-fail.
- `internal/config/` — `config.go` TOML types + `Resolve`, `load.go` read and
  merge, `validate.go` problem reporting.
- `internal/segment/` — `segment.go` `Def` registry, `builtin.go` field-mapping
  segments, `git.go`, `tokens.go`, `command.go`, `format.go`.
- `internal/render/` — `style.go` colors, palette, thresholds; `template.go`
  placeholder expansion.
- `internal/transcript/` — `scan.go` JSONL scan into `Totals`, `cursor.go` byte
  offset plus last `message.id`, cached under `~/.claude/statusline-cache/`.
- `internal/statusline/` — `Render` assemble rows, `Fallback` last resort.
- `internal/install/` — `install.go` binary copy, `settings.go` merge-safe
  settings.json write.

## Non-Negotiable Rules

- Render path NEVER exit non-zero, NEVER panic, NEVER print nothing. Top-level
  `recover` plus one per segment. Empty row leave user unable to tell crash from
  silence.
- One failing segment dropped; rest of row still draw. Broken `statusline.toml`
  fall back to builtin preset + `⚠ statusline.toml` marker; full diagnostic
  belong in `doctor`.
- Three facts, not two: zero, unknown, absent. Zero only when transcript prove
  session sent nothing yet — probe error resolve to unknown, never fresh, since
  wrong fresh print lying 0. Unknown render `…` in stable segments, row shape
  fixed so vanishing slot never read as crash. Absent stay dropped for state
  flags where nothing mean off. `limit.*` never zero: account-wide window carry
  across sessions.
- Token counting dedup on `message.id`: Claude Code write one JSONL line per
  content block, each repeating whole `usage` object, so naive sum over-count
  ~3x. Id persist in cursor beside byte offset, else message straddling two scans
  double-count.
- Four counters (input, cache write, cache read, output) stay apart. Cache read
  run orders of magnitude over fresh input and price differently.
- New segment field must be listed in `Def.Fields`, else `doctor` and config
  validation cannot see it and user get a silently blank segment.
- Config problem name file that declared it. `Load` keep bytes per layer and
  `Origin` search them, table headers before mentions. Guessing one file blame
  whichever layer merged last, pointing at its innocent rows.
- Subprocess need both a context deadline and `cmd.WaitDelay`. Context kill
  direct child alone; grandchild holding stdout keep `Output()` reading past it.
- Caches disposable: temp file then rename — two renders may overlap. Corrupt or
  version-mismatched cache = discard and rebuild.

Contribution rules — branch names, commit format, PR and merge — live in
[.github/CONTRIBUTING.md](.github/CONTRIBUTING.md). Not repeated here.

## Comments

- Caveman style: drop articles and filler. `# Deletion push = all-zero sha, no
  branch name to check.` Why, not what. NEVER restate self-evident code.
- One distinct fact per line. Technical terms, ids and numbers exact. No
  invented abbreviations.
- No `WARNING:`/`NOTE:` prefixes, no banner dividers, no dev narrative.
- `TODO` = `# TODO(#issue): concrete action`. No dead code. Comments sync when
  the code change.
