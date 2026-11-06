package fbx

import (
	"sort"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// buildAnims extracts every AnimationStack as a gltf.Animation. FBX stores
// rotation as three separate Euler-degree curves (d|X/Y/Z) under a "Lcl
// Rotation" AnimationCurveNode; we sample the union of key times and convert
// to quaternions so the channel matches glTF's rotation path.
func buildAnims(d *Doc, g *graph, doc *gltf.Document, idToNode map[int64]int) {
	for _, stack := range g.objs {
		if stack.node.Name != "AnimationStack" {
			continue
		}
		clip := gltf.Animation{Name: stack.name}
		// Stack -> Layers -> CurveNodes.
		for _, layerID := range g.parent[stack.id] {
			lo := g.objs[layerID]
			if lo == nil || lo.node.Name != "AnimationLayer" {
				continue
			}
			for _, cnID := range g.parent[layerID] {
				cn := g.objs[cnID]
				if cn == nil || cn.node.Name != "AnimationCurveNode" {
					continue
				}
				bone, path, ok := curveNodeTarget(g, cnID, idToNode)
				if !ok {
					continue
				}
				ch := buildChannel(g, cnID, bone, path)
				if ch != nil {
					clip.Channels = append(clip.Channels, *ch)
					if end := ch.Times[len(ch.Times)-1]; end > clip.Duration {
						clip.Duration = end
					}
				}
			}
		}
		if len(clip.Channels) > 0 {
			doc.Animations = append(doc.Animations, clip)
		}
	}
}

// curveNodeTarget resolves which bone node + TRS path a curve node drives,
// via its OP connection to a Model (property "Lcl Translation"/"T" ...).
func curveNodeTarget(g *graph, cnID int64, idToNode map[int64]int) (bone int, path gltf.AnimPath, ok bool) {
	for _, l := range g.propConn[cnID] {
		ni, isNode := idToNode[l.parent]
		if !isNode {
			continue
		}
		switch {
		case l.prop == "Lcl Translation" || l.prop == "T":
			return ni, gltf.PathTranslation, true
		case l.prop == "Lcl Rotation" || l.prop == "R":
			return ni, gltf.PathRotation, true
		case l.prop == "Lcl Scaling" || l.prop == "S":
			return ni, gltf.PathScale, true
		}
	}
	return 0, 0, false
}

// buildChannel assembles a translation/scale (Vec) or rotation (Rot) channel
// from a curve node's three axis curves.
func buildChannel(g *graph, cnID int64, bone int, path gltf.AnimPath) *gltf.AnimChannel {
	cx, cy, cz := axisCurves(g, cnID)
	if cx == nil && cy == nil && cz == nil {
		return nil
	}
	times := unionTimes(cx, cy, cz)
	if len(times) == 0 {
		return nil
	}
	ch := &gltf.AnimChannel{Node: bone, Path: path, Times: times}
	dx := defaultFor(path)
	for _, t := range times {
		x := sampleCurve(cx, t, dx)
		y := sampleCurve(cy, t, dx)
		z := sampleCurve(cz, t, dx)
		if path == gltf.PathRotation {
			ch.Rot = append(ch.Rot, eulerXYZ(float64(x), float64(y), float64(z)))
		} else {
			ch.Vec = append(ch.Vec, core.V3(x, y, z))
		}
	}
	return ch
}

func defaultFor(p gltf.AnimPath) float32 {
	if p == gltf.PathScale {
		return 1
	}
	return 0
}

// curve is one axis's keyframes (times in seconds, ascending).
type curve struct {
	times []float32
	vals  []float32
}

// axisCurves finds a curve node's X/Y/Z AnimationCurve children by their
// OP property tag (d|X, d|Y, d|Z).
func axisCurves(g *graph, cnID int64) (x, y, z *curve) {
	for _, l := range g.opChildren[cnID] {
		co := g.objs[l.parent]
		if co == nil || co.node.Name != "AnimationCurve" {
			continue
		}
		c := readCurve(co.node)
		if c == nil {
			continue
		}
		switch l.prop {
		case "d|X", "X":
			x = c
		case "d|Y", "Y":
			y = c
		case "d|Z", "Z":
			z = c
		}
	}
	return
}

func readCurve(n *Node) *curve {
	kt := arr(child(n, "KeyTime"))
	kv := arr(child(n, "KeyValueFloat"))
	if len(kt) == 0 || len(kt) != len(kv) {
		return nil
	}
	c := &curve{times: make([]float32, len(kt)), vals: make([]float32, len(kv))}
	for i := range kt {
		c.times[i] = float32(kt[i] / ktimePerSec)
		c.vals[i] = float32(kv[i])
	}
	return c
}

func unionTimes(cs ...*curve) []float32 {
	set := map[float32]struct{}{}
	for _, c := range cs {
		if c == nil {
			continue
		}
		for _, t := range c.times {
			set[t] = struct{}{}
		}
	}
	out := make([]float32, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// sampleCurve linearly interpolates a curve at time t (clamped); def when nil.
func sampleCurve(c *curve, t, def float32) float32 {
	if c == nil || len(c.times) == 0 {
		return def
	}
	if t <= c.times[0] {
		return c.vals[0]
	}
	n := len(c.times)
	if t >= c.times[n-1] {
		return c.vals[n-1]
	}
	hi := sort.Search(n, func(i int) bool { return c.times[i] >= t })
	lo := hi - 1
	span := c.times[hi] - c.times[lo]
	if span <= 0 {
		return c.vals[lo]
	}
	f := (t - c.times[lo]) / span
	return c.vals[lo] + (c.vals[hi]-c.vals[lo])*f
}
