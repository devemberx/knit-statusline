package config

import (
	"embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// Error carry config problem with enough location to act on.
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
	md, err := toml.Decode(string(b), &c)
	if err != nil {
		// BurntSushi report offending line. Surfacing it separate "your config
		// is broken" from fixable message.
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return nil, &Error{File: name, Line: pe.Position.Line, Msg: pe.Message}
		}
		return nil, &Error{File: name, Msg: err.Error()}
	}
	if c.Segments == nil {
		c.Segments = map[string]*Segment{}
	}

	// Decode drop key it cannot map, so bar_witdh = 20 parse clean and do
	// nothing. Recorded here, where file name and source bytes both in hand;
	// Validate report them.
	for _, k := range md.Undecoded() {
		if len(k) == 0 {
			continue
		}
		c.unknown = append(c.unknown, &Error{
			File: name,
			Line: keyLine(b, k[len(k)-1]),
			Msg:  fmt.Sprintf("unknown key %q", k.String()),
		})
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
	// Problems that forced fallback. Rendering continue; row show marker and
	// doctor print these in full.
	Errors []error
}

// Load read user config, then apply project override on top.
//
// Broken file never blank row: drop offending layer, record error, render from
// what remain -- other layer, or builtin preset. Empty home skip user layer,
// else filepath.Join yield relative ".claude/statusline.toml" and read whatever
// directory process happen to sit in.
func Load(home, projectDir string) *LoadResult {
	res := &LoadResult{}

	if home != "" {
		base, err := loadFile(UserPath(home))
		if err != nil {
			res.Errors = append(res.Errors, err)
		} else if base != nil {
			res.Config = base
			res.Sources = append(res.Sources, UserPath(home))
		}
	}

	if res.Config == nil {
		preset, err := Preset(DefaultPreset)
		if err != nil {
			// Presets embedded, so only broken build reach here.
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
			res.Errors = append(res.Errors, stripProjectCommands(override, path)...)
			res.Config = Merge(res.Config, override)
			res.Sources = append(res.Sources, path)
		}
	}

	return res
}

// stripProjectCommands drop command= out of project layer, reporting each one.
//
// Project config is repository content, and command segment run its string
// through a shell. Honouring it there mean cloning a repository and opening it
// execute whatever that file say -- no prompt, first render. Trust boundary sit
// at $HOME. Every other project setting still apply.
func stripProjectCommands(c *Config, path string) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(c.Segments)) {
		seg := c.Segments[name]
		if seg.Command == nil {
			continue
		}
		seg.Command = nil
		errs = append(errs, &Error{
			File: path,
			Msg: fmt.Sprintf(
				"segment %q: command ignored, project config may not run shell commands; move it to %s",
				name, filepath.Join("$HOME", ".claude", "statusline.toml")),
		})
	}
	return errs
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

// Merge apply override on top of base, mutating neither.
//
// Lines replace wholesale, never element-wise: no principled answer to whether
// three-line override extend or replace five-line base, so any project
// declaring lines own whole layout. Segment settings merge per key instead, so
// one segment get tweaked without restating layout.
func Merge(base, override *Config) *Config {
	out := &Config{
		Defaults: base.Defaults,
		Lines:    slices.Clone(base.Lines),
		Segments: map[string]*Segment{},
		unknown:  slices.Concat(base.unknown, override.unknown),
	}

	mergeDefaults(&out.Defaults, override.Defaults)

	if len(override.Lines) > 0 {
		out.Lines = slices.Clone(override.Lines)
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
