package p3

// World: static bodies in flat slices addressed by generation handles, with
// a rebuilt-on-change AABB BVH for query acceleration. P0 worlds are mostly
// static (level geometry + a few movers), so a full rebuild on mutation is
// simple and fast enough; incremental refit arrives with dynamics.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// Layer is a collision-category bitmask; queries match bodies where
// (query mask & body layer) != 0.
type Layer uint32

// LayerAll matches every body.
const LayerAll Layer = ^Layer(0)

// BodyID is a generation handle: stale ids (removed bodies) never alias.
type BodyID struct {
	index int32
	gen   uint32
}

// NilBody is the zero handle (never valid).
var NilBody = BodyID{index: -1}

type body struct {
	gen     uint32
	live    bool
	trigger bool // area volume: overlap events only, invisible to solid queries
	shape   Shape
	pos     core.Vec3
	rot     core.Quat
	inv     core.Quat // conjugate, cached
	layer   Layer
	bmin    core.Vec3 // world AABB
	bmax    core.Vec3

	dyn *dynBody // nil for static/kinematic bodies
}

// World holds bodies and answers queries.
type World struct {
	bodies []body
	free   []int32
	bvh    worldBVH
	dirty  bool

	gravity    core.Vec3
	hasGravity bool

	// Warm-start contact cache (solver.go), keyed by body-index pair.
	contactCache []cachedManifold
	contactIdx   map[uint64]int32

	// Scratch for island-based sleeping (dynamics.go), reused per step.
	islandParent []int32
	islandBusy   []bool
}

// NewWorld returns an empty world.
func NewWorld() *World { return &World{dirty: true} }

// AddBody inserts a static body and returns its handle.
func (w *World) AddBody(shape Shape, pos core.Vec3, rot core.Quat, layer Layer) BodyID {
	return w.add(shape, pos, rot, layer, false)
}

// AddTrigger inserts an area volume: it never blocks rays, casts or solid
// overlaps — only OverlapTriggers / TriggerEvents see it.
func (w *World) AddTrigger(shape Shape, pos core.Vec3, rot core.Quat, layer Layer) BodyID {
	return w.add(shape, pos, rot, layer, true)
}

func (w *World) add(shape Shape, pos core.Vec3, rot core.Quat, layer Layer, trigger bool) BodyID {
	var idx int32
	if n := len(w.free); n > 0 {
		idx = w.free[n-1]
		w.free = w.free[:n-1]
	} else {
		w.bodies = append(w.bodies, body{})
		idx = int32(len(w.bodies) - 1)
	}
	b := &w.bodies[idx]
	b.gen++
	b.live = true
	b.dyn = nil
	b.trigger = trigger
	b.shape = shape
	b.layer = layer
	w.dirty = true
	w.setPose(b, pos, rot)
	return BodyID{index: idx, gen: b.gen}
}

// RemoveBody deletes a body (stale handles are ignored).
func (w *World) RemoveBody(id BodyID) {
	if b := w.get(id); b != nil {
		b.live = false
		w.free = append(w.free, id.index)
		w.dirty = true
	}
}

// SetPose moves a body (marks the acceleration structure dirty).
func (w *World) SetPose(id BodyID, pos core.Vec3, rot core.Quat) {
	if b := w.get(id); b != nil {
		w.setPose(b, pos, rot)
		w.dirty = true
	}
}

// Pose returns a body's position and rotation.
func (w *World) Pose(id BodyID) (core.Vec3, core.Quat, bool) {
	if b := w.get(id); b != nil {
		return b.pos, b.rot, true
	}
	return core.Vec3{}, core.QuatIdentity(), false
}

func (w *World) get(id BodyID) *body {
	if id.index < 0 || int(id.index) >= len(w.bodies) {
		return nil
	}
	b := &w.bodies[id.index]
	if !b.live || b.gen != id.gen {
		return nil
	}
	return b
}

func (w *World) setPose(b *body, pos core.Vec3, rot core.Quat) {
	b.pos = pos
	b.rot = rot.Normalize()
	b.inv = b.rot.Conjugate()
	// World AABB: rotate the local box's 8 corners (conservative and exact
	// enough; meshes get their own tight triangle BVH inside).
	lmin, lmax := b.shape.localAABB()
	bmin := core.V3(math32.MaxFloat32, math32.MaxFloat32, math32.MaxFloat32)
	bmax := bmin.Scale(-1)
	for i := 0; i < 8; i++ {
		c := core.V3(pick(i&1 == 0, lmin.X, lmax.X), pick(i&2 == 0, lmin.Y, lmax.Y), pick(i&4 == 0, lmin.Z, lmax.Z))
		p := b.rot.Rotate(c).Add(pos)
		bmin = vmin(bmin, p)
		bmax = vmax(bmax, p)
	}
	b.bmin, b.bmax = bmin, bmax
}

func pick(cond bool, a, b float32) float32 {
	if cond {
		return a
	}
	return b
}

// toLocal carries a world point into a body's model space.
func (b *body) toLocal(p core.Vec3) core.Vec3 { return b.inv.Rotate(p.Sub(b.pos)) }

// toWorld carries a model-space point back to world space.
func (b *body) toWorld(p core.Vec3) core.Vec3 { return b.rot.Rotate(p).Add(b.pos) }

// ---- world BVH over body AABBs ----

type worldBVH struct {
	nodes []meshNode // reuse the node layout (start/n index w.order)
	order []int32    // body indices
}

// refresh rebuilds the BVH if bodies changed.
func (w *World) refresh() {
	if !w.dirty {
		return
	}
	w.dirty = false
	w.bvh.nodes = w.bvh.nodes[:0]
	w.bvh.order = w.bvh.order[:0]
	for i := range w.bodies {
		if w.bodies[i].live {
			w.bvh.order = append(w.bvh.order, int32(i))
		}
	}
	if len(w.bvh.order) > 0 {
		w.buildBVH(0, int32(len(w.bvh.order)))
	}
}

func (w *World) buildBVH(start, n int32) int32 {
	self := int32(len(w.bvh.nodes))
	w.bvh.nodes = append(w.bvh.nodes, meshNode{left: -1, right: -1})
	bmin := core.V3(math32.MaxFloat32, math32.MaxFloat32, math32.MaxFloat32)
	bmax := bmin.Scale(-1)
	for i := start; i < start+n; i++ {
		b := &w.bodies[w.bvh.order[i]]
		bmin = vmin(bmin, b.bmin)
		bmax = vmax(bmax, b.bmax)
	}
	nd := &w.bvh.nodes[self]
	nd.bmin, nd.bmax = bmin, bmax
	if n <= 2 {
		nd.start, nd.n = start, n
		return self
	}
	ext := bmax.Sub(bmin)
	axis := 0
	if ext.Y > ext.X {
		axis = 1
	}
	if ext.Z > ext.X && ext.Z > ext.Y {
		axis = 2
	}
	span := w.bvh.order[start : start+n]
	// Sort by body AABB center on the chosen axis.
	key := func(bi int32) float32 {
		b := &w.bodies[bi]
		switch axis {
		case 0:
			return b.bmin.X + b.bmax.X
		case 1:
			return b.bmin.Y + b.bmax.Y
		}
		return b.bmin.Z + b.bmax.Z
	}
	for i := 1; i < len(span); i++ {
		v := span[i]
		k := key(v)
		j := i - 1
		for j >= 0 && key(span[j]) > k {
			span[j+1] = span[j]
			j--
		}
		span[j+1] = v
	}
	half := n / 2
	l := w.buildBVH(start, half)
	r := w.buildBVH(start+half, n-half)
	nd = &w.bvh.nodes[self]
	nd.left, nd.right = l, r
	return self
}

// walkBVH visits every live body whose AABB the visitor accepts. accept
// prunes subtrees by their bounds; visit receives body indices.
func (w *World) walkBVH(accept func(bmin, bmax core.Vec3) bool, visit func(bi int32)) {
	if len(w.bvh.nodes) == 0 {
		return
	}
	var stack [64]int32
	sp := 0
	stack[sp] = 0
	sp++
	for sp > 0 {
		sp--
		nd := &w.bvh.nodes[stack[sp]]
		if !accept(nd.bmin, nd.bmax) {
			continue
		}
		if nd.left < 0 {
			for i := nd.start; i < nd.start+nd.n; i++ {
				visit(w.bvh.order[i])
			}
			continue
		}
		if sp+2 <= len(stack) {
			stack[sp] = nd.left
			stack[sp+1] = nd.right
			sp += 2
		}
	}
}
