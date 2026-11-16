// Package particles is gfx's particle system: a bounded pool of soft
// puffs and hard chips simulated on the CPU (smoke, dust, steam, a flame
// lobe, debris), the billowed smoke sprite sheet they are drawn from, the
// WGSL of the soft, depth-faded, lit pass that draws them, the WGSL of
// precipitation as drops in the world, and a pool of short-lived point
// lights (muzzle flashes, fires) ranked by what the eye sees of them.
//
// The pool works in float64 in any frame where "up" and "height above the
// ground" mean something: a flat world (Flat: y up) or a round planet
// (Round: up is away from the centre, ECEF). A renderer draws the pool's
// Sprites through WGSL (Shade, Precip) to which it adds only how it lights
// a point and projects it (camera-relative or world, perspective or
// logarithmic depth).
package particles

import (
	"math"
	"math/rand"
)

// Vec3 is a float64 vector. Any struct{ X, Y, Z float64 } converts to it
// (a game's own vector type, such as PTG's geo.Vec3).
type Vec3 struct{ X, Y, Z float64 }

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Dot(b Vec3) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Length() float64      { return math.Sqrt(a.Dot(a)) }

// Normalize returns a unit vector (zero stays zero).
func (a Vec3) Normalize() Vec3 {
	l := a.Length()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

// Space says which way is up and how high a point stands, for buoyancy,
// gravity and the ground.
type Space interface {
	// Up is the unit vector away from the ground at p.
	Up(p Vec3) Vec3
	// Height is p's height on the space's own scale (y, or the distance
	// from a planet's centre); a puff's Ground is on the same scale.
	Height(p Vec3) float64
	// AtHeight is p moved along Up to height h.
	AtHeight(p Vec3, h float64) Vec3
}

// Flat is a flat world, y up.
type Flat struct{}

func (Flat) Up(Vec3) Vec3                    { return Vec3{0, 1, 0} }
func (Flat) Height(p Vec3) float64           { return p.Y }
func (Flat) AtHeight(p Vec3, h float64) Vec3 { return Vec3{p.X, h, p.Z} }

// Round is a planet centred on the origin (ECEF): up is away from the
// centre, height the distance from it.
type Round struct{}

func (Round) Up(p Vec3) Vec3        { return p.Normalize() }
func (Round) Height(p Vec3) float64 { return p.Length() }
func (Round) AtHeight(p Vec3, h float64) Vec3 {
	if l := p.Length(); l > 0 {
		return p.Scale(h / l)
	}
	return p
}

// Kind is a particle's shape.
type Kind uint8

const (
	// Puff is a soft cloud from the smoke atlas: smoke, dust, steam, and
	// with Emit a flame lobe.
	Puff Kind = iota
	// Chip is a small hard fragment: debris, a clod of earth.
	Chip
)

// Particle is one simulated particle in a Pool.
type Particle struct {
	Pos, Vel   Vec3
	Age, Life  float64 // seconds; it is gone at Life
	R0, R1     float64 // radius at birth and at the end (m)
	Color      [3]float32
	Alpha      float32
	Emit       float32 // self-emission (radiance = Color·Emit at the core)
	Kind       Kind
	Rot, Spin  float32 // turn in the screen plane (rad) and its rate (rad/s)
	Seed       float32 // 0..1: its own atlas frame (0: drawn by Add)
	Rise       float64 // buoyancy (m/s², up; negative settles)
	Drag       float64 // 1/s toward the wind
	Gravity    bool
	Grounded   bool    // kept above Ground
	Ground     float64 // the ground's height under it (Space.Height)
	StopGround bool    // lies where it lands (fragments)
	FadeIn     float64 // seconds to full opacity (0: 0.06)
}

// Sprite is one particle as drawn this frame.
type Sprite struct {
	Pos    Vec3       // centre
	Radius float64    // m
	Color  [3]float32 // albedo (linear); also the emission's colour
	Alpha  float32    // peak opacity 0..1
	Emit   float32    // self-emission: radiance = Color·Emit at the core (a flame, an ember)
	Rot    float32    // rotation in the screen plane (rad), so puffs are not all one shape
	Kind   Kind
	Seed   float32 // 0..1: the puff's own shape (its atlas frame)
	Age    float32 // 0..1 of its life: the density threshold rises with it (erosion)
}

// Pool is a bounded set of particles stepped together: each rises or
// settles, drags toward the wind, grows and fades over its life, and is
// kept above the ground (a puff sits on it at a third of its size, a
// fragment stops).
type Pool struct {
	Space Space
	Max   int  // spawns past it are dropped (0: 2048)
	Wind  Vec3 // the air's velocity (m/s)
	Rng   *rand.Rand
	Parts []Particle
}

// NewPool makes an empty pool of at most max particles in space s.
func NewPool(s Space, max int, seed int64) *Pool {
	return &Pool{Space: s, Max: max, Rng: rand.New(rand.NewSource(seed))}
}

// Add spawns p, drawing its seed and turn if they are zero.
func (pl *Pool) Add(p Particle) {
	max := pl.Max
	if max == 0 {
		max = 2048
	}
	if len(pl.Parts) >= max {
		return
	}
	if p.Seed == 0 {
		p.Seed = pl.Rng.Float32()
	}
	if p.Rot == 0 {
		p.Rot = pl.Rng.Float32() * 6.283
	}
	pl.Parts = append(pl.Parts, p)
}

// Rnd is uniform in [a, b).
func (pl *Pool) Rnd(a, b float64) float64 { return a + (b-a)*pl.Rng.Float64() }

// Scatter is a random unit vector biased toward up by k (0: any way, 1:
// mostly up).
func (pl *Pool) Scatter(up Vec3, k float64) Vec3 {
	for {
		v := Vec3{pl.Rnd(-1, 1), pl.Rnd(-1, 1), pl.Rnd(-1, 1)}
		if l := v.Length(); l > 0.05 && l <= 1 {
			v = v.Scale(1 / l)
			return v.Add(up.Scale(k)).Normalize()
		}
	}
}

// Step ages and moves every particle by dt seconds and drops the dead.
func (pl *Pool) Step(dt float64) {
	keep := pl.Parts[:0]
	for _, p := range pl.Parts {
		p.Age += dt
		if p.Age >= p.Life {
			continue
		}
		up := pl.Space.Up(p.Pos)
		acc := pl.Wind.Sub(p.Vel).Scale(p.Drag).Add(up.Scale(p.Rise))
		if p.Gravity {
			acc = acc.Add(up.Scale(-9.8))
		}
		p.Vel = p.Vel.Add(acc.Scale(dt))
		p.Pos = p.Pos.Add(p.Vel.Scale(dt))
		if p.Grounded {
			// Kept above the ground: a puff sits on it at a third of its
			// size (it spreads rather than sinking), a fragment stops.
			r := p.R0 + (p.R1-p.R0)*(p.Age/p.Life)
			floor := p.Ground + r*0.35
			if p.Kind == Chip {
				floor = p.Ground + p.R0*0.5
			}
			if h := pl.Space.Height(p.Pos); h < floor {
				p.Pos = pl.Space.AtHeight(p.Pos, floor)
				if vu := p.Vel.Dot(up); vu < 0 {
					p.Vel = p.Vel.Sub(up.Scale(vu))
				}
				if p.StopGround {
					p.Vel, p.Spin, p.Gravity = Vec3{}, 0, false
					p.Drag = 0
				}
			}
		}
		keep = append(keep, p)
	}
	pl.Parts = keep
}

// Sprites appends this frame's draw list to dst: each particle grown
// toward R1 (fast at first), faded in over FadeIn and out over its life
// (a chip only at the end), its emission cooling, its turn spinning.
func (pl *Pool) Sprites(dst []Sprite) []Sprite {
	for i := range pl.Parts {
		p := &pl.Parts[i]
		t := p.Age / p.Life
		grow := 1 - (1-t)*(1-t)
		in := p.FadeIn
		if in == 0 {
			in = 0.06
		}
		fade := float32(math.Pow(1-t, 1.5)) * float32(math.Min(p.Age/in, 1))
		if p.Kind == Chip {
			fade = float32(math.Min((1-t)*4, 1))
		}
		dst = append(dst, Sprite{
			Pos: p.Pos, Radius: p.R0 + (p.R1-p.R0)*grow, Color: p.Color,
			Alpha: p.Alpha * fade, Emit: p.Emit * float32(1-t), Rot: p.Rot + p.Spin*float32(p.Age),
			Kind: p.Kind, Seed: p.Seed, Age: float32(t),
		})
	}
	return dst
}
