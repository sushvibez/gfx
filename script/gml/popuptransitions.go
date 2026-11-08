package gml

import (
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// A Popup's `enter` and `exit` transitions: on opening, the enter
// transition's animations of opacity and scale run (from their `from`, to
// their `to`); on closing with an exit transition the popup stays drawn
// until its animations are done. Other properties are not animated.

// popupAnim is one animated property of an enter/exit transition.
type popupAnim struct {
	prop     string
	from, to float32
	hasFrom  bool
	dur      time.Duration
	easing   core.Easing
}

// popupPhase is a popup's open/closing state across builds, and its
// animated values.
type popupPhase struct {
	shown   bool
	closing []popupAnim // the exit animations still running
	vals    map[string]*core.Property[float32]
}

// readPopupTransition reads an enter/exit Transition's opacity and scale
// animations, through Parallel/Sequential groups.
func readPopupTransition(n *node, name string, env *Env) []popupAnim {
	var tr *node
	for _, a := range n.attrs {
		if a.name == name && a.elem != nil {
			tr = a.elem
		}
	}
	if tr == nil {
		return nil
	}
	var out []popupAnim
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, c := range ns {
			switch c.typeName {
			case "ParallelAnimation", "SequentialAnimation":
				walk(c.children)
				continue
			case "NumberAnimation", "PropertyAnimation", "OpacityAnimator", "ScaleAnimator":
			default:
				continue
			}
			base := popupAnim{dur: 250 * time.Millisecond, easing: core.EaseLinear}
			var props []string
			switch c.typeName {
			case "OpacityAnimator":
				props = []string{"opacity"}
			case "ScaleAnimator":
				props = []string{"scale"}
			}
			for _, a := range c.attrs {
				switch a.name {
				case "property", "properties":
					if v, err := a.expr.eval(env); err == nil {
						for _, p := range splitProps(str(v)) {
							props = append(props, p)
						}
					}
				case "from", "to":
					if f, err := attrFloat(a, env); err == nil {
						if a.name == "from" {
							base.from, base.hasFrom = f, true
						} else {
							base.to = f
						}
					}
				case "duration":
					if f, err := attrFloat(a, env); err == nil {
						base.dur = time.Duration(f) * time.Millisecond
					}
				case "easing.type":
					if v, err := a.expr.eval(env); err == nil {
						base.easing = easingByName(str(v))
					}
				}
			}
			for _, p := range props {
				if p == "opacity" || p == "scale" {
					a := base
					a.prop = p
					out = append(out, a)
				}
			}
		}
	}
	walk(tr.children)
	return out
}

func splitProps(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			p := s[start:i]
			for len(p) > 0 && p[0] == ' ' {
				p = p[1:]
			}
			for len(p) > 0 && p[len(p)-1] == ' ' {
				p = p[:len(p)-1]
			}
			if p != "" {
				out = append(out, p)
			}
			start = i + 1
		}
	}
	return out
}

func (ph *popupPhase) val(prop string) *core.Property[float32] {
	if ph.vals == nil {
		ph.vals = map[string]*core.Property[float32]{}
	}
	p := ph.vals[prop]
	if p == nil {
		p = &core.Property[float32]{}
		p.Set(1)
		ph.vals[prop] = p
	}
	return p
}

// popupTransition runs the enter or exit animations on an open edge, and
// reports whether the popup is to be drawn this build (open, or closing).
func (doc *docState) popupTransition(n *node, env *Env, shown bool) (*popupPhase, bool) {
	if doc.popupPhases == nil {
		doc.popupPhases = map[string]*popupPhase{}
	}
	key := doc.key(n)
	ph := doc.popupPhases[key]
	if ph == nil {
		ph = &popupPhase{shown: shown}
		doc.popupPhases[key] = ph
		if shown {
			return ph, true // open from the start: no enter animation
		}
	}
	start := func(anims []popupAnim) {
		for _, a := range anims {
			p := ph.val(a.prop)
			if a.hasFrom {
				p.Set(a.from)
			}
			core.Tween(p, a.to, a.dur, a.easing)
		}
	}
	switch {
	case shown && !ph.shown: // opening
		ph.closing = nil
		start(readPopupTransition(n, "enter", env))
	case !shown && ph.shown: // closing
		ph.closing = readPopupTransition(n, "exit", env)
		start(ph.closing)
	}
	ph.shown = shown
	if shown {
		return ph, true
	}
	// Closing: drawn until every exit animation has reached its end.
	for _, a := range ph.closing {
		if ph.val(a.prop).Get() != a.to {
			return ph, true
		}
	}
	ph.closing = nil
	return ph, false
}

// applyPopupAnims puts the animated opacity and scale on the panel.
func (ph *popupPhase) apply(panel ui.Element) {
	d, ok := panel.(*ui.DivEl)
	if !ok || ph == nil {
		return
	}
	if p := ph.vals["opacity"]; p != nil {
		d.Opacity(p.Get())
	}
	if p := ph.vals["scale"]; p != nil {
		if s := p.Get(); s != 1 {
			d.Transform(ui.Transform{Kind: ui.XformScale, SX: s, SY: s, Rel: true, OriginFracX: 0.5, OriginFracY: 0.5})
		}
	}
}
