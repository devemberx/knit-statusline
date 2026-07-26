// Package config load, merge and validate statusline.toml.
//
// Format hybrid on purpose: line is ordered list of segment names, and segment
// may carry template controlling its text down to each character. Names alone
// cover common edits -- reorder, drop, two on one row -- with no templating
// language to learn.
package config

type Config struct {
	Defaults Defaults            `toml:"defaults"`
	Lines    []Line              `toml:"lines"`
	Segments map[string]*Segment `toml:"segments"`

	// Keys TOML decode dropped, located at parse while their file still known.
	// Typo discard its setting in silence otherwise.
	unknown []*Error
}

// Defaults apply to every segment that override nothing.
type Defaults struct {
	Separator *string `toml:"separator"`
	BarWidth  *int    `toml:"bar_width"`
	Warn      *int    `toml:"warn"`
	High      *int    `toml:"high"`
	Crit      *int    `toml:"crit"`
}

// Line is one rendered row.
//
// No segments = deliberate blank row. All segments empty = row dropped, so
// absent value leave no bare separator. Two distinct cases, so no blank flag.
type Line struct {
	Segments  []string `toml:"segments"`
	Separator *string  `toml:"separator"`
}

func (l Line) Blank() bool { return len(l.Segments) == 0 }

// Segment configure one instance. Every field optional; nil = inherit, which is
// what make per-key merging of project override work.
type Segment struct {
	// Implementation name. Empty = segment's own key, so most segments need no
	// [segments.NAME] block at all.
	Type     *string `toml:"type"`
	Template *string `toml:"template"`

	Warn *int `toml:"warn"`
	High *int `toml:"high"`
	Crit *int `toml:"crit"`

	BarWidth *int `toml:"bar_width"`

	// tokens segment only.
	Scope            *string `toml:"scope"`
	IncludeSidechain *bool   `toml:"include_sidechain"`

	// type = "command" only.
	Command   *string `toml:"command"`
	TimeoutMS *int    `toml:"timeout_ms"`

	// Throttle any segment that shell out.
	CacheMS *int `toml:"cache_ms"`

	// Set where project layer declared command and Load dropped it. Validate
	// then skip "no command": that is consequence of strip Load already
	// reported, and two lines for one mistake read as two mistakes.
	commandStripped bool
}

// Kind name implementation. Nil receiver = segment with no block of its own.
func (s *Segment) Kind(name string) string {
	if s != nil && s.Type != nil && *s.Type != "" {
		return *s.Type
	}
	return name
}

// Used when neither config nor preset say otherwise.
const (
	DefaultSeparator = " │ "
	DefaultBarWidth  = 10
	DefaultWarn      = 50
	DefaultHigh      = 70
	DefaultCrit      = 90
	DefaultScope     = "session"
	DefaultTimeoutMS = 1000
)

// ScopeProject count across whole project, not one session.
const ScopeProject = "project"

// Resolved collapse defaults and overrides into concrete values, so renderers
// chase no nil pointers.
type Resolved struct {
	Kind     string
	Name     string
	Template string

	Warn, High, Crit int
	BarWidth         int

	Scope            string
	IncludeSidechain bool

	Command   string
	TimeoutMS int
	CacheMS   int
}

func (c *Config) Separator(l Line) string {
	if l.Separator != nil {
		return *l.Separator
	}
	if c.Defaults.Separator != nil {
		return *c.Defaults.Separator
	}
	return DefaultSeparator
}

// Resolve produce effective settings for one segment name.
//
// defaultTemplate come from implementation, so reordering line inherit sensible
// text with no template written.
func (c *Config) Resolve(name, defaultTemplate string) Resolved {
	s := c.Segments[name]
	if s == nil {
		s = &Segment{}
	}

	r := Resolved{
		Kind:      s.Kind(name),
		Name:      name,
		Template:  defaultTemplate,
		Warn:      pick(s.Warn, c.Defaults.Warn, DefaultWarn),
		High:      pick(s.High, c.Defaults.High, DefaultHigh),
		Crit:      pick(s.Crit, c.Defaults.Crit, DefaultCrit),
		BarWidth:  pick(s.BarWidth, c.Defaults.BarWidth, DefaultBarWidth),
		Scope:     DefaultScope,
		TimeoutMS: DefaultTimeoutMS,
	}

	if s.Template != nil {
		r.Template = *s.Template
	}
	if s.Scope != nil {
		r.Scope = *s.Scope
	}
	if s.IncludeSidechain != nil {
		r.IncludeSidechain = *s.IncludeSidechain
	}
	if s.Command != nil {
		r.Command = *s.Command
	}
	if s.TimeoutMS != nil {
		r.TimeoutMS = *s.TimeoutMS
	}
	if s.CacheMS != nil {
		r.CacheMS = *s.CacheMS
	}
	return r
}

func pick(segment, global *int, builtin int) int {
	if segment != nil {
		return *segment
	}
	if global != nil {
		return *global
	}
	return builtin
}
