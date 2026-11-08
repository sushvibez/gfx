package ui

import "github.com/sushvibez/gfx/core"

// ButtonGroup makes checkable buttons exclusive (QML's ButtonGroup, and
// the automatic exclusivity of radio buttons that share a parent): checking
// one through the group unchecks the others. Keep one across frames; Add
// is idempotent, so a view can add its buttons on every build.
type ButtonGroup struct {
	// Exclusive: at most one member checked (NewButtonGroup sets it).
	Exclusive bool
	members   []*core.Property[bool]
}

// NewButtonGroup returns an empty exclusive group.
func NewButtonGroup() *ButtonGroup { return &ButtonGroup{Exclusive: true} }

// Add makes p a member (a second Add of the same property is a no-op).
func (g *ButtonGroup) Add(p *core.Property[bool]) {
	for _, m := range g.members {
		if m == p {
			return
		}
	}
	g.members = append(g.members, p)
}

// Check checks p and, for an exclusive group, unchecks every other member.
func (g *ButtonGroup) Check(p *core.Property[bool]) {
	p.Set(true)
	if !g.Exclusive {
		return
	}
	for _, m := range g.members {
		if m != p && m.Get() {
			m.Set(false)
		}
	}
}

// Checked is the checked member (the first, for a non-exclusive group), or
// nil.
func (g *ButtonGroup) Checked() *core.Property[bool] {
	for _, m := range g.members {
		if m.Get() {
			return m
		}
	}
	return nil
}

// RadioRing is a radio button's indicator, bound to checked: clicking it
// checks it — through group when there is one, so the others in the group
// uncheck — and a checked radio stays checked when clicked again (QML's
// RadioButton). onToggle runs after a click that checked it.
func RadioRing(checked *core.Property[bool], group *ButtonGroup, onToggle ...func(bool)) *DivEl {
	tm := CurrentTheme()
	ring := Div().Size(20, 20).Rounded(10).Border(2, tm.Accent).JustifyCenter().ItemsCenter()
	if checked.Get() {
		ring.Children(Div().Size(10, 10).Rounded(5).Bg(tm.Accent))
	}
	if group != nil {
		group.Add(checked)
	}
	return ring.OnClick(func() {
		if checked.Get() {
			return
		}
		if group != nil {
			group.Check(checked)
		} else {
			checked.Set(true)
		}
		for _, f := range onToggle {
			f(true)
		}
	})
}

// Radio is a labelled radio button (see RadioRing); the label clicks too.
func Radio(checked *core.Property[bool], label string, group *ButtonGroup, onToggle ...func(bool)) Element {
	tm := CurrentTheme()
	ring := RadioRing(checked, group, onToggle...)
	return Div().Row().Gap(10).ItemsCenter().OnClick(ring.onClick()).
		Children(ring, Text(label, tm.Label))
}

// Check states of a TriCheckbox (Qt's Qt::CheckState).
const (
	Unchecked        = 0
	PartiallyChecked = 1
	Checked          = 2
)

// TriCheckbox is a checkbox with a third, partly checked state (QML's
// CheckBox with tristate: true), bound to state (Unchecked,
// PartiallyChecked, Checked). A click moves it on through the three in
// that order, as Qt's does; onChange runs after with the new state.
func TriCheckbox(state *core.Property[int], label string, onChange ...func(int)) Element {
	tm := CurrentTheme()
	box := Div().Size(20, 20).Rounded(5)
	switch state.Get() {
	case Checked:
		box.Bg(tm.Accent).Children(Div().Size(8, 8).Bg(tm.OnAccent).Rounded(2)).
			JustifyCenter().ItemsCenter()
	case PartiallyChecked:
		box.Bg(tm.Accent).Children(Div().Size(10, 2).Bg(tm.OnAccent)).
			JustifyCenter().ItemsCenter()
	default:
		box.Border(2, tm.Outline)
	}
	return Div().Row().Gap(10).ItemsCenter().
		OnClick(func() {
			v := (state.Get() + 1) % 3
			state.Set(v)
			for _, f := range onChange {
				f(v)
			}
		}).
		Children(box, Text(label, tm.Label))
}
