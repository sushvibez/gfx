package ui

// SlideEl is a page-transition container: it shows `to` sliding into place
// while `from` slides out, both clipped to the element's box. progress runs
// 1→0 (1 = transition start, 0 = settled); dir +1 slides the new page in
// from the right (moving to a higher index), -1 from the left. Drive
// progress from a tweened property and rebuild per frame (SwipeView's page
// change animation).
type SlideEl struct {
	from, to Element
	progress float32
	dir      float32
	rect     Rect
}

// Slide builds the transition; from may be nil (no outgoing page).
func Slide(from, to Element, progress float32, dir int) *SlideEl {
	return &SlideEl{from: from, to: to, progress: progress, dir: float32(dir)}
}

func (e *SlideEl) measure(ctx *Context) (float32, float32) {
	w, h := e.to.measure(ctx)
	if e.from != nil {
		fw, fh := e.from.measure(ctx)
		w, h = max(w, fw), max(h, fh)
	}
	return w, h
}

func (e *SlideEl) layout(ctx *Context, r Rect) {
	e.rect = r
	off := e.dir * r.W * e.progress
	e.to.layout(ctx, Rect{r.X + off, r.Y, r.W, r.H})
	if e.from != nil {
		e.from.layout(ctx, Rect{r.X + off - e.dir*r.W, r.Y, r.W, r.H})
	}
}

func (e *SlideEl) childElems() []Element {
	if e.from == nil {
		return []Element{e.to}
	}
	return []Element{e.from, e.to}
}

// paintSig: the transition's phase (the pages are its children).
func (e *SlideEl) paintSig() uint64 {
	h := newSig(0x51d)
	h.f32(e.progress)
	h.f32(e.dir)
	return uint64(h)
}

func (e *SlideEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
	if e.from != nil {
		e.from.paint(ctx)
	}
	e.to.paint(ctx)
	ctx.R.PopClip()
}

// hit targets the incoming page only (the outgoing one is on its way out).
func (e *SlideEl) hit(x, y float32) Element {
	if !e.rect.contains(x, y) {
		return nil
	}
	return e.to.hit(x, y)
}

func (e *SlideEl) onClick() func() { return nil }
func (e *SlideEl) bounds() Rect    { return e.rect }
