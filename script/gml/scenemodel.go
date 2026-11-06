package gml

// MeshInstance: a glTF model (.glb / .gltf) placed in a Scene3D document.
//
//	MeshInstance { source: "props/crate.glb"; position: [2, 0, 5]
//	               rotationY: 30; scale: 1.5
//	               collider: "mesh" }         // "" (none), "box", "mesh"
//	MeshInstance { source: "CesiumMan.glb"; animation: 0 }
//
// Parsing happens at build (cached per source); the GPU model uploads
// lazily on the first SceneFrame.Draw, and a missing/broken source logs
// once and renders nothing (scenes stay loadable without fetched assets).
// `animation` starts a looping clip by name or index — advance it with
// SceneView.Advance(dt). Instances of one source share a model, so they
// share a pose; per-instance poses come later if a scene needs them.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/physics/p3"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/gltf"
)

// sceneModel is the retained per-source cell: parsed doc immediately, GPU
// model + animation player lazily at first draw.
type sceneModel struct {
	source string
	doc    *gltf.Document
	err    error

	model  *render.Model
	player *render.AnimationPlayer
	clip   string // requested animation ("" = none); resolved at ensure
	logged bool

	bmin, bmax core.Vec3   // doc-space AABB (posed at load), for box colliders
	trimesh    *p3.TriMesh // baked static collision mesh (built on demand)
	meshScale  float32     // scale the trimesh was baked at
}

// SceneModelDraw is one placed model instance of a built frame.
type SceneModelDraw struct {
	cell      *sceneModel
	Transform core.Mat4
}

// modelCell loads (or returns) the cached cell for a source path.
func (v *SceneView) modelCell(source string) *sceneModel {
	if c, ok := v.models[source]; ok {
		return c
	}
	c := &sceneModel{source: source}
	path := source
	if !filepath.IsAbs(path) && v.baseDir != "" {
		path = filepath.Join(v.baseDir, source)
	}
	data, err := os.ReadFile(path)
	if err == nil {
		dir := filepath.Dir(path)
		c.doc, c.err = gltf.Parse(data, func(uri string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, uri))
		})
	} else {
		c.err = err
	}
	if c.doc != nil {
		c.bmin, c.bmax = docBounds(c.doc)
	}
	v.models[source] = c
	return c
}

// docBounds computes the posed AABB of a document's default scene.
func docBounds(doc *gltf.Document) (core.Vec3, core.Vec3) {
	bmin := core.V3(math32.MaxFloat32, math32.MaxFloat32, math32.MaxFloat32)
	bmax := bmin.Scale(-1)
	var walk func(idx int, parent core.Mat4)
	walk = func(idx int, parent core.Mat4) {
		if idx < 0 || idx >= len(doc.Nodes) {
			return
		}
		n := doc.Nodes[idx]
		world := parent.Mul(n.Local)
		if n.Mesh >= 0 && n.Mesh < len(doc.Meshes) {
			for _, p := range doc.Meshes[n.Mesh].Primitives {
				for _, pos := range p.Positions {
					wp := world.MulPoint(pos)
					bmin = vmin3(bmin, wp)
					bmax = vmax3(bmax, wp)
				}
			}
		}
		for _, ch := range n.Children {
			walk(ch, world)
		}
	}
	for _, r := range doc.Roots {
		walk(r, core.Mat4Identity())
	}
	if bmin.X > bmax.X { // no geometry
		return core.Vec3{}, core.Vec3{}
	}
	return bmin, bmax
}

// bakeTriMesh flattens the document's posed triangles (scaled) into one
// static collision mesh.
func (c *sceneModel) bakeTriMesh(scale float32) *p3.TriMesh {
	if c.trimesh != nil && c.meshScale == scale {
		return c.trimesh
	}
	var verts []core.Vec3
	var idx []uint32
	var walk func(i int, parent core.Mat4)
	walk = func(i int, parent core.Mat4) {
		if i < 0 || i >= len(c.doc.Nodes) {
			return
		}
		n := c.doc.Nodes[i]
		world := parent.Mul(n.Local)
		if n.Mesh >= 0 && n.Mesh < len(c.doc.Meshes) {
			for _, p := range c.doc.Meshes[n.Mesh].Primitives {
				base := uint32(len(verts))
				for _, pos := range p.Positions {
					verts = append(verts, world.MulPoint(pos).Scale(scale))
				}
				for _, ix := range p.Indices {
					idx = append(idx, base+ix)
				}
			}
		}
		for _, ch := range n.Children {
			walk(ch, world)
		}
	}
	for _, r := range c.doc.Roots {
		walk(r, core.Mat4Identity())
	}
	c.trimesh = p3.NewTriMesh(verts, idx)
	c.meshScale = scale
	return c.trimesh
}

// ensure uploads the GPU model (and starts the requested clip) on first use.
func (c *sceneModel) ensure(r3 *render.Renderer3D) bool {
	if c.err != nil || c.doc == nil {
		if !c.logged {
			c.logged = true
			logf("gml: MeshInstance %q: %v", c.source, c.err)
		}
		return false
	}
	if c.model == nil {
		m, err := r3.BuildModel(c.doc)
		if err != nil {
			c.err = err
			return c.ensure(r3)
		}
		c.model = m
		if c.clip != "" && len(c.doc.Animations) > 0 {
			c.player = render.NewAnimationPlayer(m, c.doc.Animations)
			if !c.player.Play(c.clip) {
				c.player.PlayIndex(0) // numeric / unknown name: first clip
			}
		}
	}
	return true
}

// buildMeshInstance handles the MeshInstance element.
func (v *SceneView) buildMeshInstance(n *node, ppos core.Vec3, prot core.Quat, f *SceneFrame) error {
	env := v.env
	var source, colliderMode, clip string
	scale := float32(1)
	visible := true
	layer := p3.Layer(1)
	for _, a := range n.attrs {
		switch a.name {
		case "id", "position", "rotationY", "rotation":
		case "source":
			val, err := a.expr.eval(env)
			if err != nil {
				return err
			}
			source = str(val)
		case "scale":
			s, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			scale = s
		case "animation":
			val, err := a.expr.eval(env)
			if err != nil {
				return err
			}
			clip = str(val)
		case "collider": // "", "box", "mesh" (or false)
			val, err := a.expr.eval(env)
			if err != nil {
				return err
			}
			if b, ok := val.(bool); ok {
				if b {
					colliderMode = "box"
				}
			} else {
				colliderMode = str(val)
			}
		case "visible":
			b, err := attrBool(a, env)
			if err != nil {
				return err
			}
			visible = b
		case "layer":
			l, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			layer = p3.Layer(uint32(l))
		default:
			return fmt.Errorf("gml:%d: MeshInstance has no attribute %q", a.line, a.name)
		}
	}
	if source == "" {
		return fmt.Errorf("gml:%d: MeshInstance needs a source", n.line)
	}
	lpos, lrot, err := nodePose(n, env)
	if err != nil {
		return err
	}
	pos := ppos.Add(prot.Rotate(lpos))
	rot := prot.Mul(lrot)

	cell := v.modelCell(source)
	cell.clip = clip
	if visible {
		f.Models = append(f.Models, SceneModelDraw{
			cell:      cell,
			Transform: core.Mat4Translate(pos).Mul(rot.Mat4()).Mul(core.Mat4Scale(core.V3(scale, scale, scale))),
		})
	}

	// Collider: an AABB box around the posed model, or the baked triangles.
	if colliderMode != "" && colliderMode != "false" && cell.doc != nil {
		var shape p3.Shape
		var bpos core.Vec3
		sig := fmt.Sprintf("%s|%s|%v", source, colliderMode, scale)
		switch colliderMode {
		case "box":
			half := cell.bmax.Sub(cell.bmin).Scale(0.5 * scale)
			centerL := cell.bmin.Add(cell.bmax).Scale(0.5 * scale)
			shape = p3.Box(half)
			bpos = pos.Add(rot.Rotate(centerL))
		case "mesh":
			shape = p3.Mesh(cell.bakeTriMesh(scale))
			bpos = pos
		default:
			return fmt.Errorf("gml:%d: MeshInstance collider must be \"box\" or \"mesh\"", n.line)
		}
		key := elemKey(n)
		sb := v.bodies[key]
		if sb != nil && sb.sig != sig {
			v.world.RemoveBody(sb.id)
			sb = nil
		}
		if sb == nil {
			sb = &sceneBody{shape: shape, pos: bpos, rot: rot, sig: sig}
			sb.id = v.world.AddBody(shape, bpos, rot, layer)
			v.bodies[key] = sb
			if id := nodeID(n); id != "" {
				v.names[id] = key
			}
		} else if sb.pos != bpos || sb.rot != rot {
			v.world.SetPose(sb.id, bpos, rot)
			sb.pos, sb.rot = bpos, rot
		}
		sb.seen = true
	}
	return nil
}

// Advance ticks the scene by dt seconds: document Timers, standalone
// animations (NumberAnimation and friends driving bound properties), and
// animated model instances. Property changes bump the revision, so the
// next Frame rebuilds whatever they moved.
func (v *SceneView) Advance(dt float32) {
	if v.doc != nil {
		ms := float64(dt) * 1000
		v.doc.advanceTimers(ms)
		v.doc.advanceAnimators(ms)
	}
	for _, c := range v.models {
		if c.player != nil {
			c.player.Advance(dt)
		}
	}
}

func vmin3(a, b core.Vec3) core.Vec3 {
	return core.V3(math32.Min(a.X, b.X), math32.Min(a.Y, b.Y), math32.Min(a.Z, b.Z))
}

func vmax3(a, b core.Vec3) core.Vec3 {
	return core.V3(math32.Max(a.X, b.X), math32.Max(a.Y, b.Y), math32.Max(a.Z, b.Z))
}
