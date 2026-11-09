package gml

import (
	"fmt"
	"github.com/sushvibez/gfx/core"
	"strconv"
	"strings"
)

// AST.

type node struct {
	typeName string
	line     int
	// ord numbers the elements that start on the same source line (0 for
	// the first), so `Row { Rectangle {} Label {} }` written on one line
	// gives its three items three keys (elemKey).
	ord      int
	attrs    []attr
	children []*node
	props    []propDecl   // `property int count: 0` declarations
	funcs    []funcDecl   // `function name(params) { ... }` declarations
	signals  []signalDecl // `signal name(params)` declarations
	onTarget string       // `Type on <prop>` clause (Behavior on x, NumberAnimation on y)
	// owner is the element this node was declared inside, filled in as the
	// tree is parsed. A non-visual child sometimes has to know it: an
	// animation written `SequentialAnimation on opacity` drives the OWNER's
	// property, which for a built-in one is written as an attribute
	// override on that element.
	owner *node
	// ekey caches elemKey(n) — the registry/cache key derived from the id
	// or the line — which is asked for several times per node per build.
	ekey string
	// kprefix/kfull cache docState.key for the row prefix last asked.
	kprefix, kfull string
	// imposedWidth marks an instance whose USE SITE gives it a width —
	// anchored to both sides, or a layout told to stretch it. A styled
	// control is built from the style file's own node, which carries none
	// of the use site's anchors or Layout.* attrs, and a wrapping Label
	// has to know: QML breaks its lines only once a width is imposed.
	imposedWidth bool
	// noSelfReads marks a node whose whole subtree never reads its own
	// scope by bare name (see selfreads.go): shadowSelfGeometry is skipped
	// for it. Zero (false) keeps the shadow, so a node made at run time is
	// safe by default.
	noSelfReads bool
	// ovNode caches the node merged with its attribute overrides (see
	// withAttrOverrides); stripped caches stripAnchorAttrs' copy. Each is
	// valid only for the attrs slice it was derived from (attrsFrom): a
	// shallow copy of the node with different attributes (merged,
	// visible-stripped) inherits the fields but not their validity.
	ovNode, stripped  *node
	ovFrom, stripFrom attrsID
	// kid caches docState.keyID for the document and prefix it was asked
	// under (see keyid.go).
	kid       keyID
	kidPrefix string
	kidDoc    *docState
	// instTok is a component instance's use-site token (see
	// docState.instToken); set on the instance copy only.
	instTok   *core.Property[uint64]
	imports   []string          // file-header import module names (root node only)
	nsAliases map[string]string // import namespace aliases (root node only)
}

// funcDecl is a QML/JS function declared on an element.
type funcDecl struct {
	name     string
	params   []string
	defaults []expr // per-param default value (nil = required), ES6 style
	body     []stmt
	line     int
}

// signalDecl is a QML signal declared on an element; its `on<Name>` handler
// (on the same element) is the body run when the signal is emitted.
type signalDecl struct {
	name   string
	params []string
	line   int
}

// propDecl is a QML-style property declaration owned by the document.
type propDecl struct {
	typ      string // int, real/double, bool, string
	name     string
	def      expr
	required bool // `required property ...`: a delegate slot the view fills from the model context
	isDflt   bool // `default property ...`: where a use site's children go
	line     int
}

type attr struct {
	name string
	line int
	expr expr   // value attribute
	stmt []stmt // handler attribute (on*)
	elem *node  // element-valued attribute, e.g. `gradient: Gradient { ... }`
	// fromUse marks an attribute a USE SITE handed to a component instance
	// (`Tumbler { delegate: Text { text: root.pad2(...) } }`). Its bindings
	// belong to the use site's scope, not the component file's — which
	// matters where the two use the same id, as `root` almost always is.
	fromUse bool
}

// Statement AST (exec methods live in stmt.go).

// Expressions.

type expr interface{ eval(env *Env) (any, error) }

type litExpr struct{ v any }

type identExpr struct {
	name string
	line int
}

type memberExpr struct {
	base     expr
	name     string
	line     int
	optional bool // ?. — yields nil instead of erroring when base is nil
}

type unaryExpr struct {
	op string
	x  expr
}

type binExpr struct {
	op   string
	l, r expr
	line int
}

type condExpr struct{ c, a, b expr }

// callExpr applies args to a callee expression. The callee is an identExpr
// for a bare function (animate(), an env-registered func) or a memberExpr for
// a method / namespace call (console.log(), Math.max()).
type callExpr struct {
	callee expr
	args   []expr
	line   int
	// spread: an argument is `...expr` (Math.max(...xs)); eval flattens it.
	spread bool
}

type listExpr struct{ items []expr }

// spreadExpr is `...expr` inside an array literal; flattened by listExpr.eval.
type spreadExpr struct{ inner expr }

// updateExpr is a JS assignment or increment used as an EXPRESSION: `a = b`
// and `a += b` yield the new value, `a++` / `a--` (post) yield the old one.
type updateExpr struct {
	target expr
	op     string // "=", "+=", ... or "++" / "--"
	value  expr   // nil for ++/--
	post   bool
	line   int
}

func (x *spreadExpr) eval(env *Env) (any, error) { return x.inner.eval(env) }

// objExpr is a JS object literal { key: expr, ... }; evaluates to map[string]any.
type objExpr struct {
	keys []string
	vals []expr
}

// templateExpr is a `backtick ${expr}` string: literal parts interleaved with
// embedded expressions, concatenated at eval time.
type templateExpr struct {
	parts []string // len == len(exprs)+1
	exprs []expr
}

// funcExpr is an arrow function: (params) => body. Evaluates to a *userFunc
// closing over the current env (so callbacks see outer scope).
type funcExpr struct {
	params   []string
	defaults []expr // per-param default (nil = none)
	body     []stmt
}

// blockExpr is a QML block binding: `text: { if (x) return "a"; ... }` —
// a statement block evaluated as an expression via its return value.
type blockExpr struct {
	body []stmt
}

// indexExpr is `recv[index]` — element access on an array, object, or string.
type indexExpr struct {
	recv  expr
	index expr
	line  int
}

// elemExpr wraps a nested element used as a list item, e.g. the State /
// Transition objects in `states: [ State {...} ]`. It evaluates to the *node
// itself; the states machinery interprets it.
type elemExpr struct{ n *node }

// Parser: recursive descent over the token stream. Newlines and ';'
// terminate attributes; inside (), [] they are insignificant.

type parser struct {
	toks      []token
	lineSeen  map[int]int       // elements started per source line (node.ord)
	imports   []string          // module names from file-header import lines
	nsAliases map[string]string // `import M as A` -> A: M
	pos       int
}

// Parse parses a gml document: exactly one root element.
func Parse(src string) (*node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	p.skipNewlines()
	p.skipHeaders()
	root, err := p.element()
	if err != nil {
		return nil, err
	}
	root.imports = p.imports
	root.nsAliases = p.nsAliases
	markSelfReads(root)
	p.skipNewlines()
	if p.cur().kind != tokEOF {
		return nil, fmt.Errorf("gml:%d: unexpected %q after root element", p.cur().line, p.cur().text)
	}
	return root, nil
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) skipNewlines() {
	for p.cur().kind == tokNewline || (p.cur().kind == tokPunct && p.cur().text == ";") {
		p.pos++
	}
}

// skipHeaders consumes QML file-header statements: `import <module>
// [<version>] [as <id>]` and `pragma <name>`. Import module names are
// recorded (module resolution loads qmldir singletons and component
// dirs); the rest of each line is dropped.
func (p *parser) skipHeaders() {
	for p.cur().kind == tokIdent && (p.cur().text == "import" || p.cur().text == "pragma") {
		if p.cur().text == "import" && p.toks[p.pos+1].kind == tokIdent {
			name := p.toks[p.pos+1].text
			j := p.pos + 2
			for p.toks[j].kind == tokPunct && p.toks[j].text == "." && p.toks[j+1].kind == tokIdent {
				name += "." + p.toks[j+1].text
				j += 2
			}
			p.imports = append(p.imports, name)
			// `import <module> [ver] as Alias` — record the namespace.
			for ; p.toks[j].kind != tokNewline && p.toks[j].kind != tokEOF; j++ {
				if p.toks[j].kind == tokIdent && p.toks[j].text == "as" &&
					p.toks[j+1].kind == tokIdent {
					if p.nsAliases == nil {
						p.nsAliases = map[string]string{}
					}
					p.nsAliases[p.toks[j+1].text] = name
					break
				}
			}
		}
		for p.cur().kind != tokNewline && p.cur().kind != tokEOF {
			p.pos++
		}
		p.skipNewlines()
	}
}

func (p *parser) expect(text string) error {
	if p.cur().kind == tokPunct && p.cur().text == text {
		p.pos++
		return nil
	}
	return fmt.Errorf("gml:%d: expected %q, got %q", p.cur().line, text, p.cur().text)
}

// element := Ident '{' (attr | element)* '}'
func (p *parser) element() (*node, error) {
	if p.cur().kind != tokIdent {
		return nil, fmt.Errorf("gml:%d: expected element name, got %q", p.cur().line, p.cur().text)
	}
	n := &node{typeName: p.cur().text, line: p.cur().line}
	if p.lineSeen == nil {
		p.lineSeen = map[int]int{}
	}
	n.ord = p.lineSeen[n.line]
	p.lineSeen[n.line]++
	p.pos++
	// Namespaced type: `T.Button { ... }` (import ... as T).
	for p.cur().kind == tokPunct && p.cur().text == "." && p.toks[p.pos+1].kind == tokIdent {
		n.typeName += "." + p.toks[p.pos+1].text
		p.pos += 2
	}
	// Optional `on <property>` clause: `Behavior on opacity { ... }`,
	// `NumberAnimation on x { ... }`. Records the target property.
	if p.cur().kind == tokIdent && p.cur().text == "on" && p.toks[p.pos+1].kind == tokIdent {
		p.pos++ // "on"
		n.onTarget = p.next().text
		// allow dotted targets (e.g. `on border.width`)
		for p.cur().kind == tokPunct && p.cur().text == "." && p.toks[p.pos+1].kind == tokIdent {
			n.onTarget += "." + p.toks[p.pos+1].text
			p.pos += 2
		}
	}
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	for {
		p.skipNewlines()
		if p.cur().kind == tokPunct && p.cur().text == "}" {
			p.pos++
			// QML group-property blocks (`anchors { fill: parent }`) parse
			// as children; fold them into dotted attributes here so every
			// consumer — including the PARENT's anchor pass — sees the same
			// shape as the flat form.
			return foldGroupProperties(n), nil
		}
		if p.cur().kind != tokIdent {
			return nil, fmt.Errorf("gml:%d: expected attribute or element, got %q", p.cur().line, p.cur().text)
		}
		// `[readonly|default|required] property <type> <name>: <default>` —
		// a QML property declaration.
		if p.isPropertyStart() {
			d, err := p.propDecl()
			if err != nil {
				return nil, err
			}
			n.props = append(n.props, d)
			continue
		}
		// `function <name>(<params>) { <body> }` — QML/JS method.
		if p.cur().text == "function" && p.toks[p.pos+1].kind == tokIdent {
			f, err := p.funcDecl()
			if err != nil {
				return nil, err
			}
			n.funcs = append(n.funcs, f)
			continue
		}
		// `component <Name>: <Type> { ... }` — QML inline component.
		// Synthesized into a Component { id: Name; <Type>{...} } node so the
		// existing component registry (Loader, type resolution) serves it.
		if p.cur().text == "component" && p.toks[p.pos+1].kind == tokIdent &&
			p.toks[p.pos+2].kind == tokPunct && p.toks[p.pos+2].text == ":" {
			line := p.cur().line
			p.pos++ // "component"
			name := p.next().text
			p.pos++ // ":"
			p.skipNewlines()
			inner, err := p.element()
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, &node{
				typeName: "Component",
				line:     line,
				attrs:    []attr{{name: "id", expr: &identExpr{name: name, line: line}, line: line}},
				children: []*node{inner},
			})
			continue
		}
		// `signal <name>(<typed params>)` — QML signal.
		if p.cur().text == "signal" && p.toks[p.pos+1].kind == tokIdent {
			s, err := p.signalDecl()
			if err != nil {
				return nil, err
			}
			n.signals = append(n.signals, s)
			continue
		}
		// Lookahead: Ident ':' = attribute, Ident '{' = child element,
		// Ident 'on' Ident = child element with an on-clause (Behavior on x).
		// A dotted chain ending in '{' (T.Button {) is a namespaced element.
		if j := p.pos + 1; p.toks[j].kind == tokPunct && p.toks[j].text == "." {
			for p.toks[j].kind == tokPunct && p.toks[j].text == "." && p.toks[j+1].kind == tokIdent {
				j += 2
			}
			if p.toks[j].kind == tokPunct && p.toks[j].text == "{" {
				child, err := p.element()
				if err != nil {
					return nil, err
				}
				child.owner = n
				n.children = append(n.children, child)
				continue
			}
		}
		if p.toks[p.pos+1].kind == tokPunct && p.toks[p.pos+1].text == "{" {
			child, err := p.element()
			if err != nil {
				return nil, err
			}
			child.owner = n
			n.children = append(n.children, child)
			continue
		}
		if p.toks[p.pos+1].kind == tokIdent && p.toks[p.pos+1].text == "on" &&
			p.toks[p.pos+2].kind == tokIdent {
			child, err := p.element()
			if err != nil {
				return nil, err
			}
			child.owner = n
			n.children = append(n.children, child)
			continue
		}
		a, err := p.attr()
		if err != nil {
			return nil, err
		}
		n.attrs = append(n.attrs, a)
	}
}

func (p *parser) propDecl() (propDecl, error) {
	line := p.cur().line
	required, isDflt := false, false
	// Optional modifier: readonly / default / required.
	if t := p.cur().text; t == "readonly" || t == "default" || t == "required" {
		required, isDflt = t == "required", t == "default"
		p.pos++
	}
	p.pos++ // "property"
	typ := p.next().text
	// list<T> / typed lists collapse to var (we hold them as a boxed value).
	if p.cur().kind == tokPunct && p.cur().text == "<" {
		for p.cur().kind != tokEOF && !(p.cur().kind == tokPunct && p.cur().text == ">") {
			p.pos++
		}
		if p.cur().kind == tokPunct && p.cur().text == ">" {
			p.pos++
		}
		typ = "var"
	}
	switch typ {
	case "int", "real", "double", "bool", "string", "var", "color", "alias":
	default:
		// Unknown / object / value types (Item, point, size, date, url, ...)
		// are held as var, so real .qml property decls don't fail to parse.
		typ = "var"
	}
	if p.cur().kind != tokIdent {
		return propDecl{}, fmt.Errorf("gml:%d: expected property name", line)
	}
	name := p.next().text
	// An alias's initializer is its target reference (kept as an unevaluated
	// expression), required.
	if typ == "alias" {
		if err := p.expect(":"); err != nil {
			return propDecl{}, fmt.Errorf("gml:%d: alias needs a target (alias x: id.prop)", line)
		}
		target, err := p.expr(0)
		if err != nil {
			return propDecl{}, err
		}
		return propDecl{typ: typ, name: name, def: target, required: required, isDflt: isDflt, line: line}, nil
	}
	// QML allows a property with no initializer; default to the type's zero.
	if !(p.cur().kind == tokPunct && p.cur().text == ":") {
		return propDecl{typ: typ, name: name, def: &litExpr{zeroForType(typ)}, required: required, isDflt: isDflt, line: line}, nil
	}
	p.pos++ // ":"
	// The value may start on the NEXT line — QML allows
	//   readonly property color c:
	//       cond ? "#a" : "#b"
	// and a declaration always has one, so a newline here is continuation.
	p.skipNewlines()
	// Element-valued default: `property FontLoader x: FontLoader { ... }`.
	// The element is kept; declareProps instantiates the types it models
	// (FontLoader) and holds null for the rest, so dependent bindings on
	// unmodeled types still fail soft.
	if p.cur().kind == tokIdent && p.toks[p.pos+1].kind == tokPunct && p.toks[p.pos+1].text == "{" {
		el, err := p.element()
		if err != nil {
			return propDecl{}, err
		}
		return propDecl{typ: "var", name: name, def: &elemExpr{n: el}, required: required, isDflt: isDflt, line: line}, nil
	}
	// Block-binding default: `property color c: { if (...) return x; ... }`.
	if p.cur().kind == tokPunct && p.cur().text == "{" && !p.objectLiteralAhead() {
		b, err := p.block()
		if err != nil {
			return propDecl{}, err
		}
		return propDecl{typ: typ, name: name, def: &blockExpr{body: []stmt{b}}, required: required, isDflt: isDflt, line: line}, nil
	}
	def, err := p.expr(0)
	if err != nil {
		return propDecl{}, err
	}
	return propDecl{typ: typ, name: name, def: def, required: required, isDflt: isDflt, line: line}, nil
}

// isPropertyStart reports whether the cursor begins a property declaration,
// allowing an optional readonly/default/required modifier before `property`.
func (p *parser) isPropertyStart() bool {
	if p.cur().kind != tokIdent {
		return false
	}
	t := p.cur().text
	if t == "property" {
		// A declaration is `property <type> <name>`; an attribute is
		// `property: <expr>` (e.g. NumberAnimation.property). Distinguish by the
		// token after `property`: an ident (type) means declaration, `:` means
		// attribute.
		return p.toks[p.pos+1].kind == tokIdent
	}
	if t == "readonly" || t == "default" || t == "required" {
		return p.toks[p.pos+1].kind == tokIdent && p.toks[p.pos+1].text == "property"
	}
	return false
}

// zeroForType is the default value for an uninitialized declared property.
func zeroForType(typ string) any {
	switch typ {
	case "bool":
		return false
	case "string", "color":
		return ""
	case "var":
		return nil
	}
	return float64(0) // int, real, double
}

// attr := Ident ('.' Ident)* ':' (expr | handlerStatements)
// Dotted names cover QML group properties (border.width, border.color).
func (p *parser) attr() (attr, error) {
	name := p.cur()
	p.pos++
	for p.cur().kind == tokPunct && p.cur().text == "." && p.toks[p.pos+1].kind == tokIdent {
		name.text += "." + p.toks[p.pos+1].text
		p.pos += 2
	}
	if err := p.expect(":"); err != nil {
		return attr{}, err
	}
	a := attr{name: name.text, line: name.line}
	// An on-handler: the name (or its last dotted segment, e.g.
	// Component.onCompleted, Keys.onPressed) starts with "on".
	last := name.text
	if i := strings.LastIndexByte(last, '.'); i >= 0 {
		last = last[i+1:]
	}
	if len(last) > 2 && last[:2] == "on" {
		stmts, err := p.handler()
		if err != nil {
			return attr{}, err
		}
		a.stmt = stmts
		return a, nil
	}
	// Element-valued property: `name: Type { ... }` (e.g. gradient: Gradient,
	// Behavior's animation, `delegate: T.ItemDelegate { ... }`). An Ident —
	// or a dotted, namespaced type — immediately followed by `{` is an
	// element; a plain Ident (color: red, width: parent.width) is an
	// expression.
	if p.cur().kind == tokIdent && p.elementAhead() {
		el, err := p.element()
		if err != nil {
			return attr{}, err
		}
		a.elem = el
		return a, nil
	}
	// Block binding: `text: { if (...) return "a"; ... }` — a `{` that is
	// NOT an object literal (key ":" ahead) parses as a statement block
	// whose return value is the binding's value.
	if p.cur().kind == tokPunct && p.cur().text == "{" && !p.objectLiteralAhead() {
		b, err := p.block()
		if err != nil {
			return attr{}, err
		}
		a.expr = &blockExpr{body: []stmt{b}}
		return a, nil
	}
	e, err := p.expr(0)
	if err != nil {
		return attr{}, err
	}
	a.expr = e
	return a, nil
}

// handler parses a signal handler body: either a `{ ... }` block or an
// inline `stmt (';' stmt)*` sequence terminated by newline or '}'.
func (p *parser) handler() ([]stmt, error) {
	if p.cur().kind == tokPunct && p.cur().text == "{" {
		b, err := p.block()
		if err != nil {
			return nil, err
		}
		return []stmt{b}, nil
	}
	var out []stmt
	for {
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if p.cur().kind == tokPunct && p.cur().text == ";" {
			p.pos++
			if p.cur().kind == tokNewline || (p.cur().kind == tokPunct && p.cur().text == "}") {
				return out, nil
			}
			// `onClicked: doThing(); ToolTip.text: "Home"` — inside an
			// element body a `name:` (or `Attached.name:`) after the
			// semicolon starts the NEXT attribute, not another statement.
			if p.attributeAhead() {
				return out, nil
			}
			continue
		}
		return out, nil
	}
}

// skipTypeAnnotation consumes a QML 6 `: Type` annotation (possibly dotted,
// `: Qt.SizeMode`) when one is present. The engine is loosely typed, so the
// declared type is informational.
func (p *parser) skipTypeAnnotation() {
	if !(p.cur().kind == tokPunct && p.cur().text == ":" && p.toks[p.pos+1].kind == tokIdent) {
		return
	}
	p.pos += 2
	for p.cur().kind == tokPunct && p.cur().text == "." && p.toks[p.pos+1].kind == tokIdent {
		p.pos += 2
	}
}

// elementAhead reports whether the cursor sits on a type name (possibly
// dotted, `T.ItemDelegate`) immediately followed by an element body.
func (p *parser) elementAhead() bool {
	i := p.pos + 1
	for p.toks[i].kind == tokPunct && p.toks[i].text == "." && p.toks[i+1].kind == tokIdent {
		i += 2
	}
	return p.toks[i].kind == tokPunct && p.toks[i].text == "{"
}

// attributeAhead reports whether the tokens at the cursor spell an element
// attribute — `name:` or a dotted `Group.name:` — as opposed to another
// statement in a handler.
func (p *parser) attributeAhead() bool {
	i := p.pos
	if p.toks[i].kind != tokIdent {
		return false
	}
	i++
	for p.toks[i].kind == tokPunct && p.toks[i].text == "." && p.toks[i+1].kind == tokIdent {
		i += 2
	}
	return p.toks[i].kind == tokPunct && p.toks[i].text == ":"
}

// funcDecl parses `function name(p1, p2) { body }`.
func (p *parser) funcDecl() (funcDecl, error) {
	line := p.cur().line
	p.pos++ // "function"
	name := p.next().text
	if err := p.expect("("); err != nil {
		return funcDecl{}, err
	}
	var params []string
	var defaults []expr
	p.skipNewlines()
	for !(p.cur().kind == tokPunct && p.cur().text == ")") {
		if p.cur().kind != tokIdent {
			return funcDecl{}, fmt.Errorf("gml:%d: expected parameter name", p.cur().line)
		}
		params = append(params, p.next().text)
		// QML 6 type annotations: `function pad2(n: int) : string`. The
		// engine is loosely typed, so the annotation is accepted and
		// dropped.
		p.skipTypeAnnotation()
		var def expr
		if p.cur().kind == tokPunct && p.cur().text == "=" {
			p.pos++
			d, err := p.expr(0)
			if err != nil {
				return funcDecl{}, err
			}
			def = d
		}
		defaults = append(defaults, def)
		p.skipNewlines()
		if p.cur().kind == tokPunct && p.cur().text == "," {
			p.pos++
			p.skipNewlines()
		}
	}
	p.pos++                // ")"
	p.skipTypeAnnotation() // return type: `function f() : string { ... }`
	p.skipNewlines()
	body, err := p.blockStmts()
	if err != nil {
		return funcDecl{}, err
	}
	return funcDecl{name: name, params: params, defaults: defaults, body: body, line: line}, nil
}

// signalDecl parses `signal name` or `signal name(type p1, type p2)`. Only
// parameter names are kept (the last identifier in each comma group), since
// the engine is untyped.
func (p *parser) signalDecl() (signalDecl, error) {
	line := p.cur().line
	p.pos++ // "signal"
	name := p.next().text
	var params []string
	if p.cur().kind == tokPunct && p.cur().text == "(" {
		p.pos++
		for !(p.cur().kind == tokPunct && p.cur().text == ")") {
			last := ""
			for p.cur().kind == tokIdent {
				last = p.next().text // "int x" -> keep "x"
			}
			if last != "" {
				params = append(params, last)
			}
			if p.cur().kind == tokPunct && p.cur().text == "," {
				p.pos++
			} else {
				break
			}
		}
		if err := p.expect(")"); err != nil {
			return signalDecl{}, err
		}
	}
	return signalDecl{name: name, params: params, line: line}, nil
}

// Statement parsers (JS-subset: blocks, var/let, if/else, for, while,
// return, break, continue, assignment, and expression statements).

// statement parses one statement.
func (p *parser) statement() (stmt, error) {
	t := p.cur()
	if t.kind == tokPunct && t.text == "{" {
		return p.block()
	}
	// Prefix ++/-- on an lvalue.
	if t.kind == tokPunct && (t.text == "++" || t.text == "--") {
		p.pos++
		target, err := p.postfix()
		if err != nil {
			return nil, err
		}
		return &assignStmt{target: target,
			value: &binExpr{op: t.text[:1], l: target, r: &litExpr{float64(1)}, line: t.line}}, nil
	}
	if t.kind == tokIdent {
		switch t.text {
		case "var", "let", "const":
			return p.varStmt()
		case "if":
			return p.ifStmt()
		case "for":
			return p.forStmt()
		case "while":
			return p.whileStmt()
		case "switch":
			return p.switchStmt()
		case "return":
			return p.returnStmt()
		case "try":
			return p.tryStmt()
		case "throw":
			p.pos++
			e, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			return &throwStmt{value: e, line: t.line}, nil
		case "function":
			// A function DECLARATION statement (a function EXPRESSION —
			// `function (ctx) {...}` with no name — is parsed by simpleStmt).
			if p.toks[p.pos+1].kind == tokIdent {
				fn, err := p.funcDecl()
				if err != nil {
					return nil, err
				}
				return &funcDeclStmt{name: fn.name, fn: fn}, nil
			}
		case "break":
			p.pos++
			return &breakStmt{}, nil
		case "continue":
			p.pos++
			return &continueStmt{}, nil
		}
	}
	return p.simpleStmt()
}

// block parses `{ stmt* }`.
func (p *parser) block() (stmt, error) {
	stmts, err := p.blockStmts()
	if err != nil {
		return nil, err
	}
	return &blockStmt{stmts: stmts}, nil
}

func (p *parser) blockStmts() ([]stmt, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	var out []stmt
	for {
		p.skipNewlines()
		if p.cur().kind == tokPunct && p.cur().text == "}" {
			p.pos++
			return out, nil
		}
		if p.cur().kind == tokEOF {
			return nil, fmt.Errorf("gml:%d: unclosed '{'", p.cur().line)
		}
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
}

func (p *parser) varStmt() (stmt, error) {
	p.pos++ // var/let/const
	var out []stmt
	for {
		if p.cur().kind != tokIdent {
			return nil, fmt.Errorf("gml:%d: expected variable name", p.cur().line)
		}
		name := p.next().text
		var val expr = &litExpr{nil}
		if p.cur().kind == tokPunct && p.cur().text == "=" {
			p.pos++
			p.skipNewlines() // `var x =` may continue on the next line
			v, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			val = v
		}
		out = append(out, &varStmt{name: name, value: val})
		// JS allows several declarators in one statement:
		// `var l = [], t = [], p = []`.
		if p.cur().kind == tokPunct && p.cur().text == "," {
			p.pos++
			p.skipNewlines()
			continue
		}
		break
	}
	if len(out) == 1 {
		return out[0], nil
	}
	return &seqStmt{stmts: out}, nil
}

func (p *parser) ifStmt() (stmt, error) {
	p.pos++ // if
	if err := p.expect("("); err != nil {
		return nil, err
	}
	cond, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	then, err := p.statement()
	if err != nil {
		return nil, err
	}
	var els stmt
	save := p.pos
	p.skipNewlines()
	if p.cur().kind == tokIdent && p.cur().text == "else" {
		p.pos++
		p.skipNewlines()
		els, err = p.statement()
		if err != nil {
			return nil, err
		}
	} else {
		p.pos = save // no else: don't swallow the separator
	}
	return &ifStmt{cond: cond, then: then, els: els}, nil
}

func (p *parser) whileStmt() (stmt, error) {
	p.pos++ // while
	if err := p.expect("("); err != nil {
		return nil, err
	}
	cond, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	body, err := p.statement()
	if err != nil {
		return nil, err
	}
	return &forStmt{cond: cond, body: body}, nil
}

// switchStmt parses `switch (expr) { case <expr>: stmt* ... default: stmt* }`
// with JS fall-through semantics (exec in stmt.go).
func (p *parser) switchStmt() (stmt, error) {
	p.pos++ // switch
	if err := p.expect("("); err != nil {
		return nil, err
	}
	subject, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	s := &switchStmt{subject: subject}
	for {
		p.skipNewlines()
		if p.cur().kind == tokPunct && p.cur().text == "}" {
			p.pos++
			return s, nil
		}
		if p.cur().kind == tokEOF {
			return nil, fmt.Errorf("gml:%d: unclosed switch", p.cur().line)
		}
		t := p.cur()
		var match expr // nil = default clause
		switch {
		case t.kind == tokIdent && t.text == "case":
			p.pos++
			if match, err = p.expr(0); err != nil {
				return nil, err
			}
		case t.kind == tokIdent && t.text == "default":
			p.pos++
		default:
			return nil, fmt.Errorf("gml:%d: switch wants case/default, got %q", t.line, t.text)
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		// Clause body: statements until the next case/default or the
		// closing brace (both are clause boundaries, not statements).
		var body []stmt
		for {
			p.skipNewlines()
			c := p.cur()
			if c.kind == tokEOF || (c.kind == tokPunct && c.text == "}") ||
				(c.kind == tokIdent && (c.text == "case" || c.text == "default")) {
				break
			}
			st, err := p.statement()
			if err != nil {
				return nil, err
			}
			body = append(body, st)
		}
		s.clauses = append(s.clauses, switchClause{match: match, body: body})
	}
}

func (p *parser) forStmt() (stmt, error) {
	p.pos++ // for
	if err := p.expect("("); err != nil {
		return nil, err
	}
	// for-of: `for (var x of iterable) body` (also bare `x of`).
	if ofVar, ok := p.tryForOf(); ok {
		iter, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		p.skipNewlines()
		body, err := p.statement()
		if err != nil {
			return nil, err
		}
		return &forOfStmt{name: ofVar, iter: iter, body: body}, nil
	}
	var init stmt
	var err error
	if !(p.cur().kind == tokPunct && p.cur().text == ";") {
		if p.cur().kind == tokIdent && (p.cur().text == "var" || p.cur().text == "let" || p.cur().text == "const") {
			init, err = p.varStmt()
		} else {
			init, err = p.simpleStmt()
		}
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	var cond expr
	if !(p.cur().kind == tokPunct && p.cur().text == ";") {
		if cond, err = p.expr(0); err != nil {
			return nil, err
		}
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	var post stmt
	if !(p.cur().kind == tokPunct && p.cur().text == ")") {
		if post, err = p.simpleStmt(); err != nil {
			return nil, err
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	p.skipNewlines()
	body, err := p.statement()
	if err != nil {
		return nil, err
	}
	return &forStmt{init: init, cond: cond, post: post, body: body}, nil
}

// objectLiteralAhead reports whether the `{` at the cursor starts an
// object literal (`{}` or `{ key: ...`) rather than a statement block.
func (p *parser) objectLiteralAhead() bool {
	j := p.pos + 1
	for p.toks[j].kind == tokNewline {
		j++
	}
	if p.toks[j].kind == tokPunct && p.toks[j].text == "}" {
		return true // {} — empty object
	}
	if p.toks[j].kind != tokIdent && p.toks[j].kind != tokString {
		return false
	}
	return p.toks[j+1].kind == tokPunct && p.toks[j+1].text == ":"
}

// tryForOf detects `[var|let|const] ident of` at the cursor and, on success,
// consumes through `of`, returning the loop variable name.
func (p *parser) tryForOf() (string, bool) {
	j := p.pos
	if p.toks[j].kind == tokIdent && (p.toks[j].text == "var" || p.toks[j].text == "let" || p.toks[j].text == "const") {
		j++
	}
	if p.toks[j].kind != tokIdent {
		return "", false
	}
	name := p.toks[j].text
	if !(p.toks[j+1].kind == tokIdent && p.toks[j+1].text == "of") {
		return "", false
	}
	p.pos = j + 2 // consume [var] ident of
	return name, true
}

// tryStmt parses `try { } catch (e) { } finally { }` (either clause may be
// absent, as in JS).
func (p *parser) tryStmt() (stmt, error) {
	p.pos++ // "try"
	p.skipNewlines()
	body, err := p.blockStmts()
	if err != nil {
		return nil, err
	}
	out := &tryStmt{body: body}
	p.skipNewlines()
	if p.cur().kind == tokIdent && p.cur().text == "catch" {
		p.pos++
		if p.cur().kind == tokPunct && p.cur().text == "(" {
			p.pos++
			if p.cur().kind == tokIdent {
				out.param = p.next().text
			}
			p.skipTypeAnnotation()
			if err := p.expect(")"); err != nil {
				return nil, err
			}
		}
		p.skipNewlines()
		out.catch, err = p.blockStmts()
		if err != nil {
			return nil, err
		}
		out.hasCatch = true
		p.skipNewlines()
	}
	if p.cur().kind == tokIdent && p.cur().text == "finally" {
		p.pos++
		p.skipNewlines()
		out.finally, err = p.blockStmts()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *parser) returnStmt() (stmt, error) {
	p.pos++ // return
	var val expr
	c := p.cur()
	if !(c.kind == tokNewline || c.kind == tokEOF || (c.kind == tokPunct && (c.text == ";" || c.text == "}"))) {
		v, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		val = v
	}
	return &returnStmt{value: val}, nil
}

// simpleStmt parses an assignment (`t = e`, `t += e`), a post-increment
// (`t++`), or a bare expression. Compound and increment forms desugar to a
// plain assignment with a binary right-hand side.
func (p *parser) simpleStmt() (stmt, error) {
	// Prefix ++i / --i (common in C-style for-loop increment clauses).
	// Evaluated only as a statement here, so the pre- vs post-increment
	// value distinction doesn't matter — both desugar to i = i + 1.
	if t := p.cur(); t.kind == tokPunct && (t.text == "++" || t.text == "--") {
		p.pos++
		target, err := p.postfix()
		if err != nil {
			return nil, err
		}
		return &assignStmt{target: target,
			value: &binExpr{op: t.text[:1], l: target, r: &litExpr{float64(1)}, line: t.line}}, nil
	}
	e, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	// Assignments and postfix ++/-- are expressions (see expr/postfix), so
	// what comes back already performs the write.
	return &exprStmt{e: e}, nil
}

// Pratt expression parser.

var binPrec = map[string]int{
	"??": 1, "||": 1, "&&": 2,
	"|": 2, "^": 2, "&": 2,
	"==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
	"<<": 4, ">>": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

// assignOps are the assignment operators, which are expressions in JS
// (`a = b`, and `(t2 -= 1.5)` inside a larger expression).
var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true,
	"%=": true, "|=": true, "&=": true, "^=": true,
}

func (p *parser) expr(minPrec int) (expr, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	// Assignment binds loosest and associates to the right, so it is only
	// picked up at the top of an expression.
	if minPrec == 0 {
		if t := p.cur(); t.kind == tokPunct && assignOps[t.text] {
			p.pos++
			p.skipNewlines() // the value may start on the next line
			rhs, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			return &updateExpr{target: left, op: t.text, value: rhs, line: t.line}, nil
		}
	}
	for {
		t := p.cur()
		// JS-style continuation: an expression may wrap to the next line
		// when the following line starts with a binary operator or '?'
		// ("..." \n + "..."). Peek past newlines; only consume them when
		// the operator genuinely continues this expression.
		if t.kind == tokNewline {
			j := p.pos
			for p.toks[j].kind == tokNewline {
				j++
			}
			nt := p.toks[j]
			_, isBin := binPrec[nt.text]
			if nt.kind == tokPunct && (isBin || (nt.text == "?" && minPrec == 0)) {
				p.pos = j
				continue
			}
			return left, nil
		}
		if t.kind == tokPunct && t.text == "?" && minPrec == 0 {
			p.pos++
			p.skipNewlines()
			a, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			p.skipNewlines()
			if err := p.expect(":"); err != nil {
				return nil, err
			}
			p.skipNewlines()
			b, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			left = &condExpr{c: left, a: a, b: b}
			continue
		}
		if t.kind != tokPunct {
			return left, nil
		}
		prec, ok := binPrec[t.text]
		if !ok || prec < minPrec {
			return left, nil
		}
		p.pos++
		p.skipNewlines() // operand may start on the next line ("..." + \n)
		right, err := p.expr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &binExpr{op: t.text, l: left, r: right, line: t.line}
	}
}

func (p *parser) unary() (expr, error) {
	t := p.cur()
	// `new Type(args)` — constructor call; evaluates like calling Type().
	if t.kind == tokIdent && t.text == "new" && p.toks[p.pos+1].kind == tokIdent {
		p.pos++
		return p.postfix()
	}
	if t.kind == tokPunct && (t.text == "-" || t.text == "!" || t.text == "+" || t.text == "~") {
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: t.text, x: x}, nil
	}
	// `typeof x` — JS's type predicate, which corpus code branches on
	// (`typeof t === "object"`).
	if t.kind == tokIdent && t.text == "typeof" {
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: "typeof", x: x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (expr, error) {
	e, err := p.postfixChain()
	if err != nil {
		return nil, err
	}
	// `i++` / `i--` as a VALUE (`{ id: root.nextId++ }`): writes, yields
	// the value from before the write.
	if t := p.cur(); t.kind == tokPunct && (t.text == "++" || t.text == "--") {
		p.pos++
		return &updateExpr{target: e, op: t.text, post: true, line: t.line}, nil
	}
	return e, nil
}

func (p *parser) postfixChain() (expr, error) {
	e, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.cur()
		if t.kind == tokNewline {
			// Continue a member chain that wraps to the next line (.foo()).
			j := p.pos
			for p.toks[j].kind == tokNewline {
				j++
			}
			if nt := p.toks[j]; nt.kind == tokPunct && (nt.text == "." || nt.text == "?.") {
				p.pos = j
				continue
			}
			return e, nil
		}
		switch {
		case t.kind == tokPunct && (t.text == "." || t.text == "?."):
			opt := t.text == "?."
			p.pos++
			if p.cur().kind != tokIdent {
				return nil, fmt.Errorf("gml:%d: expected member name after '%s'", p.cur().line, t.text)
			}
			e = &memberExpr{base: e, name: p.cur().text, line: p.cur().line, optional: opt}
			p.pos++
		case t.kind == tokPunct && t.text == "(":
			args, err := p.callArgs()
			if err != nil {
				return nil, err
			}
			e = &callExpr{callee: e, args: args, line: t.line, spread: hasSpread(args)}
		case t.kind == tokPunct && t.text == "[":
			p.pos++
			idx, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			e = &indexExpr{recv: e, index: idx, line: t.line}
		default:
			return e, nil
		}
	}
}

// callArgs parses a parenthesized, comma-separated argument list.
func (p *parser) callArgs() ([]expr, error) {
	p.pos++ // consume '('
	var args []expr
	p.skipNewlines()
	for !(p.cur().kind == tokPunct && p.cur().text == ")") {
		// Spread argument: ...expr, flattened when the call is made.
		spread := p.cur().kind == tokPunct && p.cur().text == "..."
		if spread {
			p.pos++
		}
		a, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if spread {
			a = &spreadExpr{inner: a}
		}
		args = append(args, a)
		p.skipNewlines()
		if p.cur().kind == tokPunct && p.cur().text == "," {
			p.pos++
			p.skipNewlines()
		}
	}
	p.pos++ // consume ')'
	return args, nil
}

func (p *parser) primary() (expr, error) {
	t := p.cur()
	switch {
	case t.kind == tokIdent && t.text == "function":
		// Function expression (QML signal-handler style):
		//   onFoo: function(a, b) { ... }
		// Optionally named; the name is not bound (JS semantics for
		// anonymous handler values are what QML uses).
		p.pos++
		if p.cur().kind == tokIdent {
			p.pos++ // optional name, ignored
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		var params []string
		var defaults []expr
		p.skipNewlines()
		for !(p.cur().kind == tokPunct && p.cur().text == ")") {
			if p.cur().kind != tokIdent {
				return nil, fmt.Errorf("gml:%d: expected parameter name", p.cur().line)
			}
			params = append(params, p.next().text)
			var def expr
			if p.cur().kind == tokPunct && p.cur().text == "=" {
				p.pos++
				d, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				def = d
			}
			defaults = append(defaults, def)
			p.skipNewlines()
			if p.cur().kind == tokPunct && p.cur().text == "," {
				p.pos++
				p.skipNewlines()
			}
		}
		p.pos++ // ")"
		p.skipNewlines()
		body, err := p.blockStmts()
		if err != nil {
			return nil, err
		}
		return &funcExpr{params: params, defaults: defaults, body: body}, nil
	case t.kind == tokNumber:
		p.pos++
		// A radix literal (0x30A0, 0b1011, 0o17) is an integer: ParseFloat
		// only takes hex with a binary exponent, so it goes through
		// ParseInt's base-0 rules and lands as the float every number here
		// is.
		if len(t.text) > 2 && t.text[0] == '0' && radixOf(rune(t.text[1])) > 0 {
			n, err := strconv.ParseInt(t.text, 0, 64)
			if err != nil {
				return nil, fmt.Errorf("gml:%d: bad number %q", t.line, t.text)
			}
			return &litExpr{float64(n)}, nil
		}
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: bad number %q", t.line, t.text)
		}
		return &litExpr{f}, nil
	case t.kind == tokString:
		p.pos++
		return &litExpr{t.text}, nil
	case t.kind == tokRegex:
		p.pos++
		pattern, flags, _ := strings.Cut(t.text, "\x00")
		re, err := newRegExp(pattern, flags)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: %v", t.line, err)
		}
		return &litExpr{re}, nil
	case t.kind == tokTemplate:
		p.pos++
		return parseTemplate(t.text, t.line)
	case t.kind == tokIdent:
		switch t.text {
		case "true":
			p.pos++
			return &litExpr{true}, nil
		case "false":
			p.pos++
			return &litExpr{false}, nil
		case "null", "undefined":
			p.pos++
			return &litExpr{nil}, nil
		}
		// Single-param arrow function: ident => body.
		if p.toks[p.pos+1].kind == tokPunct && p.toks[p.pos+1].text == "=>" {
			p.pos += 2
			return p.arrowBody([]string{t.text})
		}
		// A call/member chain on this identifier is handled by postfix().
		p.pos++
		return &identExpr{name: t.text, line: t.line}, nil
	case t.kind == tokPunct && t.text == "(":
		// Parenthesized arrow params: ( ... ) => body. Look ahead to the
		// matching ')' and see if '=>' follows.
		if params, ok := p.tryArrowParams(); ok {
			return p.arrowBody(params)
		}
		p.pos++
		e, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	case t.kind == tokPunct && t.text == "[":
		p.pos++
		l := &listExpr{}
		p.skipNewlines()
		for !(p.cur().kind == tokPunct && p.cur().text == "]") {
			var item expr
			// Spread element: ...expr (flattened at eval).
			if p.cur().kind == tokPunct && p.cur().text == "..." {
				p.pos++
				e, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				item = &spreadExpr{inner: e}
			} else if p.cur().kind == tokIdent && p.toks[p.pos+1].kind == tokPunct && p.toks[p.pos+1].text == "{" {
				// An element item (Ident '{') — e.g. State/Transition objects.
				el, err := p.element()
				if err != nil {
					return nil, err
				}
				item = &elemExpr{n: el}
			} else {
				e, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				item = e
			}
			l.items = append(l.items, item)
			p.skipNewlines()
			if p.cur().kind == tokPunct && p.cur().text == "," {
				p.pos++
				p.skipNewlines()
			}
		}
		p.pos++
		return l, nil
	case t.kind == tokPunct && t.text == "{":
		// JS object literal: { key: expr, "key": expr, ... }. In expression
		// position (the only place primary runs) a `{` is unambiguous.
		p.pos++
		o := &objExpr{}
		p.skipNewlines()
		for !(p.cur().kind == tokPunct && p.cur().text == "}") {
			k := p.cur()
			if k.kind != tokIdent && k.kind != tokString {
				return nil, fmt.Errorf("gml:%d: object key wants a name or string, got %q", k.line, k.text)
			}
			p.pos++
			if err := p.expect(":"); err != nil {
				return nil, err
			}
			val, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			o.keys = append(o.keys, k.text)
			o.vals = append(o.vals, val)
			p.skipNewlines()
			if p.cur().kind == tokPunct && p.cur().text == "," {
				p.pos++
				p.skipNewlines()
			}
		}
		p.pos++
		return o, nil
	}
	return nil, fmt.Errorf("gml:%d: unexpected %q in expression", t.line, t.text)
}

// parseTemplate splits a template-literal body into literal parts and embedded
// ${expr} expressions (each lexed+parsed independently).
func parseTemplate(body string, line int) (expr, error) {
	tpl := &templateExpr{}
	var lit strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '$' && i+1 < len(body) && body[i+1] == '{' {
			// find matching '}' (no nested braces in expressions here)
			depth, j := 1, i+2
			for ; j < len(body) && depth > 0; j++ {
				if body[j] == '{' {
					depth++
				} else if body[j] == '}' {
					depth--
				}
			}
			if depth != 0 {
				return nil, fmt.Errorf("gml:%d: unterminated ${} in template", line)
			}
			exprSrc := body[i+2 : j-1]
			toks, err := lex(exprSrc)
			if err != nil {
				return nil, fmt.Errorf("gml:%d: template expr: %w", line, err)
			}
			sub := &parser{toks: toks}
			e, err := sub.expr(0)
			if err != nil {
				return nil, fmt.Errorf("gml:%d: template expr: %w", line, err)
			}
			tpl.parts = append(tpl.parts, lit.String())
			lit.Reset()
			tpl.exprs = append(tpl.exprs, e)
			i = j - 1
		} else {
			lit.WriteByte(body[i])
		}
	}
	tpl.parts = append(tpl.parts, lit.String())
	return tpl, nil
}

// tryArrowParams checks whether the '(' at the cursor opens arrow-function
// parameters — i.e. a comma-separated ident list closed by ')' immediately
// followed by '=>'. On success it consumes through ')' (leaving '=>' at the
// cursor) and returns the param names; otherwise it consumes nothing.
func (p *parser) tryArrowParams() ([]string, bool) {
	j := p.pos + 1 // after '('
	var params []string
	for !(p.toks[j].kind == tokPunct && p.toks[j].text == ")") {
		if p.toks[j].kind != tokIdent {
			return nil, false // not a plain param list
		}
		params = append(params, p.toks[j].text)
		j++
		if p.toks[j].kind == tokPunct && p.toks[j].text == "," {
			j++
			continue
		}
		break
	}
	if !(p.toks[j].kind == tokPunct && p.toks[j].text == ")") {
		return nil, false
	}
	j++ // past ')'
	if !(p.toks[j].kind == tokPunct && p.toks[j].text == "=>") {
		return nil, false
	}
	p.pos = j + 1 // consume '(' params ')' '=>'
	return params, true
}

// arrowBody parses the body after '=>' (already consumed): a block { stmts } or
// a single expression (wrapped in an implicit return). Returns a funcExpr.
func (p *parser) arrowBody(params []string) (expr, error) {
	if p.cur().kind == tokPunct && p.cur().text == "{" {
		b, err := p.block()
		if err != nil {
			return nil, err
		}
		return &funcExpr{params: params, body: []stmt{b}}, nil
	}
	e, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	return &funcExpr{params: params, body: []stmt{&returnStmt{value: e}}}, nil
}

// attrsID identifies a node's attrs slice by its backing array and length:
// the derived-node caches on a node are valid only while its attrs are
// the ones they were computed from.
type attrsID struct {
	first *attr
	n     int
}

func attrsOf(n *node) attrsID {
	if len(n.attrs) == 0 {
		return attrsID{}
	}
	return attrsID{&n.attrs[0], len(n.attrs)}
}

// hasSpread reports whether a call has a `...expr` argument.
func hasSpread(args []expr) bool {
	for _, a := range args {
		if _, ok := a.(*spreadExpr); ok {
			return true
		}
	}
	return false
}
