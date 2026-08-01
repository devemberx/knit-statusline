// Package statusline assemble configured segments into finished rows.
//
// Sit above config, schema, render and segment so segment depend on render with
// no cycle.
package statusline

import (
	"strings"
	"time"

	"github.com/devemberx/knit-statusline/internal/config"
	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/schema"
	"github.com/devemberx/knit-statusline/internal/segment"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

// Options carry what renderer need beyond config and input.
type Options struct {
	Palette  render.Palette
	Now      time.Time
	CacheDir string

	// Claude Code config root. Segment reading file beside settings.json get it
	// from here, never from environment -- test point it at temp directory.
	ConfigDir string

	// Appended to first row when config unusable as written. Terse -- no room
	// on row; doctor hold full text.
	Warning string

	// Pin session state instead of probing. Preview and tests use it; a fixture
	// transcript path exist or not depending on machine running it, and layout
	// check must not turn on that.
	SessionState *transcript.State
}

// Render produce status line text.
//
// Degradation partial: failing segment lose own slot, rest of row still draw.
// Empty return = caller print Fallback.
func Render(cfg *config.Config, in *schema.Input, opts Options) string {
	if cfg == nil {
		return ""
	}

	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	// One probe per render. Segment never call it: eight segments probing
	// same file eight times differ only in cost.
	fresh := sessionFresh(in, opts)

	// Same name on two rows otherwise run git and user command twice per redraw.
	memo := map[string]string{}

	var rows []string
	for _, line := range cfg.Lines {
		if line.Blank() {
			rows = append(rows, "")
			continue
		}

		parts := make([]string, 0, len(line.Segments))
		for _, name := range line.Segments {
			text, done := memo[name]
			if !done {
				text = renderSegment(cfg, in, opts, name, fresh)
				memo[name] = text
			}
			if text != "" {
				parts = append(parts, text)
			}
		}

		// Drop row: bare separator stand for information that does not exist.
		if len(parts) == 0 {
			continue
		}
		rows = append(rows, strings.Join(parts, cfg.Separator(line)))
	}

	rows = collapseBlanks(rows)

	if opts.Warning != "" {
		marker := opts.Palette.Wrap("⚠ "+opts.Warning, render.Yellow)
		if len(rows) == 0 {
			rows = []string{marker}
		} else {
			rows[0] += " " + marker
		}
	}

	return strings.Join(rows, "\n")
}

// sessionFresh resolve whether this session has sent anything yet.
//
// Nil input probe nothing: no transcript path to read, and live is answer
// that print no number.
func sessionFresh(in *schema.Input, opts Options) bool {
	state := transcript.StateLive
	switch {
	case opts.SessionState != nil:
		state = *opts.SessionState
	case in != nil:
		state = transcript.SessionState(in.TranscriptPath)
	}
	return state == transcript.StateFresh
}

func renderSegment(cfg *config.Config, in *schema.Input, opts Options, name string, fresh bool) string {
	def, ok := segment.Lookup(cfg.Segments[name].Kind(name))
	if !ok {
		// Validate catch unknown name up front. One reaching here mean build
		// lack it. Skip, still draw row.
		return ""
	}

	resolved := cfg.Resolve(name, def.DefaultTemplate)
	res := segment.Build(segment.Context{
		In:        in,
		Cfg:       resolved,
		Palette:   opts.Palette,
		Now:       opts.Now,
		CacheDir:  opts.CacheDir,
		ConfigDir: opts.ConfigDir,
		Fresh:     fresh,
	})
	if res.Empty {
		return ""
	}

	out := opts.Palette.Expand(resolved.Template, res.Fields, res.Base)
	// Trim decide emptiness only, never what get returned. Alignment spec exist
	// to produce padding -- "{pct:>5}" render "   42" -- so trimming undo every
	// spec at template edge.
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return out
}

// collapseBlanks squash blank runs to one row, drop leading and trailing blanks.
//
// One blank row between content deliberate -- reference layout split header from
// rate limit bars that way. Run of two or more is not: that is what dropped row
// leave between its neighbours.
func collapseBlanks(rows []string) []string {
	out := rows[:0]
	for _, r := range rows {
		if r == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, r)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// Fallback is last resort: whatever identity still establish.
//
// Claude Code print nothing when command print nothing, so blank status line
// read as crash. Model name alone keep row alive.
func Fallback(in *schema.Input, p render.Palette) string {
	if in != nil && in.Model.DisplayName != "" {
		return p.Wrap(in.Model.DisplayName, render.Blue)
	}
	return p.Wrap("Claude", render.Blue)
}
