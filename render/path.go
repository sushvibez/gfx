package render

import (
	"math"
	"slices"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// Path is a 2D vector path: straight and cubic-Bézier segments grouped
// into subpaths, filled by CPU ear-clip tessellation (DESIGN.md §9, the
// "vector paths via CPU tessellation first" step). Pixel coordinates,
// (0,0) top-left. Strokes and holes come later; v0 fills each subpath as a
// solid simple polygon with the non-zero-area winding made consistent.
type Path struct {
	subs      []([]core.Vec2) // flattened polylines, one per subpath
	tol       float32         // curve flatness, path units (0 = fixed steps; see Flatness)
	closed    []bool          // whether each subpath was Close()d
	cur       []core.Vec2
	curClosed bool
	penX      float32
	penY      float32
}

// NewPath starts an empty path.
func NewPath() *Path { return &Path{} }

// Polyline returns the path's flattened points, subpath by subpath — for
// callers that need the SAMPLED curve rather than a fill (spacing items
// along a path, hit-testing ink).
func (p *Path) Polyline() [][]core.Vec2 {
	p.flushSub()
	return p.subs
}

// MoveTo begins a new subpath at (x, y).
func (p *Path) MoveTo(x, y float32) *Path {
	p.flushSub()
	p.cur = []core.Vec2{{X: x, Y: y}}
	p.penX, p.penY = x, y
	return p
}

// LineTo adds a straight segment to (x, y).
func (p *Path) LineTo(x, y float32) *Path {
	if p.cur == nil {
		return p.MoveTo(x, y)
	}
	p.cur = append(p.cur, core.Vec2{X: x, Y: y})
	p.penX, p.penY = x, y
	return p
}

// CubicTo adds a cubic Bézier from the current point through control
// points (c1,c2) to (x,y), flattened to line segments.
// Flatness sets the largest chord-to-curve error this path's cubics are
// flattened to, in the path's OWN coordinate units, and switches them to
// ADAPTIVE subdivision: a shallow curve then costs a couple of segments
// and a tight one as many as it needs.
//
// Paths whose coordinates are already pixels keep the fixed subdivision
// below (it is what every existing render was tuned against). An SVG is
// the case that needs this: it is flattened in viewBox units and drawn
// much smaller, so the fixed step tessellated a 960-unit icon into
// thousands of points no 24px draw could show — the buttons page uploaded
// 195k triangle vertices a frame, 6MB of them, for six toolbar icons.
func (p *Path) Flatness(tol float32) *Path { p.tol = tol; return p }

func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float32) *Path {
	if p.cur == nil {
		p.MoveTo(p.penX, p.penY)
	}
	x0, y0 := p.penX, p.penY
	if p.tol > 0 {
		p.cubicAdaptive(x0, y0, c1x, c1y, c2x, c2y, x, y, 0)
		p.penX, p.penY = x, y
		return p
	}
	// Subdivision scaled by a rough chord length. SVG curves are flattened in
	// viewBox units (often small) then scaled up at draw time, so keep a high
	// floor and fine step or the arcs facet visibly after scaling.
	chord := abs32(x-x0) + abs32(y-y0) + abs32(c1x-x0) + abs32(c2x-x)
	n := min(int(chord*3)+16, 160)
	for i := 1; i <= n; i++ {
		t := float32(i) / float32(n)
		mt := 1 - t
		// de Casteljau weights.
		b0 := mt * mt * mt
		b1 := 3 * mt * mt * t
		b2 := 3 * mt * t * t
		b3 := t * t * t
		px := b0*x0 + b1*c1x + b2*c2x + b3*x
		py := b0*y0 + b1*c1y + b2*c2y + b3*y
		p.cur = append(p.cur, core.Vec2{X: px, Y: py})
	}
	p.penX, p.penY = x, y
	return p
}

// cubicAdaptive splits at the midpoint until both control handles lie
// within p.tol of the chord (see Flatness).
func (p *Path) cubicAdaptive(x0, y0, c1x, c1y, c2x, c2y, x, y float32, depth int) {
	if depth >= 16 || p.cubicFlat(x0, y0, c1x, c1y, c2x, c2y, x, y) {
		p.cur = append(p.cur, core.Vec2{X: x, Y: y})
		return
	}
	ax, ay := (x0+c1x)/2, (y0+c1y)/2
	bx, by := (c1x+c2x)/2, (c1y+c2y)/2
	cx, cy := (c2x+x)/2, (c2y+y)/2
	dx, dy := (ax+bx)/2, (ay+by)/2
	ex, ey := (bx+cx)/2, (by+cy)/2
	mx, my := (dx+ex)/2, (dy+ey)/2
	p.cubicAdaptive(x0, y0, ax, ay, dx, dy, mx, my, depth+1)
	p.cubicAdaptive(mx, my, ex, ey, cx, cy, x, y, depth+1)
}

// cubicFlat is the usual control-point flatness test: the curve is within
// tol of its chord when both handles are, measured as a cross product so
// no square root is needed.
func (p *Path) cubicFlat(x0, y0, c1x, c1y, c2x, c2y, x, y float32) bool {
	dx, dy := x-x0, y-y0
	dd := dx*dx + dy*dy
	if dd == 0 {
		// A degenerate chord (a loop back to the start) has no line to
		// measure against: fall back to the handles' own reach.
		return abs32(c1x-x0)+abs32(c1y-y0)+abs32(c2x-x)+abs32(c2y-y) <= p.tol
	}
	d1 := abs32((c1x-x0)*dy - (c1y-y0)*dx)
	d2 := abs32((c2x-x0)*dy - (c2y-y0)*dx)
	d := max(d1, d2)
	return d*d <= p.tol*p.tol*dd
}

// Close finishes the current subpath as closed (the fill implicitly
// closes it too; a stroke connects the last point back to the first).
func (p *Path) Close() *Path {
	p.curClosed = true
	p.flushSub()
	return p
}

func (p *Path) flushSub() {
	if len(p.cur) >= 2 {
		p.subs = append(p.subs, p.cur)
		p.closed = append(p.closed, p.curClosed)
	}
	p.cur = nil
	p.curClosed = false
}

// Translated returns a copy of p with every point offset by (dx, dy) —
// used to place a path authored in local coordinates (an SVG path string,
// a QtQuick Shape) at an item's position on screen.
func (p *Path) Translated(dx, dy float32) *Path {
	p.flushSub()
	out := &Path{
		subs:   make([]([]core.Vec2), len(p.subs)),
		closed: slices.Clone(p.closed),
		penX:   p.penX + dx,
		penY:   p.penY + dy,
	}
	for i, sub := range p.subs {
		cp := make([]core.Vec2, len(sub))
		for j, pt := range sub {
			cp[j] = core.Vec2{X: pt.X + dx, Y: pt.Y + dy}
		}
		out.subs[i] = cp
	}
	return out
}

// FillRule decides which points a path that overlaps itself encloses —
// SVG's fill-rule, QML's ShapePath.fillRule, canvas fill()'s argument. It
// only matters where a path crosses itself or nests: a pentagram's centre
// is enclosed under NonZero and hollow under EvenOdd.
type FillRule int

const (
	// EvenOdd counts crossings: a point is inside when a ray from it
	// crosses the outline an odd number of times.
	EvenOdd FillRule = iota
	// NonZero sums the directions of those crossings: inside where the
	// winding number is not zero, so overlaps stay filled.
	NonZero
)

// FillPath tessellates p and queues it as solid triangles in color c,
// treating overlaps by the even-odd rule (QML's ShapePath default).
func (r *Renderer2D) FillPath(p *Path, c RGBA) { r.FillPathRule(p, c, EvenOdd) }

// FillPathRule is FillPath with an explicit fill rule.
func (r *Renderer2D) FillPathRule(p *Path, c RGBA, rule FillRule) {
	p.flushSub()
	tris := triangulateRule(p.subs, rule, r.tmpTris[:0])
	r.tmpTris = tris[:0]
	if len(tris) == 0 {
		return
	}
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	for _, pt := range tris {
		x, y := r.toNDC(pt.X, pt.Y)
		r.vertex(x, y, 0, 0, c)
	}
	b.count += len(tris)
}

// GradientStop is one stop of a multi-stop ramp: a position along the
// gradient axis (0..1) and the colour there.
type GradientStop struct {
	Pos float32
	Col RGBA
}

// FillPathRamp fills p with a MULTI-STOP linear gradient along angleRad.
// Like FillPathGradientStops the ramp is applied per vertex, so a stop that
// falls between two vertices is interpolated by the mesh rather than the
// ramp — subdivide the path (or fill band by band) where a mid stop must be
// exact.
func (r *Renderer2D) FillPathRamp(p *Path, angleRad float32, stops []GradientStop) {
	r.FillPathRampRule(p, angleRad, stops, EvenOdd)
}

// FillPathRampRule is FillPathRamp with an explicit fill rule. The ramp
// spans the SHAPE along the axis, which is what a QML gradient does; a
// canvas gradient is positioned by its own two points instead — see
// FillPathRampAxis.
func (r *Renderer2D) FillPathRampRule(p *Path, angleRad float32, stops []GradientStop, rule FillRule) {
	r.fillPathRamp(p, angleRad, 0, 0, false, stops, rule)
}

// FillPathRampAxis fills p with a multi-stop gradient running from (x0,y0)
// to (x1,y1) IN THE SAME SPACE as the path — a 2D canvas gradient, whose
// ramp is placed by its endpoints and holds its end colours beyond them,
// however much of it the shape actually covers.
func (r *Renderer2D) FillPathRampAxis(p *Path, x0, y0, x1, y1 float32, stops []GradientStop, rule FillRule) {
	ang := math32.Atan2(y1-y0, x1-x0)
	dx, dy := math32.Cos(ang), math32.Sin(ang)
	lo := x0*dx + y0*dy
	hi := x1*dx + y1*dy
	if hi == lo {
		hi = lo + 1
	}
	r.fillPathRamp(p, ang, lo, hi-lo, true, stops, rule)
}

func (r *Renderer2D) fillPathRamp(p *Path, angleRad, axisLo, axisSpan float32, fixedAxis bool, stops []GradientStop, rule FillRule) {
	switch len(stops) {
	case 0:
		return
	case 1:
		r.FillPathRule(p, stops[0].Col, rule)
		return
	}
	p.flushSub()
	tris := triangulateRule(p.subs, rule, r.tmpTris[:0])
	r.tmpTris = tris[:0]
	if len(tris) == 0 {
		return
	}
	dx, dy := math32.Cos(angleRad), math32.Sin(angleRad)
	proj := func(pt core.Vec2) float32 { return pt.X*dx + pt.Y*dy }
	lo := math32.Inf(1)
	hi := math32.Inf(-1)
	for _, pt := range tris {
		if v := proj(pt); v < lo {
			lo = v
		}
		if v := proj(pt); v > hi {
			hi = v
		}
	}
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	if fixedAxis {
		lo, span = axisLo, axisSpan
	}
	// Split the mesh along every interior stop, so each vertex sits on one
	// side of it and the GPU's interpolation between vertices is exact.
	for _, st := range stops {
		if st.Pos <= 0 || st.Pos >= 1 {
			continue
		}
		tris = splitTrisAt(tris, proj, lo+st.Pos*span)
	}
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	for _, pt := range tris {
		t := (proj(pt) - lo) / span
		c := SampleStops(stops, t)
		x, y := r.toNDC(pt.X, pt.Y)
		r.vertex(x, y, 0, 0, c)
	}
	b.count += len(tris)
}

// SampleStops evaluates a stop list at t (0..1), clamping outside the ends.
func SampleStops(stops []GradientStop, t float32) RGBA {
	if len(stops) == 0 {
		return RGBA{A: 1}
	}
	if t <= stops[0].Pos {
		return stops[0].Col
	}
	last := stops[len(stops)-1]
	if t >= last.Pos {
		return last.Col
	}
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if t > b.Pos {
			continue
		}
		span := b.Pos - a.Pos
		if span <= 0 {
			return b.Col
		}
		return lerpRGBA(a.Col, b.Col, (t-a.Pos)/span)
	}
	return last.Col
}

// lerpRGBA mixes two colours.
func lerpRGBA(a, b RGBA, t float32) RGBA {
	return RGBA{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}

// FillRadialRing fills the annulus between radii r0 and r1 around (cx, cy)
// with a multi-stop RADIAL ramp: colour by distance from the centre, which a
// linear ramp cannot express. The inner disc takes the first stop's colour,
// so the whole circle is covered.
//
// Canvas' createRadialGradient is almost always used exactly this way — the
// same centre and radius as the arc being filled — so the ring fan matches
// the shape it is filling.
func (r *Renderer2D) FillRadialRing(cx, cy, r0, r1 float32, stops []GradientStop) {
	if len(stops) == 0 || r1 <= 0 {
		return
	}
	if r0 < 0 {
		r0 = 0
	}
	if r0 > 0 {
		// Inner disc: flat, the ramp's first colour.
		disc := NewPath()
		disc.MoveTo(cx+r0, cy)
		const segs = 48
		for i := 1; i <= segs; i++ {
			th := 2 * math32.Pi * float32(i) / segs
			disc.LineTo(cx+r0*math32.Cos(th), cy+r0*math32.Sin(th))
		}
		r.FillPath(disc.Close(), stops[0].Col)
	}
	// The annulus, as a strip of quads per ring band: one band per stop
	// interval so every stop lands exactly, subdivided for smoothness.
	const rings = 24
	const segs = 48
	prevR := r0
	for i := 1; i <= rings; i++ {
		t1 := float32(i) / rings
		curR := r0 + (r1-r0)*t1
		t0 := (prevR - r0) / max(r1-r0, 0.0001)
		c0 := SampleStops(stops, t0)
		c1 := SampleStops(stops, t1)
		for s := 0; s < segs; s++ {
			a0 := 2 * math32.Pi * float32(s) / segs
			a1 := 2 * math32.Pi * float32(s+1) / segs
			x00, y00 := cx+prevR*math32.Cos(a0), cy+prevR*math32.Sin(a0)
			x01, y01 := cx+prevR*math32.Cos(a1), cy+prevR*math32.Sin(a1)
			x10, y10 := cx+curR*math32.Cos(a0), cy+curR*math32.Sin(a0)
			x11, y11 := cx+curR*math32.Cos(a1), cy+curR*math32.Sin(a1)
			r.quadShaded(x00, y00, c0, x01, y01, c0, x11, y11, c1, x10, y10, c1)
		}
		prevR = curR
	}
}

// quadShaded queues one four-corner quad with a colour per corner.
func (r *Renderer2D) quadShaded(x0, y0 float32, c0 RGBA, x1, y1 float32, c1 RGBA,
	x2, y2 float32, c2 RGBA, x3, y3 float32, c3 RGBA) {

	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	put := func(x, y float32, c RGBA) {
		nx, ny := r.toNDC(x, y)
		r.vertex(nx, ny, 0, 0, c)
	}
	put(x0, y0, c0)
	put(x1, y1, c1)
	put(x2, y2, c2)
	put(x0, y0, c0)
	put(x2, y2, c2)
	put(x3, y3, c3)
	b.count += 6
}

// FillPathGradient fills p with a linear gradient from c0 to c1 along angleRad
// (0 = left→right, π/2 = top→bottom), measured across the path's bounding box.
// Each triangle vertex is colored by its projection onto the gradient axis and
// the GPU interpolates between them (Gouraud) — smooth for convex/star shapes.
func (r *Renderer2D) FillPathGradient(p *Path, c0, c1 RGBA, angleRad float32) {
	r.FillPathGradientStops(p, c0, c1, angleRad, 0, 1)
}

// FillPathGradientRule is FillPathGradient with an explicit fill rule, for
// a gradient-filled outline that crosses itself.
func (r *Renderer2D) FillPathGradientRule(p *Path, c0, c1 RGBA, angleRad float32, rule FillRule) {
	r.fillGradientStopsRule(p, c0, c1, angleRad, 0, 1, rule)
}

// FillPathGradientStops is FillPathGradient with ramp positions (CSS/GPUI
// linear_color_stop): c0 holds until s0, c1 from s1 on (0..1 along the axis).
// The ramp is applied per vertex, so between-stop accuracy follows the mesh —
// exact at vertices, linear between them.
func (r *Renderer2D) FillPathGradientStops(p *Path, c0, c1 RGBA, angleRad, s0, s1 float32) {
	r.fillGradientStopsRule(p, c0, c1, angleRad, s0, s1, EvenOdd)
}

func (r *Renderer2D) fillGradientStopsRule(p *Path, c0, c1 RGBA, angleRad, s0, s1 float32, rule FillRule) {
	p.flushSub()
	tris := triangulateRule(p.subs, rule, r.tmpTris[:0])
	r.tmpTris = tris[:0]
	if len(tris) == 0 {
		return
	}
	// Bounding box of all triangle vertices for axis normalization.
	minX, minY := tris[0].X, tris[0].Y
	maxX, maxY := minX, minY
	for _, pt := range tris {
		if pt.X < minX {
			minX = pt.X
		}
		if pt.X > maxX {
			maxX = pt.X
		}
		if pt.Y < minY {
			minY = pt.Y
		}
		if pt.Y > maxY {
			maxY = pt.Y
		}
	}
	dx, dy := math32.Cos(angleRad), math32.Sin(angleRad)
	// Projection range of the bbox corners onto the axis (for [0,1] mapping).
	lo := math32.Inf(1)
	hi := math32.Inf(-1)
	for _, c := range [4][2]float32{{minX, minY}, {maxX, minY}, {minX, maxY}, {maxX, maxY}} {
		pr := c[0]*dx + c[1]*dy
		if pr < lo {
			lo = pr
		}
		if pr > hi {
			hi = pr
		}
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	// Gouraud interpolation is only exact at vertices: a triangle edge that
	// crosses a clamped stop boundary would smear the blend across the whole
	// edge. Split triangles along both stop iso-lines so every vertex sits
	// on one side and interpolation between them is exact.
	proj := func(p core.Vec2) float32 { return p.X*dx + p.Y*dy }
	for _, s := range [2]float32{s0, s1} {
		if s <= 0 || s >= 1 {
			continue
		}
		tris = splitTrisAt(tris, proj, lo+s*span)
	}
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	for _, pt := range tris {
		t := (pt.X*dx + pt.Y*dy - lo) / span
		if s1 > s0 {
			t = min(max((t-s0)/(s1-s0), 0), 1)
		}
		c := RGBA{
			R: c0.R + (c1.R-c0.R)*t,
			G: c0.G + (c1.G-c0.G)*t,
			B: c0.B + (c1.B-c0.B)*t,
			A: c0.A + (c1.A-c0.A)*t,
		}
		x, y := r.toNDC(pt.X, pt.Y)
		r.vertex(x, y, 0, 0, c)
	}
	b.count += len(tris)
}

// triangulateSubs tessellates a path's subpaths with hole support. A single
// subpath ear-clips directly; multiple subpaths tessellate by even-odd
// scanline slabs (SVG fill-rule evenodd) — robust for letter counters, icon
// ring gaps, nested islands and self-intersections alike: each horizontal
// slab between consecutive vertex Ys pairs its crossing edges left-to-right
// and emits the trapezoids between odd/even pairs.
func triangulateSubs(subs [][]core.Vec2, out []core.Vec2) []core.Vec2 {
	return triangulateRule(subs, EvenOdd, out)
}

// fillEdge is one edge of an outline, oriented downward, remembering which
// way the original ran: the winding rule sums those directions.
type fillEdge struct {
	x0, y0, x1, y1 float32 // y0 < y1
	dir            int     // +1 the edge ran downward, -1 upward
}

// triangulateRule tessellates under a fill rule. A single simple outline
// goes to the ear clipper, which is exact and cheap; anything that nests or
// crosses itself goes to the scanline, where a rule is meaningful — ear
// clipping has no notion of inside-ness beyond one closed ring.
func triangulateRule(subs [][]core.Vec2, rule FillRule, out []core.Vec2) []core.Vec2 {
	edges, ys := fillEdges(subs)
	if len(edges) == 0 {
		return out
	}
	if len(subs) == 1 {
		crossings := crossingYs(edges)
		if len(crossings) == 0 {
			return earClip(subs[0], out) // simple outline: exact and cheap
		}
		ys = append(ys, crossings...)
	} else if cy := crossingYs(edges); len(cy) > 0 {
		ys = append(ys, cy...)
	}
	slices.Sort(ys)
	ys = slices.Compact(ys)

	// x where an edge meets the slab's top, bottom and midline, plus its
	// direction. Slab boundaries are every vertex Y *and every Y where two
	// edges cross*, so within a slab the edges keep their left-to-right
	// order and the quad between two of them is exactly covered.
	type crossing struct {
		xa, xb, xm float32
		dir        int
	}
	var row []crossing
	for k := 0; k+1 < len(ys); k++ {
		y0, y1 := ys[k], ys[k+1]
		if y1-y0 < 1e-6 {
			continue
		}
		ym := (y0 + y1) / 2
		row = row[:0]
		for _, e := range edges {
			if e.y0 > ym || e.y1 < ym {
				continue
			}
			xa := e.x0 + (e.x1-e.x0)*(y0-e.y0)/(e.y1-e.y0)
			xb := e.x0 + (e.x1-e.x0)*(y1-e.y0)/(e.y1-e.y0)
			row = append(row, crossing{xa, xb, (xa + xb) / 2, e.dir})
		}
		slices.SortFunc(row, func(a, b crossing) int {
			switch {
			case a.xm < b.xm:
				return -1
			case a.xm > b.xm:
				return 1
			}
			return 0
		})
		span := func(l, r crossing) {
			out = append(out,
				core.Vec2{X: l.xa, Y: y0}, core.Vec2{X: r.xa, Y: y0}, core.Vec2{X: r.xb, Y: y1},
				core.Vec2{X: l.xa, Y: y0}, core.Vec2{X: r.xb, Y: y1}, core.Vec2{X: l.xb, Y: y1})
		}
		if rule == EvenOdd {
			for i := 0; i+1 < len(row); i += 2 {
				span(row[i], row[i+1])
			}
			continue
		}
		// NonZero: the strip between two crossings is inside while the
		// running winding number is not zero, so overlaps that turn the
		// same way stay filled instead of cancelling.
		w := 0
		var start crossing
		for _, c := range row {
			prev := w
			w += c.dir
			switch {
			case prev == 0 && w != 0:
				start = c
			case prev != 0 && w == 0:
				span(start, c)
			}
		}
	}
	return out
}

// fillEdges flattens subpaths into downward-oriented edges plus the vertex
// Ys that bound the scanline's slabs. Horizontal edges carry no crossing
// and are dropped.
func fillEdges(subs [][]core.Vec2) ([]fillEdge, []float32) {
	var edges []fillEdge
	var ys []float32
	for _, s := range subs {
		if len(s) < 3 {
			continue
		}
		for i := range s {
			a, b := s[i], s[(i+1)%len(s)]
			if a.Y == b.Y {
				continue
			}
			dir := 1
			if a.Y > b.Y {
				a, b, dir = b, a, -1
			}
			edges = append(edges, fillEdge{a.X, a.Y, b.X, b.Y, dir})
		}
		for _, v := range s {
			ys = append(ys, v.Y)
		}
	}
	return edges, ys
}

// selfCrossMax bounds the O(E²) crossing search: outlines whose fill rule
// matters are small (a star, a knot, a glyph), while a path with thousands
// of edges is a flattened curve, where the search would cost more than the
// fill and the crossings are sub-pixel anyway.
const selfCrossMax = 256

// crossingYs returns the Y of every proper crossing between two edges —
// the slab boundaries the scanline needs to keep its left-to-right order,
// and the answer to "does this outline overlap itself at all". Empty for a
// simple outline, and for one too large to search.
func crossingYs(edges []fillEdge) []float32 {
	if len(edges) < 3 || len(edges) > selfCrossMax {
		return nil
	}
	var out []float32
	for i := 0; i < len(edges); i++ {
		a := edges[i]
		a1 := core.Vec2{X: a.x0, Y: a.y0}
		a2 := core.Vec2{X: a.x1, Y: a.y1}
		for j := i + 1; j < len(edges); j++ {
			b := edges[j]
			if b.y0 >= a.y1 || b.y1 <= a.y0 {
				continue // disjoint in Y: cannot cross
			}
			b1 := core.Vec2{X: b.x0, Y: b.y0}
			b2 := core.Vec2{X: b.x1, Y: b.y1}
			if y, ok := crossY(a1, a2, b1, b2); ok {
				out = append(out, y)
			}
		}
	}
	return out
}

// crossY is the Y where two segments properly cross — interior to both, so
// segments that merely share an endpoint (every pair of neighbours) do not
// count.
func crossY(a1, a2, b1, b2 core.Vec2) (float32, bool) {
	d := func(p, q, r core.Vec2) float32 {
		return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X)
	}
	d1, d2 := d(b1, b2, a1), d(b1, b2, a2)
	d3, d4 := d(a1, a2, b1), d(a1, a2, b2)
	if !(((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))) {
		return 0, false
	}
	t := d1 / (d1 - d2) // where along a the crossing falls
	return a1.Y + t*(a2.Y-a1.Y), true
}

// selfIntersects reports whether an outline crosses itself, which is what
// makes a fill rule matter.
func selfIntersects(poly []core.Vec2) bool {
	edges, _ := fillEdges([][]core.Vec2{poly})
	return len(crossingYs(edges)) > 0
}

// splitTrisAt cuts every triangle crossing the iso-line proj(p) == pv into
// sub-triangles whose vertices all lie on one side (vertices exactly on the
// line join both). The two pieces of a cut triangle are convex, so each is
// fan-triangulated.
func splitTrisAt(tris []core.Vec2, proj func(core.Vec2) float32, pv float32) []core.Vec2 {
	out := make([]core.Vec2, 0, len(tris)+12)
	fan := func(poly []core.Vec2) {
		for i := 1; i+1 < len(poly); i++ {
			out = append(out, poly[0], poly[i], poly[i+1])
		}
	}
	var below, above [5]core.Vec2
	for i := 0; i+2 < len(tris); i += 3 {
		v := [3]core.Vec2{tris[i], tris[i+1], tris[i+2]}
		d := [3]float32{proj(v[0]) - pv, proj(v[1]) - pv, proj(v[2]) - pv}
		if (d[0] <= 0 && d[1] <= 0 && d[2] <= 0) || (d[0] >= 0 && d[1] >= 0 && d[2] >= 0) {
			out = append(out, v[0], v[1], v[2])
			continue
		}
		nb, na := 0, 0
		for k := 0; k < 3; k++ {
			p0, p1 := v[k], v[(k+1)%3]
			d0, d1 := d[k], d[(k+1)%3]
			if d0 <= 0 {
				below[nb] = p0
				nb++
			}
			if d0 >= 0 {
				above[na] = p0
				na++
			}
			if (d0 < 0 && d1 > 0) || (d0 > 0 && d1 < 0) {
				t := d0 / (d0 - d1)
				x := core.Vec2{X: p0.X + (p1.X-p0.X)*t, Y: p0.Y + (p1.Y-p0.Y)*t}
				below[nb] = x
				above[na] = x
				nb++
				na++
			}
		}
		fan(below[:nb])
		fan(above[:na])
	}
	return out
}

// earClip triangulates a simple polygon, appending triangle vertices
// (3 per triangle) to out. It normalizes winding to counter-clockwise
// first, so callers may pass either orientation.
// dropCollinear removes duplicate and collinear vertices, which carry no
// shape but stall ear clipping.
func dropCollinear(poly []core.Vec2) []core.Vec2 {
	if len(poly) < 4 {
		return poly
	}
	same := func(a, b core.Vec2) bool {
		return math32.Abs(a.X-b.X) < 1e-5 && math32.Abs(a.Y-b.Y) < 1e-5
	}
	// Repeated points first, the wrap-around included: a path that closes by
	// repeating its start point (`PathLine { x: startX; y: startY }`) has one,
	// and judging a vertex against a duplicate of itself makes every corner
	// look flat — which is how a star used to lose its top point.
	ring := make([]core.Vec2, 0, len(poly))
	for _, p := range poly {
		if n := len(ring); n > 0 && same(ring[n-1], p) {
			continue
		}
		ring = append(ring, p)
	}
	for len(ring) > 3 && same(ring[0], ring[len(ring)-1]) {
		ring = ring[:len(ring)-1]
	}
	if len(ring) < 4 {
		return ring
	}
	// Then the genuinely flat vertices. The test is ANCHORED, not
	// per-vertex: a vertex is dropped only while the run it belongs to
	// still tracks the straight line from the last kept point, so an
	// exactly-collinear run collapses to its endpoints while a flattened
	// CURVE keeps its shape. Judging each vertex against its immediate
	// neighbours instead let a finely flattened curve go — every single
	// sample looks flat next to the two beside it — and the error piled up
	// into a visible polygon (the Qt logo's Q came out a dodecagon at 300px
	// and smoothed out at 600px, since the tolerance is in the path's own
	// units and SVG paths arrive already scaled to the screen).
	const flatTol = 0.02 // px of deviation from the run's chord
	const maxRun = 64    // bound the work on a long straight edge
	out := make([]core.Vec2, 0, len(ring))
	anchor := ring[0]
	out = append(out, anchor)
	run := 0
	for i := 1; i < len(ring); i++ {
		p := ring[i]
		// Would dropping everything between the anchor and p keep the run
		// within tolerance? Check the points actually being skipped.
		straight := run < maxRun
		for k := i - run; k < i && straight; k++ {
			if segDist(ring[k], anchor, p) > flatTol {
				straight = false
			}
		}
		if straight {
			run++
			continue
		}
		// ring[i-1] is where the run stopped tracking: keep it.
		out = append(out, ring[i-1])
		anchor = ring[i-1]
		run = 0
		i-- // re-judge p against the new anchor
	}
	if last := ring[len(ring)-1]; !same(out[len(out)-1], last) {
		out = append(out, last)
	}
	if len(out) < 3 {
		return ring
	}
	return out
}

// segDist is the distance from p to the segment (a,b) — how far a vertex
// strays from the straight line a run is being collapsed onto.
func segDist(p, a, b core.Vec2) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 < 1e-12 {
		return math32.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = min(max(t, 0), 1)
	return math32.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

func earClip(poly []core.Vec2, out []core.Vec2) []core.Vec2 {
	// Collinear runs (a rounded rect's straight edges, a flattened curve's
	// near-straight middle) have no ear at any of their vertices, so an
	// ear-clipper that only accepts strictly convex tips stalls on them and
	// bails with a few corner triangles. Drop them first.
	poly = dropCollinear(poly)
	n := len(poly)
	if n < 3 {
		return out
	}
	// Index ring; ensure CCW (positive signed area in y-down space means
	// clockwise on screen, so flip to keep cross-product tests consistent).
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	if signedArea(poly) < 0 {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			idx[i], idx[j] = idx[j], idx[i]
		}
	}
	// Only a REFLEX vertex can lie inside an ear, so the containment test
	// scans those alone — kept as a live set, updated where the ring
	// changes. Scanning the whole ring instead makes the clipper O(n^3):
	// the shapes page's mandala and rose curves are outlines of a thousand
	// points, rebuilt every frame, and took SECONDS a frame that way.
	reflex := make([]bool, 0, n)
	m := len(idx)
	for i := 0; i < m; i++ {
		reflex = append(reflex, cross(poly[idx[(i+m-1)%m]], poly[idx[i]], poly[idx[(i+1)%m]]) <= 0)
	}
	setConvexity := func(i int) {
		m := len(idx)
		reflex[i] = cross(poly[idx[(i+m-1)%m]], poly[idx[i]], poly[idx[(i+1)%m]]) <= 0
	}
	drop := func(i int) {
		idx = append(idx[:i], idx[i+1:]...)
		reflex = append(reflex[:i], reflex[i+1:]...)
		if m := len(idx); m > 0 {
			setConvexity(i % m)
			setConvexity((i + m - 1) % m)
		}
	}

	guard := 0
	for len(idx) > 3 {
		clipped := false
		m := len(idx)
		for i := 0; i < m; i++ {
			if reflex[i] {
				continue // reflex vertex, not an ear tip
			}
			a := poly[idx[(i+m-1)%m]]
			b := poly[idx[i]]
			cc := poly[idx[(i+1)%m]]
			if anyReflexInside(poly, idx, reflex, i, a, b, cc) {
				continue
			}
			out = append(out, a, b, cc)
			drop(i)
			clipped = true
			break
		}
		guard++
		if !clipped {
			// No strictly convex ear left (numerically flat vertices, or a
			// self-touching outline): drop the flattest vertex and retry
			// rather than abandoning the rest of the polygon.
			flat, best := 0, math32.Inf(1)
			for i := 0; i < m; i++ {
				a := poly[idx[(i+m-1)%m]]
				b := poly[idx[i]]
				cc := poly[idx[(i+1)%m]]
				if v := math32.Abs(cross(a, b, cc)); v < best {
					flat, best = i, v
				}
			}
			drop(flat)
		}

		if guard > n*n {
			break // degenerate polygon; bail with what we have
		}
	}
	if len(idx) == 3 {
		out = append(out, poly[idx[0]], poly[idx[1]], poly[idx[2]])
	}
	return out
}

func anyReflexInside(poly []core.Vec2, idx []int, reflex []bool, ear int, a, b, c core.Vec2) bool {
	minX, maxX := min(a.X, min(b.X, c.X)), max(a.X, max(b.X, c.X))
	minY, maxY := min(a.Y, min(b.Y, c.Y)), max(a.Y, max(b.Y, c.Y))
	m := len(idx)
	for j := 0; j < m; j++ {
		if !reflex[j] || j == ear || j == (ear+m-1)%m || j == (ear+1)%m {
			continue
		}
		p := poly[idx[j]]
		if p.X < minX || p.X > maxX || p.Y < minY || p.Y > maxY {
			continue
		}
		if pointInTri(p, a, b, c) {
			return true
		}
	}
	return false
}

func anyInside(poly []core.Vec2, idx []int, ear int, a, b, c core.Vec2) bool {
	// The ear's bounding box first: a point outside it cannot be inside
	// the triangle, and the box test is three comparisons against a
	// point-in-triangle's three cross products. On the shapes page's
	// mandala and rose curves — outlines of a thousand-odd points, where
	// this is the O(n^3) inner loop — it is the difference between a
	// frame taking seconds and taking milliseconds.
	minX, maxX := min(a.X, min(b.X, c.X)), max(a.X, max(b.X, c.X))
	minY, maxY := min(a.Y, min(b.Y, c.Y)), max(a.Y, max(b.Y, c.Y))
	m := len(idx)
	for j := 0; j < m; j++ {
		if j == ear || j == (ear+m-1)%m || j == (ear+1)%m {
			continue
		}
		p := poly[idx[j]]
		if p.X < minX || p.X > maxX || p.Y < minY || p.Y > maxY {
			continue
		}
		if pointInTri(p, a, b, c) {
			return true
		}
	}
	return false
}

func signedArea(p []core.Vec2) float32 {
	var a float32
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return a * 0.5
}

// cross is the z of (b-a)×(c-b); >0 means a convex (CCW) corner.
func cross(a, b, c core.Vec2) float32 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func pointInTri(p, a, b, c core.Vec2) bool {
	d1 := cross(a, b, p)
	d2 := cross(b, c, p)
	d3 := cross(c, a, p)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// StrokePath outlines p with the given width in color c. Joins use a
// clamped miter (capped to avoid spikes at sharp angles); open subpaths get
// butt caps, closed ones connect back to the start. Built as triangles
// through the sprite white-texture path, like FillPath.
func (r *Renderer2D) StrokePath(p *Path, width float32, c RGBA) {
	r.strokePath(p, width, c, false)
}

// StrokePathRound strokes p with round line caps and joins (SVG
// stroke-linecap/linejoin="round"): a filled disc of radius width/2 is added
// at every vertex, rounding both the open ends and the interior corners.
func (r *Renderer2D) StrokePathRound(p *Path, width float32, c RGBA) {
	r.strokePath(p, width, c, true)
}

func (r *Renderer2D) strokePath(p *Path, width float32, c RGBA, round bool) {
	p.flushSub()
	hw := width / 2
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	emit := func(a core.Vec2) {
		x, y := r.toNDC(a.X, a.Y)
		r.vertex(x, y, 0, 0, c)
	}
	tri := func(p0, p1, p2 core.Vec2) {
		emit(p0)
		emit(p1)
		emit(p2)
		b.count += 3
	}
	// disc emits a filled circle (triangle fan) at center — used for round
	// caps/joins. Segment count scales with radius so small glyphs stay cheap.
	disc := func(center core.Vec2) {
		segs := 8
		if hw > 4 {
			segs = 16
		}
		var prev core.Vec2
		for k := 0; k <= segs; k++ {
			ang := 2 * math.Pi * float64(k) / float64(segs)
			pt := core.Vec2{X: center.X + hw*float32(math.Cos(ang)), Y: center.Y + hw*float32(math.Sin(ang))}
			if k > 0 {
				tri(center, prev, pt)
			}
			prev = pt
		}
	}
	for si, poly := range p.subs {
		pts := poly
		if p.closed[si] && len(pts) >= 3 {
			pts = append(append([]core.Vec2{}, pts...), pts[0]) // wrap
		}
		left, right := strokeOffsets(pts, hw, p.closed[si])
		for i := 0; i+1 < len(pts); i++ {
			// Quad (left[i], right[i], right[i+1], left[i+1]).
			tri(left[i], right[i], right[i+1])
			tri(left[i], right[i+1], left[i+1])
		}
		if round {
			// Round CAPS only. The offsets above already miter each
			// interior joint, so the quads meet there — a disc at every
			// point would just overlap them, and with a translucent colour
			// that overlap shows as a bead at every vertex (a thick
			// canvas arc came out looking like a dotted line).
			if !p.closed[si] && len(pts) > 0 {
				disc(pts[0])
				disc(pts[len(pts)-1])
			}
		}
	}
}

// StrokePathAA strokes p with anti-aliased edges: the core stroke is
// flanked by ~1px feather strips whose vertex alpha fades to zero, and
// round caps/joins get a feathered rim. Sub-pixel widths render at the
// feather width with proportionally reduced alpha (coverage), so hairline
// strokes stay smooth instead of ropey. This is the stroke for projected
// vector overlays (CAD sketch ink, grids); the hard-edged StrokePath
// remains for shapes that will be MSAA'd or that tile exactly.
func (r *Renderer2D) StrokePathAA(p *Path, width float32, c RGBA) {
	// ~0.8px feather: enough to kill stairstepping without reading as
	// blur — CAD ink wants crisp.
	const feather = 0.8
	if width < 1 {
		// Coverage-compensated hairline.
		c.A *= width
		width = 1
	}
	p.flushSub()
	hw := width / 2
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	fade := c
	fade.A = 0
	emit := func(a core.Vec2, col RGBA) {
		x, y := r.toNDC(a.X, a.Y)
		r.vertex(x, y, 0, 0, col)
	}
	tri := func(p0, p1, p2 core.Vec2, c0, c1, c2 RGBA) {
		emit(p0, c0)
		emit(p1, c1)
		emit(p2, c2)
		b.count += 3
	}
	quad := func(a0, a1, b1, b0 core.Vec2, ca, cb RGBA) {
		// a-edge colored ca, b-edge colored cb.
		tri(a0, a1, b1, ca, ca, cb)
		tri(a0, b1, b0, ca, cb, cb)
	}
	// Feathered disc: solid fan to hw, rim fading hw → hw+feather.
	disc := func(center core.Vec2) {
		segs := 12
		if hw > 4 {
			segs = 24
		}
		ring := make([]core.Vec2, segs+1)
		rim := make([]core.Vec2, segs+1)
		for k := 0; k <= segs; k++ {
			ang := 2 * math.Pi * float64(k) / float64(segs)
			s, co := float32(math.Sin(ang)), float32(math.Cos(ang))
			ring[k] = core.Vec2{X: center.X + hw*co, Y: center.Y + hw*s}
			rim[k] = core.Vec2{X: center.X + (hw+feather)*co, Y: center.Y + (hw+feather)*s}
		}
		for k := 0; k < segs; k++ {
			tri(center, ring[k], ring[k+1], c, c, c)
			quad(ring[k], ring[k+1], rim[k+1], rim[k], c, fade)
		}
	}
	for si, poly := range p.subs {
		pts := poly
		if p.closed[si] && len(pts) >= 3 {
			pts = append(append([]core.Vec2{}, pts...), pts[0]) // wrap
		}
		if len(pts) < 2 {
			continue
		}
		left, right := strokeOffsets(pts, hw, p.closed[si])
		lo, ro := strokeOffsets(pts, hw+feather, p.closed[si])
		for i := 0; i+1 < len(pts); i++ {
			quad(left[i], left[i+1], right[i+1], right[i], c, c) // core
			quad(left[i], left[i+1], lo[i+1], lo[i], c, fade)    // feather L
			quad(right[i], right[i+1], ro[i+1], ro[i], c, fade)  // feather R
		}
		// Round joins everywhere; round caps on open ends.
		for i, pt := range pts {
			if !p.closed[si] || i < len(pts)-1 {
				disc(pt)
			}
		}
	}
}

// strokeOffsets returns the inner/outer offset points for each vertex of a
// polyline, using averaged-normal (miter) joins clamped to 4x half-width.
func strokeOffsets(pts []core.Vec2, hw float32, closed bool) (left, right []core.Vec2) {
	n := len(pts)
	left = make([]core.Vec2, n)
	right = make([]core.Vec2, n)
	seg := func(i int) core.Vec2 { // unit direction of segment i->i+1
		d := core.Vec2{X: pts[i+1].X - pts[i].X, Y: pts[i+1].Y - pts[i].Y}
		return d.Normalize()
	}
	norm := func(d core.Vec2) core.Vec2 { return core.Vec2{X: -d.Y, Y: d.X} }
	for i := range n {
		var nrm core.Vec2
		miter := hw
		switch {
		case i == 0:
			nrm = norm(seg(0))
		case i == n-1:
			nrm = norm(seg(n - 2))
		default:
			na, nb := norm(seg(i-1)), norm(seg(i))
			m := core.Vec2{X: na.X + nb.X, Y: na.Y + nb.Y}.Normalize()
			d := m.X*na.X + m.Y*na.Y
			if d < 0.25 {
				d = 0.25 // clamp the miter so sharp angles don't spike
			}
			nrm, miter = m, hw/d
		}
		left[i] = core.Vec2{X: pts[i].X + nrm.X*miter, Y: pts[i].Y + nrm.Y*miter}
		right[i] = core.Vec2{X: pts[i].X - nrm.X*miter, Y: pts[i].Y - nrm.Y*miter}
	}
	return left, right
}

// fillTrisAffine is fillTrisXF for the common case — no rotation — with
// the transform inline. The closure call was a tenth of a frame on a page
// with three icons: one indirect call per VERTEX.
func (r *Renderer2D) fillTrisAffine(tris []core.Vec2, c RGBA, ox, oy, sx, sy float32) {
	if len(tris) == 0 {
		return
	}
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	for _, pt := range tris {
		x, y := r.toNDC(ox+pt.X*sx, oy+pt.Y*sy)
		r.vertex(x, y, 0, 0, c)
	}
	b.count += len(tris)
}

// fillTrisXF emits an already-triangulated fill, mapping each vertex
// through xf on the way into the batch. The triangulation of an affine
// transform of a polygon is the transform of its triangulation, so a
// shape whose outline never changes — an icon, a glyph-like path — can be
// ear-clipped once and merely MOVED on every later frame.
func (r *Renderer2D) fillTrisXF(tris []core.Vec2, c RGBA, xf func(x, y float32) (float32, float32)) {
	if len(tris) == 0 {
		return
	}
	b := r.ensureBatch(kindSprite, r.white.bind, len(r.verts)/floatsPerVertex)
	for _, pt := range tris {
		X, Y := xf(pt.X, pt.Y)
		x, y := r.toNDC(X, Y)
		r.vertex(x, y, 0, 0, c)
	}
	b.count += len(tris)
}
