package ui

// Drag and drop (GPUI drag_drop): a Draggable carries a payload and, while
// dragged, a translucent ghost of its child follows the cursor; dropping
// over a DropZone whose accept predicate passes calls its OnDrop with the
// payload.

// DraggableEl is a drag source.
type DraggableEl struct {
	child   Element
	payload any
	rect    Rect
}

// Draggable makes child a drag source carrying payload.
func Draggable(child Element, payload any) *DraggableEl {
	return &DraggableEl{child: child, payload: payload}
}

func (d *DraggableEl) measure(ctx *Context) (float32, float32) { return d.child.measure(ctx) }
func (d *DraggableEl) layout(ctx *Context, r Rect) {
	d.rect = r
	d.child.layout(ctx, r)
}
func (d *DraggableEl) childElems() []Element { return []Element{d.child} }
func (d *DraggableEl) paintSig() uint64      { return 0xd7 }

func (d *DraggableEl) paint(ctx *Context) {
	if ctx.culled(d) {
		return
	}
	d.child.paint(ctx)
}
func (d *DraggableEl) hit(x, y float32) Element {
	if d.rect.contains(x, y) {
		return d
	}
	return nil
}
func (d *DraggableEl) onClick() func() { return nil }
func (d *DraggableEl) bounds() Rect    { return d.rect }

// DropZoneEl is a drop target.
type DropZoneEl struct {
	child  Element
	onDrop func(payload any)
	accept func(payload any) bool
	rect   Rect
}

// DropZone makes child a drop target; onDrop fires with the payload when a
// drag is released over it.
func DropZone(child Element, onDrop func(payload any)) *DropZoneEl {
	return &DropZoneEl{child: child, onDrop: onDrop}
}

// Accept restricts which payloads this zone will take (default: any).
func (z *DropZoneEl) Accept(fn func(payload any) bool) *DropZoneEl { z.accept = fn; return z }

func (z *DropZoneEl) measure(ctx *Context) (float32, float32) { return z.child.measure(ctx) }
func (z *DropZoneEl) layout(ctx *Context, r Rect) {
	z.rect = r
	z.child.layout(ctx, r)
	ctx.dropZones = append(ctx.dropZones, z)
}
func (z *DropZoneEl) childElems() []Element { return []Element{z.child} }
func (z *DropZoneEl) paintSig() uint64      { return 0xd8 }

func (z *DropZoneEl) paint(ctx *Context) {
	if ctx.culled(z) {
		return
	}
	z.child.paint(ctx)
}
func (z *DropZoneEl) hit(x, y float32) Element { return z.child.hit(x, y) }
func (z *DropZoneEl) onClick() func()          { return nil }
func (z *DropZoneEl) bounds() Rect             { return z.rect }
func (z *DropZoneEl) fixedWH() (float32, float32) {
	if fs, ok := z.child.(interface{ fixedWH() (float32, float32) }); ok {
		return fs.fixedWH()
	}
	return 0, 0
}
