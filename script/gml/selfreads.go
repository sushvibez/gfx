package gml

// Which nodes ever read their own scope by bare name.
//
// shadowSelfGeometry binds an item's width, height, opacity, visible (and a
// Flickable's scroll state) into the env for the item's build, so a
// `radius: height / 2` resolves the way QML's item scope does. It ran for
// every node on every build — a fifth of an animated frame's allocation —
// and most nodes never mention any of those names. The parser marks each
// node whose whole subtree is free of them (attributes, handlers, property
// defaults, functions, element-valued attributes included), and the shadow
// is skipped for those. The mark is conservative: a node built from a
// literal at run time carries the zero value, which keeps the shadow.

// selfScopeNames are the bare names shadowSelfGeometry binds.
var selfScopeNames = func() map[string]bool {
	m := map[string]bool{"width": true, "height": true, "opacity": true, "visible": true}
	for _, n := range scrollBareNames {
		m[n] = true
	}
	return m
}()

// markSelfReads sets node.noSelfReads through the tree rooted at n and
// reports whether the subtree mentions a self-scope name.
func markSelfReads(n *node) bool {
	if n == nil {
		return false
	}
	uses := false
	// `NumberAnimation on opacity` / `Behavior on width` name the owner's
	// property without an identifier: the owner's scope is read through
	// the animator, so it counts as a mention.
	if selfScopeNames[n.onTarget] {
		uses = true
	}
	for i := range n.attrs {
		if attrMentions(&n.attrs[i], selfScopeNames) {
			uses = true
		}
	}
	for _, d := range n.props {
		if d.def != nil && exprMentions(d.def, selfScopeNames) {
			uses = true
		}
	}
	for _, f := range n.funcs {
		if stmtsMention(f.body, selfScopeNames) || exprsMention(f.defaults, selfScopeNames) {
			uses = true
		}
	}
	for _, c := range n.children {
		if markSelfReads(c) {
			uses = true
		}
	}
	n.noSelfReads = !uses
	return uses
}

func attrMentions(a *attr, names map[string]bool) bool {
	if a.expr != nil && exprMentions(a.expr, names) {
		return true
	}
	if len(a.stmt) > 0 && stmtsMention(a.stmt, names) {
		return true
	}
	if a.elem != nil && markSelfReads(a.elem) {
		return true
	}
	return false
}

func exprsMention(es []expr, names map[string]bool) bool {
	for _, e := range es {
		if e != nil && exprMentions(e, names) {
			return true
		}
	}
	return false
}

// exprMentions reports whether e reads a bare identifier in names.
func exprMentions(e expr, names map[string]bool) bool {
	switch x := e.(type) {
	case nil:
		return false
	case *litExpr:
		return false
	case *identExpr:
		return names[x.name]
	case *memberExpr:
		return exprMentions(x.base, names)
	case *unaryExpr:
		return exprMentions(x.x, names)
	case *binExpr:
		return exprMentions(x.l, names) || exprMentions(x.r, names)
	case *condExpr:
		return exprMentions(x.c, names) || exprMentions(x.a, names) || exprMentions(x.b, names)
	case *callExpr:
		return exprMentions(x.callee, names) || exprsMention(x.args, names)
	case *listExpr:
		return exprsMention(x.items, names)
	case *spreadExpr:
		return exprMentions(x.inner, names)
	case *updateExpr:
		return exprMentions(x.target, names) || exprMentions(x.value, names)
	case *objExpr:
		return exprsMention(x.vals, names)
	case *templateExpr:
		return exprsMention(x.exprs, names)
	case *funcExpr:
		return stmtsMention(x.body, names) || exprsMention(x.defaults, names)
	case *blockExpr:
		return stmtsMention(x.body, names)
	case *indexExpr:
		return exprMentions(x.recv, names) || exprMentions(x.index, names)
	case *elemExpr:
		return markSelfReads(x.n)
	}
	// An expression kind this walk does not know: assume it reads.
	return true
}

func stmtsMention(ss []stmt, names map[string]bool) bool {
	for _, s := range ss {
		if stmtMentions(s, names) {
			return true
		}
	}
	return false
}

func stmtMentions(s stmt, names map[string]bool) bool {
	switch x := s.(type) {
	case nil:
		return false
	case *exprStmt:
		return exprMentions(x.e, names)
	case *assignStmt:
		return exprMentions(x.target, names) || exprMentions(x.value, names)
	case *varStmt:
		return exprMentions(x.value, names)
	case *blockStmt:
		return stmtsMention(x.stmts, names)
	case *seqStmt:
		return stmtsMention(x.stmts, names)
	case *funcDeclStmt:
		return stmtsMention(x.fn.body, names) || exprsMention(x.fn.defaults, names)
	case *ifStmt:
		return exprMentions(x.cond, names) || stmtMentions(x.then, names) || stmtMentions(x.els, names)
	case *forStmt:
		return stmtMentions(x.init, names) || exprMentions(x.cond, names) ||
			stmtMentions(x.post, names) || stmtMentions(x.body, names)
	case *forOfStmt:
		return exprMentions(x.iter, names) || stmtMentions(x.body, names)
	case *returnStmt:
		return exprMentions(x.value, names)
	case *tryStmt:
		return stmtsMention(x.body, names) || stmtsMention(x.catch, names) || stmtsMention(x.finally, names)
	case *throwStmt:
		return exprMentions(x.value, names)
	case *breakStmt, *continueStmt:
		return false
	case *switchStmt:
		if exprMentions(x.subject, names) {
			return true
		}
		for _, c := range x.clauses {
			if exprMentions(c.match, names) || stmtsMention(c.body, names) {
				return true
			}
		}
		return false
	}
	return true
}

// stmtsHaveClosure reports whether a statement list contains an arrow
// function — the one construct that can keep a reference to the scope
// it is evaluated in (see callUserFunc).
func stmtsHaveClosure(ss []stmt) bool {
	for _, s := range ss {
		if stmtHasClosure(s) {
			return true
		}
	}
	return false
}

func exprsHaveClosure(es []expr) bool {
	for _, e := range es {
		if exprHasClosure(e) {
			return true
		}
	}
	return false
}

func exprHasClosure(e expr) bool {
	switch x := e.(type) {
	case nil, *litExpr, *identExpr:
		return false
	case *memberExpr:
		return exprHasClosure(x.base)
	case *unaryExpr:
		return exprHasClosure(x.x)
	case *binExpr:
		return exprHasClosure(x.l) || exprHasClosure(x.r)
	case *condExpr:
		return exprHasClosure(x.c) || exprHasClosure(x.a) || exprHasClosure(x.b)
	case *callExpr:
		return exprHasClosure(x.callee) || exprsHaveClosure(x.args)
	case *listExpr:
		return exprsHaveClosure(x.items)
	case *spreadExpr:
		return exprHasClosure(x.inner)
	case *updateExpr:
		return exprHasClosure(x.target) || exprHasClosure(x.value)
	case *objExpr:
		return exprsHaveClosure(x.vals)
	case *templateExpr:
		return exprsHaveClosure(x.exprs)
	case *funcExpr:
		return true
	case *blockExpr:
		return stmtsHaveClosure(x.body)
	case *indexExpr:
		return exprHasClosure(x.recv) || exprHasClosure(x.index)
	case *elemExpr:
		return true // an element evaluated here may bind into the scope
	}
	return true
}

func stmtHasClosure(s stmt) bool {
	switch x := s.(type) {
	case nil, *breakStmt, *continueStmt:
		return false
	case *exprStmt:
		return exprHasClosure(x.e)
	case *assignStmt:
		return exprHasClosure(x.target) || exprHasClosure(x.value)
	case *varStmt:
		return exprHasClosure(x.value)
	case *blockStmt:
		return stmtsHaveClosure(x.stmts)
	case *seqStmt:
		return stmtsHaveClosure(x.stmts)
	case *funcDeclStmt:
		return true // a nested function declaration closes over the scope
	case *ifStmt:
		return exprHasClosure(x.cond) || stmtHasClosure(x.then) || stmtHasClosure(x.els)
	case *forStmt:
		return stmtHasClosure(x.init) || exprHasClosure(x.cond) || stmtHasClosure(x.post) || stmtHasClosure(x.body)
	case *forOfStmt:
		return exprHasClosure(x.iter) || stmtHasClosure(x.body)
	case *returnStmt:
		return exprHasClosure(x.value)
	case *tryStmt:
		return stmtsHaveClosure(x.body) || stmtsHaveClosure(x.catch) || stmtsHaveClosure(x.finally)
	case *throwStmt:
		return exprHasClosure(x.value)
	case *switchStmt:
		if exprHasClosure(x.subject) {
			return true
		}
		for _, c := range x.clauses {
			if exprHasClosure(c.match) || stmtsHaveClosure(c.body) {
				return true
			}
		}
		return false
	}
	return true
}
