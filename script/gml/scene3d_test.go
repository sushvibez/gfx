package gml

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/render"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/physics/p3"
)

const testScene = `
Scene3D {
    lightDir: [-1, -1, 0]
    property real doorOpen: 0

    Box { size: [20, 0.5, 20]; position: [0, -0.25, 0]; color: "#404448" }
    Node3D { position: [3, 0, 5]; rotationY: 90
        Sphere { id: orb; radius: 0.5; position: [2, 1, 0]; color: "#3080ff"; metallic: 0.8 }
    }
    Cylinder { id: pillar; radius: 0.5; height: 3; position: [-3, 1.5, 0]; color: "#806040" }
    Box { id: door; size: [4, 2.5, 0.5]; position: [0, 1.25 + doorOpen * 2.4, 8]; color: "#994b47" }
    Trigger { id: plate; size: [2, 1, 2]; position: [0, 0.5, 4] }
    Box { size: [1, 1, 1]; position: [5, 0.5, 0]; collider: false; color: "#ffffff" }
}`

func loadTestScene(t *testing.T) (*SceneView, *Env) {
	t.Helper()
	env := NewEnv()
	v, err := LoadScene(testScene, env)
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	return v, env
}

func TestSceneBuild(t *testing.T) {
	v, _ := loadTestScene(t)
	f, err := v.Frame()
	if err != nil {
		t.Fatal(err)
	}
	// Visible draws: floor, orb, pillar, door, no-collider box = 5 (trigger invisible).
	if len(f.Draws) != 5 {
		t.Fatalf("draws = %d, want 5", len(f.Draws))
	}
	if f.LightDir.X != -1 {
		t.Fatalf("lightDir = %+v", f.LightDir)
	}
	// The orb sits under a rotated group at (3,0,5): local (2,1,0) rotated
	// 90° about Y → (0,1,-2)+(3,0,5) = (3,1,3).
	orb := f.Draws[1]
	p := orb.Transform.MulPoint(core.Vec3{})
	if math32.Abs(p.X-3) > 1e-3 || math32.Abs(p.Y-1) > 1e-3 || math32.Abs(p.Z-3) > 1e-3 {
		t.Fatalf("orb at (%v,%v,%v), want (3,1,3)", p.X, p.Y, p.Z)
	}
	if orb.Kind != DrawSphere || orb.Metallic < 0.7 {
		t.Fatalf("orb draw wrong: %+v", orb)
	}
}

func TestScenePhysicsMirror(t *testing.T) {
	v, _ := loadTestScene(t)
	// A ray down onto the floor authored in gml.
	h, ok := v.World().RayCast(core.V3(1, 5, 1), core.V3(0, -1, 0), 100, p3.LayerAll)
	if !ok {
		t.Fatal("floor not hit")
	}
	if math32.Abs(h.T-5) > 1e-3 {
		t.Fatalf("floor hit T = %v, want 5", h.T)
	}
	// The door blocks the corridor.
	if _, ok := v.World().RayCast(core.V3(0, 1, 0), core.V3(0, 0, 1), 100, p3.LayerAll); !ok {
		t.Fatal("door not hit")
	}
	// The no-collider box is render-only.
	if _, ok := v.World().RayCast(core.V3(5, 0.5, -5), core.V3(0, 0, 1), 100, p3.LayerAll); ok {
		t.Fatal("collider:false body exists in physics")
	}
	// Trigger body resolvable by id and invisible to rays (already proven by
	// the door ray passing through z=4 region... assert explicitly).
	if _, ok := v.Body("plate"); !ok {
		t.Fatal("plate trigger not resolvable")
	}
	if _, ok := v.Body("door"); !ok {
		t.Fatal("door body not resolvable")
	}
}

func TestScenePropertyDrivesPose(t *testing.T) {
	v, env := loadTestScene(t)
	doorID, _ := v.Body("door")
	p0, _, _ := v.World().Pose(doorID)
	if math32.Abs(p0.Y-1.25) > 1e-3 {
		t.Fatalf("door starts at y=%v, want 1.25", p0.Y)
	}
	// Open the door via the bound property: the next Frame re-poses the
	// SAME body (retained, not recreated).
	if err := env.vars["doorOpen"].(Ref).set(1.0); err != nil {
		t.Fatal(err)
	}
	f, err := v.Frame()
	if err != nil {
		t.Fatal(err)
	}
	_ = f
	p1, _, ok := v.World().Pose(doorID)
	if !ok {
		t.Fatal("door body was recreated instead of re-posed")
	}
	if math32.Abs(p1.Y-(1.25+2.4)) > 1e-3 {
		t.Fatalf("door y = %v after open, want 3.65", p1.Y)
	}
	// And the ray that hit the closed door now passes.
	if _, ok := v.World().RayCast(core.V3(0, 1, 0), core.V3(0, 0, 1), 100, p3.LayerAll); ok {
		t.Fatal("open door still blocks")
	}
}

func TestSceneMemoization(t *testing.T) {
	v, env := loadTestScene(t)
	f1, _ := v.Frame()
	f2, _ := v.Frame()
	if f1 != f2 {
		t.Fatal("idle frame rebuilt the scene")
	}
	_ = env.vars["doorOpen"].(Ref).set(0.5)
	f3, _ := v.Frame()
	if f3 == f2 {
		t.Fatal("property change did not rebuild")
	}
	f4, _ := v.Frame()
	if f4 != f3 {
		t.Fatal("post-change idle frame rebuilt")
	}
}

// TestSceneSelfAnimates verifies a Scene3D document can animate itself: a
// NumberAnimation drives a property that a body's position binds to, ticked
// entirely by SceneView.Advance — no host code.
func TestSceneSelfAnimates(t *testing.T) {
	env := NewEnv()
	v, err := LoadScene(`
Scene3D {
    property real lift: 0
    NumberAnimation { property: "lift"; from: 0; to: 3; duration: 1000; running: true }
    Box { id: crate; size: [1, 1, 1]; position: [0, 0.5 + lift, 0]; color: "#fff" }
}`, env)
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	crate, _ := v.Body("crate")
	p0, _, _ := v.World().Pose(crate)
	if math32.Abs(p0.Y-0.5) > 1e-3 {
		t.Fatalf("start y = %v, want 0.5", p0.Y)
	}
	// Tick half the clip: lift ≈ eased midpoint (>0, <3); the SAME body has
	// moved with it.
	for i := 0; i < 30; i++ {
		v.Advance(1.0 / 60)
		if _, err := v.Frame(); err != nil {
			t.Fatal(err)
		}
	}
	p1, _, ok := v.World().Pose(crate)
	if !ok {
		t.Fatal("crate body recreated")
	}
	if p1.Y <= 0.6 || p1.Y >= 3.4 {
		t.Fatalf("mid-animation y = %v, want strictly between 0.6 and 3.4", p1.Y)
	}
	// Finish the clip: settles at 0.5+3.
	for i := 0; i < 90; i++ {
		v.Advance(1.0 / 60)
		v.Frame()
	}
	p2, _, _ := v.World().Pose(crate)
	if math32.Abs(p2.Y-3.5) > 1e-2 {
		t.Fatalf("end y = %v, want 3.5", p2.Y)
	}
}

func TestSceneCharacterWalksGmlLevel(t *testing.T) {
	// End to end: the physics character walks geometry authored in gml.
	v, _ := loadTestScene(t)
	c := p3.NewCharacter(v.World(), core.V3(0, 0.9, 0), 0.35, 0.55)
	var tr p3.TriggerEvents
	plate, _ := v.Body("plate")
	entered := false
	for i := 0; i < 240; i++ {
		c.MoveAndSlide(core.V3(0, -12*0.016, 2), 0.016)
		tr.Update(c, p3.LayerAll, func(id p3.BodyID) {
			if id == plate {
				entered = true
			}
		}, nil)
	}
	if !c.OnGround {
		t.Fatal("character lost the gml floor")
	}
	if !entered {
		t.Fatalf("character never entered the gml trigger (z=%v)", c.Pos.Z)
	}
	// The closed door stops the walk before z=8.
	if c.Pos.Z > 7.8-0.35+0.1 {
		t.Fatalf("walked through the closed door: z=%v", c.Pos.Z)
	}
}

// TestSceneTexturedBox: texture decodes at build with world-tiled UVs; a
// missing file soft-falls back to the flat color; non-Box texture errors.
func TestSceneTexturedBox(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wall.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `Scene3D {
	    Box { size: [8, 3, 0.5]; position: [0, 1.5, 4]; texture: "wall.png"; uvScale: 2 }
	    Box { size: [1, 1, 1]; position: [4, 0.5, 0]; texture: "absent.png"; color: "#ff0000" }
	}`
	path := filepath.Join(dir, "level.gml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := LoadSceneFile(path, NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	f, err := v.Frame()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Draws) != 2 {
		t.Fatalf("draws = %d, want 2", len(f.Draws))
	}
	wall := f.Draws[0]
	if wall.tex == nil || wall.tex.pix == nil {
		t.Fatal("wall texture not decoded at build")
	}
	if wall.boxMesh == nil {
		t.Fatal("wall has no tiled mesh")
	}
	// Face +Z spans 8x3 at uvScale 2 -> UV extent 4x1.5.
	var maxU, maxV float32
	for _, vx := range wall.boxMesh.verts[:4] {
		maxU = max(maxU, vx.U)
		maxV = max(maxV, vx.V)
	}
	if maxU != 4 || maxV != 1.5 {
		t.Fatalf("front-face UV extent = %gx%g, want 4x1.5", maxU, maxV)
	}
	if wall.Color != (render.RGBA{R: 1, G: 1, B: 1, A: 1}) {
		t.Fatalf("textured box without color should stay white, got %+v", wall.Color)
	}
	missing := f.Draws[1]
	if missing.tex == nil || missing.tex.pix != nil {
		t.Fatal("absent texture should cache a nil-pix cell")
	}
	if missing.Color.R != 1 || missing.Color.G != 0 {
		t.Fatalf("explicit tint lost: %+v", missing.Color)
	}

	// texture on a Sphere is a load error, not a silent no-op.
	if _, err := LoadScene(`Scene3D { Sphere { texture: "x.png" } }`, NewEnv()); err == nil {
		t.Fatal("Sphere texture should error")
	}
}
