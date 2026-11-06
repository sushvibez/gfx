package ui

// QML-style anchors. An anchor container (a DivEl in AnchorMode) positions
// each child absolutely from its Anchors spec, resolved against the
// container's inner rect ("parent") and sibling rects (by id). This is the
// Qt Quick positioning model that coexists with the flexbox Row/Column.

// Edge identifies an anchor line on a target rectangle.
type Edge int

const (
	EdgeLeft Edge = iota
	EdgeRight
	EdgeTop
	EdgeBottom
	EdgeHCenter
	EdgeVCenter
)

// AnchorLink binds one of an item's edges to a target's edge.
type AnchorLink struct {
	Target string // "parent" or a sibling id
	Edge   Edge
}

// Anchors is the resolved anchor spec for one item. Zero value = unanchored
// (positioned at the parent origin with its natural size).
type Anchors struct {
	Fill     string // target id to fill ("" = none)
	CenterIn string // target id to center within ("" = none)

	Left, Right, Top, Bottom *AnchorLink
	HCenter, VCenter         *AnchorLink

	Margins                                          float32
	LeftMargin, RightMargin, TopMargin, BottomMargin float32
	// HCenterOffset/VCenterOffset shift a CENTRED item off the centre it
	// is anchored to (QML's anchors.horizontalCenterOffset /
	// verticalCenterOffset) — how a page nudges something that is
	// otherwise centred.
	HCenterOffset, VCenterOffset float32

	X, Y       float32 // manual offsets, used only on an unanchored axis
	HasX, HasY bool
}

// targets lists the target ids this spec references (excluding "parent").
func (a *Anchors) targets() []string {
	var out []string
	add := func(id string) {
		if id != "" && id != "parent" {
			out = append(out, id)
		}
	}
	add(a.Fill)
	add(a.CenterIn)
	for _, l := range []*AnchorLink{a.Left, a.Right, a.Top, a.Bottom, a.HCenter, a.VCenter} {
		if l != nil {
			add(l.Target)
		}
	}
	return out
}

// AnchorEl wraps a child with an id and an anchor spec; an anchor container
// resolves its rect and lays the child into it.
type AnchorEl struct {
	id    string
	a     Anchors
	child Element
	rect  Rect
}

// Anchored wraps child with an id (may be "") and anchors for placement in an
// anchor container.
func Anchored(child Element, id string, a Anchors) *AnchorEl {
	return &AnchorEl{id: id, a: a, child: child}
}

func (e *AnchorEl) measure(ctx *Context) (float32, float32) { return e.child.measure(ctx) }

// natural forwards the wrapped item's implicit size (ui.Implicit): an
// anchored item is still the item, and what it reports must not depend on
// the box the anchors gave it — a page that sizes that box FROM the
// implicit size (the corpus' blueprint viewer: `Item { width:
// img.implicitWidth }` around an anchor-filled Image) would otherwise read
// back the zero it was waiting on.
func (e *AnchorEl) natural() (float32, float32) {
	if n, ok := e.child.(interface{ natural() (float32, float32) }); ok {
		return n.natural()
	}
	return 0, 0
}

func (e *AnchorEl) layout(ctx *Context, r Rect) {
	e.rect = r
	e.child.layout(ctx, r)
}

func (e *AnchorEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	e.child.paint(ctx)
}

func (e *AnchorEl) hit(x, y float32) Element { return e.child.hit(x, y) }

func (e *AnchorEl) bounds() Rect { return e.rect }

// childElems exposes the wrapped child, so a walk of a built tree (ui.Children,
// used by tooling and tests) does not stop at every anchored subtree.
func (e *AnchorEl) childElems() []Element { return []Element{e.child} }

// paintSig: an anchor wrapper paints nothing itself (see damage.go).
func (e *AnchorEl) paintSig() uint64 { return 0xa1 }
func (e *AnchorEl) onClick() func()  { return nil }

// edgeVal returns the coordinate of edge on rect r.
func edgeVal(r Rect, e Edge) float32 {
	switch e {
	case EdgeLeft:
		return r.X
	case EdgeRight:
		return r.X + r.W
	case EdgeTop:
		return r.Y
	case EdgeBottom:
		return r.Y + r.H
	case EdgeHCenter:
		return r.X + r.W/2
	case EdgeVCenter:
		return r.Y + r.H/2
	}
	return r.X
}

// ready reports whether every referenced sibling target is resolved.
func (a *Anchors) ready(resolved map[string]Rect) bool {
	for _, id := range a.targets() {
		if _, ok := resolved[id]; !ok {
			return false
		}
	}
	return true
}

// resolve computes the item's rect from its anchors. Missing targets fall
// back to the parent rect, so a cycle or forward reference degrades to a
// parent-relative placement instead of failing.
func (a *Anchors) resolve(natW, natH float32, resolved map[string]Rect, parent Rect) Rect {
	target := func(id string) Rect {
		if r, ok := resolved[id]; ok {
			return r
		}
		return parent
	}
	m := func(per float32) float32 {
		if per != 0 {
			return per
		}
		return a.Margins
	}
	if a.Fill != "" {
		t := target(a.Fill)
		l, r, tp, b := m(a.LeftMargin), m(a.RightMargin), m(a.TopMargin), m(a.BottomMargin)
		return Rect{t.X + l, t.Y + tp, t.W - l - r, t.H - tp - b}
	}
	if a.CenterIn != "" {
		t := target(a.CenterIn)
		return Rect{t.X + (t.W-natW)/2 + a.HCenterOffset,
			t.Y + (t.H-natH)/2 + a.VCenterOffset, natW, natH}
	}

	x, y, w, h := parent.X, parent.Y, natW, natH

	switch {
	case a.Left != nil && a.Right != nil:
		lx := edgeVal(target(a.Left.Target), a.Left.Edge) + m(a.LeftMargin)
		rx := edgeVal(target(a.Right.Target), a.Right.Edge) - m(a.RightMargin)
		x, w = lx, rx-lx
	case a.Left != nil:
		x = edgeVal(target(a.Left.Target), a.Left.Edge) + m(a.LeftMargin)
	case a.Right != nil:
		x = edgeVal(target(a.Right.Target), a.Right.Edge) - m(a.RightMargin) - w
	case a.HCenter != nil:
		x = edgeVal(target(a.HCenter.Target), a.HCenter.Edge) - w/2 + a.HCenterOffset
	case a.HasX:
		x = parent.X + a.X
	}

	switch {
	case a.Top != nil && a.Bottom != nil:
		ty := edgeVal(target(a.Top.Target), a.Top.Edge) + m(a.TopMargin)
		by := edgeVal(target(a.Bottom.Target), a.Bottom.Edge) - m(a.BottomMargin)
		y, h = ty, by-ty
	case a.Top != nil:
		y = edgeVal(target(a.Top.Target), a.Top.Edge) + m(a.TopMargin)
	case a.Bottom != nil:
		y = edgeVal(target(a.Bottom.Target), a.Bottom.Edge) - m(a.BottomMargin) - h
	case a.VCenter != nil:
		y = edgeVal(target(a.VCenter.Target), a.VCenter.Edge) - h/2 + a.VCenterOffset
	case a.HasY:
		y = parent.Y + a.Y
	}

	return Rect{x, y, w, h}
}
