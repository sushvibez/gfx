package render

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math/bits"

	"github.com/chewxy/math32"
	"golang.org/x/image/vector"

	"github.com/sushvibez/gfx/core"
)

// Small SVGs — icons — are rasterized on the CPU with exact per-pixel area
// coverage (golang.org/x/image/vector, the same signed-area accumulation as
// font rasterizers and a browser's path rasterizer), once per device size,
// and then drawn as a texture at a whole-pixel position. Drawn as
// triangles under 4× MSAA instead, a 1–2 px stroke has only five levels of
// coverage and a curve comes out stair-stepped: an icon at 14–18 px looked
// like a pixel-art copy of itself. A texel-aligned texture of true
// coverage is what a browser shows for the same SVG.
//
// Only an upright drawing (no rotation, no 2D camera, no transform but a
// translation) of at most svgRasterMax device pixels, without dashes, goes
// this way; anything else is still tessellated.

const (
	svgRasterMax   = 128 // device px: past this a drawing is not an icon
	svgRasterSweep = 600 // frames an unused raster is kept
)

type svgRasterKey struct {
	s      *SVG
	sx, sy float32 // device pixels per viewBox unit
}

type svgRaster struct {
	img  *Image
	used uint32
}

// drawSVGRaster draws s as a cached coverage raster, reporting false when
// this drawing is not one it handles.
func (r *Renderer2D) drawSVGRaster(s *SVG, x, y, sx, sy float32, c RGBA, colorize bool) bool {
	if r.camZoom != 0 || sx <= 0 || sy <= 0 {
		return false
	}
	var ex, ey float32
	if len(r.xforms) > 0 {
		m := r.xform()
		if m.A != 1 || m.B != 0 || m.C != 0 || m.D != 1 {
			return false
		}
		ex, ey = m.E, m.F
	}
	for i := range s.shapes {
		if len(s.shapes[i].dash) > 0 {
			return false
		}
	}
	dpi := r.sc()
	key := svgRasterKey{s, sx * dpi, sy * dpi}
	w, h := svgRasterSize(s, key.sx, key.sy)
	if w > svgRasterMax+2*svgRasterPad || h > svgRasterMax+2*svgRasterPad {
		return false
	}
	e := r.svgRasters[key]
	if e == nil {
		img := svgRasterize(s, key.sx, key.sy)
		if img == nil {
			return false
		}
		if r.svgRasters == nil {
			r.svgRasters = map[svgRasterKey]*svgRaster{}
		}
		e = &svgRaster{img: r.NewImage(img.Rect.Dx(), img.Rect.Dy(), img.Pix)}
		r.svgRasters[key] = e
	}
	e.used = r.frame
	// The drawing's origin moved to a whole device pixel, less the pad, so
	// every texel lands on one pixel (no resampling blur).
	ox := math32.Round((x+ex)*dpi) - svgRasterPad
	oy := math32.Round((y+ey)*dpi) - svgRasterPad
	lx, ly := ox/dpi-ex, oy/dpi-ey
	lw, lh := float32(e.img.W)/dpi, float32(e.img.H)/dpi
	if colorize {
		r.DrawImageColorized(e.img, lx, ly, lw, lh, c)
	} else {
		r.DrawImage(e.img, lx, ly, lw, lh, c)
	}
	return true
}

// sweepSVGRasters frees the rasters no frame has drawn in a while (an icon
// drawn at a size it has left, a document that was replaced).
func (r *Renderer2D) sweepSVGRasters() {
	if r.frame%shapeSweep != 0 {
		return
	}
	for k, e := range r.svgRasters {
		if r.frame-e.used >= svgRasterSweep {
			e.img.Release()
			delete(r.svgRasters, k)
		}
	}
}

func (r *Renderer2D) releaseSVGRasters() {
	for k, e := range r.svgRasters {
		e.img.Release()
		delete(r.svgRasters, k)
	}
}

// svgRasterPad is the transparent margin around the box, for the
// anti-aliased edge of a stroke that reaches it.
const svgRasterPad = 1

func svgRasterSize(s *SVG, sx, sy float32) (int, int) {
	return int(math32.Ceil(s.W*sx)) + 2*svgRasterPad, int(math32.Ceil(s.H*sy)) + 2*svgRasterPad
}

// svgRasterize renders s at sx×sy device pixels per unit into straight
// (non-premultiplied) RGBA, each shape's fill then stroke composited over
// the ones before in its own colour.
func svgRasterize(s *SVG, sx, sy float32) *image.NRGBA {
	w, h := svgRasterSize(s, sx, sy)
	if w <= 2*svgRasterPad || h <= 2*svgRasterPad {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	z := vector.NewRasterizer(w, h)
	z.DrawOp = draw.Over
	pt := func(p core.Vec2) core.Vec2 {
		return core.Vec2{X: (p.X+s.offX)*sx + svgRasterPad, Y: (p.Y+s.offY)*sy + svgRasterPad}
	}
	paint := func(c RGBA) {
		z.Draw(dst, dst.Bounds(), image.NewUniform(premul(c)), image.Point{})
		z.Reset(w, h)
		z.DrawOp = draw.Over
	}
	var mask *strokeMask // one sample grid, reused shape to shape
	for i := range s.shapes {
		sh := &s.shapes[i]
		if mask == nil && sh.hasStroke {
			mask = newStrokeMask(w, h)
		}
		if sh.hasFill && sh.fill.A > 0 {
			for _, poly := range sh.path.subs {
				if len(poly) < 3 {
					continue
				}
				z.MoveTo(pt(poly[0]).X, pt(poly[0]).Y)
				for _, p := range poly[1:] {
					q := pt(p)
					z.LineTo(q.X, q.Y)
				}
				z.ClosePath()
			}
			paint(sh.fill)
		}
		if sh.hasStroke && sh.strokeW > 0 && sh.stroke.A > 0 {
			// The exact stroke width and positions, as a browser draws
			// them: a 2-unit stroke at 18 px is 1.5 px of coverage, not
			// rounded to one or two whole pixels and snapped to the grid.
			// Fitting the strokes to pixels (SVG.Hinted, still honoured
			// by the tessellated path) changed each glyph's weight with
			// its size — thin at 14–18 px, heavy from 20 — squared off
			// small curves, and was further from Chrome's rendering than
			// the exact geometry in every size compared.
			hw := sh.strokeW * math32.Sqrt(sx*sy) / 2
			for si, poly := range sh.path.subs {
				pts := make([]core.Vec2, 0, len(poly)+1)
				for _, p := range poly {
					q := pt(p)
					if n := len(pts); n > 0 && pts[n-1] == q {
						continue
					}
					pts = append(pts, q)
				}
				pts = simplifyPolyline(pts, 0.02)
				closed := sh.path.closed[si] && len(pts) > 2
				if closed && pts[0] == pts[len(pts)-1] {
					pts = pts[:len(pts)-1]
				}
				strokeOutline(mask, pts, hw, closed, sh.round)
			}
			if a, r := mask.take(); !r.Empty() {
				draw.DrawMask(dst, r, image.NewUniform(premul(sh.stroke)), image.Point{}, a, r.Min, draw.Over)
			}
		}
	}
	return unpremul(dst)
}

// A stroke is rasterized as the UNION of its pieces — a quad per segment
// and a disc or miter at each joint and round end — by sampling a
// svgSS×svgSS grid in every pixel and counting the samples any piece
// covers. Summing each piece's own area coverage instead (what the
// non-zero rasterizer does with overlapping polygons) double-counts every
// edge pixel two pieces share: a joint's disc over its segments' quads, a
// brick joint's cap over its course; those pixels came out a full step
// brighter than a browser draws them, and a whole icon about 15% heavier.
const svgSS = 16

// strokeMask is one stroke's coverage at svgSS² samples per pixel.
type strokeMask struct {
	w, h int     // pixels
	sw   int     // samples per row
	bits []uint8 // sw × h*svgSS, 1 where covered
	// The samples touched since the last take, inclusive (x0 > x1 when
	// none): take reads and clears only those.
	x0, y0, x1, y1 int
}

func newStrokeMask(w, h int) *strokeMask {
	return &strokeMask{w: w, h: h, sw: w * svgSS, bits: make([]uint8, w*svgSS*h*svgSS), x0: 1, x1: 0}
}

// touch notes that samples i0..i1 of row j may have been set.
func (m *strokeMask) touch(j, i0, i1 int) {
	if i0 > i1 {
		return
	}
	if m.x0 > m.x1 {
		m.x0, m.y0, m.x1, m.y1 = i0, j, i1, j
		return
	}
	m.x0, m.y0, m.x1, m.y1 = min(m.x0, i0), min(m.y0, j), max(m.x1, i1), max(m.y1, j)
}

// cover marks the samples inside the piece bounded by [x0,x1]×[y0,y1]
// (pixels) that in reports as inside. The samples of a pixel are a
// sheared 16×16 grid: sample (i, j) sits at i/16 + (j+½)/256 across and
// j/16 + (i+½)/256 down, so no two share a column or a row and an
// axis-aligned edge — most of an icon — still lands on one of 256
// coverage levels, not 16 (a 1.17 px stroke came out 1.12).
func (m *strokeMask) cover(x0, y0, x1, y1 float32, in func(x, y float32) bool) {
	const k = svgSS
	i0, i1 := max(0, int(math32.Floor(x0*k))-1), min(m.sw-1, int(math32.Ceil(x1*k)))
	j0, j1 := max(0, int(math32.Floor(y0*k))-1), min(m.h*k-1, int(math32.Ceil(y1*k)))
	for j := j0; j <= j1; j++ {
		m.touch(j, i0, i1)
		row := m.bits[j*m.sw:]
		dx := (float32(j%k) + 0.5) / (k * k)
		for i := i0; i <= i1; i++ {
			if row[i] == 0 && in(float32(i)/k+dx, float32(j)/k+(float32(i%k)+0.5)/(k*k)) {
				row[i] = 1
			}
		}
	}
}

// capsule covers every point within r of the segment a–b (a disc when
// a == b): a segment's body with round ends, which also makes its joints
// round. A row of samples spans 1/16 px down (the grid's shear), within
// h = 1/32 px of its middle line: samples where the middle line crosses
// the capsule shrunk by h are inside for certain, those outside its
// crossing of the capsule grown by h are outside, and only the few
// between are tested one by one.
func (m *strokeMask) capsule(a, b core.Vec2, r float32) {
	const k = svgSS
	const h = 0.5 / k
	c := newCapsule(a, b)
	r2 := r * r
	j0 := max(0, int(math32.Floor((min(a.Y, b.Y)-r)*k))-1)
	j1 := min(m.h*k-1, int(math32.Ceil((max(a.Y, b.Y)+r)*k)))
	for j := j0; j <= j1; j++ {
		ymid := float32(j)/k + h
		ol, or, ok := c.span(r+h, ymid)
		if !ok {
			continue
		}
		row := m.bits[j*m.sw:]
		off := (float32(j%k) + 0.5) / (k * k)
		i0 := max(0, int(math32.Floor((ol-off)*k)))
		i1 := min(m.sw-1, int(math32.Ceil((or-off)*k)))
		m.touch(j, i0, i1)
		// [ia, ib]: the samples inside for certain, set without a test.
		ia, ib := i1+1, i1
		if il, ir, ok := c.span(r-h, ymid); ok {
			ia = max(i0, int(math32.Ceil((il-off)*k)))
			ib = min(i1, int(math32.Floor((ir-off)*k)))
		}
		test := func(i int) {
			if row[i] == 0 && c.dist2(float32(i)/k+off, float32(j)/k+(float32(i%k)+0.5)/(k*k)) <= r2 {
				row[i] = 1
			}
		}
		for i := i0; i <= i1 && i < ia; i++ {
			test(i)
		}
		for i := ia; i <= ib; i++ {
			row[i] = 1
		}
		for i := max(i0, ib+1); i <= i1; i++ {
			test(i)
		}
	}
}

// capsuleSeg is a segment set up for distance and crossing queries.
type capsuleSeg struct {
	a, b   core.Vec2
	dx, dy float32 // b − a
	l2, l  float32 // its length, squared and not
	ux, uy float32 // its direction (zero for a point)
}

func newCapsule(a, b core.Vec2) capsuleSeg {
	c := capsuleSeg{a: a, b: b, dx: b.X - a.X, dy: b.Y - a.Y}
	c.l2 = c.dx*c.dx + c.dy*c.dy
	if c.l = math32.Sqrt(c.l2); c.l > 0 {
		c.ux, c.uy = c.dx/c.l, c.dy/c.l
	}
	return c
}

// dist2 is the squared distance from (x, y) to the segment.
func (c *capsuleSeg) dist2(x, y float32) float32 {
	px, py := x-c.a.X, y-c.a.Y
	t := float32(0)
	if c.l2 > 0 {
		t = clamp01f((px*c.dx + py*c.dy) / c.l2)
	}
	ex, ey := px-t*c.dx, py-t*c.dy
	return ex*ex + ey*ey
}

// span is the x interval where the line at height y crosses the capsule
// of radius r around the segment: the union of its crossings of the two
// end discs and of the body between them (one interval — it is convex).
func (c *capsuleSeg) span(r, y float32) (lo, hi float32, ok bool) {
	if r <= 0 {
		return 0, 0, false
	}
	lo, hi = math32.Inf(1), math32.Inf(-1)
	if d := r*r - (y-c.a.Y)*(y-c.a.Y); d >= 0 {
		q := math32.Sqrt(d)
		lo, hi = c.a.X-q, c.a.X+q
	}
	if d := r*r - (y-c.b.Y)*(y-c.b.Y); d >= 0 {
		q := math32.Sqrt(d)
		lo, hi = min(lo, c.b.X-q), max(hi, c.b.X+q)
	}
	if c.l == 0 {
		return lo, hi, lo <= hi
	}
	// The body: 0 ≤ (p−a)·u ≤ l and |(p−a)·n| ≤ r for p = (x, y) and
	// n = (−uy, ux); each is a band, an interval in x.
	ry := y - c.a.Y
	bl, bh := math32.Inf(-1), math32.Inf(1)
	if c.ux != 0 { // along: ux·(x−ax) + uy·ry ∈ [0, l]
		x1, x2 := (0-c.uy*ry)/c.ux, (c.l-c.uy*ry)/c.ux
		bl, bh = max(bl, min(x1, x2)), min(bh, max(x1, x2))
	} else if v := c.uy * ry; v < 0 || v > c.l {
		return lo, hi, lo <= hi
	}
	if c.uy != 0 { // across: −uy·(x−ax) + ux·ry ∈ [−r, r]
		x1, x2 := (-r-c.ux*ry)/-c.uy, (r-c.ux*ry)/-c.uy
		bl, bh = max(bl, min(x1, x2)), min(bh, max(x1, x2))
	} else if v := c.ux * ry; v < -r || v > r {
		return lo, hi, lo <= hi
	}
	if bl <= bh {
		lo, hi = min(lo, c.a.X+bl), max(hi, c.a.X+bh)
	}
	return lo, hi, lo <= hi
}

// convex covers a convex polygon given in either winding.
func (m *strokeMask) convex(poly []core.Vec2) {
	if len(poly) < 3 {
		return
	}
	var area float32
	x0, y0, x1, y1 := poly[0].X, poly[0].Y, poly[0].X, poly[0].Y
	for i, p := range poly {
		q := poly[(i+1)%len(poly)]
		area += p.X*q.Y - q.X*p.Y
		x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
	}
	if area == 0 {
		return
	}
	sign := float32(1)
	if area < 0 {
		sign = -1
	}
	m.cover(x0, y0, x1, y1, func(x, y float32) bool {
		for i, p := range poly {
			q := poly[(i+1)%len(poly)]
			if sign*((q.X-p.X)*(y-p.Y)-(q.Y-p.Y)*(x-p.X)) < 0 {
				return false
			}
		}
		return true
	})
}

// take returns the mask's per-pixel coverage — the share of each pixel's
// samples set — and the pixels it touched, and clears it for the next
// stroke. Only the touched pixels are read, cleared or composited: an
// icon is a dozen small strokes, and a whole-raster pass for each was
// most of the time it took.
func (m *strokeMask) take() (*image.Alpha, image.Rectangle) {
	if m.x0 > m.x1 {
		return nil, image.Rectangle{}
	}
	r := image.Rect(m.x0/svgSS, m.y0/svgSS, m.x1/svgSS+1, m.y1/svgSS+1)
	out := image.NewAlpha(image.Rect(0, 0, m.w, m.h))
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			n := 0
			for j := range svgSS {
				row := m.bits[(py*svgSS+j)*m.sw+px*svgSS:]
				// Each sample is a 0 or 1 byte (svgSS = 16 of them, two
				// words): a word's set bits count eight samples at once.
				n += bits.OnesCount64(binary.LittleEndian.Uint64(row)) +
					bits.OnesCount64(binary.LittleEndian.Uint64(row[8:]))
				clear(row[:svgSS])
			}
			out.Pix[py*out.Stride+px] = uint8((n*255 + svgSS*svgSS/2) / (svgSS * svgSS))
		}
	}
	m.x0, m.x1 = 1, 0
	return out, r
}


// simplifyPolyline drops the points of a flattened curve that lie within
// tol pixels of the chord past them (Douglas–Peucker), keeping both ends.
// Curves are flattened for any size — a quarter of a 2-unit corner arc is
// sixteen points — and at icon size most are a few hundredths of a pixel
// apart; each costs a capsule.
func simplifyPolyline(pts []core.Vec2, tol float32) []core.Vec2 {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	var rec func(i, j int)
	rec = func(i, j int) {
		a, b := pts[i], pts[j]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math32.Sqrt(dx*dx + dy*dy)
		far, at := float32(0), -1
		for k := i + 1; k < j; k++ {
			px, py := pts[k].X-a.X, pts[k].Y-a.Y
			var d float32
			if l == 0 {
				d = math32.Sqrt(px*px + py*py)
			} else if t := (px*dx + py*dy) / (l * l); t < 0 || t > 1 {
				d = math32.Sqrt(min(px*px+py*py, (px-dx)*(px-dx)+(py-dy)*(py-dy)))
			} else {
				d = math32.Abs(px*dy-py*dx) / l
			}
			if d > far {
				far, at = d, k
			}
		}
		if at >= 0 && far > tol {
			keep[at] = true
			rec(i, at)
			rec(at, j)
		}
	}
	rec(0, len(pts)-1)
	out := pts[:0:0]
	for k, p := range pts {
		if keep[k] {
			out = append(out, p)
		}
	}
	return out
}

// strokeOutline adds a polyline's stroke to m: for round joins and caps a
// capsule per segment (a disc for a lone point), otherwise a quad per
// segment and at each joint a miter — beveled past a limit of 4 — with
// butt ends.
func strokeOutline(m *strokeMask, pts []core.Vec2, hw float32, closed, round bool) {
	n := len(pts)
	if n == 0 {
		return
	}
	if n == 1 {
		if round {
			m.capsule(pts[0], pts[0], hw)
		}
		return
	}
	segs := n - 1
	if closed {
		segs = n
	}
	if round {
		for i := range segs {
			m.capsule(pts[i], pts[(i+1)%n], hw)
		}
		return
	}
	normal := func(i int) core.Vec2 { // segment i: pts[i] → pts[i+1]
		a, b := pts[i], pts[(i+1)%n]
		d := core.Vec2{X: b.X - a.X, Y: b.Y - a.Y}.Normalize()
		return core.Vec2{X: -d.Y * hw, Y: d.X * hw}
	}
	for i := range segs {
		a, b, nm := pts[i], pts[(i+1)%n], normal(i)
		m.convex([]core.Vec2{
			{X: a.X + nm.X, Y: a.Y + nm.Y}, {X: b.X + nm.X, Y: b.Y + nm.Y},
			{X: b.X - nm.X, Y: b.Y - nm.Y}, {X: a.X - nm.X, Y: a.Y - nm.Y},
		})
	}
	for i := range n {
		if !closed && (i == 0 || i == n-1) {
			continue // butt cap
		}
		n1, n2 := normal((i-1+segs)%segs), normal(i%segs)
		p := pts[i]
		for _, side := range [2]float32{1, -1} {
			a := core.Vec2{X: p.X + n1.X*side, Y: p.Y + n1.Y*side}
			b := core.Vec2{X: p.X + n2.X*side, Y: p.Y + n2.Y*side}
			mv := core.Vec2{X: n1.X + n2.X, Y: n1.Y + n2.Y}
			ml := mv.Length()
			cos := ml / (2 * hw) // cos of half the turn
			if ml < 1e-6 || 1/cos > 4 {
				m.convex([]core.Vec2{p, a, b})
				continue
			}
			k := hw / cos / ml * side
			m.convex([]core.Vec2{p, a, {X: p.X + mv.X*k, Y: p.Y + mv.Y*k}, b})
		}
	}
}

func premul(c RGBA) color.RGBA64 {
	a := clamp01f(c.A)
	return color.RGBA64{
		R: uint16(clamp01f(c.R)*a*0xffff + 0.5), G: uint16(clamp01f(c.G)*a*0xffff + 0.5),
		B: uint16(clamp01f(c.B)*a*0xffff + 0.5), A: uint16(a*0xffff + 0.5),
	}
}

func clamp01f(v float32) float32 { return max(0, min(1, v)) }

// unpremul converts the composited raster to the straight alpha the 2D
// pipeline's images hold.
func unpremul(src *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := src.Pix[i+3]
		if a == 0 {
			continue
		}
		for c := range 3 {
			out.Pix[i+c] = uint8(min(255, (uint32(src.Pix[i+c])*255+uint32(a)/2)/uint32(a)))
		}
		out.Pix[i+3] = a
	}
	return out
}
