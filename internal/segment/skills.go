package segment

import (
	"strconv"

	"github.com/devemberx/knit-statusline/internal/render"
	"github.com/devemberx/knit-statusline/internal/transcript"
)

func init() {
	register("skills", Def{
		Fields:          []string{"count", "icon", "last"},
		DefaultTemplate: "{icon} {count}",
		Build:           buildSkills,
	})
}

// Bulb is Unicode 6.0, wide font coverage. Same generation as caveman's bone
// and context's writing hand. ✦ taken -- effortStyle render it for max.
const skillsIcon = "💡"

// buildSkills report skills Claude Code loaded for session.
//
// Count come from skill_listing attachment, which is Claude Code's own
// resolution -- plugin state, project skills and disabled entries already
// folded in. Enumerating skill directories would re-derive that and disagree.
//
// Not Stable: value set once per session and never fall back to unknown, so a
// dropped segment cannot vanish mid-row. Session with no listing print nothing
// rather than parking "💡 0" on row all session.
func buildSkills(c Context) Result {
	if c.In.TranscriptPath == "" {
		return empty
	}

	opts := transcript.Options{
		TranscriptPath: c.In.TranscriptPath,
		Scope:          transcript.ScopeSession,
	}

	cache := transcript.LoadCache(c.CacheDir, opts)
	sum, cache := transcript.Scan(opts, cache)

	// Cache write failure cost one rescan next render. Not worth failing over.
	_ = transcript.SaveCache(c.CacheDir, opts, cache)

	if !sum.Skills.Known {
		return empty
	}

	return Result{
		Base: render.Dim,
		Fields: render.Fields{
			"icon":  render.Colored(skillsIcon, render.Magenta),
			"count": render.Colored(strconv.Itoa(sum.Skills.Available), render.White),
			// Template naming {last} must not break before first invocation.
			"last": render.Colored(sum.Skills.Last, render.Dim),
		},
	}
}
