//go:build !js

package render

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// taaScene is floorAndBox plus a fence of thin things in front of the box:
// sixteen wires and poles about a third of a pixel wide at 480×320 (seen
// from logDepthView, a pixel there is about 3.5 cm), at slants that alias
// into broken dashes in a single sample per pixel.
func taaScene() []float32 {
	v := floorAndBox()
	quad := gridQuad(&v, 12)
	eye := core.V3(0, 5, 9)
	const width = 0.012
	wire := func(a, b core.Vec3) {
		o := b.Sub(a).Cross(eye.Sub(a)).Normalize().Scale(width / 2)
		quad(a.Sub(o), b.Sub(o), b.Add(o), a.Add(o))
	}
	for i := 0; i < 8; i++ {
		fi := float32(i)
		wire(core.V3(-5, 0.5+0.38*fi, 2.2-0.1*fi), core.V3(5, 0.9+0.47*fi, 2.2-0.1*fi))
		wire(core.V3(-4.2+1.15*fi, 0, 2.6), core.V3(-3.9+1.2*fi, 2.6+0.25*fi, 2.6))
	}
	return v
}

// taaFrames draws taaScene (bright things on a dark floor) from each view
// in turn with TAA on or off and returns the last frame. The grade is
// neutral: a split tone would tint a wire's faint TAA pixels unlike the
// supersampled reference's bright ones.
func taaFrames(t *testing.T, w, h int, logDepth, taa bool, views []core.Mat4, frame ...func(*Post, int)) []byte {
	t.Helper()
	return postFrames(t, w, h, logDepth, 2, taaScene(), func(p *Post) {
		s := &p.Settings
		s.Lift, s.ShadowTint, s.HighlightTint = [3]float32{}, [3]float32{1, 1, 1}, [3]float32{1, 1, 1}
		s.Contrast, s.Saturation = 0, 1
		s.TAA = taa
	}, views, frame...)
}

// taaSupersampled is taaScene seen from view at f×f the size, without TAA,
// averaged down to w×h in linear light: the picture TAA converges toward.
func taaSupersampled(t *testing.T, w, h, f int, view core.Mat4) []byte {
	t.Helper()
	big := taaFrames(t, w*f, h*f, false, false, []core.Mat4{view})
	lin := func(b byte) float64 { return math.Pow(float64(b)/255, 2.2) }
	out := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				s := 0.0
				for j := 0; j < f; j++ {
					for i := 0; i < f; i++ {
						s += lin(big[((y*f+j)*w*f+x*f+i)*4+c])
					}
				}
				out[(y*w+x)*4+c] = byte(math.Round(math.Pow(s/float64(f*f), 1/2.2) * 255))
			}
			out[(y*w+x)*4+3] = 255
		}
	}
	return out
}

func taaRepeat(v core.Mat4, n int) []core.Mat4 {
	views := make([]core.Mat4, n)
	for i := range views {
		views[i] = v
	}
	return views
}

// A still camera converges to the supersampled picture: the wires, broken
// into dashes in one sample per pixel, come out whole and faint, and the
// error against an 8×8 supersampled frame falls by well over a third.
func TestPostTAAConverges(t *testing.T) {
	const w, h = 480, 320
	ref := taaSupersampled(t, w, h, 8, logDepthView)
	off := taaFrames(t, w, h, false, false, []core.Mat4{logDepthView})
	on := taaFrames(t, w, h, false, true, taaRepeat(logDepthView, 32))
	if os.Getenv("POST_DUMP") != "" {
		dumpPNG(t, ref, w, h, "taa-ref")
		dumpPNG(t, off, w, h, "taa-off")
		dumpPNG(t, on, w, h, "taa-on")
	}
	_, eOff := maxDiff(ref, off, w, h)
	_, eOn := maxDiff(ref, on, w, h)
	t.Logf("mean error against 8×8 supersampling: %.2f without TAA, %.2f with", eOff, eOn)
	if eOn > eOff*0.6 {
		t.Fatalf("TAA did not converge toward the supersampled picture: error %.2f, %.2f without", eOn, eOff)
	}
	// It holds still: after converging, one more frame (the next jitter)
	// changes the picture little.
	next := taaFrames(t, w, h, false, true, taaRepeat(logDepthView, 33))
	if _, d := maxDiff(on, next, w, h); d > 0.4 {
		t.Fatalf("a converged still picture still changes by %.2f a frame", d)
	}
}

// taaPanViews moves the camera sideways (the box sliding across the screen,
// the floor behind it uncovered) and turns it, n frames ending at
// logDepthView.
func taaPanViews(n int, speed float32) []core.Mat4 {
	views := make([]core.Mat4, n)
	for i := range views {
		f := float32(n-1-i) / 15 * speed
		views[i] = motionView(0.12*f, -1.6*f)
	}
	return views
}

// A moving camera leaves no ghosts: after a pan the picture is the one
// taken from where the camera stopped, not a smear of where it was. Where
// the pan changed the picture most (the box's old place, the uncovered
// floor) TAA is no further from the supersampled picture than one sample
// per pixel is, and over the whole frame it is closer.
func TestPostTAAPan(t *testing.T) {
	const w, h = 480, 320
	views := taaPanViews(16, 1)
	ref := taaSupersampled(t, w, h, 8, logDepthView)
	start := taaSupersampled(t, w, h, 2, views[0])
	off := taaFrames(t, w, h, false, false, views)
	on := taaFrames(t, w, h, false, true, views)
	if os.Getenv("POST_DUMP") != "" {
		dumpPNG(t, on, w, h, "taa-pan")
		dumpPNG(t, start, w, h, "taa-pan-start")
	}
	var nMask int
	var gOn, gOff float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if math.Abs(lumAt(start, w, x, y)-lumAt(ref, w, x, y)) < 60 {
				continue
			}
			nMask++
			gOn += math.Abs(lumAt(on, w, x, y) - lumAt(ref, w, x, y))
			gOff += math.Abs(lumAt(off, w, x, y) - lumAt(ref, w, x, y))
		}
	}
	gOn, gOff = gOn/float64(nMask), gOff/float64(nMask)
	_, eOff := maxDiff(ref, off, w, h)
	_, eOn := maxDiff(ref, on, w, h)
	t.Logf("after a pan: mean error %.2f with TAA, %.2f without; where the pan changed the picture (%d px): %.2f, %.2f",
		eOn, eOff, nMask, gOn, gOff)
	if nMask < 2000 {
		t.Fatalf("the pan changed only %d pixels", nMask)
	}
	if gOn > gOff*1.1+1 {
		t.Fatalf("TAA ghosts: %.2f from the supersampled picture where the pan changed it, %.2f without TAA", gOn, gOff)
	}
	fastOn, fastOff := eOn, eOff
	// A slow pan (about a pixel a frame): TAA is no worse than one sample
	// per pixel even on wires this thin, the hardest case (each frame's
	// resampling of the history spreads a third of a pixel's line).
	slow := taaPanViews(32, 0.15)
	off = taaFrames(t, w, h, false, false, slow)
	on = taaFrames(t, w, h, false, true, slow)
	_, eOff = maxDiff(ref, off, w, h)
	_, eOn = maxDiff(ref, on, w, h)
	t.Logf("after a slow pan: mean error %.2f with TAA, %.2f without", eOn, eOff)
	if os.Getenv("POST_DUMP") != "" {
		dumpPNG(t, on, w, h, "taa-slowpan")
	}
	if fastOn > fastOff*1.25 {
		t.Fatalf("TAA after a pan is further from the supersampled picture (%.2f) than no TAA (%.2f)", fastOn, fastOff)
	}
	if eOn > eOff {
		t.Fatalf("TAA after a slow pan: error %.2f, %.2f without", eOn, eOff)
	}
}

// Ordinary edges stay smoother in motion too: after a pan over the
// checkered floor and box (a pixel and more a frame) the picture is
// closer to a 4×4 supersampled one than a single sample per pixel is.
func TestPostTAAEdgesInMotion(t *testing.T) {
	const w, h = 480, 320
	run := func(w, h int, taa bool, views []core.Mat4) []byte {
		return logDepthFrames(t, w, h, false, true, func(p *Post) {
			s := &p.Settings
			s.Lift, s.ShadowTint, s.HighlightTint = [3]float32{}, [3]float32{1, 1, 1}, [3]float32{1, 1, 1}
			s.Contrast, s.Saturation = 0, 1
			s.TAA = taa
		}, views)
	}
	big := run(w*4, h*4, false, []core.Mat4{logDepthView})
	ref := make([]byte, w*h*4)
	lin := func(b byte) float64 { return math.Pow(float64(b)/255, 2.2) }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				s := 0.0
				for j := 0; j < 4; j++ {
					for i := 0; i < 4; i++ {
						s += lin(big[((y*4+j)*w*4+x*4+i)*4+c])
					}
				}
				ref[(y*w+x)*4+c] = byte(math.Round(math.Pow(s/16, 1/2.2) * 255))
			}
		}
	}
	for _, speed := range []float32{0.15, 1} {
		views := taaPanViews(32, speed)
		_, eOff := maxDiff(ref, run(w, h, false, views), w, h)
		_, eOn := maxDiff(ref, run(w, h, true, views), w, h)
		t.Logf("pan at %v: mean error against 4×4 supersampling %.2f without TAA, %.2f with", speed, eOff, eOn)
		if eOn > eOff*0.9 {
			t.Fatalf("pan at %v: TAA error %.2f, %.2f without", speed, eOn, eOff)
		}
	}
}

// TAA reprojects through a logarithmic depth buffer as through a
// perspective one: the same pan comes out the same.
func TestPostTAALogDepth(t *testing.T) {
	const w, h = 480, 320
	views := taaPanViews(16, 1)
	persp := taaFrames(t, w, h, false, true, views)
	logd := taaFrames(t, w, h, true, true, views)
	plain := taaFrames(t, w, h, true, false, views)
	_, mean := maxDiff(persp, logd, w, h)
	_, taa := maxDiff(plain, logd, w, h)
	t.Logf("log depth differs from perspective by %.2f on average; TAA changed %.2f", mean, taa)
	if mean > taa/4 {
		t.Fatalf("TAA over log depth differs from perspective by %.2f on average (TAA itself changes %.2f)", mean, taa)
	}
}

// A cut starts the history afresh: ResetMotion, or a turn of 90° in one
// frame, gives the picture a camera that was always there gives after
// ResetMotion (the same frame of the jitter, no history).
func TestPostTAACuts(t *testing.T) {
	const w, h = 480, 320
	reset := func(at int) func(*Post, int) {
		return func(p *Post, i int) {
			if i == at {
				p.ResetMotion()
			}
		}
	}
	other := motionView(0.3, 1)
	fresh := taaFrames(t, w, h, false, true, taaRepeat(other, 13), reset(12))
	cut := taaFrames(t, w, h, false, true, append(taaRepeat(logDepthView, 12), other), reset(12))
	if worst, _ := maxDiff(fresh, cut, w, h); worst > 0 {
		t.Fatalf("ResetMotion: the picture kept up to %v of the last view", worst)
	}
	turned := motionView(1.57, 0)
	fresh = taaFrames(t, w, h, false, true, taaRepeat(turned, 13), reset(12))
	cut = taaFrames(t, w, h, false, true, append(taaRepeat(logDepthView, 12), turned))
	if worst, _ := maxDiff(fresh, cut, w, h); worst > 0 {
		t.Fatalf("a 90° turn: the picture kept up to %v of the last view", worst)
	}
	// Without the reset the history carries over: the converged picture
	// differs from a fresh one.
	conv := taaFrames(t, w, h, false, true, taaRepeat(other, 13))
	if _, mean := maxDiff(fresh, conv, w, h); mean < 0.3 {
		t.Fatalf("the history made no difference (%.2f)", mean)
	}
}

// The jitter is a Halton (2, 3) cycle of eight within half a pixel, zero
// while TAA is off; JitterProjection moves every point by it in NDC.
func TestPostJitter(t *testing.T) {
	p := &Post{w: 400, h: 200}
	if dx, dy := p.Jitter(); dx != 0 || dy != 0 {
		t.Fatalf("jitter with TAA off: %v, %v", dx, dy)
	}
	p.Settings.TAA = true
	var sx, sy float32
	seen := map[[2]float32]bool{}
	for i := 0; i < taaSamples; i++ {
		p.frame = i
		dx, dy := p.Jitter()
		if math.Abs(float64(dx)) > 1.0/400 || math.Abs(float64(dy)) > 1.0/200 {
			t.Fatalf("frame %d: jitter %v, %v is more than half a pixel", i, dx, dy)
		}
		seen[[2]float32{dx, dy}] = true
		sx, sy = sx+dx*200, sy+dy*100
	}
	if len(seen) != taaSamples || math.Abs(float64(sx/taaSamples)) > 0.1 || math.Abs(float64(sy/taaSamples)) > 0.1 {
		t.Fatalf("%d distinct offsets, mean (%v, %v) px", len(seen), sx/taaSamples, sy/taaSamples)
	}
	proj := core.Mat4Perspective(0.9, 1.5, 0.1, 100)
	j := JitterProjection(proj, 0.01, -0.02)
	for _, pt := range []core.Vec3{{X: 1, Y: 2, Z: -5}, {X: -3, Y: 0.5, Z: -40}} {
		a := proj.MulVec4(core.V4(pt.X, pt.Y, pt.Z, 1))
		b := j.MulVec4(core.V4(pt.X, pt.Y, pt.Z, 1))
		if math.Abs(float64(b.X/b.W-a.X/a.W-0.01)) > 1e-5 || math.Abs(float64(b.Y/b.W-a.Y/a.W+0.02)) > 1e-5 {
			t.Fatalf("JitterProjection moved %v by (%v, %v)", pt, b.X/b.W-a.X/a.W, b.Y/b.W-a.Y/a.W)
		}
	}
}

// TestPostTAACost times Post at 1600×900 with and without TAA, the camera
// still and turning. Set GFX_POST_BENCH=1 to run it.
func TestPostTAACost(t *testing.T) {
	if testing.Short() || os.Getenv("GFX_POST_BENCH") == "" {
		t.Skip("set GFX_POST_BENCH=1 to time TAA")
	}
	const w, h, n = 1600, 900, 120
	still := taaRepeat(logDepthView, n)
	turning := make([]core.Mat4, n)
	for i := range turning {
		turning[i] = motionView(0.03*float32(i%2), 0)
	}
	dev, _ := headlessDevice(t)
	timeIt := func(taa bool, views []core.Mat4) time.Duration {
		var start time.Time
		logDepthFrames(t, w, h, false, true, func(p *Post) {
			p.Settings.Lift = [3]float32{}
			p.Settings.TAA = taa
		}, views, func(p *Post, i int) {
			if i == 20 {
				dev.WaitIdle()
				start = time.Now()
			}
		})
		dev.WaitIdle()
		return time.Since(start) / (n - 20)
	}
	timeIt(false, still) // warm up
	for round := 0; round < 2; round++ {
		t.Logf("frame (scene + Post) at %d×%d: still %v without TAA, %v with; turning %v, %v",
			w, h, timeIt(false, still), timeIt(true, still), timeIt(false, turning), timeIt(true, turning))
	}
}

// View3D jitters a Renderer3D for its Post: a pole a third of a pixel
// wide, dashed in one sample per pixel, comes out in partial-coverage
// greys along (nearly) its whole length once TAA has run a few frames.
func TestView3DTAA(t *testing.T) {
	const w, h = 160, 120
	dev, q := headlessDevice(t)
	r2, err := New2D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Release()
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	fs.Sun = RGBA{R: 2, G: 2, B: 2, A: 1}
	r3.SetFrameSettings(fs)
	render := func(taa bool) []byte {
		post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer post.Release()
		s := &post.Settings
		s.AutoExposure, s.Bloom, s.Grain, s.Vignette, s.ChromaticAberration, s.AO, s.FXAA = 0, 0, 0, 0, 0, 0, false
		s.TAA = taa
		v := NewView3D(dev, r2, gpu.FormatRGBA8Unorm)
		defer v.release()
		v.Post = post
		v.Clear = gpu.Color{A: 1}
		v.Resize(w, h)
		eye := core.V3(0, 1, 6)
		proj := core.Mat4Perspective(0.8, float32(w)/float32(h), 0.1, 100)
		view := core.Mat4LookAt(eye, core.V3(0, 1, 0), core.V3(0, 1, 0))
		pole := &Material{Color: RGBA{R: 1, G: 1, B: 1, A: 1}, Roughness: 1}
		for i := 0; i < 16; i++ {
			post.Projection, post.View = proj, view
			r3.Begin(proj.Mul(view), core.V3(0.3, -1, -0.5), eye)
			// About 0.02 wide at 6 m, where a pixel is about 0.04; leaning.
			r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 1, 0)).
				Mul(core.Mat4RotateZ(0.2)).Mul(core.Mat4Scale(core.V3(0.013, 2.4, 0.013))), pole)
			enc := dev.CreateCommandEncoder()
			if err := v.Render(enc, r3); err != nil {
				t.Fatal(err)
			}
			q.Submit(enc.Finish())
		}
		// The view's own target cannot be read back: show the last frame
		// once more into one that can.
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		tv := target.CreateView()
		defer func() { tv.Release(); target.Release() }()
		enc := dev.CreateCommandEncoder()
		post.Render(enc, tv)
		q.Submit(enc.Finish())
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		return pix
	}
	// Each row the pole crosses: how much light it leaves there. A dashed
	// pole leaves none on some rows; a covered one some on every row.
	rows := func(pix []byte) (lit int) {
		bg := lumAt(pix, w, 4, h/2)
		for y := 3 * h / 8; y < 5*h/8; y++ {
			m := 0.0
			for x := w/2 - 20; x < w/2+20; x++ {
				m = math.Max(m, lumAt(pix, w, x, y))
			}
			if m > bg+6 {
				lit++
			}
		}
		return lit
	}
	off, on := rows(render(false)), rows(render(true))
	t.Logf("rows the pole shows in: %d of %d without TAA, %d with", off, h/4, on)
	if on < h/4*9/10 || off > h/4*3/4 {
		t.Fatalf("the pole shows in %d of %d rows with TAA (%d without)", on, h/4, off)
	}
}
