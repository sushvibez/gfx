//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// TestGraphOrderingCullingPixels declares passes out of order, includes a
// dead pass, and verifies: dependency-correct execution, culling, and the
// offscreen result actually feeding the final image.
func TestGraphOrderingCullingPixels(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 100, 100

	final := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer final.Release()
	finalView := final.CreateView()
	defer finalView.Release()

	// Separate renderers per pass: one Begin/Flush per submission each.
	rScene, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer rScene.Release()
	rFinal, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer rFinal.Release()
	rDead, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer rDead.Release()

	g := NewGraph(device, queue)
	defer g.Release()

	for frame := range 2 { // run twice to exercise the transient pool
		scene := g.Texture("scene", W, H)
		deadTex := g.Texture("dead", W, H)
		deadRan := false

		// Resources a pass records must stay alive until Execute submits.
		var frameImages []*Image

		// Declared FIRST but depends on scene: the graph must reorder.
		g.PassToTarget("final", gpu.Color{A: 1}, func(p *gpu.RenderPass) {
			img := rFinal.ImageFromView(scene.View(), W, H)
			frameImages = append(frameImages, img)
			rFinal.Begin(W, H)
			rFinal.DrawImage(img, 0, 0, W, H, White)
			if err := rFinal.Flush(p); err != nil {
				t.Error(err)
			}
		}, scene)

		g.Pass("scene", scene, gpu.Color{A: 1}, func(p *gpu.RenderPass) {
			rScene.Begin(W, H)
			rScene.FillRect(20, 20, 60, 60, RGBA{R: 1, A: 1})
			if err := rScene.Flush(p); err != nil {
				t.Error(err)
			}
		})

		// Writes a texture nobody reads: must be culled.
		g.Pass("dead", deadTex, gpu.Color{A: 1}, func(p *gpu.RenderPass) {
			deadRan = true
		})

		if err := g.Execute(finalView); err != nil {
			t.Fatalf("Execute (frame %d): %v", frame, err)
		}
		for _, img := range frameImages {
			img.Release()
		}
		if deadRan {
			t.Fatal("culled pass executed")
		}

		pix, err := device.ReadTexture(queue, final, W, H)
		if err != nil {
			t.Fatalf("ReadTexture: %v", err)
		}
		at := func(x, y int) byte { return pix[(y*W+x)*4] } // red channel
		if at(50, 50) < 250 {
			t.Fatalf("frame %d: offscreen scene did not reach the final target (r=%d)", frame, at(50, 50))
		}
		if at(5, 5) > 5 {
			t.Fatalf("frame %d: background not clear (r=%d)", frame, at(5, 5))
		}
	}
}

// TestGraphCycleError: two passes reading each other must error, not hang.
func TestGraphCycleError(t *testing.T) {
	device, queue := headlessDevice(t)
	g := NewGraph(device, queue)
	defer g.Release()

	a := g.Texture("a", 8, 8)
	b := g.Texture("b", 8, 8)
	g.Pass("A", a, gpu.Color{}, func(*gpu.RenderPass) {}, b)
	g.Pass("B", b, gpu.Color{}, func(*gpu.RenderPass) {}, a)
	// A presented pass depending on the cycle forces both to be kept.
	g.PassToTarget("final", gpu.Color{}, func(*gpu.RenderPass) {}, a)

	final := device.CreateRenderTexture(8, 8, gpu.FormatRGBA8Unorm)
	defer final.Release()
	view := final.CreateView()
	defer view.Release()
	if err := g.Execute(view); err == nil {
		t.Fatal("expected cycle error")
	}
}
