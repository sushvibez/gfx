package ui

import (
	"math"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// Tumbler (QML's Tumbler) is a vertical wheel of items centred on the
// current one: Visible slots, each item drawn by the caller with its
// displacement from the centre (0 for the current item, ±1 for its
// neighbours, which a delegate fades and shrinks by). A click picks an
// item, Up/Down step, a drag turns it a row at a time, and a release that
// is still moving coasts on and settles on an item.

// TumblerState is a wheel's retained state: its continuous position (which
// a fling animates) and the drag in progress. Keep one per wheel.
type TumblerState struct {
	spin     core.Property[float32]
	dragFrom int
	dragged  bool   // the drag in progress has turned the wheel
	cur      int    // the index now (a drag region outlives the frame it was built in)
	box      *DivEl // last frame's wheel, whose height gives the row height
}

// Moving reports whether a flick is still rolling the wheel.
func (st *TumblerState) Moving() bool { return core.TweenRunning(&st.spin) }

// TumblerOpts configures a Tumbler.
type TumblerOpts struct {
	Count   int
	Visible int  // slots shown; 0 is 3
	Wrap    bool // the wheel wraps around (never with fewer items than slots)
	// W and H size the wheel; 0 sizes it to its items (the row height then
	// comes from the last frame's layout).
	W, H    float32
	Spacing float32
}

// TumblerSlot is one visible position on a wheel: the item it shows (-1 for
// an empty slot past the end of a wheel that does not wrap) and its
// displacement from the centre.
type TumblerSlot struct {
	Index        int
	Displacement float32
}

// TumblerWindow is the slots visible with cur centred.
func TumblerWindow(cur, count, visible int, wrap bool) []TumblerSlot {
	half := visible / 2
	slots := make([]TumblerSlot, 0, visible)
	for d := -half; d <= half; d++ {
		i := cur + d
		switch {
		case wrap:
			i = ((i % count) + count) % count
		case i < 0 || i >= count:
			i = -1
		}
		slots = append(slots, TumblerSlot{Index: i, Displacement: float32(d)})
	}
	return slots
}

// Tumbler draws the wheel with current centred; set runs with the item the
// user turns it to (store it there). item draws one item; nil draws its
// index as text.
func Tumbler(st *TumblerState, current int, set func(int), o TumblerOpts, item func(i int, displacement float32) Element) *DivEl {
	visible := o.Visible
	if visible < 1 {
		visible = 3
	}
	// The wheel never paints outside its own box: a long label is cut, not
	// spilled over a neighbouring wheel.
	// The cells stretch across the wheel (each centres its own item), so a
	// click beside a short label still picks it.
	box := Div().JustifyCenter().Clip()
	if o.W > 0 {
		box.W(o.W)
	}
	if o.H > 0 {
		box.H(o.H)
	}
	if o.Spacing > 0 {
		box.Gap(o.Spacing)
	}
	prev := st.box
	st.box = box
	count := o.Count
	if count == 0 {
		return box
	}
	wrap := o.Wrap && count >= visible
	settle := func(next int) int {
		if wrap {
			return ((next % count) + count) % count
		}
		return min(max(next, 0), count-1)
	}
	cur := current
	if cur >= count || cur < 0 {
		// The model shrank under the index (February losing its 29th).
		cur = settle(cur)
		if set != nil {
			set(cur)
		}
	}
	// While a flick is rolling, the animation owns the wheel: follow the
	// continuous position. Once it stops, the index is the truth again.
	if core.TweenRunning(&st.spin) {
		if at := settle(int(math.Round(float64(st.spin.Get())))); at != cur {
			cur = at
			if set != nil {
				set(at)
			}
		}
	} else if st.spin.Get() != float32(cur) {
		st.spin.Set(float32(cur))
	}
	st.cur = cur
	box.Access(Access{Role: RoleSpinButton, Value: itoa(cur), Checked: -1, Expanded: -1})
	tm := CurrentTheme()
	for _, slot := range TumblerWindow(cur, count, visible, wrap) {
		if slot.Index < 0 {
			box.Children(Div().Grow()) // keeps the current item centred
			continue
		}
		var el Element
		if item != nil {
			el = item(slot.Index, slot.Displacement)
		} else {
			el = Text(itoa(slot.Index), tm.BandText)
		}
		cell := Div().ItemsCenter().JustifyCenter().Grow().Children(el)
		if set != nil {
			pick := slot.Index
			cell.OnClick(func() { set(pick) })
		}
		box.Children(cell)
	}
	if set == nil {
		return box
	}
	step := func(from, by int) {
		next := settle(from + by)
		st.cur = next
		set(next)
		st.spin.Set(float32(next))
	}
	box.Focusable().OnKey(func(e app.KeyEvent) {
		switch {
		case !e.Down:
		case e.Key == app.KeyUp:
			step(st.cur, -1)
		case e.Key == app.KeyDown:
			step(st.cur, 1)
		}
	})
	// A drag turns it a row at a time from the item centred at the press,
	// so half a row does nothing and letting go leaves it snapped. Dragging
	// DOWN brings earlier items down into view.
	rowH := o.H / float32(visible)
	if rowH <= 0 && prev != nil {
		rowH = prev.Rect().H / float32(visible)
	}
	if rowH > 0 && count > 1 {
		box.OnDrag(func(_, dy float32, phase DragPhase) {
			if phase == DragStart {
				core.StopTween(&st.spin) // a press catches a spinning wheel
				st.dragFrom, st.dragged = st.cur, false
				return
			}
			// Until it has moved a row, a press is a click: a release then must
			// not put back the item the click just picked.
			if rows := -int(math.Round(float64(dy / rowH))); rows != 0 || st.dragged {
				st.dragged = true
				step(st.dragFrom, rows)
			}
		})
		box.OnFling(func(_, vy float32) {
			rows := -flingDistance(vy) / rowH
			target := settle(st.cur + int(math.Round(float64(rows))))
			st.spin.Set(float32(st.cur))
			core.Tween(&st.spin, float32(target), flingDuration(vy), core.EaseOutQuad)
		})
	}
	return box
}

// flingDeceleration is how fast a flicked wheel slows, in pixels per second
// squared — the rate a flicked list uses.
const flingDeceleration = 1500

// flingDistance is how far a release at this speed travels before it
// stops: v² / 2a, signed like the velocity.
func flingDistance(v float32) float32 {
	d := v * v / (2 * flingDeceleration)
	if v < 0 {
		return -d
	}
	return d
}

// flingDuration is how long that takes: v / a.
func flingDuration(v float32) time.Duration {
	if v < 0 {
		v = -v
	}
	return time.Duration(float64(v) / flingDeceleration * float64(time.Second))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
