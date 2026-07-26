// Package fixtures hold sample status line documents.
//
// Same files serve the tests and preview. Deliberate: preview is what a user
// run to check a config edit, so it must exercise the shapes the tests assert
// on -- degraded one included, where a layout fine on complete data fall apart.
//
// Coverage track ported render path, not every schema field: agent, pr and
// worktree shapes land with segment that render them.
package fixtures

import _ "embed"

// Full carry subscriber rate limits, populated usage, effort, git repo, vim mode.
//
//go:embed full.json
var Full []byte

// Sparse is the degraded case: non-subscriber, first render, before any API
// response. No rate_limits, current_usage null, used_percentage null, no
// effort, no repo, no vim, no session_name.
//
//go:embed sparse.json
var Sparse []byte

// Empty is the empty JSON object, the floor every code path must survive.
//
//go:embed empty.json
var Empty []byte

// PreviewEpoch is the instant preview pretend to run at, so its output stay
// stable and diff-reviewable.
const PreviewEpoch int64 = 1753243200
