package render

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Field is one interpolatable value. Color sit beside text where one color
// cover whole field. Fields needing two colors at once -- Bar, dir's {git} --
// bake escapes into Text and rely on pad skipping them.
type Field struct {
	Text  string
	Color Color
}

func Plain(s string) Field { return Field{Text: s} }

func Colored(s string, c Color) Field { return Field{Text: s, Color: c} }

// Fields is value set a segment expose to its template.
type Fields map[string]Field

// Expand substitute {name} and {name:spec} placeholder in tmpl.
//
// Literal text take base, each field keep its own: reference layout keep labels
// muted and let only numbers carry severity. Unknown placeholder expand to
// nothing -- Validate report those up front, so one reaching here mean config
// changed underneath, and dropping it beat printing raw brace.
//
// Brace never escape: "{{n}}" read inner as name "{n", drop it, leave stray
// "}". Test pin that, design did not pick it -- escape syntax need config
// support first.
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
	gap := width - visibleWidth(s)
	if gap <= 0 {
		return s
	}
	if align == '<' {
		return s + strings.Repeat(" ", gap)
	}
	return strings.Repeat(" ", gap) + s
}

// visibleWidth count printed runes, skipping escape sequences. Bar and dir's
// {git} bake color into Text, and counting those escapes as width make
// {bar:>12} pad by nothing at all.
func visibleWidth(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i += escapeLen(s[i:])
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

// escapeLen measure escape sequence at start of s, 1 for lone ESC.
//
// Palette emit "\033[...m" alone, but unrecognised sequence must still advance,
// else scan stall on its own ESC forever.
func escapeLen(s string) int {
	if len(s) < 2 || s[1] != '[' {
		return 1
	}
	// CSI run over parameter and intermediate bytes, then one final byte in
	// 0x40..0x7e closing it.
	for i := 2; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
	}
	return len(s)
}
