package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// animatedGLTF builds a minimal document: one node (no mesh needed for
// animation parsing) with a 1s clip — rotation 0→180° about Y (two keys)
// and translation X 0→2 (three keys, to exercise segment search).
func animatedGLTF(t *testing.T, interpolation string) []byte {
	t.Helper()
	f32 := func(vals ...float32) []byte {
		b := make([]byte, len(vals)*4)
		for i, v := range vals {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		return b
	}
	// Buffer layout: rotTimes(2) | rotVals(2x4) | traTimes(3) | traVals(3x3)
	rotTimes := f32(0, 1)
	rotVals := f32(0, 0, 0, 1 /*identity*/, 0, 1, 0, 0 /*180° about Y*/)
	traTimes := f32(0, 0.5, 1)
	traVals := f32(0, 0, 0, 1, 0, 0, 2, 0, 0)
	buf := append(append(append(append([]byte{}, rotTimes...), rotVals...), traTimes...), traVals...)

	o1 := len(rotTimes)
	o2 := o1 + len(rotVals)
	o3 := o2 + len(traTimes)
	doc := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": "spinner", "translation": [0,0,0]}],
  "animations": [{
    "name": "Spin",
    "channels": [
      {"sampler": 0, "target": {"node": 0, "path": "rotation"}},
      {"sampler": 1, "target": {"node": 0, "path": "translation"}}
    ],
    "samplers": [
      {"input": 0, "output": 1, "interpolation": %q},
      {"input": 2, "output": 3, "interpolation": %q}
    ]
  }],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 2, "type": "SCALAR"},
    {"bufferView": 1, "componentType": 5126, "count": 2, "type": "VEC4"},
    {"bufferView": 2, "componentType": 5126, "count": 3, "type": "SCALAR"},
    {"bufferView": 3, "componentType": 5126, "count": 3, "type": "VEC3"}
  ],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d}
  ],
  "buffers": [{"byteLength": %d, "uri": "data:application/octet-stream;base64,%s"}]
}`, interpolation, interpolation,
		len(rotTimes), o1, len(rotVals), o2, len(traTimes), o3, len(traVals),
		len(buf), base64.StdEncoding.EncodeToString(buf))
	return []byte(doc)
}

func TestParseAnimation(t *testing.T) {
	doc, err := Parse(animatedGLTF(t, "LINEAR"), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Animations) != 1 {
		t.Fatalf("animations = %d, want 1", len(doc.Animations))
	}
	a := doc.Animations[0]
	if a.Name != "Spin" || len(a.Channels) != 2 {
		t.Fatalf("clip = %q with %d channels, want Spin/2", a.Name, len(a.Channels))
	}
	if a.Duration != 1 {
		t.Fatalf("duration = %v, want 1", a.Duration)
	}
	if a.Channels[0].Path != PathRotation || len(a.Channels[0].Rot) != 2 {
		t.Fatalf("channel 0 wrong: %+v", a.Channels[0])
	}
	if a.Channels[1].Path != PathTranslation || len(a.Channels[1].Vec) != 3 {
		t.Fatalf("channel 1 wrong: %+v", a.Channels[1])
	}
}

func TestAnimationApplySampling(t *testing.T) {
	doc, err := Parse(animatedGLTF(t, "LINEAR"), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := doc.Animations[0]

	// t=0.5: rotation halfway (90° about Y), translation at key 2 exactly (x=1).
	a.Apply(0.5, doc.Nodes)
	n := doc.Nodes[0]
	if math32.Abs(n.T.X-1) > 1e-4 {
		t.Fatalf("T.X = %v at t=0.5, want 1", n.T.X)
	}
	// A 90° Y rotation maps +Z to +X: local * (0,0,1) ≈ (1,0,0)+T.
	p := n.Local.MulPoint(core.V3(0, 0, 1))
	if math32.Abs(p.X-(n.T.X+1)) > 1e-3 || math32.Abs(p.Z) > 1e-3 {
		t.Fatalf("rotation at t=0.5 wrong: +Z -> (%v,%v,%v)", p.X, p.Y, p.Z)
	}

	// t=0.75: translation interpolates within the second segment (x=1.5).
	a.Apply(0.75, doc.Nodes)
	if x := doc.Nodes[0].T.X; math32.Abs(x-1.5) > 1e-4 {
		t.Fatalf("T.X = %v at t=0.75, want 1.5", x)
	}

	// Clamps: before the first key and past the last.
	a.Apply(-1, doc.Nodes)
	if x := doc.Nodes[0].T.X; x != 0 {
		t.Fatalf("T.X = %v at t<0, want 0", x)
	}
	a.Apply(5, doc.Nodes)
	if x := doc.Nodes[0].T.X; x != 2 {
		t.Fatalf("T.X = %v past the end, want 2", x)
	}
}

func TestAnimationStepInterpolation(t *testing.T) {
	doc, err := Parse(animatedGLTF(t, "STEP"), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := doc.Animations[0]
	// STEP holds the previous key: at t=0.75 translation stays at key 2 (x=1).
	a.Apply(0.75, doc.Nodes)
	if x := doc.Nodes[0].T.X; math32.Abs(x-1) > 1e-4 {
		t.Fatalf("STEP T.X = %v at t=0.75, want 1 (held)", x)
	}
	// And the rotation holds identity before the last key.
	p := doc.Nodes[0].Local.MulPoint(core.V3(0, 0, 1))
	if math32.Abs(p.Z-1) > 1e-3 {
		t.Fatalf("STEP rotation moved early: +Z -> (%v,%v,%v)", p.X, p.Y, p.Z)
	}
}

// TestPoseSampleAndBlend covers the buffer-form sampling the crossfading
// player uses: Reset seeds the rest pose, SampleInto marks and writes only
// the channels' nodes, BlendPose mixes two poses, and Write pushes the
// touched nodes back.
func TestPoseSampleAndBlend(t *testing.T) {
	nodes := []Node{
		{Name: "a", Mesh: -1, Skin: -1, T: core.V3(0, 0, 0), R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
		{Name: "b", Mesh: -1, Skin: -1, T: core.V3(9, 0, 0), R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
	}
	rest := NewPose(nodes)
	clipA := Animation{Name: "A", Duration: 1, Channels: []AnimChannel{{
		Node: 0, Path: PathTranslation, Times: []float32{0, 1},
		Vec: []core.Vec3{{X: 0}, {X: 10}},
	}}}
	clipB := Animation{Name: "B", Duration: 1, Channels: []AnimChannel{{
		Node: 0, Path: PathTranslation, Times: []float32{0, 1},
		Vec: []core.Vec3{{X: 100}, {X: 100}},
	}}}

	a, b, out := NewPose(nodes), NewPose(nodes), NewPose(nodes)
	a.Reset(rest)
	clipA.SampleInto(0.5, a)
	if a.T[0].X != 5 {
		t.Fatalf("sampled x = %v, want 5", a.T[0].X)
	}
	if !a.Touched[0] || a.Touched[1] {
		t.Fatalf("touched marks = %v, want [true false]", a.Touched)
	}
	// Untouched nodes keep the rest value the Reset seeded.
	if a.T[1].X != 9 {
		t.Fatalf("untouched node drifted: %v", a.T[1].X)
	}

	b.Reset(rest)
	clipB.SampleInto(0.5, b)
	BlendPose(out, a, b, 0.25)
	if got := out.T[0].X; got < 28.7 || got > 28.8 {
		t.Fatalf("blended x = %v, want 28.75", got)
	}
	BlendPose(out, a, b, 2) // weights clamp
	if out.T[0].X != 100 {
		t.Fatalf("clamped blend = %v, want 100", out.T[0].X)
	}

	out.Write(nodes)
	if nodes[0].Local[12] != 100 {
		t.Fatalf("Write did not update the local matrix: %v", nodes[0].Local[12])
	}
	if nodes[1].T.X != 9 {
		t.Fatal("Write touched a node no channel drives")
	}
}
