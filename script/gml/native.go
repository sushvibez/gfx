package gml

// Go-registered QML types — the qmlRegisterType equivalent. An app
// registers a factory for a type name; markup then instantiates it like
// any element:
//
//	gml.RegisterType("WebSocketClient", func(emit gml.EmitFunc) *gml.NativeObject {
//	    connected := &core.Property[bool]{}
//	    return &gml.NativeObject{
//	        Props: map[string]any{"connected": gml.BindProp(connected)},
//	        Methods: map[string]any{
//	            "connectToServer": func() { connected.Set(true); emit("connected") },
//	        },
//	    }
//	})
//
//	WebSocketClient {
//	    id: ws
//	    onMessageReceived: function(message) { log.insert(0, {text: message}) }
//	}
//
// Properties backed by core.Property (via BindProp) are live bindings;
// methods are invokable through the element's id (ws.connectToServer());
// emit fires the element's on<Signal> handlers with the arguments bound.
// Emit is safe from any goroutine: emissions queue and drain on the UI
// thread at the start of each frame. Instances persist across the
// per-frame rebuilds, keyed by the element's id (one instance per id).

import (
	"strings"
	"sync"
	"time"

	"github.com/sushvibez/gfx/core"
)

// NativeObject is what a registered type's factory returns.
type NativeObject struct {
	// Props exposes bindable state: values created with BindProp are live
	// (a Set on the Go side re-renders dependents); plain values are
	// static. Markup attribute assignments write into Refs.
	Props map[string]any
	// Methods exposes invokables; supported shapes: func(),
	// func(float64), func(string), func(float64, float64),
	// func([]any) (any, error).
	Methods map[string]any
}

// EmitFunc fires one of the element's signals (its on<Name> handler).
// Callable from any goroutine.
type EmitFunc func(signal string, args ...any)

// BindProp adapts a core.Property so markup bindings read it live and
// attribute assignments write through it.
func BindProp[T comparable](p *core.Property[T], conv func(any) (T, error)) Ref {
	return propRef[T]{p, conv, nil}
}

// BindBool, BindString, BindFloat are the common BindProp shapes.
func BindBool(p *core.Property[bool]) Ref { return propRef[bool]{p, toBool, nil} }
func BindString(p *core.Property[string]) Ref {
	return propRef[string]{p, func(v any) (string, error) { return str(v), nil }, nil}
}
func BindFloat(p *core.Property[float32]) Ref {
	return propRef[float32]{p, toFloat32, func(to float32, d time.Duration) { core.Tween(p, to, d, nil) }}
}

// VarProp is a live `property var` for Go backends: an array or object
// whose CONTENTS change (a task's results, a recent-files list). Markup
// reads re-evaluate whenever Set is called, which a plain Go slice cannot
// do — the box gives the property pointer identity to compare on.
type VarProp struct{ p core.Property[*varBox] }

// NewVarProp creates a var property holding v.
func NewVarProp(v any) *VarProp {
	vp := &VarProp{}
	vp.p.Set(&varBox{v})
	return vp
}

// Set replaces the value, marking markup that read it dirty.
func (v *VarProp) Set(x any) { v.p.Set(&varBox{x}) }

// Get returns the current value.
func (v *VarProp) Get() any {
	if b := v.p.Get(); b != nil {
		return b.v
	}
	return nil
}

// BindVar exposes a VarProp to markup (the var counterpart of BindBool /
// BindString / BindFloat).
func BindVar(v *VarProp) Ref { return varRef{&v.p} }

// Post queues fn to run on the UI thread at the start of the next frame,
// alongside queued signal emissions. Properties and models are single-owner
// (the event-loop goroutine), so a backend's worker goroutine must hand its
// updates over rather than writing them directly.
func Post(fn func()) {
	postedMu.Lock()
	posted = append(posted, fn)
	postedMu.Unlock()
}

var (
	postedMu sync.Mutex
	posted   []func()
)

// drainPosted runs everything Post queued. Returns whether anything ran, so
// the caller can invalidate its build cache.
func drainPosted() bool {
	postedMu.Lock()
	q := posted
	posted = nil
	postedMu.Unlock()
	for _, fn := range q {
		fn()
	}
	return len(q) > 0
}

// nativeTypes is the process-global registry (like qmlRegisterType).
var (
	nativeMu    sync.RWMutex
	nativeTypes = map[string]func(EmitFunc) *NativeObject{}
)

// RegisterType registers a Go-implemented QML element type. Call before
// loading documents that use the type.
func RegisterType(name string, factory func(EmitFunc) *NativeObject) {
	nativeMu.Lock()
	nativeTypes[name] = factory
	nativeMu.Unlock()
}

// isNativeType reports whether a type name has a registered factory.
func isNativeType(name string) bool {
	nativeMu.RLock()
	_, ok := nativeTypes[name]
	nativeMu.RUnlock()
	return ok
}

// emission is one queued signal fire.
type emission struct {
	signal string
	args   []any
}

// nativeInst is one live instance of a registered type.
type nativeInst struct {
	obj      *NativeObject
	handlers map[string]handlerRef // signal name -> latest on<Name> handler
	// pending are initial property attributes that did not resolve yet —
	// typically a forward reference to an element declared further down the
	// document (`url: connectionCard.url`). They are retried on later
	// frames and reported only if they never resolve.
	pending map[string]int

	mu    sync.Mutex
	queue []emission
}

type handlerRef struct {
	stmts  []stmt
	params []string // set when sourced from a Connections named function
	env    *Env
}

// syncNative creates (once) or refreshes a registered-type element:
// initial property attributes apply at creation; on<Signal> handlers
// re-capture every rebuild so hot-reloaded markup stays wired.
func (doc *docState) syncNative(n *node, env *Env) {
	id := nodeID(n)
	if id == "" {
		logf("gml:%d: %s needs an id (Go-registered type)", n.line, n.typeName)
		return
	}
	if doc.nativeInsts == nil {
		doc.nativeInsts = map[string]*nativeInst{}
	}
	inst := doc.nativeInsts[id]
	created := false
	if inst == nil {
		nativeMu.RLock()
		factory := nativeTypes[n.typeName]
		nativeMu.RUnlock()
		inst = &nativeInst{handlers: map[string]handlerRef{}}
		inst.obj = factory(func(signal string, args ...any) {
			inst.mu.Lock()
			inst.queue = append(inst.queue, emission{signal, args})
			inst.mu.Unlock()
		})
		doc.nativeInsts[id] = inst
		created = true
	}
	// Expose props + methods through the element's id.
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		obj = &elemObject{}
		doc.elemObjs.set(doc.intern(id), obj)
	}
	obj.props = inst.obj.Props
	obj.methods = inst.obj.Methods
	env.vars[id] = obj

	// Connections { target: <this id>; function on<Signal>(...) {...} } —
	// the QML-idiomatic alternative to an inline onSignal: attribute.
	// Consulted second, so an inline attribute on the element itself wins.
	for sig, fd := range doc.connections[id] {
		name := lowerFirst(strings.TrimPrefix(sig, "on"))
		if _, has := inst.handlers[name]; !has {
			inst.handlers[name] = handlerRef{stmts: fd.body, params: fd.params, env: env}
		}
	}

	for _, a := range n.attrs {
		switch {
		case a.name == "id":
		case len(a.name) > 2 && strings.HasPrefix(a.name, "on"):
			sig := lowerFirst(strings.TrimPrefix(a.name, "on"))
			inst.handlers[sig] = handlerRef{stmts: a.stmt, env: env}
		case created || inst.pending[a.name] > 0:
			if a.expr == nil {
				continue
			}
			// Initial property values (creation only — the Go side owns the
			// state afterwards), plus a retry for the ones whose expression
			// could not resolve yet.
			v, err := a.expr.eval(env)
			if err != nil {
				if inst.pending == nil {
					inst.pending = map[string]int{}
				}
				inst.pending[a.name]++
				if inst.pending[a.name] == 3 {
					logf("gml:%d: %s.%s: %v", a.line, n.typeName, a.name, err)
				}
				continue
			}
			delete(inst.pending, a.name)
			if ref, ok := inst.obj.Props[a.name].(Ref); ok {
				if err := ref.set(v); err != nil {
					logf("gml:%d: %s.%s: %v", a.line, n.typeName, a.name, err)
				}
			} else {
				inst.obj.Props[a.name] = v
			}
		}
	}
}

// queueNativeSignal queues an emission for the element with this id, from
// any goroutine — the shared path for Go-implemented data and painted
// types. Handlers run on the UI thread at the start of the next frame.
func (doc *docState) queueNativeSignal(id, signal string, args []any) {
	if id == "" {
		return
	}
	if doc.nativeInsts == nil {
		doc.nativeInsts = map[string]*nativeInst{}
	}
	inst := doc.nativeInsts[id]
	if inst == nil {
		inst = &nativeInst{handlers: map[string]handlerRef{}}
		doc.nativeInsts[id] = inst
	}
	inst.mu.Lock()
	inst.queue = append(inst.queue, emission{signal, args})
	inst.mu.Unlock()
}

// drainNativeSignals runs queued signal handlers (UI thread, per frame).
// Returns whether any handler ran (callers invalidate the build cache —
// handlers mutate models and properties).
func (doc *docState) drainNativeSignals() bool {
	ran := drainPosted()
	for _, inst := range doc.nativeInsts {
		inst.mu.Lock()
		q := inst.queue
		inst.queue = nil
		inst.mu.Unlock()
		for _, e := range q {
			h, ok := inst.handlers[e.signal]
			if !ok || len(h.stmts) == 0 {
				continue
			}
			ran = true
			// A signal handler resolves the element's own properties and
			// methods by bare name (`onIsOpenChanged: if (isOpen) ...`),
			// like QML's object scope.
			h.env = nativeSelfScope(h.env, inst.obj)
			callHandler(h, e.args)
		}
	}
	return ran
}

// callHandler invokes an on<Signal> handler. QML's common form binds the
// arguments through a function expression (`onFoo: function(a, b) {...}`
// or an arrow); a body that is exactly one function expression is called
// with the signal args, anything else runs as plain statements.
func callHandler(h handlerRef, args []any) {
	if h.params != nil {
		// Sourced from a Connections named function: real parameter names.
		fn := &userFunc{params: h.params, body: h.stmts, env: h.env}
		if _, err := callUserFunc(fn, args); err != nil {
			logf("gml: signal handler: %v", err)
		}
		return
	}
	if len(h.stmts) == 1 {
		if es, ok := h.stmts[0].(*exprStmt); ok {
			if fe, feOk := es.e.(*funcExpr); feOk {
				fn := &userFunc{params: fe.params, defaults: fe.defaults, body: fe.body, env: h.env}
				if _, err := callUserFunc(fn, args); err != nil {
					logf("gml: signal handler: %v", err)
				}
				return
			}
		}
	}
	if err := runStmts(h.stmts, h.env); err != nil {
		logf("gml: signal handler: %v", err)
	}
}

// nativeSelfScope layers a backend object's properties and methods over a
// handler's captured scope, so bare names inside the handler mean "mine".
func nativeSelfScope(env *Env, obj *NativeObject) *Env {
	if obj == nil || (len(obj.Props) == 0 && len(obj.Methods) == 0) {
		return env
	}
	scope := env.child()
	for name, v := range obj.Props {
		scope.vars[name] = v
	}
	for name, fn := range obj.Methods {
		if _, taken := scope.vars[name]; !taken {
			scope.vars[name] = fn
		}
	}
	return scope
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
