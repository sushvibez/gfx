//go:build linux || windows

package shell

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Dynamic resolution in a real window: a hidden window runs the shell's
// own frame loop (swapchain, present, vsync), drawing a Renderer3D scene
// through render.Post at the render scale a render.Quality steers from
// the frame times. For the first two and a half seconds each frame is
// made 30 ms slower (an artificial load), then the load goes. The scale
// must fall under the load and climb back once it is gone; every change
// is logged with the mean frame time that caused it. Screenshots only
// ever take the offscreen path, which never steers. Run by hand
// (GFX_WINDOW_TESTS=1), at a desk: a window gets no frames while the
// session is locked.
func TestDynamicResolutionWindow(t *testing.T) {
	if os.Getenv("GFX_WINDOW_TESTS") == "" {
		t.Skip("set GFX_WINDOW_TESTS=1 to open a window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	needGPU(t)
	const (
		loadFor = 2500 * time.Millisecond
		loadMS  = 30
		runFor  = 9 * time.Second
	)
	done := errors.New("done")
	var (
		r3      *render.Renderer3D
		post    *render.Post
		q       *render.Quality
		start   time.Time
		last    time.Time
		frames  int
		scale   = float32(-1)
		minLoad = float32(2)
		after   float32
		drops   int
	)
	hooks := Hooks{
		Init: func(a *App) (func() ui.Element, error) {
			var err error
			if r3, err = render.New3D(a.Device, a.Queue, gpu.FormatRGBA16Float); err != nil {
				return nil, err
			}
			if post, err = render.NewPost(a.Device, a.Queue, a.Format); err != nil {
				return nil, err
			}
			fs := render.DefaultFrameSettings()
			fs.Tonemap = render.TonemapHDR
			r3.SetFrameSettings(fs)
			q = render.NewQuality(render.QualityHigh)
			q.Dynamic = true // forced on: the window is the point
			return func() ui.Element { return ui.Div() }, nil
		},
		// A per-frame hook: the window draws every frame, not on damage.
		FrameStart: func(*App, float32, []app.MouseEvent, []app.KeyEvent) {},
		Encode3D: func(a *App, enc *gpu.CommandEncoder, target, _ *gpu.TextureView) error {
			now := time.Now()
			if start.IsZero() {
				start, last = now, now
			}
			el := now.Sub(start)
			if el < loadFor {
				time.Sleep(loadMS * time.Millisecond) // the load: a slow frame
			}
			if q.Frame(float32(now.Sub(last).Seconds())) {
				drops++
				t.Logf("%5.2f s  tier dropped to %s", el.Seconds(), q.Tier)
			}
			last = now
			s := q.Scale(a.W, a.H)
			if s != scale {
				t.Logf("%5.2f s  render scale %.2f (%s, mean frame %.1f ms, load %v)", el.Seconds(), s, q.Tier, q.Dyn.Mean(), el < loadFor)
				scale = s
			}
			if el < loadFor {
				minLoad = min(minLoad, s)
			} else {
				after = s
			}
			w, h := max(int(float32(a.W)*s+0.5), 1), max(int(float32(a.H)*s+0.5), 1)
			post.Resize(w, h)
			eye := core.V3(2.5, 2, 4)
			view := core.Mat4LookAt(eye, core.V3(0, 0.3, 0), core.V3(0, 1, 0))
			proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 100)
			post.Projection, post.View = proj, view
			r3.Begin(proj.Mul(view), core.V3(-0.4, -1, -0.3), eye)
			r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 0.5, 0)), &render.Material{Color: render.RGBA{R: 0.8, G: 0.4, B: 0.2, A: 1}, Roughness: 0.5})
			r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.05, 0)).Mul(core.Mat4Scale(core.V3(20, 0.1, 20))), &render.Material{Color: render.RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}, Roughness: 1})
			if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{R: 0.3, G: 0.4, B: 0.6, A: 1}); err != nil {
				return err
			}
			post.Render(enc, target)
			frames++
			if el > runFor {
				return done
			}
			return nil
		},
		Close: func(*App) {
			if post != nil {
				post.Release()
			}
			if r3 != nil {
				r3.Release()
			}
		},
	}
	err := Run(Options{Title: "dynamic resolution", Width: 1280, Height: 720, Hidden: true, MaxFrames: 5000}, hooks)
	if err != nil && !errors.Is(err, done) {
		t.Fatal(err)
	}
	t.Logf("%d frames in %.1f s; lowest scale under the load %.2f, at the end %.2f (%s, %d tier drops)", frames, time.Since(start).Seconds(), minLoad, after, q.Tier, drops)
	if minLoad >= 0.999 {
		t.Fatalf("the scale never fell under a %d ms load", loadMS)
	}
	if after <= minLoad {
		t.Fatalf("the scale did not climb back once the load went: %.2f, %.2f under it", after, minLoad)
	}
}
