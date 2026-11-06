package gml

// The gml writer: serialize a document AST back to source — the save path
// for documents built or edited programmatically (cad feature trees, scene
// editors) and a formatter for hand-written files.
//
//	out, err := gml.Format(src)            // gofmt-for-gml (parse + print)
//
//	b := gml.NewBuilder("Scene3D")         // programmatic construction
//	b.Attr("lightDir", []any{-0.5, -1.0, -0.3})
//	box := b.Child("Box")
//	box.Attr("size", []any{4, 2.5, 0.5})
//	box.AttrExpr("position", "[0, 1.25 + doorOpen*2.4, 19]")
//	src, err := b.String()
//
// Comments are not preserved (the lexer drops them), and a node's
// declarations print in canonical order: properties, signals, attributes,
// children, functions. Everything else round-trips: Format is idempotent
// and re-parses to the same semantics.

import (
	"fmt"
	"strconv"
	"strings"
)

// Format parses gml source and pretty-prints it.
func Format(src string) (string, error) {
	root, err := Parse(src)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	writeNode(&sb, root, 0)
	return sb.String(), nil
}

// ---- builder ----------------------------------------------------------------

// Builder constructs a document programmatically. Methods chain; the first
// error (a bad AttrExpr) is retained and returned by String.
type Builder struct {
	n   *node
	err *error // shared across the tree
}

// NewBuilder starts a document with the given root element type.
func NewBuilder(typeName string) *Builder {
	var err error
	return &Builder{n: &node{typeName: typeName}, err: &err}
}

// Attr sets a literal-valued attribute: numbers, strings, bools, and
// []any / []float64 / []int lists (nested lists allowed).
func (b *Builder) Attr(name string, value any) *Builder {
	e, err := valueExpr(value)
	if err != nil && *b.err == nil {
		*b.err = fmt.Errorf("gml: attr %s: %w", name, err)
	}
	b.n.attrs = append(b.n.attrs, attr{name: name, expr: e})
	return b
}

// AttrExpr sets an attribute to a gml/JS expression given as source — the
// way to write bindings: b.AttrExpr("position", "[0, doorOpen*2.4, 19]").
func (b *Builder) AttrExpr(name, src string) *Builder {
	e, err := parseExprSrc(src)
	if err != nil && *b.err == nil {
		*b.err = fmt.Errorf("gml: attr %s: %w", name, err)
	}
	if e != nil {
		b.n.attrs = append(b.n.attrs, attr{name: name, expr: e})
	}
	return b
}

// Prop declares a document property (`property <typ> <name>: <default>`).
func (b *Builder) Prop(typ, name string, def any) *Builder {
	e, err := valueExpr(def)
	if err != nil && *b.err == nil {
		*b.err = fmt.Errorf("gml: property %s: %w", name, err)
	}
	b.n.props = append(b.n.props, propDecl{typ: typ, name: name, def: e})
	return b
}

// ID sets the element's id.
func (b *Builder) ID(id string) *Builder {
	b.n.attrs = append(b.n.attrs, attr{name: "id", expr: &identExpr{name: id}})
	return b
}

// Child appends a child element and returns its builder.
func (b *Builder) Child(typeName string) *Builder {
	c := &node{typeName: typeName}
	b.n.children = append(b.n.children, c)
	return &Builder{n: c, err: b.err}
}

// String serializes the document (or reports the first construction error).
func (b *Builder) String() (string, error) {
	if *b.err != nil {
		return "", *b.err
	}
	var sb strings.Builder
	writeNode(&sb, b.n, 0)
	return sb.String(), nil
}

// valueExpr converts a Go literal to an expression node.
func valueExpr(v any) (expr, error) {
	switch t := v.(type) {
	case nil:
		return &litExpr{v: nil}, nil
	case bool, string:
		return &litExpr{v: t}, nil
	case float64:
		return &litExpr{v: t}, nil
	case float32:
		return &litExpr{v: float64(t)}, nil
	case int:
		return &litExpr{v: float64(t)}, nil
	case []any:
		l := &listExpr{}
		for _, item := range t {
			e, err := valueExpr(item)
			if err != nil {
				return nil, err
			}
			l.items = append(l.items, e)
		}
		return l, nil
	case []float64:
		items := make([]any, len(t))
		for i, f := range t {
			items[i] = f
		}
		return valueExpr(items)
	case []float32:
		items := make([]any, len(t))
		for i, f := range t {
			items[i] = float64(f)
		}
		return valueExpr(items)
	case []int:
		items := make([]any, len(t))
		for i, f := range t {
			items[i] = float64(f)
		}
		return valueExpr(items)
	}
	return nil, fmt.Errorf("unsupported value type %T", v)
}

// parseExprSrc parses a standalone expression.
func parseExprSrc(src string) (expr, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	p.skipNewlines()
	e, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	p.skipNewlines()
	if p.cur().kind != tokEOF {
		return nil, fmt.Errorf("gml: unexpected %q after expression", p.cur().text)
	}
	return e, nil
}

// ---- node printing ----------------------------------------------------------

func writeNode(sb *strings.Builder, n *node, depth int) {
	ind := strings.Repeat("    ", depth)
	sb.WriteString(ind)
	sb.WriteString(n.typeName)
	if n.onTarget != "" {
		sb.WriteString(" on ")
		sb.WriteString(n.onTarget)
	}
	empty := len(n.attrs) == 0 && len(n.children) == 0 && len(n.props) == 0 &&
		len(n.funcs) == 0 && len(n.signals) == 0
	if empty {
		sb.WriteString(" { }\n")
		return
	}
	sb.WriteString(" {\n")
	inner := ind + "    "
	for _, p := range n.props {
		fmt.Fprintf(sb, "%sproperty %s %s: %s\n", inner, p.typ, p.name, exprString(p.def, 0))
	}
	for _, s := range n.signals {
		fmt.Fprintf(sb, "%ssignal %s(%s)\n", inner, s.name, strings.Join(s.params, ", "))
	}
	for _, a := range n.attrs {
		writeAttr(sb, a, depth+1)
	}
	for _, c := range n.children {
		writeNode(sb, c, depth+1)
	}
	for _, f := range n.funcs {
		fmt.Fprintf(sb, "%sfunction %s(%s) {\n", inner, f.name, strings.Join(f.params, ", "))
		for _, s := range f.body {
			writeStmt(sb, s, depth+2)
		}
		sb.WriteString(inner + "}\n")
	}
	sb.WriteString(ind + "}\n")
}

func writeAttr(sb *strings.Builder, a attr, depth int) {
	ind := strings.Repeat("    ", depth)
	switch {
	case a.elem != nil:
		// Element-valued attribute: `gradient: Gradient { ... }`.
		sb.WriteString(ind)
		sb.WriteString(a.name)
		sb.WriteString(": ")
		var esb strings.Builder
		writeNode(&esb, a.elem, depth)
		sb.WriteString(strings.TrimPrefix(esb.String(), ind))
	case a.stmt != nil:
		// Handler attribute.
		if len(a.stmt) == 1 {
			if line, ok := stmtOneLine(a.stmt[0]); ok {
				fmt.Fprintf(sb, "%s%s: %s\n", ind, a.name, line)
				return
			}
		}
		fmt.Fprintf(sb, "%s%s: {\n", ind, a.name)
		for _, s := range a.stmt {
			writeStmt(sb, s, depth+1)
		}
		sb.WriteString(ind + "}\n")
	default:
		fmt.Fprintf(sb, "%s%s: %s\n", ind, a.name, exprString(a.expr, 0))
	}
}

// ---- statement printing -----------------------------------------------------

func writeStmt(sb *strings.Builder, s stmt, depth int) {
	ind := strings.Repeat("    ", depth)
	if line, ok := stmtOneLine(s); ok {
		sb.WriteString(ind)
		sb.WriteString(line)
		sb.WriteString("\n")
		return
	}
	switch t := s.(type) {
	case *blockStmt:
		for _, inner := range t.stmts {
			writeStmt(sb, inner, depth)
		}
	case *ifStmt:
		fmt.Fprintf(sb, "%sif (%s) {\n", ind, exprString(t.cond, 0))
		writeStmt(sb, t.then, depth+1)
		if t.els != nil {
			sb.WriteString(ind + "} else {\n")
			writeStmt(sb, t.els, depth+1)
		}
		sb.WriteString(ind + "}\n")
	case *forStmt:
		init, post := "", ""
		if t.init != nil {
			init, _ = stmtOneLine(t.init)
		}
		if t.post != nil {
			post, _ = stmtOneLine(t.post)
		}
		fmt.Fprintf(sb, "%sfor (%s; %s; %s) {\n", ind, init, exprString(t.cond, 0), post)
		writeStmt(sb, t.body, depth+1)
		sb.WriteString(ind + "}\n")
	case *forOfStmt:
		fmt.Fprintf(sb, "%sfor (var %s of %s) {\n", ind, t.name, exprString(t.iter, 0))
		writeStmt(sb, t.body, depth+1)
		sb.WriteString(ind + "}\n")
	case *tryStmt:
		sb.WriteString(ind + "try {\n")
		for _, inner := range t.body {
			writeStmt(sb, inner, depth+1)
		}
		if t.hasCatch {
			fmt.Fprintf(sb, "%s} catch (%s) {\n", ind, t.param)
			for _, inner := range t.catch {
				writeStmt(sb, inner, depth+1)
			}
		}
		if len(t.finally) > 0 {
			sb.WriteString(ind + "} finally {\n")
			for _, inner := range t.finally {
				writeStmt(sb, inner, depth+1)
			}
		}
		sb.WriteString(ind + "}\n")
	case *throwStmt:
		fmt.Fprintf(sb, "%sthrow %s\n", ind, exprString(t.value, 0))
	case *funcDeclStmt:
		fmt.Fprintf(sb, "%sfunction %s(%s) {\n", ind, t.name, strings.Join(t.fn.params, ", "))
		for _, inner := range t.fn.body {
			writeStmt(sb, inner, depth+1)
		}
		sb.WriteString(ind + "}\n")
	default:
		// Fallback: unknown statements print as comments so output re-parses.
		fmt.Fprintf(sb, "%s// <unprintable statement %T>\n", ind, s)
	}
}

// stmtOneLine renders simple statements on one line (false for control flow).
func stmtOneLine(s stmt) (string, bool) {
	switch t := s.(type) {
	case *exprStmt:
		return exprString(t.e, 0), true
	case *assignStmt:
		return exprString(t.target, 0) + " = " + exprString(t.value, 0), true
	case *varStmt:
		return "var " + t.name + " = " + exprString(t.value, 0), true
	case *seqStmt:
		// `var a = 1, b = 2` — several declarators in one statement.
		var parts []string
		for _, st := range t.stmts {
			line, ok := stmtOneLine(st)
			if !ok {
				return "", false
			}
			parts = append(parts, line)
		}
		return strings.Join(parts, "; "), true
	case *returnStmt:
		if t.value == nil {
			return "return", true
		}
		return "return " + exprString(t.value, 0), true
	case *breakStmt:
		return "break", true
	case *continueStmt:
		return "continue", true
	case *blockStmt:
		if len(t.stmts) == 1 {
			return stmtOneLine(t.stmts[0])
		}
	}
	return "", false
}

// ---- expression printing ----------------------------------------------------

// Precedence levels for parenthesization: higher binds tighter. Mirrors the
// parser (binPrec 1..5), with conditional below all binaries and unary /
// postfix above.
func exprPrec(e expr) int {
	switch t := e.(type) {
	case *condExpr:
		return 0
	case *binExpr:
		return binPrec[t.op]
	case *unaryExpr:
		return 6
	case *funcExpr:
		return 0
	default:
		return 7 // primary / postfix
	}
}

// exprString prints e, parenthesized if its precedence is below min.
func exprString(e expr, min int) string {
	if e == nil {
		return "null"
	}
	s := exprStringRaw(e)
	if exprPrec(e) < min {
		return "(" + s + ")"
	}
	return s
}

func exprStringRaw(e expr) string {
	switch t := e.(type) {
	case *litExpr:
		return litString(t.v)
	case *identExpr:
		return t.name
	case *memberExpr:
		dot := "."
		if t.optional {
			dot = "?."
		}
		return exprString(t.base, 7) + dot + t.name
	case *indexExpr:
		return exprString(t.recv, 7) + "[" + exprString(t.index, 0) + "]"
	case *callExpr:
		args := make([]string, len(t.args))
		for i, a := range t.args {
			args[i] = exprString(a, 0)
		}
		return exprString(t.callee, 7) + "(" + strings.Join(args, ", ") + ")"
	case *unaryExpr:
		return t.op + exprString(t.x, 6)
	case *binExpr:
		p := binPrec[t.op]
		// Left-associative: the right side needs strictly higher precedence.
		return exprString(t.l, p) + " " + t.op + " " + exprString(t.r, p+1)
	case *condExpr:
		return exprString(t.c, 1) + " ? " + exprString(t.a, 1) + " : " + exprString(t.b, 1)
	case *listExpr:
		items := make([]string, len(t.items))
		for i, it := range t.items {
			items[i] = exprString(it, 0)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case *spreadExpr:
		return "..." + exprString(t.inner, 0)
	case *objExpr:
		parts := make([]string, len(t.keys))
		for i, k := range t.keys {
			parts[i] = objKey(k) + ": " + exprString(t.vals[i], 1)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *templateExpr:
		var sb strings.Builder
		sb.WriteString("`")
		for i, part := range t.parts {
			sb.WriteString(escapeTemplate(part))
			if i < len(t.exprs) {
				sb.WriteString("${")
				sb.WriteString(exprString(t.exprs[i], 0))
				sb.WriteString("}")
			}
		}
		sb.WriteString("`")
		return sb.String()
	case *funcExpr:
		params := "(" + strings.Join(t.params, ", ") + ")"
		if len(t.body) == 1 {
			if r, ok := t.body[0].(*returnStmt); ok && r.value != nil {
				return params + " => " + exprString(r.value, 1)
			}
			if line, ok := stmtOneLine(t.body[0]); ok {
				return params + " => { " + line + " }"
			}
		}
		var sb strings.Builder
		sb.WriteString(params + " => {\n")
		for _, s := range t.body {
			writeStmt(&sb, s, 1)
		}
		sb.WriteString("}")
		return sb.String()
	case *elemExpr:
		var sb strings.Builder
		writeNode(&sb, t.n, 0)
		return strings.TrimSuffix(sb.String(), "\n")
	case *updateExpr:
		// `a = b`, `a += b`, `a++` — assignments are expressions in JS.
		if t.value == nil {
			if t.post {
				return exprString(t.target, 0) + t.op
			}
			return t.op + exprString(t.target, 0)
		}
		return exprString(t.target, 0) + " " + t.op + " " + exprString(t.value, 0)
	}
	return fmt.Sprintf("/* unprintable %T */", e)
}

func litString(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case colorVal:
		return strconv.Quote(string(t))
	case string:
		return strconv.Quote(t)
	case float64:
		// Prefer plain notation when it's exact and not absurdly long
		// (1718000000, not 1.718e+09); fall back to %g for extremes.
		if plain := strconv.FormatFloat(t, 'f', -1, 64); len(plain) <= 21 {
			return plain
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

// objKey prints an object key bare when it is identifier-like.
func objKey(k string) string {
	if k == "" {
		return `""`
	}
	for i, r := range k {
		alpha := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !alpha && (i == 0 || r < '0' || r > '9') {
			return strconv.Quote(k)
		}
	}
	return k
}

func escapeTemplate(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	return strings.ReplaceAll(s, "${", "\\${")
}
