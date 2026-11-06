package msdf

import "math"

// Field is a generated multi-channel distance field: RGB, one byte a
// channel, row-major from the top.
//
// Zero distance encodes to 128. A byte therefore spans ±Range glyph
// units around the outline, which is what the shader needs to know to
// turn a sample back into a screen-space distance and antialias it.
type Field struct {
	W, H  int
	Pix   []byte  // 3 bytes a texel
	Range float64 // glyph units either side of the edge that the byte spans
	// The glyph-unit rectangle the field covers, which is the bounds
	// grown by the padding.
	MinX, MinY, MaxX, MaxY float64
}

// At returns the three channel bytes of a texel.
func (f *Field) At(x, y int) (r, g, b byte) {
	o := (y*f.W + x) * 3
	return f.Pix[o], f.Pix[o+1], f.Pix[o+2]
}

// Median is what the shader computes: the middle of the three channels,
// which is the reconstructed distance.
func Median(r, g, b byte) float64 {
	return float64(max8(min8(r, g), min8(max8(r, g), b)))
}

func min8(a, b byte) byte {
	if a < b {
		return a
	}
	return b
}

func max8(a, b byte) byte {
	if a > b {
		return a
	}
	return b
}

// Generate rasterises a shape into a field of w×h texels.
//
// pad is how many texels of margin to leave round the glyph, and it is
// not decoration: it is the distance the shader is allowed to read, so
// it sets how far an outline, a glow or a soft shadow can be pushed
// before it runs off the edge of the tile and clips.
func Generate(s Shape, w, h, pad int) *Field {
	f := &Field{W: w, H: h, Pix: make([]byte, w*h*3)}
	if len(s.Edges) == 0 || w <= 0 || h <= 0 {
		return f
	}
	// Fit the glyph into the tile less the padding, keeping its aspect.
	gw, gh := s.MaxX-s.MinX, s.MaxY-s.MinY
	iw, ih := float64(w-2*pad), float64(h-2*pad)
	scale := math.Min(iw/math.Max(gw, 1e-9), ih/math.Max(gh, 1e-9))
	// The rectangle of glyph space the tile covers, centred.
	cx, cy := (s.MinX+s.MaxX)/2, (s.MinY+s.MaxY)/2
	halfW, halfH := float64(w)/2/scale, float64(h)/2/scale
	f.MinX, f.MaxX = cx-halfW, cx+halfW
	f.MinY, f.MaxY = cy-halfH, cy+halfH
	// A byte spans the padding either side of the edge: that is the
	// most distance the tile can represent without clipping.
	f.Range = float64(pad) / scale
	fill(f, s, scale)
	return f
}

// GenerateTile fits the tile to the glyph rather than the glyph to the
// tile: one texel per glyph unit, in a box just large enough to hold the
// outline plus pad texels of margin.
//
// That is what an atlas wants. Generate's fitted scale differs per glyph
// — a wide 'm' and a narrow 'i' in the same 32x32 box are drawn at
// different scales — so the distance a byte spans differs per glyph too,
// and the shader would need a value per glyph to undo it. Here every
// glyph of a family shares the scale it was loaded at, the range is pad
// texels everywhere, and the shader carries one constant.
func GenerateTile(s Shape, pad int) *Field {
	if len(s.Edges) == 0 {
		return &Field{}
	}
	x0 := math.Floor(s.MinX) - float64(pad)
	y0 := math.Floor(s.MinY) - float64(pad)
	x1 := math.Ceil(s.MaxX) + float64(pad)
	y1 := math.Ceil(s.MaxY) + float64(pad)
	w, h := int(x1-x0), int(y1-y0)
	if w <= 0 || h <= 0 {
		return &Field{}
	}
	f := &Field{
		W: w, H: h, Pix: make([]byte, w*h*3),
		Range: float64(pad),
		MinX:  x0, MinY: y0, MaxX: x1, MaxY: y1,
	}
	fill(f, s, 1)
	return f
}

// fill samples every texel centre of a field whose glyph-space rectangle
// and range are already set.
func fill(f *Field, s Shape, scale float64) {
	for y := 0; y < f.H; y++ {
		// Texel centres. No vertical flip: x/image hands back outlines
		// in image convention, Y increasing downwards, because they are
		// meant to feed a rasteriser. Flipping them here as if they were
		// font-space (Y up) turns every glyph upside down, which is
		// invisible on an 'o' and obvious on an 'A'.
		py := f.MinY + (float64(y)+0.5)/scale
		for x := 0; x < f.W; x++ {
			px := f.MinX + (float64(x)+0.5)/scale
			ch, td := sample(s, point{px, py})
			// Error correction. Three channels each pick their own
			// nearest edge, and far from the outline — out in the
			// padding, where several edges are about equally distant —
			// two of them can pick edges whose planes disagree about
			// which side the point is on. The median then reports the
			// wrong sign and a speck of glyph appears in empty space,
			// or a notch in a stem.
			//
			// The true distance never has that problem: it is one
			// nearest edge and one winding sign. So wherever the median
			// contradicts it, the multi-channel reconstruction has
			// nothing to offer and the texel falls back to the plain
			// distance. Corners survive, because there the two planes
			// disagree about *where* the edge is, not about which side
			// of it the point is on.
			if (median3(ch) > 0) != (td > 0) {
				ch = [3]float64{td, td, td}
			}
			o := (y*f.W + x) * 3
			for c := 0; c < 3; c++ {
				f.Pix[o+c] = encode(ch[c], f.Range)
			}
		}
	}
}

func median3(d [3]float64) float64 {
	return math.Max(math.Min(d[0], d[1]), math.Min(math.Max(d[0], d[1]), d[2]))
}

// encode maps a signed distance to a byte, zero at 128.
func encode(d, rng float64) byte {
	v := d/(2*rng) + 0.5
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	}
	return byte(v*255 + 0.5)
}

// sample is the heart of the generator: the three channel distances at a
// point, and the plain single-channel distance beside them.
//
// The channel distance is a *pseudo*-distance, which is the whole trick.
// The true distance to a line segment clamps at its endpoints, so just
// beyond the end of an edge it becomes the distance to a point: a cone.
// A cone is what rounds a corner off, because a bilinear filter
// interpolates the apex of a cone into a dome, and that happens whether
// there is one channel or three.
//
// The pseudo-distance is the perpendicular distance to the edge's
// infinite line. Near a corner each of the two edges meeting there
// reports a *plane*, which interpolates exactly; the two sides land in
// different channels, and the median picks whichever is nearer. That
// reconstructs the point.
//
// Which edge to ask is still decided by true distance — a far-away edge
// whose line happens to pass close by must not win.
func sample(s Shape, p point) (ch [3]float64, trueDist float64) {
	var best, ortho [3]float64
	var idx [3]int
	bestAll, orthoAll, idxAll := math.Inf(1), 1.0, -1
	for c := range best {
		best[c], ortho[c], idx[c] = math.Inf(1), 1, -1
	}
	for i := range s.Edges {
		d, o := segDistance(p, s.Edges[i].A, s.Edges[i].B)
		if closer(d, o, bestAll, orthoAll) {
			bestAll, orthoAll, idxAll = d, o, i
		}
		for c := 0; c < 3; c++ {
			if s.Edges[i].Col.has(c) && closer(d, o, best[c], ortho[c]) {
				best[c], ortho[c], idx[c] = d, o, i
			}
		}
	}
	if idxAll < 0 {
		return [3]float64{}, 0
	}
	trueDist = signed(s, idxAll, p, bestAll)
	for c := 0; c < 3; c++ {
		i := idx[c]
		if i < 0 {
			// No edge of this colour at all — a contour with no corners
			// takes one colour, and then two channels have nothing. Fall
			// back to the whole outline so the channel still carries a
			// usable distance instead of a flat maximum.
			i = idxAll
		}
		e := s.Edges[i]
		ch[c] = signed(s, i, p, lineDistance(p, e.A, e.B))
	}
	return ch, trueDist
}

// closer decides which of two edges a point belongs to, and the second
// term is not a detail. At a convex corner the two edges meeting there
// are EXACTLY equidistant from any point beyond the tip, and they
// disagree about which side of the outline that point is on — one of
// them by 180 degrees. Taking whichever came first in the contour signs
// a wedge of empty space as if it were inside the glyph, which is how a
// speck of ink appears in the padding above a 'v'.
//
// The tie breaks on squareness: the edge the point lies most directly
// off the flank of is the one that owns it.
func closer(d, o, bestD, bestO float64) bool {
	const eps = 1e-6
	return d < bestD-eps || (d < bestD+eps && o < bestO)
}

// signed puts a magnitude on the side of an edge the point falls — per
// channel, not from one winding number shared across all three. The
// shared sign was the reason the corner barely improved: with every
// channel forced to the same side, the median can only choose between
// magnitudes, and reconstructing a corner needs two channels to disagree
// about which side of the outline the point is on at all.
func signed(s Shape, i int, p point, d float64) float64 {
	e := s.Edges[i]
	if s.Orient*cross(e.A, e.B, p) < 0 {
		return -d
	}
	return d
}

// lineDistance is the perpendicular distance to the infinite line
// through two points.
func lineDistance(p, a, b point) float64 {
	ab := b.sub(a)
	l := ab.len()
	if l < 1e-12 {
		return p.sub(a).len()
	}
	return math.Abs(cross(a, b, p)) / l
}

// cross is the z of (b-a)×(p-a): which side of the line p is on.
func cross(a, b, p point) float64 {
	return (b.X-a.X)*(p.Y-a.Y) - (b.Y-a.Y)*(p.X-a.X)
}

// segDistance is the distance from a point to a line segment, clamped
// at its ends, and how squarely the segment faces the point: 0 when the
// point is straight off its flank, up to 1 when it lies along the line
// beyond an end. It decides *which* edge is nearest; what gets stored is
// that edge's perpendicular distance.
func segDistance(p, a, b point) (d, ortho float64) {
	ab := b.sub(a)
	l2 := ab.dot(ab)
	if l2 < 1e-18 {
		return p.sub(a).len(), 1
	}
	t := p.sub(a).dot(ab) / l2
	switch {
	case t < 0:
		t = 0
	case t > 1:
		t = 1
	default:
		// The foot of the perpendicular is on the segment: as square as
		// it gets, and nothing to break a tie with.
		return p.sub(point{a.X + t*ab.X, a.Y + t*ab.Y}).len(), 0
	}
	v := p.sub(point{a.X + t*ab.X, a.Y + t*ab.Y})
	d = v.len()
	if d < 1e-12 {
		return d, 0
	}
	return d, math.Abs(v.dot(ab)) / (d * math.Sqrt(l2))
}
