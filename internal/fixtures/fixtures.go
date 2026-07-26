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

// Empty is bare JSON object -- floor every code path must survive.
//
//go:embed empty.json
var Empty []byte

// PreviewEpoch is instant preview pretend to run at, so its output stay stable
// and diff-reviewable.
const PreviewEpoch int64 = 1753243200
