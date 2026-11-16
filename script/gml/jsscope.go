package gml

import (
	"fmt"
	"maps"
	"time"
)

// var, let and const.
//
// A `var` belongs to the FUNCTION it is written in — a handler, a block
// binding, a function — wherever in it the declaration stands, and exists
// (undefined) from the function's start. A `let` or `const` belongs to the
// BLOCK it is written in, from the block's start, but may not be read or
// written before its declaration has run (its "temporal dead zone"); a
// const may not be assigned after. Function declarations are bound at the
// start of their block.
//
// The parser works out, per statement list, what has to exist before the
// first statement runs, and puts it in a scopeStmt at the head of the list
// — only where there is something to do, so a list that declares nothing
// (most of them) runs exactly as it did. At run time a scope knows whether
// it is a block's (Env.block), which is how a `var` finds its function.

// declKind is which keyword declared a variable.
type declKind uint8

const (
	declVar declKind = iota
	declLet
	declConst
)

func declKindOf(keyword string) declKind {
	switch keyword {
	case "let":
		return declLet
	case "const":
		return declConst
	}
	return declVar
}

func (k declKind) String() string {
	switch k {
	case declLet:
		return "let"
	case declConst:
		return "const"
	}
	return "var"
}

// deadZone is what a let or const holds between the start of its block and
// its declaration. Reading it is an error (identExpr.eval), writing it too
// (assignIn).
//
// It is a Ref — one that reads as undefined and refuses writes — so that
// the one type test every identifier read already makes (is this a Ref?)
// finds it: a separate test for it on every read of a plain value cost
// the arithmetic benchmark 7 %.
type deadZone struct{}

var inDeadZone any = deadZone{}

func (deadZone) get() any { return nil }
func (deadZone) set(any) error {
	return fmt.Errorf("gml: cannot access a variable before initialization")
}
func (deadZone) animate(any, time.Duration) error {
	return fmt.Errorf("gml: cannot access a variable before initialization")
}
func (deadZone) onChange(func()) {}

func deadZoneError(name string, line int) error {
	if line > 0 {
		return fmt.Errorf("gml:%d: cannot access %q before initialization", line, name)
	}
	return fmt.Errorf("gml: cannot access %q before initialization", name)
}

// blockChild is a nested scope for a block: a `var` declared in it
// belongs to the function outside.
func (e *Env) blockChild() *Env { return &Env{vars: map[string]any{}, parent: e, block: true} }

// varScope is the scope a `var` declared here lands in: the nearest one
// that is not a block's.
func (e *Env) varScope() *Env {
	s := e
	for s.block && s.parent != nil {
		s = s.parent
	}
	return s
}

// markConst makes a binding of this scope unassignable.
func (e *Env) markConst(name string) {
	if e.consts == nil {
		e.consts = map[string]bool{}
	}
	e.consts[name] = true
}

// iteration is a copy of a loop's scope for the next time round, so a
// closure made in one iteration keeps that iteration's variables.
func (e *Env) iteration() *Env {
	next := &Env{vars: maps.Clone(e.vars), parent: e.parent, block: e.block, derived: e.derived}
	if e.consts != nil {
		next.consts = maps.Clone(e.consts)
	}
	if e.derivedNames != nil {
		next.derivedNames = maps.Clone(e.derivedNames)
	}
	return next
}

// scopeStmt stands at the head of a statement list and sets its scope up
// before the first real statement runs.
type scopeStmt struct {
	// vars are the `var`s of the whole function (through its nested
	// blocks, not its nested functions): declared undefined where not
	// already bound — a parameter keeps its value.
	vars []string
	// lets are the let and const names of this list: in their dead zone.
	lets []string
	// funcs are this list's function declarations: bound now, so a call
	// above the declaration finds it.
	funcs []*funcDeclStmt
}

func (s *scopeStmt) exec(env *Env) (ctrl, error) {
	for _, f := range s.funcs {
		f.bind(env)
	}
	for _, name := range s.lets {
		env.vars[name] = inDeadZone
	}
	if len(s.vars) > 0 {
		fn := env.varScope()
		for _, name := range s.vars {
			if _, bound := fn.vars[name]; !bound {
				fn.declareLocal(name, nil)
			}
		}
	}
	return ctrl{}, nil
}

// scopeOf checks a parsed statement list's declarations and returns it
// with a scopeStmt at its head if it needs one. fn marks a function's
// whole body, whose vars are declared on entry; params are the function's
// parameters when its statements share their scope.
func scopeOf(list []stmt, fn bool, params []string) ([]stmt, error) {
	if err := checkRedeclared(list, params); err != nil {
		return nil, err
	}
	hdr := &scopeStmt{}
	for _, s := range list {
		switch t := s.(type) {
		case *varStmt:
			if t.kind != declVar {
				hdr.lets = append(hdr.lets, t.name)
			}
		case *seqStmt:
			for _, d := range t.stmts {
				if v, ok := d.(*varStmt); ok && v.kind != declVar {
					hdr.lets = append(hdr.lets, v.name)
				}
			}
		case *funcDeclStmt:
			hdr.funcs = append(hdr.funcs, t)
		}
	}
	if fn {
		seen := map[string]bool{}
		for _, v := range hoistedVars(list, nil) {
			if !seen[v.name] {
				seen[v.name] = true
				hdr.vars = append(hdr.vars, v.name)
			}
		}
	}
	if len(hdr.vars)+len(hdr.lets)+len(hdr.funcs) == 0 {
		return list, nil
	}
	for _, f := range hdr.funcs {
		f.hoisted = true
	}
	return append([]stmt{hdr}, list...), nil
}

// declared is one declaration found in a statement list.
type declared struct {
	name string
	line int
}

// hoistedVars appends the `var` declarations of a statement list, through
// its nested blocks and loops but not its nested functions.
func hoistedVars(list []stmt, out []declared) []declared {
	for _, s := range list {
		out = hoistedVarsOf(s, out)
	}
	return out
}

func hoistedVarsOf(s stmt, out []declared) []declared {
	switch t := s.(type) {
	case *varStmt:
		if t.kind == declVar {
			out = append(out, declared{t.name, t.line})
		}
	case *seqStmt:
		out = hoistedVars(t.stmts, out)
	case *blockStmt:
		out = hoistedVars(t.stmts, out)
	case *ifStmt:
		out = hoistedVarsOf(t.then, out)
		if t.els != nil {
			out = hoistedVarsOf(t.els, out)
		}
	case *forStmt:
		if t.init != nil {
			out = hoistedVarsOf(t.init, out)
		}
		out = hoistedVarsOf(t.body, out)
	case *forOfStmt:
		if t.decl && t.kind == declVar {
			out = append(out, declared{t.name, t.line})
		}
		out = hoistedVarsOf(t.body, out)
	case *tryStmt:
		out = hoistedVars(t.body, out)
		out = hoistedVars(t.catch, out)
		out = hoistedVars(t.finally, out)
	case *switchStmt:
		for _, c := range t.clauses {
			out = hoistedVars(c.body, out)
		}
	}
	return out
}

// checkRedeclared refuses a statement list that declares a name twice in
// its own scope — two lets, a let and a var that reaches this scope, a
// let and a function, a let and a parameter — which JS refuses before
// running anything (a SyntaxError). Two vars are fine.
func checkRedeclared(list []stmt, params []string) error {
	lets := map[string]bool{}
	again := func(name string, line int) error {
		return fmt.Errorf("gml:%d: %q has already been declared", line, name)
	}
	lexical := func(v *varStmt) error {
		if v.kind == declVar {
			return nil
		}
		if lets[v.name] {
			return again(v.name, v.line)
		}
		lets[v.name] = true
		return nil
	}
	for _, s := range list {
		switch t := s.(type) {
		case *varStmt:
			if err := lexical(t); err != nil {
				return err
			}
		case *seqStmt:
			for _, d := range t.stmts {
				if v, ok := d.(*varStmt); ok {
					if err := lexical(v); err != nil {
						return err
					}
				}
			}
		}
	}
	if len(lets) == 0 {
		return nil
	}
	for _, v := range hoistedVars(list, nil) {
		if lets[v.name] {
			return again(v.name, v.line)
		}
	}
	for _, s := range list {
		if f, ok := s.(*funcDeclStmt); ok && lets[f.name] {
			return again(f.name, f.fn.line)
		}
	}
	for _, p := range params {
		if lets[p] {
			return fmt.Errorf("gml: %q has already been declared (it is a parameter)", p)
		}
	}
	return nil
}
