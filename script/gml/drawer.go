package gml

// Qt Quick Controls' Drawer, over ui.Drawer (the Go API for the same): a
// panel that slides in from the window edge its `edge` names, `width` (or
// `height`, for a top or bottom drawer) wide, over a dim scrim. open()/
// close() and a `visible:`/`opened:` binding slide it; a scrim click,
// Escape or a drag toward the edge closes it and runs onClosed. Its
// `position` (0 closed .. 1 open) is readable on its id.

import "github.com/sushvibez/gfx/ui"

func buildDrawer(n *node, env *Env, doc *docState, st *popupState) (ui.Element, error) {
	ds, o, visible := doc.drawerOf(n, env, st, 0)
	if !visible {
		// Closed: nothing to build, but ui.Drawer keeps the edge a drag can
		// pull it open from.
		return ui.Drawer(ds, nil, o), nil
	}
	cp := stripPopupAttrs(n)
	kept := cp.attrs[:0:0]
	for _, a := range cp.attrs {
		switch a.name {
		case "edge", "width", "height", "position", "dragMargin", "interactive":
			// the drawer's own — the panel sizes the content
		default:
			kept = append(kept, a)
		}
	}
	cp.attrs = kept
	content, err := buildBox(cp, env, doc)
	if err != nil {
		return nil, err
	}
	return ui.Drawer(ds, content, o), nil
}

// drawerOf keeps the Drawer's ui.DrawerState in step with its open state
// and reads its options; visible reports whether it is open or still
// sliding. size is the panel's extent when the markup states none (a
// style's implicit width).
func (doc *docState) drawerOf(n *node, env *Env, st *popupState, size float32) (*ui.DrawerState, ui.DrawerOpts, bool) {
	if doc.drawers == nil {
		doc.drawers = map[string]*ui.DrawerState{}
	}
	key := doc.key(n)
	ds := doc.drawers[key]
	if ds == nil {
		ds = &ui.DrawerState{}
		if st.shown {
			ds.SetPosition(1) // open from the start: no slide
		}
		doc.drawers[key] = ds
	}
	if st.shown != ds.IsOpen() {
		ds.SetOpen(st.shown)
	}
	doc.exposeIDProp(n, "position", float64(ds.Position()))
	o := ui.DrawerOpts{Edge: ui.EdgeLeft, Dark: doc.groundDark, NoDim: !st.modal && hasAttr(n, "modal")}
	if v, ok := attrNum(n, "edge", env); ok {
		switch int(v) {
		case 1:
			o.Edge = ui.EdgeTop
		case 4:
			o.Edge = ui.EdgeRight
		case 8:
			o.Edge = ui.EdgeBottom
		}
	}
	across := "width"
	if o.Edge == ui.EdgeTop || o.Edge == ui.EdgeBottom {
		across = "height"
	}
	o.Size = attrFloatOr(n, across, env, size)
	o.OnClosed = st.dismiss
	// A drag in from the edge opens it, within dragMargin of the edge (Qt's
	// default is the platform's drag distance), unless it is not interactive.
	interactive := true
	for _, a := range n.attrs {
		if a.name == "interactive" {
			if b, err := attrBool(a, env); err == nil {
				interactive = b
			}
		}
	}
	if interactive {
		o.DragMargin = attrFloatOr(n, "dragMargin", env, 10)
		open, fire := st.open, st.fire
		o.OnOpened = func() {
			_ = open.set(true)
			fire("onOpened")
		}
	}
	return ds, o, ds.Position() > 0 || ds.IsOpen()
}

// attrNum evaluates a numeric attribute.
func attrNum(n *node, name string, env *Env) (float64, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			if f, err := attrFloat(a, env); err == nil {
				return float64(f), true
			}
		}
	}
	return 0, false
}
