//go:build !js

package render

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// render3D draws the given draw list headlessly and returns the pixels.
func render3D(t *testing.T, draw func(r *Renderer3D)) []byte {
	t.Helper()
	device, queue := headlessDevice(t)
	const W, H = 240, 240

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	depthTex := device.CreateDepthTexture(W, H)
	defer depthTex.Release()
	depthView := depthTex.CreateView()
	defer depthView.Release()

	r, err := New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New3D: %v", err)
	}
	defer r.Release()
	draw(r)

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, gpu.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, 240, 240)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	return pix
}

const testW = 240

func camera() (core.Mat4, core.Vec3) {
	cam := core.V3(2.2, 1.8, 3.5)
	viewM := core.Mat4LookAt(cam, core.V3(0, 0, 0), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math.Pi/3, 1, 0.1, 100)
	return proj.Mul(viewM), cam
}

// TestRenderer3DOcclusion: a small near cube sits between the camera and a
// big far cube; whichever order they are drawn in, the near cube's color
// must win at the center — that is the depth buffer working.
func TestRenderer3DOcclusion(t *testing.T) {
	vp, cam := camera()
	near := core.Mat4Translate(core.V3(1.1, 0.9, 1.75)) // on the camera ray
	far := core.Mat4Scale(core.V3(2, 2, 2))

	for name, order := range map[string][2]bool{"near-first": {true, false}, "far-first": {false, true}} {
		pix := render3D(t, func(r *Renderer3D) {
			cube := r.CubeMesh()
			r.Begin(vp, core.V3(-0.5, -1, -0.6), cam)
			if order[0] {
				r.Draw(cube, near, RGBA{G: 1, A: 1})
				r.Draw(cube, far, RGBA{R: 1, A: 1})
			} else {
				r.Draw(cube, far, RGBA{R: 1, A: 1})
				r.Draw(cube, near, RGBA{G: 1, A: 1})
			}
		})
		i := (120*testW + 120) * 4
		cr, cg := pix[i], pix[i+1]
		if cg < 50 || cr > cg {
			t.Errorf("%s: center = r%d g%d, want green near cube on top", name, cr, cg)
		}
	}
}

// TestRenderer3DLightingAndCoverage: an angled camera sees three faces of
// one cube; directional lighting must shade them differently, and the
// background must stay clear.
func TestRenderer3DLightingAndCoverage(t *testing.T) {
	vp, cam := camera()
	pix := render3D(t, func(r *Renderer3D) {
		cube := r.CubeMesh()
		r.Begin(vp, core.V3(-0.5, -1, -0.6), cam)
		r.Draw(cube, core.Mat4Scale(core.V3(2, 2, 2)), RGBA{R: 1, A: 1})
	})

	// Background corner: 0.1 clear ≈ 25.
	if d := int(pix[(5*testW+5)*4]) - 25; d < -2 || d > 2 {
		t.Errorf("background r = %d, want ~25", pix[(5*testW+5)*4])
	}

	// Collect the red levels of cube pixels (red-dominant, non-background).
	var reds []int
	for y := range testW {
		for x := range testW {
			i := (y*testW + x) * 4
			r, g := int(pix[i]), int(pix[i+1])
			if r > 40 && g < r/2 {
				reds = append(reds, r)
			}
		}
	}
	if len(reds) < 2000 {
		t.Fatalf("cube covers only %d pixels — projection or culling broken", len(reds))
	}
	lo, hi := 255, 0
	for _, v := range reds {
		lo, hi = min(lo, v), max(hi, v)
	}
	// Tone mapping compresses contrast; ~25 levels still separates the
	// three visible faces' light angles.
	if hi-lo < 25 {
		t.Errorf("lighting flat: red range %d..%d across faces", lo, hi)
	}
}

// TestRenderer3DShadow: a cube floats above a large plane with the light
// nearly overhead — the plane under the cube must be darker than the open
// plane, by the shadow pass.
func TestRenderer3DShadow(t *testing.T) {
	// Camera looks down at the plane from the +z side.
	cam := core.V3(0, 5, 7)
	viewM := core.Mat4LookAt(cam, core.V3(0, 0, 0), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math.Pi/3, 1, 0.1, 100)
	vp := proj.Mul(viewM)

	pix := render3D(t, func(r *Renderer3D) {
		cube := r.CubeMesh()
		r.SetShadowArea(core.V3(0, 0, 0), 8)
		r.Begin(vp, core.V3(-0.15, -1, -0.1), cam) // nearly straight down
		// Ground: a wide flat cube; top surface at y=0.
		r.Draw(cube, core.Mat4Scale(core.V3(12, 0.2, 12)).Mul(core.Mat4Translate(core.V3(0, -0.5, 0))), RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1})
		// Caster floating above the origin.
		r.Draw(cube, core.Mat4Translate(core.V3(0, 1.5, 0)), RGBA{R: 0.9, G: 0.2, B: 0.2, A: 1})
	})

	lum := func(x, y int) int {
		i := (y*testW + x) * 4
		return int(pix[i]) + int(pix[i+1]) + int(pix[i+2])
	}
	// The cube sits at the upper screen center; its shadow patch lands on
	// the ground just below it (~y 105..130). Compare against open ground.
	shadowed := lum(115, 118)
	open := lum(40, 200)
	open2 := lum(200, 200)
	if shadowed >= open-60 || shadowed >= open2-60 {
		dumpPNG(t, pix, testW, testW, "shadow-debug")
		t.Fatalf("no shadow: under-cube=%d open=%d/%d", shadowed, open, open2)
	}
}

// dumpPNG writes test pixels for eyeball debugging.
func dumpPNG(t *testing.T, pix []byte, w, h int, name string) {
	t.Helper()
	img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	path := filepath.Join(os.TempDir(), "gfx-"+name+".png")
	if f, err := os.Create(path); err == nil {
		_ = png.Encode(f, img)
		f.Close()
		t.Logf("debug image: %s", path)
	}
}

// A sphere's triangles must be wound counter-clockwise seen from OUTSIDE,
// or back-face culling keeps the FAR hemisphere and its inward normals
// shade the sphere as though lit from behind. Every sphere in a 3D scene
// read flat until this was fixed, and a white QtQuick3D sphere came out
// grey.
func TestSphereMeshWindingFacesOutward(t *testing.T) {
	var verts []Vertex3D
	var idx []uint32
	// The same parametrisation buildSphereMesh uses, without a GPU.
	const segs, rings = 8, 6
	for ri := 0; ri <= rings; ri++ {
		phi := math.Pi * float64(ri) / rings
		sp, cp := math.Sin(phi), math.Cos(phi)
		for si := 0; si <= segs; si++ {
			th := 2 * math.Pi * float64(si) / segs
			st, ct := math.Sin(th), math.Cos(th)
			n := core.V3(float32(sp*ct), float32(cp), float32(sp*st))
			verts = append(verts, Vertex3D{Pos: n, Normal: n})
		}
	}
	stride := uint32(segs + 1)
	for ri := 0; ri < rings; ri++ {
		for si := 0; si < segs; si++ {
			a := uint32(ri)*stride + uint32(si)
			idx = append(idx, a, a+1, a+stride, a+1, a+stride+1, a+stride)
		}
	}
	checked := 0
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := verts[idx[i]].Pos, verts[idx[i+1]].Pos, verts[idx[i+2]].Pos
		// Degenerate at the poles, where two corners coincide.
		if a.Sub(b).Length() < 1e-4 || b.Sub(c).Length() < 1e-4 || a.Sub(c).Length() < 1e-4 {
			continue
		}
		face := b.Sub(a).Cross(c.Sub(a)).Normalize()
		out := a.Add(b).Add(c).Normalize() // the outward direction there
		if face.Dot(out) <= 0 {
			t.Fatalf("triangle %d faces inward: face %v vs outward %v", i/3, face, out)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no non-degenerate triangles checked")
	}
}
