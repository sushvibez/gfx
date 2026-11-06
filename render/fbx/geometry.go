package fbx

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// buildMeshes extracts each geometry (a separate Geometry object in 7700, or
// a mesh Model carrying Vertices in 6100), triangulates it, resolves normals
// + UVs, attaches skin weights, and assigns the mesh + skin to its node.
func buildMeshes(d *Doc, g *graph, doc *gltf.Document, idToNode map[int64]int,
	resolve func(uri string) ([]byte, error)) map[int]core.Mat4 {
	bindWorlds := map[int]core.Mat4{}
	// Deterministic order: map iteration would otherwise shuffle mesh/skin
	// indices between runs.
	objs := make([]*object, 0, len(g.objs))
	for _, o := range g.objs {
		objs = append(objs, o)
	}
	sortObjs(objs)
	mats := newMatCache(resolve)
	for _, o := range objs {
		var vtxNode *Node
		var ownerNodeIdx int = -1
		var ownerID int64
		switch {
		case o.node.Name == "Geometry" && child(o.node, "Vertices") != nil:
			vtxNode = o.node
			// Owner = the Model this geometry connects up to (OO parent).
			if pid, ok := g.childOf[o.id]; ok {
				ownerNodeIdx = idToNode[pid]
				ownerID = pid
			}
		case o.node.Name == "Model" && child(o.node, "Vertices") != nil && len(arr(child(o.node, "Vertices"))) > 0:
			vtxNode = o.node
			ownerNodeIdx = idToNode[o.id]
			ownerID = o.id
		default:
			continue
		}
		prims, cpCount := extractPrimitives(vtxNode)
		if len(prims) == 0 {
			continue
		}
		// Material slots: the Materials OO-connected to the owner Model, in
		// connection order — LayerElementMaterial indices index this list.
		slots := mats.slots(g, ownerID, doc)
		for _, pr := range prims {
			if pr.slot >= 0 && pr.slot < len(slots) {
				pr.Material = slots[pr.slot]
			}
		}
		// Skin: a Deformer(Skin) connected to this geometry. Per-control-
		// point weights expand into each primitive's emitted-vertex order.
		if skin, cpJW, cpWW, bind := extractSkin(g, o.id, cpCount, idToNode); skin != nil {
			for _, pr := range prims {
				pr.Joints = make([][4]uint16, len(pr.expandCP))
				pr.Weights = make([][4]float32, len(pr.expandCP))
				for i, c := range pr.expandCP {
					if c >= 0 && c < cpCount {
						pr.Joints[i] = cpJW[c]
						pr.Weights[i] = cpWW[c]
					}
				}
			}
			doc.Skins = append(doc.Skins, *skin)
			if ownerNodeIdx >= 0 {
				doc.Nodes[ownerNodeIdx].Skin = len(doc.Skins) - 1
			}
			for k, v := range bind {
				bindWorlds[k] = v
			}
		}
		mesh := gltf.Mesh{Name: o.name}
		for _, pr := range prims {
			mesh.Primitives = append(mesh.Primitives, pr.Primitive)
		}
		doc.Meshes = append(doc.Meshes, mesh)
		if ownerNodeIdx >= 0 {
			doc.Nodes[ownerNodeIdx].Mesh = len(doc.Meshes) - 1
		}
	}
	return bindWorlds
}

// primBuild is a triangulated primitive plus the mapping from each emitted
// vertex back to its control-point index (for skin weight expansion) and the
// mesh-local material slot its polygons use.
type primBuild struct {
	gltf.Primitive
	expandCP []int // emitted-vertex -> control point
	slot     int   // LayerElementMaterial index (-1: mesh has no material layer)
}

// extractPrimitives triangulates an FBX mesh into one gltf.Primitive per
// material slot (a mesh with a ByPolygon material layer splits; AllSame or
// no layer yields a single primitive). FBX polygons are index runs where a
// NEGATIVE index (^i) closes a polygon; polygons of >3 verts are
// fan-triangulated. Normals/UVs are resolved from their LayerElement
// mapping/reference modes.
func extractPrimitives(geo *Node) ([]*primBuild, int) {
	verts := arr(child(geo, "Vertices"))
	polys := arr(child(geo, "PolygonVertexIndex"))
	if len(verts) == 0 || len(polys) == 0 {
		return nil, 0
	}
	cpCount := len(verts) / 3
	cp := make([]core.Vec3, cpCount)
	for i := 0; i < cpCount; i++ {
		cp[i] = core.V3(float32(verts[i*3]), float32(verts[i*3+1]), float32(verts[i*3+2]))
	}

	normals := layerVec3(geo, "LayerElementNormal", "Normals")
	uvs := layerVec2(geo, "LayerElementUV", "UV")
	matLayer := materialLayer(geo)

	bySlot := map[int]*primBuild{}
	var order []*primBuild
	forSlot := func(slot int) *primBuild {
		if pb := bySlot[slot]; pb != nil {
			return pb
		}
		pb := &primBuild{slot: slot}
		pb.Material = -1
		bySlot[slot] = pb
		order = append(order, pb)
		return pb
	}
	// Walk polygons, fan-triangulating.
	var poly []int    // control-point indices of the current polygon
	polyVertBase := 0 // index into the ByPolygonVertex streams at poly start
	pvIndex := 0      // running polygon-vertex counter
	polyIndex := 0    // running polygon counter (ByPolygon material lookup)
	emit := func(pb *primBuild, cpIdx, pvIdx int) {
		pb.Positions = append(pb.Positions, cp[cpIdx])
		if normals != nil {
			pb.Normals = append(pb.Normals, normals.at(cpIdx, pvIdx))
		}
		if uvs != nil {
			pb.UVs = append(pb.UVs, uvs.at(cpIdx, pvIdx))
		}
		pb.expandCP = append(pb.expandCP, cpIdx)
		pb.Indices = append(pb.Indices, uint32(len(pb.Positions)-1))
	}
	for i := 0; i < len(polys); i++ {
		raw := int(polys[i])
		idx := raw
		end := false
		if raw < 0 {
			idx = ^raw
			end = true
		}
		poly = append(poly, idx)
		if end {
			pb := forSlot(matLayer.at(polyIndex))
			// Fan: (0,k,k+1) for k in 1..n-2, using polygon-vertex offsets.
			for k := 1; k+1 < len(poly); k++ {
				emit(pb, poly[0], polyVertBase+0)
				emit(pb, poly[k], polyVertBase+k)
				emit(pb, poly[k+1], polyVertBase+k+1)
			}
			polyVertBase = pvIndex + 1
			poly = poly[:0]
			polyIndex++
		}
		pvIndex++
	}
	return order, cpCount
}

// matLayer resolves LayerElementMaterial to a per-polygon material slot.
type matLayer struct {
	idx    []int // ByPolygon indices (nil otherwise)
	allDef int   // slot for every polygon when idx is nil
}

func (l matLayer) at(poly int) int {
	if l.idx != nil {
		if poly < len(l.idx) {
			return l.idx[poly]
		}
		return -1
	}
	return l.allDef
}

func materialLayer(geo *Node) matLayer {
	ln := child(geo, "LayerElementMaterial")
	if ln == nil {
		// No layer: everything rides slot 0 (the model's first material,
		// when it has one — harmless otherwise, the slot list is empty).
		return matLayer{}
	}
	raw := arr(child(ln, "Materials"))
	m := child(ln, "MappingInformationType")
	byPoly := m != nil && len(m.Props) > 0 && m.Props[0].S == "ByPolygon"
	if byPoly && len(raw) > 0 {
		idx := make([]int, len(raw))
		for i, v := range raw {
			idx[i] = int(v)
		}
		return matLayer{idx: idx}
	}
	// AllSame (or a degenerate ByPolygon with no indices): one slot.
	slot := 0
	if len(raw) > 0 {
		slot = int(raw[0])
	}
	return matLayer{allDef: slot}
}

// layerData resolves a LayerElement* (normals/uv) against its mapping and
// reference modes, exposing a per-(control-point, polygon-vertex) accessor.
type vec3Layer struct {
	data      []core.Vec3
	index     []int // IndexToDirect table (nil = direct)
	byPolyVtx bool  // mapping is ByPolygonVertex (else ByControlPoint/ByVertex)
}

func (l *vec3Layer) at(cp, pv int) core.Vec3 {
	i := cp
	if l.byPolyVtx {
		i = pv
	}
	if l.index != nil {
		if i < len(l.index) {
			i = l.index[i]
		}
	}
	if i >= 0 && i < len(l.data) {
		return l.data[i]
	}
	return core.Vec3{}
}

func layerVec3(geo *Node, layerName, dataName string) *vec3Layer {
	ln := child(geo, layerName)
	if ln == nil {
		return nil
	}
	raw := arr(child(ln, dataName))
	if len(raw) < 3 {
		return nil
	}
	data := make([]core.Vec3, len(raw)/3)
	for i := range data {
		data[i] = core.V3(float32(raw[i*3]), float32(raw[i*3+1]), float32(raw[i*3+2]))
	}
	l := &vec3Layer{data: data}
	l.byPolyVtx = mappingByPolyVtx(ln)
	l.index = layerIndex(ln, dataName)
	return l
}

type vec2Layer struct {
	data      []core.Vec2
	index     []int
	byPolyVtx bool
}

func (l *vec2Layer) at(cp, pv int) core.Vec2 {
	i := cp
	if l.byPolyVtx {
		i = pv
	}
	if l.index != nil && i < len(l.index) {
		i = l.index[i]
	}
	if i >= 0 && i < len(l.data) {
		return l.data[i]
	}
	return core.Vec2{}
}

func layerVec2(geo *Node, layerName, dataName string) *vec2Layer {
	ln := child(geo, layerName)
	if ln == nil {
		return nil
	}
	raw := arr(child(ln, dataName))
	if len(raw) < 2 {
		return nil
	}
	data := make([]core.Vec2, len(raw)/2)
	for i := range data {
		data[i] = core.V2(float32(raw[i*2]), float32(raw[i*2+1]))
	}
	l := &vec2Layer{data: data, byPolyVtx: mappingByPolyVtx(ln)}
	l.index = layerIndex(ln, dataName)
	return l
}

func mappingByPolyVtx(ln *Node) bool {
	m := child(ln, "MappingInformationType")
	if m == nil || len(m.Props) == 0 {
		return false
	}
	return m.Props[0].S == "ByPolygonVertex"
}

func layerIndex(ln *Node, dataName string) []int {
	ref := child(ln, "ReferenceInformationType")
	if ref == nil || len(ref.Props) == 0 || ref.Props[0].S == "Direct" {
		return nil
	}
	// IndexToDirect: an "<data>Index" array.
	idxNode := child(ln, dataName+"Index")
	if idxNode == nil {
		idxNode = child(ln, "Indexes")
	}
	raw := arr(idxNode)
	if raw == nil {
		return nil
	}
	out := make([]int, len(raw))
	for i, v := range raw {
		out[i] = int(v)
	}
	return out
}
