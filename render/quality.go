package render

// Quality tiers and dynamic resolution: what a GPU is asked to do, chosen
// from what it is, and the render scale steered each frame so the frame
// rate holds.
//
//	q := render.NewQuality(render.QualityForAdapter(adapter.Name()))
//	// each frame:
//	if q.Frame(dt) { /* a tier was dropped: rebuild what Budget sizes */ }
//	b := q.Budget()
//	b.Apply(&post.Settings)
//	b.ApplyAtmosphere(&post.Atmosphere)
//	view.RenderScale = q.Scale(w, h)
//
// The tiers follow a WebGL showcase's table (Saltline: scale, shadow map,
// ambient occlusion, volumetrics, bloom, motion blur, rain by tier, picked
// from the GPU string), and the controller its rule: a mean frame over
// 21.5 ms lowers the scale a tenth (a fifth when far over), one under
// 17.4 ms raises it a tenth, waiting longer each time a raise had to be
// taken back; twice at the floor and still slow drops a tier. The grow
// threshold sits just over a 60 Hz frame, so a vsync-paced game that has
// headroom reads as 16.7 ms and climbs back to full resolution.

import (
	"math"
	"runtime"
	"strings"
)

// QualityTier names how much a GPU is asked to do.
type QualityTier int8

const (
	QualityLow    QualityTier = iota // phones, software renderers
	QualityMedium                    // integrated and mobile-class GPUs
	QualityHigh                      // a desktop discrete GPU
	QualityUltra                     // chosen, never detected
)

var qualityNames = [...]string{"low", "medium", "high", "ultra"}

func (t QualityTier) String() string {
	if t < QualityLow || t > QualityUltra {
		return "quality?"
	}
	return qualityNames[t]
}

// ParseQualityTier reads a tier's name (low, medium, high, ultra; any case).
func ParseQualityTier(s string) (QualityTier, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range qualityNames {
		if s == n {
			return QualityTier(i), true
		}
	}
	return QualityHigh, false
}

// QualityBudget is what a tier allows. Apply and ApplyAtmosphere switch
// off what it does not; the sizes (ShadowSize, Rain) are for the app to
// use where it builds those things.
type QualityBudget struct {
	// Scale is the render scale (View3D.RenderScale) a tier starts at;
	// MinScale is the floor dynamic resolution may lower it to.
	Scale, MinScale float32
	// MaxPixels caps the scene target in pixels: on a large display the
	// render scale is lowered until width·height·scale² fits.
	MaxPixels int
	// ShadowSize is the sun's shadow map, in texels a side.
	ShadowSize int
	// AO is ambient occlusion, Shafts the volumetric air (light shafts and
	// lamp beams), Bloom the glow, Lens the flare, streak and dirt,
	// MotionBlur the camera's, TAA temporal anti-aliasing, Reflections
	// screen-space reflections of the scene.
	AO, Shafts, Bloom, Lens, MotionBlur, TAA, Reflections bool
	// Rain is the share of the rain's drops drawn (0…1).
	Rain float32
}

// Budget is the tier's table row.
func (t QualityTier) Budget() QualityBudget {
	switch {
	case t <= QualityLow:
		return QualityBudget{Scale: 0.75, MinScale: 0.5, MaxPixels: 1_000_000, ShadowSize: 1024,
			Rain: 0.4}
	case t == QualityMedium:
		return QualityBudget{Scale: 0.85, MinScale: 0.6, MaxPixels: 1_500_000, ShadowSize: 2048,
			AO: true, Shafts: true, Bloom: true, Lens: true, TAA: true, Rain: 0.7}
	case t == QualityHigh:
		return QualityBudget{Scale: 1, MinScale: 0.6, MaxPixels: 2_300_000, ShadowSize: 2048,
			AO: true, Shafts: true, Bloom: true, Lens: true, MotionBlur: true, TAA: true, Reflections: true, Rain: 1}
	}
	return QualityBudget{Scale: 1, MinScale: 0.7, MaxPixels: 3_700_000, ShadowSize: 4096,
		AO: true, Shafts: true, Bloom: true, Lens: true, MotionBlur: true, TAA: true, Reflections: true, Rain: 1}
}

// Apply switches off in s what the budget does not allow; what it allows
// is left as the look set it.
func (b QualityBudget) Apply(s *PostSettings) {
	if !b.AO {
		s.AO = 0
	}
	if !b.Bloom {
		s.Bloom = 0
	}
	if !b.Lens {
		s.LensFlare, s.LensStreak, s.LensDirt = 0, 0, 0
	}
	if !b.MotionBlur {
		s.MotionBlur = 0
	}
	if !b.TAA {
		s.TAA = false
	}
}

// ApplyAtmosphere switches off the volumetric air the budget does not
// allow and thins the rain to its share.
func (b QualityBudget) ApplyAtmosphere(a *Atmosphere) {
	if !b.Shafts {
		a.Shafts, a.Lamps = 0, 0
	}
	a.Rain *= b.Rain
}

// QualityForAdapter picks a tier from the adapter's name (gpu.Adapter.Name,
// "NVIDIA GeForce RTX 4070 (vulkan)") and the platform: a phone or a
// software renderer is low, an integrated or mobile-class GPU medium,
// anything else high. Ultra is only ever chosen.
func QualityForAdapter(name string) QualityTier {
	return qualityFor(name, runtime.GOOS)
}

func qualityFor(name, goos string) QualityTier {
	n := strings.ToLower(name)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(n, w) {
				return true
			}
		}
		return false
	}
	switch {
	case goos == "android" || goos == "ios":
		return QualityLow
	case has("llvmpipe", "lavapipe", "swiftshader", "softpipe", "software", "basic render"):
		return QualityLow
	case has("intel") && !has(" arc"):
		return QualityMedium
	case has("mali", "adreno", "powervr", "videocore", "immortalis", "vivante"):
		return QualityMedium
	case goos == "js" && strings.TrimSpace(strings.Trim(n, "(),webgpu ")) == "":
		return QualityMedium // a browser that does not say what it runs on
	}
	return QualityHigh
}

// DynResEvent is what one DynamicResolution.Update decided.
type DynResEvent int8

const (
	DynResHeld     DynResEvent = iota // nothing changed
	DynResShrank                      // the scale went down
	DynResGrew                        // the scale went up
	DynResDropTier                    // at the floor and still slow: drop a tier
)

// DynamicResolution steers a render scale to hold a frame time. Feed it
// every frame's time; every Interval it takes the mean and moves the
// scale a step down when the mean is over Shrink, a step up when it is
// under Grow (after a wait that doubles each time a raise had to be taken
// back), and reports DynResDropTier when it is at Floor and still over
// Shrink twice running. The zero value is ready (scale 1, Saltline's
// thresholds).
type DynamicResolution struct {
	// Shrink and Grow are the mean frame times, in milliseconds, above
	// which the scale goes down and below which it may go up (0: 21.5 and
	// 17.4). Between them it holds.
	Shrink, Grow float32
	// Floor and Ceil bound the scale (0: 0.6 and 1).
	Floor, Ceil float32
	// Step is a move (0: 0.1); twice that when the mean is 1.5× Shrink.
	Step float32
	// Interval is how many seconds of frames each decision averages
	// (0: 0.5).
	Interval float32

	scale     float32 // 0 = not set: Ceil
	sum       float32 // ms over the interval
	n         int
	elapsed   float32 // s into the interval
	mean      float32 // the last interval's mean, ms
	sinceMove float32 // s since the scale last moved
	sinceGrow float32 // s since it last went up (large: never)
	growWait  float32 // s to wait at a level before going up (0: the base)
	floorHits int
}

const (
	dynResGrowWait    = 2  // s, the first wait before a raise
	dynResGrowWaitMax = 32 // s, the longest
	dynResRegret      = 3  // s: a shrink this soon after a raise takes it back
	dynResHitch       = 0.25
)

func (d *DynamicResolution) bounds() (lo, hi float32) {
	lo, hi = d.Floor, d.Ceil
	if hi <= 0 {
		hi = 1
	}
	if lo <= 0 {
		lo = 0.6
	}
	return min(lo, hi), hi
}

// Scale is the render scale now.
func (d *DynamicResolution) Scale() float32 {
	lo, hi := d.bounds()
	if d.scale <= 0 {
		return hi
	}
	return min(max(d.scale, lo), hi)
}

// SetScale sets the scale (clamped to Floor…Ceil) and starts measuring
// afresh.
func (d *DynamicResolution) SetScale(s float32) {
	lo, hi := d.bounds()
	d.scale = min(max(s, lo), hi)
	d.Reset()
}

// Reset forgets the frames measured so far and the floor hits: call it
// after a load, a screen change or anything else that stalls a frame on
// purpose.
func (d *DynamicResolution) Reset() {
	d.sum, d.n, d.elapsed, d.floorHits, d.sinceMove = 0, 0, 0, 0, 0
}

// Mean is the last decision's mean frame time in milliseconds (0 before
// the first).
func (d *DynamicResolution) Mean() float32 { return d.mean }

// Update takes one frame's time in seconds. A frame over a quarter of a
// second is a hitch (a load, a stall) and is not counted.
func (d *DynamicResolution) Update(dt float32) DynResEvent {
	if dt <= 0 || dt > dynResHitch {
		return DynResHeld
	}
	if d.scale <= 0 {
		d.scale = d.Scale()
	}
	if d.growWait <= 0 {
		d.growWait = dynResGrowWait
		d.sinceGrow = 1e9
	}
	d.sum += dt * 1000
	d.n++
	d.elapsed += dt
	d.sinceMove += dt
	d.sinceGrow += dt
	interval := d.Interval
	if interval <= 0 {
		interval = 0.5
	}
	if d.elapsed < interval {
		return DynResHeld
	}
	d.mean = d.sum / float32(d.n)
	d.sum, d.n, d.elapsed = 0, 0, 0

	shrink, grow, step := d.Shrink, d.Grow, d.Step
	if shrink <= 0 {
		shrink = 21.5
	}
	if grow <= 0 {
		grow = 17.4
	}
	if step <= 0 {
		step = 0.1
	}
	lo, hi := d.bounds()
	const eps = 1e-4
	switch {
	case d.mean > shrink:
		if d.scale <= lo+eps {
			d.floorHits++
			if d.floorHits >= 2 {
				d.floorHits = 0
				return DynResDropTier
			}
			return DynResHeld
		}
		if d.mean > 1.5*shrink {
			step *= 2
		}
		if d.sinceGrow < dynResRegret {
			// The last raise did not hold: wait longer before the next.
			d.growWait = min(d.growWait*2, dynResGrowWaitMax)
		}
		d.scale = max(d.scale-step, lo)
		d.sinceMove = 0
		return DynResShrank
	case d.mean < grow:
		d.floorHits = 0
		if d.scale >= hi-eps || d.sinceMove < d.growWait {
			return DynResHeld
		}
		d.scale = min(d.scale+step, hi)
		d.sinceMove, d.sinceGrow = 0, 0
		return DynResGrew
	}
	d.floorHits = 0
	return DynResHeld
}

// Quality is a tier and the dynamic resolution that serves it: the tier's
// budget sets the scale it starts at and the floor, and the controller
// asking for a tier drop takes it down one (never below Lowest).
type Quality struct {
	Tier QualityTier
	// Lowest is the tier dynamic resolution may drop to.
	Lowest QualityTier
	// Dynamic turns the steering on; off, the scale is the tier's (or
	// what Dyn.SetScale set).
	Dynamic bool
	Dyn     DynamicResolution
}

// NewQuality starts at a tier with dynamic resolution on.
func NewQuality(t QualityTier) *Quality {
	q := &Quality{Dynamic: true}
	q.SetTier(t)
	return q
}

// SetTier changes the tier: the budget's starting scale and floor.
func (q *Quality) SetTier(t QualityTier) {
	t = min(max(t, QualityLow), QualityUltra)
	q.Tier = t
	b := t.Budget()
	q.Dyn.Floor = b.MinScale
	q.Dyn.SetScale(b.Scale)
}

// Budget is the current tier's.
func (q *Quality) Budget() QualityBudget { return q.Tier.Budget() }

// Frame feeds one frame's time (seconds) to the steering and reports
// whether the tier dropped (what the budget sizes — the shadow map, the
// rain — may want rebuilding).
func (q *Quality) Frame(dt float32) bool {
	if !q.Dynamic {
		return false
	}
	if q.Dyn.Update(dt) == DynResDropTier && q.Tier > q.Lowest {
		q.SetTier(q.Tier - 1)
		q.Dyn.SetScale(q.Dyn.Floor + 0.1) // a lighter tier starts a step over its floor
		return true
	}
	return false
}

// Scale is the render scale for a w×h display: the steered scale, lowered
// further when the tier's pixel budget would be exceeded.
func (q *Quality) Scale(w, h int) float32 {
	s := q.Dyn.Scale()
	return CapScale(s, w, h, q.Budget().MaxPixels)
}

// CapScale lowers a render scale until a w×h target at that scale has at
// most maxPixels pixels (0: no cap).
func CapScale(s float32, w, h, maxPixels int) float32 {
	if maxPixels <= 0 || w <= 0 || h <= 0 {
		return s
	}
	if c := float32(math.Sqrt(float64(maxPixels) / float64(w*h))); c < s {
		return c
	}
	return s
}
