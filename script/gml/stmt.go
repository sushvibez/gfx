package gml

import (
	"fmt"
	"strconv"
	"sync"
)

// JavaScript-subset statements (phase D2): blocks, var/let, if/else,
// for/while, return/break/continue, assignment and expression statements,
// plus user-declared functions. Execution threads a ctrl value so loops and
// functions unwind correctly.

// stmt is an executable statement. exec returns a ctrl signal (return/break/
// continue) so enclosing constructs can react.
type stmt interface {
	exec(env *Env) (ctrl, error)
}

type ctrlKind int

const (
	ctrlNone ctrlKind = iota
	ctrlReturn
	ctrlBreak
	ctrlContinue
)

type ctrl struct {
	kind ctrlKind
	val  any
}

// --- AST nodes ---

type exprStmt struct{ e expr }

type assignStmt struct {
	target expr
	value  expr
}

// varStmt is one declared name: `var x = e`, `let x`, `const x = e`. value
// is nil when the declaration gives none.
type varStmt struct {
	kind  declKind
	name  string
	value expr
	line  int
}

type blockStmt struct{ stmts []stmt }

// funcDeclStmt is a nested function DECLARATION — `function drawPine(x, y) {
// ... }` inside another function's body, which these canvas painters use to
// factor a repeated shape. It binds a userFunc closed over the scope it runs
// in, and is hoisted to the top of its block, as JS hoists declarations.
type funcDeclStmt struct {
	name string
	fn   funcDecl
	// hoisted: the head of the enclosing list binds it (scopeStmt), so
	// reaching the declaration itself does nothing.
	hoisted bool
}

// seqStmt runs several statements in the CURRENT scope (no child scope, so
// declarations stay visible) — `var a = [], b = []` desugars to one.
type seqStmt struct{ stmts []stmt }

type ifStmt struct {
	cond      expr
	then, els stmt
}

type forStmt struct {
	init stmt // nil for while
	cond expr
	post stmt
	body stmt
	// capture: the loop variable is a let or const and something in the
	// loop can keep a reference to it, so each iteration needs a scope of
	// its own.
	capture bool
}

// forOfStmt is `for (var name of iter) body` over an array or string, or
// `for (var name in iter) body` over an object's keys.
type forOfStmt struct {
	name string
	iter expr
	body stmt
	decl bool     // the loop declares its variable (var, let or const)
	kind declKind // which, when decl
	keys bool     // for-in: the keys, not the values
	// capture: the variable is a let or const and the body can keep a
	// reference to it (see forStmt.capture).
	capture bool
	line    int
}

type returnStmt struct{ value expr } // value may be nil

// tryStmt is JS's try/catch/finally. A thrown value (or any evaluation
// error, which is what a runtime fault looks like here) is caught and bound
// to the catch parameter as an Error-shaped object.
type tryStmt struct {
	body     []stmt
	catch    []stmt
	finally  []stmt
	param    string
	hasCatch bool
}

// throwStmt raises a value as an error.
type throwStmt struct {
	value expr
	line  int
}

// thrownError carries a thrown JS value through Go's error channel.
type thrownError struct{ v any }

func (e thrownError) Error() string { return str(e.v) }

type breakStmt struct{}

type continueStmt struct{}

// switchStmt is a JS switch: clauses run from the first matching case with
// fall-through until a break (which the switch consumes); a nil match is the
// default clause, chosen only when no case matches.
type switchClause struct {
	match expr // nil = default
	body  []stmt
}

type switchStmt struct {
	subject expr
	clauses []switchClause
}

// userFunc is a markup-declared function bound to its lexical (document)
// scope; calling it binds params in a child scope and runs the body.
type userFunc struct {
	params   []string
	defaults []expr // per-param default (nil = none)
	body     []stmt
	env      *Env
	// closure caches whether the body can capture its call scope (an
	// arrow function inside it): 0 unknown, 1 no, 2 yes. A body that
	// cannot runs in a pooled scope (see callUserFunc).
	closure int8
}

// --- execution ---

func (s *exprStmt) exec(env *Env) (ctrl, error) {
	_, err := s.e.eval(env)
	return ctrl{}, err
}

func (s *assignStmt) exec(env *Env) (ctrl, error) {
	return ctrl{}, assign(s.target, s.value, env)
}

func (s *varStmt) exec(env *Env) (ctrl, error) {
	var v any
	if s.value != nil {
		var err error
		if v, err = s.value.eval(env); err != nil {
			return ctrl{}, err
		}
	}
	if s.kind == declVar {
		// A var is the function's, not the block's (see jsscope.go), and
		// `var x` with no value leaves an x that already exists alone.
		fn := env.varScope()
		if s.value == nil {
			if _, bound := fn.vars[s.name]; bound {
				return ctrl{}, nil
			}
		}
		fn.declareLocal(s.name, v)
		return ctrl{}, nil
	}
	env.declareLocal(s.name, v) // let, const: the current (innermost) scope
	if s.kind == declConst {
		env.markConst(s.name)
	}
	return ctrl{}, nil
}

func (s *funcDeclStmt) exec(env *Env) (ctrl, error) {
	if !s.hoisted {
		s.bind(env)
	}
	return ctrl{}, nil
}

// bind makes the declared function, closed over env, and names it there.
func (s *funcDeclStmt) bind(env *Env) {
	env.vars[s.name] = &userFunc{params: s.fn.params, defaults: s.fn.defaults,
		body: s.fn.body, env: env}
}

func (s *throwStmt) exec(env *Env) (ctrl, error) {
	v, err := s.value.eval(env)
	if err != nil {
		return ctrl{}, err
	}
	return ctrl{}, thrownError{v: v}
}

func (s *tryStmt) exec(env *Env) (ctrl, error) {
	c, err := execBlock(s.body, env.blockChild())
	// A spent budget is not the page's to catch: every further step would
	// fail the same way, and a handler that swallowed it would be the
	// runaway loop the budget is there to end.
	if err != nil && s.hasCatch && !budgetSpent() {
		scope := env.blockChild()
		if s.param != "" {
			var thrown any
			if t, ok := err.(thrownError); ok {
				thrown = t.v
			} else {
				// A runtime fault: JS hands the handler an Error object, and
				// these pages read `e.message`.
				thrown = map[string]any{"message": err.Error(), "name": "Error"}
			}
			scope.vars[s.param] = thrown
		}
		c, err = execBlock(s.catch, scope)
	}
	if len(s.finally) > 0 {
		// finally runs either way; its own return/break wins, as in JS.
		fc, ferr := execBlock(s.finally, env.blockChild())
		if ferr != nil {
			return fc, ferr
		}
		if fc.kind != ctrlNone {
			return fc, nil
		}
	}
	return c, err
}

func (s *blockStmt) exec(env *Env) (ctrl, error) {
	return execBlock(s.stmts, env.blockChild())
}

func (s *seqStmt) exec(env *Env) (ctrl, error) {
	return execBlock(s.stmts, env)
}

func (s *ifStmt) exec(env *Env) (ctrl, error) {
	ok, err := truthy(s.cond, env)
	if err != nil {
		return ctrl{}, err
	}
	if ok {
		return s.then.exec(env)
	}
	if s.els != nil {
		return s.els.exec(env)
	}
	return ctrl{}, nil
}

func (s *forStmt) exec(env *Env) (ctrl, error) {
	iters := 0
	budgetEnter()
	defer func() { budgetLoopEnd(iters) }()
	scope := env.blockChild()
	if s.init != nil {
		if _, err := s.init.exec(scope); err != nil {
			return ctrl{}, err
		}
	}
	for {
		// Every iteration is a step of the shared budget (budget.go), which
		// is what ends `while (true) {}`.
		if err := budgetLoopStep(&iters); err != nil {
			return ctrl{}, err
		}
		if s.cond != nil {
			ok, err := truthy(s.cond, scope)
			if err != nil {
				return ctrl{}, err
			}
			if !ok {
				break
			}
		}
		c, err := s.body.exec(scope)
		if err != nil {
			return ctrl{}, err
		}
		switch c.kind {
		case ctrlReturn:
			return c, nil
		case ctrlBreak:
			return ctrl{}, nil
		}
		// `for (let i ...)` with a closure in it: the next iteration gets
		// its own copy of the loop's variables, made before the increment,
		// so a function created this time round keeps this i.
		if s.capture {
			scope = scope.iteration()
		}
		if s.post != nil {
			if _, err := s.post.exec(scope); err != nil {
				return ctrl{}, err
			}
		}
	}
	return ctrl{}, nil
}

func (s *forOfStmt) exec(env *Env) (ctrl, error) {
	v, err := s.iter.eval(env)
	if err != nil {
		return ctrl{}, err
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if m, ok := v.(*jsMatch); ok {
		v = m.items
	}
	var items []any
	switch it := v.(type) {
	case []any:
		if s.keys { // for-in over an array: its indices, as strings
			for i := range it {
				items = append(items, strconv.Itoa(i))
			}
		} else {
			items = it
		}
	case string:
		for _, r := range it {
			items = append(items, string(r))
		}
	case map[string]any:
		if !s.keys {
			return ctrl{}, nil // an object is not iterable with for-of
		}
		// The keys, sorted: a Go map has no order to keep (JS gives them
		// in the order they were added).
		for _, k := range sortedKeys(it) {
			items = append(items, k)
		}
	default:
		return ctrl{}, nil // non-iterable: no iterations (lenient)
	}
	iters := 0
	budgetEnter()
	defer func() { budgetLoopEnd(iters) }()
	// Where the loop variable lives: a var in the function, a let or
	// const in the loop's own scope, an undeclared name wherever it
	// already is (and in the loop's scope when it is nowhere).
	scope := env.blockChild()
	target := scope
	switch {
	case s.decl && s.kind == declVar:
		target = env.varScope()
	case !s.decl:
		if found := env.findScope(s.name); found != nil {
			target = found
		}
	}
	for _, item := range items {
		if err := budgetLoopStep(&iters); err != nil {
			return ctrl{}, err
		}
		if s.capture {
			scope = env.blockChild() // this iteration's own binding
			target = scope
		}
		if target != scope {
			if err := assignIn(target, s.name, item, s.line); err != nil {
				return ctrl{}, err
			}
		} else {
			scope.declareLocal(s.name, item)
			if s.kind == declConst && s.decl {
				scope.markConst(s.name)
			}
		}
		c, err := s.body.exec(scope)
		if err != nil {
			return ctrl{}, err
		}
		switch c.kind {
		case ctrlReturn:
			return c, nil
		case ctrlBreak:
			return ctrl{}, nil
		}
	}
	return ctrl{}, nil
}

func (s *returnStmt) exec(env *Env) (ctrl, error) {
	if s.value == nil {
		return ctrl{kind: ctrlReturn}, nil
	}
	v, err := s.value.eval(env)
	if err != nil {
		return ctrl{}, err
	}
	return ctrl{kind: ctrlReturn, val: v}, nil
}

func (s *breakStmt) exec(*Env) (ctrl, error)    { return ctrl{kind: ctrlBreak}, nil }
func (s *continueStmt) exec(*Env) (ctrl, error) { return ctrl{kind: ctrlContinue}, nil }

func (s *switchStmt) exec(env *Env) (ctrl, error) {
	v, err := s.subject.eval(env)
	if err != nil {
		return ctrl{}, err
	}
	start := -1
	for i, c := range s.clauses {
		if c.match == nil {
			continue
		}
		mv, err := c.match.eval(env)
		if err != nil {
			return ctrl{}, err
		}
		if looseEq(v, mv) {
			start = i
			break
		}
	}
	if start < 0 { // no case matched: the default clause, wherever declared
		for i, c := range s.clauses {
			if c.match == nil {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return ctrl{}, nil
	}
	scope := env.blockChild()
	for i := start; i < len(s.clauses); i++ { // fall through until break
		for _, st := range s.clauses[i].body {
			c, err := st.exec(scope)
			if err != nil {
				return ctrl{}, err
			}
			switch c.kind {
			case ctrlBreak:
				return ctrl{}, nil // the switch consumes its break
			case ctrlReturn, ctrlContinue:
				return c, nil
			}
		}
	}
	return ctrl{}, nil
}

// looseEq is JS == for the value kinds expressions produce here: strings
// compare as strings, bools coerce the other side, everything else numeric.
func looseEq(a, b any) bool {
	a, b = plainColor(a), plainColor(b)
	if nullish(a) || nullish(b) {
		// A switch compares strictly: `case null` does not take undefined.
		return jsKind(a) == jsKind(b)
	}
	if as, ok := a.(string); ok {
		return as == str(b)
	}
	if bs, ok := b.(string); ok {
		return str(a) == bs
	}
	if ab, ok := a.(bool); ok {
		bb, err := toBool(b)
		return err == nil && ab == bb
	}
	af, errA := toFloat64(a)
	bf, errB := toFloat64(b)
	return errA == nil && errB == nil && af == bf
}

// execBlock runs statements in env until one signals control flow.
func execBlock(stmts []stmt, env *Env) (ctrl, error) {
	for _, s := range stmts {
		c, err := s.exec(env)
		if err != nil {
			return ctrl{}, err
		}
		if c.kind != ctrlNone {
			return c, nil
		}
	}
	return ctrl{}, nil
}

// runStmts executes a handler body in a fresh child scope (so var-declared
// locals don't leak into the document scope).
func runStmts(stmts []stmt, env *Env) error {
	// One handler is one activation: its loops share a budget.
	budgetEnter()
	defer budgetLeave()
	_, err := execBlock(stmts, env.child())
	return err
}

// assign writes value to target: a local variable in the scope chain, a
// declared property (Ref), or an object member (e.g. input.text).
func assign(target, value expr, env *Env) error {
	v, err := value.eval(env)
	if err != nil {
		return err
	}
	return assignValue(target, v, env, false)
}

// assignIn writes a variable in the scope that holds it: through a Ref,
// and never into a const or a let that is still in its dead zone.
func assignIn(scope *Env, name string, v any, line int) error {
	switch cur := scope.vars[name].(type) {
	case deadZone: // before Ref: it is one
		return deadZoneError(name, line)
	case Ref:
		return cur.set(v)
	}
	if scope.consts[name] {
		if line > 0 {
			return fmt.Errorf("gml:%d: assignment to constant variable %q", line, name)
		}
		return fmt.Errorf("gml: assignment to constant variable %q", name)
	}
	scope.vars[name] = v
	return nil
}

// assignValue is assign with the value in hand. mutated says the write
// stores back an array a mutator changed (`xs.push(1)`): arrays are Go
// slices here, so the binding is rewritten, but it is the same array as
// far as the page can tell and a const may hold it.
func assignValue(target expr, v any, env *Env, mutated bool) error {
	if id, ok := target.(*identExpr); ok {
		if scope := env.findScope(id.name); scope != nil {
			if mutated && scope.consts[id.name] {
				scope.vars[id.name] = v
				return nil
			}
			return assignIn(scope, id.name, v, id.line)
		}
	}
	// Element / key assignment: a[i] = v mutates the array or object in place.
	// (The container is a reference type; a var box's pointer is unchanged, so
	// this does not by itself re-fire reactive bindings on the var.)
	if ix, ok := target.(*indexExpr); ok {
		recv, err := ix.recv.eval(env)
		if err != nil {
			return err
		}
		idx, err := ix.index.eval(env)
		if err != nil {
			return err
		}
		switch c := recv.(type) {
		case []any:
			i, err := toInt(idx)
			if err != nil {
				return err
			}
			if i < 0 || i >= len(c) {
				return fmt.Errorf("gml: index %d out of range (len %d)", i, len(c))
			}
			c[i] = v
			return nil
		case map[string]any:
			c[str(idx)] = v
			return nil
		}
		return fmt.Errorf("gml: cannot index-assign %T", recv)
	}
	ref, err := resolveRef(target, env)
	if err == nil {
		return ref.set(v)
	}
	// A field of a plain JS object (a parsed-JSON value, a `{...}` literal
	// held in a var): `cols[c].chars = [...]`. Objects are reference types,
	// so the key is written in place — no Ref, and nothing to write back
	// through the expression it came from. Element ids never land here:
	// they resolve as Refs above.
	if m, ok := target.(*memberExpr); ok {
		if base, berr := m.base.eval(env); berr == nil {
			if r, isRef := base.(Ref); isRef {
				base = r.get()
			}
			if obj, isMap := base.(map[string]any); isMap {
				obj[m.name] = v
				return nil
			}
			// `re.lastIndex = 0`.
			if re, isRe := base.(*jsRegExp); isRe && re.setMember(m.name, v) {
				return nil
			}
		}
	}
	return err
}

func truthy(e expr, env *Env) (bool, error) {
	v, err := e.eval(env)
	if err != nil {
		return false, err
	}
	return toBool(v)
}

// callUserFunc binds args into a child of the function's closure scope and
// runs the body, returning the value of any `return`.
// callAny invokes an already-resolved callable value with plain arguments —
// for the places that hold a function value rather than a call expression
// (Qt.callLater, a signal connected to a handler).
func callAny(fn any, args []any) (any, error) {
	switch f := fn.(type) {
	case jsFunc:
		return f(args)
	case func([]any) (any, error):
		return f(hostArgs(args))
	case *userFunc:
		return callUserFunc(f, args)
	}
	return nil, fmt.Errorf("gml: value of type %T is not callable", fn)
}

// maxCallDepth is how deep a page's functions may call one another. The
// evaluator recurses on the Go stack, and a Go stack overflow cannot be
// recovered: a function that calls itself for ever must be an error from
// the script, not the end of the host.
const maxCallDepth = 1000

// The depth itself is counted in budgetState (budget.go), beside the
// steps: the engine belongs to one goroutine at a time, and it is atomic
// so that a second document on another does not corrupt it, only share
// the budget.

func callUserFunc(fn *userFunc, args []any) (any, error) {
	// One atomic add takes the call a level deeper and charges it as a
	// step of the shared budget (budget.go): depth alone does not bound
	// two functions that call each other for ever without going deep.
	state := budgetState.Add(budgetCall)
	if state&budgetDepthMask > maxCallDepth {
		budgetState.Add(-1)
		return nil, fmt.Errorf("gml: functions nested deeper than %d calls (a function that calls itself without end?)", maxCallDepth)
	}
	// A call from Go with nothing else running is an activation of its own.
	outermost := state&budgetDepthMask == 1 && budgetNest.Load() == 0
	if outermost {
		budgetBegin()
	}
	defer leaveCall(outermost)
	if left := state >> budgetDepthBits; left&(budgetCheckEvery-1) == 0 || left < 0 {
		if err := budgetCheck(left); err != nil {
			return nil, err
		}
	}
	// The call scope is pooled when nothing in the body can keep a
	// reference to it — no arrow function that would close over it. The
	// corpus calls `Style.resize(x)` (an identity function) for nearly
	// every size on every page, and a fresh map per call was the largest
	// allocation left in a rebuilt frame.
	if fn.closure == 0 {
		fn.closure = 1
		if stmtsHaveClosure(fn.body) || exprsHaveClosure(fn.defaults) {
			fn.closure = 2
		}
	}
	var scope *Env
	if fn.closure == 1 {
		scope = scopePool.Get().(*Env)
		scope.parent = fn.env
		defer func() {
			clear(scope.vars)
			scope.parent = nil
			scope.consts = nil
			scopePool.Put(scope)
		}()
	} else {
		scope = fn.env.child()
	}
	// Parameters are the caller's already-evaluated arguments — whatever
	// they depended on was recorded where they were computed — so reading
	// one back is not a mutable read (see Env.derived). Without this every
	// `Style.resize(40)` in the corpus poisoned the subtree that called it.
	scope.derived = true
	for i, p := range fn.params {
		switch {
		case i < len(args) && args[i] != nil:
			scope.vars[p] = args[i]
		case i < len(fn.defaults) && fn.defaults[i] != nil:
			v, err := fn.defaults[i].eval(scope)
			if err != nil {
				return nil, err
			}
			scope.vars[p] = v
		default:
			scope.vars[p] = nil
		}
	}
	c, err := execBlock(fn.body, scope)
	if err != nil {
		return nil, err
	}
	if c.kind == ctrlReturn {
		return c.val, nil
	}
	return nil, nil
}

// leaveCall undoes callUserFunc's entry: a level shallower, or — when the
// call was the outermost thing running — the budget made whole again
// (which also puts the depth back to zero).
func leaveCall(outermost bool) {
	if outermost {
		budgetRefill()
		return
	}
	budgetState.Add(-1)
}

// scopePool recycles the call scopes of closure-free user functions.
var scopePool = sync.Pool{New: func() any { return &Env{vars: make(map[string]any, 4), derived: true} }}
