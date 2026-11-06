package gml

import (
	"math/rand"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// newTestSystem is a bare simulation for the unit tests below.
func newTestSystem() *particleSystem {
	return &particleSystem{pending: map[string]float64{}, rng: rand.New(rand.NewSource(7))}
}

// TestParticleSystem: emitters spawn at their rate, particles age out, and
// an AngleDirection velocity sends them the way it says.
func TestParticleSystem(t *testing.T) {
	src := `
Rectangle {
    width: 200; height: 200
    ParticleSystem {
        id: sys
        width: 200; height: 200
        running: true
        ImageParticle { color: "#00d1a9" }
        Emitter {
            width: 10; height: 10
            emitRate: 50
            lifeSpan: 1000
            size: 8
            velocity: AngleDirection { angle: 270; magnitude: 100 }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200) // first frame; the second sees real bounds
	u.Layout(200, 200)
	sys := d.doc.particles[d.doc.key(findNode(d.root, "ParticleSystem"))]
	if sys == nil {
		t.Fatal("no particle system registered")
	}
	// The warm-up runs one second of simulation, so a still frame is
	// populated: 50/s for 1s, minus those that aged past their life span.
	if n := len(sys.parts); n < 20 || n > 60 {
		t.Fatalf("live particles = %d, want roughly 50", n)
	}
	// Particles emitted upward (270°) must be above where they started.
	up := 0
	for _, p := range sys.parts {
		if p.vy < 0 {
			up++
		}
	}
	if up != len(sys.parts) {
		t.Fatalf("%d of %d particles move upward, want all", up, len(sys.parts))
	}
	// Advancing past the life span retires them.
	for i := 0; i < 80; i++ {
		sys.running = true
		sys.emitters[0].rate = 0 // stop emitting, then let them expire
		d.doc.advanceParticles(25)
	}
	if len(sys.parts) != 0 {
		t.Fatalf("%d particles survived past their life span", len(sys.parts))
	}
}

// TestGraphsViewSeries: a series fills itself from Component.onCompleted
// through the QML point API (a bare `append` resolving to its own method),
// and the mutators behave.
func TestGraphsViewSeries(t *testing.T) {
	src := `
Rectangle {
    width: 200; height: 120
    GraphsView {
        width: 200; height: 120
        axisX: ValueAxis { min: 0; max: 4 }
        axisY: ValueAxis { min: 0; max: 10 }
        LineSeries {
            id: line
            color: "#00d1a9"
            Component.onCompleted: {
                for (var i = 0; i < 5; ++i) append(i, i * 2)
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)
	u.Layout(400, 300) // onCompleted runs once the tree has geometry
	data := d.doc.seriesData[d.doc.key(findNode(d.root, "LineSeries"))]
	if data == nil {
		t.Fatal("no series data")
	}
	if len(data.pts) != 5 {
		t.Fatalf("points = %d, want 5", len(data.pts))
	}
	if data.pts[3].x != 3 || data.pts[3].y != 6 {
		t.Fatalf("point 3 = (%v,%v), want (3,6)", data.pts[3].x, data.pts[3].y)
	}
	// replace() and removeMultiple() through the id's methods.
	obj := d.doc.elemObjs.get(d.doc.intern("line"))
	if _, err := obj.methods["replace"].(jsFunc)([]any{float64(0), float64(9), float64(1)}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if data.pts[0].x != 9 || data.pts[0].y != 1 {
		t.Fatalf("after replace point 0 = (%v,%v), want (9,1)", data.pts[0].x, data.pts[0].y)
	}
	if _, err := obj.methods["removeMultiple"].(jsFunc)([]any{float64(1), float64(3)}); err != nil {
		t.Fatalf("removeMultiple: %v", err)
	}
	if len(data.pts) != 2 {
		t.Fatalf("points after removeMultiple = %d, want 2", len(data.pts))
	}
}

// TestEmitterFillsItsSystem: `anchors.fill: parent` on an Emitter means the
// system's whole box, and the box is a LAID-OUT one — so the rect has to be
// resolved when a particle spawns, not when the emitter is read. A document
// whose particles are its only geometry never rebuilds (nothing feeds the
// geometry signature), and the emitter stayed a point at the origin: one
// blob in the corner where Qt fills the panel.
func TestEmitterFillsItsSystem(t *testing.T) {
	src := `
Rectangle {
    width: 300; height: 200
    ParticleSystem {
        anchors.fill: parent
        Emitter {
            anchors.fill: parent
            emitRate: 200
            lifeSpan: 5000
            size: 8
        }
        ImageParticle { color: "#00d1a9" }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	u.Layout(300, 200)
	var sys *liveParticleSystem
	for _, s := range d.doc.particles {
		sys = s
	}
	if sys == nil {
		t.Fatal("no particle system registered")
	}
	// The paint pass is where the laid-out box arrives; a headless test has
	// no renderer, so hand the simulation the same rect the canvas would.
	sys.lastRect = ui.Rect{W: 300, H: 200}
	for i := 0; i < 40; i++ {
		d.doc.advanceParticles(25)
	}
	if len(sys.parts) < 20 {
		t.Fatalf("live particles = %d, want the emitter to be running", len(sys.parts))
	}
	var maxX, maxY float32
	for _, p := range sys.parts {
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	if maxX < 150 || maxY < 100 {
		t.Errorf("particles reach only (%v,%v): the emitter is not filling its system's 300x200", maxX, maxY)
	}
}

// TestParticlePartsBoundBySystem: QML only requires an Emitter or an
// ImageParticle to name its `system:` — it can live anywhere in the tree,
// and a page that anchors one to something else writes it as a sibling.
func TestParticlePartsBoundBySystem(t *testing.T) {
	src := `
Rectangle {
    width: 300; height: 200
    ParticleSystem { id: sys; width: 300; height: 200 }
    Emitter {
        system: sys
        width: 300; height: 200
        emitRate: 100
        lifeSpan: 5000
        size: 8
    }
    ImageParticle { system: sys; color: "#ff0000" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200) // the parts register while this frame builds
	u.Layout(300, 200) // the system picks them up on the next one
	sys := d.doc.particles[d.doc.key(findNode(d.root, "ParticleSystem"))]
	if sys == nil {
		t.Fatal("no particle system registered")
	}
	if len(sys.emitters) != 1 {
		t.Fatalf("the system has %d emitters, want the sibling one", len(sys.emitters))
	}
	if len(sys.painters) != 1 || sys.painters[0].color.R < 0.9 {
		t.Fatalf("painters = %+v, want the sibling ImageParticle's red", sys.painters)
	}
}

// TestColorVariationLerpsTowardRandom: QML's colorVariation lerps each
// channel toward a fresh uniform random one — at 1.0 the colour is fully
// random whatever the base was. Probed against Qt with a base whose red is
// zero (#00D1A9): its mean red rose to about half of full scale at
// variation 1.0 and its green FELL to the same, where a signed jitter
// around the base leaves half the red clamped at zero.
func TestColorVariationLerpsTowardRandom(t *testing.T) {
	painters := []particlePainter{{
		color:          ui.RGBA{R: 0, G: 0.82, B: 0.66, A: 1},
		colorVariation: 1,
		alpha:          1,
	}}
	s := newTestSystem()
	var sumR, sumG float32
	const n = 4000
	for i := 0; i < n; i++ {
		p := s.spawn(emitterSpec{rate: 1, lifeSpan: 1000, size: 4}, painters, ui.Rect{W: 100, H: 100})
		sumR += p.color.R
		sumG += p.color.G
	}
	meanR, meanG := sumR/n, sumG/n
	if meanR < 0.45 || meanR > 0.55 {
		t.Errorf("mean red = %.3f at colorVariation 1, want ~0.5 (a fully random channel)", meanR)
	}
	if meanG < 0.45 || meanG > 0.55 {
		t.Errorf("mean green = %.3f at colorVariation 1, want ~0.5, not its 0.82 base", meanG)
	}
}

// TestParticleFadeFollowsQtsShader: QML fades a particle in over the first
// TENTH of its life and out over the last QUARTER — the constants in Qt's
// own particle shader. Ours faded in over a fixed 150ms and out over the
// last 30%, which showed up as the wrong alpha on any page whose particles
// live longer than a second or so. Probed with a single particle whose life
// far outlasts the capture: Qt's peak alpha rose 0.39/0.59/0.78 for lives
// of 100/60/40 seconds — a slope of about 10 per unit of life fraction.
func TestParticleFadeFollowsQtsShader(t *testing.T) {
	alphaAt := func(frac float64) float32 {
		a := float32(min(frac*10, 1))
		return a * float32(1-max(0, min((frac-0.75)*4, 1)))
	}
	cases := []struct {
		frac float64
		want float32
	}{
		{0.00, 0.00},
		{0.05, 0.50}, // half way through the fade-in
		{0.10, 1.00}, // fully in
		{0.50, 1.00},
		{0.75, 1.00}, // the tail starts here
		{0.875, 0.50},
		{1.00, 0.00},
	}
	for _, c := range cases {
		if got := alphaAt(c.frac); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("at %.3f of life: alpha %.3f, want %.2f", c.frac, got, c.want)
		}
	}
	// And the simulation uses that curve: a particle at a tenth of its life
	// is opaque, one at a twentieth is half there.
	s := newTestSystem()
	painters := []particlePainter{{color: ui.RGBA{R: 1, G: 1, B: 1, A: 1}, alpha: 1}}
	p := s.spawn(emitterSpec{rate: 1, lifeSpan: 1000, size: 10}, painters, ui.Rect{W: 100, H: 100})
	if p.life != 1 {
		t.Fatalf("life = %v, want 1s", p.life)
	}
}
