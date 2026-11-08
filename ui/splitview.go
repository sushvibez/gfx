package ui

import "github.com/sushvibez/gfx/core"

// SplitView (QML's SplitView): panes along one axis with a handle between
// each pair. Each pane states its size along the axis (Preferred, or Fill
// to take what is left, and a Minimum/Maximum); dragging a handle moves
// the boundary, adding to the pane before it and taking from the pane
// after, never past either pane's minimum or maximum.

// SplitPane is one pane of a SplitView and its size along the split axis.
type SplitPane struct {
	El        Element
	Preferred float32 // its size; 0 with Fill false shares what is left
	Minimum   float32
	Maximum   float32 // 0: none
	Fill      bool    // takes what the others leave (up to Maximum)
}

// SplitState is a split view's retained state: what the user has dragged
// each handle by, and each handle's hover and press. Keep one per view.
type SplitState struct {
	off            []*core.Property[float32]
	hover, pressed []*core.Property[bool]
}

func (s *SplitState) grow(n int) {
	for len(s.off) < n {
		s.off = append(s.off, &core.Property[float32]{})
		s.hover = append(s.hover, &core.Property[bool]{})
		s.pressed = append(s.pressed, &core.Property[bool]{})
	}
}

// Offset is how far handle i (between panes i and i+1) has been dragged.
func (s *SplitState) Offset(i int) float32 {
	s.grow(i + 1)
	return s.off[i].Get()
}

// Hovered and Pressed are handle i's pointer state (QML's
// SplitHandle.hovered / .pressed).
func (s *SplitState) Hovered(i int) bool { s.grow(i + 1); return s.hover[i].Get() }
func (s *SplitState) Pressed(i int) bool { s.grow(i + 1); return s.pressed[i].Get() }

// SplitOpts configures a SplitView.
type SplitOpts struct {
	Vertical bool // panes stacked top to bottom (default: side by side)
	// Handle draws handle i; nil is a 4 px bar. A handle sizes its own
	// thickness and is stretched along the other axis.
	Handle func(i int, hovered, pressed bool) Element
}

// SplitView lays the panes out with a draggable handle between each pair.
// The panes' elements are sized in place, so build them fresh each frame.
func SplitView(st *SplitState, panes []SplitPane, o SplitOpts) *DivEl {
	box := Div()
	if !o.Vertical {
		box.Row()
	}
	st.grow(len(panes))
	for i := range panes {
		if i > 0 {
			box.Children(splitHandle(st, i-1, &panes[i-1], &panes[i], o))
		}
		var dragged float32
		if i < len(panes)-1 {
			dragged += st.off[i].Get()
		}
		if i > 0 {
			dragged -= st.off[i-1].Get()
		}
		sizePane(panes[i], o.Vertical, dragged)
		box.Children(panes[i].El)
	}
	return box
}

// splitHandle is handle i, which drags the boundary between before and
// after. The offset is absolute from the press, so the drag follows the
// pointer, and clamped by both panes' limits.
func splitHandle(st *SplitState, i int, before, after *SplitPane, o SplitOpts) Element {
	hover, pressed, off := st.hover[i], st.pressed[i], st.off[i]
	var h Element
	if o.Handle != nil {
		h = o.Handle(i, hover.Get(), pressed.Get())
	}
	d, ok := h.(*DivEl)
	if h == nil {
		d, ok = Div().Bg(RGBA{R: 0.35, G: 0.36, B: 0.4, A: 1}), true
		if o.Vertical {
			d.H(4)
		} else {
			d.W(4)
		}
	} else if ok {
		// The handle states its thickness; it stretches along the other
		// axis.
		if o.Vertical {
			d.W(0)
		} else {
			d.H(0)
		}
	}
	if !ok {
		return h // a handle that is not a box cannot be dragged
	}
	d.OnHoverChanged(func(v bool) {
		if hover.Get() != v {
			hover.Set(v)
		}
	})
	start := float32(0)
	d.OnDrag(func(dx, dy float32, phase DragPhase) {
		moved := dx
		if o.Vertical {
			moved = dy
		}
		switch phase {
		case DragStart:
			start = off.Get()
			pressed.Set(true)
			return
		case DragEnd:
			pressed.Set(false)
		}
		want := start + moved
		// A pane with no preferred size has no floor to compute against;
		// then only the other pane's limits hold the drag.
		if before.Preferred > 0 && before.Minimum > 0 {
			want = max(want, before.Minimum-before.Preferred)
		}
		if after.Preferred > 0 && after.Minimum > 0 {
			want = min(want, after.Preferred-after.Minimum)
		}
		if before.Preferred > 0 && before.Maximum > 0 {
			want = min(want, before.Maximum-before.Preferred)
		}
		if after.Preferred > 0 && after.Maximum > 0 {
			want = max(want, after.Preferred-after.Maximum)
		}
		off.Set(want)
	})
	return d
}

// sizePane sizes a pane along the split axis, with what the user dragged
// onto it.
func sizePane(p SplitPane, vertical bool, dragged float32) {
	d, ok := p.El.(*DivEl)
	if !ok {
		return
	}
	switch {
	case p.Fill:
		d.Grow()
		if p.Maximum > 0 {
			if vertical {
				d.MaxH(p.Maximum)
			} else {
				d.MaxW(p.Maximum)
			}
		}
	case p.Preferred > 0:
		size := max(p.Preferred+dragged, 1)
		if p.Maximum > 0 {
			size = min(size, p.Maximum)
		}
		if vertical {
			d.H(size)
		} else {
			d.W(size)
		}
	}
	if p.Minimum > 0 {
		if vertical {
			d.MinH(p.Minimum)
		} else {
			d.MinW(p.Minimum)
		}
	}
	if !p.Fill && p.Preferred == 0 {
		d.Grow() // no size of its own: it shares what is left
	}
}
