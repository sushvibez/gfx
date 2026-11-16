// Package wgsl is the WGSL that every 3D renderer on gfx includes, so that
// each technique exists once: the hashes and value noise, depth (linear,
// perspective or logarithmic) and soft-particle fades, PCSS soft shadows,
// GGX and the light falloff, the wet film, puddles and rain rings, the
// phase function and the lightning bolt.
//
// Each piece is a Go string constant of plain WGSL functions, prepended to
// a renderer's own module (Go constant concatenation is the include
// mechanism: `const shader = wgsl.Hash + wgsl.Shadow + "..."`). The
// functions are pure: they read no bindings and no host struct. A renderer
// passes in what only it knows: how it gets a position, a normal and a
// view distance (camera-relative or world, perspective or logarithmic
// depth), its shadow map and its constants. So the same PCSS kernel serves
// gfx's Renderer3D and a planet-scale renderer with camera-relative
// coordinates and log depth.
//
// Dependencies between the pieces are noted on each constant; a renderer
// includes each once, in that order (Hash first). Names are unprefixed but
// chosen not to collide with the hosts in this repository and the games
// built on it; a host must not define them again.
package wgsl

// Join concatenates pieces into one module source, in order. Constant
// concatenation does the same at compile time; Join is for a module
// assembled at run time.
func Join(parts ...string) string {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 0, n)
	for _, p := range parts {
		b = append(b, p...)
	}
	return string(b)
}
