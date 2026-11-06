package core

import "github.com/chewxy/math32"

// Quat is a rotation quaternion (X, Y, Z imaginary, W real).
type Quat struct{ X, Y, Z, W float32 }

func QuatIdentity() Quat { return Quat{W: 1} }

func QuatAxisAngle(axis Vec3, rad float32) Quat {
	s, c := sincos(rad / 2)
	a := axis.Normalize()
	return Quat{a.X * s, a.Y * s, a.Z * s, c}
}

// Mul returns a*b: rotation b followed by rotation a.
func (a Quat) Mul(b Quat) Quat {
	return Quat{
		a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

func (a Quat) Conjugate() Quat { return Quat{-a.X, -a.Y, -a.Z, a.W} }

func (a Quat) Normalize() Quat {
	l := sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z + a.W*a.W)
	if l == 0 {
		return QuatIdentity()
	}
	return Quat{a.X / l, a.Y / l, a.Z / l, a.W / l}
}

// Rotate applies the rotation to a vector.
func (a Quat) Rotate(v Vec3) Vec3 {
	q := a.Mul(Quat{v.X, v.Y, v.Z, 0}).Mul(a.Conjugate())
	return Vec3{q.X, q.Y, q.Z}
}

func (a Quat) Slerp(b Quat, t float32) Quat {
	dot := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	if dot < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		dot = -dot
	}
	if dot > 0.9995 { // nearly parallel: lerp to avoid division by ~0
		return Quat{
			lerp(a.X, b.X, t), lerp(a.Y, b.Y, t),
			lerp(a.Z, b.Z, t), lerp(a.W, b.W, t),
		}.Normalize()
	}
	theta := math32.Acos(dot)
	sa, sb := func() (float32, float32) {
		s, _ := sincos(theta * (1 - t))
		s2, _ := sincos(theta * t)
		return s, s2
	}()
	st, _ := sincos(theta)
	wa, wb := sa/st, sb/st
	return Quat{
		a.X*wa + b.X*wb, a.Y*wa + b.Y*wb,
		a.Z*wa + b.Z*wb, a.W*wa + b.W*wb,
	}
}

// Mat4 converts the (unit) quaternion to a rotation matrix.
func (a Quat) Mat4() Mat4 {
	x, y, z, w := a.X, a.Y, a.Z, a.W
	return Mat4{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
}
