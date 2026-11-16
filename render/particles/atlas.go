package particles

import (
	"math"
	"math/rand"
	"sync"
)

// The smoke atlas: a few frames of billowed puff baked on the CPU once,
// sampled by the particle shader (Shade: one frame per particle, picked by
// its seed). Each texel holds
//
//	R, G  the billow's surface normal across the sprite (x right, y up),
//	      0.5 + 0.5·n
//	B     thickness: how much smoke the line of sight crosses there
//	A     density: what the opacity is cut from; ragged inside (noise) so
//	      an ageing puff erodes into wisps instead of shrinking whole.
//
// A frame is a heightfield of overlapping balls (the cauliflower of a
// smoke puff) roughened with a billowy noise, and its normal is the
// heightfield's slope.

// The atlas's layout: SmokeCols × SmokeRows frames of SmokeCell² texels,
// SmokeMips levels.
const (
	SmokeCols = 4
	SmokeRows = 2
	SmokeCell = 128
	// SmokeMips is how far the chain goes (a cell of 8 texels at the
	// last), so a puff a few pixels across does not shimmer.
	SmokeMips = 5
)

var (
	smokeOnce   sync.Once
	smokeLevels [][]byte
)

// SmokeAtlas returns the atlas's mip chain (RGBA8, level 0 is
// SmokeCols·SmokeCell × SmokeRows·SmokeCell), baked on first use.
func SmokeAtlas() [][]byte {
	smokeOnce.Do(func() { smokeLevels = bakeSmokeAtlas() })
	return smokeLevels
}

// smokeHash is a lattice hash in 0..1.
func smokeHash(x, y, z int32, seed uint32) float64 {
	h := uint32(x)*0x8da6b343 ^ uint32(y)*0xd8163841 ^ uint32(z)*0xcb1ab31f ^ seed*0x165667b1
	h ^= h >> 15
	h *= 0x2c1b3c6d
	h ^= h >> 12
	h *= 0x297a2d39
	h ^= h >> 15
	return float64(h) / 4294967296.0
}

// smokeNoise is smooth value noise in 0..1.
func smokeNoise(x, y, z float64, seed uint32) float64 {
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	ix, iy, iz := int32(fx), int32(fy), int32(fz)
	tx, ty, tz := x-fx, y-fy, z-fz
	tx, ty, tz = tx*tx*(3-2*tx), ty*ty*(3-2*ty), tz*tz*(3-2*tz)
	lerp := func(a, b, t float64) float64 { return a + (b-a)*t }
	c := func(dx, dy, dz int32) float64 { return smokeHash(ix+dx, iy+dy, iz+dz, seed) }
	return lerp(
		lerp(lerp(c(0, 0, 0), c(1, 0, 0), tx), lerp(c(0, 1, 0), c(1, 1, 0), tx), ty),
		lerp(lerp(c(0, 0, 1), c(1, 0, 1), tx), lerp(c(0, 1, 1), c(1, 1, 1), tx), ty), tz)
}

// smokeBillow is a turbulence fbm (folded noise: rounded humps, sharp creases)
// in 0..~1.
func smokeBillow(x, y, z float64, seed uint32) float64 {
	s, a, norm := 0.0, 0.5, 0.0
	for o := 0; o < 4; o++ {
		s += a * math.Abs(smokeNoise(x, y, z, seed+uint32(o))*2-1)
		norm += a
		x, y, z, a = x*2.03, y*2.03, z*2.03, a*0.5
	}
	return s / norm
}

func bakeSmokeAtlas() [][]byte {
	W, H := SmokeCols*SmokeCell, SmokeRows*SmokeCell
	n := SmokeCell
	px := make([]byte, W*H*4)
	h := make([]float64, n*n)
	dens := make([]float64, n*n)
	for f := 0; f < SmokeCols*SmokeRows; f++ {
		rng := rand.New(rand.NewSource(int64(f*7919 + 13)))
		seed := uint32(f*31 + 5)
		// The lobes: soft blobs, a big core and smaller ones round it,
		// summed (so they merge like metaballs) over a plane bent by
		// noise (so no lobe is a circle).
		type lobe struct{ x, y, r, w float64 }
		lobes := []lobe{{0, 0, 0.42, 1}}
		for i := 0; i < 26; i++ {
			a := rng.Float64() * 2 * math.Pi
			d := 0.58 * math.Sqrt(rng.Float64())
			r := (0.10 + 0.17*rng.Float64()) * (1 - 0.4*d)
			lobes = append(lobes, lobe{d * math.Cos(a), d * math.Sin(a), r, 0.55 + 0.45*rng.Float64()})
		}
		dmax := 0.0
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				x := (float64(i)+0.5)/float64(n)*2 - 1
				y := 1 - (float64(j)+0.5)/float64(n)*2
				z := float64(f) * 3.7
				wx := x + 0.16*(smokeNoise(x*2.5, y*2.5, z, seed+40)-0.5)
				wy := y + 0.16*(smokeNoise(x*2.5+9, y*2.5, z, seed+41)-0.5)
				d := 0.0
				for _, l := range lobes {
					dx, dy := wx-l.x, wy-l.y
					d += l.w * math.Exp(-(dx*dx+dy*dy)/(l.r*l.r))
				}
				// Rolled, cauliflower relief where there is smoke (two
				// octaves: the finer grain is the density's, below).
				d *= 0.7 + 0.45*smokeNoise(x*3, y*3, z, seed) + 0.2*smokeNoise(x*6.5, y*6.5, z, seed+1)
				// Never past the frame (and so never into the next one
				// at a small mip).
				d *= smokeStep(0.97, 0.78, math.Hypot(x, y))
				h[j*n+i] = d
				dmax = math.Max(dmax, d)
				dens[j*n+i] = smokeBillow(x*2.3+11, y*2.3, z+5, seed+17)
			}
		}
		ox, oy := (f%SmokeCols)*n, (f/SmokeCols)*n
		at := func(i, j int) float64 {
			i, j = max(0, min(n-1, i)), max(0, min(n-1, j))
			return math.Sqrt(h[j*n+i] / dmax)
		}
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				dn := h[j*n+i] / dmax
				// The surface is the square root of the density (a
				// ball's profile); its slope in sprite units per unit
				// across (a texel is 2/n units) gives the normal.
				gx := (at(i+1, j) - at(i-1, j)) / (4.0 / float64(n))
				gy := (at(i, j-1) - at(i, j+1)) / (4.0 / float64(n))
				const k = 0.35
				nx, ny, nz := -gx*k, -gy*k, 1.0
				l := math.Sqrt(nx*nx + ny*ny + nz*nz)
				nx, ny = nx/l, ny/l
				a := smokeStep(0.04, 0.55, dn) * (0.45 + 0.75*dens[j*n+i])
				o := ((oy+j)*W + ox + i) * 4
				px[o] = smokeU8(0.5 + 0.5*nx)
				px[o+1] = smokeU8(0.5 + 0.5*ny)
				px[o+2] = smokeU8(math.Sqrt(dn))
				px[o+3] = smokeU8(math.Min(a, 1))
			}
		}
	}
	levels := [][]byte{px}
	w, hh := W, H
	for m := 1; m < SmokeMips; m++ {
		src := levels[m-1]
		nw, nh := w/2, hh/2
		dst := make([]byte, nw*nh*4)
		for y := 0; y < nh; y++ {
			for x := 0; x < nw; x++ {
				for c := 0; c < 4; c++ {
					s := int(src[((2*y)*w+2*x)*4+c]) + int(src[((2*y)*w+2*x+1)*4+c]) +
						int(src[((2*y+1)*w+2*x)*4+c]) + int(src[((2*y+1)*w+2*x+1)*4+c])
					dst[(y*nw+x)*4+c] = byte((s + 2) / 4)
				}
			}
		}
		levels = append(levels, dst)
		w, hh = nw, nh
	}
	return levels
}

func smokeStep(e0, e1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

func smokeU8(v float64) byte { return byte(math.Max(0, math.Min(255, math.Round(v*255)))) }
