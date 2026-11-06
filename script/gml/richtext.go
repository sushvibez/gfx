package gml

// textFormat: RichText — the practical subset of Qt's HTML-ish rich text:
// inline style tags (<b>/<strong>, <i>/<em>, <u>, <s>/<strike>/<del>,
// <font color="...">), structure tags that break lines (<br>, <p>, <h1>-<h4>,
// <div>, <li> with a bullet), and the common entities. Unknown tags are
// ignored (their content renders), matching Qt's lenient parser. Each line
// renders as a Row of styled Text runs.

import (
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// richSpan is one run of uniformly-styled text.
type richSpan struct {
	text          string
	bold, italic  bool
	uline, strike bool
	color         string  // "" = inherit the Text element color
	scale         float32 // font-size multiplier (headings); 0 = 1
}

// richStyle is the inheritable state carried by the tag stack.
type richStyle struct {
	bold, italic, uline, strike bool
	color                       string
	scale                       float32
}

// parseRichText splits s into lines of styled spans.
func parseRichText(s string) [][]richSpan {
	var lines [][]richSpan
	var line []richSpan
	stack := []richStyle{{}}
	cur := func() richStyle { return stack[len(stack)-1] }
	flushLine := func() {
		lines = append(lines, line)
		line = nil
	}
	emit := func(text string) {
		if text == "" {
			return
		}
		st := cur()
		line = append(line, richSpan{text: text, bold: st.bold, italic: st.italic,
			uline: st.uline, strike: st.strike, color: st.color, scale: st.scale})
	}

	for i := 0; i < len(s); {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			emit(decodeEntities(s[i:]))
			break
		}
		emit(decodeEntities(s[i : i+lt]))
		i += lt
		gt := strings.IndexByte(s[i:], '>')
		if gt < 0 {
			emit(decodeEntities(s[i:])) // stray '<': render literally
			break
		}
		tag := s[i+1 : i+gt]
		i += gt + 1

		closing := strings.HasPrefix(tag, "/")
		name := strings.ToLower(strings.TrimSuffix(strings.Fields(strings.TrimPrefix(tag, "/"))[0], "/"))
		switch name {
		case "b", "strong", "i", "em", "u", "s", "strike", "del", "font",
			"h1", "h2", "h3", "h4":
			if closing {
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
				if name[0] == 'h' && len(name) == 2 {
					flushLine() // headings end their line
				}
				continue
			}
			st := cur()
			switch name {
			case "b", "strong":
				st.bold = true
			case "i", "em":
				st.italic = true
			case "u":
				st.uline = true
			case "s", "strike", "del":
				st.strike = true
			case "font":
				if c := attrValue(tag, "color"); c != "" {
					st.color = c
				}
			case "h1":
				st.bold, st.scale = true, 2.0
			case "h2":
				st.bold, st.scale = true, 1.6
			case "h3":
				st.bold, st.scale = true, 1.3
			case "h4":
				st.bold, st.scale = true, 1.1
			}
			if name[0] == 'h' && len(name) == 2 && len(line) > 0 {
				flushLine() // headings start on their own line
			}
			stack = append(stack, st)
		case "br":
			flushLine()
		case "p", "div":
			if closing || len(line) > 0 {
				flushLine()
			}
		case "li":
			if closing {
				flushLine()
			} else {
				if len(line) > 0 {
					flushLine()
				}
				emit("• ")
			}
		default:
			// html/body/span/a/ul/ol/... — content renders, tag ignored.
		}
	}
	if len(line) > 0 {
		flushLine()
	}
	// Drop a single trailing empty line from a closing block tag.
	for len(lines) > 1 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// attrValue pulls attr="value" (or attr='value') out of a raw tag string.
func attrValue(tag, attr string) string {
	low := strings.ToLower(tag)
	idx := strings.Index(low, attr+"=")
	if idx < 0 {
		return ""
	}
	rest := tag[idx+len(attr)+1:]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		q := rest[0]
		if end := strings.IndexByte(rest[1:], q); end >= 0 {
			return rest[1 : 1+end]
		}
		return ""
	}
	if end := strings.IndexAny(rest, " \t/>"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// decodeEntities handles the entities Qt rich text actually meets.
func decodeEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&apos;", "'", "&nbsp;", " ")
	return r.Replace(s)
}

// buildRichText renders parsed rich text as a Column of Rows of styled runs.
// base is the Text element's own color (spans without a color inherit it);
// sizePx its font size (0 = default).
func buildRichText(s string, base ui.RGBA, sizePx float64) (ui.Element, error) {
	lines := parseRichText(s)
	col := ui.Div().Col().Gap(2)
	for _, spans := range lines {
		if len(spans) == 0 { // blank line (e.g. <br/><br/>): half-line spacer
			col.Children(ui.Div().H(float32(max(sizePx, 8))))
			continue
		}
		row := ui.Div().Row().Items(ui.AlignEnd) // baseline-ish: bottom-align mixed sizes
		for _, sp := range spans {
			c := base
			if sp.color != "" {
				if pc, err := parseColor(sp.color); err == nil {
					c = pc
				}
			}
			t := ui.Text(sp.text, c)
			size := sizePx
			if size == 0 {
				size = 14 // scale needs a concrete base
			}
			if sp.scale > 0 && sp.scale != 1 {
				t.SizePx(size * float64(sp.scale))
			} else if sizePx > 0 {
				t.SizePx(sizePx)
			}
			if sp.bold {
				t.Bold()
			}
			if sp.italic {
				t.Italic()
			}
			if sp.uline {
				t.Underline()
			}
			if sp.strike {
				t.StrikeThrough()
			}
			row.Children(t)
		}
		col.Children(row)
	}
	return col, nil
}
