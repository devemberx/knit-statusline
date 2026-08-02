// Package fixtures hold sample status line documents.
//
// Same files serve tests and preview. Deliberate: preview is what a user run to
// check a config edit, so it must exercise every shape tests assert on --
// degraded one included, where a layout fine on complete data fall apart.
//
// Coverage track ported render path, not every schema field: agent and
// top-level worktree shapes land with segment that render them.
package fixtures

import _ "embed"

// Full carry subscriber rate limits, populated usage, effort, git repo, vim
// mode, worktree name, open pull request.
//
//go:embed full.json
var Full []byte

// Sparse is degraded case: non-subscriber, first render, before any API
// response. No rate_limits, current_usage null, used_percentage null, no
// effort, no repo, no vim, no session_name.
//
//go:embed sparse.json
var Sparse []byte

// Unknown is resumed session: nothing reported yet and nothing proven either.
// Sparse minus cost and minus context_window, so every stable segment reach
// its placeholder. Sparse keep both blocks populated with real zeros, which
// win on known path under either freshness and hide four of seven placeholders.
//
//go:embed unknown.json
var Unknown []byte

// Empty is bare JSON object -- floor every code path must survive.
//
//go:embed empty.json
var Empty []byte

// TodosJSONL is transcript lines, not a status line document. Fixture JSON carry
// no transcript_path, so segments reading transcript off disk have nothing to
// open -- preview and tests both need a real file.
//
// Last non-sidechain TodoWrite hold 7 entries, 3 completed. Sidechain line carry
// different counts -- bad skip logic shows wrong count in output, not only a
// failing test.
//
// transcript_path is one field, so preview point every transcript-reading
// segment here, todo or not. Lines stay bare for that reason: no usage key, so
// tokens still draw its no-usage shape, and no deferred_tools_delta, so mcp
// still drop. Segment added later inherit this file -- give it what it need or
// its preview go silent.
//
//go:embed todos.jsonl
var TodosJSONL []byte

// PreviewEpoch is instant preview pretend to run at, so its output stay stable
// and diff-reviewable.
const PreviewEpoch int64 = 1753243200
