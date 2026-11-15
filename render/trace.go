package render

import (
	"sort"
	"strings"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// Traced lighting (docs/notes/raytracing.md). The scene a ray can hit is a
// list of shapes under a bounding volume hierarchy, built here on the CPU
// and read by the shaders from storage buffers; the traversal itself is
// WGSL (traceWGSL), so it runs wherever the material shader does. A shape
// is a simple solid (a box, a cylinder, a wedge: exact, and the cheapest
// there is) or a triangle mesh, which has a hierarchy of its own over its
// triangles, shared by every instance of it.

// TraceShape is what a trace instance is: the unit shape its Model places.
type TraceShape int

const (
	TraceBox      TraceShape = iota // the unit cube, -0.5…0.5 (CubeMesh)
	TraceCylinder                   // radius 1, height 1 about the origin, axis Y (CylinderMesh)
	TraceWedge                      // the unit cube cut to a prism: full width in Z at x = -0.5, a point at x = +0.5
	TraceMesh                       // the triangles of TraceInstance.Mesh
)

// TraceInstance is one shape rays can hit.
type TraceInstance struct {
	Model core.Mat4
	Shape TraceShape
	// Color is the shape's diffuse colour (linear), for the light it
	// bounces (FrameSettings.TracedBounce). Zero bounces none.
	Color [3]float32
	// Mesh is the geometry of a TraceMesh shape, in the space Model places
	// (the mesh a draw of it would use). Triangles are two-sided to a ray.
	Mesh *Mesh
}

// traceNode is a BVH node as the shader reads it (32 bytes): a box, and
// either two children (count 0: left is the first, the second follows it)
// or a run of leaves (left is the first, count how many): instances in the
// scene's hierarchy, triangles in a mesh's.
type traceNode struct {
	min   [3]float32
	left  uint32
	max   [3]float32
	count uint32
}

// traceInst is a shape as the shader reads it (80 bytes): the rows of the
// matrix that takes the world into its own space, its shape and colour,
// and for a mesh the root of its hierarchy.
type traceInst struct {
	rows [3][4]float32
	meta [4]float32 // x shape, yzw its diffuse colour
	mesh [4]float32 // x the mesh hierarchy's root node
}

// traceTri is a triangle as the shader reads it (48 bytes): a corner and
// the two edges from it.
type traceTri struct {
	v0, e1, e2 [4]float32
}

type traceBounds struct{ min, max core.Vec3 }

func (b traceBounds) grow(p core.Vec3) traceBounds {
	return traceBounds{
		core.V3(min(b.min.X, p.X), min(b.min.Y, p.Y), min(b.min.Z, p.Z)),
		core.V3(max(b.max.X, p.X), max(b.max.Y, p.Y), max(b.max.Z, p.Z)),
	}
}

var traceEmpty = traceBounds{core.V3(1e30, 1e30, 1e30), core.V3(-1e30, -1e30, -1e30)}

// traceBVH builds a hierarchy over boxes: split at the median of the
// longest axis of the centres until four or fewer are left. It returns the
// nodes (the root first; indices are into this slice, and a leaf's left is
// a position in order) and the order the leaves must be stored in.
func traceBVH(bs []traceBounds) ([]traceNode, []int) {
	order := make([]int, len(bs))
	for i := range order {
		order[i] = i
	}
	centre := func(i int) core.Vec3 { return bs[i].min.Add(bs[i].max).Scale(0.5) }
	nodes := make([]traceNode, 1, 2*len(bs)+1)
	var rec func(n, lo, hi int)
	rec = func(n, lo, hi int) {
		bb, cb := traceEmpty, traceEmpty
		for _, i := range order[lo:hi] {
			bb = bb.grow(bs[i].min).grow(bs[i].max)
			cb = cb.grow(centre(i))
		}
		nodes[n].min, nodes[n].max = [3]float32{bb.min.X, bb.min.Y, bb.min.Z}, [3]float32{bb.max.X, bb.max.Y, bb.max.Z}
		if hi-lo <= 4 {
			nodes[n].left, nodes[n].count = uint32(lo), uint32(hi-lo)
			return
		}
		ext := cb.max.Sub(cb.min)
		along := func(v core.Vec3) float32 {
			switch {
			case ext.X >= ext.Y && ext.X >= ext.Z:
				return v.X
			case ext.Y >= ext.Z:
				return v.Y
			}
			return v.Z
		}
		part := order[lo:hi]
		sort.Slice(part, func(i, j int) bool { return along(centre(part[i])) < along(centre(part[j])) })
		mid := (lo + hi) / 2
		l := len(nodes)
		nodes = append(nodes, traceNode{}, traceNode{})
		nodes[n].left, nodes[n].count = uint32(l), 0
		rec(l, lo, mid)
		rec(l+1, mid, hi)
	}
	rec(0, 0, len(bs))
	return nodes, order
}

// buildTrace turns instances into the shader's arrays: the scene's
// hierarchy first, then each distinct mesh's hierarchy after it in the same
// node array; the instances in leaf order; and the meshes' triangles in
// their leaf order.
func buildTrace(insts []TraceInstance) ([]traceNode, []traceInst, []traceTri) {
	var gi []traceInst
	var bs []traceBounds
	var meshOf []*Mesh // per kept instance: its mesh, or nil
	for _, in := range insts {
		inv, ok := in.Model.Inverse()
		if !ok {
			continue // a shape with no volume
		}
		// The shape's own box, to place in the world.
		lo, hi := core.V3(-0.5, -0.5, -0.5), core.V3(0.5, 0.5, 0.5)
		var mesh *Mesh
		switch in.Shape {
		case TraceCylinder:
			lo, hi = core.V3(-1, -0.5, -1), core.V3(1, 0.5, 1)
		case TraceMesh:
			if in.Mesh == nil || in.Mesh.lines || len(in.Mesh.idx) < 3 {
				continue // nothing a ray can hit
			}
			mesh = in.Mesh
			lo, hi = mesh.bmin, mesh.bmax
		}
		var g traceInst
		for r := 0; r < 3; r++ {
			g.rows[r] = [4]float32{inv[r], inv[4+r], inv[8+r], inv[12+r]}
		}
		g.meta = [4]float32{float32(in.Shape), in.Color[0], in.Color[1], in.Color[2]}
		b := traceEmpty
		for c := 0; c < 8; c++ {
			p := core.V3(lo.X, lo.Y, lo.Z)
			if c&1 != 0 {
				p.X = hi.X
			}
			if c&2 != 0 {
				p.Y = hi.Y
			}
			if c&4 != 0 {
				p.Z = hi.Z
			}
			b = b.grow(in.Model.MulPoint(p))
		}
		gi = append(gi, g)
		bs = append(bs, b)
		meshOf = append(meshOf, mesh)
	}
	if len(gi) == 0 {
		return nil, nil, nil
	}
	nodes, order := traceBVH(bs)
	// Each mesh once: its triangles and its hierarchy, placed after what is
	// already there (child and triangle indices shifted to match).
	var tris []traceTri
	roots := map[*Mesh]uint32{}
	for _, m := range meshOf {
		if m == nil {
			continue
		}
		if _, done := roots[m]; done {
			continue
		}
		n := len(m.idx) / 3
		tb := make([]traceBounds, n)
		for t := 0; t < n; t++ {
			b := traceEmpty
			for k := 0; k < 3; k++ {
				b = b.grow(m.verts[m.idx[t*3+k]].Pos)
			}
			tb[t] = b
		}
		mn, mo := traceBVH(tb)
		nodeBase, triBase := uint32(len(nodes)), uint32(len(tris))
		for i := range mn {
			if mn[i].count > 0 {
				mn[i].left += triBase
			} else {
				mn[i].left += nodeBase
			}
		}
		roots[m] = nodeBase
		nodes = append(nodes, mn...)
		for _, t := range mo {
			a, b, c := m.verts[m.idx[t*3]].Pos, m.verts[m.idx[t*3+1]].Pos, m.verts[m.idx[t*3+2]].Pos
			e1, e2 := b.Sub(a), c.Sub(a)
			tris = append(tris, traceTri{
				v0: [4]float32{a.X, a.Y, a.Z, 0},
				e1: [4]float32{e1.X, e1.Y, e1.Z, 0},
				e2: [4]float32{e2.X, e2.Y, e2.Z, 0},
			})
		}
	}
	sorted := make([]traceInst, len(gi))
	for k, i := range order {
		sorted[k] = gi[i]
		if m := meshOf[i]; m != nil {
			sorted[k].mesh[0] = float32(roots[m])
		}
	}
	return nodes, sorted, tris
}

// SetTraceScene gives the renderer the shapes rays can hit: what casts the
// traced shadows FrameSettings.TracedShadows asks for, and what the other
// traced terms meet. The hierarchy is built here and now, so call it when
// the scene is made and again when it changes, not every frame; nil or
// empty clears it (nothing is then in shadow). The shapes need not be the
// ones drawn: a stand-in box for a detailed model is the usual trade, and
// far cheaper to trace than its triangles.
func (r *Renderer3D) SetTraceScene(insts []TraceInstance) {
	nodes, gi, tris := buildTrace(insts)
	if len(nodes) == 0 {
		// A root no ray can enter (its box is inside out).
		nodes = []traceNode{{min: [3]float32{1, 1, 1}, max: [3]float32{-1, -1, -1}, count: 1}}
		gi = []traceInst{{}}
	}
	if len(tris) == 0 {
		tris = []traceTri{{}} // a buffer to bind
	}
	rel(r.traceNodes)
	rel(r.traceInsts)
	rel(r.traceTris)
	nb := unsafe.Slice((*byte)(unsafe.Pointer(&nodes[0])), len(nodes)*int(unsafe.Sizeof(nodes[0])))
	ib := unsafe.Slice((*byte)(unsafe.Pointer(&gi[0])), len(gi)*int(unsafe.Sizeof(gi[0])))
	tb := unsafe.Slice((*byte)(unsafe.Pointer(&tris[0])), len(tris)*int(unsafe.Sizeof(tris[0])))
	r.traceNodes = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, len(nb))
	r.traceInsts = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, len(ib))
	r.traceTris = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, len(tb))
	r.queue.WriteBuffer(r.traceNodes, 0, nb)
	r.queue.WriteBuffer(r.traceInsts, 0, ib)
	r.queue.WriteBuffer(r.traceTris, 0, tb)
	r.traceShapes = len(insts)
	r.traceMeshes = len(tris) > 1 || tris[0] != (traceTri{})
	if r.mdraws != nil {
		r.growMDraws(r.mdraws.Size()) // the material bind groups hold the buffers
	}
}

// TraceShapes reports how many shapes the trace scene holds.
func (r *Renderer3D) TraceShapes() int { return r.traceShapes }

// traceWGSL is the tracer the material shader carries, and the first half
// of what the tracing passes carry (traceNearestWGSL is the rest): the
// three buffers (bound at group 0, bindings 10, 11 and 13) and trace_any, which says whether
// anything stands on a ray between tmin and tmax. Every shape is convex, so
// a hit is an interval of the ray that is not empty: the shape's own
// interval, in its unit space, where the ray keeps its parameter.
const traceWGSL = `
// Every walk of the hierarchy is bounded: by nodes visited, and by shapes
// or triangles in a leaf. A walk that ends by its own logic never reaches
// either bound (a ray visits tens of nodes, and the builder's leaves are
// small), but a GPU must not be handed a loop only the data ends: on Intel
// integrated graphics (D3D12) the unbounded nearest-hit walk hung the GPU
// on the first run of a newly built shader, every time, until the driver
// was reset and the device lost (docs/notes/raytracing.md).
const TRACE_STEPS = 1024;
const TRACE_LEAF = 64u;
struct TraceNode { mn : vec3f, left : u32, mx : vec3f, count : u32 }
struct TraceInst { r0 : vec4f, r1 : vec4f, r2 : vec4f, info : vec4f, mesh : vec4f }
struct TraceTri { v0 : vec4f, e1 : vec4f, e2 : vec4f }
@group(0) @binding(10) var<storage, read> trace_nodes : array<TraceNode>;
@group(0) @binding(11) var<storage, read> trace_insts : array<TraceInst>;
@group(0) @binding(13) var<storage, read> trace_tris : array<TraceTri>;

fn trace_slab(ro : vec3f, inv : vec3f, mn : vec3f, mx : vec3f, tmin : f32, tmax : f32) -> bool {
    let a = (mn - ro) * inv;
    let b = (mx - ro) * inv;
    let lo = min(a, b);
    let hi = max(a, b);
    return max(max(lo.x, lo.y), max(lo.z, tmin)) <= min(min(hi.x, hi.y), min(hi.z, tmax));
}
// The part of a ray inside a half-space n·p <= d, as (enter, exit) limits.
fn trace_clip(lo : vec3f, ld : vec3f, n : vec3f, d : f32, span : vec2f) -> vec2f {
    let denom = dot(n, ld);
    let dist = d - dot(n, lo);
    if (abs(denom) < 1e-9) {
        if (dist < 0.0) { return vec2f(1.0, -1.0); } // parallel and outside
        return span;
    }
    let t = dist / denom;
    if (denom > 0.0) { return vec2f(span.x, min(span.y, t)); }
    return vec2f(max(span.x, t), span.y);
}
// A ray against a mesh, in the mesh's own space (where it keeps its
// parameter): down the mesh's hierarchy to its triangles (Möller and
// Trumbore's test, two-sided). It returns the nearest hit's parameter in w
// (-1 for none) and the triangle's normal in xyz; with any set it stops at
// the first hit.
fn trace_mesh(root : u32, lo : vec3f, ld : vec3f, tmin : f32, tmax : f32, any : bool) -> vec4f {
    var hit = vec4f(0.0, 0.0, 0.0, -1.0);
    var best = tmax;
    let inv = 1.0 / ld;
    var stack : array<u32, 24>;
    stack[0] = root;
    var sp = 1;
    var steps = 0;
    while (sp > 0 && steps < TRACE_STEPS) {
        steps += 1;
        sp -= 1;
        let nd = trace_nodes[stack[sp]];
        if (!trace_slab(lo, inv, nd.mn, nd.mx, tmin, best)) { continue; }
        if (nd.count > 0u) {
            for (var k = 0u; k < min(nd.count, TRACE_LEAF); k++) {
                let tr = trace_tris[nd.left + k];
                let pv = cross(ld, tr.e2.xyz);
                let det = dot(tr.e1.xyz, pv);
                if (abs(det) < 1e-12) { continue; }
                let tv = lo - tr.v0.xyz;
                let u = dot(tv, pv) / det;
                if (u < 0.0 || u > 1.0) { continue; }
                let qv = cross(tv, tr.e1.xyz);
                let v = dot(ld, qv) / det;
                if (v < 0.0 || u + v > 1.0) { continue; }
                let t = dot(tr.e2.xyz, qv) / det;
                if (t > tmin && t < best) {
                    best = t;
                    hit = vec4f(cross(tr.e1.xyz, tr.e2.xyz), t);
                    if (any) { return hit; }
                }
            }
        } else if (sp < 22) {
            stack[sp] = nd.left;
            stack[sp + 1] = nd.left + 1u;
            sp += 2;
        }
    }
    return hit;
}
fn trace_shape(it : TraceInst, ro : vec3f, rd : vec3f, tmin : f32, tmax : f32) -> bool {
    let lo = vec3f(dot(it.r0.xyz, ro) + it.r0.w, dot(it.r1.xyz, ro) + it.r1.w, dot(it.r2.xyz, ro) + it.r2.w);
    let ld = vec3f(dot(it.r0.xyz, rd), dot(it.r1.xyz, rd), dot(it.r2.xyz, rd));
    var span = vec2f(tmin, tmax);
    let shape = u32(it.info.x);
    if (shape == 3u) {
        return trace_mesh(u32(it.mesh.x), lo, ld, tmin, tmax, true).w > 0.0;
    }
    // Every shape lies between y = -0.5 and +0.5.
    span = trace_clip(lo, ld, vec3f(0.0, 1.0, 0.0), 0.5, span);
    span = trace_clip(lo, ld, vec3f(0.0, -1.0, 0.0), 0.5, span);
    if (shape == 1u) {
        // The cylinder: inside x² + z² = 1.
        let a = ld.x * ld.x + ld.z * ld.z;
        let b = lo.x * ld.x + lo.z * ld.z;
        let c = lo.x * lo.x + lo.z * lo.z - 1.0;
        if (a < 1e-12) {
            if (c > 0.0) { return false; }
        } else {
            let disc = b * b - a * c;
            if (disc < 0.0) { return false; }
            let s = sqrt(disc);
            span = vec2f(max(span.x, (-b - s) / a), min(span.y, (-b + s) / a));
        }
    } else {
        span = trace_clip(lo, ld, vec3f(-1.0, 0.0, 0.0), 0.5, span);
        if (shape == 2u) {
            // The wedge: two slanted sides meeting at x = +0.5.
            span = trace_clip(lo, ld, vec3f(0.4472136, 0.0, 0.8944272), 0.2236068, span);
            span = trace_clip(lo, ld, vec3f(0.4472136, 0.0, -0.8944272), 0.2236068, span);
        } else {
            span = trace_clip(lo, ld, vec3f(1.0, 0.0, 0.0), 0.5, span);
            span = trace_clip(lo, ld, vec3f(0.0, 0.0, 1.0), 0.5, span);
            span = trace_clip(lo, ld, vec3f(0.0, 0.0, -1.0), 0.5, span);
        }
    }
    return span.x <= span.y;
}
// Whether anything stands on the ray between tmin and tmax.
fn trace_any(ro : vec3f, rd : vec3f, tmin : f32, tmax : f32) -> bool {
    let inv = 1.0 / rd;
    var stack : array<u32, 32>;
    stack[0] = 0u;
    var sp = 1;
    var steps = 0;
    while (sp > 0 && steps < TRACE_STEPS) {
        steps += 1;
        sp -= 1;
        let nd = trace_nodes[stack[sp]];
        if (!trace_slab(ro, inv, nd.mn, nd.mx, tmin, tmax)) { continue; }
        if (nd.count > 0u) {
            for (var k = 0u; k < min(nd.count, TRACE_LEAF); k++) {
                if (trace_shape(trace_insts[nd.left + k], ro, rd, tmin, tmax)) { return true; }
            }
        } else if (sp < 30) {
            stack[sp] = nd.left;
            stack[sp + 1] = nd.left + 1u;
            sp += 2;
        }
    }
    return false;
}
`

// traceNearestBody is the nearest-hit half of the tracer, which only the
// passes that shade what a ray meets carry (the material shader asks only
// whether anything is in the way). /*MESH*/ stands where the mesh branch
// goes: see traceNearestWGSL.
const traceNearestBody = `
// The nearest thing on a ray: where (t, or -1 for nothing before tmax), the
// surface's normal there, and its colour. The same intervals as trace_any,
// remembering which face each ray came in through.
struct TraceSpan { a : f32, b : f32, n : vec3f }
struct TraceHit { t : f32, n : vec3f, color : vec3f }
fn trace_cut(lo : vec3f, ld : vec3f, n : vec3f, d : f32, s : TraceSpan) -> TraceSpan {
    var o = s;
    let denom = dot(n, ld);
    let dist = d - dot(n, lo);
    if (abs(denom) < 1e-9) {
        if (dist < 0.0) { o.b = -1e30; }
        return o;
    }
    let t = dist / denom;
    if (denom > 0.0) {
        o.b = min(o.b, t);
    } else if (t > o.a) {
        o.a = t;
        o.n = n;
    }
    return o;
}
fn trace_enter(it : TraceInst, ro : vec3f, rd : vec3f, tmin : f32, tmax : f32) -> TraceSpan {
    let lo = vec3f(dot(it.r0.xyz, ro) + it.r0.w, dot(it.r1.xyz, ro) + it.r1.w, dot(it.r2.xyz, ro) + it.r2.w);
    let ld = vec3f(dot(it.r0.xyz, rd), dot(it.r1.xyz, rd), dot(it.r2.xyz, rd));
    var s = TraceSpan(tmin, tmax, vec3f(0.0));
    let shape = u32(it.info.x);
/*MESH*/    s = trace_cut(lo, ld, vec3f(0.0, 1.0, 0.0), 0.5, s);
    s = trace_cut(lo, ld, vec3f(0.0, -1.0, 0.0), 0.5, s);
    if (shape == 1u) {
        let a = ld.x * ld.x + ld.z * ld.z;
        let b = lo.x * ld.x + lo.z * ld.z;
        let c = lo.x * lo.x + lo.z * lo.z - 1.0;
        if (a < 1e-12) {
            if (c > 0.0) { s.b = -1e30; }
        } else {
            let disc = b * b - a * c;
            if (disc < 0.0) {
                s.b = -1e30;
            } else {
                let q = sqrt(disc);
                let t0 = (-b - q) / a;
                if (t0 > s.a) {
                    s.a = t0;
                    let p = lo + ld * t0;
                    s.n = vec3f(p.x, 0.0, p.z);
                }
                s.b = min(s.b, (-b + q) / a);
            }
        }
    } else {
        s = trace_cut(lo, ld, vec3f(-1.0, 0.0, 0.0), 0.5, s);
        if (shape == 2u) {
            s = trace_cut(lo, ld, vec3f(0.4472136, 0.0, 0.8944272), 0.2236068, s);
            s = trace_cut(lo, ld, vec3f(0.4472136, 0.0, -0.8944272), 0.2236068, s);
        } else {
            s = trace_cut(lo, ld, vec3f(1.0, 0.0, 0.0), 0.5, s);
            s = trace_cut(lo, ld, vec3f(0.0, 0.0, 1.0), 0.5, s);
            s = trace_cut(lo, ld, vec3f(0.0, 0.0, -1.0), 0.5, s);
        }
    }
    return s;
}
fn trace_nearest(ro : vec3f, rd : vec3f, tmin : f32, tmax : f32) -> TraceHit {
    var hit = TraceHit(-1.0, vec3f(0.0), vec3f(0.0));
    var best = tmax;
    let inv = 1.0 / rd;
    var stack : array<u32, 32>;
    stack[0] = 0u;
    var sp = 1;
    var steps = 0;
    while (sp > 0 && steps < TRACE_STEPS) {
        steps += 1;
        sp -= 1;
        let nd = trace_nodes[stack[sp]];
        if (!trace_slab(ro, inv, nd.mn, nd.mx, tmin, best)) { continue; }
        if (nd.count > 0u) {
            for (var k = 0u; k < min(nd.count, TRACE_LEAF); k++) {
                let it = trace_insts[nd.left + k];
                let s = trace_enter(it, ro, rd, tmin, best);
                // A ray that starts inside a shape came in through no face.
                if (s.a <= s.b && dot(s.n, s.n) > 0.0) {
                    best = s.a;
                    hit.t = s.a;
                    hit.n = normalize(it.r0.xyz * s.n.x + it.r1.xyz * s.n.y + it.r2.xyz * s.n.z);
                    hit.color = it.info.yzw;
                }
            }
        } else if (sp < 30) {
            stack[sp] = nd.left;
            stack[sp + 1] = nd.left + 1u;
            sp += 2;
        }
    }
    return hit;
}
`

// traceNearestMesh is the mesh branch of trace_enter.
const traceNearestMesh = `    if (shape == 3u) {
        // A mesh: where the ray first meets a triangle, and that triangle's
        // normal turned to face the ray.
        let m = trace_mesh(u32(it.mesh.x), lo, ld, tmin, tmax, false);
        if (m.w <= 0.0) {
            s.b = -1e30;
            return s;
        }
        s.a = m.w;
        s.b = m.w;
        s.n = select(m.xyz, -m.xyz, dot(m.xyz, ld) > 0.0);
        return s;
    }
`

// traceNearestWGSL is the nearest-hit code, with the mesh branch only when
// the trace scene holds meshes: a scene of simple shapes should not pay for
// a second traversal in every pixel's shader.
func traceNearestWGSL(mesh bool) string {
	branch := ""
	if mesh {
		branch = traceNearestMesh
	}
	return strings.Replace(traceNearestBody, "/*MESH*/", branch, 1)
}
