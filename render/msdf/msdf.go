// Package msdf builds multi-channel signed distance fields from glyph
// outlines.
//
// A signed distance field stores, per texel, how far that texel is from
// the nearest edge of the shape and on which side. Sampled with a
// hardware bilinear filter and thresholded at zero it reconstructs the
// shape at any size, so one small texture draws text at every scale and
// rotation — which is the point: the alternative, and what this
// framework does today, is a separate rasterised atlas per pixel size.
//
// A *single*-channel field rounds off corners, because the distance to a
// corner is a cone and a cone interpolates to a curve. Chlumský's trick
// is to keep three of them. Each edge of the outline is assigned two of
// the three colour channels, switching assignment at every sharp corner,
// so at a corner two channels disagree about where the edge is. Taking
// the median of the three recovers the corner exactly: the two channels
// that agree win everywhere except at the corner itself, where the third
// breaks the tie the right way.
//
// The distances here are computed against a flattened outline — every
// curve subdivided into line segments far finer than a texel. Analytic
// distance to a cubic is a quartic root-find per texel per edge and the
// error it saves is well under the quantisation of an 8-bit texture.
package msdf

import (
	"math"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Channel is the pair of colour channels an edge is drawn into. An edge
// always carries exactly two, so that the median of the three channels
// is the distance to the edge everywhere except at a corner.
type Channel uint8

// The three edge colours, named for the channels they light.
const (
	Magenta Channel = 0b101 // red + blue
	Yellow  Channel = 0b110 // red + green
	Cyan    Channel = 0b011 // green + blue
)

// rotate returns the next colour in the cycle, which is how the
// assignment alternates around a contour.
func (c Channel) rotate() Channel {
	switch c {
	case Magenta:
		return Yellow
	case Yellow:
		return Cyan
	default:
		return Magenta
	}
}

// has reports whether this colour lights a channel (0 = red).
func (c Channel) has(i int) bool { return c&(1<<uint(2-i)) != 0 }

// point is a position in glyph units.
type point struct{ X, Y float64 }

func (p point) sub(q point) point   { return point{p.X - q.X, p.Y - q.Y} }
func (p point) len() float64        { return math.Hypot(p.X, p.Y) }
func (p point) dot(q point) float64 { return p.X*q.X + p.Y*q.Y }
func (p point) norm() point {
	if l := p.len(); l > 1e-12 {
		return point{p.X / l, p.Y / l}
	}
	return point{}
}

// edge is one flattened line segment of the outline, with the colour it
// was assigned.
type edge struct {
	A, B point
	Col  Channel
}

// Shape is a glyph's outline, flattened and coloured, in glyph units.
type Shape struct {
	Edges []edge
	// Orient is +1 or -1: which side of an edge, by the sign of the
	// cross product, is the inside of the shape. Outlines do not agree
	// on a winding direction between formats — and within one glyph the
	// counters are wound the other way on purpose, which is what makes
	// the hole in an 'o' — so it is measured from the largest contour
	// rather than assumed.
	Orient float64
	// Bounds in glyph units.
	MinX, MinY, MaxX, MaxY float64
}

// FlattenTol is how far a flattened curve may stray from the true one,
// in glyph units at the loading ppem used by Load. A glyph is loaded at
// a high ppem so that this is comfortably sub-texel for any atlas tile
// worth making.
const FlattenTol = 0.05

// Load reads a glyph's outline and returns it flattened and coloured.
//
// ppem is the resolution the outline is loaded at — not the size it will
// be drawn at. A field is scale-free once it is built; the loading ppem
// only decides how much curve detail survives flattening, so it is
// chosen large and fixed.
func Load(f *sfnt.Font, b *sfnt.Buffer, r rune, ppem fixed.Int26_6, cornerCos float64) (Shape, bool) {
	gi, err := f.GlyphIndex(b, r)
	if err != nil || gi == 0 {
		return Shape{}, false
	}
	segs, err := f.LoadGlyph(b, gi, ppem, nil)
	if err != nil {
		return Shape{}, false
	}
	var contours [][]point
	var cur []point
	flush := func() {
		if len(cur) > 1 {
			contours = append(contours, cur)
		}
		cur = nil
	}
	pt := func(p fixed.Point26_6) point {
		return point{float64(p.X) / 64, float64(p.Y) / 64}
	}
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			flush()
			cur = []point{pt(s.Args[0])}
		case sfnt.SegmentOpLineTo:
			cur = append(cur, pt(s.Args[0]))
		case sfnt.SegmentOpQuadTo:
			cur = flattenQuad(cur, cur[len(cur)-1], pt(s.Args[0]), pt(s.Args[1]))
		case sfnt.SegmentOpCubeTo:
			cur = flattenCubic(cur, cur[len(cur)-1], pt(s.Args[0]), pt(s.Args[1]), pt(s.Args[2]))
		}
	}
	flush()
	if len(contours) == 0 {
		return Shape{}, false
	}
	return colourise(contours, cornerCos), true
}

// flattenQuad appends a quadratic Bézier as line segments.
func flattenQuad(out []point, p0, p1, p2 point) []point {
	// Steps from the control polygon's deviation: a quadratic is within
	// a quarter of |p0 - 2p1 + p2| of its chord, which bounds the error
	// of n even steps by that over n².
	d := point{p0.X - 2*p1.X + p2.X, p0.Y - 2*p1.Y + p2.Y}.len()
	n := steps(d / 4)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		out = append(out, point{
			u*u*p0.X + 2*u*t*p1.X + t*t*p2.X,
			u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y,
		})
	}
	return out
}

// flattenCubic appends a cubic Bézier as line segments.
func flattenCubic(out []point, p0, p1, p2, p3 point) []point {
	d := math.Max(
		point{p0.X - 2*p1.X + p2.X, p0.Y - 2*p1.Y + p2.Y}.len(),
		point{p1.X - 2*p2.X + p3.X, p1.Y - 2*p2.Y + p3.Y}.len())
	n := steps(d * 3 / 4)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		out = append(out, point{
			u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
			u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
		})
	}
	return out
}

// steps is how many line segments a curve of this deviation needs to
// stay within FlattenTol.
func steps(dev float64) int {
	if dev <= 0 {
		return 1
	}
	n := int(math.Ceil(math.Sqrt(dev / FlattenTol)))
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	return n
}

// DefaultCornerCos is the cosine of the angle past which a join counts
// as a corner: about 20°, so a smooth curve's joins stay one colour and
// the corners of an 'A' or an 'M' get three.
const DefaultCornerCos = 0.94

// colourise flattens contours into coloured edges.
//
// The colour changes at every corner and nowhere else, so along a smooth
// curve all three channels agree and the median is simply the distance.
// At a corner two channels carry one side and two the other, sharing
// one; the median then follows whichever side the texel is actually
// nearest, which is what keeps the corner sharp instead of rounding it
// into a cone.
//
// A contour with no corners at all — an 'o', a bowl — gets a single
// colour, because there is nothing to disambiguate and switching colour
// mid-curve would put a seam in it.
func colourise(contours [][]point, cornerCos float64) Shape {
	// A negative threshold means no join is ever a corner, so the whole
	// contour takes one colour: that is an ordinary single-channel
	// field, and the tests use it to show what the other two buy.
	oneColour := cornerCos < 0
	s := Shape{Orient: 1, MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
	// The outer contour is the biggest one, and its winding is the
	// shape's.
	biggest := 0.0
	for _, c := range contours {
		if a := signedArea(c); math.Abs(a) > math.Abs(biggest) {
			biggest = a
		}
	}
	if biggest < 0 {
		s.Orient = -1
	}
	for _, c := range contours {
		// Drop a repeated closing point: the contour is implicitly closed.
		if n := len(c); n > 1 && c[0].sub(c[n-1]).len() < 1e-9 {
			c = c[:n-1]
		}
		n := len(c)
		if n < 2 {
			continue
		}
		// Where the corners are.
		corner := make([]bool, n)
		any := false
		for i := range c {
			in := c[i].sub(c[(i-1+n)%n]).norm()
			out := c[(i+1)%n].sub(c[i]).norm()
			if !oneColour && in.dot(out) < cornerCos {
				corner[i], any = true, true
			}
		}
		col := Magenta
		if oneColour {
			col = Magenta | Yellow | Cyan
		}
		for i := range c {
			if any && corner[i] {
				col = col.rotate()
			}
			a, b := c[i], c[(i+1)%n]
			if a.sub(b).len() < 1e-9 {
				continue
			}
			s.Edges = append(s.Edges, edge{A: a, B: b, Col: col})
			for _, p := range [2]point{a, b} {
				s.MinX, s.MaxX = math.Min(s.MinX, p.X), math.Max(s.MaxX, p.X)
				s.MinY, s.MaxY = math.Min(s.MinY, p.Y), math.Max(s.MaxY, p.Y)
			}
		}
	}
	return s
}

// signedArea is twice the area of a closed polygon, negative for one
// winding and positive for the other.
func signedArea(c []point) float64 {
	a := 0.0
	for i := range c {
		p, q := c[i], c[(i+1)%len(c)]
		a += p.X*q.Y - q.X*p.Y
	}
	return a
}
