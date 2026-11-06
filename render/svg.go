package render

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// SVG is a parsed vector image: a flat list of filled/stroked shapes in the
// document's coordinate space (GPUI svg / icon rendering). Subset: <path>
// (M,L,H,V,C,Q,S,T,Z, absolute + relative), <rect>, <circle>, <line>,
// <polygon>, <polyline>, with fill/stroke/stroke-width and <g> grouping
// (style inheritance; transforms beyond the root viewBox are not applied).
type SVG struct {
	W, H float32
	// dispW/dispH are the document's DECLARED size — its width/height
	// attributes ("48px"), which is what a page means by an image's
	// intrinsic size. They differ from W/H, the viewBox the paths are drawn
	// in: a Material icon declares 48x48 over a 960-unit viewBox, and
	// sizing an icon by the viewBox paints it across the whole page.
	dispW, dispH float32
	// offX/offY translate path coordinates into the 0..W/0..H box —
	// viewBox min-x/min-y negated (Google Material icons use
	// viewBox="0 -960 960 960").
	offX, offY float32
	shapes     []svgShape
}

type svgShape struct {
	path      *Path
	fill      RGBA
	hasFill   bool
	stroke    RGBA
	strokeW   float32
	hasStroke bool
	round     bool     // stroke-linecap/linejoin = round
	rule      FillRule // fill-rule; SVG's default is nonzero
	dash      []float32
	dashOff   float32
	// tris is the shape's fill TRIANGULATED ONCE, in the document's own
	// coordinates. Ear clipping is by far the most expensive thing an SVG
	// does — re-triangulating three toolbar icons was 79% of a live frame
	// on the buttons page — and it does not depend on where the icon is
	// drawn: an affine transform maps a triangulation to the transformed
	// polygon's triangulation, so the draw only has to move the vertices.
	tris []core.Vec2
}

type svgStyle struct {
	fill, stroke       RGBA
	hasFill, hasStroke bool
	strokeW            float32
	round              bool
	rule               FillRule
	dash               []float32
	dashOff            float32
}

// ParseSVG parses an SVG document.
func ParseSVG(data []byte) (*SVG, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	s := &SVG{}
	// SVG default: black fill, no stroke, stroke-width 1.
	stack := []svgStyle{{fill: RGBA{A: 1}, hasFill: true, strokeW: 1, rule: NonZero}}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			st := mergeStyle(stack[len(stack)-1], t.Attr)
			switch t.Name.Local {
			case "svg":
				s.W, s.H, s.offX, s.offY, s.dispW, s.dispH = svgSize(t.Attr)
				stack = append(stack, st)
			case "g":
				stack = append(stack, st)
			default:
				if p := shapePath(t, svgFlatness(s)); p != nil {
					s.shapes = append(s.shapes, styledShape(p, st))
				}
				stack = append(stack, st) // shapes can nest (rare) — keep balanced
			}
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(s.shapes) == 0 {
		return nil, fmt.Errorf("render: SVG has no supported shapes")
	}
	if s.W == 0 {
		s.W, s.H = 24, 24
	}
	return s, nil
}

// svgFlatness is the chord error curves are flattened to, in viewBox
// units: a thousandth of the document's own extent. A Material icon's
// 960-unit box gives about one unit, which is a fortieth of a pixel at
// the 24px these are actually drawn at — and a couple of hundred points
// per icon instead of a couple of thousand.
func svgFlatness(s *SVG) float32 {
	ext := max(s.W, s.H)
	if ext <= 0 {
		ext = 24 // no viewBox yet: the default box ParseSVG falls back to
	}
	return ext / 1000
}

func styledShape(p *Path, st svgStyle) svgShape {
	p.flushSub() // open (un-Closed) subpaths must reach p.subs for DrawSVG
	return svgShape{path: p, fill: st.fill, hasFill: st.hasFill,
		stroke: st.stroke, strokeW: st.strokeW, hasStroke: st.hasStroke, round: st.round,
		rule: st.rule, dash: st.dash, dashOff: st.dashOff}
}

// Size returns the coordinate space the paths are drawn in (the viewBox
// when the document declares one).
func (s *SVG) Size() (w, h float32) { return s.W, s.H }

// IntrinsicSize returns the size the document says it should be DRAWN at —
// its width/height attributes, or the viewBox when it states none. This is
// the size QML reports as an Image's sourceSize.
func (s *SVG) IntrinsicSize() (w, h float32) {
	if s.dispW > 0 && s.dispH > 0 {
		return s.dispW, s.dispH
	}
	return s.W, s.H
}

// DrawSVG renders s at (x,y), uniformly scaled, tinted by c (White = none).
func (r *Renderer2D) DrawSVG(s *SVG, x, y, scale float32, c RGBA) {
	r.DrawSVGRotated(s, x, y, scale, 0, c)
}

// DrawSVGRotated is DrawSVG rotated by angleRad around the drawn image's
// center (QML rotation / GPUI with_transformation). Paths are already
// transformed CPU-side per draw, so rotation composes with the scale free.
func (r *Renderer2D) DrawSVGRotated(s *SVG, x, y, scale, angleRad float32, c RGBA) {
	r.drawSVG(s, x, y, scale, scale, angleRad, c, mulColor)
}

// DrawSVGScaled scales the two axes INDEPENDENTLY — what an
// `Image { fillMode: Image.Stretch }` does to a vector source, which the
// uniform scale cannot express (it letterboxes instead).
func (r *Renderer2D) DrawSVGScaled(s *SVG, x, y, sx, sy, angleRad float32, c RGBA) {
	r.drawSVG(s, x, y, sx, sy, angleRad, c, mulColor)
}

// DrawSVGColorized draws s recoloured to c at each shape's own luminance,
// keeping its alpha — QML's `MultiEffect { colorization: 1 }`, how a
// Controls style tints an icon. A multiply (DrawSVG) can only darken, so a
// coloured icon comes out muddy instead of recoloured.
func (r *Renderer2D) DrawSVGColorized(s *SVG, x, y, scale, angleRad float32, c RGBA) {
	r.drawSVG(s, x, y, scale, scale, angleRad, c, lumColor)
}

// DrawSVGScaledColorized is DrawSVGScaled with a colorization.
func (r *Renderer2D) DrawSVGScaledColorized(s *SVG, x, y, sx, sy, angleRad float32, c RGBA) {
	r.drawSVG(s, x, y, sx, sy, angleRad, c, lumColor)
}

// drawSVG walks the shapes, mapping every shape colour through mix (a
// multiply, or a colorization).
func (r *Renderer2D) drawSVG(s *SVG, x, y, sx, sy, angleRad float32, c RGBA, mix func(a, b RGBA) RGBA) {
	cx, cy := x+s.W*sx/2, y+s.H*sy/2
	sin, cos := math32.Sincos(angleRad)
	xf := func(px, py float32) (float32, float32) {
		X, Y := x+(px+s.offX)*sx, y+(py+s.offY)*sy
		if angleRad == 0 {
			return X, Y
		}
		dx, dy := X-cx, Y-cy
		return cx + dx*cos - dy*sin, cy + dx*sin + dy*cos
	}
	// A stroke has ONE width: under a non-uniform scale take the geometric
	// mean, the way a renderer approximates a scaled pen.
	scale := math32.Sqrt(max(sx*sy, 0))
	for i := range s.shapes {
		sh := &s.shapes[i]
		// The fill: cached triangles, transformed straight into the
		// batch. Only the STROKE still needs the transformed path.
		if sh.hasFill {
			if sh.tris == nil {
				lp := NewPath()
				for si, poly := range sh.path.subs {
					for j, pt := range poly {
						if j == 0 {
							lp.MoveTo(pt.X, pt.Y)
						} else {
							lp.LineTo(pt.X, pt.Y)
						}
					}
					if sh.path.closed[si] {
						lp.Close()
					}
				}
				lp.flushSub()
				sh.tris = triangulateRule(lp.subs, sh.rule, nil)
				if sh.tris == nil {
					sh.tris = []core.Vec2{} // triangulated to nothing; do not retry
				}
			}
			if angleRad == 0 {
				r.fillTrisAffine(sh.tris, mix(sh.fill, c),
					x+s.offX*sx, y+s.offY*sy, sx, sy)
			} else {
				r.fillTrisXF(sh.tris, mix(sh.fill, c), xf)
			}
		}
		if !sh.hasStroke || sh.strokeW <= 0 {
			continue
		}
		tp := NewPath()
		for si, poly := range sh.path.subs {
			for j, pt := range poly {
				X, Y := xf(pt.X, pt.Y)
				if j == 0 {
					tp.MoveTo(X, Y)
				} else {
					tp.LineTo(X, Y)
				}
			}
			if sh.path.closed[si] {
				tp.Close()
			}
		}
		{
			if len(sh.dash) > 0 {
				// stroke-dasharray is in user units, so it scales with the
				// drawing like the geometry does.
				pat := make([]float32, len(sh.dash))
				for i, d := range sh.dash {
					pat[i] = d * scale
				}
				r.StrokePathDashed(tp, sh.strokeW*scale, mix(sh.stroke, c),
					pat, sh.dashOff*scale, sh.round)
				continue
			}
			if sh.round {
				r.StrokePathRound(tp, sh.strokeW*scale, mix(sh.stroke, c))
			} else {
				r.StrokePath(tp, sh.strokeW*scale, mix(sh.stroke, c))
			}
		}
	}
}

func mulColor(a, b RGBA) RGBA {
	return RGBA{a.R * b.R, a.G * b.G, a.B * b.B, a.A * b.A}
}

// lumColor takes b's hue at a's luminance, keeping both alphas — the
// colorization counterpart of mulColor.
func lumColor(a, b RGBA) RGBA {
	lum := 0.2126*a.R + 0.7152*a.G + 0.0722*a.B
	return RGBA{b.R * lum, b.G * lum, b.B * lum, a.A * b.A}
}

func svgSize(attr []xml.Attr) (w, h, offX, offY, dispW, dispH float32) {
	var vb string
	for _, a := range attr {
		switch a.Name.Local {
		case "width":
			w = parseLen(a.Value)
		case "height":
			h = parseLen(a.Value)
		case "viewBox":
			vb = a.Value
		}
	}
	// The declared size, before the viewBox replaces it below.
	dispW, dispH = w, h
	// Path coordinates live in the viewBox space, so when one is declared it
	// defines the document size and origin — the width/height attributes only
	// set the DISPLAY size, which DrawSVG's scale parameter handles.
	if vb != "" {
		f := strings.Fields(vb)
		if len(f) == 4 {
			offX, offY = -parseLen(f[0]), -parseLen(f[1])
			w = parseLen(f[2])
			h = parseLen(f[3])
		}
	}
	return
}

func parseLen(s string) float32 {
	s = strings.TrimRight(s, "px")
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 32)
	return float32(v)
}

// parseLenList reads an SVG length list ("4 2", "4,2", "4, 2"): the form
// stroke-dasharray takes.
func parseLenList(s string) []float32 {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]float32, 0, len(fields))
	for _, f := range fields {
		out = append(out, parseLen(f))
	}
	return out
}

func mergeStyle(parent svgStyle, attr []xml.Attr) svgStyle {
	st := parent
	apply := func(k, v string) {
		switch k {
		case "fill":
			if v == "none" {
				st.hasFill = false
			} else if c, ok := svgColor(v); ok {
				st.fill, st.hasFill = c, true
			}
		case "stroke":
			if v == "none" {
				st.hasStroke = false
			} else if c, ok := svgColor(v); ok {
				st.stroke, st.hasStroke = c, true
			}
		case "stroke-width":
			st.strokeW = parseLen(v)
		case "stroke-linecap", "stroke-linejoin":
			if v == "round" {
				st.round = true
			}
		case "stroke-dasharray":
			if v == "none" {
				st.dash = nil
			} else {
				st.dash = parseLenList(v)
			}
		case "stroke-dashoffset":
			st.dashOff = parseLen(v)
		case "fill-rule", "clip-rule":
			// SVG's default is nonzero; evenodd is the one worth spelling.
			if strings.Contains(v, "even") {
				st.rule = EvenOdd
			} else {
				st.rule = NonZero
			}
		}
	}
	for _, a := range attr {
		if a.Name.Local == "style" {
			for _, decl := range strings.Split(a.Value, ";") {
				kv := strings.SplitN(decl, ":", 2)
				if len(kv) == 2 {
					apply(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
				}
			}
			continue
		}
		apply(a.Name.Local, a.Value)
	}
	return st
}

var svgNamed = map[string]uint32{
	"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000,
	"blue": 0x0000ff, "yellow": 0xffff00, "gray": 0x808080, "grey": 0x808080,
	"orange": 0xffa500, "purple": 0x800080, "none": 0,
}

func svgColor(s string) (RGBA, bool) {
	s = strings.TrimSpace(s)
	// currentColor inherits the DrawSVG tint: White here, multiplied by c later.
	if s == "currentColor" {
		return White, true
	}
	if hx, ok := svgNamed[strings.ToLower(s)]; ok && s != "none" {
		return rgbFromHex(hx), true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 { // #rgb -> #rrggbb
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) == 6 {
			v, err := strconv.ParseUint(h, 16, 32)
			if err == nil {
				return rgbFromHex(uint32(v)), true
			}
		}
	}
	return RGBA{}, false
}

func rgbFromHex(h uint32) RGBA {
	return RGBA{float32(h>>16&0xff) / 255, float32(h>>8&0xff) / 255, float32(h&0xff) / 255, 1}
}

// shapePath builds a Path for a single SVG shape element (nil if
// unsupported).
func shapePath(t xml.StartElement, tol float32) *Path {
	attr := func(name string) string {
		for _, a := range t.Attr {
			if a.Name.Local == name {
				return a.Value
			}
		}
		return ""
	}
	switch t.Name.Local {
	case "path":
		return parsePathD(attr("d"), tol)
	case "rect":
		x, y := parseLen(attr("x")), parseLen(attr("y"))
		w, h := parseLen(attr("width")), parseLen(attr("height"))
		return NewPath().MoveTo(x, y).LineTo(x+w, y).LineTo(x+w, y+h).LineTo(x, y+h).Close()
	case "circle":
		return ellipsePath(parseLen(attr("cx")), parseLen(attr("cy")), parseLen(attr("r")), parseLen(attr("r")))
	case "ellipse":
		return ellipsePath(parseLen(attr("cx")), parseLen(attr("cy")), parseLen(attr("rx")), parseLen(attr("ry")))
	case "line":
		return NewPath().MoveTo(parseLen(attr("x1")), parseLen(attr("y1"))).
			LineTo(parseLen(attr("x2")), parseLen(attr("y2")))
	case "polygon", "polyline":
		nums := parseNums(attr("points"))
		if len(nums) < 4 {
			return nil
		}
		p := NewPath().MoveTo(nums[0], nums[1])
		for i := 2; i+1 < len(nums); i += 2 {
			p.LineTo(nums[i], nums[i+1])
		}
		if t.Name.Local == "polygon" {
			p.Close()
		}
		return p
	}
	return nil
}

func ellipsePath(cx, cy, rx, ry float32) *Path {
	p := NewPath()
	const seg = 32
	for i := 0; i <= seg; i++ {
		a := float32(i) / seg * 2 * math32.Pi
		x, y := cx+rx*math32.Cos(a), cy+ry*math32.Sin(a)
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	return p.Close()
}

func parseNums(s string) []float32 {
	var out []float32
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '\t'
	}) {
		if v, err := strconv.ParseFloat(f, 32); err == nil {
			out = append(out, float32(v))
		}
	}
	return out
}
