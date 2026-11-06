package pdf

// The interpreter: a page's operators become Renderer2D calls. PDF's
// imaging model maps onto ours closely — paths with a fill rule, strokes
// with a width, an affine transform, alpha — so this is mostly bookkeeping
// plus the coordinate flip (PDF's origin is bottom-left, ours top-left).

import (
	"github.com/sushvibez/gfx/render"
)

// Rect is a device-space rectangle: where a page landed, and the clip
// region the operators narrow.
type Rect struct {
	X, Y, W, H float32
}

// gstate is the PDF graphics state the operators mutate.
type gstate struct {
	ctm        render.Affine2
	fill       render.RGBA
	stroke     render.RGBA
	lineWidth  float32
	fillAlpha  float32
	strokeAlph float32
	clip       Rect // in device pixels; the whole page when unset
	// clipPath is the chain of non-rectangular clips in force, outermost
	// first. A rectangular clip needs none: the box is exact.
	clipPath *clipChain
	// blend is the /BM of the ExtGState: how a drawing operation combines
	// with what is under it.
	blend render.BlendMode
	// smask is the /SMask of the ExtGState: a whole drawing whose
	// luminosity (or alpha) is the coverage for everything drawn under
	// this state. nil for /None, which is the default.
	smask    *softMask
	fillCS   *colorSpace
	strokeCS *colorSpace
	// text state
	font     *fontRes
	fontSize float32
	charSp   float32
	wordSp   float32
	hscale   float32
	leading  float32
	rise     float32
	render   int
}

// Renderer draws pages of a document into a Renderer2D.
type Renderer struct {
	doc      *Document
	r        *render.Renderer2D
	fonts    map[string]*fontRes
	imgs     map[string]*render.Image
	fallback *render.Font // the face for fonts that are not embedded
	// budget caps the work one page may queue, so a pathological document
	// cannot stall a frame.
	budget int
	// maskDepth guards against a soft mask whose own drawing carries a
	// soft mask, which a file is free to nest without end.
	maskDepth int
}

// NewRenderer returns a renderer that draws doc's pages through r.
func NewRenderer(doc *Document, r *render.Renderer2D) *Renderer {
	return &Renderer{doc: doc, r: r, fonts: map[string]*fontRes{}, imgs: map[string]*render.Image{}}
}

// DrawPage draws page i scaled to fit the rect (x, y, w, h) in device
// pixels, letterboxed to keep the page's aspect ratio. It returns the rect
// the page actually covered.
func (rd *Renderer) DrawPage(i int, x, y, w, h float32) Rect {
	p := rd.doc.Page(i)
	if p == nil || rd.r == nil {
		return Rect{}
	}
	pw, ph := p.Size()
	if pw <= 0 || ph <= 0 || w <= 0 || h <= 0 {
		return Rect{}
	}
	scale := minf(w/pw, h/ph)
	dw, dh := pw*scale, ph*scale
	dx, dy := x+(w-dw)/2, y+(h-dh)/2
	rd.drawPageAt(p, dx, dy, scale)
	return Rect{X: dx, Y: dy, W: dw, H: dh}
}

// DrawPageAt draws page i at a given origin and scale (1 = 72dpi points to
// pixels), which is what a scrolling viewer wants.
func (rd *Renderer) DrawPageAt(i int, x, y, scale float32) Rect {
	p := rd.doc.Page(i)
	if p == nil || rd.r == nil || scale <= 0 {
		return Rect{}
	}
	rd.drawPageAt(p, x, y, scale)
	pw, ph := p.Size()
	return Rect{X: x, Y: y, W: pw * scale, H: ph * scale}
}

// drawPageAt is the shared body: build the page-to-device matrix, then run
// the content stream through it.
func (rd *Renderer) drawPageAt(p *Page, x, y, scale float32) {
	pw, ph := p.Size()
	// PDF user space is y-up from the MediaBox's lower-left corner; the
	// renderer is y-down from the page's top-left. Rotation is applied
	// about the page's own centre before the flip.
	base := render.Affine2{A: scale, D: -scale, E: x - p.originX*scale, F: y + ph*scale + p.originY*scale}
	switch p.Rotate {
	case 90:
		base = render.Affine2{A: 0, B: scale, C: scale, D: 0, E: x, F: y}
	case 180:
		base = render.Affine2{A: -scale, D: scale, E: x + pw*scale, F: y}
	case 270:
		base = render.Affine2{A: 0, B: -scale, C: -scale, D: 0, E: x + pw*scale, F: y + ph*scale}
	}
	clip := Rect{X: x, Y: y, W: pw * scale, H: ph * scale}
	rd.budget = 300000 // operators; a book page runs in the low thousands
	st := gstate{
		ctm: base, lineWidth: 1, fillAlpha: 1, strokeAlph: 1, hscale: 1,
		clip: clip, fill: render.RGBA{A: 1}, stroke: render.RGBA{A: 1},
	}
	rd.r.PushClip(clip.X, clip.Y, clip.W, clip.H)
	rd.run(p.Content(), p.Resources, st, 0)
	rd.r.PopClip()
}

// run interprets one content stream (a page, or a form XObject) with the
// given starting state.
func (rd *Renderer) run(content []byte, res Dict, st gstate, depth int) {
	if depth > 12 || len(content) == 0 { // recursive forms must terminate
		return
	}
	ops := scanContent(content, rd.doc)
	var stack []gstate
	path := render.NewPath()
	var startX, startY, curX, curY float32 // in user space
	pendingClip := 0                       // 0 none, 1 nonzero, 2 even-odd
	var text textState

	moveTo := func(ux, uy float32) {
		dx, dy := st.ctm.Apply(ux, uy)
		path.MoveTo(dx, dy)
		startX, startY, curX, curY = ux, uy, ux, uy
	}
	lineTo := func(ux, uy float32) {
		dx, dy := st.ctm.Apply(ux, uy)
		path.LineTo(dx, dy)
		curX, curY = ux, uy
	}
	curveTo := func(c1x, c1y, c2x, c2y, ux, uy float32) {
		a1x, a1y := st.ctm.Apply(c1x, c1y)
		a2x, a2y := st.ctm.Apply(c2x, c2y)
		dx, dy := st.ctm.Apply(ux, uy)
		path.CubicTo(a1x, a1y, a2x, a2y, dx, dy)
		curX, curY = ux, uy
	}
	endPath := func() {
		if pendingClip != 0 {
			// The clip is the path itself. Its box narrows the scissor (and
			// is what the mask covers); a path that is not a rectangle also
			// carries a coverage mask, which the renderer resolves when the
			// clip is pushed for real (see clipped()).
			if b, ok := pathBounds(path); ok {
				st.clip = intersect(st.clip, b)
				rule := render.NonZero
				if pendingClip == 2 {
					rule = render.EvenOdd
				}
				if !isBoxPath(path) {
					st.clipPath = clipPathOf(path, rule, st.clipPath)
				}
			}
			pendingClip = 0
		}
		path = render.NewPath()
	}

	for _, o := range ops {
		if rd.budget <= 0 {
			return
		}
		rd.budget--
		switch o.name {
		// -- graphics state ------------------------------------------------
		case "q":
			stack = append(stack, st)
		case "Q":
			if n := len(stack); n > 0 {
				st = stack[n-1]
				stack = stack[:n-1]
			}
		case "cm":
			v := numArgs(o, 6)
			m := render.Affine2{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
			st.ctm = st.ctm.Mul(m)
		case "w":
			st.lineWidth = numArgs(o, 1)[0]
		case "gs":
			rd.applyExtGState(&st, res, nameArg(o, 0))
		// -- colour --------------------------------------------------------
		case "g", "G":
			v := numArgs(o, 1)[0]
			rd.setColor(&st, o.name == "g", render.RGBA{R: v, G: v, B: v, A: 1})
		case "rg", "RG":
			v := numArgs(o, 3)
			rd.setColor(&st, o.name == "rg", render.RGBA{R: v[0], G: v[1], B: v[2], A: 1})
		case "k", "K":
			v := numArgs(o, 4)
			rd.setColor(&st, o.name == "k", cmyk(v[0], v[1], v[2], v[3]))
		case "cs", "CS":
			cs := rd.colorSpace(res, nameArg(o, 0))
			if o.name == "cs" {
				st.fillCS = cs
				st.fill = cs.initial()
			} else {
				st.strokeCS = cs
				st.stroke = cs.initial()
			}
		case "sc", "scn", "SC", "SCN":
			fill := o.name == "sc" || o.name == "scn"
			cs := st.fillCS
			if !fill {
				cs = st.strokeCS
			}
			if c, ok := cs.color(o.args); ok {
				rd.setColor(&st, fill, c)
			}
		// -- path construction ---------------------------------------------
		case "m":
			v := numArgs(o, 2)
			moveTo(v[0], v[1])
		case "l":
			v := numArgs(o, 2)
			lineTo(v[0], v[1])
		case "c":
			v := numArgs(o, 6)
			curveTo(v[0], v[1], v[2], v[3], v[4], v[5])
		case "v":
			v := numArgs(o, 4)
			curveTo(curX, curY, v[0], v[1], v[2], v[3])
		case "y":
			v := numArgs(o, 4)
			curveTo(v[0], v[1], v[2], v[3], v[2], v[3])
		case "h":
			path.Close()
			curX, curY = startX, startY
		case "re":
			v := numArgs(o, 4)
			x0, y0, w, h := v[0], v[1], v[2], v[3]
			moveTo(x0, y0)
			lineTo(x0+w, y0)
			lineTo(x0+w, y0+h)
			lineTo(x0, y0+h)
			path.Close()
			curX, curY = x0, y0
		// -- path painting -------------------------------------------------
		case "n":
			endPath()
		case "f", "F", "f*":
			rd.fill(path, st, o.name == "f*")
			endPath()
		case "S":
			rd.stroke(path, st)
			endPath()
		case "s":
			path.Close()
			rd.stroke(path, st)
			endPath()
		case "B", "B*":
			rd.fill(path, st, o.name == "B*")
			rd.stroke(path, st)
			endPath()
		case "b", "b*":
			path.Close()
			rd.fill(path, st, o.name == "b*")
			rd.stroke(path, st)
			endPath()
		case "W":
			pendingClip = 1
		case "W*":
			pendingClip = 2
		// -- XObjects and images -------------------------------------------
		case "Do":
			rd.doXObject(res, nameArg(o, 0), st, depth)
		case "INLINE_IMAGE":
			rd.drawImageStream(o.inline, st)
		case "sh":
			// A shading fills the clip with a gradient; the average colour
			// of its function is closer than leaving a hole.
			if c, ok := rd.shadingColor(res, nameArg(o, 0)); ok {
				rd.fillRect(st.clip, c, st)
			}
		// -- text ----------------------------------------------------------
		case "BT":
			text.begin()
		case "ET":
			text.end()
		case "Tf":
			st.font = rd.font(res, nameArg(o, 0))
			st.fontSize = numArgs(o, 1)[0]
		case "Td":
			v := numArgs(o, 2)
			text.nextLine(v[0], v[1])
		case "TD":
			v := numArgs(o, 2)
			st.leading = -v[1]
			text.nextLine(v[0], v[1])
		case "Tm":
			v := numArgs(o, 6)
			text.setMatrix(render.Affine2{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]})
		case "T*":
			text.nextLine(0, -st.leading)
		case "TL":
			st.leading = numArgs(o, 1)[0]
		case "Tc":
			st.charSp = numArgs(o, 1)[0]
		case "Tw":
			st.wordSp = numArgs(o, 1)[0]
		case "Tz":
			st.hscale = numArgs(o, 1)[0] / 100
		case "Ts":
			st.rise = numArgs(o, 1)[0]
		case "Tr":
			st.render = int(numArgs(o, 1)[0])
		case "Tj":
			if s, ok := lastString(o.args); ok {
				rd.showText(s, &st, &text)
			}
		case "'":
			text.nextLine(0, -st.leading)
			if s, ok := lastString(o.args); ok {
				rd.showText(s, &st, &text)
			}
		case "\"":
			v := numArgs(o, 3)
			st.wordSp, st.charSp = v[0], v[1]
			text.nextLine(0, -st.leading)
			if s, ok := lastString(o.args); ok {
				rd.showText(s, &st, &text)
			}
		case "TJ":
			if len(o.args) == 0 {
				break
			}
			arr, _ := o.args[len(o.args)-1].(Array)
			for _, el := range arr {
				switch v := el.(type) {
				case String:
					rd.showText(v, &st, &text)
				default:
					if n, ok := numOf(v); ok {
						// A number moves the pen by -n/1000 of an em,
						// scaled by the font size and horizontal scale.
						text.advance(-float32(n)/1000*st.fontSize*st.hscale, 0)
					}
				}
			}
		}
	}
}

// setColor assigns the fill or stroke colour, carrying the alpha from the
// graphics state.
func (rd *Renderer) setColor(st *gstate, fill bool, c render.RGBA) {
	if fill {
		c.A = st.fillAlpha
		st.fill = c
		return
	}
	c.A = st.strokeAlph
	st.stroke = c
}

// fill paints the current path.
func (rd *Renderer) fill(p *render.Path, st gstate, evenOdd bool) {
	if st.fill.A <= 0 {
		return
	}
	rule := render.NonZero
	if evenOdd {
		rule = render.EvenOdd
	}
	rd.clippedBy(st, func() { rd.r.FillPathRule(p, st.fill, rule) })
}

// stroke paints the current path's outline. The line width is in user
// space, so it scales with the transform.
func (rd *Renderer) stroke(p *render.Path, st gstate) {
	if st.stroke.A <= 0 {
		return
	}
	sx, sy, _ := decompose(st.ctm)
	w := st.lineWidth * (sx + sy) / 2
	if w < 0.75 { // PDF's zero width means "the thinnest line the device draws"
		w = 0.75
	}
	rd.clippedBy(st, func() { rd.r.StrokePath(p, w, st.stroke) })
}

// fillRect paints an axis-aligned rect in device space.
func (rd *Renderer) fillRect(r Rect, c render.RGBA, st gstate) {
	if r.W <= 0 || r.H <= 0 || c.A <= 0 {
		return
	}
	c.A *= st.fillAlpha
	rd.clippedBy(st, func() { rd.r.FillRect(r.X, r.Y, r.W, r.H, c) })
}

// clipped runs draw under the graphics state's clip: the box as a scissor,
// plus every non-rectangular clip path in force as a coverage mask.
func (rd *Renderer) clipped(clip Rect, draw func()) {
	if clip.W <= 0 || clip.H <= 0 {
		return
	}
	rd.r.PushClip(clip.X, clip.Y, clip.W, clip.H)
	draw()
	rd.r.PopClip()
}

// clippedBy is clipped with the path chain of a graphics state.
func (rd *Renderer) clippedBy(st gstate, draw func()) {
	if st.clip.W <= 0 || st.clip.H <= 0 {
		return
	}
	if st.smask != nil && rd.maskDepth < 4 {
		// The mask is drawn afresh for each operation it applies to: it is
		// part of the graphics state, and the state can move between one
		// operation and the next.
		sm, inner := st.smask, draw
		draw = func() {
			rd.maskDepth++
			mst := gstate{
				ctm: sm.ctm, lineWidth: 1, fillAlpha: 1, strokeAlph: 1,
				hscale: 1, clip: st.clip,
				fill: render.RGBA{A: 1}, stroke: render.RGBA{A: 1},
			}
			rd.r.BeginSoftMask()
			rd.drawForm(sm.form, nil, mst, 0, false)
			rd.r.EndSoftMask(sm.kind, sm.backdrop)
			inner()
			rd.r.PopClip()
			rd.maskDepth--
		}
	}
	if st.blend != render.BlendNormal {
		// A blend mode is a property of the compositing step, so the
		// operation is drawn into a group of one and that group is
		// composited with the mode. Its alpha already sits in the colours.
		inner := draw
		draw = func() {
			rd.r.BeginGroup()
			inner()
			rd.r.EndGroup(st.blend, 1)
		}
	}
	n := 0
	for c := st.clipPath; c != nil; c = c.next {
		rd.r.PushClipPath(c.path, c.rule)
		n++
	}
	rd.r.PushClip(st.clip.X, st.clip.Y, st.clip.W, st.clip.H)
	draw()
	rd.r.PopClip()
	for i := 0; i < n; i++ {
		rd.r.PopClip()
	}
}

// clipChain is a stack of path clips, innermost last. The states share
// their tails: a `q`/`Q` pair copies the pointer, not the paths.
type clipChain struct {
	path *render.Path
	rule render.FillRule
	next *clipChain
}

// clipPathOf appends a path clip to a chain.
func clipPathOf(p *render.Path, rule render.FillRule, parent *clipChain) *clipChain {
	// The path is kept as it stands: the interpreter builds a fresh one for
	// every subpath, so nothing else writes to this one afterwards.
	c := &clipChain{path: p, rule: rule}
	if parent == nil {
		return c
	}
	// Chain outermost-first so the renderer pushes them in order.
	head := *parent
	tail := &head
	for tail.next != nil {
		cp := *tail.next
		tail.next = &cp
		tail = tail.next
	}
	tail.next = c
	return &head
}

// isBoxPath reports a path that is exactly one axis-aligned rectangle —
// the clip almost every document actually uses, for which the scissor is
// already exact and a mask would be waste.
func isBoxPath(p *render.Path) bool {
	subs := p.Polyline()
	if len(subs) != 1 {
		return false
	}
	pts := subs[0]
	// A rectangle from `re` is four or five points (closed), and every
	// edge is axis-aligned.
	if len(pts) < 4 || len(pts) > 5 {
		return false
	}
	for i := 0; i+1 < len(pts); i++ {
		dx := absf(pts[i+1].X - pts[i].X)
		dy := absf(pts[i+1].Y - pts[i].Y)
		if dx > 0.01 && dy > 0.01 {
			return false
		}
	}
	return true
}

// applyExtGState reads the pieces of an /ExtGState we model: the constant
// alphas and the line width.
func (rd *Renderer) applyExtGState(st *gstate, res Dict, name Name) {
	ext, _ := rd.doc.Resolve(dictGet(rd.doc, res, "ExtGState", name)).(Dict)
	if ext == nil {
		return
	}
	if v, ok := numOf(rd.doc.Resolve(ext["ca"])); ok {
		st.fillAlpha = float32(v)
		st.fill.A = st.fillAlpha
	}
	if v, ok := numOf(rd.doc.Resolve(ext["CA"])); ok {
		st.strokeAlph = float32(v)
		st.stroke.A = st.strokeAlph
	}
	if v, ok := numOf(rd.doc.Resolve(ext["LW"])); ok {
		st.lineWidth = float32(v)
	}
	if bm, ok := ext["BM"]; ok {
		st.blend = rd.blendModeOf(bm)
	}
	if sm, ok := ext["SMask"]; ok {
		st.smask = rd.softMaskOf(sm, st.ctm)
	}
	if f, ok := rd.doc.Resolve(ext["Font"]).(Array); ok && len(f) == 2 {
		if fd, ok := rd.doc.Resolve(f[0]).(Dict); ok {
			st.font = rd.fontFromDict(fd, "gs")
		}
		if v, ok := numOf(rd.doc.Resolve(f[1])); ok {
			st.fontSize = float32(v)
		}
	}
}

// softMask is a resolved /SMask: the form XObject that draws the mask, how
// its coverage is taken, the backdrop a luminosity mask is measured
// against, and the CTM the mask is drawn under.
//
// The CTM is the one in force when the `gs` operator ran, not the one the
// masked drawing uses — the spec is explicit about that, and it is what
// makes a mask stay put while the drawing it fades moves.
type softMask struct {
	form     *Stream
	kind     render.SoftMaskKind
	backdrop render.RGBA
	ctm      render.Affine2
}

// softMaskOf resolves an /SMask entry. /None (and anything unreadable)
// clears the mask.
func (rd *Renderer) softMaskOf(obj Object, ctm render.Affine2) *softMask {
	d, ok := rd.doc.Resolve(obj).(Dict)
	if !ok {
		return nil // /None
	}
	form, ok := rd.doc.Resolve(d["G"]).(*Stream)
	if !ok {
		return nil
	}
	m := &softMask{form: form, ctm: ctm, backdrop: render.RGBA{A: 1}}
	if s, _ := rd.doc.Resolve(d["S"]).(Name); s == "Alpha" {
		m.kind = render.SoftMaskAlpha
	}
	// /BC is the backdrop, in the mask group's OWN colour space, and only
	// a luminosity mask has one.
	if bc, ok := rd.doc.Resolve(d["BC"]).(Array); ok && m.kind == render.SoftMaskLuminosity {
		grp, _ := rd.doc.Resolve(form.Dict["Group"]).(Dict)
		cs := rd.colorSpaceObj(grp["CS"], nil, 0)
		comps := make([]Object, 0, len(bc))
		for _, c := range bc {
			comps = append(comps, rd.doc.Resolve(c))
		}
		if c, ok := cs.color(comps); ok {
			m.backdrop = c
		}
	}
	return m
}

// blendModeOf maps a /BM entry onto a renderer blend mode. It may be a
// name or an ARRAY of names, in which case the first one the reader knows
// wins — that is the spec's own compatibility mechanism.
//
// Only the separable modes a fixed-function blend equation can express are
// supported (see render/group.go); the rest — the ones that need to read
// the backdrop in a shader, plus the non-separable Hue/Saturation/Colour/
// Luminosity — fall back to Normal rather than guess.
func (rd *Renderer) blendModeOf(obj Object) render.BlendMode {
	switch v := rd.doc.Resolve(obj).(type) {
	case Name:
		switch v {
		case "Multiply":
			return render.BlendMultiply
		case "Screen":
			return render.BlendScreen
		case "Darken":
			return render.BlendDarken
		case "Lighten":
			return render.BlendLighten
		}
	case Array:
		for _, e := range v {
			if m := rd.blendModeOf(e); m != render.BlendNormal {
				return m
			}
		}
	}
	return render.BlendNormal
}

// doXObject draws a named XObject: an image, or a form (a nested content
// stream with its own resources and matrix).
func (rd *Renderer) doXObject(res Dict, name Name, st gstate, depth int) {
	obj := rd.doc.Resolve(dictGet(rd.doc, res, "XObject", name))
	strm, ok := obj.(*Stream)
	if !ok {
		return
	}
	switch sub, _ := rd.doc.Resolve(strm.Dict["Subtype"]).(Name); sub {
	case "Image":
		rd.drawImageStream(strm, st)
	case "Form":
		rd.drawForm(strm, res, st, depth, true)
	}
}

// drawForm draws a form XObject: a nested content stream with its own
// resources, matrix and bounding box. group says whether a /Group /S
// /Transparency on it should be honoured — a soft mask's form is already
// being drawn into a group of the caller's, so it must not open another.
func (rd *Renderer) drawForm(strm *Stream, res Dict, st gstate, depth int, group bool) {
	inner := st
	if m, ok := rd.doc.Resolve(strm.Dict["Matrix"]).(Array); ok && len(m) == 6 {
		v := make([]float32, 6)
		for i := range v {
			v[i] = floatOr(rd.doc, m[i], 0)
		}
		inner.ctm = inner.ctm.Mul(render.Affine2{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]})
	}
	if bbox, ok := rd.doc.Resolve(strm.Dict["BBox"]).(Array); ok && len(bbox) == 4 {
		// A form clips to its BBox, in its own space.
		x0, y0 := floatOr(rd.doc, bbox[0], 0), floatOr(rd.doc, bbox[1], 0)
		x1, y1 := floatOr(rd.doc, bbox[2], 0), floatOr(rd.doc, bbox[3], 0)
		if b, ok := quadBounds(inner.ctm, x0, y0, x1, y1); ok {
			inner.clip = intersect(inner.clip, b)
		}
	}
	formRes, _ := rd.doc.Resolve(strm.Dict["Resources"]).(Dict)
	if formRes == nil {
		formRes = res // a form may rely on the page's resources
	}
	data, err := strm.Decode()
	if err != nil {
		return
	}
	// A transparency group is composited as a whole: the alpha and blend
	// mode in force apply to the group's result, and inside it they start
	// over. That is the difference a group makes — the objects in it do
	// not show through each other.
	grp, _ := rd.doc.Resolve(strm.Dict["Group"]).(Dict)
	if s, _ := rd.doc.Resolve(grp["S"]).(Name); group && s == "Transparency" {
		alpha, mode := st.fillAlpha, st.blend
		inner.fillAlpha, inner.strokeAlph = 1, 1
		inner.fill.A, inner.stroke.A = 1, 1
		inner.blend = render.BlendNormal
		inner.smask = nil // the group's own drawing is not masked twice
		rd.r.BeginGroup()
		rd.run(data, formRes, inner, depth+1)
		rd.r.EndGroup(mode, alpha)
		return
	}
	rd.run(data, formRes, inner, depth+1)
}

// dictGet looks a name up in a resource sub-dictionary (Font, XObject, …).
func dictGet(doc *Document, res Dict, kind, name Name) Object {
	sub, _ := doc.Resolve(res[kind]).(Dict)
	if sub == nil {
		return nil
	}
	return sub[name]
}

// lastString returns the last string operand.
func lastString(args []Object) (String, bool) {
	for i := len(args) - 1; i >= 0; i-- {
		if s, ok := args[i].(String); ok {
			return s, true
		}
	}
	return nil, false
}

// pathBounds is a path's device-space bounding box.
func pathBounds(p *render.Path) (Rect, bool) {
	subs := p.Polyline()
	first := true
	var x0, y0, x1, y1 float32
	for _, sub := range subs {
		for _, pt := range sub {
			if first {
				x0, y0, x1, y1 = pt.X, pt.Y, pt.X, pt.Y
				first = false
				continue
			}
			x0, y0 = minf(x0, pt.X), minf(y0, pt.Y)
			x1, y1 = maxf(x1, pt.X), maxf(y1, pt.Y)
		}
	}
	if first {
		return Rect{}, false
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// quadBounds is the device-space box of a user-space rectangle under m.
func quadBounds(m render.Affine2, x0, y0, x1, y1 float32) (Rect, bool) {
	xs := [4]float32{}
	ys := [4]float32{}
	xs[0], ys[0] = m.Apply(x0, y0)
	xs[1], ys[1] = m.Apply(x1, y0)
	xs[2], ys[2] = m.Apply(x1, y1)
	xs[3], ys[3] = m.Apply(x0, y1)
	lx, ly, hx, hy := xs[0], ys[0], xs[0], ys[0]
	for i := 1; i < 4; i++ {
		lx, ly = minf(lx, xs[i]), minf(ly, ys[i])
		hx, hy = maxf(hx, xs[i]), maxf(hy, ys[i])
	}
	if hx-lx <= 0 || hy-ly <= 0 {
		return Rect{}, false
	}
	return Rect{X: lx, Y: ly, W: hx - lx, H: hy - ly}, true
}

// intersect is the overlap of two rects (zero-sized when they miss).
func intersect(a, b Rect) Rect {
	x0, y0 := maxf(a.X, b.X), maxf(a.Y, b.Y)
	x1, y1 := minf(a.X+a.W, b.X+b.W), minf(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// decompose is the scale a transform applies on each axis.
func decompose(m render.Affine2) (sx, sy, rot float32) {
	sx = hypot(m.A, m.B)
	sy = hypot(m.C, m.D)
	return sx, sy, 0
}

func hypot(a, b float32) float32 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	// Good enough for a line width: the exact hypot costs a sqrt per stroke.
	if a < b {
		a, b = b, a
	}
	r := b / a
	return a * (1 + 0.428*r*r)
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// cmyk converts a CMYK colour to RGB the way PDF viewers do without a
// colour profile: a straight subtractive mix.
func cmyk(c, m, y, k float32) render.RGBA {
	return render.RGBA{
		R: clamp01f((1 - c) * (1 - k)),
		G: clamp01f((1 - m) * (1 - k)),
		B: clamp01f((1 - y) * (1 - k)),
		A: 1,
	}
}

func clamp01f(v float32) float32 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
