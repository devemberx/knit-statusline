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
)

// Options carry what renderer need beyond config and input.
type Options struct {
	Palette  render.Palette
	Now      time.Time
	CacheDir string

	// Appended to first row when config could not be used as written. Terse on
	// purpose -- no room on row; doctor hold full text.
	Warning string
}

// Render produce status line text.
//
// Return something printable whatever it get handed. Silent failure leave user
// a blank row and no way to find out why, so degradation stay partial.
func Render(cfg *config.Config, in *schema.Input, opts Options) string {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	var rows []string
	for _, line := range cfg.Lines {
		// No segments = deliberate blank row.
		if line.Blank() {
			rows = append(rows, "")
			continue
		}

		parts := make([]string, 0, len(line.Segments))
		for _, name := range line.Segments {
			if text := renderSegment(cfg, in, opts, name); text != "" {
				parts = append(parts, text)
			}
		}

		// Every segment empty, so drop row. Emitting it leave a bare separator
		// standing for information that does not exist.
		if len(parts) == 0 {
			continue
		}
		rows = append(rows, strings.Join(parts, cfg.Separator(line)))
	}

	rows = trimBlankEdges(rows)

	if opts.Warning != "" {
		marker := opts.Palette.Wrap("⚠ "+opts.Warning, render.Yellow)
		if len(rows) == 0 {
			rows = []string{marker}
		} else {
			rows[0] = strings.TrimRight(rows[0]+" "+marker, " ")
		}
	}

	return strings.Join(rows, "\n")
}

func renderSegment(cfg *config.Config, in *schema.Input, opts Options, name string) string {
	def, ok := segment.Lookup(cfg.Segments[name].Kind(name))
	if !ok {
		// Validate report unknown segments up front. One reaching here mean
		// config name something this build lack. Skip it, still draw row.
		return ""
	}

	resolved := cfg.Resolve(name, def.DefaultTemplate)
	res := segment.Build(segment.Context{
		In:       in,
		Cfg:      resolved,
		Palette:  opts.Palette,
		Now:      opts.Now,
		CacheDir: opts.CacheDir,
	})
	if res.Empty {
		return ""
	}

	out := opts.Palette.Expand(resolved.Template, res.Fields, res.Base)
	// Trim decide emptiness only, never what get returned. Alignment spec exist
	// to produce padding -- "{pct:>5}" render "   42" -- and trimming output
	// silently undo every spec that sit at either end of a template.
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return out
}

// trimBlankEdges drop leading and trailing blank rows.
//
// Blank row between content carry meaning -- reference layout split header from
// rate limit bars that way. First or last one is wasted terminal space, left
// behind when segments around it come back empty.
func trimBlankEdges(rows []string) []string {
	start, end := 0, len(rows)
	for start < end && rows[start] == "" {
		start++
	}
	for end > start && rows[end-1] == "" {
		end--
	}
	return rows[start:end]
}

// Fallback is last resort: whatever identity still establish.
//
// Claude Code print nothing when command print nothing, so empty status line
// read as crash. Model name alone keep row alive.
func Fallback(in *schema.Input, p render.Palette) string {
	if in != nil && in.Model.DisplayName != "" {
		return p.Wrap(in.Model.DisplayName, render.Blue)
	}
	return p.Wrap("Claude", render.Blue)
}
