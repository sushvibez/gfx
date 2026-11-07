package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestKeyNavAndFocusRequests(t *testing.T) {
	field := NewInput("")
	focused := map[string]bool{}
	request := ""
	u := New(nil, nil, func() Element {
		box := func(id string) *DivEl {
			d := Div().Size(40, 40).Focusable().FocusID(id).OnFocusChange(func(on bool) { focused[id] = on })
			if id == request {
				d.RequestFocus()
			}
			return d
		}
		a := box("a").KeyNav(NavRight, "c").KeyNav(NavDown, "field")
		b := box("b")
		c := box("c").KeyNav(NavLeft, "a").KeyNav(NavTab, "a")
		in := Input(field, 100).FocusID("field").KeyNav(NavUp, "b")
		return Div().Col().Children(Div().Row().Children(a, b, c), in)
	})
	frame := func() { u.Layout(300, 200) }
	key := func(k app.Key, shift bool) {
		u.Key(app.KeyEvent{Down: true, Key: k, Shift: shift})
		frame()
	}
	frame()
	if !u.FocusID("a") {
		t.Fatal("FocusID(a) found nothing")
	}
	frame()
	if !focused["a"] || focused["b"] {
		t.Fatalf("OnFocusChange: %v, want only a", focused)
	}
	key(app.KeyRight, false) // a → c, skipping b
	if got := u.FocusedID(); got != "c" {
		t.Errorf("Right from a: %q, want c", got)
	}
	key(app.KeyTab, false) // c's Tab is KeyNav'd back to a
	if got := u.FocusedID(); got != "a" {
		t.Errorf("Tab from c: %q, want a (KeyNav overrides the chain)", got)
	}
	key(app.KeyDown, false) // a → the text field
	if got := u.FocusedID(); got != "field" {
		t.Errorf("Down from a: %q, want field", got)
	}
	u.Key(app.KeyEvent{Down: true, Rune: 'x'})
	frame()
	if field.Text.Get() != "x" {
		t.Errorf("the field reached by KeyNav does not type: %q", field.Text.Get())
	}
	key(app.KeyUp, false) // field → b
	if got := u.FocusedID(); got != "b" {
		t.Errorf("Up from the field: %q, want b", got)
	}
	key(app.KeyTab, false) // b has no KeyNav: the plain Tab chain, to c
	if got := u.FocusedID(); got != "c" {
		t.Errorf("Tab from b: %q, want c (the chain)", got)
	}
	request = "b" // RequestFocus takes focus from c on the frame it is built
	frame()
	if got := u.FocusedID(); got != "b" || !focused["b"] || focused["c"] {
		t.Errorf("after RequestFocus: %q, %v; want b", got, focused)
	}
}

// A FocusScope passes focus to the focusable inside it that asked for it,
// and reports focus while anything inside it holds it; a plain parent
// reports only its own.
func TestFocusScope(t *testing.T) {
	seen := map[string]bool{}
	reqScope := false
	u := New(nil, nil, func() Element {
		inner1 := Div().Size(30, 30).Focusable().FocusID("in1")
		inner2 := Div().Size(30, 30).Focus().FocusID("in2")
		scope := Div().Row().FocusScope().FocusID("scope").
			OnFocusChange(func(on bool) { seen["scope"] = on }).Children(inner1, inner2)
		if reqScope {
			scope.RequestFocus()
		}
		plain := Div().Row().FocusID("plain").OnFocusChange(func(on bool) { seen["plain"] = on }).
			Children(Div().Size(30, 30).Focusable().FocusID("p1"))
		start := Div().Size(30, 30).Focusable().FocusID("start").KeyNav(NavRight, "scope")
		return Div().Col().Children(start, scope, plain)
	})
	frame := func() { u.Layout(300, 200) }
	frame()
	u.FocusID("start")
	frame()
	u.Key(app.KeyEvent{Down: true, Key: app.KeyRight}) // KeyNav to the scope
	frame()
	if got := u.FocusedID(); got != "in2" {
		t.Errorf("KeyNav to the scope focused %q, want in2 (its Focus child)", got)
	}
	if !seen["scope"] {
		t.Error("the scope does not report the focus inside it")
	}
	u.FocusID("p1")
	frame()
	if seen["plain"] {
		t.Error("a plain parent reported its child's focus as its own")
	}
	if seen["scope"] {
		t.Error("the scope still reports focus after it left")
	}
	reqScope = true // RequestFocus on the scope: inside again
	frame()
	if got := u.FocusedID(); got != "in2" {
		t.Errorf("RequestFocus on the scope focused %q, want in2", got)
	}
}
