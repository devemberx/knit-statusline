package render

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Field is one interpolatable value. Color sit beside text, never baked in, so
// alignment measure visible width: padding computed over a string carrying
// escape codes come out wrong by however long those codes run.
type Field struct {
	Text  string
	Color Color
}

func Plain(s string) Field { return Field{Text: s} }

func Colored(s string, c Color) Field { return Field{Text: s, Color: c} }

// Fields is value set a segment expose to its template.
type Fields map[string]Field

// Expand substitute {name} and {name:spec} placeholders in tmpl.
//
// Literal text take base, each field keep its own: reference layout keep labels
// muted and let only numbers carry severity. Unknown placeholder expand to
// nothing -- Validate report those up front, so one reaching here mean config
// changed underneath, and dropping it beat printing a raw brace.
func (p Palette) Expand(tmpl string, fields Fields, base Color) string {
	var b strings.Builder
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() > 0 {
			b.WriteString(p.Wrap(literal.String(), base))
			literal.Reset()
		}
	}

	for {
		open := strings.IndexByte(tmpl, '{')
		if open < 0 {
			literal.WriteString(tmpl)
			break
		}
		closing := strings.IndexByte(tmpl[open:], '}')
		if closing < 0 {
			// Unterminated brace is literal text, not worth blanking row.
			literal.WriteString(tmpl)
			break
		}

		literal.WriteString(tmpl[:open])
		name, spec := splitSpec(tmpl[open+1 : open+closing])
		tmpl = tmpl[open+closing+1:]

		f, ok := fields[name]
		if !ok {
			continue
		}
		flushLiteral()
		b.WriteString(p.Wrap(pad(f.Text, spec), f.Color))
	}

	flushLiteral()
	return b.String()
}

// Split "pct:>3" into name and alignment spec.
func splitSpec(inner string) (name, spec string) {
	if i := strings.IndexByte(inner, ':'); i >= 0 {
		return inner[:i], inner[i+1:]
	}
	return inner, ""
}

// pad apply alignment spec: ">N" right, "<N" left, bare "N" right -- what
// whoever write {pct:3} mean for a number.
//
// Width counted in runes, so a bar of ● and ○ pad as it look.
func pad(s, spec string) string {
	if spec == "" {
		return s
	}

	align := byte('>')
	if spec[0] == '>' || spec[0] == '<' {
		align = spec[0]
		spec = spec[1:]
	}

	width, err := strconv.Atoi(spec)
	if err != nil || width <= 0 {
		return s
	}
	gap := width - utf8.RuneCountInString(s)
	if gap <= 0 {
		return s
	}
	if align == '<' {
		return s + strings.Repeat(" ", gap)
	}
	return strings.Repeat(" ", gap) + s
}
