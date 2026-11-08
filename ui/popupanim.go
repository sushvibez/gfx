package ui

import (
	"time"

	"github.com/sushvibez/gfx/core"
)

// PopupState is the retained open/closing state of a popup with enter and
// exit animations (QML's Popup.enter / exit): Open runs the Enter
// animations, Close runs the Exit ones, and Visible stays true until they
// are done, so the popup is still drawn while it fades out. Keep one
// across frames, build the popup while Visible, and Apply the animated
// values to its panel:
//
//	if st.Visible() {
//	    root.Children(ui.Overlay(st.Apply(panel), x, y))
//	}
//
// Opacity and scale are animated; the scale is about the panel's centre.
type PopupState struct {
	Enter, Exit []PopupAnim

	open, closing  bool
	opacity, scale core.Property[float32]
	inited         bool
}

// PopupAnim animates one property of a popup.
type PopupAnim struct {
	Prop     PopupProp
	From, To float32
	// FromSet: start from From; otherwise from the current value.
	FromSet bool
	Dur     time.Duration
	Easing  core.Easing // nil: linear
}

// PopupProp names what a PopupAnim animates.
type PopupProp int

const (
	PopupOpacity PopupProp = iota
	PopupScale
)

// FadeIn and FadeOut are the usual enter and exit: opacity over d.
func FadeIn(d time.Duration) []PopupAnim {
	return []PopupAnim{{Prop: PopupOpacity, From: 0, To: 1, FromSet: true, Dur: d}}
}
func FadeOut(d time.Duration) []PopupAnim {
	return []PopupAnim{{Prop: PopupOpacity, From: 1, To: 0, FromSet: true, Dur: d}}
}

func (s *PopupState) init() {
	if !s.inited {
		s.inited = true
		s.opacity.Set(1)
		s.scale.Set(1)
	}
}

func (s *PopupState) prop(p PopupProp) *core.Property[float32] {
	s.init()
	if p == PopupScale {
		return &s.scale
	}
	return &s.opacity
}

func (s *PopupState) run(anims []PopupAnim) {
	for _, a := range anims {
		p := s.prop(a.Prop)
		if a.FromSet {
			p.Set(a.From)
		}
		e := a.Easing
		if e == nil {
			e = core.EaseLinear
		}
		core.Tween(p, a.To, a.Dur, e)
	}
}

// Open opens the popup, running the Enter animations (from full opacity
// and scale, wherever an Exit left them).
func (s *PopupState) Open() {
	if s.open {
		return
	}
	s.init()
	core.StopTween(&s.opacity)
	core.StopTween(&s.scale)
	s.opacity.Set(1)
	s.scale.Set(1)
	s.open, s.closing = true, false
	s.run(s.Enter)
}

// Close closes it, running the Exit animations; it stays Visible until they
// are done.
func (s *PopupState) Close() {
	if !s.open {
		return
	}
	s.open = false
	s.closing = len(s.Exit) > 0
	s.run(s.Exit)
}

// SetOpen opens or closes it (for an open flag kept elsewhere).
func (s *PopupState) SetOpen(open bool) {
	if open {
		s.Open()
	} else {
		s.Close()
	}
}

// Snap sets the state without animating: a popup that starts open.
func (s *PopupState) Snap(open bool) {
	s.init()
	s.open, s.closing = open, false
	s.opacity.Set(1)
	s.scale.Set(1)
}

// IsOpen reports whether it is open (not counting a running exit).
func (s *PopupState) IsOpen() bool { return s.open }

// Visible reports whether to draw it: open, or still running its Exit.
func (s *PopupState) Visible() bool {
	if s.open {
		return true
	}
	if !s.closing {
		return false
	}
	for _, a := range s.Exit {
		if s.prop(a.Prop).Get() != a.To {
			return true
		}
	}
	s.closing = false
	return false
}

// Apply puts the animated opacity and scale on the popup's panel.
func (s *PopupState) Apply(d *DivEl) *DivEl {
	s.init()
	if o := s.opacity.Get(); o != 1 {
		d.Opacity(o)
	}
	if sc := s.scale.Get(); sc != 1 {
		d.Transform(Transform{Kind: XformScale, SX: sc, SY: sc, Rel: true, OriginFracX: 0.5, OriginFracY: 0.5})
	}
	return d
}

// Opacity and Scale are the animated values now.
func (s *PopupState) Opacity() float32 { s.init(); return s.opacity.Get() }
func (s *PopupState) Scale() float32   { s.init(); return s.scale.Get() }
