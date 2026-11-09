package ui

import "github.com/sushvibez/gfx/app"

// Keyboard focus beyond Tab order: an element can be named (FocusID) so
// focus can be moved to it across frames — the tree is rebuilt every
// frame, so focus is otherwise only a position in the frame's focusables
// — and can name where the arrow keys and Tab take focus from it (KeyNav,
// QML's KeyNavigation). RequestFocus takes focus on the frame it is built
// (QML's forceActiveFocus()), and OnFocusChange reports whether focus is
// on the element or inside it after every layout (QML's activeFocus).
//
// A focus scope (FocusScope, QML's FocusScope) passes focus on: focus sent
// to it — a RequestFocus, a KeyNav or FocusID naming it — goes to the
// focusable inside it that asked for the initial focus (Focus), or else
// its first focusable, and it reports focus while anything inside it
// holds it. Any other element takes focus itself.

// NavDir is a direction KeyNav moves focus in.
type NavDir int

const (
	NavLeft    NavDir = iota // the Left arrow
	NavRight                 // the Right arrow
	NavUp                    // the Up arrow
	NavDown                  // the Down arrow
	NavTab                   // Tab
	NavBacktab               // Shift+Tab
	navDirs
)

// focusMeta is an element's focus identity and navigation, and the run of
// the frame's focusables that it and its descendants registered
// ([start, end), filled in during layout).
type focusMeta struct {
	id         string
	nav        [navDirs]string
	request    bool
	onChange   func(bool)
	scope      bool
	start, end int
}

// active reports whether the element takes part in focus resolution.
func (m *focusMeta) active() bool { return m.id != "" || m.request || m.onChange != nil }

func (d *DivEl) meta() *focusMeta   { return &d.fm }
func (e *InputEl) meta() *focusMeta { return &e.fm }

// FocusID names the element for focus moves (KeyNav, UI.FocusID). The name
// must be stable across frames and unique in the frame.
func (d *DivEl) FocusID(id string) *DivEl { d.fm.id = id; return d }

// KeyNav makes the key for dir move focus from this element to the one
// named id (FocusID), before the element's own key handlers see it (QML's
// KeyNavigation with its default BeforeItem priority). Implies Focusable.
func (d *DivEl) KeyNav(dir NavDir, id string) *DivEl {
	d.fm.nav[dir] = id
	d.focusable = true
	return d
}

// RequestFocus takes keyboard focus on the frame this element is built,
// once, from whatever holds it (QML forceActiveFocus()). It makes the
// element focusable, except a FocusScope, which passes focus inside.
func (d *DivEl) RequestFocus() *DivEl {
	d.fm.request = true
	if !d.fm.scope {
		d.focusable = true
	}
	return d
}

// FocusScope makes the element a focus scope (see above). Call it before
// RequestFocus.
func (d *DivEl) FocusScope() *DivEl { d.fm.scope = true; return d }

// OnFocusChange reports, after every layout, whether focus is on the
// element (or, for a FocusScope, inside it). It does not make the element
// focusable.
func (d *DivEl) OnFocusChange(fn func(focused bool)) *DivEl {
	d.fm.onChange = fn
	return d
}

// FocusID, KeyNav, RequestFocus and OnFocusChange on a text field, as on
// a DivEl.
func (e *InputEl) FocusID(id string) *InputEl { e.fm.id = id; return e }

func (e *InputEl) KeyNav(dir NavDir, id string) *InputEl { e.fm.nav[dir] = id; return e }

func (e *InputEl) RequestFocus() *InputEl { e.fm.request = true; return e }

func (e *InputEl) OnFocusChange(fn func(focused bool)) *InputEl { e.fm.onChange = fn; return e }

// metaOf is el's focus metadata, or nil.
func metaOf(el Element) *focusMeta {
	if m, ok := el.(interface{ meta() *focusMeta }); ok {
		return m.meta()
	}
	return nil
}

// beginFocus is called by an element's layout before it registers itself
// as focusable; the returned func, deferred, closes its run of focusables
// once its children are laid out. Nothing happens for an element without
// focus metadata.
func (ctx *Context) beginFocus(el Element, m *focusMeta) func() {
	if !m.active() {
		return noFocusEnd
	}
	m.start = len(ctx.focusables)
	ctx.focusMetas = append(ctx.focusMetas, el)
	return func() { m.end = len(ctx.focusables) }
}

func noFocusEnd() {}

// target is the focusable index focus sent to m goes to, or -1: the
// element itself, else (a scope) what asked for the initial focus inside
// it, else its first focusable.
func (u *UI) target(el Element, m *focusMeta) int {
	if m.start >= m.end || m.end > len(u.ctx.focusables) {
		return -1
	}
	if u.ctx.focusables[m.start] == el {
		return m.start
	}
	for i := m.start; i < m.end; i++ {
		if wf, ok := u.ctx.focusables[i].(interface{ wantsInitialFocus() bool }); ok && wf.wantsInitialFocus() {
			return i
		}
	}
	return m.start
}

// FocusID focuses the element named id in the last layout (or, for a
// scope, the focusable inside it), reporting whether there was one.
func (u *UI) FocusID(id string) bool {
	for _, el := range u.ctx.focusMetas {
		if m := metaOf(el); m != nil && m.id == id {
			if i := u.target(el, m); i >= 0 {
				u.setFocusIdx(i)
				return true
			}
		}
	}
	return false
}

// FocusedID is the name of the focused element ("" when nothing named
// holds focus).
func (u *UI) FocusedID() string {
	if m := metaOf(u.focusedEl()); m != nil {
		return m.id
	}
	return ""
}

// setFocusIdx moves focus to focusable i, keeping a text field's caret
// state in step.
func (u *UI) setFocusIdx(i int) {
	u.focusIdx = i
	if in, ok := u.focusedEl().(*InputEl); ok {
		u.ctx.setFocus(in.state)
	} else {
		u.ctx.setFocus(nil)
	}
}

// resolveFocusRequests runs after layout: a RequestFocus takes focus (the
// last one built wins), then every OnFocusChange hears the outcome.
func (u *UI) resolveFocusRequests() {
	for _, el := range u.ctx.focusMetas {
		if m := metaOf(el); m != nil && m.request {
			m.request = false // one-shot: an element kept across frames asks once
			if i := u.target(el, m); i >= 0 {
				u.focusIdx = i
			}
		}
	}
	for i, f := range u.ctx.focusables {
		if in, ok := f.(*InputEl); ok && in.state != nil && in.state.focusReq {
			u.focusIdx = i
			in.state.focusReq = false
		}
	}
	for _, el := range u.ctx.focusMetas {
		if m := metaOf(el); m != nil && m.onChange != nil {
			in := u.focusIdx >= m.start && u.focusIdx < m.end
			if !m.scope {
				in = in && u.ctx.focusables[u.focusIdx] == el // on it, not inside it
			}
			m.onChange(in)
		}
	}
}

// keyNav moves focus by the focused element's KeyNav for ev, reporting
// whether it did.
func (u *UI) keyNav(ev app.KeyEvent) bool {
	if !ev.Down || ev.Ctrl || ev.Alt {
		return false
	}
	m := metaOf(u.focusedEl())
	if m == nil {
		return false
	}
	dir := NavDir(-1)
	switch ev.Key {
	case app.KeyLeft:
		dir = NavLeft
	case app.KeyRight:
		dir = NavRight
	case app.KeyUp:
		dir = NavUp
	case app.KeyDown:
		dir = NavDown
	case app.KeyTab:
		dir = NavTab
		if ev.Shift {
			dir = NavBacktab
		}
	}
	if dir < 0 || m.nav[dir] == "" {
		return false
	}
	if !u.FocusID(m.nav[dir]) {
		return false
	}
	u.focusVisible = true // keyboard-moved focus is focus-visible
	return true
}

// RequestFocus focuses the field this state belongs to at the next layout
// (QML forceActiveFocus() on a TextField) — for code that holds the state
// rather than the element.
func (s *InputState) RequestFocus() { s.focusReq = true }

// TextFocused reports whether a text field holds the keyboard: a host's
// own single-key shortcuts (Delete, F, letters) should leave the keys to
// it.
func (u *UI) TextFocused() bool { return u.ctx.focus != nil }
