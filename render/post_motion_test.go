//go:build !js

package render

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
)

// motionView is logDepthView turned by yaw radians about the vertical and
// moved sideways by side metres.
func motionView(yaw, side float32) core.Mat4 {
	eye := core.V3(side, 5, 9)
	at := core.V3(0, 0.5, 0).Sub(core.V3(0, 5, 9))
	c, s := float32(math.Cos(float64(yaw))), float32(math.Sin(float64(yaw)))
	at = core.V3(at.X*c-at.Z*s, at.Y, at.X*s+at.Z*c)
	return core.Mat4LookAt(eye, eye.Add(at), core.V3(0, 1, 0))
}

// motionScene draws the checkered floor and box from each view in turn
// with the given motion blur strength (at 60 frames a second) and returns
// the last frame.
func motionScene(t *testing.T, w, h int, logDepth bool, blur float32, views []core.Mat4, frame ...func(*Post, int)) []byte {
	t.Helper()
	return logDepthFrames(t, w, h, logDepth, true, func(p *Post) {
		p.Settings.Lift = [3]float32{}
		p.Settings.MotionBlur = blur
		p.DeltaTime = 1.0 / 60
	}, views, frame...)
}

// gradients sums the squared luminance steps between horizontal and
// between vertical neighbours over the lower two thirds of the picture
// (floor and box): a blur spreads one sharp step into many small ones,
// which keeps their sum but not the sum of their squares.
func gradients(pix []byte, w, h int) (gx, gy float64) {
	for y := h / 3; y < h-1; y++ {
		for x := 0; x < w-1; x++ {
			l := lumAt(pix, w, x, y)
			dx, dy := lumAt(pix, w, x+1, y)-l, lumAt(pix, w, x, y+1)-l
			gx += dx * dx
			gy += dy * dy
		}
	}
	return gx, gy
}

// maxDiff is the largest per-pixel luminance difference, and the mean.
func maxDiff(a, b []byte, w, h int) (worst, mean float64) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Abs(lumAt(a, w, x, y) - lumAt(b, w, x, y))
			worst = math.Max(worst, d)
			mean += d
		}
	}
	return worst, mean / float64(w*h)
}

// A camera that does not move is not blurred: the frame is the one
// without motion blur, pixel for pixel.
func TestPostMotionBlurStatic(t *testing.T) {
	const w, h = 480, 320
	still := []core.Mat4{logDepthView, logDepthView, logDepthView}
	off := motionScene(t, w, h, false, 0, still)
	on := motionScene(t, w, h, false, 1, still)
	if worst, _ := maxDiff(off, on, w, h); worst > 0 {
		t.Fatalf("a still camera changed the picture by up to %v", worst)
	}
	// Nor is a turn too small to move a pixel by a pixel's fraction.
	tiny := motionScene(t, w, h, false, 1, []core.Mat4{motionView(0.00005, 0), logDepthView})
	if worst, _ := maxDiff(off, tiny, w, h); worst > 0 {
		t.Fatalf("a turn of a twentieth of a milliradian changed the picture by up to %v", worst)
	}
}

// A yaw smears the picture sideways and a pitch up and down: the yaw
// softens the steps between horizontal neighbours more than the pitch
// does, and the pitch those between vertical ones more than the yaw.
func TestPostMotionBlurYaw(t *testing.T) {
	const w, h = 480, 320
	pitch := core.Mat4LookAt(core.V3(0, 5, 9), core.V3(0, 1.1, 0), core.V3(0, 1, 0)) // about 3° up
	off := motionScene(t, w, h, false, 0, []core.Mat4{logDepthView})
	yaw := motionScene(t, w, h, false, 1, []core.Mat4{motionView(0.05, 0), logDepthView})
	tilt := motionScene(t, w, h, false, 1, []core.Mat4{pitch, logDepthView})
	if os.Getenv("POST_DUMP") != "" {
		dumpPNG(t, off, w, h, "motion-off")
		dumpPNG(t, yaw, w, h, "motion-yaw")
		dumpPNG(t, tilt, w, h, "motion-pitch")
	}
	ox, oy := gradients(off, w, h)
	yx, yy := gradients(yaw, w, h)
	px, py := gradients(tilt, w, h)
	t.Logf("yaw: horizontal steps −%.0f%%, vertical −%.0f%%; pitch: −%.0f%%, −%.0f%%",
		(1-yx/ox)*100, (1-yy/oy)*100, (1-px/ox)*100, (1-py/oy)*100)
	if 1-yx/ox < 0.3 {
		t.Fatalf("a yaw softened the horizontal steps by only %.0f%%", (1-yx/ox)*100)
	}
	if yx > px*0.75 || py > yy*0.5 {
		t.Fatalf("the blur does not follow the motion: horizontal steps %.0f after a yaw, %.0f after a pitch; vertical %.0f, %.0f", yx, px, yy, py)
	}
}

// Motion blur rebuilds positions from a logarithmic depth buffer as from
// a perspective one: the same pan and sideways step blur the same way.
func TestPostMotionBlurLogDepth(t *testing.T) {
	const w, h = 480, 320
	move := []core.Mat4{motionView(0.03, -0.6), logDepthView}
	persp := motionScene(t, w, h, false, 1, move)
	logd := motionScene(t, w, h, true, 1, move)
	still := motionScene(t, w, h, true, 0, move)
	_, mean := maxDiff(persp, logd, w, h)
	_, moved := maxDiff(still, logd, w, h)
	t.Logf("log depth differs from perspective by %.2f on average; the blur changed %.2f", mean, moved)
	if moved < 2 {
		t.Fatalf("the blur over log depth changed the picture by only %.2f on average", moved)
	}
	if mean > moved/6 {
		t.Fatalf("the blur over log depth differs from perspective by %.2f on average (the blur itself is %.2f)", mean, moved)
	}
}

// A cut is not smeared: the first frame, a turn of 90° in one frame,
// ResetMotion. And UsePreviousView stands in for the remembered camera, for
// a renderer whose world moves with the camera.
func TestPostMotionBlurCuts(t *testing.T) {
	const w, h = 480, 320
	off := motionScene(t, w, h, false, 0, []core.Mat4{logDepthView})
	same := func(name string, pix []byte) {
		t.Helper()
		if worst, _ := maxDiff(off, pix, w, h); worst > 0 {
			t.Fatalf("%s: the picture changed by up to %v", name, worst)
		}
	}
	same("first frame", motionScene(t, w, h, false, 1, []core.Mat4{logDepthView}))
	same("a 90° turn", motionScene(t, w, h, false, 1, []core.Mat4{motionView(1.57, 0), logDepthView}))
	same("ResetMotion", motionScene(t, w, h, false, 1, []core.Mat4{motionView(0.05, 0), logDepthView},
		func(p *Post, i int) {
			if i == 1 {
				p.ResetMotion()
			}
		}))
	// A teleport sideways: the near floor jumps more than a quarter of
	// the screen and is left sharp.
	jump := motionScene(t, w, h, false, 1, []core.Mat4{motionView(0, 12), logDepthView})
	if d := math.Abs(lumAt(off, w, w/2, h-10) - lumAt(jump, w, w/2, h-10)); d > 0 {
		t.Fatalf("a teleport smeared the near floor by %v", d)
	}
	// The same view twice, but the previous one given turned: blurred.
	given := motionScene(t, w, h, false, 1, []core.Mat4{logDepthView, logDepthView},
		func(p *Post, i int) {
			if i == 1 {
				p.UsePreviousView(motionView(0.05, 0))
			}
		})
	if _, mean := maxDiff(off, given, w, h); mean < 2 {
		t.Fatalf("UsePreviousView blurred the picture by only %.2f on average", mean)
	}
}

// TestPostMotionBlurCost times Post at 1600×900 with and without the
// blur, the camera turning every frame. Set GFX_POST_BENCH=1 to run it.
func TestPostMotionBlurCost(t *testing.T) {
	if testing.Short() || os.Getenv("GFX_POST_BENCH") == "" {
		t.Skip("set GFX_POST_BENCH=1 to time motion blur")
	}
	const w, h, n = 1600, 900, 120
	views := make([]core.Mat4, n)
	for i := range views {
		views[i] = motionView(0.03*float32(i%2), 0)
	}
	dev, _ := headlessDevice(t)
	timeIt := func(blur float32) time.Duration {
		var start time.Time
		motionScene(t, w, h, false, blur, views, func(p *Post, i int) {
			if i == 20 {
				dev.WaitIdle()
				start = time.Now()
			}
		})
		dev.WaitIdle()
		return time.Since(start) / (n - 20)
	}
	timeIt(0) // warm up
	off, on := timeIt(0), timeIt(1)
	off2, on2 := timeIt(0), timeIt(1)
	t.Logf("frame (scene + Post) at %d×%d: %v without motion blur, %v with (%v, %v)", w, h, off, on, off2, on2)
}

// What rides with the eye is not smeared by its travel: a camera that
// only steps sideways blurs the scene, unless the scene is within
// RideRadius (then it rides along and stays sharp); a turn still blurs
// what rides.
func TestPostMotionBlurRide(t *testing.T) {
	const w, h = 480, 320
	step := []core.Mat4{motionView(0, -0.6), logDepthView}
	off := motionScene(t, w, h, false, 0, step)
	travel := motionScene(t, w, h, false, 1, step)
	ride := func(radius float32) func(*Post, int) {
		return func(p *Post, i int) { p.RideRadius = radius }
	}
	rode := motionScene(t, w, h, false, 1, step, ride(1e6))
	turned := motionScene(t, w, h, false, 1, []core.Mat4{motionView(0.05, -0.6), logDepthView}, ride(1e6))
	_, mTravel := maxDiff(off, travel, w, h)
	worst, mRode := maxDiff(off, rode, w, h)
	_, mTurned := maxDiff(off, turned, w, h)
	t.Logf("a sideways step blurs by %.2f on average; riding %.2f (worst %v); riding through a turn %.2f", mTravel, mRode, worst, mTurned)
	if mTravel < 1 {
		t.Fatalf("a sideways step blurred the picture by only %.2f on average", mTravel)
	}
	if worst > 0 {
		t.Fatalf("a scene riding with the eye was smeared by its travel (up to %v)", worst)
	}
	if mTurned < 1 {
		t.Fatalf("a turn blurred a riding scene by only %.2f on average", mTurned)
	}
	// A radius short of the scene leaves it to the travel's blur.
	if _, m := maxDiff(off, motionScene(t, w, h, false, 1, step, ride(0.5)), w, h); m < mTravel*0.8 {
		t.Fatalf("a ride radius of half a metre kept the scene sharp (%.2f against %.2f)", m, mTravel)
	}
}
