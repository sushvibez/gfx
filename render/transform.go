package render

// A 2D transform stack for the 2D renderer: everything queued while a
// transform is pushed is drawn through it. This is what makes a rotated or
// scaled item possible (QML's `rotation`, `scale` and `transform:` lists,
// CSS transforms) without every primitive growing its own angle argument.
//
// Vertex-based primitives (paths, text, images, lines) go through toNDC, so
// they transform exactly. The SDF quad primitive is different: the shader
// reconstructs a rounded rect from an axis-aligned pixel rect, so a quad
// carries its own rotation angle instead — pushQuad decomposes the current
// transform into (centre, scale, rotation) and hands the angle to the
// shader, which rotates the geometry and its distance field together. Skew
// is therefore not modeled for quads; nothing in QML's transform types
// produces it.

import (
	"github.com/chewxy/math32"
)

// Affine2 is a 2x3 affine: x' = A*x + C*y + E, y' = B*x + D*y + F.
type Affine2 struct{ A, B, C, D, E, F float32 }

// Identity2 is the do-nothing transform.
func Identity2() Affine2 { return Affine2{A: 1, D: 1} }

// IsIdentity reports whether m changes nothing.
func (m Affine2) IsIdentity() bool {
	return m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1 && m.E == 0 && m.F == 0
}

// Mul returns m∘n: apply n first, then m.
func (m Affine2) Mul(n Affine2) Affine2 {
	return Affine2{
		A: m.A*n.A + m.C*n.B,
		B: m.B*n.A + m.D*n.B,
		C: m.A*n.C + m.C*n.D,
		D: m.B*n.C + m.D*n.D,
		E: m.A*n.E + m.C*n.F + m.E,
		F: m.B*n.E + m.D*n.F + m.F,
	}
}

// Apply transforms a point.
func (m Affine2) Apply(x, y float32) (float32, float32) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// Invert returns the inverse transform and whether it exists (it does unless
// the transform collapses an axis).
func (m Affine2) Invert() (Affine2, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 {
		return Identity2(), false
	}
	id := 1 / det
	out := Affine2{A: m.D * id, B: -m.B * id, C: -m.C * id, D: m.A * id}
	out.E = -(out.A*m.E + out.C*m.F)
	out.F = -(out.B*m.E + out.D*m.F)
	return out, true
}

// Translate2 / Rotate2 / Scale2 build the basic transforms. Rotate2 and
// Scale2 act about a given origin, which is how QML's Rotation and Scale
// transform types are written.
func Translate2(dx, dy float32) Affine2 { return Affine2{A: 1, D: 1, E: dx, F: dy} }

func Rotate2(rad, ox, oy float32) Affine2 {
	s, c := math32.Sincos(rad)
	m := Affine2{A: c, B: s, C: -s, D: c}
	m.E = ox - (m.A*ox + m.C*oy)
	m.F = oy - (m.B*ox + m.D*oy)
	return m
}

// Linear2 is an arbitrary 2x2 linear map about a point — a shear, a skew, or
// the orthographic projection of a 3D rotation (see ui Transform.Axis).
// x' = a*x + c*y, y' = b*x + d*y, with (ox,oy) held fixed.
func Linear2(a, b, c, d, ox, oy float32) Affine2 {
	m := Affine2{A: a, B: b, C: c, D: d}
	m.E = ox - (m.A*ox + m.C*oy)
	m.F = oy - (m.B*ox + m.D*oy)
	return m
}

func Scale2(sx, sy, ox, oy float32) Affine2 {
	m := Affine2{A: sx, D: sy}
	m.E = ox - sx*ox
	m.F = oy - sy*oy
	return m
}

// decompose splits the transform into the pieces the quad shader needs: the
// scale on each axis and the rotation of the x axis. Skew (a rotation
// followed by a non-uniform scale) folds into these approximately.
func (m Affine2) decompose() (sx, sy, rot float32) {
	sx = math32.Hypot(m.A, m.B)
	sy = math32.Hypot(m.C, m.D)
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	rot = math32.Atan2(m.B, m.A)
	return sx, sy, rot
}

// shearDecompose splits the transform into what the SDF quad shader can
// apply: a scale on each axis, a horizontal SHEAR, and a rotation — the QR
// decomposition M = R(rot) · [[sx, shear*sy], [0, sy]]. Unlike decompose it
// loses nothing: any 2x2 is exactly a scale, a shear and a rotation, which
// is what lets a quad carry the orthographic projection of a rotation about
// a DIAGONAL axis (a shear, where an axis-aligned one is a pure scale).
//
// shear is stated in the SCALED frame — the x offset per unit of scaled y —
// since the shader works in the quad's own pixel box.
func (m Affine2) shearDecompose() (sx, sy, shear, rot float32) {
	// Columns of the linear part: c1 = (A,B), c2 = (C,D).
	sx = math32.Hypot(m.A, m.B)
	if sx == 0 {
		return 1, 1, 0, 0
	}
	// q1 is the rotated x axis; the rest of c2 after removing its q1
	// component is the y extent, signed by the determinant so a mirrored
	// transform stays mirrored rather than flipping the rotation.
	q1x, q1y := m.A/sx, m.B/sx
	r12 := q1x*m.C + q1y*m.D
	perpX, perpY := m.C-r12*q1x, m.D-r12*q1y
	sy = math32.Hypot(perpX, perpY)
	if det := m.A*m.D - m.B*m.C; det < 0 {
		sy = -sy
	}
	rot = math32.Atan2(m.B, m.A)
	if sy != 0 {
		shear = r12 / sy
	}
	return sx, sy, shear, rot
}

// PushTransform draws everything queued until the matching PopTransform
// through m, composed with any transform already pushed.
func (r *Renderer2D) PushTransform(m Affine2) {
	r.xforms = append(r.xforms, r.xform().Mul(m))
}

// PushRotate is PushTransform for a rotation (radians) about a point.
func (r *Renderer2D) PushRotate(rad, ox, oy float32) { r.PushTransform(Rotate2(rad, ox, oy)) }

// PushScale is PushTransform for a scale about a point.
func (r *Renderer2D) PushScale(sx, sy, ox, oy float32) { r.PushTransform(Scale2(sx, sy, ox, oy)) }

// PopTransform restores the transform active before the matching push.
func (r *Renderer2D) PopTransform() {
	if len(r.xforms) > 0 {
		r.xforms = r.xforms[:len(r.xforms)-1]
	}
}

// CurrentTransform is the transform in effect (identity when none).
func (r *Renderer2D) CurrentTransform() Affine2 { return r.xform() }

// xform is the top of the stack, or identity.
func (r *Renderer2D) xform() Affine2 {
	if n := len(r.xforms); n > 0 {
		return r.xforms[n-1]
	}
	return Identity2()
}

// clipBox maps a clip rectangle through the current transform, returning
// the axis-aligned box it covers and the scale its corner radius should
// take. With no transform in force it returns the rectangle unchanged.
func (r *Renderer2D) clipBox(x, y, w, h float32) (float32, float32, float32, float32, float32) {
	if len(r.xforms) == 0 {
		return x, y, w, h, 1
	}
	m := r.xform()
	x0, y0 := m.Apply(x, y)
	x1, y1 := m.Apply(x+w, y)
	x2, y2 := m.Apply(x+w, y+h)
	x3, y3 := m.Apply(x, y+h)
	minx, maxx := min(min(x0, x1), min(x2, x3)), max(max(x0, x1), max(x2, x3))
	miny, maxy := min(min(y0, y1), min(y2, y3)), max(max(y0, y1), max(y2, y3))
	// The radius follows the transform's own scale (the geometric mean of
	// the two axes, so a non-uniform scale still rounds sensibly).
	sx := math32.Hypot(m.A, m.B)
	sy := math32.Hypot(m.C, m.D)
	rs := math32.Sqrt(max(sx*sy, 0))
	if rs <= 0 {
		rs = 1
	}
	return minx, miny, maxx - minx, maxy - miny, rs
}
