package gml

import (
	"fmt"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// Exclusive checkable buttons, over ui.ButtonGroup: a RadioButton or CheckBox
// belongs to a group when it names one (`ButtonGroup.group: g`), when a
// ButtonGroup lists it (`buttons: [a, b]`), or — autoExclusive, the default
// for a RadioButton — with the other autoExclusive buttons of its parent.
// Checking one through its group unchecks the others when the group is
// exclusive (ButtonGroup.exclusive, default true). A ButtonGroup's id
// carries checkedButton, and its onClicked(button) runs when a member is
// clicked.

// buttonGroup is one group: the ui group, its ButtonGroup element (nil for
// an automatic one), and its members' id objects.
type buttonGroup struct {
	g    *ui.ButtonGroup
	node *node
	env  *Env
	objs map[*core.Property[bool]]any
}

// scanButtonGroups records, once at load, which button ids a ButtonGroup
// lists in `buttons`.
func (doc *docState) scanButtonGroups(n *node) {
	if n == nil {
		return
	}
	if n.typeName == "ButtonGroup" {
		gid := nodeID(n)
		if gid == "" {
			gid = fmt.Sprintf("__bg%d", n.line)
		}
		for _, a := range n.attrs {
			if a.name != "buttons" || a.expr == nil {
				continue
			}
			if l, ok := a.expr.(*listExpr); ok {
				for _, e := range l.items {
					if id, ok := e.(*identExpr); ok {
						if doc.groupOfButton == nil {
							doc.groupOfButton = map[string]string{}
						}
						doc.groupOfButton[id.name] = gid
					}
				}
			}
		}
	}
	for _, c := range n.children {
		doc.scanButtonGroups(c)
	}
	for _, a := range n.attrs {
		if a.elem != nil {
			doc.scanButtonGroups(a.elem)
		}
	}
}

// buttonGroupFor is the group the button n belongs to, or nil; autoDefault
// is whether it is autoExclusive when the markup does not say (true for a
// RadioButton).
func (doc *docState) buttonGroupFor(n *node, env *Env, autoDefault bool) *buttonGroup {
	gid := ""
	auto := autoDefault
	for _, a := range n.attrs {
		switch a.name {
		case "ButtonGroup.group":
			if id, ok := a.expr.(*identExpr); ok {
				gid = id.name
			}
		case "autoExclusive":
			if b, err := attrBool(a, env); err == nil {
				auto = b
			}
		}
	}
	if gid == "" {
		gid = doc.groupOfButton[nodeID(n)]
	}
	if gid != "" {
		bg := doc.groupNamed("named:"+gid, nil)
		if bg.node == nil {
			if obj := doc.elemObjs.get(doc.intern(gid)); obj != nil {
				bg.node, bg.env = obj.node, obj.nodeEnv
			}
		}
		bg.g.Exclusive = true
		if bg.node != nil && bg.env != nil {
			for _, a := range bg.node.attrs {
				if a.name == "exclusive" {
					if b, err := attrBool(a, bg.env); err == nil {
						bg.g.Exclusive = b
					}
				}
			}
		}
		return bg
	}
	if !auto || n.owner == nil {
		return nil
	}
	// The automatic group of the buttons that share this parent.
	return doc.groupNamed("auto:"+doc.keyStr(doc.keyID(n.owner)), nil)
}

func (doc *docState) groupNamed(key string, _ *node) *buttonGroup {
	if doc.buttonGroups == nil {
		doc.buttonGroups = map[string]*buttonGroup{}
	}
	bg := doc.buttonGroups[key]
	if bg == nil {
		bg = &buttonGroup{g: ui.NewButtonGroup(), objs: map[*core.Property[bool]]any{}}
		doc.buttonGroups[key] = bg
	}
	return bg
}

// join adds the button n (its checked property p) to the group, and
// publishes the group's checkedButton on the ButtonGroup's id.
func (doc *docState) join(bg *buttonGroup, n *node, p *core.Property[bool]) {
	bg.g.Add(p)
	if id := nodeID(n); id != "" {
		if obj := doc.elemObjs.get(doc.intern(id)); obj != nil {
			bg.objs[p] = obj
		}
	}
	if bg.node == nil {
		return
	}
	if gid := nodeID(bg.node); gid != "" {
		if obj := doc.elemObjs.get(doc.intern(gid)); obj != nil {
			obj.setProp("checkedButton", computedRef{fn: func() any {
				if c := bg.g.Checked(); c != nil {
					return bg.objs[c]
				}
				return nil
			}})
		}
	}
}

// clicked runs the ButtonGroup's onClicked with the member that was
// clicked bound as `button`.
func (doc *docState) clicked(bg *buttonGroup, p *core.Property[bool]) {
	if bg == nil || bg.node == nil || bg.env == nil {
		return
	}
	for _, a := range bg.node.attrs {
		if a.name == "onClicked" && a.stmt != nil {
			runEvent(doc.handlerRunner(bg.env), bg.env, a.stmt, "ButtonGroup.onClicked", "button", bg.objs[p])
		}
	}
}

// actionGroup is the ActionGroup named gid, over the same machinery as a
// ButtonGroup: exclusive unless its `exclusive` says otherwise, its
// checkedAction published on its id.
func (doc *docState) actionGroup(gid string) *buttonGroup {
	bg := doc.groupNamed("action:"+gid, nil)
	if bg.node == nil {
		if obj := doc.elemObjs.get(doc.intern(gid)); obj != nil {
			bg.node, bg.env = obj.node, obj.nodeEnv
		}
	}
	bg.g.Exclusive = true
	if bg.node != nil && bg.env != nil {
		for _, a := range bg.node.attrs {
			if a.name == "exclusive" {
				if b, err := attrBool(a, bg.env); err == nil {
					bg.g.Exclusive = b
				}
			}
		}
		if obj := doc.elemObjs.get(doc.intern(gid)); obj != nil {
			obj.setProp("checkedAction", computedRef{fn: func() any {
				if c := bg.g.Checked(); c != nil {
					return bg.objs[c]
				}
				return nil
			}})
		}
	}
	return bg
}

// groupTriggered runs an ActionGroup's onTriggered with the member that
// was triggered bound as `action`.
func (doc *docState) groupTriggered(bg *buttonGroup, p *core.Property[bool]) {
	if bg == nil || bg.node == nil || bg.env == nil {
		return
	}
	for _, a := range bg.node.attrs {
		if a.name == "onTriggered" && a.stmt != nil {
			runEvent(doc.handlerRunner(bg.env), bg.env, a.stmt, "ActionGroup.onTriggered", "action", bg.objs[p])
		}
	}
}
