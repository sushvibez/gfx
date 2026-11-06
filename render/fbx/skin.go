package fbx

import (
	"sort"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// extractSkin builds a gltf.Skin (joint node list + inverse-bind matrices)
// and per-CONTROL-POINT [4]joint/[4]weight for a geometry (the caller
// expands them into each primitive's emitted-vertex order). geoID is the
// geometry object's id; cpCount the control-point count. Returns nil when
// the geometry has no skin.
func extractSkin(g *graph, geoID int64, cpCount int, idToNode map[int64]int) (*gltf.Skin, [][4]uint16, [][4]float32, map[int]core.Mat4) {
	// A Deformer(Skin) is an OO child of the geometry.
	var skinObj *object
	for _, cid := range g.parent[geoID] {
		if o := g.objs[cid]; o != nil && o.node.Name == "Deformer" && isSkin(o) {
			skinObj = o
			break
		}
	}
	// 6100 links the other way (skin's parent is the geometry via childOf) —
	// also scan every deformer whose parent chain reaches this geometry.
	if skinObj == nil {
		for _, o := range g.objs {
			if o.node.Name == "Deformer" && isSkin(o) && g.childOf[o.id] == geoID {
				skinObj = o
				break
			}
		}
	}
	if skinObj == nil {
		return nil, nil, nil, nil
	}

	// Clusters (SubDeformers) hang off the skin.
	type cluster struct {
		joint   int // node index
		invBind core.Mat4
	}
	var clusters []cluster
	bindWorld := map[int]core.Mat4{} // joint node -> TransformLink (bind world)
	// Accumulate per-control-point (joint,weight) contributions.
	type cw struct {
		j int
		w float32
	}
	perCP := make([][]cw, cpCount)

	for _, cid := range g.parent[skinObj.id] {
		co := g.objs[cid]
		if co == nil || co.node.Name != "Deformer" || isSkin(co) {
			continue
		}
		bone := clusterBone(g, co.id, idToNode)
		if bone < 0 {
			continue
		}
		ji := len(clusters)
		clusters = append(clusters, cluster{joint: bone, invBind: clusterInvBind(co.node)})
		if link := mat4Of(child(co.node, "TransformLink")); link != nil {
			bindWorld[bone] = *link
		}

		idxs := arr(child(co.node, "Indexes"))
		wts := arr(child(co.node, "Weights"))
		for k := 0; k < len(idxs) && k < len(wts); k++ {
			c := int(idxs[k])
			if c >= 0 && c < cpCount {
				perCP[c] = append(perCP[c], cw{j: ji, w: float32(wts[k])})
			}
		}
	}
	if len(clusters) == 0 {
		return nil, nil, nil, nil
	}

	skin := &gltf.Skin{}
	for _, c := range clusters {
		skin.Joints = append(skin.Joints, c.joint)
		skin.InverseBind = append(skin.InverseBind, c.invBind)
	}

	// Top-4 weights per control point, normalized.
	cpJW := make([][4]uint16, cpCount)
	cpWW := make([][4]float32, cpCount)
	for c := 0; c < cpCount; c++ {
		list := perCP[c]
		sort.Slice(list, func(a, b int) bool { return list[a].w > list[b].w })
		var sum float32
		for k := 0; k < 4 && k < len(list); k++ {
			cpJW[c][k] = uint16(list[k].j)
			cpWW[c][k] = list[k].w
			sum += list[k].w
		}
		if sum > 0 {
			for k := 0; k < 4; k++ {
				cpWW[c][k] /= sum
			}
		} else {
			cpWW[c][0] = 1 // unweighted vertex rides its first joint
		}
	}

	return skin, cpJW, cpWW, bindWorld
}

func isSkin(o *object) bool {
	if o.class == "Skin" {
		return true
	}
	// 6100 stores the sub-type as a string property after the name.
	for _, p := range o.node.Props {
		if p.Kind == 'S' && p.S == "Skin" {
			return true
		}
	}
	return false
}

// clusterBone resolves the bone-node index a cluster deforms (the Model it
// connects to via OO).
func clusterBone(g *graph, clusterID int64, idToNode map[int64]int) int {
	for _, cid := range g.parent[clusterID] {
		if ni, ok := idToNode[cid]; ok {
			return ni
		}
	}
	// Or the cluster's OO parent is the bone (6100 direction).
	if pid, ok := g.childOf[clusterID]; ok {
		if ni, ok := idToNode[pid]; ok {
			return ni
		}
	}
	return -1
}

// clusterInvBind reads a cluster's inverse-bind (offset) matrix. FBX's
// Transform is exactly the mesh-geometry -> bone-local matrix at bind time —
// i.e. the glTF inverseBindMatrix — so it is used directly. TransformLink
// (the bone's bind world) is only a fallback when Transform is absent, and
// separately feeds the bind-pose reconstruction. Using inverse(TransformLink)
// * Transform here would double-apply the inverse and collapse the mesh.
func clusterInvBind(cl *Node) core.Mat4 {
	if tf := mat4Of(child(cl, "Transform")); tf != nil {
		return *tf
	}
	if link := mat4Of(child(cl, "TransformLink")); link != nil {
		if inv, ok := link.Inverse(); ok {
			return inv
		}
	}
	return core.Mat4Identity()
}

// mat4Of reads a 16-float array node into a column-major Mat4 (FBX order).
func mat4Of(n *Node) *core.Mat4 {
	a := arr(n)
	if len(a) < 16 {
		return nil
	}
	var m core.Mat4
	for i := 0; i < 16; i++ {
		m[i] = float32(a[i])
	}
	return &m
}
