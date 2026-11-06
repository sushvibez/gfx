package fbx

import (
	"math"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// ktimeToSec converts FBX's KTime (units of 1/46186158000 s) to seconds.
const ktimePerSec = 46186158000.0

// ToGLTF converts a parsed FBX document into the engine's gltf.Document, so
// render.BuildModel + AnimationPlayer consume it unchanged. Handles both the
// 6100 (name-keyed, mesh-in-Model) and 7700 (id-keyed, separate Geometry)
// generations. Rotation is FBX Euler-XYZ degrees on Lcl Rotation.
//
// resolve fetches texture files the FBX references but does not embed
// (same contract as gltf.Parse): it is tried with each recorded path —
// relative, absolute, then bare basename — until one succeeds. nil is fine
// when only embedded textures matter.
func ToGLTF(d *Doc, resolve func(uri string) ([]byte, error)) (*gltf.Document, error) {
	g := d.buildGraph()
	doc := &gltf.Document{}

	// --- Nodes: every Model becomes a node; index by object id. ---
	idToNode := map[int64]int{}
	var modelObjs []*object
	for _, o := range g.objs {
		if o.node.Name == "Model" {
			modelObjs = append(modelObjs, o)
		}
	}
	// Deterministic order: by id (map iteration is random).
	sortObjs(modelObjs)
	for _, o := range modelObjs {
		idToNode[o.id] = len(doc.Nodes)
		doc.Nodes = append(doc.Nodes, modelNode(o))
	}
	// Hierarchy + roots from OO links between models.
	isChild := make([]bool, len(doc.Nodes))
	for _, o := range modelObjs {
		ni := idToNode[o.id]
		for _, cid := range g.parent[o.id] {
			if cj, ok := idToNode[cid]; ok {
				doc.Nodes[ni].Children = append(doc.Nodes[ni].Children, cj)
				isChild[cj] = true
			}
		}
	}
	for i := range doc.Nodes {
		if !isChild[i] {
			doc.Roots = append(doc.Roots, i)
		}
	}

	// --- Geometry + skin. ---
	bindWorlds := buildMeshes(d, g, doc, idToNode, resolve)

	// --- Bind pose from clusters. Mixamo 6100 leaves every bone's Lcl
	// transform at zero and stores the rest pose only in the cluster
	// TransformLink (bind world). Reconstruct each joint's local transform
	// (local = inverse(parentBindWorld) * bindWorld) so the rest pose
	// deforms to identity and rotation-only clips compose on the right
	// bone offsets. ---
	if len(bindWorlds) > 0 {
		applyBindPose(doc, bindWorlds)
	}

	// --- Animation clips. ---
	buildAnims(d, g, doc, idToNode)

	return doc, nil
}

// applyBindPose rewrites joint node locals (and T/R/S) from cluster bind
// worlds, top-down so each parent's world is known first.
func applyBindPose(doc *gltf.Document, bindWorlds map[int]core.Mat4) {
	parent := make([]int, len(doc.Nodes))
	for i := range parent {
		parent[i] = -1
	}
	for i, n := range doc.Nodes {
		for _, c := range n.Children {
			parent[c] = i
		}
	}
	world := make([]core.Mat4, len(doc.Nodes))
	done := make([]bool, len(doc.Nodes))
	var resolve func(i int) core.Mat4
	resolve = func(i int) core.Mat4 {
		if done[i] {
			return world[i]
		}
		var pw core.Mat4 = core.Mat4Identity()
		if parent[i] >= 0 {
			pw = resolve(parent[i])
		}
		bw, isJoint := bindWorlds[i]
		if isJoint {
			world[i] = bw
			// local = inverse(parentWorld) * bindWorld
			if inv, ok := pw.Inverse(); ok {
				local := inv.Mul(bw)
				t, r, s := decompose(local)
				doc.Nodes[i].T, doc.Nodes[i].R, doc.Nodes[i].S = t, r, s
				doc.Nodes[i].HasTRS = true
				doc.Nodes[i].RecomputeLocal()
			}
		} else {
			world[i] = pw.Mul(doc.Nodes[i].Local)
		}
		done[i] = true
		return world[i]
	}
	for i := range doc.Nodes {
		resolve(i)
	}
}

// decompose splits an affine matrix into translation, rotation quat, scale.
func decompose(m core.Mat4) (core.Vec3, core.Quat, core.Vec3) {
	t := core.V3(m[12], m[13], m[14])
	// Column lengths = scale.
	sx := core.V3(m[0], m[1], m[2]).Length()
	sy := core.V3(m[4], m[5], m[6]).Length()
	sz := core.V3(m[8], m[9], m[10]).Length()
	s := core.V3(sx, sy, sz)
	// Normalize the rotation basis, then extract a quaternion.
	if sx == 0 || sy == 0 || sz == 0 {
		return t, core.QuatIdentity(), s
	}
	var rm core.Mat4 = core.Mat4Identity()
	rm[0], rm[1], rm[2] = m[0]/sx, m[1]/sx, m[2]/sx
	rm[4], rm[5], rm[6] = m[4]/sy, m[5]/sy, m[6]/sy
	rm[8], rm[9], rm[10] = m[8]/sz, m[9]/sz, m[10]/sz
	return t, quatFromMat(rm), s
}

// quatFromMat extracts a unit quaternion from a rotation matrix (column-major).
func quatFromMat(m core.Mat4) core.Quat {
	tr := m[0] + m[5] + m[10]
	var q core.Quat
	switch {
	case tr > 0:
		s := float32(math.Sqrt(float64(tr+1))) * 2
		q.W = 0.25 * s
		q.X = (m[6] - m[9]) / s
		q.Y = (m[8] - m[2]) / s
		q.Z = (m[1] - m[4]) / s
	case m[0] > m[5] && m[0] > m[10]:
		s := float32(math.Sqrt(float64(1+m[0]-m[5]-m[10]))) * 2
		q.W = (m[6] - m[9]) / s
		q.X = 0.25 * s
		q.Y = (m[4] + m[1]) / s
		q.Z = (m[8] + m[2]) / s
	case m[5] > m[10]:
		s := float32(math.Sqrt(float64(1+m[5]-m[0]-m[10]))) * 2
		q.W = (m[8] - m[2]) / s
		q.X = (m[4] + m[1]) / s
		q.Y = 0.25 * s
		q.Z = (m[9] + m[6]) / s
	default:
		s := float32(math.Sqrt(float64(1+m[10]-m[0]-m[5]))) * 2
		q.W = (m[1] - m[4]) / s
		q.X = (m[8] + m[2]) / s
		q.Y = (m[9] + m[6]) / s
		q.Z = 0.25 * s
	}
	return q
}

// modelNode builds a gltf.Node from a Model's Lcl TRS (Euler-XYZ degrees).
func modelNode(o *object) gltf.Node {
	p := prop70(o.node)
	t := p["Lcl Translation"]
	r := p["Lcl Rotation"]
	s, hasS := p["Lcl Scaling"]
	if !hasS {
		s = [3]float64{1, 1, 1}
	}
	n := gltf.Node{
		Name:   o.name,
		Mesh:   -1,
		Skin:   -1,
		HasTRS: true,
		T:      core.V3(float32(t[0]), float32(t[1]), float32(t[2])),
		R:      eulerXYZ(r[0], r[1], r[2]),
		S:      core.V3(float32(s[0]), float32(s[1]), float32(s[2])),
	}
	n.RecomputeLocal()
	return n
}

// eulerXYZ builds a quaternion from FBX Euler degrees (XYZ order: apply X,
// then Y, then Z → q = qz * qy * qx).
func eulerXYZ(xd, yd, zd float64) core.Quat {
	const d2r = math.Pi / 180
	qx := core.QuatAxisAngle(core.V3(1, 0, 0), float32(xd*d2r))
	qy := core.QuatAxisAngle(core.V3(0, 1, 0), float32(yd*d2r))
	qz := core.QuatAxisAngle(core.V3(0, 0, 1), float32(zd*d2r))
	return qz.Mul(qy).Mul(qx)
}

func sortObjs(o []*object) {
	for i := 1; i < len(o); i++ {
		for j := i; j > 0 && o[j-1].id > o[j].id; j-- {
			o[j-1], o[j] = o[j], o[j-1]
		}
	}
}
