package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestDialogButtonsAndDismiss(t *testing.T) {
	var open core.Property[bool]
	var log []string
	var dlg Element
	u := New(nil, nil, func() Element {
		dlg = Dialog(&open, Text("Overwrite?", RGB(0)), DialogOpts{
			Title: "Save", Buttons: ButtonOk | ButtonCancel,
			OnAccepted: func() { log = append(log, "accepted") },
			OnRejected: func() { log = append(log, "rejected") },
			OnClosed:   func() { log = append(log, "closed") },
		})
		return Div().Children(dlg)
	})
	frame := func() { u.Layout(800, 600) }
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: x, Y: y, Button: 1}, {Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	// button finds the button box's i-th button.
	button := func(i int) Rect {
		var box Element
		var walk func(Element)
		walk = func(e Element) {
			for _, c := range Children(e) {
				if d, ok := c.(*DivEl); ok && d.rect.H == dialogFooterH {
					box = d
				}
				walk(c)
			}
		}
		walk(dlg)
		if box == nil {
			t.Fatal("no button box")
		}
		return Bounds(Children(box)[i])
	}
	frame()
	if len(Children(dlg)) != 0 {
		t.Fatal("a closed dialog drew something")
	}
	for _, c := range []struct {
		i    int
		want string
	}{{0, "accepted"}, {1, "rejected"}} {
		log = nil
		open.Set(true)
		frame()
		r := button(c.i)
		if r.W != dialogButtonW {
			t.Fatalf("button %d is %v wide, want %d", c.i, r.W, dialogButtonW)
		}
		click(r.X+r.W/2, r.Y+r.H/2)
		if open.Get() || len(log) != 2 || log[0] != c.want || log[1] != "closed" {
			t.Errorf("button %d: open %v, log %v; want closed, [%s closed]", c.i, open.Get(), log, c.want)
		}
	}
	// A click on the scrim dismisses: closed, but neither role ran.
	log = nil
	open.Set(true)
	frame()
	click(5, 5)
	if open.Get() || len(log) != 1 || log[0] != "closed" {
		t.Errorf("scrim click: open %v, log %v; want closed, [closed]", open.Get(), log)
	}
	if ButtonSaveAll.Label() != "Save All" || !ButtonApply.Accepts() || ButtonDiscard.Accepts() {
		t.Error("standard button labels/roles")
	}
}
