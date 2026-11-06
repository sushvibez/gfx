package ui

// HiddenEl lays its child out but neither paints it nor lets it take input:
// QML's `visible: false`, where the item keeps its geometry — a sibling
// still anchors to it, an effect still samples it — and simply is not drawn.
type HiddenEl struct {
	child Element
	rect  Rect
}

// Hidden wraps an element that must occupy its place without being seen.
func Hidden(child Element) *HiddenEl { return &HiddenEl{child: child} }

func (h *HiddenEl) measure(ctx *Context) (float32, float32) {
	if h.child == nil {
		return 0, 0
	}
	return h.child.measure(ctx)
}

func (h *HiddenEl) layout(ctx *Context, r Rect) {
	h.rect = r
	if h.child != nil {
		h.child.layout(ctx, r)
	}
}

func (h *HiddenEl) paint(*Context)               {}
func (h *HiddenEl) paintSig() uint64             { return 0x41 }
func (h *HiddenEl) hit(float32, float32) Element { return nil }
func (h *HiddenEl) onClick() func()              { return nil }
func (h *HiddenEl) bounds() Rect                 { return h.rect }

// fixedWH forwards the child's own stated size, so a layout treats a hidden
// item exactly as it would the visible one.
func (h *HiddenEl) fixedWH() (float32, float32) {
	if f, ok := h.child.(interface{ fixedWH() (float32, float32) }); ok {
		return f.fixedWH()
	}
	return 0, 0
}
