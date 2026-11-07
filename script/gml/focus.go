package gml

import (
	"fmt"
	"time"

	"github.com/sushvibez/gfx/ui"
)

// Keyboard focus for QML pages, over ui's FocusID / KeyNav / RequestFocus /
// OnFocusChange (ui/focus.go):
//
//   - every registered element is named by its registry key, so focus can
//     be moved to it;
//   - KeyNavigation.left/right/up/down/tab/backtab name the item the key
//     moves focus to;
//   - forceActiveFocus(), and `item.focus = true` from a handler, focus
//     the item on the next build;
//   - item.activeFocus reads whether it holds focus (from the frame after
//     it is first read, which is when it starts being tracked);
//   - activeFocusOnTab: true puts a plain item in the Tab chain.

// keyNavDirs maps the KeyNavigation attached properties to ui directions.
var keyNavDirs = map[string]ui.NavDir{
	"KeyNavigation.left": ui.NavLeft, "KeyNavigation.right": ui.NavRight,
	"KeyNavigation.up": ui.NavUp, "KeyNavigation.down": ui.NavDown,
	"KeyNavigation.tab": ui.NavTab, "KeyNavigation.backtab": ui.NavBacktab,
}

// keyNav is one KeyNavigation target, resolved to a focus name.
type keyNav struct {
	dir ui.NavDir
	id  string
}

// readKeyNav takes a KeyNavigation.* attribute, reporting whether it was
// one (priority and enabled are accepted and not modelled).
func (doc *docState) readKeyNav(a attr, env *Env, navs *[]keyNav) bool {
	switch a.name {
	case "KeyNavigation.priority", "KeyNavigation.enabled":
		return true
	}
	dir, ok := keyNavDirs[a.name]
	if !ok {
		return false
	}
	if a.expr != nil {
		if v, err := a.expr.eval(env); err == nil {
			if id, ok := doc.focusIDOf(v); ok {
				*navs = append(*navs, keyNav{dir, id})
			}
		}
	}
	return true
}

// focusIDOf is the focus name of the item v refers to.
func (doc *docState) focusIDOf(v any) (string, bool) {
	// A text field's id is its input state: back to the element.
	if in, ok := v.(*ui.InputState); ok {
		if key, ok := doc.inputKeys[in]; ok {
			return doc.keyStr(key), true
		}
		return "", false
	}
	eo, ok := v.(*elemObject)
	if !ok || eo == nil {
		return "", false
	}
	if key := doc.focusKeyOf(eo); key != 0 {
		return doc.keyStr(key), true
	}
	return "", false
}

// focusKeyOf is the registry key the element eo refers to was built under.
func (doc *docState) focusKeyOf(eo *elemObject) keyID {
	key := eo.key
	// An id'd item inside a component instance is registered under its
	// instance's key, not the document-wide one its id object carries.
	if eo.node != nil {
		if id := nodeID(eo.node); id != "" {
			if k, ok := doc.idKeys.get2(doc.intern(id)); ok {
				key = k
			}
		}
	}
	return key
}

// stampFocus names a freshly registered element for focus moves and
// applies what is pending for it: a forceActiveFocus, an activeFocus
// being read.
func (doc *docState) stampFocus(key keyID, el ui.Element) {
	id := doc.keyStr(key)
	req := doc.focusReq[key]
	if req {
		delete(doc.focusReq, key)
	}
	ref := doc.focusRefs[key]
	var report func(bool)
	if ref != nil {
		report = func(on bool) {
			setBool(ref, on)
			// Reports run outer to inner, so the last item that has focus
			// is the innermost: Window.activeFocusItem.
			cur := doc.focusedKey()
			if on && cur != key {
				_ = doc.focusedRef().set(float64(key))
			} else if !on && cur == key {
				_ = doc.focusedRef().set(0.0)
			}
		}
	}
	switch e := el.(type) {
	case *ui.DivEl:
		e.FocusID(id)
		if req {
			e.RequestFocus()
		}
		if report != nil {
			e.OnFocusChange(report)
		}
	case *ui.InputEl:
		e.FocusID(id)
		if req {
			e.RequestFocus()
		}
		if report != nil {
			e.OnFocusChange(report)
		}
	}
}

// requestFocus focuses the element under key on the next build.
func (doc *docState) requestFocus(key keyID) {
	if key == 0 {
		return
	}
	if doc.focusReq == nil {
		doc.focusReq = map[keyID]bool{}
	}
	doc.focusReq[key] = true
	doc.invalidateCache() // a cached subtree would never rebuild to take it
}

// activeFocusRef is the tracked activeFocus of the element under key,
// created on first read; a late first read rebuilds so the element
// starts reporting.
func (doc *docState) activeFocusRef(key keyID) Ref {
	r, fresh := doc.trackFocus(key, nil)
	if fresh {
		doc.invalidateCache()
	}
	return r
}

// trackFocus is activeFocusRef for an element being declared, which
// builds fresh anyway: the id objects seed activeFocus with it, and obj
// is what activeFocusItem names while the element holds focus.
func (doc *docState) trackFocus(key keyID, obj any) (r Ref, fresh bool) {
	if obj != nil {
		if doc.focusObjs == nil {
			doc.focusObjs = map[keyID]any{}
		}
		doc.focusObjs[key] = obj
	}
	if doc.focusRefs == nil {
		doc.focusRefs = map[keyID]Ref{}
	}
	if r = doc.focusRefs[key]; r == nil {
		r, fresh = doc.boolProp(doc.keyStr(key)+".activeFocus", false), true
		doc.focusRefs[key] = r
	}
	return r, fresh
}

var activeFocusName = map[string]bool{"activeFocus": true}

// focusScope binds an item's own activeFocus by bare name for its
// attributes and handlers (`color: activeFocus ? … : …`), as QML's item
// scope resolves it — only for an item that mentions it, since binding it
// starts tracking that item's focus.
func (doc *docState) focusScope(n *node, env *Env) *Env {
	if doc.readsFocus == nil {
		doc.readsFocus = map[*node]bool{}
	}
	reads, seen := doc.readsFocus[n]
	if !seen {
		for i := range n.attrs {
			if attrMentions(&n.attrs[i], activeFocusName) {
				reads = true
				break
			}
		}
		doc.readsFocus[n] = reads
	}
	if !reads {
		return env
	}
	s := env.child()
	s.vars["activeFocus"] = doc.activeFocusRef(doc.keyID(n))
	return s
}

// activeFocusItem is the id object (or a text field's state) of the
// innermost tracked item holding focus, or nil.
func (doc *docState) activeFocusItem() any {
	if k := doc.focusedKey(); k != 0 {
		return doc.focusObjs[k]
	}
	return nil
}

// focusedRef holds the innermost tracked focused item's key: a document
// property, so a binding reading activeFocusItem follows focus moves.
func (doc *docState) focusedRef() Ref { return doc.floatProp("__activeFocusItem", 0) }

func (doc *docState) focusedKey() keyID {
	f, _ := toFloat64(doc.focusedRef().get())
	return keyID(f)
}

// windowAttached is QML's attached Window object, readable from any item:
// Window.activeFocusItem, and the QWindow::Visibility values pages state
// (`visibility: Window.Maximized`).
func (doc *docState) windowAttached() map[string]any {
	return map[string]any{
		"activeFocusItem": computedRef{fn: doc.activeFocusItem},
		"Hidden":          float64(0), "AutomaticVisibility": float64(1), "Windowed": float64(2),
		"Minimized": float64(3), "Maximized": float64(4), "FullScreen": float64(5),
	}
}

// computedRef is a read-only Ref whose value is computed on each read; the
// computation's own property reads record the dependencies.
type computedRef struct{ fn func() any }

func (r computedRef) get() any { return r.fn() }
func (r computedRef) set(any) error {
	return fmt.Errorf("gml: a read-only property cannot be assigned")
}
func (r computedRef) animate(any, time.Duration) error { return nil }
func (r computedRef) onChange(func())                  {}
