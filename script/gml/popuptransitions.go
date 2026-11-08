package gml

import (
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// A Popup's `enter` and `exit` transitions, over ui.PopupState (the Go API
// for the same): their opacity and scale animations run as the popup opens
// and closes, and a closing popup stays drawn until its exit is done.

// readPopupTransition reads an enter/exit Transition's opacity and scale
// animations, through Parallel/Sequential groups.
func readPopupTransition(n *node, name string, env *Env) []ui.PopupAnim {
	var tr *node
	for _, a := range n.attrs {
		if a.name == name && a.elem != nil {
			tr = a.elem
		}
	}
	if tr == nil {
		return nil
	}
	var out []ui.PopupAnim
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
			base := ui.PopupAnim{Dur: 250 * time.Millisecond, Easing: core.EaseLinear}
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
						props = append(props, splitProps(str(v))...)
					}
				case "from", "to":
					if f, err := attrFloat(a, env); err == nil {
						if a.name == "from" {
							base.From, base.FromSet = f, true
						} else {
							base.To = f
						}
					}
				case "duration":
					if f, err := attrFloat(a, env); err == nil {
						base.Dur = time.Duration(f) * time.Millisecond
					}
				case "easing.type":
					if v, err := a.expr.eval(env); err == nil {
						base.Easing = easingByName(str(v))
					}
				}
			}
			for _, p := range props {
				a := base
				switch p {
				case "opacity":
					a.Prop = ui.PopupOpacity
				case "scale":
					a.Prop = ui.PopupScale
				default:
					continue
				}
				out = append(out, a)
			}
		}
	}
	walk(tr.children)
	return out
}

func splitProps(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// popupTransition keeps the popup's ui.PopupState in step with its open
// state, and reports whether it is to be drawn (open, or closing).
func (doc *docState) popupTransition(n *node, env *Env, shown bool) (*ui.PopupState, bool) {
	if doc.popupStates == nil {
		doc.popupStates = map[string]*ui.PopupState{}
	}
	key := doc.key(n)
	st := doc.popupStates[key]
	if st == nil {
		st = &ui.PopupState{}
		st.Snap(shown) // open from the start: no enter animation
		doc.popupStates[key] = st
	}
	st.Enter = readPopupTransition(n, "enter", env)
	st.Exit = readPopupTransition(n, "exit", env)
	if shown != st.IsOpen() {
		st.SetOpen(shown)
	}
	return st, st.Visible()
}
