package gml

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// TestView3DScene: a View3D's markup becomes a scene — camera, light and
// models with composed transforms — read fresh every frame. No GPU is
// involved, so this checks the scene the encoder would draw.
func TestView3DScene(t *testing.T) {
	src := `
Item {
    width: 400; height: 300
    property real lift: 25
    View3D {
        id: view
        width: 400; height: 300
        environment: SceneEnvironment { clearColor: "#101418" }
        PerspectiveCamera { position: Qt.vector3d(0, 200, 300); eulerRotation.x: -30 }
        DirectionalLight { eulerRotation.x: -30; eulerRotation.y: -70; brightness: 0.5 }
        Model {
            position: Qt.vector3d(-100, 0, 0)
            source: "#Cube"
            materials: [ DefaultMaterial { diffuseColor: "#00d1a9" } ]
        }
        Node {
            position: Qt.vector3d(100, 0, 0)
            Model {
                source: "#Sphere"
                position: Qt.vector3d(0, lift, 0)
                materials: [ DefaultMaterial { diffuseColor: "#ffffff" } ]
            }
        }
        Model { source: "#Cone"; visible: false }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)

	var sc *qview3D
	for _, s := range d.doc.scenes {
		sc = s
	}
	if sc == nil {
		t.Fatal("no scene registered for the View3D")
	}
	if sc.clear.R < 0.05 || sc.clear.R > 0.09 {
		t.Errorf("clearColor = %v, want the environment's #101418", sc.clear)
	}
	if sc.camera.pos != core.V3(0, 200, 300) || sc.camera.euler.X != -30 {
		t.Errorf("camera = %v / %v", sc.camera.pos, sc.camera.euler)
	}
	if sc.light.euler.Y != -70 || sc.light.brightness != 0.5 {
		t.Errorf("light = %v brightness %v", sc.light.euler, sc.light.brightness)
	}
	// An invisible Model contributes nothing.
	if len(sc.models) != 2 {
		t.Fatalf("got %d models, want 2 (the invisible one is skipped)", len(sc.models))
	}
	if got := sc.models[0].xform.MulPoint(core.Vec3{}); got != core.V3(-100, 0, 0) {
		t.Errorf("first model at %v, want (-100,0,0)", got)
	}
	// Nested: the Node's position composes with the model's own.
	if got := sc.models[1].xform.MulPoint(core.Vec3{}); got != core.V3(100, 25, 0) {
		t.Errorf("nested model at %v, want (100,25,0) — Node + own position", got)
	}
	if sc.models[0].mesh != "#Cube" || sc.models[1].mesh != "#Sphere" {
		t.Errorf("meshes = %q, %q", sc.models[0].mesh, sc.models[1].mesh)
	}
	if c := sc.models[0].color; c.G < 0.5 || c.R > 0.2 {
		t.Errorf("material colour = %v, want the teal diffuseColor", c)
	}

	// A binding drives the scene: moving `lift` re-poses the nested model.
	if !d.SetProperty("lift", float64(60)) {
		t.Fatal("lift is not a document property")
	}
	u.Layout(400, 300)
	for _, s := range d.doc.scenes {
		sc = s
	}
	if got := sc.models[1].xform.MulPoint(core.Vec3{}); got != core.V3(100, 60, 0) {
		t.Errorf("after the binding changed, nested model at %v, want (100,60,0)", got)
	}
}

// TestView3DAnimatedTransform: `NumberAnimation on eulerRotation.y` drives
// the component as an engine property, so the scene rotates.
func TestView3DAnimatedTransform(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    View3D {
        width: 200; height: 200
        PerspectiveCamera { position: Qt.vector3d(0, 0, 300) }
        Model {
            source: "#Cube"
            NumberAnimation on eulerRotation.y {
                loops: Animation.Infinite
                from: 0; to: 360; duration: 1000
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	var key string
	for k := range d.doc.scenes {
		key = k
	}
	// The animation drives an engine property under the name it targets,
	// and an `on <prop>` animation RUNS BY DEFAULT (QML's
	// property-value-source form) — so a quarter of its 1s loop turns the
	// model 90°, which sends its +X axis to -Z.
	node := findNode(d.root, "Model")
	if node == nil {
		t.Fatal("no Model node")
	}
	if ref := d.doc.synthRefs[elemKey(node)+".eulerRotation.y"]; ref == nil {
		t.Fatal("no property for the animated component")
	}
	for i := 0; i < 5; i++ { // 5 x 50ms slices = 250ms, a quarter turn
		d.doc.advanceAnimators(50)
	}
	core.BumpRevision()
	u.Layout(200, 200)
	sc := d.doc.scenes[key]
	if sc == nil || len(sc.models) != 1 {
		t.Fatal("scene lost its model")
	}
	got := sc.models[0].xform.MulPoint(core.V3(1, 0, 0))
	if got.Z > -0.9 {
		t.Errorf("model +X maps to %v; the eulerRotation.y animation did not apply", got)
	}
}

// A DefaultMaterial with `lighting: DefaultMaterial.NoLighting` draws in
// FLAT colour, and Qt's DefaultMaterial is not a PBR material at all — both
// of which decide how a 3D page reads. The enum was unregistered (so the
// expression failed and the model came out shaded), and every material went
// through the PBR path, which tone-mapped a white sphere to grey.
func TestQtQuick3DMaterialModes(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    View3D {
        anchors.fill: parent
        PerspectiveCamera { position: Qt.vector3d(0, 0, 300) }
        DirectionalLight {}
        Model { position: Qt.vector3d(-60, 0, 0); source: "#Sphere"
                materials: [ DefaultMaterial { lighting: DefaultMaterial.NoLighting
                                               diffuseColor: "#ff0000" } ] }
        Model { position: Qt.vector3d(0, 0, 0); source: "#Sphere"
                materials: [ DefaultMaterial { diffuseColor: "#00ff00" } ] }
        Model { position: Qt.vector3d(60, 0, 0); source: "#Sphere"
                materials: [ PrincipledMaterial { baseColor: "#0000ff" } ] }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	var sc *qview3D
	for _, v := range d.doc.scenes {
		sc = v
	}
	if sc == nil || len(sc.models) != 3 {
		t.Fatalf("scene has %v models, want 3", sc)
	}
	if !sc.models[0].unlit {
		t.Error("NoLighting did not make the first model unlit")
	}
	if sc.models[1].unlit || sc.models[1].pbr {
		t.Error("a plain DefaultMaterial must be lit, and NOT through PBR")
	}
	if !sc.models[2].pbr {
		t.Error("a PrincipledMaterial must shade through PBR")
	}
}

// An animation on a component of a value-type property writes the WHOLE
// property: Qt builds a default-constructed vector carrying only the
// animated component, so the others go to zero. MEASURED against Qt 6.10 —
// a Model at x=200 with a `SequentialAnimation on position.y` renders dead
// centre, and the corpus' 3D page puts its "right" figure in the middle.
func TestValueTypeAnimationClobbersItsSiblings(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    View3D {
        anchors.fill: parent
        PerspectiveCamera { position: Qt.vector3d(0, 0, 300) }
        Model {
            position: Qt.vector3d(200, 0, 50); source: "#Cube"
            SequentialAnimation on position.y {
                loops: Animation.Infinite
                NumberAnimation { from: -50; to: 50; duration: 2000 }
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	var sc *qview3D
	for _, v := range d.doc.scenes {
		sc = v
	}
	if sc == nil || len(sc.models) != 1 {
		t.Fatal("scene lost its model")
	}
	p := sc.models[0].xform.MulPoint(core.V3(0, 0, 0))
	if p.X != 0 || p.Z != 0 {
		t.Errorf("model sits at %v; the animated position.y must zero x and z", p)
	}
}

// SEVERAL animations on components of one value type ALL apply: Qt drives
// them through a value-type proxy that merges their writes. MEASURED — a
// cube with animations on eulerRotation.x and .y renders at both angles
// (281x227 wide, where either alone gives a different silhouette). Only a
// RUNNING animation counts, which is how the corpus' 3D page picks one of
// three float styles on position.y.
func TestValueTypeAnimationsAllApply(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    View3D {
        anchors.fill: parent
        PerspectiveCamera { position: Qt.vector3d(0, 0, 300) }
        Model {
            position: Qt.vector3d(0, 0, 0); source: "#Cube"
            NumberAnimation on eulerRotation.x { from: 90; to: 90; duration: 4000 }
            NumberAnimation on eulerRotation.y { from: 90; to: 90; duration: 4000 }
            NumberAnimation on eulerRotation.z { running: false; from: 45; to: 45; duration: 4000 }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	d.doc.advanceAnimators(125)
	core.BumpRevision()
	u.Layout(200, 200)
	var sc *qview3D
	for _, v := range d.doc.scenes {
		sc = v
	}
	if sc == nil || len(sc.models) != 1 {
		t.Fatal("scene lost its model")
	}
	// X then Y, both 90°: the model's +X axis ends up along -Z... and with
	// only one of them applied it would not.
	got := sc.models[0].xform.MulPoint(core.V3(1, 0, 0))
	if got.Z > -0.9 {
		t.Errorf("model +X maps to %v; both animated components must apply", got)
	}
	// X then Y, both 90°, also sends +Y to +X — and the STOPPED z
	// animation contributes nothing: its 45° would leave +Y half way.
	up := sc.models[0].xform.MulPoint(core.V3(0, 1, 0))
	if up.X < 0.9 {
		t.Errorf("model +Y maps to %v, want +X (both components, and no stopped one)", up)
	}
}
