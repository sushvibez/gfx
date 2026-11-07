package gml

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// Env is the document's scope: properties, state objects and functions the
// Go side exposes to markup. The same Env is read every frame, so markup
// expressions are live bindings by construction.
type Env struct {
	vars   map[string]any
	parent *Env // scope chain: function/handler/block locals shadow the document
	// derived marks a scope whose plain values were COMPUTED from tracked
	// inputs during this build (an item's own width/height expressions,
	// see selfGeomScope) rather than injected from outside the property
	// system. Reading one is not a mutable read, so it does not poison
	// the subtree's read-set — which every element that mentioned its own
	// width for a radius was doing, and no styled control ever cached.
	derived bool
	// frozen marks the builtins root: constant for the life of the
	// document, never bound into, safe to share by reference.
	frozen bool
	// derivedNames marks individual names bound into an existing scope
	// the same way — a styled control's contract values (text, padding,
	// implicit sizes), installed into the enclosing env for the style's
	// expressions and removed again after (see installTemplateProps).
	derivedNames map[string]bool
	// derivedTok gives a derived value the TOKEN a read of it depends on
	// (a scroll state's, an item's geometry): the value was computed
	// outside the reader's read-set — in shadowSelfGeometry, ahead of a
	// property binding's TrackReads — so the read has to record it itself,
	// or `page: Math.round(contentX / 200)` never re-evaluates.
	derivedTok map[string]*core.Property[uint64]
}

// declareLocal binds a JS local — a `var`/`let`/`const`, a loop
// variable, a function parameter, a catch binding — and marks it
// DERIVED, so reading it later does not poison the reader's read-set.
//
// It cannot: a local is computed inside the very computation being
// tracked, from inputs that were recorded (or that poisoned it) as they
// were read. Reading the result back adds nothing the read-set does not
// already know. Treating it as an injected value instead meant that
// `var r = cx - Style.resize(4)` in a Canvas handler made every
// subtree that used r uncacheable — 28 reads a frame on one corpus
// page, which was the whole of it.
func (e *Env) declareLocal(name string, v any) {
	e.vars[name] = v
	if e.derived {
		return
	}
	if e.derivedNames == nil {
		e.derivedNames = map[string]bool{}
	}
	e.derivedNames[name] = true
}

// noteDerivedRead records the token behind a derived name, if any.
func (e *Env) noteDerivedRead(name string) {
	if t := e.derivedTok[name]; t != nil {
		t.Get()
	}
}

// isDerived reports whether name, found in this scope, is a derived value.
func (e *Env) isDerived(name string) bool { return e.derived || e.derivedNames[name] }

// lookupOwner is lookup that also reports the scope the value came from.
func (e *Env) lookupOwner(name string) (any, *Env, bool) {
	for s := e; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, s, true
		}
	}
	return nil, nil, false
}

func NewEnv() *Env {
	vars := map[string]any{
		"Math":    mathNS(),
		"console": consoleNS(),
		"JSON":    jsonNS(),
		// `new Date()` — a date object with the JS getters code actually
		// calls, plus the epoch milliseconds under "time" so Qt.formatTime
		// and arithmetic keep working on it.
		"Date": func(args []any) (any, error) { return dateObject(jsDate(args)), nil },
		// JS conversion globals.
		"Number": func(args []any) (any, error) {
			if len(args) == 0 {
				return float64(0), nil
			}
			f, err := toFloat32(args[0])
			if err != nil {
				return math.NaN(), nil
			}
			return float64(f), nil
		},
		"String": stringGlobal(),
		// RegExp(source, flags), with or without `new`.
		"RegExp": func(args []any) (any, error) {
			if len(args) > 0 {
				if re, ok := args[0].(*jsRegExp); ok {
					return re, nil
				}
			}
			src, flags := "", ""
			if len(args) > 0 {
				src = str(args[0])
			}
			if len(args) > 1 {
				flags = str(args[1])
			}
			return newRegExp(src, flags)
		},
		// Qt's translation functions. There are no translation catalogues,
		// so each returns its source text: qsTr(text, disambiguation, n)
		// with %n replaced by n, qsTranslate(context, text, …) the same,
		// qsTrId(id) the id; QT_TR_NOOP and friends mark text unchanged.
		"qsTr":              func(args []any) (any, error) { return trText(args, 0), nil },
		"qsTranslate":       func(args []any) (any, error) { return trText(args, 1), nil },
		"qsTrId":            func(args []any) (any, error) { return trText(args, 0), nil },
		"QT_TR_NOOP":        func(args []any) (any, error) { return trText(args, 0), nil },
		"QT_TRANSLATE_NOOP": func(args []any) (any, error) { return trText(args, 1), nil },
		"QT_TRID_NOOP":      func(args []any) (any, error) { return trText(args, 0), nil },
		"Boolean": func(args []any) (any, error) {
			if len(args) == 0 {
				return false, nil
			}
			b, err := toBool(args[0])
			return b && err == nil, nil
		},
		"parseInt": func(args []any) (any, error) {
			if len(args) == 0 {
				return math.NaN(), nil
			}
			f, err := toFloat32(args[0])
			if err != nil {
				return math.NaN(), nil
			}
			return float64(int(f)), nil
		},
		"parseFloat": func(args []any) (any, error) {
			if len(args) == 0 {
				return math.NaN(), nil
			}
			f, err := toFloat32(args[0])
			if err != nil {
				return math.NaN(), nil
			}
			return float64(f), nil
		},
	}
	registerEnums(vars)
	// Everything bound here is a constant namespace or a builtin function:
	// reading `Math`, `Qt.rgba` or `Text.AlignHCenter` is not a mutable
	// read, and marking it one poisoned nearly every subtree's read-set.
	derived := make(map[string]bool, len(vars))
	for k := range vars {
		derived[k] = true
	}
	// The builtins live in a FROZEN root of their own, under the document
	// env this returns: nothing is ever bound or unbound in it, so a
	// handler's scope snapshot can keep it as a live parent instead of
	// copying a hundred and more names every time (see snapshot).
	builtins := &Env{vars: vars, derivedNames: derived, frozen: true}
	return &Env{vars: map[string]any{}, parent: builtins}
}

// child returns a nested scope whose lookups fall through to this one.
func (e *Env) child() *Env { return &Env{vars: map[string]any{}, parent: e} }

// snapshot returns a flattened copy of the whole scope chain (outermost
// first, so inner bindings win). Lazily-evaluated closures (Canvas onPaint)
// capture a snapshot at build time: the live env's transient instance/row
// bindings are restored away before paint runs, so reading the live chain
// would resolve the WRONG (document-global or last-instance) values.
// snapshotSites counts snapshot callers per build under GFX_BUILD_STATS.
var snapshotSites = map[string]int{}

func (e *Env) snapshot() *Env {
	if buildStats {
		_, file, line, _ := runtime.Caller(1)
		sizes := ""
		for c := e; c != nil; c = c.parent {
			sizes += fmt.Sprintf("/%d", len(c.vars))
			if c.frozen {
				sizes += "F"
			}
		}
		snapshotSites[fmt.Sprintf("%s:%d layers%s", filepath.Base(file), line, sizes)]++
	}
	var chain []*Env
	for s := e; s != nil; s = s.parent {
		chain = append(chain, s)
	}
	// The FROZEN builtins root (see NewEnv) is not transient: it stays the
	// snapshot's parent instead of being copied — copying its hundred and
	// more names for every handler of every element on every build was an
	// eighth of a frame. Everything else IS copied, the document env
	// included: an instance's ids and properties are bound into it around
	// the instance's build and restored after, and a handler captured in
	// between has to keep what it saw (a component's onOpenedChanged
	// writing its own `position` resolved nothing once they were gone).
	last := len(chain) - 1
	var parent *Env
	if chain[last].frozen {
		parent = chain[last]
		last--
	}
	// Sized up front: a map grown entry by entry to a hundred and more
	// names reallocates its table five times on the way, and the copies
	// were the largest allocation of an animated frame.
	total := 0
	for i := last; i >= 0; i-- {
		total += len(chain[i].vars)
	}
	out := &Env{vars: make(map[string]any, total), parent: parent}
	for i := last; i >= 0; i-- {
		src := chain[i]
		for k, v := range src.vars {
			out.vars[k] = v
			// A flattened scope keeps each name's derived status (see
			// Env.derived): a contract value bound into a style's scope
			// stays a derived read, or `root.palette` inside a style
			// poisons every subtree that mentions it.
			if src.isDerived(k) {
				if out.derivedNames == nil {
					out.derivedNames = make(map[string]bool, total)
				}
				out.derivedNames[k] = true
			}
			if t := src.derivedTok[k]; t != nil {
				if out.derivedTok == nil {
					out.derivedTok = map[string]*core.Property[uint64]{}
				}
				out.derivedTok[k] = t
			}
		}
	}
	return out
}

// findScope returns the nearest scope that already binds name, or nil.
func (e *Env) findScope(name string) *Env {
	for s := e; s != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			return s
		}
	}
	return nil
}

// Ref is a uniform handle over a typed core.Property, giving markup get,
// set and animate.
// colorVal is a COLOUR, as distinct from a string that happens to spell
// one. QML draws the line where JavaScript does: a `property color` (or a
// Qt.rgba/Qt.color/Qt.darker result) is a colour OBJECT whose .r/.g/.b/.a
// are readable, while a `property string` holding "#00D1A9" is just text
// and `.r` on it is UNDEFINED. The corpus depends on the difference — its
// Style.mainColor is a string, so `Qt.rgba(Style.mainColor.r, ...)` is
// Qt.rgba(undefined, ...) = BLACK, which is what the shapes page's arc
// fill actually is (we painted it teal). The value stays a string
// underneath, so everything that formats or parses one is unaffected.
type colorVal string

// colorRef wraps a colour-typed property's ref so reads come back as a
// colorVal — the value is the same string, tagged as a colour.
type colorRef struct{ Ref }

func (c colorRef) get() any { return colorVal(str(c.Ref.get())) }

// unwrap exposes the plain ref underneath, so the decorator-digging in
// propOf (which binds a widget to the concrete core.Property) still finds
// it.
func (c colorRef) unwrap() Ref { return c.Ref }

// plainColor unwraps a colour to its text, for the paths that treat values
// as plain strings (comparison, concatenation, serialisation). The tag only
// has to survive as far as a member read or a colour attribute.
func plainColor(v any) any {
	if c, ok := v.(colorVal); ok {
		return string(c)
	}
	return v
}

type Ref interface {
	get() any
	set(v any) error
	animate(to any, d time.Duration) error
	onChange(fn func()) // subscribe to value changes (for on<Prop>Changed)
}

type propRef[T comparable] struct {
	p     *core.Property[T]
	conv  func(any) (T, error)
	tween func(to T, d time.Duration)
}

// varBox boxes a `property var` value so the backing Property compares by
// pointer (never by the contained, possibly non-comparable, value).
type varBox struct{ v any }

// varRef is the Ref for a `property var`: get/set the boxed value.
type varRef struct{ p *core.Property[*varBox] }

func (r varRef) get() any {
	if b := r.p.Get(); b != nil {
		return b.v
	}
	return nil
}
func (r varRef) set(v any) error { r.p.Set(&varBox{v}); return nil }
func (r varRef) animate(any, time.Duration) error {
	return fmt.Errorf("a var property is not animatable")
}
func (r varRef) onChange(fn func()) { r.p.Changed().Connect(func(*varBox) { fn() }) }

// aliasRef forwards to another object's property (`property alias x:
// target.prop`). The target is resolved lazily on each access, so it works
// even when the target element is registered later (e.g. a TextField's id).
type aliasRef struct {
	target expr
	env    *Env
}

func (r aliasRef) get() any {
	ref, err := resolveRef(r.target, r.env)
	if err != nil {
		return nil
	}
	return ref.get()
}
func (r aliasRef) set(v any) error {
	ref, err := resolveRef(r.target, r.env)
	if err != nil {
		return err
	}
	return ref.set(v)
}
func (r aliasRef) animate(to any, d time.Duration) error {
	ref, err := resolveRef(r.target, r.env)
	if err != nil {
		return err
	}
	return ref.animate(to, d)
}
func (r aliasRef) onChange(fn func()) {
	if ref, err := resolveRef(r.target, r.env); err == nil {
		ref.onChange(fn)
	}
}

func (r propRef[T]) get() any { return r.p.Get() }

func (r propRef[T]) set(v any) error {
	t, err := r.conv(v)
	if err != nil {
		return err
	}
	r.p.Set(t)
	return nil
}

func (r propRef[T]) animate(to any, d time.Duration) error {
	t, err := r.conv(to)
	if err != nil {
		return err
	}
	if r.tween == nil {
		return fmt.Errorf("property is not animatable")
	}
	r.tween(t, d)
	return nil
}

func (r propRef[T]) onChange(fn func()) { r.p.Changed().Connect(func(T) { fn() }) }

// Prop exposes a property to markup under name. Int, Float, Bool and Str
// wrap the supported types.
func (e *Env) Int(name string, p *core.Property[int]) {
	e.vars[name] = propRef[int]{p, toInt, func(to int, d time.Duration) { core.Tween(p, to, d, nil) }}
}

func (e *Env) Float(name string, p *core.Property[float32]) {
	e.vars[name] = propRef[float32]{p, toFloat32, func(to float32, d time.Duration) { core.Tween(p, to, d, nil) }}
}

func (e *Env) Bool(name string, p *core.Property[bool]) {
	e.vars[name] = propRef[bool]{p, toBool, nil}
}

func (e *Env) Str(name string, p *core.Property[string]) {
	e.vars[name] = propRef[string]{p, func(v any) (string, error) { return str(v), nil }, nil}
}

// Func exposes a Go function callable from handlers; fn may take zero args
// or a single float64/string and return nothing.
func (e *Env) Func(name string, fn any) { e.vars[name] = fn }

// Set exposes an arbitrary host value to markup under name — e.g. a
// *render.View3D for a Viewport3D's source. Env values are reactive
// inputs: a CHANGED value bumps the frame revision so memoized documents
// rebuild (a host callback flipping state must be visible next frame).
// Unchanged re-Sets (per-frame host refresh loops) don't invalidate.
func (e *Env) Set(name string, v any) {
	if old, ok := e.vars[name]; ok && scalarEqual(old, v) {
		return
	}
	e.vars[name] = v
	core.BumpRevision()
}

// scalarEqual reports equality for the comparable host-value kinds
// (strings, bools, numbers); everything else (funcs, maps, slices,
// objects) compares unequal so Set conservatively invalidates.
func scalarEqual(a, b any) bool {
	a, b = plainColor(a), plainColor(b)
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case int:
		bv, ok := b.(int)
		return ok && av == bv
	}
	return false
}

// Get reads a document-scope value back out (hosts inspecting state the
// markup declared — a property Ref, a TextField id's InputState, ...).
func (e *Env) Get(name string) (any, bool) {
	v, ok := e.vars[name]
	return v, ok
}

// RefValue dereferences a property Ref (host-side convenience).
func RefValue(r Ref) any { return r.get() }

// Input exposes a text-field state; markup references its text as
// name.text.
func (e *Env) Input(name string, s *ui.InputState) { e.vars[name] = s }

// Scroll exposes a scroll state for Scroll { state: name }.
func (e *Env) Scroll(name string, s *ui.ScrollState) { e.vars[name] = s }

func (e *Env) lookup(name string) (any, bool) {
	for s := e; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// ---- evaluation ----

func (x *litExpr) eval(*Env) (any, error) { return x.v, nil }

func (x *elemExpr) eval(*Env) (any, error) { return x.n, nil }

func (x *indexExpr) eval(env *Env) (any, error) {
	recv, err := x.recv.eval(env)
	if err != nil {
		return nil, err
	}
	idx, err := x.index.eval(env)
	if err != nil {
		return nil, err
	}
	switch c := recv.(type) {
	case []any:
		i, err := toInt(idx)
		if err != nil {
			return nil, err
		}
		if i < 0 || i >= len(c) {
			return nil, nil // JS: out-of-range index is undefined, not an error
		}
		return c[i], nil
	case map[string]any:
		return c[str(idx)], nil // missing key -> nil (JS undefined)
	case string:
		r := []rune(c)
		i, err := toInt(idx)
		if err != nil {
			return nil, err
		}
		if i < 0 || i >= len(r) {
			return nil, nil // JS: undefined
		}
		return string(r[i]), nil
	}
	return nil, fmt.Errorf("gml:%d: cannot index %T", x.line, recv)
}

func (x *identExpr) eval(env *Env) (any, error) {
	v, owner, ok := env.lookupOwner(x.name)
	if !ok {
		return nil, fmt.Errorf("gml:%d: unknown identifier %q", x.line, x.name)
	}
	if r, ok := v.(Ref); ok {
		return r.get(), nil
	}
	if !owner.isDerived(x.name) {
		markMutableReadNamed(x.name, v)
	} else {
		owner.noteDerivedRead(x.name)
	}
	return v, nil
}

// markMutableRead poisons the active subtree read-set when an expression
// resolved an env value the property system can't stamp: plain injected
// values (Env.Set), model rows, list models. Refs read through properties
// (stamped), and id objects/functions/components route their mutable state
// through Refs or their own marks — those stay cacheable.
func markMutableRead(v any) { markMutableReadNamed("", v) }

// markMutableReadNamed is markMutableRead with the identifier that resolved
// to v, for the GFX_UNTRACKED_TRACE breakdown (which names poison caches).
func markMutableReadNamed(name string, v any) {
	switch t := v.(type) {
	case nil, Ref, *elemObject, *userFunc, *node:
	case *listModel:
		// A model carries its own version property and stamps it on
		// every mutation (listModel.ver) — which is exactly a tracked
		// read, so take it instead of poisoning. Marking it untracked
		// rebuilt every view with a model, every frame.
		t.ver.Get()
	default:
		if untrackedNames != nil {
			untrackedNames[fmt.Sprintf("%s:%T", name, v)]++
		}
		core.MarkUntrackedRead()
	}
}

// untrackedNames counts identifier:type pairs behind untracked reads when
// GFX_UNTRACKED_TRACE is set (nil otherwise).
var untrackedNames = func() map[string]int {
	if os.Getenv("GFX_UNTRACKED_TRACE") != "" {
		return map[string]int{}
	}
	return nil
}()

// elemObject is the live view of an id'd element in expressions: its declared
// properties (props, as Refs) plus geometry (width/height/x/y/right/bottom)
// read from the element's previous-frame layout (el). Geometry lags one frame
// because bindings evaluate during build, before this frame's layout.
type elemObject struct {
	props   map[string]any // declared property name -> Ref
	methods map[string]any // method name -> callable (jsFunc); e.g. StackView.push
	el      ui.Element     // previous-frame element, for geometry (may be nil)
	// input is the live text state when this id names a text field, so
	// `field.text` / `field.activeFocus` resolve through the id object too
	// (the field itself rebinds the id to the state during its own build,
	// but sibling attributes and property defaults see this object).
	input *ui.InputState
	// scroll is the live scroll state when this id names a Flickable or
	// ScrollView, for contentX/contentY/visibleArea reads.
	scroll *ui.ScrollState
	// pdf is the parsed document when this id names a PdfDocument, so a
	// viewer's `document: <id>` resolves to the pages themselves.
	pdf *pdfState
	// node/nodeEnv let a plain visual ATTRIBUTE be read back through the
	// object (`radius: parent.radius`, `color: parent.color`). Only set
	// where the element behind the object is known — parent references.
	node    *node
	nodeEnv *Env
	// parentKey is the element-registry key of the item this one sits in.
	// The parent OBJECT is captured when the id is declared, so its element
	// is whatever existed then (nothing, at load time); the key stays valid
	// and resolves against the live registry, which is what a
	// parent-relative x/y needs.
	parentKey keyID
	// parentObj is the object for the item this one sits in, for
	// `someId.parent.<prop>`.
	parentObj *elemObject
	// derive computes a control's read-only properties on demand, for reads
	// that happen before the control has built (see seedDerivedProps).
	derive func(name string) (any, bool)
	// natW/natH are the item's own declared size, used only when no
	// previous-frame element exists (frame one): a styled control knows its
	// implicit size before its delegates build, and `parent.width - width`
	// in a delegate has to mean something on the first frame too.
	natW, natH float32
	// ovW/ovH OVERRIDE the size a geometry read reports (when > 0). A
	// Flickable's content item is as big as contentWidth/contentHeight,
	// not as the viewport it scrolls inside, and it is that item — not the
	// Flickable — that a child means by `parent`.
	ovW, ovH float32
	// orgX/orgY make x/y read RELATIVE to an origin, as QML's x/y are
	// relative to the parent item. Set where the enclosing item is known
	// (a styled control's delegate objects); zero elsewhere, where reads
	// stay in window coordinates.
	orgX, orgY float32
	// doc and key let a write to a plain ATTRIBUTE through this id
	// (`box.width = 100`, or a `property alias` pointing at it) land as an
	// override the element picks up on its next build.
	doc *docState
	key keyID
}

// setProp stores an exposed value, stamping its token when it changed.
//
// The token is the DOCUMENT's, keyed by the element key and the name — a
// styled control's shadow object is made anew every build, so a token on
// the object itself was recorded by a reader and then never written again
// (the content item that read `root.text` was served with "Press Me" for
// ever while every new object said "Pressed!"). An object with no key has
// no durable identity; its readers stay untracked.
func (o *elemObject) setProp(name string, v any) {
	if o.props == nil {
		o.props = map[string]any{}
	}
	// A scalar that is what it was: nothing to write, nothing to stamp.
	if old, ok := o.props[name]; ok && sameScalar(old, v) {
		return
	}
	o.props[name] = v
	if _, isRef := v.(Ref); isRef {
		return
	}
	if t := o.propToken(name); t != nil {
		t.Set(tokenStr(v))
	}
}

// sameScalar reports two values equal when both are the same scalar type
// and value; anything else (maps, refs, objects) reports false and goes
// through the token.
func sameScalar(a, b any) bool {
	switch x := b.(type) {
	case string:
		y, ok := a.(string)
		return ok && x == y
	case float64:
		y, ok := a.(float64)
		return ok && x == y
	case bool:
		y, ok := a.(bool)
		return ok && x == y
	case int:
		y, ok := a.(int)
		return ok && x == y
	case colorVal:
		y, ok := a.(colorVal)
		return ok && x == y
	}
	return false
}

// tokenStr is the string a token carries for a value: cheap for the
// scalars that make up nearly every exposed value (str() formats floats
// through strconv, and a map or slice through fmt — which was a tenth of
// a frame), and identity for anything else, whose CONTENTS the token does
// not follow.
func tokenStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string, colorVal, float64, float32, int, int64, bool:
		return str(t)
	case Ref, *elemObject:
		return fmt.Sprintf("%p", t)
	case map[string]any, []any:
		// A contract sub-object (palette, icon, font): hashed by CONTENT,
		// order-independently, so a map rebuilt with the same entries
		// reads as unchanged — formatting it (fmt sorts the keys) was a
		// sixth of every frame.
		return strconv.FormatUint(tokenHash(t, 0), 36)
	}
	return str(v)
}

// tokenHash folds a value into a 64-bit hash for tokenStr (depth-limited;
// maps are order-independent).
func tokenHash(v any, depth int) uint64 {
	const prime = 1099511628211
	switch t := v.(type) {
	case nil:
		return 0x9e37
	case map[string]any:
		if depth > 3 {
			return uint64(len(t))
		}
		var acc uint64
		for k, x := range t {
			h := uint64(14695981039346656037)
			for i := 0; i < len(k); i++ {
				h ^= uint64(k[i])
				h *= prime
			}
			h ^= tokenHash(x, depth+1)
			h *= prime
			acc ^= h // order-independent
		}
		return acc ^ uint64(len(t))<<56
	case []any:
		h := uint64(14695981039346656037)
		for _, x := range t {
			h ^= tokenHash(x, depth+1)
			h *= prime
		}
		return h
	case string:
		h := uint64(14695981039346656037)
		for i := 0; i < len(t); i++ {
			h ^= uint64(t[i])
			h *= prime
		}
		return h
	case colorVal:
		return tokenHash(string(t), depth)
	case float64:
		return math.Float64bits(t) * prime
	case float32:
		return uint64(math.Float32bits(t)) * prime
	case int:
		return uint64(t) * prime
	case int64:
		return uint64(t) * prime
	case bool:
		if t {
			return 2
		}
		return 1
	}
	// Anything else: identity, through the formatted pointer.
	h := uint64(14695981039346656037)
	for _, c := range fmt.Sprintf("%p", v) {
		h ^= uint64(c)
		h *= prime
	}
	return h
}

// propToken is the tracking token for a plain exposed value (nil when the
// object cannot be identified across builds).
func (o *elemObject) propToken(name string) *core.Property[string] {
	if o.doc == nil || o.key == 0 {
		return nil
	}
	if o.doc.propTok == nil {
		o.doc.propTok = map[propTokKey]*core.Property[string]{}
	}
	k := propTokKey{o.key, name}
	t := o.doc.propTok[k]
	if t == nil {
		t = &core.Property[string]{}
		o.doc.propTok[k] = t
	}
	return t
}

// propTokKey identifies an exposed value's token: element key and name.
type propTokKey struct {
	key  keyID
	name string
}

// trackGeom records a geometry read of this item in the active read-set.
// The token is the DOCUMENT's, keyed by the element registry key, because
// the object doing the reading is often transient — a `parent` object is
// made per build — and a token on it would never be stamped again: the
// layer that read `parent.width` at 0 on frame one was served at 0x0 for
// ever. An object with no key cannot be tracked and poisons the read-set.
func (o *elemObject) trackGeom() {
	if o.doc == nil || o.key == 0 {
		core.MarkUntrackedRead()
		return
	}
	// The token is keyed by the REGISTRY key the object's element comes
	// from, which beginFrame resolves through idKeys (an id declared
	// document-wide whose element lives in a component instance) — keyed
	// by the bare id, the token hashed a key nothing registers under, so
	// it never stamped and its readers were served for ever (an effect
	// capture stuck at x=0). An element that is not in the registry (a
	// closed Menu is never laid out) hashes as absent; the frame it
	// appears, the token stamps. Treating that read as UNTRACKED instead
	// made every ancestor of a closed menu rebuild every frame.
	key := o.key
	if k, ok := o.doc.idKeys.get2(key); ok {
		key = k
	}
	o.doc.geoToken(key, o.el).Get()
}

// geomHash folds everything a geometry read can observe about an element:
// its laid-out rect and its implicit (content) size.
func geomHash(el ui.Element) uint64 {
	if el == nil {
		return 0
	}
	r := ui.Bounds(el)
	iw, ih := ui.Implicit(el)
	h := uint64(1469598103934665603)
	for _, f := range [...]float32{r.X, r.Y, r.W, r.H, iw, ih} {
		h ^= uint64(math.Float32bits(f))
		h *= 1099511628211
	}
	return h
}

func (o *elemObject) member(name string) (any, bool) {
	if o.props != nil {
		if v, ok := o.props[name]; ok {
			if r, ok := v.(Ref); ok {
				return r.get(), true
			}
			if t := o.propToken(name); t != nil {
				t.Get() // a plain exposed value: record the read (see setProp)
			} else {
				core.MarkUntrackedRead() // exposed without a token: cannot be proven fresh
			}
			return v, true
		}
	}
	if o.methods != nil {
		if fn, ok := o.methods[name]; ok {
			return fn, true
		}
	}
	// Keyboard focus on any item (focus.go): forceActiveFocus() and the
	// activeFocus it leads to. A text field reports its own below.
	if o.doc != nil && o.key != 0 {
		switch name {
		case "forceActiveFocus":
			doc, key := o.doc, o.doc.focusKeyOf(o)
			return jsFunc(func([]any) (any, error) { doc.requestFocus(key); return nil, nil }), true
		case "activeFocusItem": // Window's, readable on any item
			return o.doc.activeFocusItem(), true
		case "activeFocus":
			if o.input == nil {
				b, _ := toBool(o.doc.activeFocusRef(o.doc.focusKeyOf(o)).get())
				return b, true
			}
		}
	}
	if o.derive != nil {
		if v, ok := o.derive(name); ok {
			return v, true
		}
	}
	if o.scroll != nil {
		if v, handled := scrollMember(o.scroll, name); handled {
			return v, true
		}
	}
	if o.input != nil {
		switch name {
		case "text", "displayText":
			return o.input.Text.Get(), true
		case "length":
			return float64(len([]rune(o.input.Text.Get()))), true
		case "activeFocus", "focus":
			return o.input.HasFocus(), true
		case "acceptableInput":
			return true, true
		}
	}
	if name == "parent" && o.parentObj != nil {
		return o.parentObj, true
	}
	switch name {
	case "implicitWidth", "implicitHeight":
		// The item's natural content size, which is what markup sizing off
		// another item asks for (`Layout.preferredHeight: col.implicitHeight
		// + 20`) — its laid-out height there is the one being computed.
		o.trackGeom()
		w, h := o.natW, o.natH
		if o.el != nil {
			if iw, ih := ui.Implicit(o.el); iw > 0 || ih > 0 {
				w, h = iw, ih
			}
		}
		// Frame one has no element yet (the handler that reads this runs
		// right after the build that queued it), so the declared size
		// stands in — `Component.onCompleted: fitToView()` divides by this
		// number, and a zero there poisons the property for good.
		if name == "implicitWidth" {
			return float64(w), true
		}
		return float64(h), true
	case "contentWidth", "contentHeight", "paintedWidth", "paintedHeight":
		// What the CONTENT occupies: a Text's laid-out lines (the widest
		// one, and their total height), a control's content item. A style
		// sizes itself off it (`implicitWidth: contentWidth + padding`),
		// and a page reads it to see how much room a label needs.
		o.trackGeom()
		if o.el != nil {
			w, h := ui.Implicit(o.el)
			if name == "contentWidth" || name == "paintedWidth" {
				return float64(w), true
			}
			return float64(h), true
		}
		return float64(0), true
	case "availableWidth":
		name = "width" // padding-adjusted size ≈ laid-out size here
	case "availableHeight":
		name = "height"
	}
	switch name {
	case "width", "height", "x", "y", "right", "bottom":
		// Layout geometry lives outside the property system, so the read
		// goes through the item's geometry token (see trackGeom): the
		// subtree is stale exactly when this item moved.
		o.trackGeom()
		if o.doc != nil && o.key != 0 {
			if f, ok := o.doc.pendingGeom(o.key, name); ok {
				return f, true
			}
		}
		var r ui.Rect
		if o.el != nil {
			r = ui.Bounds(o.el)
		}
		if r.W == 0 && r.H == 0 {
			r.W, r.H = o.natW, o.natH // frame one: the declared size
		}
		if o.ovW > 0 {
			r.W = o.ovW
		}
		if o.ovH > 0 {
			r.H = o.ovH
		}
		// QML's x/y are relative to the parent item (parentOrigin).
		ox, oy := o.parentOrigin()
		switch name {
		case "width":
			return float64(r.W), true
		case "height":
			return float64(r.H), true
		case "x":
			return float64(r.X - ox), true
		case "y":
			return float64(r.Y - oy), true
		case "right":
			return float64(r.X - ox + r.W), true
		case "bottom":
			return float64(r.Y - oy + r.H), true
		}
	}
	// An attribute assigned imperatively (`item.scale = 2`, a handler
	// driving it) reads back as assigned: the override wins over the
	// markup, as it does when the element builds.
	if o.doc != nil && o.key != 0 {
		if v, ok := o.doc.attrOverride(o.key, name); ok {
			return v, true
		}
	}
	// A plain visual attribute of the element itself, evaluated from the
	// markup: `radius: parent.radius` copies the value its parent was
	// given. Geometry is handled above — its live value beats the markup.
	if o.node != nil && o.nodeEnv != nil {
		for _, a := range o.node.attrs {
			if a.name != name || a.expr == nil {
				continue
			}
			if v, err := a.expr.eval(o.nodeEnv); err == nil {
				return v, true
			}
		}
	}
	// Every Item has these, at their defaults when the markup is silent.
	switch name {
	case "scale", "opacity":
		return 1.0, true
	case "rotation":
		return 0.0, true
	}
	return nil, false
}

// resolveRef returns the Ref behind an expression without dereferencing,
// for assignment targets and ref-typed attributes.
func resolveRef(e expr, env *Env) (Ref, error) {
	switch t := e.(type) {
	case *identExpr:
		v, ok := env.lookup(t.name)
		if !ok {
			return nil, fmt.Errorf("gml:%d: unknown identifier %q", t.line, t.name)
		}
		if r, ok := v.(Ref); ok {
			return r, nil
		}
		return nil, fmt.Errorf("gml:%d: %q is not a property", t.line, t.name)
	case *memberExpr:
		base, err := resolveAny(t.base, env)
		if err != nil {
			return nil, err
		}
		// A Flickable's scroll offset is assignable: `flick.contentX = 0`
		// (and positionViewAt* below go through the same state).
		if eo, ok := base.(*elemObject); ok && eo.scroll != nil {
			if r, ok := scrollOffsetRef(eo.scroll, t.name); ok {
				return r, nil
			}
		}
		if sc, ok := base.(*ui.ScrollState); ok {
			if r, ok := scrollOffsetRef(sc, t.name); ok {
				return r, nil
			}
		}
		if in, ok := base.(*ui.InputState); ok && t.name == "text" {
			return propRef[string]{&in.Text, func(v any) (string, error) { return str(v), nil }, nil}, nil
		}
		// id-object: someId.declaredProp resolves to that element's Ref.
		if eo, ok := base.(*elemObject); ok {
			if eo.props != nil {
				if r, ok := eo.props[t.name].(Ref); ok {
					return r, nil
				}
			}
			// A plain attribute of the element (`icon.width = 40`, or an
			// alias declared onto it): writable as an override.
			if eo.doc != nil && eo.key != 0 {
				return attrOverrideRef{doc: eo.doc, key: eo.key, name: t.name, eo: eo}, nil
			}
			// A text field's id: `field.text = "..."` writes its live state.
			if eo.input != nil && t.name == "text" {
				in := eo.input
				return propRef[string]{&in.Text, func(v any) (string, error) { return str(v), nil }, nil}, nil
			}
			if eo.scroll != nil {
				if r, ok := scrollOffsetRef(eo.scroll, t.name); ok {
					return r, nil
				}
			}
		}
		if obj, ok := base.(map[string]any); ok {
			if r, ok := obj[t.name].(Ref); ok {
				return r, nil
			}
		}
		return nil, fmt.Errorf("gml:%d: no property %q", t.line, t.name)
	}
	return nil, fmt.Errorf("gml: not an assignable property")
}

// attrOverrideRef is the Ref behind a plain attribute of an element reached
// through its id. QML lets an attribute be assigned imperatively (`box.width
// = 100`) or aliased into (`property alias w: box.width`), and the value
// wins over the markup from then on — buildFresh merges these overrides in
// before the element's attributes are applied.
type attrOverrideRef struct {
	doc  *docState
	key  keyID
	name string
	eo   *elemObject
}

func (r attrOverrideRef) get() any {
	if v, ok := r.doc.attrOverride(r.key, r.name); ok {
		return v
	}
	if r.eo != nil {
		if v, ok := r.eo.member(r.name); ok {
			return v
		}
	}
	return nil
}

func (r attrOverrideRef) set(v any) error {
	r.doc.setAttrOverride(r.key, r.name, v)
	return nil
}

func (r attrOverrideRef) animate(to any, _ time.Duration) error { return r.set(to) }
func (r attrOverrideRef) onChange(func())                       {}

// scrollOffsetRef is the Ref behind an assignable scroll offset. The offset
// is a plain field on the retained state (no property/binding of its own), so
// a write moves the view on the next frame and reads mark the subtree
// untracked, exactly like scrollMember's reads.
func scrollOffsetRef(s *ui.ScrollState, name string) (Ref, bool) {
	switch name {
	case "contentX":
		return scrollFieldRef{s: s, x: true}, true
	case "contentY":
		return scrollFieldRef{s: s}, true
	}
	return nil, false
}

// scrollFieldRef is a Ref over one axis of a ScrollState's offset.
type scrollFieldRef struct {
	s *ui.ScrollState
	x bool
}

func (r scrollFieldRef) get() any {
	scrollTok(r.s).Get()
	if r.x {
		return float64(r.s.X)
	}
	return float64(r.s.Y)
}

func (r scrollFieldRef) set(v any) error {
	f, err := toFloat32(v)
	if err != nil {
		return err
	}
	if r.x {
		r.s.X = max(f, 0)
	} else {
		r.s.Y = max(f, 0)
	}
	core.BumpRevision() // geometry-only state: nothing else marks the frame dirty
	return nil
}

func (r scrollFieldRef) animate(to any, d time.Duration) error { return r.set(to) }
func (r scrollFieldRef) onChange(func())                       {}

// resolveAny evaluates an expression but keeps state objects intact.
func resolveAny(e expr, env *Env) (any, error) {
	if id, ok := e.(*identExpr); ok {
		if v, owner, found := env.lookupOwner(id.name); found {
			if !owner.isDerived(id.name) {
				markMutableReadNamed(id.name, v)
			} else {
				owner.noteDerivedRead(id.name)
			}
			return v, nil
		}
		return nil, fmt.Errorf("gml:%d: unknown identifier %q", id.line, id.name)
	}
	return e.eval(env)
}

func (x *memberExpr) eval(env *Env) (any, error) {
	// Member access on a map base: a delegate row's role (`model.role`), an
	// id-object's declared property (`someId.prop`), or a field of a var
	// holding a JSON object (`info.v`). A Ref base is dereferenced first.
	if base, err := resolveAny(x.base, env); err == nil {
		if r, ok := base.(Ref); ok {
			base = r.get()
		}
		// Optional chaining: a nil base yields nil instead of "no member".
		if x.optional && base == nil {
			return nil, nil
		}
		if re, ok := base.(*jsRegExp); ok {
			if v, ok := re.member(x.name); ok {
				return v, nil
			}
		}
		if x.name == "length" {
			switch c := base.(type) {
			case []any:
				return float64(len(c)), nil
			case string:
				return float64(len([]rune(c))), nil
			}
		}
		// QML color component access — on a COLOUR (see colorVal), not on
		// any string that parses as one.
		if len(x.name) == 1 && strings.ContainsAny(x.name, "rgba") {
			var c ui.RGBA
			have := false
			switch t := base.(type) {
			case colorVal:
				if v, cerr := parseColor(string(t)); cerr == nil {
					c, have = v, true
				}
			case ui.RGBA:
				c, have = t, true
			}
			if have {
				switch x.name {
				case "r":
					return float64(c.R), nil
				case "g":
					return float64(c.G), nil
				case "b":
					return float64(c.B), nil
				case "a":
					return float64(c.A), nil
				}
			}
			if _, isStr := base.(string); isStr {
				// A string is not a colour: undefined, as in Qt.
				return nil, nil
			}
		}
		// Model ids expose their row count (`warningModel.count > 0`), the
		// read-only property QML delegates and bindings use.
		if lm, ok := base.(*listModel); ok {
			switch x.name {
			case "count", "length":
				lm.ver.Get() // stamped on every mutation (listModel.ver)
				return float64(len(lm.rows)), nil
			}
			return nil, fmt.Errorf("gml:%d: no member %q", x.line, x.name)
		}
		// Flickable/ScrollView ids: contentX/contentY and the read-only
		// visibleArea sub-object (position/size as fractions).
		if sc, ok := base.(*ui.ScrollState); ok {
			if v, handled := scrollMember(sc, x.name); handled {
				return v, nil
			}
			return nil, fmt.Errorf("gml:%d: no member %q", x.line, x.name)
		}
		// A text field's id resolves to its live input state: expose the
		// QML TextInput members bindings actually read.
		if in, ok := base.(*ui.InputState); ok {
			switch x.name {
			case "text", "displayText":
				return in.Text.Get(), nil
			case "length":
				return float64(len([]rune(in.Text.Get()))), nil
			case "activeFocus", "focus":
				return in.HasFocus(), nil
			case "acceptableInput":
				return true, nil
			case "forceActiveFocus":
				return jsFunc(func([]any) (any, error) { in.RequestFocus(); return nil, nil }), nil
			}
			return nil, fmt.Errorf("gml:%d: no member %q", x.line, x.name)
		}
		if eo, ok := base.(*elemObject); ok {
			if v, ok := eo.member(x.name); ok {
				return v, nil
			}
			return nil, fmt.Errorf("gml:%d: no member %q", x.line, x.name)
		}
		if ns, ok := base.(*callableNS); ok {
			if v, ok := ns.members[x.name]; ok {
				return v, nil
			}
			return nil, fmt.Errorf("gml:%d: no member %q", x.line, x.name)
		}
		if row, ok := base.(map[string]any); ok {
			v, ok := row[x.name]
			if !ok {
				// A plain data object (a model row, parsed JSON): a missing
				// key is `undefined`, as in JS — code branches on exactly
				// that (`model.active !== undefined`). Element ids stay
				// strict below, where a missing member IS a mistake.
				return nil, nil
			}
			if r, ok := v.(Ref); ok {
				return r.get(), nil
			}
			return v, nil
		}
	}
	r, err := resolveRef(x, env)
	if err != nil {
		return nil, err
	}
	return r.get(), nil
}

func (x *unaryExpr) eval(env *Env) (any, error) {
	// `typeof` must tolerate an undeclared name (JS returns "undefined"
	// rather than throwing), so it evaluates its operand itself.
	if x.op == "typeof" {
		v, err := x.x.eval(env)
		if err != nil {
			return "undefined", nil
		}
		return jsTypeOf(v), nil
	}
	v, err := x.x.eval(env)
	if err != nil {
		return nil, err
	}
	switch x.op {
	case "-":
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return -f, nil
	case "+":
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return f, nil
	case "~":
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return float64(^int64(f)), nil
	case "!":
		b, err := toBool(v)
		if err != nil {
			return nil, err
		}
		return !b, nil
	}
	return nil, fmt.Errorf("gml: bad unary %q", x.op)
}

// blockExpr runs its statements in a child scope; the block's return
// value is the expression's value (QML block bindings).
func (x *blockExpr) eval(env *Env) (any, error) {
	return callUserFunc(&userFunc{body: x.body, env: env}, nil)
}

func (x *binExpr) eval(env *Env) (any, error) {
	l, err := x.l.eval(env)
	if err != nil {
		return nil, err
	}
	// Nullish coalescing: left unless it's null/undefined.
	if x.op == "??" {
		if l != nil {
			return l, nil
		}
		return x.r.eval(env)
	}
	// Short-circuit logic — JS value semantics, not boolean coercion:
	// `a || b` yields a when truthy else b (the default-value idiom,
	// `widths[col] || 120`), `a && b` yields a when falsy else b.
	if x.op == "&&" || x.op == "||" {
		lb, err := toBool(l)
		if err != nil {
			return nil, err
		}
		if (x.op == "&&" && !lb) || (x.op == "||" && lb) {
			return l, nil
		}
		return x.r.eval(env)
	}
	r, err := x.r.eval(env)
	if err != nil {
		return nil, err
	}
	l, r = plainColor(l), plainColor(r)
	// String concatenation when either side is a string.
	if x.op == "+" {
		if _, ok := l.(string); ok {
			return str(l) + str(r), nil
		}
		if _, ok := r.(string); ok {
			return str(l) + str(r), nil
		}
	}
	if x.op == "==" || x.op == "!=" {
		// Comparing against null/undefined never coerces: `model.active !==
		// undefined` must answer "is it set", not try to read nil as a
		// number. (=== / !== lex to ==/!=, so this covers both.)
		if l == nil || r == nil {
			eq := l == nil && r == nil
			if x.op == "!=" {
				eq = !eq
			}
			return eq, nil
		}
		if ls, ok := l.(string); ok {
			eq := ls == str(r)
			if x.op == "!=" {
				eq = !eq
			}
			return eq, nil
		}
		if lb, ok := l.(bool); ok {
			rb, err := toBool(r)
			if err != nil {
				return nil, err
			}
			eq := lb == rb
			if x.op == "!=" {
				eq = !eq
			}
			return eq, nil
		}
		// Objects (an item, a text field's state, an array) are equal when
		// they are the same object, as in JS: `Window.activeFocusItem ===
		// field`. Only values that are not numbers get here.
		if _, errL := toFloat64(l); errL != nil {
			if _, errR := toFloat64(r); errR != nil {
				eq := sameObject(l, r)
				if x.op == "!=" {
					eq = !eq
				}
				return eq, nil
			}
		}
	}
	lf, err := toFloat64(l)
	if err != nil {
		return nil, fmt.Errorf("gml:%d: %v", x.line, err)
	}
	rf, err := toFloat64(r)
	if err != nil {
		return nil, fmt.Errorf("gml:%d: %v", x.line, err)
	}
	switch x.op {
	case "+":
		return lf + rf, nil
	case "-":
		return lf - rf, nil
	case "*":
		return lf * rf, nil
	case "/":
		return lf / rf, nil
	case "%":
		// JS's remainder is fmod on doubles, NOT integer modulo: a hue
		// written `(0.47 + i * 0.04) % 1` must stay 0.47, and truncating it
		// to 0 turned every ring of the breathing-circles canvas red.
		return math.Mod(lf, rf), nil
	case "<<":
		return float64(int64(lf) << uint(int64(rf))), nil
	case ">>":
		return float64(int64(lf) >> uint(int64(rf))), nil
	case "|":
		return float64(int64(lf) | int64(rf)), nil
	case "&":
		return float64(int64(lf) & int64(rf)), nil
	case "^":
		return float64(int64(lf) ^ int64(rf)), nil
	case "<":
		return lf < rf, nil
	case ">":
		return lf > rf, nil
	case "<=":
		return lf <= rf, nil
	case ">=":
		return lf >= rf, nil
	case "==":
		return lf == rf, nil
	case "!=":
		return lf != rf, nil
	}
	return nil, fmt.Errorf("gml:%d: bad operator %q", x.line, x.op)
}

func (x *condExpr) eval(env *Env) (any, error) {
	c, err := x.c.eval(env)
	if err != nil {
		return nil, err
	}
	b, err := toBool(c)
	if err != nil {
		return nil, err
	}
	if b {
		return x.a.eval(env)
	}
	return x.b.eval(env)
}

func (x *listExpr) eval(env *Env) (any, error) {
	out := make([]any, 0, len(x.items))
	for _, it := range x.items {
		if sp, ok := it.(*spreadExpr); ok { // ...expr → flatten an array in place
			v, err := sp.inner.eval(env)
			if err != nil {
				return nil, err
			}
			if arr, ok := v.([]any); ok {
				out = append(out, arr...)
			} else if v != nil {
				out = append(out, v)
			}
			continue
		}
		v, err := it.eval(env)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (x *funcExpr) eval(env *Env) (any, error) {
	return &userFunc{params: x.params, defaults: x.defaults, body: x.body, env: env}, nil
}

func (x *templateExpr) eval(env *Env) (any, error) {
	var b strings.Builder
	for i, p := range x.parts {
		b.WriteString(p)
		if i < len(x.exprs) {
			v, err := x.exprs[i].eval(env)
			if err != nil {
				return nil, err
			}
			b.WriteString(str(v))
		}
	}
	return b.String(), nil
}

func (x *objExpr) eval(env *Env) (any, error) {
	out := make(map[string]any, len(x.keys))
	for i, k := range x.keys {
		v, err := x.vals[i].eval(env)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func (x *callExpr) eval(env *Env) (any, error) {
	if x.spread {
		// f(a, ...xs): evaluate the arguments once, flatten the spread
		// ones, and make the call with the values as literals.
		args, err := expandArgs(x.args, env)
		if err != nil {
			return nil, err
		}
		flat := *x
		flat.args, flat.spread = args, false
		return flat.eval(env)
	}
	// Bare-identifier calls: the animate() builtin and env-registered Go funcs.
	if id, ok := x.callee.(*identExpr); ok {
		if id.name == "animate" {
			return x.evalAnimate(env)
		}
		fnv, found := env.lookup(id.name)
		if !found {
			return nil, fmt.Errorf("gml:%d: unknown function %q", x.line, id.name)
		}
		return callValue(fnv, x.args, env, x.line, id.name)
	}
	// Builtin methods on primitive values: number.toFixed(n),
	// string.toUpperCase(), array.join(","), etc. Only primitives are handled
	// here; map/namespace bases (Math, Qt, ctx) fall through to callee.eval.
	if m, ok := x.callee.(*memberExpr); ok {
		if base, err := resolveAny(m.base, env); err == nil {
			if r, ok := base.(Ref); ok {
				base = r.get()
			}
			// JS array mutators. Our arrays are Go slices (values), so a
			// mutation is computed and written back through the receiver
			// expression — the variable or property the array came from.
			if arr, ok := base.([]any); ok && isArrayMutator(m.name) {
				args, err := evalArgs(x.args, env)
				if err != nil {
					return nil, err
				}
				var out []any
				var ret any
				if m.name == "sort" && len(args) > 0 && args[0] != nil {
					// sort(compare): ordered by the comparator's sign.
					out = append([]any{}, arr...)
					if err := sortWith(out, args[0]); err != nil {
						return nil, fmt.Errorf("gml:%d: sort: %v", x.line, err)
					}
					ret = out
				} else {
					out, ret = mutateArray(arr, m.name, args)
				}
				// A receiver that is a temporary (items.slice().sort(), a
				// literal) has nowhere to be written back: the result is
				// the value, as in JS.
				if assignable(m.base) {
					if err := assign(m.base, &litExpr{out}, env); err != nil {
						return nil, fmt.Errorf("gml:%d: %s: %v", x.line, m.name, err)
					}
				} else if m.name == "sort" || m.name == "reverse" {
					return out, nil
				}
				return ret, nil
			}
			if lm, ok := base.(*listModel); ok {
				return lm.call(m.name, x.args, env, x.line)
			}
			if tm, ok := base.(*tableModel); ok {
				return tm.call(m.name, x.args, env, x.line)
			}
			if v, handled, err := callBuiltinMethod(base, m.name, x.args, env, x.line); handled {
				return v, err
			}
		}
	}
	// Method / namespace call: evaluate the callee to a callable value.
	fnv, err := x.callee.eval(env)
	if err != nil {
		return nil, err
	}
	// o?.f() with o null or undefined: the optional chain short-circuits
	// the whole call to undefined, as in JS, instead of calling nothing.
	if fnv == nil && optionalChain(x.callee) {
		return nil, nil
	}
	return callValue(fnv, x.args, env, x.line, "value")
}

func (x *callExpr) evalAnimate(env *Env) (any, error) {
	if len(x.args) != 3 {
		return nil, fmt.Errorf("gml:%d: animate(prop, to, ms)", x.line)
	}
	ref, err := resolveRef(x.args[0], env)
	if err != nil {
		return nil, err
	}
	to, err := x.args[1].eval(env)
	if err != nil {
		return nil, err
	}
	ms, err := x.args[2].eval(env)
	if err != nil {
		return nil, err
	}
	msF, err := toFloat64(ms)
	if err != nil {
		return nil, err
	}
	return nil, ref.animate(to, time.Duration(msF*float64(time.Millisecond)))
}

// callBuiltinMethod dispatches JS builtin methods on primitive receivers
// (number, string, array). Returns handled=false for non-primitive bases so
// the caller falls back to namespace/method resolution. This covers the
// common QML idioms (n.toFixed(2), s.toUpperCase(), arr.join(",")).
func callBuiltinMethod(base any, name string, argExprs []expr, env *Env, line int) (any, bool, error) {
	args, err := evalArgs(argExprs, env)
	if err != nil {
		return nil, true, err
	}
	// Normalize numeric receivers to float64 (refs may yield float32/int).
	switch b := base.(type) {
	case float32:
		base = float64(b)
	case int:
		base = float64(b)
	}
	// Every JS value answers toString()/valueOf() — a number, a string, a
	// bool, null (`getValue(key, "").toString()` on a missing key) — so
	// answer it once here rather than per receiver type. Numbers keep their
	// own toString below via str().
	switch name {
	case "toString":
		return str(base), true, nil
	case "valueOf":
		return base, true, nil
	}
	// RegExp methods, and the String methods given a RegExp argument.
	if re, ok := base.(*jsRegExp); ok {
		v, handled := re.call(name, args)
		return v, handled, nil
	}
	if s, ok := base.(string); ok && len(args) > 0 {
		if re, ok := args[0].(*jsRegExp); ok {
			if v, handled, err := regexStringMethod(s, name, re, args); handled {
				return v, true, err
			}
		}
	}
	switch recv := base.(type) {
	case float64:
		switch name {
		case "toFixed":
			prec := 0
			if len(args) > 0 {
				if f, err := toFloat64(args[0]); err == nil {
					prec = int(f)
				}
			}
			return strconv.FormatFloat(recv, 'f', prec, 64), true, nil
		case "toPrecision":
			if len(args) > 0 {
				if f, err := toFloat64(args[0]); err == nil {
					return strconv.FormatFloat(recv, 'g', int(f), 64), true, nil
				}
			}
			return str(recv), true, nil
		case "toString", "valueOf":
			return str(recv), true, nil
		case "toLocaleString":
			// QML: Number.toLocaleString(locale, format = 'f', precision).
			// Without arguments it is JS's own thousands-grouped form. The
			// locale itself is en-US-like here (comma grouping, dot point),
			// which is what the pages ask for.
			if len(args) == 0 {
				return groupThousands(recv), true, nil
			}
			verb, prec := byte('f'), 2
			if len(args) >= 2 {
				if f := str(args[1]); f != "" {
					verb = f[0]
				}
			}
			if len(args) >= 3 {
				if f, err := toFloat64(args[2]); err == nil {
					prec = int(f)
				}
			}
			return localeNumber(recv, verb, prec), true, nil
		}
		return nil, false, nil
	case string:
		switch name {
		case "toUpperCase":
			return strings.ToUpper(recv), true, nil
		case "toLowerCase":
			return strings.ToLower(recv), true, nil
		case "trim":
			return strings.TrimSpace(recv), true, nil
		case "trimStart":
			return strings.TrimLeft(recv, " \t\n\r"), true, nil
		case "trimEnd":
			return strings.TrimRight(recv, " \t\n\r"), true, nil
		case "charAt":
			i := argInt(args, 0)
			rs := []rune(recv)
			if i >= 0 && i < len(rs) {
				return string(rs[i]), true, nil
			}
			return "", true, nil
		case "indexOf":
			return float64(strings.Index(recv, strArgs(args, 0))), true, nil
		case "lastIndexOf":
			return float64(strings.LastIndex(recv, strArgs(args, 0))), true, nil
		case "includes":
			return strings.Contains(recv, strArgs(args, 0)), true, nil
		case "startsWith":
			return strings.HasPrefix(recv, strArgs(args, 0)), true, nil
		case "endsWith":
			return strings.HasSuffix(recv, strArgs(args, 0)), true, nil
		case "substring", "slice", "substr":
			rs := []rune(recv)
			start := argInt(args, 0)
			if start < 0 {
				if name == "slice" {
					start += len(rs)
				} else {
					start = 0
				}
			}
			end := len(rs)
			if len(args) > 1 {
				e := argInt(args, 1)
				if name == "substr" {
					e = start + e
				} else if e < 0 && name == "slice" {
					e += len(rs)
				}
				end = e
			}
			if start < 0 {
				start = 0
			}
			if end > len(rs) {
				end = len(rs)
			}
			if start > end {
				start = end
			}
			return string(rs[start:end]), true, nil
		case "split":
			sep := strArgs(args, 0)
			parts := strings.Split(recv, sep)
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out, true, nil
		case "replace":
			return strings.Replace(recv, strArgs(args, 0), strArgs(args, 1), 1), true, nil
		case "replaceAll":
			return strings.ReplaceAll(recv, strArgs(args, 0), strArgs(args, 1)), true, nil
		case "repeat":
			n := argInt(args, 0)
			if n < 0 {
				n = 0
			}
			return strings.Repeat(recv, n), true, nil
		case "padStart", "padEnd":
			// JS String.prototype.padStart/padEnd: pad to a target length
			// with a fill string (a space by default), truncating the last
			// repetition. Uses runes, so "0".padStart on CJK behaves.
			target := argInt(args, 0)
			fill := " "
			if len(args) > 1 {
				fill = str(args[1])
			}
			cur := len([]rune(recv))
			if fill == "" || target <= cur {
				return recv, true, nil
			}
			pad := []rune(strings.Repeat(fill, (target-cur)/len([]rune(fill))+1))[:target-cur]
			if name == "padStart" {
				return string(pad) + recv, true, nil
			}
			return recv + string(pad), true, nil
		case "toString", "valueOf":
			return recv, true, nil
		}
		return nil, false, nil
	case []any:
		switch name {
		case "join":
			sep := ","
			if len(args) > 0 {
				sep = str(args[0])
			}
			parts := make([]string, len(recv))
			for i, v := range recv {
				parts[i] = str(v)
			}
			return strings.Join(parts, sep), true, nil
		case "indexOf":
			for i, v := range recv {
				if str(v) == strArgs(args, 0) {
					return float64(i), true, nil
				}
			}
			return float64(-1), true, nil
		case "includes":
			for _, v := range recv {
				if str(v) == strArgs(args, 0) {
					return true, true, nil
				}
			}
			return false, true, nil
		case "slice":
			start, end := argInt(args, 0), len(recv)
			if start < 0 {
				start += len(recv)
			}
			if len(args) > 1 {
				end = argInt(args, 1)
				if end < 0 {
					end += len(recv)
				}
			}
			if start < 0 {
				start = 0
			}
			if end > len(recv) {
				end = len(recv)
			}
			if start > end {
				start = end
			}
			return append([]any{}, recv[start:end]...), true, nil
		case "map":
			out := make([]any, 0, len(recv))
			for i, v := range recv {
				r, err := callCallback(args, 0, v, i)
				if err != nil {
					return nil, true, err
				}
				out = append(out, r)
			}
			return out, true, nil
		case "filter":
			out := []any{}
			for i, v := range recv {
				r, err := callCallback(args, 0, v, i)
				if err != nil {
					return nil, true, err
				}
				if b, _ := toBool(r); b {
					out = append(out, v)
				}
			}
			return out, true, nil
		case "forEach":
			for i, v := range recv {
				if _, err := callCallback(args, 0, v, i); err != nil {
					return nil, true, err
				}
			}
			return nil, true, nil
		case "some", "every":
			want := name == "every"
			for i, v := range recv {
				r, err := callCallback(args, 0, v, i)
				if err != nil {
					return nil, true, err
				}
				b, _ := toBool(r)
				if name == "some" && b {
					return true, true, nil
				}
				if name == "every" && !b {
					return false, true, nil
				}
			}
			return want, true, nil
		case "find":
			for i, v := range recv {
				r, err := callCallback(args, 0, v, i)
				if err != nil {
					return nil, true, err
				}
				if b, _ := toBool(r); b {
					return v, true, nil
				}
			}
			return nil, true, nil
		case "reduce":
			fn, ok := args[0].(*userFunc)
			if !ok {
				return nil, true, nil
			}
			items := recv
			var acc any
			if len(args) > 1 {
				acc = args[1] // explicit initial value
			} else if len(items) > 0 {
				acc, items = items[0], items[1:] // seed with first element
			}
			for i, v := range items {
				r, err := callUserFunc(fn, []any{acc, v, float64(i)})
				if err != nil {
					return nil, true, err
				}
				acc = r
			}
			return acc, true, nil
		}
		return nil, false, nil
	}
	return nil, false, nil
}

// callCallback invokes args[i] (an arrow/userFunc) with (element, index). If
// it isn't callable, returns the element unchanged (lenient).
func callCallback(args []any, i int, elem any, idx int) (any, error) {
	if i >= len(args) {
		return elem, nil
	}
	if fn, ok := args[i].(*userFunc); ok {
		return callUserFunc(fn, []any{elem, float64(idx)})
	}
	if fn, ok := args[i].(jsFunc); ok {
		return fn([]any{elem, float64(idx)})
	}
	return elem, nil
}

func argInt(args []any, i int) int {
	if i < len(args) {
		if f, err := toFloat64(args[i]); err == nil {
			return int(f)
		}
	}
	return 0
}

func strArgs(args []any, i int) string {
	if i < len(args) {
		return str(args[i])
	}
	return ""
}

// callValue invokes a resolved callable with the given argument expressions.
// It dispatches jsFunc (variadic builtins) and the simple Go func shapes that
// Env.Func registers.
func callValue(fnv any, argExprs []expr, env *Env, line int, name string) (any, error) {
	switch fn := fnv.(type) {
	case *callableNS:
		return callValue(fn.call, argExprs, env, line, name)
	case jsFunc:
		args, err := evalArgs(argExprs, env)
		if err != nil {
			return nil, err
		}
		return fn(args)
	case func([]any) (any, error):
		args, err := evalArgs(argExprs, env)
		if err != nil {
			return nil, err
		}
		return fn(args)
	case *userFunc:
		args, err := evalArgs(argExprs, env)
		if err != nil {
			return nil, err
		}
		return callUserFunc(fn, args)
	case func():
		if len(argExprs) != 0 {
			return nil, fmt.Errorf("gml:%d: %s takes no arguments", line, name)
		}
		fn()
		return nil, nil
	case func(float64):
		if len(argExprs) != 1 {
			return nil, fmt.Errorf("gml:%d: %s takes one number", line, name)
		}
		v, err := argExprs[0].eval(env)
		if err != nil {
			return nil, err
		}
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		fn(f)
		return nil, nil
	case func(float64, float64):
		if len(argExprs) != 2 {
			return nil, fmt.Errorf("gml:%d: %s takes two numbers", line, name)
		}
		a0, err := argExprs[0].eval(env)
		if err != nil {
			return nil, err
		}
		a1, err := argExprs[1].eval(env)
		if err != nil {
			return nil, err
		}
		f0, err := toFloat64(a0)
		if err != nil {
			return nil, err
		}
		f1, err := toFloat64(a1)
		if err != nil {
			return nil, err
		}
		fn(f0, f1)
		return nil, nil
	case func(string):
		if len(argExprs) != 1 {
			return nil, fmt.Errorf("gml:%d: %s takes one string", line, name)
		}
		v, err := argExprs[0].eval(env)
		if err != nil {
			return nil, err
		}
		fn(str(v))
		return nil, nil
	}
	// A C++ type often exposes the same thing as both a property and an
	// invokable (QSqlQueryModel: `rowCount` reads as a property,
	// `columnCount()` is called), and which one the markup uses varies by
	// page. So a no-argument call on a plain value yields the value.
	if len(argExprs) == 0 {
		switch fnv.(type) {
		case float64, float32, int, string, colorVal, bool, []any, map[string]any:
			return fnv, nil
		}
	}
	return nil, fmt.Errorf("gml:%d: %q is not callable", line, name)
}

func evalArgs(args []expr, env *Env) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := a.eval(env)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// ---- conversions (QML-ish coercion) ----

func toFloat64(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case float32:
		return float64(t), nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	case string:
		return strconv.ParseFloat(t, 64)
	}
	return 0, fmt.Errorf("cannot use %T as number", v)
}

func toFloat32(v any) (float32, error) {
	f, err := toFloat64(v)
	return float32(f), err
}

func toInt(v any) (int, error) {
	f, err := toFloat64(v)
	return int(f), err
}

func toBool(v any) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case float64:
		return t != 0, nil
	case int:
		return t != 0, nil
	case string:
		return t != "", nil
	case nil:
		return false, nil // JS: null/undefined are falsy
	case []any, map[string]any:
		return true, nil // JS: objects and arrays (even empty) are truthy
	}
	// Every other value here is an OBJECT — an element's id object, a model,
	// a function — and in JS an object is truthy (`visible: someItem`,
	// `enabled: !!handler`). Only nil is falsy, handled above.
	return v != nil, nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case colorVal:
		return string(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', 6, 64)
	case int:
		return strconv.Itoa(t)
	case float32:
		return str(float64(t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// parseColor parses a QML color string. The parsing lives in ui
// (ui.ParseColor) so non-markup Go code can use it too; this is the
// engine-side alias.
// parseColor is ui.ParseColor with QML's 8-digit convention: "#AARRGGBB"
// puts the alpha FIRST (the 4-digit short form already agrees), so a
// document's "#FEA60140" is a near-opaque crimson, not a translucent
// orange. Everything else — names, functional forms, #rgb, #rrggbb —
// parses the same.
func parseColor(s string) (ui.RGBA, error) {
	if t := strings.TrimSpace(s); len(t) == 9 && t[0] == '#' {
		if _, err := strconv.ParseUint(t[1:], 16, 64); err == nil {
			t = "#" + t[3:] + t[1:3] // AARRGGBB -> RRGGBBAA
			return ui.ParseColor(t)
		}
	}
	return ui.ParseColor(s)
}

// localeNumber formats a number the way QML's Number.toLocaleString does
// with an explicit format: 'f' fixed-point (thousands-grouped), 'e'
// scientific, 'g' the shorter of the two.
func localeNumber(v float64, verb byte, prec int) string {
	switch verb {
	case 'e', 'E', 'g', 'G':
		return strconv.FormatFloat(v, verb, prec, 64)
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	out := b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

// groupThousands formats a float64 with comma-grouped integer digits
// (JS Number.prototype.toLocaleString's default en-US-like behavior).
func groupThousands(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

// jsTypeOf is JavaScript's typeof over our value space.
func jsTypeOf(v any) string {
	switch t := v.(type) {
	case nil:
		return "undefined"
	case bool:
		return "boolean"
	case string:
		return "string"
	case colorVal:
		return "object" // QML colours are objects, as in Qt's JS
	case float64, float32, int:
		return "number"
	case jsFunc, *userFunc:
		return "function"
	case *varBox:
		return jsTypeOf(t.v)
	case Ref:
		return jsTypeOf(t.get())
	}
	return "object"
}

// eval performs the write and yields JS's result value: the new value for an
// assignment, the value from before the write for a postfix increment.
func (x *updateExpr) eval(env *Env) (any, error) {
	if x.op == "++" || x.op == "--" {
		old, err := x.target.eval(env)
		if err != nil {
			return nil, err
		}
		f, err := toFloat64(old)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: %s wants a number", x.line, x.op)
		}
		next := f + 1
		if x.op == "--" {
			next = f - 1
		}
		if err := assign(x.target, &litExpr{next}, env); err != nil {
			return nil, err
		}
		if x.post {
			return f, nil
		}
		return next, nil
	}
	value := x.value
	if x.op != "=" {
		// `a += b` is `a = a + b` (the operator is the first character).
		value = &binExpr{op: x.op[:1], l: x.target, r: x.value, line: x.line}
	}
	v, err := value.eval(env)
	if err != nil {
		return nil, err
	}
	if err := assign(x.target, &litExpr{v}, env); err != nil {
		return nil, err
	}
	return v, nil
}

// isArrayMutator reports the JS Array methods that change the array in
// place (as opposed to returning a new one, like map/filter/slice).
func isArrayMutator(name string) bool {
	switch name {
	case "push", "pop", "shift", "unshift", "splice", "reverse", "sort":
		return true
	}
	return false
}

// mutateArray applies a JS array mutator, returning the new array and the
// method's own result value.
func mutateArray(arr []any, name string, args []any) (out []any, ret any) {
	out = append([]any{}, arr...)
	switch name {
	case "push":
		out = append(out, args...)
		return out, float64(len(out))
	case "pop":
		if len(out) == 0 {
			return out, nil
		}
		last := out[len(out)-1]
		return out[:len(out)-1], last
	case "shift":
		if len(out) == 0 {
			return out, nil
		}
		return out[1:], out[0]
	case "unshift":
		return append(append([]any{}, args...), out...), float64(len(out) + len(args))
	case "splice":
		start := clampIndex(args, 0, len(out))
		count := len(out) - start
		if len(args) > 1 {
			if n, err := toInt(args[1]); err == nil {
				count = min(max(n, 0), len(out)-start)
			}
		}
		removed := append([]any{}, out[start:start+count]...)
		rest := append([]any{}, out[start+count:]...)
		out = append(out[:start:start], args[min(2, len(args)):]...)
		return append(out, rest...), removed
	case "reverse":
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out, out
	case "sort":
		// Without a comparator JS compares as strings (a comparator is
		// handled by the caller, which can call it: sortWith).
		sortAnyStrings(out)
		return out, out
	}
	return out, nil
}

// clampIndex reads argument i as an array index, JS-style: negative counts
// from the end, out-of-range clamps.
func clampIndex(args []any, i, length int) int {
	if i >= len(args) {
		return 0
	}
	n, err := toInt(args[i])
	if err != nil {
		return 0
	}
	if n < 0 {
		n += length
	}
	return min(max(n, 0), length)
}

// sortAnyStrings sorts values by their string form (JS's default order).
func sortAnyStrings(vs []any) {
	sort.SliceStable(vs, func(i, j int) bool { return str(vs[i]) < str(vs[j]) })
}

// dateObject is `new Date()` as our value space sees it: the JS getters as
// callables, the epoch milliseconds for arithmetic and formatting.
// jsDate builds the time `new Date(...)` names: now with no arguments,
// epoch milliseconds or a parsed string with one, and the (year, month,
// day, hours, minutes, seconds, ms) form with more — months 0-based, and
// out-of-range parts normalized as JS does, so the "day 0 of next month"
// idiom really is the last day of this one (`new Date(y, m + 1, 0)` is how
// QML pages count the days in a month).
func jsDate(args []any) time.Time {
	switch len(args) {
	case 0:
		return timeNow()
	case 1:
		if s, ok := args[0].(string); ok {
			if t, ok := parseJSDate(s); ok {
				return t
			}
			return timeNow()
		}
		if ms, err := toFloat64(args[0]); err == nil {
			return time.UnixMilli(int64(ms))
		}
		return timeNow()
	}
	part := func(i int, def int) int {
		if i >= len(args) {
			return def
		}
		f, err := toFloat64(args[i])
		if err != nil {
			return def
		}
		return int(f)
	}
	// time.Date normalizes exactly the way JS's constructor does: month 12
	// rolls the year, day 0 is the previous month's last day.
	return time.Date(part(0, 0), time.Month(part(1, 0)+1), part(2, 1),
		part(3, 0), part(4, 0), part(5, 0), part(6, 0)*int(time.Millisecond),
		timeNow().Location())
}

// parseJSDate reads the date-string forms a page is likely to write.
func parseJSDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05",
		"2006-01-02 15:04:05", "2006-01-02", "2006/01/02", "01/02/2006",
	} {
		if t, err := time.ParseInLocation(layout, s, timeNow().Location()); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func dateObject(t time.Time) map[string]any {
	num := func(v int) jsFunc {
		return jsFunc(func([]any) (any, error) { return float64(v), nil })
	}
	pad := func(v int) string {
		if v < 10 {
			return "0" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	}
	clock := pad(t.Hour()) + ":" + pad(t.Minute()) + ":" + pad(t.Second())
	date := strconv.Itoa(t.Year()) + "-" + pad(int(t.Month())) + "-" + pad(t.Day())
	return map[string]any{
		"time":               float64(t.UnixMilli()),
		"getTime":            num(int(t.UnixMilli())),
		"getFullYear":        num(t.Year()),
		"getMonth":           num(int(t.Month()) - 1), // JS months are 0-based
		"getDate":            num(t.Day()),
		"getDay":             num(int(t.Weekday())),
		"getHours":           num(t.Hour()),
		"getMinutes":         num(t.Minute()),
		"getSeconds":         num(t.Second()),
		"getMilliseconds":    num(t.Nanosecond() / 1e6),
		"toLocaleTimeString": jsFunc(func([]any) (any, error) { return clock, nil }),
		"toLocaleDateString": jsFunc(func([]any) (any, error) { return date, nil }),
		"toLocaleString":     jsFunc(func([]any) (any, error) { return date + " " + clock, nil }),
		"toISOString": jsFunc(func([]any) (any, error) {
			return t.UTC().Format("2006-01-02T15:04:05.000Z"), nil
		}),
		"toString": jsFunc(func([]any) (any, error) { return date + " " + clock, nil }),
		"valueOf":  num(int(t.UnixMilli())),
	}
}

// callableNS is a JS global that is BOTH a function and a namespace —
// `String(x)` converts, `String.fromCharCode(n)` builds. A plain jsFunc has
// no members and a plain map is not callable, so neither alone models one.
type callableNS struct {
	call    jsFunc
	members map[string]any
}

// stringGlobal is JS's String: the conversion function plus the static
// members code actually calls. String.fromCharCode is how the matrix-rain
// canvas picks its katakana (`String.fromCharCode(0x30A0 + ...)`).
func stringGlobal() *callableNS {
	return &callableNS{
		call: func(args []any) (any, error) {
			if len(args) == 0 {
				return "", nil
			}
			return str(args[0]), nil
		},
		members: map[string]any{
			"fromCharCode": jsFunc(func(args []any) (any, error) {
				var b strings.Builder
				for _, a := range args {
					f, err := toFloat64(a)
					if err != nil {
						return "", nil
					}
					b.WriteRune(rune(int(f)))
				}
				return b.String(), nil
			}),
		},
	}
}

// Scroll-state tokens. A scroll offset and a content size live on the
// retained ScrollState, outside the property system, so a read of one used
// to leave the subtree UNTRACKED — stale for ever, rebuilt every frame.
// Every ScrollState now has a token carrying a hash of what a binding can
// read off it (offsets, content and view sizes), stamped once per frame by
// beginFrame; a read Gets the token, so the subtree is stale exactly when
// the view moved or its content changed. The registry is process-wide:
// states belong to one document each, and there is one goroutine.
var scrollToks = map[*ui.ScrollState]*core.Property[uint64]{}

func scrollTok(s *ui.ScrollState) *core.Property[uint64] {
	t := scrollToks[s]
	if t == nil {
		t = &core.Property[uint64]{}
		t.Set(scrollHash(s))
		scrollToks[s] = t
	}
	return t
}

func scrollHash(s *ui.ScrollState) uint64 {
	h := uint64(1469598103934665603)
	for _, f := range [...]float32{s.X, s.Y, s.ContentW(), s.ContentH(), s.ViewW(), s.ViewH()} {
		h ^= uint64(math.Float32bits(f))
		h *= 1099511628211
	}
	return h
}

// stampScrollTokens writes every token's current hash (a no-op while the
// state is unchanged).
func stampScrollTokens() {
	for s, t := range scrollToks {
		t.Set(scrollHash(s))
	}
}

// trText is a translation call's result with no catalogue loaded: the
// source text at args[at], with %n replaced by the count two places after
// it (qsTr(text, disambiguation, n)), as Qt does for the untranslated case.
func trText(args []any, at int) string {
	if at >= len(args) {
		return ""
	}
	s := str(args[at])
	if at+2 < len(args) {
		if n, err := toFloat64(args[at+2]); err == nil {
			s = strings.ReplaceAll(s, "%n", strconv.FormatFloat(n, 'f', -1, 64))
		}
	}
	return s
}

// expandArgs evaluates call arguments, flattening `...expr` ones into the
// list, and returns them as literal expressions.
func expandArgs(args []expr, env *Env) ([]expr, error) {
	out := make([]expr, 0, len(args))
	for _, a := range args {
		sp, isSpread := a.(*spreadExpr)
		if !isSpread {
			v, err := a.eval(env)
			if err != nil {
				return nil, err
			}
			out = append(out, &litExpr{v})
			continue
		}
		v, err := sp.inner.eval(env)
		if err != nil {
			return nil, err
		}
		if r, ok := v.(Ref); ok {
			v = r.get()
		}
		switch t := v.(type) {
		case []any:
			for _, item := range t {
				out = append(out, &litExpr{item})
			}
		case string: // a string spreads into its characters, as in JS
			for _, c := range t {
				out = append(out, &litExpr{string(c)})
			}
		case nil:
			return nil, fmt.Errorf("gml: cannot spread %v", v)
		default:
			return nil, fmt.Errorf("gml: cannot spread a %T", v)
		}
	}
	return out, nil
}

// assignable reports whether a mutator's receiver names a place the
// changed array can be written back to (a variable, a property, an
// element), rather than a temporary such as a call's result.
func assignable(e expr) bool {
	switch e.(type) {
	case *identExpr, *memberExpr, *indexExpr:
		return true
	}
	return false
}

// sortWith sorts vs in place by a JS comparator: negative puts a first.
// The sort is stable, as JS's is since ES2019.
func sortWith(vs []any, cmp any) error {
	var firstErr error
	sort.SliceStable(vs, func(i, j int) bool {
		if firstErr != nil {
			return false
		}
		r, err := callAny(cmp, []any{vs[i], vs[j]})
		if err != nil {
			firstErr = err
			return false
		}
		f, err := toFloat64(r)
		if err != nil {
			return false
		}
		return f < 0
	})
	return firstErr
}

// optionalChain reports whether a callee is reached through `?.` anywhere
// along its member chain (o?.f, o?.a.f).
func optionalChain(e expr) bool {
	for {
		m, ok := e.(*memberExpr)
		if !ok {
			return false
		}
		if m.optional {
			return true
		}
		e = m.base
	}
}

// parentOrigin is the window position of the item this element's x/y are
// relative to. Where the enclosing item is known — a styled control's
// delegate, or any element whose parent view we hold — it is that item's
// origin; window coordinates (0, 0) are the fallback, not the meaning.
func (o *elemObject) parentOrigin() (float32, float32) {
	if o.orgX != 0 || o.orgY != 0 {
		return o.orgX, o.orgY
	}
	// The enclosing item's origin, resolved against the live registry: an
	// object captured at declare time holds no element (nothing was laid
	// out yet), and without the offset x/y read as window coordinates —
	// which is what a page positioning something by `-root.x` inside a
	// component then lands off by.
	if o.parentObj != nil && o.parentObj.el != nil {
		pr := ui.Bounds(o.parentObj.el)
		return pr.X, pr.Y
	}
	if o.doc != nil && o.parentKey != 0 {
		if pe, ok := o.doc.prevElems.get2(o.parentKey); ok {
			pr := ui.Bounds(pe)
			return pr.X, pr.Y
		}
	}
	return 0, 0
}

// sameObject is JS object identity: the same pointer, map, slice or
// function, or equal comparable values otherwise.
func sameObject(a, b any) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Type() != vb.Type() {
		return false
	}
	switch va.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return va.Pointer() == vb.Pointer()
	}
	if va.Comparable() {
		return va.Equal(vb)
	}
	return false
}
