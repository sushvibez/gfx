package gml

// QtQuick3D's View3D: a 3D scene declared in markup and rendered into an
// offscreen target the 2D layout embeds like any other element.
//
//	View3D {
//	    anchors.fill: parent
//	    environment: SceneEnvironment { clearColor: "#101418" }
//	    PerspectiveCamera { position: Qt.vector3d(0, 200, 300); eulerRotation.x: -30 }
//	    DirectionalLight { eulerRotation.x: -30; brightness: 1.0 }
//	    Model {
//	        position: Qt.vector3d(-100, 0, 0)
//	        source: "#Cube"
//	        materials: [ DefaultMaterial { diffuseColor: "#00d1a9" } ]
//	        NumberAnimation on eulerRotation.y { from: 0; to: 360; duration: 4000
//	                                            loops: Animation.Infinite }
//	    }
//	}
//
// The scene is re-read from markup every frame (bindings are live), and each
// transform component an animation drives is an engine property the ordinary
// animation machinery writes — `NumberAnimation on eulerRotation.y` targets
// the name directly.
//
// Built-in meshes (#Cube, #Sphere, #Cylinder, #Cone, #Plane) come from the
// renderer's shared primitives; a mesh FILE source is not loaded here.
// Materials contribute their colour (diffuseColor / baseColor), and one
// directional light lights the scene — SceneEnvironment beyond clearColor,
// shadows, IBL, instancing and the rest of QtQuick3D are accepted and not
// modeled.
//
// The host wires the GPU once (SetView3DGPU) and calls EncodeView3D each
// frame from its Encode3D hook; without that, a View3D lays out as an empty
// box of the right size.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// qview3D is one View3D's frame: what to draw, from where, lit how.
type qview3D struct {
	clear    gpu.Color
	camera   qcamera3D
	light    qlight3D
	models   []qmodel3D
	view     *render.View3D
	renderer *render.Renderer3D
}

type qcamera3D struct {
	pos, euler core.Vec3
	fov        float32 // degrees
	near, far  float32
	ortho      bool
	orthoSize  float32
}

type qlight3D struct {
	euler      core.Vec3
	color      render.RGBA
	ambient    float32
	brightness float32
	// shadows is QtQuick3D's castsShadow, which is FALSE by default: a
	// scene that never asked for shadows must not get them (the cube above
	// the sphere would darken it where Qt leaves it lit).
	shadows bool
}

type qmodel3D struct {
	mesh    string // "#Cube", "#Sphere", …
	xform   core.Mat4
	color   render.RGBA
	unlit   bool
	pbr     bool // a PrincipledMaterial: Qt shades those through PBR
	visible bool
}

// q3DGPU is the host-provided GPU context shared by every View3D.
var q3DGPU struct {
	mu     sync.Mutex
	device *gpu.Device
	queue  *gpu.Queue
	r2     *render.Renderer2D
	format gpu.TextureFormat
	// scenes are the live targets, in creation order: one per declared
	// View3D element, retained as long as its document is. Each REBUILD
	// produces a new qview3D for that element (fresh model transforms), so
	// the entry is replaced rather than appended — keeping the first one
	// froze every animated 3D scene at the frame it was created in.
	scenes []*qview3D
}

// registerScene publishes the scene the encoder should draw for this
// element's viewport, replacing whatever the previous build left.
func registerScene(sc *qview3D) {
	q3DGPU.mu.Lock()
	defer q3DGPU.mu.Unlock()
	for i, prev := range q3DGPU.scenes {
		if prev.view == sc.view {
			q3DGPU.scenes[i] = sc
			return
		}
	}
	q3DGPU.scenes = append(q3DGPU.scenes, sc)
}

// SetView3DGPU gives the engine what it needs to render declared View3D
// scenes: the device and queue, the 2D renderer whose images the viewport
// samples, and the target format. Call it once, from the host's Init.
func SetView3DGPU(device *gpu.Device, queue *gpu.Queue, r2 *render.Renderer2D, format gpu.TextureFormat) {
	q3DGPU.mu.Lock()
	defer q3DGPU.mu.Unlock()
	q3DGPU.device, q3DGPU.queue = device, queue
	q3DGPU.r2, q3DGPU.format = r2, format
}

// q3DReady reports whether the GPU context is set.
func q3DReady() bool {
	q3DGPU.mu.Lock()
	defer q3DGPU.mu.Unlock()
	return q3DGPU.device != nil
}

// buildView3D renders a View3D element: it (re)reads the scene from markup
// and returns the viewport element that samples the rendered frame.
func buildView3D(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	grow := false
	sc := &qview3D{
		clear: gpu.Color{R: 0.075, G: 0.08, B: 0.10, A: 1},
		camera: qcamera3D{
			pos: core.V3(0, 0, 600), fov: 60, near: 10, far: 10000, orthoSize: 600,
		},
		light: qlight3D{
			euler: core.V3(-30, -70, 0), color: render.White,
			ambient: 0.2, brightness: 1,
		},
	}
	for _, a := range n.attrs {
		switch a.name {
		case "environment":
			if a.elem != nil {
				readQ3DEnvironment(a.elem, env, sc)
			}
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "anchors.fill":
			grow = true
		case "id", "camera", "renderMode", "importScene", "clip", "z", "opacity",
			"visible", "enabled", "anchors.margins", "anchors.rightMargin",
			"anchors.leftMargin", "anchors.topMargin", "anchors.bottomMargin",
			"anchors.centerIn", "objectName", "focus":
			// accepted, applied below or not modeled
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: View3D has no attribute %q", a.line, a.name)
		}
	}
	// Read the scene graph. Nodes nest, so transforms compose.
	for _, c := range n.children {
		readQ3DNode(c, env, doc, core.Mat4Identity(), sc)
	}

	// Retain the target and its renderer per element, and hand the scene to
	// the frame encoder.
	if doc.scenes == nil {
		doc.scenes = map[string]*qview3D{}
	}
	key := doc.key(n)
	if prev := doc.scenes[key]; prev != nil {
		sc.view, sc.renderer = prev.view, prev.renderer
	}
	if sc.view == nil && q3DReady() {
		q3DGPU.mu.Lock()
		dev, queue, r2, format := q3DGPU.device, q3DGPU.queue, q3DGPU.r2, q3DGPU.format
		q3DGPU.mu.Unlock()
		// One Renderer3D per viewport: its draw buffer is written per
		// Render, so viewports sharing one would clobber each other.
		r3, err := render.New3D(dev, queue, format)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: View3D: %v", n.line, err)
		}
		sc.view = render.NewView3D(dev, r2, format)
		sc.renderer = r3
	}
	if sc.view != nil {
		registerScene(sc)
	}
	doc.scenes[key] = sc
	if sc.view == nil {
		// No GPU context (a null host, or a pure-2D screenshot): lay out the
		// space so the rest of the page is unaffected.
		box := ui.Div()
		if w > 0 || h > 0 {
			box.Size(w, h)
		}
		if grow {
			box.Grow()
		}
		return box, nil
	}
	sc.view.Clear = sc.clear
	el := ui.Viewport3D(sc.view)
	if w > 0 || h > 0 {
		el.Size(w, h)
	}
	if grow {
		el.Grow()
	}
	return el, nil
}

// readQ3DEnvironment reads the SceneEnvironment properties that matter
// here: the clear colour.
func readQ3DEnvironment(n *node, env *Env, sc *qview3D) {
	for _, a := range n.attrs {
		if a.name != "clearColor" {
			continue
		}
		if c, err := attrColor(a, env); err == nil {
			sc.clear = gpu.Color{R: float64(c.R), G: float64(c.G), B: float64(c.B), A: float64(c.A)}
		}
	}
}

// readQ3DNode reads one node of the scene graph under the transform
// accumulated from its ancestors.
func readQ3DNode(n *node, env *Env, doc *docState, parent core.Mat4, sc *qview3D) {
	switch n.typeName {
	case "PerspectiveCamera", "OrthographicCamera":
		t := readQ3DTransform(n, env, doc)
		sc.camera.pos = parent.MulPoint(t.pos)
		sc.camera.euler = t.euler
		sc.camera.ortho = n.typeName == "OrthographicCamera"
		for _, a := range n.attrs {
			switch a.name {
			case "fieldOfView":
				if f, err := attrFloat(a, env); err == nil && f > 0 {
					sc.camera.fov = f
				}
			case "clipNear":
				if f, err := attrFloat(a, env); err == nil && f > 0 {
					sc.camera.near = f
				}
			case "clipFar":
				if f, err := attrFloat(a, env); err == nil && f > 0 {
					sc.camera.far = f
				}
			case "verticalMagnification", "horizontalMagnification":
				if f, err := attrFloat(a, env); err == nil && f > 0 {
					sc.camera.orthoSize = 600 / f
				}
			}
		}
	case "DirectionalLight", "PointLight", "SpotLight":
		t := readQ3DTransform(n, env, doc)
		sc.light.euler = t.euler
		for _, a := range n.attrs {
			switch a.name {
			case "color":
				if c, err := attrColor(a, env); err == nil {
					sc.light.color = render.RGBA{R: c.R, G: c.G, B: c.B, A: 1}
				}
			case "ambientColor":
				if c, err := attrColor(a, env); err == nil {
					// One scalar ambient term here: take the colour's
					// luminance.
					sc.light.ambient = 0.299*c.R + 0.587*c.G + 0.114*c.B
				}
			case "brightness":
				if f, err := attrFloat(a, env); err == nil {
					sc.light.brightness = f
				}
			case "castsShadow":
				if b, err := attrBool(a, env); err == nil {
					sc.light.shadows = b
				}
			}
		}
	case "Model":
		t := readQ3DTransform(n, env, doc)
		m := qmodel3D{mesh: "#Cube", color: render.White, visible: true}
		for _, a := range n.attrs {
			switch a.name {
			case "source":
				if v, err := a.expr.eval(env); err == nil {
					m.mesh = str(v)
				}
			case "visible":
				if b, err := attrBool(a, env); err == nil {
					m.visible = b
				}
			case "materials", "material":
				m.color, m.unlit, m.pbr = readQ3DMaterials(a, env)
			}
		}
		// A material may also be a child element (`Model { DefaultMaterial
		// {} }` is not valid QML, but a materials LIST of inline elements
		// arrives as one; children are scanned for nested nodes below).
		m.xform = parent.Mul(t.matrix())
		if m.visible {
			sc.models = append(sc.models, m)
		}
	case "Node":
		t := readQ3DTransform(n, env, doc)
		parent = parent.Mul(t.matrix())
	default:
		if isNonVisual(n) {
			doc.refreshNonVisual(n, env)
			return
		}
		return // Not part of the 3D scene (a 2D overlay item, say).
	}
	// Children of a camera/light/model still nest in QtQuick3D.
	for _, c := range n.children {
		if isQ3DNode(c.typeName) {
			readQ3DNode(c, env, doc, parent, sc)
		}
	}
}

// isQ3DNode reports the 3D node types this engine reads.
func isQ3DNode(t string) bool {
	switch t {
	case "Node", "Model", "PerspectiveCamera", "OrthographicCamera",
		"DirectionalLight", "PointLight", "SpotLight":
		return true
	}
	return false
}

// readQ3DMaterials reads a material list's (or single material's) colour,
// whether it is unlit, and whether it is a PBR material. Qt shades a
// PrincipledMaterial through PBR and a DefaultMaterial with plain diffuse,
// which look very different — a white DefaultMaterial sphere is white.
func readQ3DMaterials(a attr, env *Env) (render.RGBA, bool, bool) {
	col, unlit, pbr := render.White, false, false
	mats := []*node{}
	if a.elem != nil {
		mats = append(mats, a.elem)
	}
	// `materials: [ DefaultMaterial { ... } ]` — a list of inline elements.
	if a.expr != nil {
		if v, err := a.expr.eval(env); err == nil {
			if items, ok := v.([]any); ok {
				for _, it := range items {
					if mn, ok := it.(*node); ok {
						mats = append(mats, mn)
					}
				}
			}
		}
	}
	for _, mn := range mats {
		if mn.typeName == "PrincipledMaterial" || mn.typeName == "DefaultMaterial" ||
			mn.typeName == "CustomMaterial" {
			if mn.typeName != "DefaultMaterial" {
				pbr = true
			}
			for _, ma := range mn.attrs {
				switch ma.name {
				case "diffuseColor", "baseColor", "albedoColor":
					if c, err := attrColor(ma, env); err == nil {
						col = render.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
					}
				case "lighting":
					// The enum is a name here (DefaultMaterial.NoLighting)
					// but a page may state Qt's number instead, where
					// NoLighting is 0.
					if v, err := ma.expr.eval(env); err == nil {
						if f, ferr := toFloat64(v); ferr == nil {
							unlit = f == 0
						} else {
							unlit = strings.Contains(str(v), "NoLighting")
						}
					}
				}
			}
		}
	}
	return col, unlit, pbr
}

// q3DTransform is a node's own transform, each component either from markup or
// from the engine property an animation drives.
type q3DTransform struct {
	pos, euler, scale core.Vec3
}

func (t q3DTransform) matrix() core.Mat4 {
	const deg = math32.Pi / 180
	m := core.Mat4Translate(t.pos)
	m = m.Mul(core.Mat4RotateY(t.euler.Y * deg))
	m = m.Mul(core.Mat4RotateX(t.euler.X * deg))
	m = m.Mul(core.Mat4RotateZ(t.euler.Z * deg))
	return m.Mul(core.Mat4Scale(t.scale))
}

// readQ3DTransform reads position/eulerRotation/scale, honouring the animations
// declared on the node: a component an animation targets is an engine
// property, seeded from markup once and written by the animation after that.
func readQ3DTransform(n *node, env *Env, doc *docState) q3DTransform {
	t := q3DTransform{scale: core.V3(1, 1, 1)}
	setVec := func(v *core.Vec3, a attr) {
		if val, err := a.expr.eval(env); err == nil {
			if vec, ok := asQVector3d(val); ok {
				*v = vec
			}
		}
	}
	for _, a := range n.attrs {
		switch a.name {
		case "position":
			setVec(&t.pos, a)
		case "eulerRotation":
			setVec(&t.euler, a)
		case "scale":
			setVec(&t.scale, a)
		case "x", "position.x":
			if f, err := attrFloat(a, env); err == nil {
				t.pos.X = f
			}
		case "y", "position.y":
			if f, err := attrFloat(a, env); err == nil {
				t.pos.Y = f
			}
		case "z", "position.z":
			if f, err := attrFloat(a, env); err == nil {
				t.pos.Z = f
			}
		case "eulerRotation.x":
			if f, err := attrFloat(a, env); err == nil {
				t.euler.X = f
			}
		case "eulerRotation.y":
			if f, err := attrFloat(a, env); err == nil {
				t.euler.Y = f
			}
		case "eulerRotation.z":
			if f, err := attrFloat(a, env); err == nil {
				t.euler.Z = f
			}
		case "scale.x":
			if f, err := attrFloat(a, env); err == nil {
				t.scale.X = f
			}
		case "scale.y":
			if f, err := attrFloat(a, env); err == nil {
				t.scale.Y = f
			}
		case "scale.z":
			if f, err := attrFloat(a, env); err == nil {
				t.scale.Z = f
			}
		}
	}
	// Animated components: bind each one as a property under the name the
	// animation targets, run the animations, then read the values back.
	comps := map[string]*float32{
		"position.x": &t.pos.X, "position.y": &t.pos.Y, "position.z": &t.pos.Z,
		"eulerRotation.x": &t.euler.X, "eulerRotation.y": &t.euler.Y,
		"eulerRotation.z": &t.euler.Z,
		"scale.x":         &t.scale.X, "scale.y": &t.scale.Y, "scale.z": &t.scale.Z,
	}
	var anims []*node
	targeted := map[string]bool{}
	var order []*node // declaration order, which decides who wins a group
	for _, c := range n.children {
		if !isAnimation(c.typeName) {
			continue
		}
		anims = append(anims, c)
		if c.onTarget != "" {
			targeted[c.onTarget] = true
			order = append(order, c)
		}
	}
	if len(anims) == 0 {
		return t
	}
	// An animation `on position.y` (or on any component of a value type)
	// writes the WHOLE property: Qt builds a default-constructed vector
	// carrying only the animated component, so the others go to ZERO.
	// MEASURED against Qt 6.10: a Model at x=200 with a
	// `SequentialAnimation on position.y` renders at x=0 — dead centre —
	// and an animated eulerRotation.y drops a stated eulerRotation.x tilt.
	// (It collapses a scale the same way, which Qt also shows: a model
	// with an animated scale.y disappears.)
	//
	// SEVERAL animations on components of one value type all apply, though:
	// Qt drives them through a value-type proxy that merges their writes
	// (MEASURED: a cube with animations on eulerRotation.x AND .y renders
	// at both angles, 281x227 where either alone gives a different
	// silhouette). Only an animation that is RUNNING counts — the 3D page
	// declares three float styles on position.y and runs the one its combo
	// box selects.
	zero := map[string]bool{}
	for _, c := range order {
		if a := doc.animators[animatorKey(c)]; a != nil && !a.running() {
			continue
		}
		if group, _, ok := strings.Cut(c.onTarget, "."); ok {
			zero[group] = true
		}
	}
	for group := range zero {
		switch group {
		case "position":
			t.pos = core.Vec3{}
		case "eulerRotation":
			t.euler = core.Vec3{}
		case "scale":
			t.scale = core.Vec3{}
		}
	}
	key := doc.key(n)
	scope := env.child()
	refs := map[string]Ref{}
	for name := range targeted {
		p, ok := comps[name]
		if !ok {
			continue
		}
		ref := doc.floatProp(key+"."+name, *p)
		refs[name] = ref
		scope.vars[name] = ref
	}
	for _, c := range anims {
		doc.refreshAnimator(c, scope)
	}
	// Only the winner of each group survives, in declaration order — the
	// losers were zeroed above and stay there.
	for name, ref := range refs {
		if f, err := toFloat32(ref.get()); err == nil {
			*comps[name] = f
		}
	}
	return t
}

// asQVector3d reads a Qt.vector3d() value (a map with x/y/z, which is how the
// engine represents one) or a plain number triple.
func asQVector3d(v any) (core.Vec3, bool) {
	switch t := v.(type) {
	case map[string]any:
		x, _ := toFloat32(t["x"])
		y, _ := toFloat32(t["y"])
		z, _ := toFloat32(t["z"])
		return core.V3(x, y, z), true
	case []any:
		var out core.Vec3
		if len(t) > 0 {
			out.X, _ = toFloat32(t[0])
		}
		if len(t) > 1 {
			out.Y, _ = toFloat32(t[1])
		}
		if len(t) > 2 {
			out.Z, _ = toFloat32(t[2])
		}
		return out, true
	}
	return core.Vec3{}, false
}

// EncodeView3D renders every View3D declared in any loaded document into
// its offscreen target. Call it from the host's Encode3D hook, before the 2D
// pass that samples the results.
func EncodeView3D(enc *gpu.CommandEncoder) error {
	q3DGPU.mu.Lock()
	scenes := append([]*qview3D{}, q3DGPU.scenes...)
	q3DGPU.mu.Unlock()
	for _, sc := range scenes {
		if err := sc.encode(enc); err != nil {
			return err
		}
	}
	return nil
}

// encode records one scene's frame.
func (sc *qview3D) encode(enc *gpu.CommandEncoder) error {
	if sc.view == nil || sc.renderer == nil {
		return nil
	}
	w, h := sc.view.Size()
	if w <= 0 || h <= 0 {
		return nil // not laid out yet
	}
	const deg = math32.Pi / 180
	// Camera: QtQuick3D looks down -Z from the camera's own rotation.
	rot := core.Mat4RotateY(sc.camera.euler.Y * deg).
		Mul(core.Mat4RotateX(sc.camera.euler.X * deg)).
		Mul(core.Mat4RotateZ(sc.camera.euler.Z * deg))
	fwd := rot.MulPoint(core.V3(0, 0, -1))
	up := rot.MulPoint(core.V3(0, 1, 0))
	eye := sc.camera.pos
	view := core.Mat4LookAt(eye, eye.Add(fwd), up)
	aspect := float32(w) / float32(h)
	var proj core.Mat4
	if sc.camera.ortho {
		half := sc.camera.orthoSize / 2
		proj = core.Mat4Ortho(-half*aspect, half*aspect, -half, half, sc.camera.near, sc.camera.far)
	} else {
		proj = core.Mat4Perspective(sc.camera.fov*deg, aspect, sc.camera.near, sc.camera.far)
	}
	// Light direction: the direction a light rotated by its euler angles
	// points (QtQuick3D's default direction is -Z).
	lrot := core.Mat4RotateY(sc.light.euler.Y * deg).
		Mul(core.Mat4RotateX(sc.light.euler.X * deg))
	lightDir := lrot.MulPoint(core.V3(0, 0, -1)).Normalize()

	r3 := sc.renderer
	r3.Begin(proj.Mul(view), lightDir, eye)
	ambient := sc.light.ambient
	for _, m := range sc.models {
		mesh, base := sc.meshFor(m.mesh)
		if mesh == nil {
			continue
		}
		// QtQuick3D's built-in meshes are 100 units across (a #Sphere has
		// radius 50); the shared primitives here are unit-sized, so each
		// carries the scale that makes markup coordinates mean the same
		// thing they do in Qt.
		xform := m.xform.Mul(base)
		c := m.color
		if b := sc.light.brightness; b != 1 && b >= 0 {
			// The renderer takes no light intensity, so brightness scales
			// the surface colour — same visible effect for these scenes.
			c = render.RGBA{R: c.R * b, G: c.G * b, B: c.B * b, A: c.A}
		}
		switch {
		case m.unlit:
			r3.DrawUnlit(mesh, xform, c)
		case m.pbr:
			r3.DrawShaded(mesh, xform, c, ambient, nil)
		default:
			// A DefaultMaterial is Qt's NON-PBR material: diffuse only, in
			// display space. Shading it through the PBR path tone-mapped
			// every colour down (a white sphere came out grey).
			r3.DrawLambert(mesh, xform, c, ambient, sc.light.shadows)
		}
	}
	return sc.view.Render(enc, r3)
}

// meshFor maps a QtQuick3D built-in mesh name to a shared primitive plus the
// scale that gives it Qt's built-in size: 100 units across, a #Sphere of
// radius 50.
func (sc *qview3D) meshFor(name string) (*render.Mesh, core.Mat4) {
	all100 := core.Mat4Scale(core.V3(100, 100, 100))
	switch name {
	case "#Sphere":
		return sc.renderer.SphereMesh(), core.Mat4Scale(core.V3(50, 50, 50))
	case "#Cylinder":
		// Unit cylinder: radius 1, height 1.
		return sc.renderer.CylinderMesh(), core.Mat4Scale(core.V3(50, 100, 50))
	case "#Cone":
		return sc.renderer.ConeMesh(), core.Mat4Scale(core.V3(50, 100, 50))
	case "#Plane", "#Rectangle":
		return sc.renderer.PlaneMesh(), all100
	case "#Cube", "":
		return sc.renderer.CubeMesh(), all100
	}
	// A mesh FILE (.mesh/.gltf) is not loaded here; a cube stands in so the
	// scene still reads.
	return sc.renderer.CubeMesh(), all100
}
