package fbx

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// cqbAsset resolves "models/hostile.fbx" against the Mixamo-style asset
// root named by GFX_FBX_ASSETS; tests skip when it is unset or absent.
func cqbAsset(t *testing.T, rel string) string {
	t.Helper()
	if root := os.Getenv("GFX_FBX_ASSETS"); root != "" {
		p := filepath.Join(root, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("asset absent (set GFX_FBX_ASSETS): %s", rel)
	return ""
}

func loadGLTF(t *testing.T, path string) *gltf.Document {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("asset absent: %v", err)
	}
	d, err := Parse(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	doc, err := ToGLTF(d, func(string) ([]byte, error) { return nil, os.ErrNotExist })
	if err != nil {
		t.Fatalf("convert %s: %v", path, err)
	}
	return doc
}

// TestRetargetPairs is the robustness sweep: every clip must retarget
// onto every character rig purely by bone name, drive a real share of
// that rig's joints, and produce finite transforms at every sampled time.
// The mesh rigs ("mixamorig:") and the clip rigs ("mixamorig6:", and the
// 7700-exporter files) differ, so this exercises Retarget's name matching
// and rest-relative rebasing rather than an index remap.
func TestRetargetPairs(t *testing.T) {
	pairs := []struct{ mesh, clip string }{
		{"models/hostile.fbx", "animations/Idle.fbx"},
		{"models/hostile.fbx", "animations/Stepping Backward.fbx"},
		{"models/hostile.fbx", "animations/Firing Rifle.fbx"},
		{"models/hostile.fbx", "animations/Dying.fbx"},
		{"models/sophie.fbx", "animations/Terrified.fbx"},
	}
	for _, pair := range pairs {
		t.Run(pair.clip, func(t *testing.T) {
			meshDoc := loadGLTF(t, cqbAsset(t, pair.mesh))
			clipDoc := loadGLTF(t, cqbAsset(t, pair.clip))
			if len(clipDoc.Animations) == 0 {
				t.Fatalf("%s has no clips", pair.clip)
			}
			before := 0
			for _, a := range clipDoc.Animations {
				before += len(a.Channels)
			}

			rest := gltf.NewPose(meshDoc.Nodes)
			Retarget(meshDoc, clipDoc)

			after := 0
			for _, a := range clipDoc.Animations {
				after += len(a.Channels)
			}
			if after == 0 {
				t.Fatalf("every channel dropped (%d before): no bone names matched", before)
			}
			if after*4 < before*3 {
				t.Fatalf("only %d/%d channels retargeted — name matching is losing bones", after, before)
			}
			t.Logf("%s → %s: %d/%d channels retargeted onto %d nodes",
				filepath.Base(pair.clip), filepath.Base(pair.mesh), after, before, len(meshDoc.Nodes))

			for ci := range clipDoc.Animations {
				clip := &clipDoc.Animations[ci]
				if clip.Duration <= 0 {
					t.Fatalf("clip %q has zero duration", clip.Name)
				}
				// Every channel must target one of the mesh rig's nodes.
				for _, ch := range clip.Channels {
					if ch.Node < 0 || ch.Node >= len(meshDoc.Nodes) {
						t.Fatalf("channel targets node %d, outside the mesh rig", ch.Node)
					}
				}
				pose := gltf.NewPose(meshDoc.Nodes)
				moved := 0
				for _, f := range []float32{0, 0.17, 0.5, 0.83, 1} {
					pose.Reset(rest)
					clip.SampleInto(f*clip.Duration, pose)
					pose.Write(meshDoc.Nodes)
					for i := range meshDoc.Nodes {
						for _, v := range meshDoc.Nodes[i].Local {
							if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
								t.Fatalf("clip %q at t=%v: node %q has a non-finite transform",
									clip.Name, f*clip.Duration, meshDoc.Nodes[i].Name)
							}
						}
					}
					if f > 0 {
						moved = 0
						for i := range meshDoc.Nodes {
							if !quatNear(meshDoc.Nodes[i].R, rest.R[i], 1e-3) {
								moved++
							}
						}
					}
				}
				t.Logf("clip %q: %.2fs, %d joints moved", clip.Name, clip.Duration, moved)
				if moved < 5 {
					t.Fatalf("clip %q moved only %d joints — retargeting is inert", clip.Name, moved)
				}
			}
			// Rotations must stay unit quaternions through the rebasing —
			// the cheapest tripwire for a retarget that has started
			// accumulating error or blowing up.
			for i := range meshDoc.Nodes {
				q := meshDoc.Nodes[i].R
				l := float64(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)
				if meshDoc.Nodes[i].HasTRS && math.Abs(l-1) > 1e-3 {
					t.Fatalf("node %q rotation is not unit (len2=%v)", meshDoc.Nodes[i].Name, l)
				}
			}
		})
	}
}

func quatNear(a, b core.Quat, eps float32) bool {
	d := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	if d < 0 {
		d = -d
	}
	return 1-d < eps
}
