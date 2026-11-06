package ui

// Item transforms: QML's `rotation`, `scale` and `transform:` list on any
// item. A transformed element paints (and hit-tests) through the transform;
// LAYOUT is untouched, exactly as in QML — a rotated item still occupies its
// unrotated box, which is why a rotated card does not disturb its siblings.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/render"
)

// XformKind is which transform an entry describes.
type XformKind int

const (
	XformTranslate XformKind = iota
	XformRotate
	XformScale
)

// Transform is one entry of an item's transform list — QML's Translate,
// Rotation and Scale types. Rel marks an entry whose origin is the item's
// own centre (QML's `rotation` / `scale` properties); otherwise OX/OY are
// the origin in item coordinates.
type Transform struct {
	Kind   XformKind
	X, Y   float32 // Translate offsets
	Angle  float32 // Rotation, in degrees
	SX, SY float32 // Scale factors
	OX, OY float32 // origin, in item coordinates
	Rel    bool    // origin is the item's centre
	// OriginFracX/Y place the origin as a FRACTION of the item's box — how
	// QML's transformOrigin is written (TopLeft, Center, BottomRight…).
	// Used when Rel is false and OX/OY are zero.
	OriginFracX, OriginFracY float32
	// AxisX/Y/Z is the axis a Rotation turns about (QML's `axis`); the
	// zero value means the Z axis, a plain in-plane rotation. Turning
	// about X or Y tilts the item away from the viewer, which projects to
	// a squash of the perpendicular dimension — how a card flip and every
	// 3D-rotation demo is written.
	AxisX, AxisY, AxisZ float32
}

// Translate builds a translate entry.
func Translate(dx, dy float32) Transform {
	return Transform{Kind: XformTranslate, X: dx, Y: dy}
}

// Rotation builds a rotation entry (degrees) about an origin in item
// coordinates.
func Rotation(deg, ox, oy float32) Transform {
	return Transform{Kind: XformRotate, Angle: deg, OX: ox, OY: oy}
}

// Rotation3D builds a rotation entry about an arbitrary axis (QML's
// `Rotation { axis { x: 0; y: 1; z: 0 } }`). A z-only axis is the plain
// in-plane rotation Rotation builds.
func Rotation3D(deg, ox, oy, ax, ay, az float32) Transform {
	return Transform{Kind: XformRotate, Angle: deg, OX: ox, OY: oy,
		AxisX: ax, AxisY: ay, AxisZ: az}
}

// ScaleXY builds a scale entry about an origin in item coordinates.
func ScaleXY(sx, sy, ox, oy float32) Transform {
	return Transform{Kind: XformScale, SX: sx, SY: sy, OX: ox, OY: oy}
}

// affine turns the entry into a matrix in WINDOW coordinates: item-relative
// origins are offset by the item's own rect.
func (t Transform) affine(rect Rect, cx, cy float32) render.Affine2 {
	ox, oy := rect.X+t.OX, rect.Y+t.OY
	switch {
	case t.Rel:
		ox, oy = cx, cy
	case t.OX == 0 && t.OY == 0 && (t.OriginFracX != 0 || t.OriginFracY != 0):
		ox = rect.X + rect.W*t.OriginFracX
		oy = rect.Y + rect.H*t.OriginFracY
	}
	switch t.Kind {
	case XformTranslate:
		return render.Translate2(t.X, t.Y)
	case XformRotate:
		rad := t.Angle * math32.Pi / 180
		if a, b, c, d, ok := t.axisRotation(rad); ok {
			return render.Linear2(a, b, c, d, ox, oy)
		}
		return render.Rotate2(rad, ox, oy)
	case XformScale:
		sx, sy := t.SX, t.SY
		if sx == 0 {
			sx = 1
		}
		if sy == 0 {
			sy = 1
		}
		return render.Scale2(sx, sy, ox, oy)
	}
	return render.Identity2()
}

// axisRotation is the 2x2 an off-Z rotation axis projects to: the top-left
// block of the 3D rotation matrix, which IS the orthographic projection of
// the turned plane. MEASURED against Qt 6.10: a 100px square turned 30° / 60°
// / 80° about Y is 87 / 50 / 17 px wide and full height, exactly cos(angle)
// — Qt applies no perspective unless one is set, so the projection stays
// affine and a plain matrix carries it.
//
// It reports false for a Z-only (or degenerate) axis, which is the ordinary
// in-plane rotation.
func (t Transform) axisRotation(rad float32) (a, b, c, d float32, ok bool) {
	x, y, z := t.AxisX, t.AxisY, t.AxisZ
	if x == 0 && y == 0 {
		return 0, 0, 0, 0, false // z-only, or no axis stated at all
	}
	n := math32.Sqrt(x*x + y*y + z*z)
	if n == 0 {
		return 0, 0, 0, 0, false
	}
	x, y, z = x/n, y/n, z/n
	sin, cos := math32.Sincos(rad)
	k := 1 - cos
	// Rodrigues, top-left 2x2 (the projection drops the Z row and column).
	// Column-major to match Affine2: x' = A*x + C*y, y' = B*x + D*y.
	a = cos + x*x*k
	c = x*y*k - z*sin
	b = y*x*k + z*sin
	d = cos + y*y*k
	return a, b, c, d, true
}
