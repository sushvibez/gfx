package ui

// ReplaceChild swaps one element for another somewhere under root, in
// place: the container that holds old points at repl afterwards. It is
// how a retained subtree gets a rebuilt piece patched in without its
// ancestors being rebuilt (script/gml's subtree cache does this when a
// parent's own reads are fresh and only a child's are stale). Reports
// whether old was found in a container that knows how to swap.
func ReplaceChild(root, old, repl Element) bool {
	if root == nil || old == nil || repl == nil || root == old {
		return false
	}
	for _, c := range Children(root) {
		if c == old {
			if r, ok := root.(interface{ replaceChild(old, repl Element) bool }); ok {
				return r.replaceChild(old, repl)
			}
			return false
		}
	}
	for _, c := range Children(root) {
		if ReplaceChild(c, old, repl) {
			return true
		}
	}
	return false
}

func (d *DivEl) replaceChild(old, repl Element) bool {
	for i, c := range d.children {
		if c == old {
			d.children[i] = repl
			return true
		}
	}
	return false
}

func (e *AnchorEl) replaceChild(old, repl Element) bool {
	if e.child != old {
		return false
	}
	e.child = repl
	return true
}

func (e *LayerEl) replaceChild(old, repl Element) bool {
	if e.child != old {
		return false
	}
	e.child = repl
	return true
}

func (o *OverlayEl) replaceChild(old, repl Element) bool {
	if o.child != old {
		return false
	}
	o.child = repl
	return true
}

func (m *ModalEl) replaceChild(old, repl Element) bool {
	if m.child != old {
		return false
	}
	m.child = repl
	return true
}

func (e *ScrollEl) replaceChild(old, repl Element) bool {
	d, ok := repl.(*DivEl)
	if !ok || Element(e.content) != old {
		return false
	}
	e.content = d
	return true
}

func (g *GridEl) replaceChild(old, repl Element) bool {
	for i := range g.items {
		if g.items[i].el == old {
			g.items[i].el = repl
			return true
		}
	}
	return false
}
