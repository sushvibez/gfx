package render

import (
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// testClip returns a 1s translation clip on node 0 (x: 0→2) and a model
// with one animated node parenting a second (to check world propagation).
func testClip() (*Model, []gltf.Animation) {
	nodes := []gltf.Node{
		{Name: "root", Mesh: -1, Children: []int{1},
			Local: core.Mat4Identity(), T: core.Vec3{}, R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
		{Name: "child", Mesh: -1,
			Local: core.Mat4Translate(core.V3(0, 1, 0)), T: core.V3(0, 1, 0), R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
	}
	m := &Model{
		nodes: nodes,
		roots: []int{0},
		items: []modelItem{{node: 1, skin: -1, world: core.Mat4Translate(core.V3(0, 1, 0))}},
	}
	clips := []gltf.Animation{{
		Name:     "Move",
		Duration: 1,
		Channels: []gltf.AnimChannel{{
			Node: 0, Path: gltf.PathTranslation,
			Times: []float32{0, 1},
			Vec:   []core.Vec3{{}, {X: 2}},
		}},
	}}
	return m, clips
}

func TestAnimationPlayer(t *testing.T) {
	m, clips := testClip()
	p := NewAnimationPlayer(m, clips)
	if !p.Play("Move") {
		t.Fatal("Play(Move) failed")
	}
	if p.Play("nope") {
		t.Fatal("unknown clip accepted")
	}

	// Advance to the middle: root at x=1, child world = root * (0,1,0).
	p.Advance(0.5)
	w := m.items[0].world.MulPoint(core.Vec3{})
	if math32.Abs(w.X-1) > 1e-4 || math32.Abs(w.Y-1) > 1e-4 {
		t.Fatalf("child world origin = (%v,%v,%v), want (1,1,0)", w.X, w.Y, w.Z)
	}

	// Looping wraps: 0.5 + 0.75 = 1.25 → t=0.25.
	p.Advance(0.75)
	if math32.Abs(p.Position()-0.25) > 1e-4 {
		t.Fatalf("looped position = %v, want 0.25", p.Position())
	}
	if p.Playing() != "Move" {
		t.Fatalf("Playing() = %q, want \"Move\"", p.Playing())
	}

	// Non-looping clamps at the end and stops.
	p.Loop = false
	p.Advance(10)
	if p.Position() != 1 || p.IsPlaying() || !p.Finished() {
		t.Fatalf("non-loop end: pos=%v playing=%v finished=%v, want 1/false/true",
			p.Position(), p.IsPlaying(), p.Finished())
	}
	w = m.items[0].world.MulPoint(core.Vec3{})
	if math32.Abs(w.X-2) > 1e-4 {
		t.Fatalf("end pose x = %v, want 2", w.X)
	}

	// Seek re-poses without playing.
	p.Seek(0.25)
	w = m.items[0].world.MulPoint(core.Vec3{})
	if math32.Abs(w.X-0.5) > 1e-4 {
		t.Fatalf("seek pose x = %v, want 0.5", w.X)
	}
	if p.Duration() != 1 {
		t.Fatalf("duration = %v, want 1", p.Duration())
	}
}

// rotClips builds a model with one rotating joint plus two clips that hold
// it at fixed rotations (0 and 90° about Z), so a crossfade's midpoint is
// checkable exactly: 45°.
func rotClips() (*Model, []gltf.Animation) {
	nodes := []gltf.Node{
		{Name: "mixamorig:Hips", Mesh: -1, Skin: -1, Children: []int{1},
			Local: core.Mat4Identity(), R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
		{Name: "mixamorig:RightHand", Mesh: -1, Skin: -1,
			Local: core.Mat4Translate(core.V3(0, 1, 0)), T: core.V3(0, 1, 0),
			R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
	}
	m := &Model{nodes: nodes, roots: []int{0},
		items: []modelItem{{node: 1, skin: -1, world: core.Mat4Translate(core.V3(0, 1, 0))}}}
	hold := func(name string, deg float32) gltf.Animation {
		q := core.QuatAxisAngle(core.V3(0, 0, 1), deg*math32.Pi/180)
		return gltf.Animation{Name: name, Duration: 1, Channels: []gltf.AnimChannel{{
			Node: 0, Path: gltf.PathRotation,
			Times: []float32{0, 1}, Rot: []core.Quat{q, q},
		}}}
	}
	return m, []gltf.Animation{hold("A", 0), hold("B", 90)}
}

// hipsAngle recovers the root joint's rotation about Z from the child's
// world position (the child sits at (0,1,0) in the root's frame).
func hipsAngle(m *Model) float32 {
	p := m.items[0].world.MulPoint(core.Vec3{})
	return math32.Atan2(-p.X, p.Y) * 180 / math32.Pi
}

// TestCrossfade: a 1s fade from clip A (0°) to clip B (90°) passes through
// 45° at the halfway mark and lands exactly on B when the fade ends.
func TestCrossfade(t *testing.T) {
	m, clips := rotClips()
	p := NewAnimationPlayer(m, clips)
	p.Play("A", PlayOpts{Loop: true})
	if got := hipsAngle(m); math32.Abs(got) > 0.01 {
		t.Fatalf("clip A angle = %v, want 0", got)
	}
	if !p.Play("B", PlayOpts{Loop: true, Fade: 1}) {
		t.Fatal("Play(B) failed")
	}
	// Still fully A at fade start.
	if got := hipsAngle(m); math32.Abs(got) > 0.01 {
		t.Fatalf("fade start angle = %v, want 0", got)
	}
	p.Advance(0.5)
	if fading, w := p.Fading(); !fading || math32.Abs(w-0.5) > 1e-4 {
		t.Fatalf("fade progress = %v (fading=%v), want 0.5", w, fading)
	}
	if got := hipsAngle(m); math32.Abs(got-45) > 0.5 {
		t.Fatalf("midpoint angle = %v, want 45 (slerp halfway)", got)
	}
	p.Advance(0.5)
	if fading, _ := p.Fading(); fading {
		t.Fatal("fade still running past its length")
	}
	if got := hipsAngle(m); math32.Abs(got-90) > 0.01 {
		t.Fatalf("fade end angle = %v, want 90", got)
	}
	if p.Playing() != "B" {
		t.Fatalf("Playing() = %q, want B", p.Playing())
	}
}

// TestFinishedCallback: a non-looping clip stops at its end, reports
// Finished and fires the callback exactly once.
func TestFinishedCallback(t *testing.T) {
	m, clips := rotClips()
	p := NewAnimationPlayer(m, clips)
	var fired []string
	p.SetClipCallbacks(func(name string) { fired = append(fired, name) }, nil)
	p.Play("A", PlayOpts{Loop: false})
	p.Advance(0.5)
	if p.Finished() || len(fired) != 0 {
		t.Fatal("finished early")
	}
	p.Advance(1)
	if !p.Finished() || p.Playing() != "" {
		t.Fatalf("not finished: finished=%v playing=%q", p.Finished(), p.Playing())
	}
	p.Advance(1) // further advances are no-ops
	if len(fired) != 1 || fired[0] != "A" {
		t.Fatalf("OnFinished fired %v, want [A] once", fired)
	}
}

// TestStopFreezesPose: Stop holds the pose it had (no reset to rest) and
// Advance no longer moves it.
func TestStopFreezesPose(t *testing.T) {
	m, clips := rotClips()
	p := NewAnimationPlayer(m, clips)
	p.Play("B", PlayOpts{Loop: true})
	before := hipsAngle(m)
	p.Stop()
	p.Advance(0.4)
	if p.IsPlaying() {
		t.Fatal("stopped player still playing")
	}
	if got := hipsAngle(m); math32.Abs(got-before) > 1e-4 {
		t.Fatalf("frozen pose moved: %v → %v", before, got)
	}
	if p.Current() != "B" {
		t.Fatalf("Current() = %q, want B", p.Current())
	}
	p.Resume()
	if !p.IsPlaying() {
		t.Fatal("Resume did not restart playback")
	}
}

// TestSpeedAndSeek: Speed scales the clip clock; Seek still re-poses.
func TestSpeedAndSeek(t *testing.T) {
	m, clips := testClip()
	p := NewAnimationPlayer(m, clips)
	p.Play("Move", PlayOpts{Loop: true, Speed: 2})
	p.Advance(0.25)
	if math32.Abs(p.Position()-0.5) > 1e-4 {
		t.Fatalf("position at 2x = %v, want 0.5", p.Position())
	}
	p.Seek(0.1)
	if math32.Abs(p.Position()-0.1) > 1e-4 {
		t.Fatalf("seek position = %v", p.Position())
	}
	if w := m.items[0].world.MulPoint(core.Vec3{}); math32.Abs(w.X-0.2) > 1e-4 {
		t.Fatalf("seek pose x = %v, want 0.2", w.X)
	}
}

// TestJointAttachment: JointWorld/JointBySuffix return the ANIMATED joint
// world (the attachment anchor), not the rest pose.
func TestJointAttachment(t *testing.T) {
	m, clips := rotClips()
	p := NewAnimationPlayer(m, clips)
	p.Play("B", PlayOpts{Loop: true}) // hips rotated 90° about Z

	w, ok := m.JointWorld("mixamorig:RightHand")
	if !ok {
		t.Fatal("JointWorld(mixamorig:RightHand) not found")
	}
	// The hand sits at (0,1,0) under a root turned 90° about Z → (-1,0,0).
	got := w.MulPoint(core.Vec3{})
	if math32.Abs(got.X+1) > 1e-3 || math32.Abs(got.Y) > 1e-3 {
		t.Fatalf("animated hand world = (%v,%v,%v), want (-1,0,0)", got.X, got.Y, got.Z)
	}
	if _, ok := m.JointWorld("nope"); ok {
		t.Fatal("unknown joint reported found")
	}
	for _, suffix := range []string{"_RightHand", "RightHand", ":rightHand"} {
		s, ok := m.JointBySuffix(suffix)
		if !ok || s != w {
			t.Fatalf("JointBySuffix(%q) = %v (ok=%v), want the hand world", suffix, s, ok)
		}
	}
	if _, ok := m.JointBySuffix("LeftHand"); ok {
		t.Fatal("LeftHand matched the right hand")
	}
	if names := m.JointNames(); len(names) != 2 {
		t.Fatalf("JointNames = %v", names)
	}
}
