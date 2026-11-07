package ui

import "github.com/sushvibez/gfx/app"

// Keyboard focus beyond Tab order: an element can be named (FocusID) so
// focus can be moved to it across frames â the tree is rebuilt every
// frame, so focus is otherwise only a position in the frame's focusables
// â and can name where the arrow keys and Tab take focus from it (KeyNav,
// QML's KeyNavigation). RequestFocus takes focus on the frame it is built
// (QML's forceActiveFocus()), and OnFocusChange reports whether the
// element holds focus after every layout (QML's activeFocus).

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

// focusMeta is an element's focus identity and navigation.
type focusMeta struct {
	id       string
	nav      [navDirs]string
	request  bool
	onChange func(bool)
}

func (d *DivEl) meta() *focusMeta   { return &d.fm }
func (e *InputEl) meta() *focusMeta { return &e.fm }

// FocusID names the element for focus moves (KeyNav, UI.FocusID). The name
// must be stable across frames and unique among the frame's focusables.
func (d *DivEl) FocusID(id string) *DivEl { d.fm.id = id; return d }

// KeyNav makes the key for dir move focus from this element to the
// focusable named id (FocusID), before the element's own key handlers
// see it (QML's KeyNavigation with its default BeforeItem priority).
// Implies Focusable.
func (d *DivEl) KeyNav(dir NavDir, id string) *DivEl {
	d.fm.nav[dir] = id
	d.focusable = true
	return d
}

// RequestFocus takes keyboard focus on the frame this element is built,
// from whatever holds it (QML forceActiveFocus()). Implies Focusable.
func (d *DivEl) RequestFocus() *DivEl {
	d.fm.request = true
	d.focusable = true
	return d
}

// OnFocusChange reports, after every layout, whether the element holds
// keyboard focus. It does not make the element focusable: one that is not
// (Focusable, OnKey, KeyNav, …) is never told anything.
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

// FocusID focuses the focusable named id among the last layout's,
// reporting whether there was one.
func (u *UI) FocusID(id string) bool {
	for i, f := range u.ctx.focusables {
		if m := metaOf(f); m != nil && m.id == id {
			u.setFocusIdx(i)
			return true
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
	for i, f := range u.ctx.focusables {
		if m := metaOf(f); m != nil && m.request {
			u.focusIdx = i
			m.request = false // one-shot: an element kept across frames asks once
		}
		if in, ok := f.(*InputEl); ok && in.state != nil && in.state.focusReq {
			u.focusIdx = i
			in.state.focusReq = false
		}
	}
	for i, f := range u.ctx.focusables {
		if m := metaOf(f); m != nil && m.onChange != nil {
			m.onChange(i == u.focusIdx)
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
