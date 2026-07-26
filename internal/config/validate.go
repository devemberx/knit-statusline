package config

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// KnownFunc report whether kind exist, plus fields its template may use.
type KnownFunc func(kind string) (fields []string, ok bool)

// Origin locate file and row that declared token. Row 0 = unknown, so error
// print without one rather than invent it.
type Origin func(tok string) (file string, line int)

// FileOrigin locate token inside one file's bytes.
func FileOrigin(file string, source []byte) Origin {
	return func(tok string) (string, int) { return file, lineOf(source, tok) }
}

// problem build error located through origin.
func problem(origin Origin, tok, msg string) *Error {
	if origin == nil {
		return &Error{Msg: msg}
	}
	file, line := origin(tok)
	return &Error{File: file, Line: line, Msg: msg}
}

// Validate check config against registered segments.
//
// Report every problem, not first: one doctor pass show whole list. Segment map
// walked sorted, else two runs print findings in different order and read as
// disagreeing about config that never changed.
//
// origin locate what this pass raise. Keys dropped at decode already carry their
// own, so merged config still name right file for each.
func Validate(c *Config, origin Origin, known KnownFunc) []error {
	var errs []error

	for _, e := range c.unknown {
		errs = append(errs, e)
	}

	for i, line := range c.Lines {
		for _, name := range line.Segments {
			kind := c.Segments[name].Kind(name)
			if _, ok := known(kind); !ok {
				errs = append(errs, problem(origin, name,
					fmt.Sprintf("line %d references unknown segment %q", i+1, name)))
			}
		}
	}

	for _, name := range sortedSegments(c) {
		seg := c.Segments[name]
		kind := seg.Kind(name)
		fields, ok := known(kind)
		if !ok {
			errs = append(errs, problem(origin, name,
				fmt.Sprintf("segment %q has unknown type %q", name, kind)))
			continue
		}
		if seg.Template != nil {
			for _, ph := range placeholders(*seg.Template) {
				if !slices.Contains(fields, ph) {
					errs = append(errs, problem(origin, name,
						fmt.Sprintf("segment %q template references unknown field {%s}; available: %s",
							name, ph, strings.Join(fields, ", "))))
				}
			}
		}
		if kind == "command" && !seg.commandStripped && (seg.Command == nil || *seg.Command == "") {
			errs = append(errs, problem(origin, name,
				fmt.Sprintf("segment %q has type \"command\" but no command", name)))
		}
		if seg.Scope != nil && *seg.Scope != DefaultScope && *seg.Scope != ScopeProject {
			errs = append(errs, problem(origin, name,
				fmt.Sprintf("segment %q has scope %q; want %q or %q",
					name, *seg.Scope, DefaultScope, ScopeProject)))
		}
	}

	errs = append(errs, validateThresholds(c, origin)...)
	return errs
}

func sortedSegments(c *Config) []string {
	names := make([]string, 0, len(c.Segments))
	for name := range c.Segments {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func validateThresholds(c *Config, origin Origin) []error {
	var errs []error

	check := func(name string, warn, high, crit *int) {
		ranged := true
		for _, f := range []struct {
			label string
			v     *int
		}{{"warn", warn}, {"high", high}, {"crit", crit}} {
			if f.v != nil && (*f.v < 0 || *f.v > 100) {
				ranged = false
				errs = append(errs, problem(origin, name,
					fmt.Sprintf("%s %s = %d is outside 0-100", name, f.label, *f.v)))
			}
		}
		// Out-of-range value suppress its own order check: reporting
		// "warn = 150 is above high = 70" beside it bury which number to fix.
		if !ranged {
			return
		}

		// Order read on effective values, not declared ones. Segment naming
		// crit alone still invert against warn inherited from [defaults], and
		// that pairing is what grade percentage at render time.
		w := pick(warn, c.Defaults.Warn, DefaultWarn)
		h := pick(high, c.Defaults.High, DefaultHigh)
		cr := pick(crit, c.Defaults.Crit, DefaultCrit)

		// Pair with both sides inherited belong to [defaults], reported once
		// under that name. Repeating per segment point every finding at block
		// holding no threshold at all.
		if w > h && (warn != nil || high != nil) {
			errs = append(errs, problem(origin, name,
				fmt.Sprintf("%s warn = %d is above high = %d", name, w, h)))
		}
		if h > cr && (high != nil || crit != nil) {
			errs = append(errs, problem(origin, name,
				fmt.Sprintf("%s high = %d is above crit = %d", name, h, cr)))
		}
	}

	check("defaults", c.Defaults.Warn, c.Defaults.High, c.Defaults.Crit)
	for _, name := range sortedSegments(c) {
		seg := c.Segments[name]
		check(name, seg.Warn, seg.High, seg.Crit)
	}
	return errs
}

// placeholders pull field names from template, dropping any ":spec" suffix.
//
// Name holding "{" mean brace never closed -- "{{pct}}" read inner as "{pct".
// Skipped, not reported: render.Expand drop that placeholder too, so no setting
// is lost and error would name field nobody wrote.
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
		tmpl = tmpl[open+end+1:]

		if i := strings.IndexByte(inner, ':'); i >= 0 {
			inner = inner[:i]
		}
		if inner != "" && !strings.ContainsRune(inner, '{') {
			out = append(out, inner)
		}
	}
}

// lineOf find where tok sit. Zero = unknown; error print without line rather
// than invent one.
//
// Table headers sweep first, per headerLine. Comments skipped throughout --
// prose name segments freely, and preset header naming one sit dozens of lines
// above its block, so contains-anywhere scan report line 1 for fault on line 7
// and read authoritative doing it.
func lineOf(source []byte, tok string) int {
	if n := headerLine(source, tok); n > 0 {
		return n
	}
	return mentionLine(source, tok)
}

// headerLine find [segments.tok]: block that declare tok's settings.
//
// Beat mentionLine, since segments = ["dir"] merely name dir while
// [segments.dir] hold what it was configured with. One file carry both, and so
// do two layers -- LoadResult.Origin sweep this across all of them first.
func headerLine(source []byte, tok string) int {
	rows, ok := rowsOf(source, tok)
	if !ok {
		return 0
	}
	return scan(rows, func(line []byte) bool { return tableKey(line) == tok })
}

// mentionLine find tok assigned, or named inside quotes.
func mentionLine(source []byte, tok string) int {
	rows, ok := rowsOf(source, tok)
	if !ok {
		return 0
	}
	quoted := []byte(`"` + tok + `"`)
	return scan(rows, func(line []byte) bool {
		return assignedKey(line) == tok || bytes.Contains(line, quoted)
	})
}

func rowsOf(source []byte, tok string) ([][]byte, bool) {
	if len(source) == 0 || tok == "" {
		return nil, false
	}
	return bytes.Split(source, []byte("\n")), true
}

// keyLine locate assignment of bare key, so key decode dropped point at its own
// row rather than at its table header.
func keyLine(source []byte, key string) int {
	return scan(bytes.Split(source, []byte("\n")),
		func(line []byte) bool { return assignedKey(line) == key })
}

// scan return 1-based row of first match, skipping blanks and comments.
func scan(rows [][]byte, match func([]byte) bool) int {
	for i, raw := range rows {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if match(line) {
			return i + 1
		}
	}
	return 0
}

// assignedKey return name left of "=", or empty when line hold no assignment.
func assignedKey(line []byte) string {
	eq := bytes.IndexByte(line, '=')
	if eq < 0 {
		return ""
	}
	return string(bytes.Trim(bytes.TrimSpace(line[:eq]), `"`))
}

// tableKey return last path component of table header: [defaults] give
// "defaults", [segments.dir] give "dir", [segments."limit.5h"] give "limit.5h".
//
// Empty for anything else, [[lines]] included -- array-of-tables declare rows,
// never segment, and segment named "lines" would otherwise match every row
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
