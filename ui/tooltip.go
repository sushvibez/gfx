package ui

import (
	"time"

	"github.com/sushvibez/gfx/core"
)

// Tool tips (QML's ToolTip): a small dark label that floats centred above
// an element once the pointer has rested on it for Delay, and goes after
// Timeout or when the pointer leaves.

// ToolTip is the tip's panel in Qt's Basic look: white 12 px text on a dark
// rounded card.
func ToolTip(text string) *DivEl {
	return Div().Row().ItemsCenter().Padding(6, 12, 6, 12).Rounded(6).
		Bg(RGB(0x333333)).
		Children(Text(text, RGB(0xffffff)).SizePx(12))
}

// ToolTipOpts configures an attached tool tip.
type ToolTipOpts struct {
	// Delay is how long the pointer rests before the tip shows (0: at
	// once); Timeout how long it stays (0: until the pointer leaves).
	Delay, Timeout time.Duration
}

// ToolTipState is an attached tip's retained state: whether the element is
// hovered, and since when. Keep one per element.
type ToolTipState struct {
	hover core.Property[bool]
	since time.Duration
}

// Attach floats a tip reading text above box while the pointer rests on it,
// and returns box.
func (st *ToolTipState) Attach(box *DivEl, text string, o ToolTipOpts) *DivEl {
	box.OnHoverChanged(func(v bool) {
		if st.hover.Get() != v {
			st.hover.Set(v)
			st.since = core.AnimClock()
		}
	})
	if !st.hover.Get() || text == "" {
		core.SetExternalAnimation(st, false)
		return box
	}
	rested := core.AnimClock() - st.since
	// Waiting on the clock, not on a property: keep frames coming until
	// the tip shows (and, with a timeout, until it goes).
	waiting := rested < o.Delay || (o.Timeout > 0 && rested <= o.Delay+o.Timeout)
	core.SetExternalAnimation(st, waiting)
	if rested < o.Delay || (o.Timeout > 0 && rested > o.Delay+o.Timeout) {
		return box
	}
	return box.Children(Overlay(ToolTip(text), 0, 0).AnchorAbove(box, 4).SnapToWindow(2))
}
