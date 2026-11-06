package render

import (
	"math"
	"strconv"
)

// parsePathD parses an SVG path "d" attribute into a Path. Supports
// M/m L/l H/h V/v C/c S/s Q/q T/t Z/z (absolute and relative); arcs (A)
// and elliptical commands are not yet handled (they're skipped).
// ParsePathData parses an SVG path-data string ("M 0 0 L 10 10 Z") into a
// Path in its own coordinate space — QtQuick's PathSvg takes the same
// syntax. Nil when d is empty.
func ParsePathData(d string) *Path { return parsePathD(d, 0) }

func parsePathD(d string, tol float32) *Path {
	if d == "" {
		return nil
	}
	toks := tokenizeD(d)
	// The tolerance has to be set before any curve is flattened, which
	// happens as the commands are read (see Path.Flatness).
	p := NewPath().Flatness(tol)
	var cx, cy, sx, sy float32  // current point, subpath start
	var lastC, lastQ [2]float32 // last control points for smooth S/T
	var prevCmd byte
	i := 0
	num := func() float32 {
		if i >= len(toks) {
			return 0
		}
		v, _ := strconv.ParseFloat(toks[i], 32)
		i++
		return float32(v)
	}
	hasNum := func() bool {
		if i >= len(toks) {
			return false
		}
		_, err := strconv.ParseFloat(toks[i], 32)
		return err == nil
	}

	for i < len(toks) {
		cmd := toks[i][0]
		if _, err := strconv.ParseFloat(toks[i], 32); err == nil {
			// Implicit repeat of the previous command's letter.
			cmd = prevCmd
		} else {
			i++
		}
		rel := cmd >= 'a'
		switch cmd | 0x20 {
		case 'm':
			x, y := num(), num()
			if rel {
				x, y = cx+x, cy+y
			}
			p.MoveTo(x, y)
			cx, cy, sx, sy = x, y, x, y
			// Subsequent pairs after M are implicit L.
			for hasNum() {
				x, y := num(), num()
				if rel {
					x, y = cx+x, cy+y
				}
				p.LineTo(x, y)
				cx, cy = x, y
			}
		case 'l':
			x, y := num(), num()
			if rel {
				x, y = cx+x, cy+y
			}
			p.LineTo(x, y)
			cx, cy = x, y
		case 'h':
			x := num()
			if rel {
				x = cx + x
			}
			p.LineTo(x, cy)
			cx = x
		case 'v':
			y := num()
			if rel {
				y = cy + y
			}
			p.LineTo(cx, y)
			cy = y
		case 'c':
			c1x, c1y, c2x, c2y, x, y := num(), num(), num(), num(), num(), num()
			if rel {
				c1x, c1y, c2x, c2y, x, y = cx+c1x, cy+c1y, cx+c2x, cy+c2y, cx+x, cy+y
			}
			p.CubicTo(c1x, c1y, c2x, c2y, x, y)
			lastC = [2]float32{c2x, c2y}
			cx, cy = x, y
		case 's':
			c1x, c1y := cx, cy
			if prevCmd|0x20 == 'c' || prevCmd|0x20 == 's' {
				c1x, c1y = 2*cx-lastC[0], 2*cy-lastC[1] // reflect
			}
			c2x, c2y, x, y := num(), num(), num(), num()
			if rel {
				c2x, c2y, x, y = cx+c2x, cy+c2y, cx+x, cy+y
			}
			p.CubicTo(c1x, c1y, c2x, c2y, x, y)
			lastC = [2]float32{c2x, c2y}
			cx, cy = x, y
		case 'q':
			qx, qy, x, y := num(), num(), num(), num()
			if rel {
				qx, qy, x, y = cx+qx, cy+qy, cx+x, cy+y
			}
			quadTo(p, cx, cy, qx, qy, x, y)
			lastQ = [2]float32{qx, qy}
			cx, cy = x, y
		case 't':
			qx, qy := cx, cy
			if prevCmd|0x20 == 'q' || prevCmd|0x20 == 't' {
				qx, qy = 2*cx-lastQ[0], 2*cy-lastQ[1]
			}
			x, y := num(), num()
			if rel {
				x, y = cx+x, cy+y
			}
			quadTo(p, cx, cy, qx, qy, x, y)
			lastQ = [2]float32{qx, qy}
			cx, cy = x, y
		case 'z':
			p.Close()
			cx, cy = sx, sy
		case 'a':
			rx, ry := num(), num()
			xrot := num() * math.Pi / 180
			large := num() != 0
			sweep := num() != 0
			x, y := num(), num()
			if rel {
				x, y = cx+x, cy+y
			}
			arcTo(p, cx, cy, rx, ry, xrot, large, sweep, x, y)
			cx, cy = x, y
		default:
			return p // unsupported command: stop here
		}
		prevCmd = cmd
	}
	return p
}

// arcTo converts an SVG elliptical-arc segment (endpoint parameterization) to
// cubic Béziers and appends them. Implements the W3C SVG arc algorithm.
func arcTo(p *Path, x0, y0, rx, ry, phi float32, large, sweep bool, x, y float32) {
	if rx == 0 || ry == 0 || (x0 == x && y0 == y) {
		p.LineTo(x, y)
		return
	}
	rx, ry = float32(math.Abs(float64(rx))), float32(math.Abs(float64(ry)))
	cosp, sinp := float32(math.Cos(float64(phi))), float32(math.Sin(float64(phi)))
	// Step 1: midpoint in the rotated frame.
	dx, dy := (x0-x)/2, (y0-y)/2
	x1 := cosp*dx + sinp*dy
	y1 := -sinp*dx + cosp*dy
	// Correct radii if too small.
	lam := x1*x1/(rx*rx) + y1*y1/(ry*ry)
	if lam > 1 {
		s := float32(math.Sqrt(float64(lam)))
		rx, ry = rx*s, ry*s
	}
	// Step 2: center in the rotated frame.
	denom := rx*rx*y1*y1 + ry*ry*x1*x1
	num := rx*rx*ry*ry - denom
	if num < 0 {
		num = 0
	}
	co := float32(math.Sqrt(float64(num / denom)))
	if large == sweep {
		co = -co
	}
	cxp := co * rx * y1 / ry
	cyp := -co * ry * x1 / rx
	// Step 3: center in original frame.
	ccx := cosp*cxp - sinp*cyp + (x0+x)/2
	ccy := sinp*cxp + cosp*cyp + (y0+y)/2
	// Step 4: start/sweep angles.
	ang := func(ux, uy, vx, vy float32) float32 {
		dot := ux*vx + uy*vy
		ln := float32(math.Sqrt(float64((ux*ux + uy*uy) * (vx*vx + vy*vy))))
		a := float32(math.Acos(float64(clampf(dot/ln, -1, 1))))
		if ux*vy-uy*vx < 0 {
			a = -a
		}
		return a
	}
	t1 := ang(1, 0, (x1-cxp)/rx, (y1-cyp)/ry)
	dt := ang((x1-cxp)/rx, (y1-cyp)/ry, (-x1-cxp)/rx, (-y1-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	// Flatten into cubic segments (≤90° each).
	segs := int(math.Ceil(math.Abs(float64(dt)) / (math.Pi / 2)))
	if segs < 1 {
		segs = 1
	}
	delta := dt / float32(segs)
	tf := 4.0 / 3.0 * float32(math.Tan(float64(delta)/4))
	for s := 0; s < segs; s++ {
		a1 := t1 + float32(s)*delta
		a2 := a1 + delta
		c1, s1 := float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1)))
		c2, s2 := float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2)))
		// Points on the unit ellipse, rotated/translated to world.
		map_ := func(c, sn float32) (float32, float32) {
			ex, ey := rx*c, ry*sn
			return ccx + cosp*ex - sinp*ey, ccy + sinp*ex + cosp*ey
		}
		p0x, p0y := map_(c1, s1)
		p3x, p3y := map_(c2, s2)
		c1x := p0x + tf*(-(cosp*rx*s1)-sinp*ry*c1)
		c1y := p0y + tf*(-(sinp*rx*s1)+cosp*ry*c1)
		c2x := p3x - tf*(-(cosp*rx*s2)-sinp*ry*c2)
		c2y := p3y - tf*(-(sinp*rx*s2)+cosp*ry*c2)
		p.CubicTo(c1x, c1y, c2x, c2y, p3x, p3y)
	}
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// quadTo flattens a quadratic Bézier by elevating it to a cubic.
func quadTo(p *Path, x0, y0, qx, qy, x, y float32) {
	c1x := x0 + 2.0/3.0*(qx-x0)
	c1y := y0 + 2.0/3.0*(qy-y0)
	c2x := x + 2.0/3.0*(qx-x)
	c2y := y + 2.0/3.0*(qy-y)
	p.CubicTo(c1x, c1y, c2x, c2y, x, y)
}

// tokenizeD splits a path data string into command letters and numbers,
// handling comma/space separators and sign-glued numbers ("10-5", "1.2.3").
func tokenizeD(d string) []string {
	var out []string
	i := 0
	for i < len(d) {
		c := d[i]
		switch {
		case c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r':
			i++
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			out = append(out, string(c))
			i++
		default:
			// Number: optional sign, digits, dot, exponent.
			j := i
			if d[j] == '+' || d[j] == '-' {
				j++
			}
			seenDot := false
			for j < len(d) {
				ch := d[j]
				if ch >= '0' && ch <= '9' {
					j++
				} else if ch == '.' && !seenDot {
					seenDot = true
					j++
				} else if ch == 'e' || ch == 'E' {
					j++
					if j < len(d) && (d[j] == '+' || d[j] == '-') {
						j++
					}
				} else {
					break
				}
			}
			if j == i {
				i++ // unknown char, skip
				continue
			}
			out = append(out, d[i:j])
			i = j
		}
	}
	return out
}
