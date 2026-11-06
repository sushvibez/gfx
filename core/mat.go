package core

import "github.com/chewxy/math32"

// Mat4 is a 4x4 float32 matrix in column-major order (WGSL convention):
// element (row r, column c) is at index c*4+r. Transforms are right-handed;
// projection matrices map depth to WebGPU's [0,1] clip range.
type Mat4 [16]float32

func Mat4Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Mul returns a*b (apply b first, then a).
func (a Mat4) Mul(b Mat4) Mat4 {
	var out Mat4
	for c := range 4 {
		for r := range 4 {
			var sum float32
			for k := range 4 {
				sum += a[k*4+r] * b[c*4+k]
			}
			out[c*4+r] = sum
		}
	}
	return out
}

func (a Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		a[0]*v.X + a[4]*v.Y + a[8]*v.Z + a[12]*v.W,
		a[1]*v.X + a[5]*v.Y + a[9]*v.Z + a[13]*v.W,
		a[2]*v.X + a[6]*v.Y + a[10]*v.Z + a[14]*v.W,
		a[3]*v.X + a[7]*v.Y + a[11]*v.Z + a[15]*v.W,
	}
}

// MulPoint transforms a position (w=1) and drops w.
func (a Mat4) MulPoint(p Vec3) Vec3 {
	return a.MulVec4(Vec4{p.X, p.Y, p.Z, 1}).XYZ()
}

func (a Mat4) Transpose() Mat4 {
	var out Mat4
	for c := range 4 {
		for r := range 4 {
			out[c*4+r] = a[r*4+c]
		}
	}
	return out
}

func Mat4Translate(t Vec3) Mat4 {
	m := Mat4Identity()
	m[12], m[13], m[14] = t.X, t.Y, t.Z
	return m
}

func Mat4Scale(s Vec3) Mat4 {
	m := Mat4Identity()
	m[0], m[5], m[10] = s.X, s.Y, s.Z
	return m
}

func Mat4RotateX(rad float32) Mat4 {
	s, c := sincos(rad)
	m := Mat4Identity()
	m[5], m[9] = c, -s
	m[6], m[10] = s, c
	return m
}

func Mat4RotateY(rad float32) Mat4 {
	s, c := sincos(rad)
	m := Mat4Identity()
	m[0], m[8] = c, s
	m[2], m[10] = -s, c
	return m
}

func Mat4RotateZ(rad float32) Mat4 {
	s, c := sincos(rad)
	m := Mat4Identity()
	m[0], m[4] = c, -s
	m[1], m[5] = s, c
	return m
}

// Mat4Perspective builds a right-handed perspective projection with depth in
// [0,1] (WebGPU/D3D clip space, not OpenGL's [-1,1]).
func Mat4Perspective(fovyRad, aspect, znear, zfar float32) Mat4 {
	f := 1 / math32.Tan(fovyRad/2)
	var m Mat4
	m[0] = f / aspect
	m[5] = f
	m[10] = zfar / (znear - zfar)
	m[11] = -1
	m[14] = znear * zfar / (znear - zfar)
	return m
}

// Mat4Ortho builds a right-handed orthographic projection with depth in [0,1].
// For 2D UI use Mat4Ortho(0, w, h, 0, -1, 1): y grows downward like QML.
func Mat4Ortho(left, right, bottom, top, znear, zfar float32) Mat4 {
	m := Mat4Identity()
	m[0] = 2 / (right - left)
	m[5] = 2 / (top - bottom)
	m[10] = 1 / (znear - zfar)
	m[12] = (right + left) / (left - right)
	m[13] = (top + bottom) / (bottom - top)
	m[14] = znear / (znear - zfar)
	return m
}

// Mat4LookAt builds a right-handed view matrix.
func Mat4LookAt(eye, center, up Vec3) Mat4 {
	f := center.Sub(eye).Normalize()
	s := f.Cross(up).Normalize()
	u := s.Cross(f)
	return Mat4{
		s.X, u.X, -f.X, 0,
		s.Y, u.Y, -f.Y, 0,
		s.Z, u.Z, -f.Z, 0,
		-s.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

// Inverse returns the matrix inverse (cofactor expansion; ok=false and
// identity for a singular matrix). Used to unproject screen rays and to
// carry rays into a draw's model space for picking.
func (a Mat4) Inverse() (Mat4, bool) {
	// Laplace expansion via 2x2 sub-determinants (column-major indexing).
	b00 := a[0]*a[5] - a[1]*a[4]
	b01 := a[0]*a[6] - a[2]*a[4]
	b02 := a[0]*a[7] - a[3]*a[4]
	b03 := a[1]*a[6] - a[2]*a[5]
	b04 := a[1]*a[7] - a[3]*a[5]
	b05 := a[2]*a[7] - a[3]*a[6]
	b06 := a[8]*a[13] - a[9]*a[12]
	b07 := a[8]*a[14] - a[10]*a[12]
	b08 := a[8]*a[15] - a[11]*a[12]
	b09 := a[9]*a[14] - a[10]*a[13]
	b10 := a[9]*a[15] - a[11]*a[13]
	b11 := a[10]*a[15] - a[11]*a[14]

	det := b00*b11 - b01*b10 + b02*b09 + b03*b08 - b04*b07 + b05*b06
	if det == 0 {
		return Mat4Identity(), false
	}
	inv := 1 / det
	var m Mat4
	m[0] = (a[5]*b11 - a[6]*b10 + a[7]*b09) * inv
	m[1] = (a[2]*b10 - a[1]*b11 - a[3]*b09) * inv
	m[2] = (a[13]*b05 - a[14]*b04 + a[15]*b03) * inv
	m[3] = (a[10]*b04 - a[9]*b05 - a[11]*b03) * inv
	m[4] = (a[6]*b08 - a[4]*b11 - a[7]*b07) * inv
	m[5] = (a[0]*b11 - a[2]*b08 + a[3]*b07) * inv
	m[6] = (a[14]*b02 - a[12]*b05 - a[15]*b01) * inv
	m[7] = (a[8]*b05 - a[10]*b02 + a[11]*b01) * inv
	m[8] = (a[4]*b10 - a[5]*b08 + a[7]*b06) * inv
	m[9] = (a[1]*b08 - a[0]*b10 - a[3]*b06) * inv
	m[10] = (a[12]*b04 - a[13]*b02 + a[15]*b00) * inv
	m[11] = (a[9]*b02 - a[8]*b04 - a[11]*b00) * inv
	m[12] = (a[5]*b07 - a[4]*b09 - a[6]*b06) * inv
	m[13] = (a[0]*b09 - a[1]*b07 + a[2]*b06) * inv
	m[14] = (a[13]*b01 - a[12]*b03 - a[14]*b00) * inv
	m[15] = (a[8]*b03 - a[9]*b01 + a[10]*b00) * inv
	return m, true
}
