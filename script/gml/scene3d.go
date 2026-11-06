package gml

// Scene3D: 3D scenes as executable .gml documents (the gml-as-document
// decision) — one declaration produces both the render draw list AND the
// physics bodies, and property bindings make the scene live: a door whose
// position binds to `doorOpen` re-poses its collider automatically when
// the property changes.
//
//	Scene3D { lightDir: [-0.5, -1, -0.3]
//	    Box { size: [24, 0.5, 40]; position: [0, -0.25, 8]; color: "#404448" }
//	    Node3D { position: [0, 0, 10]; rotationY: 45
//	        Cylinder { id: pillar; radius: 0.5; height: 3; color: "#806040" }
//	    }
//	    Box { id: door; position: [0, 1.25 + doorOpen*2.4, 19]; size: [7.4, 2.5, 0.5]; color: "#994b47" }
//	    Trigger { id: plate; position: [0, 0.6, 13]; size: [2.4, 1.2, 2.4] }
//	}
//
// Solids get physics bodies by default (collider: false opts out); Trigger
// volumes are physics-only areas. Bodies are retained across rebuilds keyed
// by element id/position and re-posed (SetPose) when a binding moves them —
// the dirty tracker makes an unchanged scene cost nothing per frame.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/physics/p3"
	"github.com/sushvibez/gfx/render"
)

// SceneDrawKind selects the unit mesh a draw scales.
type SceneDrawKind int

const (
	DrawBox SceneDrawKind = iota
	DrawSphere
	DrawCylinder
)

// SceneDraw is one renderable item of a built frame.
type SceneDraw struct {
	Kind      SceneDrawKind
	Transform core.Mat4 // includes the size/radius scaling
	Color     render.RGBA
	Metallic  float32
	Roughness float32

	tex     *sceneTex     // nil = flat color
	boxMesh *sceneBoxMesh // nil = the renderer's shared unit mesh
}

// SceneFrame is the immutable result of one scene build.
type SceneFrame struct {
	Draws    []SceneDraw
	Models   []SceneModelDraw
	LightDir core.Vec3
}

// Draw queues the frame into the 3D renderer using its shared unit meshes;
// model instances upload lazily on their first draw.
func (f *SceneFrame) Draw(r3 *render.Renderer3D) {
	for i := range f.Models {
		md := &f.Models[i]
		if md.cell.ensure(r3) {
			md.cell.model.Draw(r3, md.Transform)
		}
	}
	for i := range f.Draws {
		d := &f.Draws[i]
		var mesh *render.Mesh
		var img *render.Image3D
		switch d.Kind {
		case DrawSphere:
			mesh = r3.SphereMesh()
		case DrawCylinder:
			mesh = r3.CylinderMesh()
		default:
			mesh = r3.CubeMesh()
		}
		if d.tex != nil {
			if img = d.tex.ensure(r3); img != nil && d.boxMesh != nil {
				mesh = d.boxMesh.ensure(r3) // tiled UVs (missing texture keeps the plain cube)
			}
		}
		r3.DrawPBR(mesh, d.Transform, d.Color, d.Metallic, d.Roughness, img)
	}
}

// sceneBody is the retained physics mirror of one scene element. sig
// identifies mesh-instance shapes (source|mode|scale) whose Shape structs
// aren't comparable across rebuilds.
type sceneBody struct {
	id    p3.BodyID
	shape p3.Shape
	pos   core.Vec3
	rot   core.Quat
	sig   string
	seen  bool
}

// SceneView is a loaded Scene3D document bound to an Env: Frame() rebuilds
// (or returns the memoized frame), keeping the physics world in sync.
type SceneView struct {
	env       *Env
	root      *node
	world     *p3.World
	bodies    map[string]*sceneBody
	names     map[string]string // element id -> body key (for Body lookup)
	models    map[string]*sceneModel
	texes     map[string]*sceneTex
	boxMeshes map[boxMeshKey]*sceneBoxMesh
	baseDir   string    // MeshInstance source resolution root
	doc       *docState // timers/animators declared in the scene

	cached *SceneFrame
	rev    uint64
	have   bool
}

// LoadScene parses a Scene3D document and binds it to env (declare
// host properties on env first; the document's own `property` declarations
// work like the ui documents').
func LoadScene(src string, env *Env) (*SceneView, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if root.typeName != "Scene3D" {
		return nil, fmt.Errorf("gml: scene root must be Scene3D, got %q", root.typeName)
	}
	v := &SceneView{
		env: env, root: root, world: p3.NewWorld(),
		bodies: map[string]*sceneBody{}, names: map[string]string{},
		models: map[string]*sceneModel{}, texes: map[string]*sceneTex{},
		boxMeshes: map[boxMeshKey]*sceneBoxMesh{},
	}
	// Document-declared properties (reuse the ui declaration pass; scene
	// docs share the same property/binding semantics).
	v.doc = newDocState()
	if err := declareProps(root, env, v.doc); err != nil {
		return nil, err
	}
	declareFuncs(root, env)
	if _, err := v.Frame(); err != nil {
		return nil, err
	}
	return v, nil
}

// LoadSceneFile is LoadScene from a file; the file's directory becomes the
// resolution root for MeshInstance sources.
func LoadSceneFile(path string, env *Env) (*SceneView, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := Parse(string(src))
	if err != nil {
		return nil, err
	}
	if root.typeName != "Scene3D" {
		return nil, fmt.Errorf("gml: scene root must be Scene3D, got %q", root.typeName)
	}
	v := &SceneView{
		env: env, root: root, world: p3.NewWorld(),
		bodies: map[string]*sceneBody{}, names: map[string]string{},
		models: map[string]*sceneModel{}, texes: map[string]*sceneTex{},
		boxMeshes: map[boxMeshKey]*sceneBoxMesh{}, baseDir: filepath.Dir(path),
	}
	v.doc = newDocState()
	if err := declareProps(root, env, v.doc); err != nil {
		return nil, err
	}
	declareFuncs(root, env)
	if _, err := v.Frame(); err != nil {
		return nil, err
	}
	return v, nil
}

// World exposes the physics world the scene populates.
func (v *SceneView) World() *p3.World { return v.world }

// Body resolves an element id to its physics body.
func (v *SceneView) Body(id string) (p3.BodyID, bool) {
	if key, ok := v.names[id]; ok {
		if sb, ok := v.bodies[key]; ok {
			return sb.id, true
		}
	}
	return p3.NilBody, false
}

// Frame rebuilds the scene (memoized: an unchanged property revision
// returns the previous frame and touches nothing).
func (v *SceneView) Frame() (*SceneFrame, error) {
	if rev := core.Revision(); v.have && rev == v.rev {
		return v.cached, nil
	}
	f := &SceneFrame{LightDir: core.V3(-0.5, -1, -0.35)}
	for _, sb := range v.bodies {
		sb.seen = false
	}
	if err := v.buildNode(v.root, core.Vec3{}, core.QuatIdentity(), f); err != nil {
		v.have = false
		return nil, err
	}
	// Remove bodies whose elements disappeared (bindings with visible:).
	for key, sb := range v.bodies {
		if !sb.seen {
			v.world.RemoveBody(sb.id)
			delete(v.bodies, key)
		}
	}
	v.cached, v.rev, v.have = f, core.Revision(), true
	return f, nil
}

// buildNode walks one element, accumulating the parent pose.
func (v *SceneView) buildNode(n *node, ppos core.Vec3, prot core.Quat, f *SceneFrame) error {
	env := v.env
	switch n.typeName {
	case "Scene3D":
		for _, a := range n.attrs {
			switch a.name {
			case "id":
			case "lightDir":
				if vec, err := attrVec3(a, env); err == nil {
					f.LightDir = vec
				}
			default:
				return fmt.Errorf("gml:%d: Scene3D has no attribute %q", a.line, a.name)
			}
		}
		return v.buildChildren(n, ppos, prot, f)
	case "Node3D":
		pos, rot, err := nodePose(n, env)
		if err != nil {
			return err
		}
		return v.buildChildren(n, ppos.Add(prot.Rotate(pos)), prot.Mul(rot), f)
	case "Box", "Sphere", "Cylinder", "Trigger":
		return v.buildSolid(n, ppos, prot, f)
	case "MeshInstance":
		return v.buildMeshInstance(n, ppos, prot, f)
	}
	return fmt.Errorf("gml:%d: Scene3D has no element %q", n.line, n.typeName)
}

func (v *SceneView) buildChildren(n *node, pos core.Vec3, rot core.Quat, f *SceneFrame) error {
	for _, c := range n.children {
		if isNonVisual(c) {
			// Timers and standalone animations tick via Advance, so a
			// scene can self-animate (Timer{onTriggered}, NumberAnimation
			// on a property that positions bind to).
			if c.typeName == "Timer" {
				v.doc.refreshTimer(c, v.env)
			}
			if isAnimation(c.typeName) {
				v.doc.refreshAnimator(c, v.env)
			}
			continue
		}
		if err := v.buildNode(c, pos, rot, f); err != nil {
			return err
		}
	}
	return nil
}

// nodePose reads position / rotationY / rotation ([ax, ay, az, deg]).
func nodePose(n *node, env *Env) (core.Vec3, core.Quat, error) {
	pos := core.Vec3{}
	rot := core.QuatIdentity()
	for _, a := range n.attrs {
		switch a.name {
		case "position":
			p, err := attrVec3(a, env)
			if err != nil {
				return pos, rot, err
			}
			pos = p
		case "rotationY":
			deg, err := attrFloat(a, env)
			if err != nil {
				return pos, rot, err
			}
			rot = core.QuatAxisAngle(core.V3(0, 1, 0), deg*math32.Pi/180)
		case "rotation": // [axisX, axisY, axisZ, degrees]
			vals, err := attrFloats(a, env, 4)
			if err != nil {
				return pos, rot, err
			}
			rot = core.QuatAxisAngle(core.V3(vals[0], vals[1], vals[2]), vals[3]*math32.Pi/180)
		}
	}
	return pos, rot, nil
}

// buildSolid emits the draw and keeps the physics body in sync.
func (v *SceneView) buildSolid(n *node, ppos core.Vec3, prot core.Quat, f *SceneFrame) error {
	env := v.env
	isTrigger := n.typeName == "Trigger"
	collider := true
	visible := !isTrigger // triggers are invisible unless given a color
	var size core.Vec3
	radius, height := float32(0.5), float32(1)
	color := render.RGBA{R: 0.6, G: 0.62, B: 0.66, A: 1}
	metallic, roughness := float32(0.05), float32(0.7)
	layer := p3.Layer(1)
	texture := ""
	uvScale := float32(1)
	colorSet := false

	for _, a := range n.attrs {
		switch a.name {
		case "id", "position", "rotationY", "rotation": // consumed by nodePose
		case "size":
			s, err := attrVec3(a, env)
			if err != nil {
				return err
			}
			size = s
		case "radius":
			r, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			radius = r
		case "height":
			h, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			height = h
		case "color":
			c, err := attrColor(a, env)
			if err != nil {
				return err
			}
			color, colorSet, visible = c, true, true
		case "metallic":
			m, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			metallic = m
		case "roughness":
			r, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			roughness = r
		case "collider":
			b, err := attrBool(a, env)
			if err != nil {
				return err
			}
			collider = b
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
		case "texture":
			if n.typeName != "Box" {
				return fmt.Errorf("gml:%d: texture is Box-only (for now)", a.line)
			}
			s, err := attrString(a, env)
			if err != nil {
				return err
			}
			texture, visible = s, true
		case "uvScale": // texture repeats every uvScale meters
			s, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			uvScale = s
		default:
			return fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}
	lpos, lrot, err := nodePose(n, env)
	if err != nil {
		return err
	}
	pos := ppos.Add(prot.Rotate(lpos))
	rot := prot.Mul(lrot)

	// Shape + draw transform per kind.
	var shape p3.Shape
	var draw SceneDraw
	switch n.typeName {
	case "Box", "Trigger":
		if size == (core.Vec3{}) {
			size = core.V3(1, 1, 1)
		}
		shape = p3.Box(size.Scale(0.5))
		draw = SceneDraw{Kind: DrawBox, Transform: core.Mat4Translate(pos).Mul(rot.Mat4()).Mul(core.Mat4Scale(size))}
	case "Sphere":
		shape = p3.Sphere(radius)
		draw = SceneDraw{Kind: DrawSphere, Transform: core.Mat4Translate(pos).Mul(rot.Mat4()).Mul(core.Mat4Scale(core.V3(radius, radius, radius)))}
	case "Cylinder":
		// Physics approximates a cylinder with a capsule of matching
		// bounds (P0 has no cylinder shape yet).
		hh := math32.Max(height/2-radius, 0)
		shape = p3.Capsule(radius, hh)
		draw = SceneDraw{Kind: DrawCylinder, Transform: core.Mat4Translate(pos).Mul(rot.Mat4()).Mul(core.Mat4Scale(core.V3(radius, height, radius)))}
	}
	draw.Color, draw.Metallic, draw.Roughness = color, metallic, roughness
	if texture != "" {
		draw.tex = v.texCell(texture)
		draw.boxMesh = v.boxMeshCell(size, uvScale)
		if !colorSet {
			// The shader multiplies color into the texture — stay white
			// unless the document explicitly tints.
			draw.Color = render.RGBA{R: 1, G: 1, B: 1, A: 1}
		}
	}
	if visible {
		f.Draws = append(f.Draws, draw)
	}

	// Physics body: create/re-pose the retained mirror.
	if collider || isTrigger {
		key := elemKey(n)
		sb := v.bodies[key]
		if sb != nil && sb.shape != shape {
			v.world.RemoveBody(sb.id) // shape edited (hot value change)
			sb = nil
		}
		if sb == nil {
			sb = &sceneBody{shape: shape, pos: pos, rot: rot}
			if isTrigger {
				sb.id = v.world.AddTrigger(shape, pos, rot, layer)
			} else {
				sb.id = v.world.AddBody(shape, pos, rot, layer)
			}
			v.bodies[key] = sb
			if id := nodeID(n); id != "" {
				v.names[id] = key
			}
		} else if sb.pos != pos || sb.rot != rot {
			v.world.SetPose(sb.id, pos, rot)
			sb.pos, sb.rot = pos, rot
		}
		sb.seen = true
	}
	return nil
}

// attrVec3 evaluates an attribute as [x, y, z].
func attrVec3(a attr, env *Env) (core.Vec3, error) {
	vals, err := attrFloats(a, env, 3)
	if err != nil {
		return core.Vec3{}, err
	}
	return core.V3(vals[0], vals[1], vals[2]), nil
}

// attrFloats evaluates an attribute as a fixed-length number list.
func attrFloats(a attr, env *Env, n int) ([]float32, error) {
	v, err := a.expr.eval(env)
	if err != nil {
		return nil, err
	}
	list, ok := v.([]any)
	if !ok || len(list) != n {
		return nil, fmt.Errorf("gml:%d: %s wants a list of %d numbers", a.line, a.name, n)
	}
	out := make([]float32, n)
	for i, item := range list {
		f, err := toFloat64(item)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: %s[%d]: %v", a.line, a.name, i, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}
