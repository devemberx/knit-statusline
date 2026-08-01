// Package segment hold pieces a status line row is built from.
//
// Each segment declare its template fields and default template. Reordering row
// then need no template at all, and doctor name every field that exist.
package segment

import (
	"slices"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
)

// Context is everything segment may read.
type Context struct {
	In      *schema.Input
	Cfg     config.Resolved
	Palette render.Palette
	Now     time.Time

	// Backs segments that must not recompute every render.
	CacheDir string

	// Claude Code config root -- settings.json live here, and caveman flag
	// beside it. Level never reach stdin payload, so file is only source.
	ConfigDir string
}

func (c Context) Thresholds() render.Thresholds {
	return render.Thresholds{Warn: c.Cfg.Warn, High: c.Cfg.High, Crit: c.Cfg.Crit}
}

// pipeDrain bound Wait after context cancellation. Killing a child leave any
// grandchild holding inherited stdout, and Output() read to EOF regardless of
// deadline, so exec.Cmd.WaitDelay is what enforce a timeout.
const pipeDrain = 100 * time.Millisecond

// budget bound every subprocess one segment start. Nonsense value fall back
// rather than disable timeout: zero would mean cancel before exec.
func (c Context) budget() time.Duration {
	if c.Cfg.TimeoutMS <= 0 {
		return config.DefaultTimeoutMS * time.Millisecond
	}
	return time.Duration(c.Cfg.TimeoutMS) * time.Millisecond
}

// Result is what segment produce.
//
// Empty = nothing to say: no rate limits on this plan, no git repo here, no
// transcript yet. Renderer drop it with its separator -- "│ │" tell reader less
// than nothing.
type Result struct {
	Fields render.Fields
	Base   render.Color
	Empty  bool
}

var empty = Result{Empty: true}

type Def struct {
	// Every placeholder template may use. Validate reject anything else, before
	// user meet silently blank segment.
	Fields          []string
	DefaultTemplate string
	Build           func(Context) Result
}

var registry = map[string]Def{}

// Called from package init functions.
func register(name string, d Def) {
	if _, dup := registry[name]; dup {
		panic("segment registered twice: " + name)
	}
	registry[name] = d
}

func Lookup(kind string) (Def, bool) {
	d, ok := registry[kind]
	return d, ok
}

// Known report kind's fields in shape config.Validate expect.
func Known(kind string) ([]string, bool) {
	d, ok := registry[kind]
	if !ok {
		return nil, false
	}
	return d.Fields, true
}

// Names list every registered segment, sorted, for doctor output.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Build run segment, turning panic into empty result.
//
// Segments are one place running open-ended work -- subprocesses, file reads,
// user commands. Contain failure here or one bad segment cost whole row.
func Build(ctx Context) (res Result) {
	d, ok := registry[ctx.Cfg.Kind]
	if !ok {
		return empty
	}
	defer func() {
		if recover() != nil {
			res = empty
		}
	}()
	return d.Build(ctx)
}
