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
	tm := CurrentTheme()
	return Div().Row().ItemsCenter().Padding(6, 12, 6, 12).Rounded(6).
		Bg(tm.ToolTip).
		Children(Text(text, tm.ToolTipText).SizePx(12)).Role(RoleToolTip, text)
}

// ToolTipOpts configures an attached tool tip.
type ToolTipOpts struct {
	// Delay is how long the pointer rests before the tip shows (0: at
	// once); Timeout how long it stays (0: until the pointer leaves).
	Delay, Timeout time.Duration
}

// ToolTipState is an attached tip's retained state: whether its element is
// hovered, and since when the tip has been wanted. Keep one per element.
//
// Attach does it all for a tip that shows on hover. A host with its own
// rule for when the tip is wanted (QML's `ToolTip.visible: expr`) or its
// own panel uses the parts: Track the element's hover, decide, then ask
// Due whether the delay has passed.
type ToolTipState struct {
	hover  core.Property[bool]
	wanted bool
	since  time.Duration // the animation clock when it became wanted
}

// Track follows box's hover state into the tip's.
func (st *ToolTipState) Track(box *DivEl) {
	box.OnHoverChanged(func(v bool) {
		if st.hover.Get() != v {
			st.hover.Set(v)
		}
	})
}

// Hovered reports whether the tracked element is under the pointer.
func (st *ToolTipState) Hovered() bool { return st.hover.Get() }

// Due reports whether a tip that is wanted (or not) shows now: wanted for
// at least o.Delay and, with a timeout, not yet o.Timeout after that.
// waiting reports that the answer will change with the clock alone, so a
// host that caches its tree has to keep rebuilding (Due itself asks the
// frame loop for the frames).
func (st *ToolTipState) Due(wanted bool, o ToolTipOpts) (show, waiting bool) {
	if !wanted {
		st.wanted = false
		core.SetExternalAnimation(st, false)
		return false, false
	}
	now := core.AnimClock()
	if !st.wanted {
		st.wanted, st.since = true, now
	}
	rested := now - st.since
	waiting = rested < o.Delay || (o.Timeout > 0 && rested <= o.Delay+o.Timeout)
	core.SetExternalAnimation(st, waiting)
	show = rested >= o.Delay && (o.Timeout == 0 || rested <= o.Delay+o.Timeout)
	return show, waiting
}

// Attach floats a tip reading text above box once the pointer has rested
// on it for o.Delay, and returns box.
func (st *ToolTipState) Attach(box *DivEl, text string, o ToolTipOpts) *DivEl {
	st.Track(box)
	if show, _ := st.Due(st.Hovered() && text != "", o); show {
		AttachTip(box, ToolTip(text))
	}
	return box
}

// AttachTip floats panel centred above box, 4 px clear of it and kept on
// screen — where Qt's ToolTip style places a control's tip.
func AttachTip(box *DivEl, panel Element) *DivEl {
	return box.Children(Overlay(panel, 0, 0).AnchorAbove(box, 4).SnapToWindow(2))
}
