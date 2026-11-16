package particles

import (
	"math"
	"sort"
)

// Light is a short-lived point light an effect offers: a muzzle flash, a
// laser's burn, a fireball, a burning wreck, a lamp's flicker.
type Light struct {
	Pos   Vec3
	Color [3]float32 // colour × intensity
	Range float32    // m
}

// Lights keeps the few strongest lights of a frame as the eye sees them:
// every effect that burns offers one, and only Max are lit (a renderer's
// pool of point lights is small, and the rest would cost every pixel).
type Lights struct {
	Max   int // 0: 6
	cands []lightCand
	out   []Light
}

type lightCand struct {
	l     Light
	score float64
}

// Reset starts a frame's offers.
func (ls *Lights) Reset() { ls.cands = ls.cands[:0] }

// Offer adds a light at p of colour col × k reaching rng metres, scored by
// how much of it the eye at eye can see: its strength over the distance
// beyond its reach (a light past its range from the eye still lights smoke
// and ground there, but less of the screen).
func (ls *Lights) Offer(eye, p Vec3, col [3]float32, k float32, rng float64) {
	if k <= 0.01 {
		return
	}
	far := math.Max(p.Sub(eye).Length()-rng, 0) / rng
	ls.cands = append(ls.cands, lightCand{
		l:     Light{Pos: p, Color: [3]float32{col[0] * k, col[1] * k, col[2] * k}, Range: float32(rng)},
		score: float64(k) * rng / (1 + far*far),
	})
}

// Pick returns the strongest Max of this frame's offers, strongest first
// (valid until the next Pick).
func (ls *Lights) Pick() []Light {
	max := ls.Max
	if max == 0 {
		max = 6
	}
	sort.Slice(ls.cands, func(i, j int) bool { return ls.cands[i].score > ls.cands[j].score })
	ls.out = ls.out[:0]
	for i := 0; i < len(ls.cands) && i < max; i++ {
		ls.out = append(ls.out, ls.cands[i].l)
	}
	return ls.out
}

// Flicker is a fire's unsteady brightness, about 0.55..1, its own for
// seed, at time t (s).
func Flicker(t, seed float64) float64 {
	return 0.78 + 0.12*math.Sin(t*23+seed*17) + 0.1*math.Sin(t*37.7+seed*5.3)
}
