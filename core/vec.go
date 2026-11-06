package core

import "github.com/chewxy/math32"

// Vec2 is a 2-component float32 vector (UI coordinates, texture coords).
type Vec2 struct{ X, Y float32 }

func V2(x, y float32) Vec2 { return Vec2{x, y} }

func (a Vec2) Add(b Vec2) Vec2      { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2      { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Mul(b Vec2) Vec2      { return Vec2{a.X * b.X, a.Y * b.Y} }
func (a Vec2) Scale(s float32) Vec2 { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float32   { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Length() float32      { return sqrt(a.Dot(a)) }
func (a Vec2) Lerp(b Vec2, t float32) Vec2 {
	return Vec2{lerp(a.X, b.X, t), lerp(a.Y, b.Y, t)}
}
func (a Vec2) Normalize() Vec2 {
	if l := a.Length(); l > 0 {
		return a.Scale(1 / l)
	}
	return Vec2{}
}

// Vec3 is a 3-component float32 vector. Note that in GPU-shared structs a
// Vec3 field must be padded to 16 bytes to satisfy WGSL alignment; the
// gfx gen tool enforces this (DESIGN.md §4).
type Vec3 struct{ X, Y, Z float32 }

func V3(x, y, z float32) Vec3 { return Vec3{x, y, z} }

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Mul(b Vec3) Vec3      { return Vec3{a.X * b.X, a.Y * b.Y, a.Z * b.Z} }
func (a Vec3) Scale(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Neg() Vec3            { return Vec3{-a.X, -a.Y, -a.Z} }
func (a Vec3) Dot(b Vec3) float32   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Length() float32      { return sqrt(a.Dot(a)) }
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}
func (a Vec3) Lerp(b Vec3, t float32) Vec3 {
	return Vec3{lerp(a.X, b.X, t), lerp(a.Y, b.Y, t), lerp(a.Z, b.Z, t)}
}
func (a Vec3) Normalize() Vec3 {
	if l := a.Length(); l > 0 {
		return a.Scale(1 / l)
	}
	return Vec3{}
}

// Vec4 is a 4-component float32 vector (homogeneous coords, colors).
type Vec4 struct{ X, Y, Z, W float32 }

func V4(x, y, z, w float32) Vec4 { return Vec4{x, y, z, w} }

func (a Vec4) Add(b Vec4) Vec4      { return Vec4{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }
func (a Vec4) Sub(b Vec4) Vec4      { return Vec4{a.X - b.X, a.Y - b.Y, a.Z - b.Z, a.W - b.W} }
func (a Vec4) Scale(s float32) Vec4 { return Vec4{a.X * s, a.Y * s, a.Z * s, a.W * s} }
func (a Vec4) Dot(b Vec4) float32   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }
func (a Vec4) XYZ() Vec3            { return Vec3{a.X, a.Y, a.Z} }

func sqrt(x float32) float32       { return math32.Sqrt(x) }
func lerp(a, b, t float32) float32 { return a + (b-a)*t }
func sincos(x float32) (s, c float32) {
	return math32.Sincos(x)
}
