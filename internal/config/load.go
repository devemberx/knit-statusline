package config

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed presets/*.toml
var presetFS embed.FS

// DefaultPreset serve users holding no statusline.toml at all.
const DefaultPreset = "reference"

func Preset(name string) (*Config, error) {
	b, err := presetFS.ReadFile("presets/" + name + ".toml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q", name)
	}
	return parse(b, "preset:"+name)
}

func PresetNames() []string {
	entries, err := presetFS.ReadDir("presets")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}
	return names
}

// PresetSource return raw TOML, so installer write it out with comments intact.
func PresetSource(name string) ([]byte, error) {
	return presetFS.ReadFile("presets/" + name + ".toml")
}

// Error carry a config problem with enough location to act on.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// Short render for row itself: space scarce, file name implied.
func (e *Error) Short() string {
	base := filepath.Base(e.File)
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d", base, e.Line)
	}
	return base
}

// ParseBytes decode TOML from no file. name label errors only.
func ParseBytes(b []byte, name string) (*Config, error) { return parse(b, name) }

func parse(b []byte, name string) (*Config, error) {
	var c Config
	if _, err := toml.Decode(string(b), &c); err != nil {
		// BurntSushi report offending line. Surfacing it separate "your config
		// is broken" from a fixable message.
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, &Error{File: name, Line: pe.Position.Line, Msg: pe.Message}
		}
		return nil, &Error{File: name, Msg: err.Error()}
	}
	if c.Segments == nil {
		c.Segments = map[string]*Segment{}
	}
	return &c, nil
}

func UserPath(home string) string {
	return filepath.Join(home, ".claude", "statusline.toml")
}

func ProjectPath(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "statusline.toml")
}

// LoadResult report what Load produced, survived problems included.
type LoadResult struct {
	Config *Config
	// Contributing files, in application order.
	Sources []string
	// Problems that forced a fallback. Rendering continue; row show a marker
	// and doctor print these in full.
	Errors []error
}

// Load read user config, then apply a project override on top.
//
// Broken file never blank row: drop offending layer, record error, render from
// what remain -- other layer, or builtin preset.
func Load(home, projectDir string) *LoadResult {
	res := &LoadResult{}

	base, err := loadFile(UserPath(home))
	if err != nil {
		res.Errors = append(res.Errors, err)
	} else if base != nil {
		res.Config = base
		res.Sources = append(res.Sources, UserPath(home))
	}

	if res.Config == nil {
		preset, err := Preset(DefaultPreset)
		if err != nil {
			// Presets embedded, so only a broken build reach here.
			res.Errors = append(res.Errors, err)
			preset = &Config{Segments: map[string]*Segment{}}
		}
		res.Config = preset
		res.Sources = append(res.Sources, "builtin:"+DefaultPreset)
	}

	if projectDir != "" {
		path := ProjectPath(projectDir)
		override, err := loadFile(path)
		if err != nil {
			res.Errors = append(res.Errors, err)
		} else if override != nil {
			res.Config = Merge(res.Config, override)
			res.Sources = append(res.Sources, path)
		}
	}

	return res
}

// nil, nil when file does not exist.
func loadFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &Error{File: path, Msg: err.Error()}
	}
	return parse(b, path)
}

// Merge apply override on top of base.
//
// Lines replace wholesale, never element-wise: no principled answer to whether
// a three-line override extend or replace a five-line base, so any project
// declaring lines own whole layout. Segment settings merge per key, so one
// segment get tweaked without restating a layout.
func Merge(base, override *Config) *Config {
	out := &Config{
		Defaults: base.Defaults,
		Lines:    base.Lines,
		Segments: map[string]*Segment{},
	}

	mergeDefaults(&out.Defaults, override.Defaults)

	if len(override.Lines) > 0 {
		out.Lines = override.Lines
	}

	for name, seg := range base.Segments {
		copied := *seg
		out.Segments[name] = &copied
	}
	for name, seg := range override.Segments {
		if existing, ok := out.Segments[name]; ok {
			mergeSegment(existing, seg)
			continue
		}
		copied := *seg
		out.Segments[name] = &copied
	}
	return out
}

func mergeDefaults(dst *Defaults, src Defaults) {
	if src.Separator != nil {
		dst.Separator = src.Separator
	}
	if src.BarWidth != nil {
		dst.BarWidth = src.BarWidth
	}
	if src.Warn != nil {
		dst.Warn = src.Warn
	}
	if src.High != nil {
		dst.High = src.High
	}
	if src.Crit != nil {
		dst.Crit = src.Crit
	}
}

func mergeSegment(dst, src *Segment) {
	if src.Type != nil {
		dst.Type = src.Type
	}
	if src.Template != nil {
		dst.Template = src.Template
	}
	if src.Warn != nil {
		dst.Warn = src.Warn
	}
	if src.High != nil {
		dst.High = src.High
	}
	if src.Crit != nil {
		dst.Crit = src.Crit
	}
	if src.BarWidth != nil {
		dst.BarWidth = src.BarWidth
	}
	if src.Scope != nil {
		dst.Scope = src.Scope
	}
	if src.IncludeSidechain != nil {
		dst.IncludeSidechain = src.IncludeSidechain
	}
	if src.Command != nil {
		dst.Command = src.Command
	}
	if src.TimeoutMS != nil {
		dst.TimeoutMS = src.TimeoutMS
	}
	if src.CacheMS != nil {
		dst.CacheMS = src.CacheMS
	}
}

// KnownFunc report whether a kind exist, plus fields its template may use.
type KnownFunc func(kind string) (fields []string, ok bool)

// Validate check config against registered segments.
//
// Report every problem, not first: one doctor pass show whole list. Segment map
// walked in sorted order, else doctor print its findings in a fresh order every
// run and two invocations read as disagreeing.
func Validate(c *Config, source []byte, file string, known KnownFunc) []error {
	var errs []error

	for i, line := range c.Lines {
		for _, name := range line.Segments {
			kind := c.Segments[name].Kind(name)
			if _, ok := known(kind); !ok {
				errs = append(errs, &Error{
					File: file,
					Line: lineOf(source, name),
					Msg:  fmt.Sprintf("line %d references unknown segment %q", i+1, name),
				})
			}
		}
	}

	for _, name := range sortedSegments(c) {
		seg := c.Segments[name]
		kind := seg.Kind(name)
		fields, ok := known(kind)
		if !ok {
			errs = append(errs, &Error{
				File: file,
				Line: lineOf(source, name),
				Msg:  fmt.Sprintf("segment %q has unknown type %q", name, kind),
			})
			continue
		}
		if seg.Template != nil {
			for _, ph := range placeholders(*seg.Template) {
				if !contains(fields, ph) {
					errs = append(errs, &Error{
						File: file,
						Line: lineOf(source, name),
						Msg: fmt.Sprintf("segment %q template references unknown field {%s}; available: %s",
							name, ph, strings.Join(fields, ", ")),
					})
				}
			}
		}
		if kind == "command" && (seg.Command == nil || *seg.Command == "") {
			errs = append(errs, &Error{
				File: file,
				Line: lineOf(source, name),
				Msg:  fmt.Sprintf("segment %q has type \"command\" but no command", name),
			})
		}
		if seg.Scope != nil && *seg.Scope != "session" && *seg.Scope != "project" {
			errs = append(errs, &Error{
				File: file,
				Line: lineOf(source, name),
				Msg:  fmt.Sprintf("segment %q has scope %q; want \"session\" or \"project\"", name, *seg.Scope),
			})
		}
	}

	errs = append(errs, validateThresholds(c, source, file)...)
	return errs
}

func sortedSegments(c *Config) []string {
	names := make([]string, 0, len(c.Segments))
	for name := range c.Segments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateThresholds(c *Config, source []byte, file string) []error {
	var errs []error

	check := func(name string, warn, high, crit *int) {
		ranged := true
		for _, f := range []struct {
			label string
			v     *int
		}{{"warn", warn}, {"high", high}, {"crit", crit}} {
			if f.v != nil && (*f.v < 0 || *f.v > 100) {
				ranged = false
				errs = append(errs, &Error{
					File: file,
					Line: lineOf(source, name),
					Msg:  fmt.Sprintf("%s %s = %d is outside 0-100", name, f.label, *f.v),
				})
			}
		}
		if !ranged {
			return
		}

		// Order checked on effective values, not declared ones. Segment naming
		// crit alone still invert against a warn inherited from defaults, and
		// that pairing is what grade a percentage at render time.
		w := pick(warn, c.Defaults.Warn, DefaultWarn)
		h := pick(high, c.Defaults.High, DefaultHigh)
		cr := pick(crit, c.Defaults.Crit, DefaultCrit)
		if w > h {
			errs = append(errs, &Error{
				File: file,
				Line: lineOf(source, name),
				Msg:  fmt.Sprintf("%s warn = %d is above high = %d", name, w, h),
			})
		}
		if h > cr {
			errs = append(errs, &Error{
				File: file,
				Line: lineOf(source, name),
				Msg:  fmt.Sprintf("%s high = %d is above crit = %d", name, h, cr),
			})
		}
	}

	check("defaults", c.Defaults.Warn, c.Defaults.High, c.Defaults.Crit)
	for _, name := range sortedSegments(c) {
		seg := c.Segments[name]
		check(name, seg.Warn, seg.High, seg.Crit)
	}
	return errs
}

// placeholders pull field names from a template, dropping any ":spec" suffix.
func placeholders(tmpl string) []string {
	var out []string
	for {
		open := strings.IndexByte(tmpl, '{')
		if open < 0 {
			return out
		}
		end := strings.IndexByte(tmpl[open:], '}')
		if end < 0 {
			return out
		}
		inner := tmpl[open+1 : open+end]
		if i := strings.IndexByte(inner, ':'); i >= 0 {
			inner = inner[:i]
		}
		if inner != "" {
			out = append(out, inner)
		}
		tmpl = tmpl[open+end+1:]
	}
}

// lineOf find where tok is declared, so a semantic error point roughly right.
// Zero = unknown; error print without a line rather than invent one.
//
// Declaration only, never loose substring. Comment prose and neighbouring keys
// mention segment names freely -- `project_dir` hold "dir", and a preset header
// naming a segment sit dozens of lines above its block -- so a contains-anywhere
// scan report line 1 for a fault on line 7 and read authoritative doing it.
func lineOf(source []byte, tok string) int {
	if len(source) == 0 || tok == "" {
		return 0
	}
	quoted := []byte(`"` + tok + `"`)
	for i, raw := range bytes.Split(source, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if bytes.Contains(line, quoted) || tableKey(line) == tok {
			return i + 1
		}
	}
	return 0
}

// tableKey return last path component of a table header: [defaults] give
// "defaults", [segments.dir] give "dir", [segments."limit.5h"] give "limit.5h".
//
// Empty for anything else, [[lines]] included -- array-of-tables declare rows,
// never a segment, and a segment named "lines" would otherwise match every row
// header present.
func tableKey(line []byte) string {
	if len(line) < 2 || line[0] != '[' || line[1] == '[' {
		return ""
	}
	end := bytes.IndexByte(line, ']')
	if end < 0 {
		return ""
	}
	inner := line[1:end]

	// Quoted component hold dots of its own, so it win before any dot split.
	if i := bytes.IndexByte(inner, '"'); i >= 0 {
		if j := bytes.LastIndexByte(inner, '"'); j > i {
			return string(inner[i+1 : j])
		}
		return ""
	}
	if i := bytes.LastIndexByte(inner, '.'); i >= 0 {
		inner = inner[i+1:]
	}
	return string(inner)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
