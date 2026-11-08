package ui

import (
	"strconv"
	"strings"
)

// Accessibility: what a screen reader needs to know about the tree. Each
// element can carry a role, a name, a description, a value and states
// (checked, expanded, selected, disabled); the controls kit tags its own.
// UI.AccessTree builds the tree a platform bridge (UI Automation on
// Windows, AT-SPI on Linux, ARIA on the web) presents: elements with no
// role are folded into their parent, a node with no name takes its text,
// and AccessActivate does what a click on a node does.

// Role is what kind of thing an element is to assistive technology. The
// names follow ARIA's roles, which UIA and AT-SPI both map from.
type Role int

const (
	RoleNone Role = iota
	RoleWindow
	RoleGroup
	RoleStaticText
	RoleButton
	RoleCheckBox
	RoleSwitch
	RoleRadio
	RoleSlider
	RoleSpinButton
	RoleTextField
	RoleComboBox
	RoleMenuBar
	RoleMenu
	RoleMenuItem
	RoleDialog
	RoleTabList
	RoleTab
	RoleList
	RoleListItem
	RoleTable
	RoleRow
	RoleCell
	RoleColumnHeader
	RoleTree
	RoleTreeItem
	RoleProgressBar
	RoleToolTip
	RoleToolBar
	RoleSeparator
	RoleImage
	RoleLink
)

var roleNames = [...]string{
	"none", "window", "group", "text", "button", "checkbox", "switch", "radio",
	"slider", "spinbutton", "textbox", "combobox", "menubar", "menu", "menuitem",
	"dialog", "tablist", "tab", "list", "listitem", "table", "row", "cell",
	"columnheader", "tree", "treeitem", "progressbar", "tooltip", "toolbar",
	"separator", "img", "link",
}

// String is the role's ARIA name ("button", "checkbox").
func (r Role) String() string {
	if int(r) < len(roleNames) {
		return roleNames[r]
	}
	return "unknown"
}

// RoleByName is the role an ARIA (or QML Accessible) name stands for.
func RoleByName(name string) Role {
	n := strings.ToLower(name)
	for i, s := range roleNames {
		if s == n {
			return Role(i)
		}
	}
	switch n { // QML's Accessible.Role names that differ from ARIA's
	case "pushbutton", "toolbutton", "buttonmenu":
		return RoleButton
	case "radiobutton":
		return RoleRadio
	case "editabletext", "edit":
		return RoleTextField
	case "statictext", "label":
		return RoleStaticText
	case "pagetab":
		return RoleTab
	case "pagetablist":
		return RoleTabList
	case "popupmenu":
		return RoleMenu
	case "progressbar", "indicator":
		return RoleProgressBar
	case "graphic":
		return RoleImage
	case "pane", "grouping", "panel":
		return RoleGroup
	case "spinbox":
		return RoleSpinButton
	}
	return RoleNone
}

// Access is what an element tells assistive technology about itself.
type Access struct {
	Role        Role
	Name        string
	Description string
	Value       string // a slider's or field's current value, as read out
	// Checked is the check state: -1 not checkable, 0 off, 1 on, 2 mixed.
	Checked  int8
	Expanded int8 // -1 not expandable, 0 collapsed, 1 expanded
	Selected bool
	Disabled bool
	Level    int // a tree item's depth (1 at the top), 0 elsewhere
}

// accessible is an element that describes itself.
type accessible interface {
	access() (Access, bool)
}

// Access tags the element for assistive technology (see Access).
func (d *DivEl) Access(a Access) *DivEl {
	d.acc = &a
	return d
}

// Role sets just the role and name (the common case).
func (d *DivEl) Role(r Role, name string) *DivEl {
	return d.Access(Access{Role: r, Name: name, Checked: -1, Expanded: -1})
}

func (d *DivEl) access() (Access, bool) {
	if d.acc == nil {
		return Access{}, false
	}
	return *d.acc, true
}

// AccessNode is one node of the accessibility tree.
type AccessNode struct {
	Access
	Focused   bool
	Focusable bool
	Bounds    Rect
	Children  []*AccessNode
	el        Element
}

// AccessTree is the accessibility tree of the last laid-out frame: a window
// node holding the tagged elements (and the text outside them) in tree
// order, overlays included.
func (u *UI) AccessTree() *AccessNode {
	root := &AccessNode{Access: Access{Role: RoleWindow, Checked: -1, Expanded: -1}}
	if u.root == nil {
		return root
	}
	focused := u.focusedEl()
	var walk func(e Element, into *AccessNode, named bool)
	walk = func(e Element, into *AccessNode, named bool) {
		if e == nil {
			return
		}
		if a, ok := accessOf(e); ok {
			n := &AccessNode{Access: a, Bounds: e.bounds(), el: e, Focused: e == focused}
			if f, ok := e.(interface{ isFocusable() bool }); ok {
				n.Focusable = f.isFocusable()
			}
			into.Children = append(into.Children, n)
			fromContent := nameFromContent(n.Role)
			for _, c := range Children(e) {
				walk(c, n, fromContent)
			}
			if n.Name == "" && fromContent {
				n.Name = textUnder(e)
			}
			return
		}
		if t, ok := e.(*TextEl); ok {
			// Inside a control named by its content the text IS the name,
			// not a second thing to read out.
			if s := t.s; strings.TrimSpace(s) != "" && !named {
				into.Children = append(into.Children, &AccessNode{
					Access: Access{Role: RoleStaticText, Name: s, Checked: -1, Expanded: -1},
					Bounds: t.bounds(), el: e})
			}
			return
		}
		for _, c := range Children(e) {
			walk(c, into, named)
		}
	}
	walk(u.root, root, false)
	return root
}

// accessOf is an element's own description, if it has one.
func accessOf(e Element) (Access, bool) {
	if a, ok := e.(accessible); ok {
		return a.access()
	}
	return Access{}, false
}

// textUnder joins the text inside an element (a button's label).
func textUnder(e Element) string {
	var parts []string
	var walk func(Element)
	walk = func(e Element) {
		if t, ok := e.(*TextEl); ok {
			if s := strings.TrimSpace(t.s); s != "" {
				parts = append(parts, s)
			}
			return
		}
		for _, c := range Children(e) {
			walk(c)
		}
	}
	walk(e)
	return strings.Join(parts, " ")
}

// AccessActivate does what a click on the node does (a button's press, a
// check box's toggle); false when it has no action.
func (u *UI) AccessActivate(n *AccessNode) bool {
	if n == nil || n.el == nil {
		return false
	}
	if c := n.el.onClick(); c != nil {
		c()
		return true
	}
	return false
}

// Find is the first node under n (n included) with the role and name
// (an empty name matches any).
func (n *AccessNode) Find(r Role, name string) *AccessNode {
	if n == nil {
		return nil
	}
	if n.Role == r && (name == "" || n.Name == name) {
		return n
	}
	for _, c := range n.Children {
		if f := c.Find(r, name); f != nil {
			return f
		}
	}
	return nil
}

// String renders the subtree one node a line, indented — for tests and a
// debug overlay.
func (n *AccessNode) String() string {
	var b strings.Builder
	var walk func(n *AccessNode, depth int)
	walk = func(n *AccessNode, depth int) {
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString(n.Role.String())
		if n.Name != "" {
			b.WriteString(" \"" + n.Name + "\"")
		}
		if n.Value != "" {
			b.WriteString(" value=" + n.Value)
		}
		switch n.Checked {
		case 0:
			b.WriteString(" unchecked")
		case 1:
			b.WriteString(" checked")
		case 2:
			b.WriteString(" mixed")
		}
		switch n.Expanded {
		case 0:
			b.WriteString(" collapsed")
		case 1:
			b.WriteString(" expanded")
		}
		if n.Selected {
			b.WriteString(" selected")
		}
		if n.Disabled {
			b.WriteString(" disabled")
		}
		if n.Focused {
			b.WriteString(" focused")
		}
		b.WriteByte('\n')
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// checkState is a bool as an Access.Checked value.
func checkState(on bool) int8 {
	if on {
		return 1
	}
	return 0
}

// The widgets that are not boxes describe themselves.

func formatValue(v float32) string { return strconv.FormatFloat(float64(v), 'g', 4, 32) }

func (e *SliderEl) access() (Access, bool) {
	v := ""
	if e.value != nil {
		v = formatValue(e.value.Get())
	}
	return Access{Role: RoleSlider, Value: v, Checked: -1, Expanded: -1, Disabled: e.disabled}, true
}

func (e *RangeSliderEl) access() (Access, bool) {
	return Access{Role: RoleSlider, Value: formatValue(e.first.Get()) + " – " + formatValue(e.second.Get()),
		Checked: -1, Expanded: -1}, true
}

func (e *DialEl) access() (Access, bool) {
	return Access{Role: RoleSlider, Value: formatValue(e.value.Get()), Checked: -1, Expanded: -1}, true
}

func (e *InputEl) access() (Access, bool) {
	a := Access{Role: RoleTextField, Name: e.placeholder, Checked: -1, Expanded: -1, Disabled: e.readOnly}
	if !e.hasEcho && !e.noEcho { // a password's value is not read out
		a.Value = e.state.Text.Get()
	}
	return a, true
}

// nameFromContent reports a role whose name, unless given, is its text
// (ARIA's "name from content": a button's label, a tab's title).
func nameFromContent(r Role) bool {
	switch r {
	case RoleButton, RoleCheckBox, RoleSwitch, RoleRadio, RoleMenuItem, RoleTab,
		RoleRow, RoleCell, RoleColumnHeader, RoleTreeItem, RoleListItem,
		RoleToolTip, RoleLink, RoleStaticText:
		return true
	}
	return false
}

// AccessOf is what an element says about itself for assistive technology.
func AccessOf(e Element) (Access, bool) { return accessOf(e) }
