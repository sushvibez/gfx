package render

// Shared unit primitive meshes for scene building (gml Scene3D, level
// geometry): unit sphere (radius 1) and unit cylinder (radius 1, height 1,
// centered), cached like CubeMesh and owned by the renderer.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// SphereMesh returns the shared unit UV sphere (radius 1).
func (r *Renderer3D) SphereMesh() *Mesh {
	if r.unitSphere == nil {
		r.unitSphere = r.buildSphereMesh(24, 16)
	}
	return r.unitSphere
}

func (r *Renderer3D) buildSphereMesh(segs, rings int) *Mesh {
	var verts []Vertex3D
	var idx []uint32
	for ri := 0; ri <= rings; ri++ {
		phi := math32.Pi * float32(ri) / float32(rings)
		sp, cp := math32.Sincos(phi)
		for si := 0; si <= segs; si++ {
			th := 2 * math32.Pi * float32(si) / float32(segs)
			st, ct := math32.Sincos(th)
			n := core.V3(sp*ct, cp, sp*st)
			verts = append(verts, Vertex3D{Pos: n, Normal: n,
				U: float32(si) / float32(segs), V: float32(ri) / float32(rings)})
		}
	}
	stride := uint32(segs + 1)
	for ri := 0; ri < rings; ri++ {
		for si := 0; si < segs; si++ {
			a := uint32(ri)*stride + uint32(si)
			// Wound counter-clockwise seen from OUTSIDE, so back-face
			// culling keeps the near hemisphere. Inverted, the near side
			// was culled and the far side's inward-facing normals shaded
			// the sphere as though lit from behind (a white QtQuick3D
			// sphere came out grey, and every sphere in a 3D scene read
			// flat).
			idx = append(idx, a, a+1, a+stride, a+1, a+stride+1, a+stride)
		}
	}
	return r.NewMesh(verts, idx)
}

// ConeMesh returns the shared unit cone (base radius 1 at y=-0.5, apex at
// y=+0.5, axis Y).
func (r *Renderer3D) ConeMesh() *Mesh {
	if r.unitCone == nil {
		r.unitCone = r.buildConeMesh(32)
	}
	return r.unitCone
}

func (r *Renderer3D) buildConeMesh(segs int) *Mesh {
	var verts []Vertex3D
	var idx []uint32
	apex := core.V3(0, 0.5, 0)
	// Side: one triangle per segment, with the normal of the slanted face
	// (flat-shaded, so each triangle carries its own vertices).
	for si := 0; si < segs; si++ {
		th0 := 2 * math32.Pi * float32(si) / float32(segs)
		th1 := 2 * math32.Pi * float32(si+1) / float32(segs)
		s0, c0 := math32.Sincos(th0)
		s1, c1 := math32.Sincos(th1)
		b0 := core.V3(c0, -0.5, s0)
		b1 := core.V3(c1, -0.5, s1)
		// Face normal: cross of the two edges from the apex.
		e0 := b0.Sub(apex)
		e1 := b1.Sub(apex)
		n := e1.Cross(e0).Normalize()
		base := uint32(len(verts))
		verts = append(verts,
			Vertex3D{Pos: apex, Normal: n, U: 0.5, V: 0},
			Vertex3D{Pos: b0, Normal: n, U: float32(si) / float32(segs), V: 1},
			Vertex3D{Pos: b1, Normal: n, U: float32(si+1) / float32(segs), V: 1})
		idx = append(idx, base, base+1, base+2)
	}
	// Base disc, facing down.
	down := core.V3(0, -1, 0)
	center := uint32(len(verts))
	verts = append(verts, Vertex3D{Pos: core.V3(0, -0.5, 0), Normal: down, U: 0.5, V: 0.5})
	for si := 0; si <= segs; si++ {
		th := 2 * math32.Pi * float32(si) / float32(segs)
		st, ct := math32.Sincos(th)
		verts = append(verts, Vertex3D{Pos: core.V3(ct, -0.5, st), Normal: down,
			U: (ct + 1) / 2, V: (st + 1) / 2})
	}
	for si := 0; si < segs; si++ {
		a := center + 1 + uint32(si)
		idx = append(idx, center, a+1, a)
	}
	return r.NewMesh(verts, idx)
}

// PlaneMesh returns the shared unit plane: a 1x1 quad in the XY plane facing
// +Z, which is how QtQuick3D's #Plane is oriented.
func (r *Renderer3D) PlaneMesh() *Mesh {
	if r.unitPlane == nil {
		n := core.V3(0, 0, 1)
		verts := []Vertex3D{
			{Pos: core.V3(-0.5, -0.5, 0), Normal: n, U: 0, V: 1},
			{Pos: core.V3(0.5, -0.5, 0), Normal: n, U: 1, V: 1},
			{Pos: core.V3(0.5, 0.5, 0), Normal: n, U: 1, V: 0},
			{Pos: core.V3(-0.5, 0.5, 0), Normal: n, U: 0, V: 0},
		}
		r.unitPlane = r.NewMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
	}
	return r.unitPlane
}

// CylinderMesh returns the shared unit cylinder (radius 1, height 1,
// centered on the origin, axis Y).
func (r *Renderer3D) CylinderMesh() *Mesh {
	if r.unitCylinder == nil {
		r.unitCylinder = r.buildCylinderMesh(32)
	}
	return r.unitCylinder
}

func (r *Renderer3D) buildCylinderMesh(segs int) *Mesh {
	var verts []Vertex3D
	var idx []uint32
	// Side wall (smooth normals).
	for i := 0; i <= segs; i++ {
		a := float32(i) / float32(segs) * 2 * math32.Pi
		s, c := math32.Sincos(a)
		n := core.V3(c, 0, s)
		verts = append(verts,
			Vertex3D{Pos: core.V3(c, 0.5, s), Normal: n, U: float32(i) / float32(segs)},
			Vertex3D{Pos: core.V3(c, -0.5, s), Normal: n, U: float32(i) / float32(segs), V: 1})
	}
	for i := 0; i < segs; i++ {
		t := uint32(i * 2)
		idx = append(idx, t, t+2, t+1, t+1, t+2, t+3)
	}
	// Caps (flat normals).
	ring := uint32(len(verts))
	topC := ring
	verts = append(verts,
		Vertex3D{Pos: core.V3(0, 0.5, 0), Normal: core.V3(0, 1, 0)},
		Vertex3D{Pos: core.V3(0, -0.5, 0), Normal: core.V3(0, -1, 0)})
	ring += 2
	for i := 0; i <= segs; i++ {
		a := float32(i) / float32(segs) * 2 * math32.Pi
		s, c := math32.Sincos(a)
		verts = append(verts,
			Vertex3D{Pos: core.V3(c, 0.5, s), Normal: core.V3(0, 1, 0)},
			Vertex3D{Pos: core.V3(c, -0.5, s), Normal: core.V3(0, -1, 0)})
	}
	for i := 0; i < segs; i++ {
		t := ring + uint32(i*2)
		idx = append(idx, topC, t+2, t)
		idx = append(idx, topC+1, t+1, t+3)
	}
	return r.NewMesh(verts, idx)
}
