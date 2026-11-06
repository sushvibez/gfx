//go:build !js

package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

func wgpuLib(t testing.TB) string {
	t.Helper()
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return p
	}
	p, err := filepath.Abs(filepath.Join("..", "build", "wgpu", gpu.DefaultLibNames()[0]))
	if err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			return p
		}
	}
	t.Skip("wgpu-native not found; run scripts/fetch-wgpu.sh or set GFX_WGPU_LIB")
	return ""
}

// headlessDevice acquires a device with no window or surface — the
// DESIGN.md §8.1 headless mode at the gpu layer.
// headlessDevice is ONE device for the whole package, made on first ask.
//
// It used to be one per test, and somewhere past thirty of those the
// driver starts refusing to make more — which surfaces as a validation
// error in whichever test happened to be next, not in the test that
// exhausted anything. Releasing them did not help: wgpu keeps a device
// alive while any object made from it is, and the tests keep textures
// and renderers past their own end.
//
// Nothing here needs an isolated device. Each test still makes its own
// renderer and its own render target.
var (
	sharedDeviceOnce sync.Once
	sharedDevice     *gpu.Device
	sharedQueue      *gpu.Queue
	sharedDeviceErr  error
)

func headlessDevice(t testing.TB) (*gpu.Device, *gpu.Queue) {
	t.Helper()
	lib := wgpuLib(t)
	sharedDeviceOnce.Do(func() {
		inst, err := gpu.NewInstance(lib)
		if err != nil {
			sharedDeviceErr = fmt.Errorf("NewInstance: %w", err)
			return
		}
		adapter, err := inst.RequestAdapter(nil)
		if err != nil {
			sharedDeviceErr = fmt.Errorf("RequestAdapter: %w", err)
			return
		}
		device, err := adapter.RequestDevice()
		if err != nil {
			sharedDeviceErr = fmt.Errorf("RequestDevice: %w", err)
			return
		}
		sharedDevice, sharedQueue = device, device.Queue()
	})
	if sharedDeviceErr != nil {
		t.Fatal(sharedDeviceErr)
	}
	return sharedDevice, sharedQueue
}

func TestRenderer2DHeadlessPixels(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 320, 240

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	r.FillRect(40, 40, 120, 80, RGBA{R: 1, A: 1})     // red, opaque
	r.FillRect(200, 100, 80, 100, RGBA{G: 1, A: 1})   // green, opaque
	r.FillRect(100, 80, 120, 120, RGBA{B: 1, A: 0.5}) // blue, 50% over both

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{R: 0.2, G: 0.2, B: 0.2, A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}

	at := func(x, y int) (r, g, b, a byte) {
		i := (y*W + x) * 4
		return pix[i], pix[i+1], pix[i+2], pix[i+3]
	}
	near := func(v byte, want int) bool {
		d := int(v) - want
		return d >= -3 && d <= 3
	}

	// Background corner.
	if cr, cg, cb, _ := at(5, 5); !near(cr, 51) || !near(cg, 51) || !near(cb, 51) {
		t.Errorf("background = %d,%d,%d, want ~51,51,51", cr, cg, cb)
	}
	// Solid red rect interior (outside the blue overlay).
	if cr, cg, cb, _ := at(60, 60); !near(cr, 255) || !near(cg, 0) || !near(cb, 0) {
		t.Errorf("red rect = %d,%d,%d, want 255,0,0", cr, cg, cb)
	}
	// Solid green rect interior (outside the blue overlay).
	if cr, cg, cb, _ := at(260, 180); !near(cr, 0) || !near(cg, 255) || !near(cb, 0) {
		t.Errorf("green rect = %d,%d,%d, want 0,255,0", cr, cg, cb)
	}
	// 50% blue over red: r = 255*0.5, b = 255*0.5.
	if cr, cg, cb, _ := at(110, 90); !near(cr, 128) || !near(cg, 0) || !near(cb, 128) {
		t.Errorf("blue-over-red = %d,%d,%d, want ~128,0,128", cr, cg, cb)
	}
	// 50% blue over background: 0.2*0.5 + 1*0.5 in blue.
	if cr, cg, cb, _ := at(190, 190); !near(cr, 26) || !near(cg, 26) || !near(cb, 153) {
		t.Errorf("blue-over-bg = %d,%d,%d, want ~26,26,153", cr, cg, cb)
	}

	// Keep a human-checkable artifact next to the test log.
	img := &image.RGBA{Pix: pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
	out := filepath.Join(t.TempDir(), "renderer2d.png")
	f, err := os.Create(out)
	if err == nil {
		_ = png.Encode(f, img)
		f.Close()
		t.Logf("screenshot: %s", out)
	}
}

// TestRoundedQuadSDF verifies the GPUI-derived SDF path: rounded corners
// cut away, borders ring the fill, and the radius-0 fast path matches.
func TestRoundedQuadSDF(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 200

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	// Rounded red quad with a 4px white border, radius 20.
	r.Quad(20, 20, 120, 120, RGBA{R: 1, A: 1}, 20, 4, RGBA{1, 1, 1, 1})
	// Sharp green quad via the fast path.
	r.FillRect(160, 160, 30, 30, RGBA{G: 1, A: 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) {
		i := (y*W + x) * 4
		return pix[i], pix[i+1], pix[i+2]
	}

	// Center: red fill.
	if cr, cg, _ := at(80, 80); cr < 250 || cg > 5 {
		t.Errorf("center = %d,%d, want red fill", cr, cg)
	}
	// Border midpoint of the top edge (2px into the 4px border): white.
	if cr, cg, cb := at(80, 22); cr < 250 || cg < 250 || cb < 250 {
		t.Errorf("top border = %d,%d,%d, want white", cr, cg, cb)
	}
	// Corner pixel (21,21): with radius 20 the corner is cut — background.
	if cr, cg, cb := at(21, 21); cr > 20 || cg > 20 || cb > 20 {
		t.Errorf("rounded corner = %d,%d,%d, want background", cr, cg, cb)
	}
	// Inside the corner arc (the 45° point of the radius circle is inside).
	if cr, _, _ := at(34, 34); cr < 100 {
		t.Errorf("inside corner arc r=%d, want filled (border or red)", cr)
	}
	// Fast-path green square.
	if _, cg, _ := at(175, 175); cg < 250 {
		t.Errorf("fast-path quad g=%d, want 255", cg)
	}
}

// TestClipScissor draws across a clip boundary and asserts pixels stop at
// the scissor edge, including nested intersection and restore-after-pop.
func TestClipScissor(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 120, 120

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	r.PushClip(20, 20, 40, 40)
	r.FillRect(0, 0, 120, 120, RGBA{R: 1, A: 1}) // red, clipped to 20..60
	r.PushClip(40, 40, 100, 100)                 // intersects to 40..60
	r.FillRect(0, 0, 120, 120, RGBA{G: 1, A: 1}) // green, clipped to 40..60
	r.PopClip()
	r.PopClip()
	r.FillRect(100, 100, 10, 10, RGBA{B: 1, A: 1}) // unclipped after pops

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) {
		i := (y*W + x) * 4
		return pix[i], pix[i+1], pix[i+2]
	}
	if cr, cg, _ := at(30, 30); cr != 255 || cg != 0 {
		t.Errorf("inside clip = %d,%d, want red", cr, cg)
	}
	if cr, cg, cb := at(10, 30); cr != 0 || cg != 0 || cb != 0 {
		t.Errorf("outside clip = %d,%d,%d, want background", cr, cg, cb)
	}
	if cr, cg, cb := at(70, 30); cr != 0 || cg != 0 || cb != 0 {
		t.Errorf("right of clip = %d,%d,%d, want background", cr, cg, cb)
	}
	if _, cg, _ := at(50, 50); cg != 255 {
		t.Errorf("nested clip interior g=%d, want 255", cg)
	}
	if _, cg, _ := at(30, 30); cg != 0 {
		t.Error("green leaked outside the nested intersection")
	}
	if _, _, cb := at(105, 105); cb != 255 {
		t.Errorf("post-pop draw b=%d, want 255 (scissor not restored)", cb)
	}
}

// TestGradientRect verifies the linear-gradient quad path: a left→right
// red→blue gradient reads red at the left edge, blue at the right, and a
// even mix in the middle.
func TestGradientRect(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 80

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	// angle 0 = left→right.
	r.GradientRect(0, 0, W, H, 0, RGBA{R: 1, A: 1}, RGBA{B: 1, A: 1}, 0)

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x int) (r, g, b byte) {
		i := (40*W + x) * 4
		return pix[i], pix[i+1], pix[i+2]
	}
	lr, _, lb := at(4)     // left: red
	mr, _, mb := at(W / 2) // middle: ~half/half
	rr, _, rb := at(W - 5) // right: blue
	if lr < 230 || lb > 25 {
		t.Errorf("left = %d,_,%d, want red", lr, lb)
	}
	if rb < 230 || rr > 25 {
		t.Errorf("right = %d,_,%d, want blue", rr, rb)
	}
	if mr < 100 || mr > 160 || mb < 100 || mb > 160 {
		t.Errorf("middle = %d,_,%d, want ~half red/blue", mr, mb)
	}
	// Monotonic: red falls, blue rises left→right.
	if !(lr > mr && mr > rr) || !(lb < mb && mb < rb) {
		t.Errorf("gradient not monotonic: r %d>%d>%d  b %d<%d<%d", lr, mr, rr, lb, mb, rb)
	}
}

// TestFillPath tessellates and fills a concave arrow polygon, asserting
// the interior is filled and a point in the concavity is not.
func TestFillPath(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 200

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	// A concave "arrow"/chevron pointing right — its notch on the left
	// proves ear-clipping handles reflex vertices.
	p := NewPath().
		MoveTo(40, 40).
		LineTo(160, 100).
		LineTo(40, 160).
		LineTo(80, 100). // notch vertex (reflex), pulls the left edge in
		Close()

	r.Begin(W, H)
	r.FillPath(p, RGBA{G: 1, A: 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) {
		i := (y*W + x) * 4
		return pix[i], pix[i+1], pix[i+2]
	}
	// Solidly inside the right body of the chevron.
	if _, g, _ := at(120, 100); g < 250 {
		t.Errorf("interior g=%d, want filled", g)
	}
	// Inside the left notch (x<80 at the vertical center) — outside the
	// polygon, must be background.
	if _, g, _ := at(55, 100); g != 0 {
		t.Errorf("notch g=%d, want empty (ear clip kept the concavity)", g)
	}
	// Well outside.
	if _, g, _ := at(180, 30); g != 0 {
		t.Errorf("outside g=%d, want empty", g)
	}
	// A point clearly inside near the tip.
	if _, g, _ := at(140, 100); g < 250 {
		t.Errorf("near tip g=%d, want filled", g)
	}
}

// TestLoadImage encodes a 2x2 PNG in-test, decodes it through LoadImage,
// draws it scaled, and checks the texels land with straight alpha.
func TestLoadImage(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 80, 80
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	// 2x2: red green / blue white, as a PNG.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	src.Set(1, 0, color.NRGBA{0, 255, 0, 255})
	src.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	src.Set(1, 1, color.NRGBA{255, 255, 255, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := r.LoadImage(buf.Bytes())
	if err != nil {
		t.Fatalf("LoadImage: %v", err)
	}
	defer img.Release()
	if img.W != 2 || img.H != 2 {
		t.Fatalf("decoded size %dx%d, want 2x2", img.W, img.H)
	}

	r.Begin(W, H)
	r.DrawImage(img, 0, 0, 80, 80, White)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) { i := (y*W + x) * 4; return pix[i], pix[i+1], pix[i+2] }
	if cr, _, _ := at(10, 10); cr < 250 {
		t.Errorf("top-left r=%d, want red texel", cr)
	}
	if _, _, cb := at(10, 70); cb < 250 {
		t.Errorf("bottom-left b=%d, want blue texel", cb)
	}
}

// TestStrokePath strokes an L-shaped open polyline and checks the line is
// drawn at its width along both arms, with empty space off the line.
func TestStrokePath(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 200
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	// Horizontal then vertical: an "L".
	p := NewPath().MoveTo(40, 40).LineTo(160, 40).LineTo(160, 160)
	r.StrokePath(p, 12, RGBA{G: 1, A: 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	g := func(x, y int) byte { return pix[(y*W+x)*4+1] }
	if g(100, 40) < 250 {
		t.Errorf("on horizontal arm g=%d, want stroked", g(100, 40))
	}
	if g(160, 100) < 250 {
		t.Errorf("on vertical arm g=%d, want stroked", g(160, 100))
	}
	if g(100, 100) != 0 {
		t.Errorf("interior of the L g=%d, want empty (stroke, not fill)", g(100, 100))
	}
	if g(40, 160) != 0 {
		t.Errorf("off-path corner g=%d, want empty", g(40, 160))
	}
}

// TestSVG parses a tiny SVG (a filled blue rect + a red-stroked diagonal)
// and verifies both render.
func TestSVG(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 200
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	svg, err := ParseSVG([]byte(`<svg viewBox="0 0 100 100">
		<rect x="10" y="10" width="40" height="40" fill="#0000ff"/>
		<path d="M60 60 L90 90" stroke="#ff0000" stroke-width="8" fill="none"/>
	</svg>`))
	if err != nil {
		t.Fatalf("ParseSVG: %v", err)
	}
	if w, h := svg.Size(); w != 100 || h != 100 {
		t.Fatalf("size = %vx%v, want 100x100", w, h)
	}

	r.Begin(W, H)
	r.DrawSVG(svg, 0, 0, 2, White) // scale 2 → 200x200 doc space

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) { i := (y*W + x) * 4; return pix[i], pix[i+1], pix[i+2] }
	// Inside the rect (doc 10..50 *2 = 20..100): blue.
	if _, _, b := at(60, 60); b < 250 {
		t.Errorf("rect interior b=%d, want blue", b)
	}
	// On the diagonal stroke (doc 75,75 *2 = 150,150): red.
	if rr, _, _ := at(150, 150); rr < 200 {
		t.Errorf("stroke point r=%d, want red", rr)
	}
	// Empty corner.
	if rr, g, b := at(195, 10); rr|g|b != 0 {
		t.Errorf("empty corner = %d,%d,%d, want background", rr, g, b)
	}
}

// TestShadowGaussian verifies the GPUI-derived blur: soft alpha outside
// the rect that decays with distance, and nothing far away.
func TestShadowGaussian(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 200, 200

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(W, H)
	// White shadow on black background so alpha reads directly as gray.
	r.Shadow(60, 60, 80, 80, 8, 10, RGBA{1, 1, 1, 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	lum := func(x, y int) int { return int(pix[(y*W+x)*4]) }

	center := lum(100, 100)   // deep inside the shadow rect
	nearEdge := lum(100, 145) // 5px outside the rect edge
	farther := lum(100, 165)  // 25px outside
	veryFar := lum(100, 195)  // way past 3*sigma

	if center < 200 {
		t.Errorf("shadow center luminance %d, want near-opaque", center)
	}
	if !(center > nearEdge && nearEdge > farther && farther > veryFar) {
		t.Errorf("blur not monotonic: center=%d near=%d farther=%d veryFar=%d",
			center, nearEdge, farther, veryFar)
	}
	if nearEdge < 20 {
		t.Errorf("near-edge alpha %d too small — blur tail missing", nearEdge)
	}
	if veryFar > 5 {
		t.Errorf("alpha %d beyond 3*sigma — shadow leaks", veryFar)
	}
}

func TestRenderer2DTexturedQuads(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 160, 160

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	// 2x2 checker: red green / blue white.
	checker := r.NewImage(2, 2, []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	})
	defer checker.Release()

	r.Begin(W, H)
	r.DrawImage(checker, 20, 20, 80, 80, White)    // quadrants of 40px
	r.FillRect(120, 120, 30, 30, RGBA{G: 1, A: 1}) // batch break back to white tex

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (r, g, b byte) {
		i := (y*W + x) * 4
		return pix[i], pix[i+1], pix[i+2]
	}
	near := func(v byte, want int) bool {
		d := int(v) - want
		return d >= -3 && d <= 3
	}
	// Sample near the image's corners: for a 2x2 texture under bilinear
	// filtering, uv outside the 0.25..0.75 texel-center span is clamped to
	// a single texel, so corners are pure while quadrant centers blend.
	checks := []struct {
		x, y    int
		r, g, b int
		what    string
	}{
		{25, 25, 255, 0, 0, "top-left texel (red)"},
		{95, 25, 0, 255, 0, "top-right texel (green)"},
		{25, 95, 0, 0, 255, "bottom-left texel (blue)"},
		{95, 95, 255, 255, 255, "bottom-right texel (white)"},
		{60, 60, 127, 127, 127, "image center (all four blended)"},
		{135, 135, 0, 255, 0, "solid rect after batch break"},
		{5, 5, 0, 0, 0, "background"},
	}
	for _, c := range checks {
		cr, cg, cb := at(c.x, c.y)
		if !near(cr, c.r) || !near(cg, c.g) || !near(cb, c.b) {
			t.Errorf("%s at (%d,%d) = %d,%d,%d, want %d,%d,%d",
				c.what, c.x, c.y, cr, cg, cb, c.r, c.g, c.b)
		}
	}
}

func TestCameraRoundTrip(t *testing.T) {
	r := &Renderer2D{width: 800, height: 600}
	r.SetCamera(1000, 2000, 0.5)
	// A world point maps to screen and back to itself.
	for _, p := range [][2]float32{{1000, 2000}, {1200, 2100}, {0, 0}, {5000, -300}} {
		sx, sy := r.WorldToScreen(p[0], p[1])
		wx, wy := r.ScreenToWorld(sx, sy)
		if abs32(wx-p[0]) > 0.01 || abs32(wy-p[1]) > 0.01 {
			t.Errorf("round-trip (%g,%g) -> screen(%g,%g) -> world(%g,%g)", p[0], p[1], sx, sy, wx, wy)
		}
	}
	// The camera center maps to the screen center.
	sx, sy := r.WorldToScreen(1000, 2000)
	if abs32(sx-400) > 0.01 || abs32(sy-300) > 0.01 {
		t.Errorf("camera center -> (%g,%g), want (400,300)", sx, sy)
	}
	// Reset returns to identity.
	r.CameraReset()
	if sx, sy := r.WorldToScreen(123, 456); sx != 123 || sy != 456 {
		t.Errorf("after reset WorldToScreen = (%g,%g), want identity", sx, sy)
	}
}

// A corner radius past half the shorter side is a stadium, not a bigger
// circle: left unclamped the shader's corner centre falls outside the box
// and the quad renders as a lens (which is what a QML pill — `radius: 30`
// on a 36px-tall button — used to look like).
func TestClampCornerRadius(t *testing.T) {
	for _, tc := range []struct{ r, w, h, want float32 }{
		{30, 40, 36, 18}, // taller radius than the box: half the short side
		{18, 40, 36, 18}, // exactly a stadium: unchanged
		{60, 80, 36, 18}, // a wide pill
		{30, 60, 60, 30}, // a circle: unchanged
		{8, 120, 40, 8},  // an ordinary rounded rect: unchanged
		{0, 40, 40, 0},   // square corners stay square
		{-4, 40, 40, 0},  // a negative radius is no radius
		{30, 0, 0, 0},    // a degenerate box has no corners
	} {
		if got := clampCornerRadius(tc.r, tc.w, tc.h); got != tc.want {
			t.Errorf("clampCornerRadius(%v, %v, %v) = %v, want %v",
				tc.r, tc.w, tc.h, got, tc.want)
		}
	}
}

// TestDrawImageColorized recolours a coloured source: every texel comes out
// in the given hue at its own luminance, which is what Qt's
// `MultiEffect { colorization: 1 }` does to a style's icon. A tint multiply
// cannot: it would leave the red texel black under a teal tint.
func TestDrawImageColorized(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 40, 40
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	// A 2x1 source: pure red, then white — two very different luminances
	// (0.2126 and 1) of the same colorized hue.
	img := r.NewImage(2, 1, []byte{255, 0, 0, 255, 255, 255, 255, 255})
	defer img.Release()

	teal := RGBA{0, 1, 0.66, 1}
	r.Begin(W, H)
	r.DrawImageColorized(img, 0, 0, W, H, teal)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (byte, byte, byte) { i := (y*W + x) * 4; return pix[i], pix[i+1], pix[i+2] }
	// The red half: teal at 0.2126 luminance — no red left, and green
	// nowhere near full.
	lr, lg, lb := at(8, 20)
	if lr > 8 {
		t.Errorf("colorized red texel r=%d, want ~0 (the hue replaces it)", lr)
	}
	if lg < 40 || lg > 80 {
		t.Errorf("colorized red texel g=%d, want ~54 (0.2126 of the hue)", lg)
	}
	// The white half: the hue at full strength.
	wr, wg, wb := at(32, 20)
	if wr > 8 || wg < 240 || wb < 150 || wb > 190 {
		t.Errorf("colorized white texel = (%d,%d,%d), want ~(0,255,168)", wr, wg, wb)
	}
	if lb >= wb {
		t.Errorf("dark texel blue %d not below bright texel %d — luminance ignored", lb, wb)
	}
}

// A sprite quad puts each of its FOUR corners through the transform. Taking
// two and pairing their coordinates works only while the transform keeps the
// quad axis-aligned; under a rotation it mixes one corner's x with another's
// y, which tore every glyph of a rotated text into fragments (and dropped
// the quad entirely at some angles).
func TestSpriteQuadRotatesAsAWhole(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 40, 40
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	// A 2x1 image: red left, blue right.
	img := r.NewImage(2, 1, []byte{255, 0, 0, 255, 0, 0, 255, 255})
	defer img.Release()

	// Drawn unrotated the red half is on the left; rotated 90° about the
	// centre of the box, it has to end up at the TOP.
	r.Begin(W, H)
	r.PushRotate(math.Pi/2, 20, 20)
	r.DrawImage(img, 10, 15, 20, 10, White)
	r.PopTransform()
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) (byte, byte, byte) { i := (y*W + x) * 4; return pix[i], pix[i+1], pix[i+2] }
	// A 90° turn about (20,20) sends the left half up and the right half
	// down (y grows downward, so +90° maps -x to -y).
	tr, tg, tb := at(20, 14)
	br, bg, bb := at(20, 26)
	if tr < 200 || tg > 60 || tb > 60 {
		t.Errorf("top of the rotated sprite = (%d,%d,%d), want red", tr, tg, tb)
	}
	if bb < 200 || br > 60 || bg > 60 {
		t.Errorf("bottom of the rotated sprite = (%d,%d,%d), want blue", br, bg, bb)
	}
}
