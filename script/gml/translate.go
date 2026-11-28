package gml

import "unicode"

// Display text through the app's catalogue (SetTranslator), with no
// markup in the pages: the values of the attributes a user reads — text,
// ToolTip.text, placeholderText, title — go through the translator as they
// are shown. A literal is translated once, when the page is parsed (the
// catalogue is installed before the pages are loaded); a computed value
// ("Props (" + n + ")") as it is evaluated, so the catalogue sees the whole
// sentence and can match it against a template ("Props (%1)"). Only the
// final value is looked up — never a string inside the expression, which
// may be compared as data. A text field's text is what the user typed and
// is left alone.

// parseForDisplay parses a page or component that will be built and shown:
// Parse, then its display text through the catalogue. (Tools that only
// read or rewrite the source — the formatter, the tree view — use Parse.)
func parseForDisplay(src string) (*node, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	translateDisplayText(root)
	return root, nil
}

// displayAttrs are the attributes holding text a user reads.
var displayAttrs = map[string]bool{
	"text": true, "ToolTip.text": true, "placeholderText": true, "title": true,
}

// editableText are the element types whose text is the user's own.
var editableText = map[string]bool{
	"TextField": true, "TextInput": true, "TextArea": true, "TextEdit": true,
}

// trExpr is a display value passed through the translator when evaluated.
type trExpr struct{ inner expr }

func (t *trExpr) eval(env *Env) (any, error) {
	v, err := t.inner.eval(env)
	if err != nil {
		return v, err
	}
	if s, ok := v.(string); ok {
		if fn := translator.Load(); fn != nil && s != "" {
			return (*fn)(s), nil
		}
	}
	return v, nil
}

// translateDisplayText walks a parsed tree, translating literal display
// text now and wrapping computed display text in a trExpr.
func translateDisplayText(n *node) {
	if n == nil {
		return
	}
	for i := range n.attrs {
		a := &n.attrs[i]
		if a.expr == nil || !displayAttrs[a.name] || (a.name == "text" && editableText[n.typeName]) {
			continue
		}
		switch e := a.expr.(type) {
		case *litExpr:
			if s, ok := e.v.(string); ok && s != "" {
				if fn := translator.Load(); fn != nil {
					e.v = (*fn)(s)
				}
			}
		case *trExpr:
		default:
			a.expr = &trExpr{inner: e}
		}
	}
	for i := range n.attrs {
		translateDisplayText(n.attrs[i].elem)
	}
	for _, c := range n.children {
		translateDisplayText(c)
	}
}

// DisplayTexts lists what a page shows a user, as catalogue keys: every
// literal in a display attribute (see displayAttrs), both branches of a
// conditional, and a string built by concatenation (or a template
// literal) as a template — its computed parts numbered %1, %2… left to
// right, the form the catalogue matches a shown value against
// ("Props (" + n + ")" → "Props (%1)"). Strings in a `model:` list that
// read as text (a space, or a capital first letter) count too: a delegate
// shows them through modelData. For an app's catalogue extractor.
func DisplayTexts(src string) ([]string, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && hasLetter(s) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	var text func(e expr)
	text = func(e expr) {
		switch e := e.(type) {
		case *litExpr:
			if s, ok := e.v.(string); ok {
				add(s)
			}
		case *condExpr:
			text(e.a)
			text(e.b)
		case *binExpr:
			if e.op == "+" {
				if t, ok := concatTemplate(e); ok {
					add(t)
				}
			}
		case *templateExpr:
			var b []byte
			for i, p := range e.parts {
				b = append(b, p...)
				if i < len(e.exprs) {
					b = append(b, '%')
					b = append(b, []byte(itoaSmall(i+1))...)
				}
			}
			add(string(b))
		}
	}
	var model func(e expr)
	model = func(e expr) {
		switch e := e.(type) {
		case *listExpr:
			for _, it := range e.items {
				model(it)
			}
		case *litExpr:
			if s, ok := e.v.(string); ok && readsAsText(s) {
				add(s)
			}
		case *binExpr, *condExpr, *templateExpr:
			text(e)
		}
	}
	var walk func(n *node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		for _, a := range n.attrs {
			switch {
			case a.expr == nil:
			case displayAttrs[a.name] && !(a.name == "text" && editableText[n.typeName]):
				text(a.expr)
			case a.name == "model":
				model(a.expr)
			}
			walk(a.elem)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	return out, nil
}

// concatTemplate turns a + chain holding at least one string literal into
// a template: literals kept, anything else %1, %2… in order.
func concatTemplate(e *binExpr) (string, bool) {
	var parts []expr
	var flat func(x expr)
	flat = func(x expr) {
		if b, ok := x.(*binExpr); ok && b.op == "+" {
			flat(b.l)
			flat(b.r)
			return
		}
		parts = append(parts, x)
	}
	flat(e)
	var b []byte
	n, lit := 0, false
	for _, p := range parts {
		if l, ok := p.(*litExpr); ok {
			if s, ok := l.v.(string); ok {
				b = append(b, s...)
				lit = true
				continue
			}
		}
		n++
		b = append(b, '%')
		b = append(b, itoaSmall(n)...)
	}
	return string(b), lit
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// readsAsText tells display text from an id in a model list: it has a
// space or starts with a capital.
func readsAsText(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func itoaSmall(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoaSmall(i/10) + string(rune('0'+i%10))
}
