package particles

import (
	"math"
	"testing"
)

// A puff rises and drifts with the wind; a chip falls and lies where it
// lands; both fade out and are gone at the end of their lives.
func TestPoolSteps(t *testing.T) {
	pl := NewPool(Flat{}, 8, 1)
	pl.Wind = Vec3{2, 0, 0}
	pl.Add(Particle{Pos: Vec3{0, 1, 0}, Life: 2, R0: 0.2, R1: 1, Alpha: 1, Rise: 0.5, Drag: 1})
	pl.Add(Particle{Pos: Vec3{0, 1, 0}, Vel: Vec3{0, 3, 0}, Life: 2, R0: 0.1, Alpha: 1, Kind: Chip,
		Gravity: true, Grounded: true, StopGround: true})
	for i := 0; i < 60; i++ {
		pl.Step(1.0 / 60)
	}
	puff, chip := pl.Parts[0], pl.Parts[1]
	if puff.Pos.Y <= 1 || puff.Pos.X <= 0 {
		t.Errorf("the puff should rise and drift downwind: %+v", puff.Pos)
	}
	if math.Abs(chip.Pos.Y-0.05) > 1e-6 || chip.Vel != (Vec3{}) {
		t.Errorf("the chip should lie on the ground at half its size: %+v %+v", chip.Pos, chip.Vel)
	}
	sp := pl.Sprites(nil)
	if len(sp) != 2 || sp[0].Radius <= 0.2 || sp[0].Alpha >= 1 || sp[0].Age <= 0 {
		t.Errorf("sprites: %+v", sp)
	}
	for i := 0; i < 61; i++ {
		pl.Step(1.0 / 60)
	}
	if len(pl.Parts) != 0 {
		t.Errorf("%d particles outlived their life", len(pl.Parts))
	}
}

// On a round planet up is away from the centre and the ground is a radius.
func TestPoolRound(t *testing.T) {
	const R = 6.371e6
	pl := NewPool(Round{}, 0, 1)
	pl.Add(Particle{Pos: Vec3{0, 0, R + 1}, Life: 5, R0: 0.1, Kind: Chip, Gravity: true, Grounded: true, Ground: R, StopGround: true})
	for i := 0; i < 120; i++ {
		pl.Step(1.0 / 60)
	}
	if h := pl.Parts[0].Pos.Length() - R; math.Abs(h-0.05) > 1e-3 {
		t.Errorf("the chip should lie on the planet: %.4f m up", h)
	}
}

// The strongest lights as seen from the eye are kept.
func TestLightsPick(t *testing.T) {
	var ls Lights
	ls.Max = 2
	ls.Reset()
	eye := Vec3{}
	ls.Offer(eye, Vec3{1000, 0, 0}, [3]float32{1, 1, 1}, 50, 10) // far away
	ls.Offer(eye, Vec3{5, 0, 0}, [3]float32{1, 1, 1}, 8, 10)
	ls.Offer(eye, Vec3{3, 0, 0}, [3]float32{1, 1, 1}, 4, 5)
	ls.Offer(eye, Vec3{3, 0, 0}, [3]float32{1, 1, 1}, 0.001, 5) // too faint to offer
	got := ls.Pick()
	if len(got) != 2 || got[0].Pos.X != 5 || got[1].Pos.X != 3 {
		t.Errorf("picked %+v", got)
	}
}

// The atlas bakes its mip chain at the documented size.
func TestSmokeAtlas(t *testing.T) {
	lv := SmokeAtlas()
	if len(lv) != SmokeMips || len(lv[0]) != SmokeCols*SmokeCell*SmokeRows*SmokeCell*4 {
		t.Fatalf("atlas: %d levels, %d bytes", len(lv), len(lv[0]))
	}
}
