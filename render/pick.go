package render

// Ray picking against the 3D frame's draw list: unproject a screen point
// through the camera (ScreenRay), then intersect the retained draws — AABB
// broad phase in model space, Möller–Trumbore triangles for the exact hit
// (Renderer3D.Pick). Draws are identified by SetPickTag stamps, so a click
// resolves to an application-level part id, not just a submission index.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// ScreenRay unprojects a window-pixel point into a world-space ray through
// the camera frustum. viewProj is the matrix given to Renderer3D.Begin;
// w, h the viewport pixel size; x, y the point (viewport-relative).
func ScreenRay(viewProj core.Mat4, x, y, w, h float32) (origin, dir core.Vec3, ok bool) {
	inv, ok := viewProj.Inverse()
	if !ok || w <= 0 || h <= 0 {
		return core.Vec3{}, core.Vec3{}, false
	}
	nx := 2*x/w - 1
	ny := 1 - 2*y/h
	unproject := func(z float32) (core.Vec3, bool) {
		p := inv.MulVec4(core.V4(nx, ny, z, 1))
		if p.W == 0 {
			return core.Vec3{}, false
		}
		return core.V3(p.X/p.W, p.Y/p.W, p.Z/p.W), true
	}
	near, ok1 := unproject(0) // WebGPU clip depth is [0,1]
	far, ok2 := unproject(1)
	if !ok1 || !ok2 {
		return core.Vec3{}, core.Vec3{}, false
	}
	d := far.Sub(near)
	if d.Length() == 0 {
		return core.Vec3{}, core.Vec3{}, false
	}
	return near, d.Normalize(), true
}

// RayAABB reports whether the ray hits the axis-aligned box, and the entry
// distance along dir (0 when the origin starts inside). Delegates to core
// (shared with physics queries).
func RayAABB(origin, dir, bmin, bmax core.Vec3) (float32, bool) {
	return core.RayAABB(origin, dir, bmin, bmax)
}

// RayTriangle intersects a ray with triangle abc (Möller–Trumbore,
// both-sided). Delegates to core (shared with physics queries).
func RayTriangle(origin, dir, a, b, c core.Vec3) (float32, bool) {
	return core.RayTriangle(origin, dir, a, b, c)
}

// SetPickTag stamps every subsequently queued draw with tag, so Pick can
// resolve hits to application part ids. Begin resets the stamp to -1.
func (r *Renderer3D) SetPickTag(tag int) { r.pickTag = tag }

// PickHit describes the nearest draw a ray hits.
type PickHit struct {
	Index int       // submission index within the frame's draw list
	Tag   int       // SetPickTag stamp (-1 = untagged)
	T     float32   // world-space distance along the ray
	Point core.Vec3 // world-space hit point
}

// Pick intersects a world-space ray with the current frame's draw list (the
// draws queued since the last Begin — after Render they remain valid until
// the next Begin, so input handling can pick against what was just shown).
// The ray is carried into each draw's model space, AABB-tested, then
// triangle-tested; the nearest world-space hit wins.
//
// SKINNED geometry is tested in its BIND POSE. Mesh.verts is the CPU copy
// NewMesh was given and neither skinning path rewrites it — the CPU skinner
// deforms into its own scratch and uploads that, the GPU skinner deforms in
// the vertex shader — so this is unchanged by GPU skinning, not a
// regression from it. A caller that needs the posed silhouette should pick
// against joint capsules (Model.JointWorld) or CPU-skin the primitive on
// demand with jointPalette + skinVertices.
func (r *Renderer3D) Pick(origin, dir core.Vec3) (PickHit, bool) {
	return r.PickWhere(origin, dir, nil)
}

// PickWhere is Pick restricted to draws whose tag passes keep (nil keeps
// every draw): geometry drawn for context — ghosted in an x-ray view, say —
// is then transparent to the ray instead of hiding what is behind it.
func (r *Renderer3D) PickWhere(origin, dir core.Vec3, keep func(tag int) bool) (PickHit, bool) {
	best := PickHit{T: math32.MaxFloat32}
	found := false
	// Material draws (DrawMaterial/DrawInstanced) are pickable too; their
	// submission indices continue after the base draw list. Only an
	// instanced draw's first transform is tested — per-instance picking
	// would need the whole model list retained.
	items := make([]drawItem, 0, len(r.items)+len(r.mitems))
	items = append(items, r.items...)
	for _, m := range r.mitems {
		items = append(items, drawItem{mesh: m.mesh, model: m.model, tag: m.tag})
	}
	for i, it := range items {
		if keep != nil && !keep(it.tag) {
			continue
		}
		if it.lines {
			continue
		}
		inv, ok := it.model.Inverse()
		if !ok || it.mesh == nil || len(it.mesh.verts) == 0 {
			continue
		}
		// Model-space ray: transform origin as a point, direction as a
		// vector (no translation). Non-uniform scale is fine — distances
		// are compared in world space via the hit point.
		mo := inv.MulPoint(origin)
		md4 := inv.MulVec4(core.V4(dir.X, dir.Y, dir.Z, 0))
		md := core.V3(md4.X, md4.Y, md4.Z)
		if md.Length() == 0 {
			continue
		}
		mdn := md.Normalize()
		if _, hit := RayAABB(mo, mdn, it.mesh.bmin, it.mesh.bmax); !hit {
			continue
		}
		verts, idx := it.mesh.verts, it.mesh.idx
		for t := 0; t+2 < len(idx); t += 3 {
			mt, hit := RayTriangle(mo, mdn, verts[idx[t]].Pos, verts[idx[t+1]].Pos, verts[idx[t+2]].Pos)
			if !hit {
				continue
			}
			// World distance: transform the model-space hit point back.
			wp := it.model.MulPoint(mo.Add(mdn.Scale(mt)))
			wt := wp.Sub(origin).Dot(dir)
			if wt > 0 && wt < best.T {
				best = PickHit{Index: i, Tag: it.tag, T: wt, Point: wp}
				found = true
			}
		}
	}
	return best, found
}
