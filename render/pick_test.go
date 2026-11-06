package render

import (
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// unitCubeMesh builds a CPU-only unit cube (no GPU buffers) for pick tests.
func unitCubeMesh() *Mesh {
	// 8 corners, 12 triangles.
	c := []core.Vec3{
		{X: -0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: -0.5, Z: -0.5},
		{X: 0.5, Y: 0.5, Z: -0.5}, {X: -0.5, Y: 0.5, Z: -0.5},
		{X: -0.5, Y: -0.5, Z: 0.5}, {X: 0.5, Y: -0.5, Z: 0.5},
		{X: 0.5, Y: 0.5, Z: 0.5}, {X: -0.5, Y: 0.5, Z: 0.5},
	}
	var verts []Vertex3D
	for _, p := range c {
		verts = append(verts, Vertex3D{Pos: p})
	}
	idx := []uint32{
		0, 2, 1, 0, 3, 2, // back
		4, 5, 6, 4, 6, 7, // front
		0, 1, 5, 0, 5, 4, // bottom
		3, 6, 2, 3, 7, 6, // top
		0, 7, 3, 0, 4, 7, // left
		1, 2, 6, 1, 6, 5, // right
	}
	return &Mesh{verts: verts, idx: idx,
		bmin: core.V3(-0.5, -0.5, -0.5), bmax: core.V3(0.5, 0.5, 0.5)}
}

func TestRayPrimitives(t *testing.T) {
	// AABB: straight-on hit, miss, inside.
	if _, ok := RayAABB(core.V3(0, 0, -5), core.V3(0, 0, 1), core.V3(-1, -1, -1), core.V3(1, 1, 1)); !ok {
		t.Fatal("AABB straight hit missed")
	}
	if _, ok := RayAABB(core.V3(0, 3, -5), core.V3(0, 0, 1), core.V3(-1, -1, -1), core.V3(1, 1, 1)); ok {
		t.Fatal("AABB miss reported hit")
	}
	if tt, ok := RayAABB(core.V3(0, 0, 0), core.V3(0, 0, 1), core.V3(-1, -1, -1), core.V3(1, 1, 1)); !ok || tt != 0 {
		t.Fatalf("inside-origin AABB = (%v,%v), want (0,true)", tt, ok)
	}
	// Triangle: center hit at the right distance, outside miss.
	a, b, c := core.V3(-1, -1, 2), core.V3(1, -1, 2), core.V3(0, 1, 2)
	if tt, ok := RayTriangle(core.V3(0, 0, 0), core.V3(0, 0, 1), a, b, c); !ok || math32.Abs(tt-2) > 1e-5 {
		t.Fatalf("triangle hit = (%v,%v), want (2,true)", tt, ok)
	}
	if _, ok := RayTriangle(core.V3(5, 5, 0), core.V3(0, 0, 1), a, b, c); ok {
		t.Fatal("triangle miss reported hit")
	}
}

func TestScreenRayRoundTrip(t *testing.T) {
	// A camera looking at the origin; the ray through a world point's
	// projected pixel must pass back through that point.
	view := core.Mat4LookAt(core.V3(4, 3, 8), core.V3(0, 0, 0), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(1.0, 16.0/9.0, 0.1, 100)
	vp := proj.Mul(view)
	w, h := float32(1280), float32(720)

	world := core.V3(0.7, -0.4, 1.2)
	clip := vp.MulVec4(core.V4(world.X, world.Y, world.Z, 1))
	sx := (clip.X/clip.W + 1) / 2 * w
	sy := (1 - clip.Y/clip.W) / 2 * h

	origin, dir, ok := ScreenRay(vp, sx, sy, w, h)
	if !ok {
		t.Fatal("ScreenRay failed")
	}
	// Distance from `world` to the ray must be ~0.
	toP := world.Sub(origin)
	perp := toP.Sub(dir.Scale(toP.Dot(dir)))
	if d := perp.Length(); d > 1e-3 {
		t.Fatalf("ray misses the source point by %v", d)
	}
}

func TestPickNearestAndTags(t *testing.T) {
	cube := unitCubeMesh()
	r := &Renderer3D{}
	add := func(model core.Mat4, tag int) {
		r.items = append(r.items, drawItem{mesh: cube, model: model, tag: tag})
	}
	// Two cubes along +Z: near at z=2 (tag 7), far at z=6 (tag 9); one off
	// to the side (tag 11).
	add(core.Mat4Translate(core.V3(0, 0, 2)), 7)
	add(core.Mat4Translate(core.V3(0, 0, 6)), 9)
	add(core.Mat4Translate(core.V3(5, 0, 2)), 11)

	hit, ok := r.Pick(core.V3(0, 0, 0), core.V3(0, 0, 1))
	if !ok {
		t.Fatal("no hit")
	}
	if hit.Tag != 7 || hit.Index != 0 {
		t.Fatalf("hit tag/index = %d/%d, want 7/0 (nearest)", hit.Tag, hit.Index)
	}
	if math32.Abs(hit.T-1.5) > 1e-4 { // front face of the near cube
		t.Fatalf("hit T = %v, want 1.5", hit.T)
	}
	if math32.Abs(hit.Point.Z-1.5) > 1e-4 {
		t.Fatalf("hit point Z = %v, want 1.5", hit.Point.Z)
	}
	// A scaled cube: non-uniform model transforms must still yield world T.
	r.items = r.items[:0]
	add(core.Mat4Translate(core.V3(0, 0, 4)).Mul(core.Mat4Scale(core.V3(4, 0.5, 2))), 3)
	hit, ok = r.Pick(core.V3(0, 0, 0), core.V3(0, 0, 1))
	if !ok || hit.Tag != 3 {
		t.Fatalf("scaled cube not hit: %+v %v", hit, ok)
	}
	if math32.Abs(hit.T-3) > 1e-4 { // front face at z = 4 - 2/2 = 3
		t.Fatalf("scaled hit T = %v, want 3", hit.T)
	}
	// Miss entirely.
	if _, ok := r.Pick(core.V3(0, 10, 0), core.V3(0, 0, 1)); ok {
		t.Fatal("miss reported hit")
	}
}
