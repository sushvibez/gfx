//go:build linux || windows || js

package shell

import "github.com/sushvibez/gfx/render"

// Frame is the per-frame context handed to a game's update function: the
// drawing surface, its pixel size, elapsed seconds since the last frame, and
// the current input state. Draw with G (render.Renderer2D: FillRect/Quad/
// DrawText). Return false from the update to quit.
type Frame struct {
	G    *render.Renderer2D
	Font *render.Font
	W, H int
	Dt   float64 // seconds since previous frame
	In   *Input

	// DragRegion, set by the game, reports whether a point (logical
	// pixels) is the title-bar area that moves an Undecorated window;
	// presses there start a move from the next frame on. Undecorated
	// windows also resize from a ResizeBand-wide border unless
	// Unresizable. RunGame only.
	DragRegion func(x, y float32) bool

	skip bool
}

// Unchanged tells the shell this frame changed nothing, so it need not
// be submitted or presented and the loop can go to sleep until
// something happens.
//
// A game loop renders continuously because it has to: the shell cannot
// know whether the world moved. RunUI can — it watches properties,
// animations and input — and a static window there costs nothing. This
// is how a game-loop app says the same thing.
//
// Input, a resize and the window's own damage override it, so a frame
// that says this can still be drawn; the flag is a hint, not a veto.
// Call it and return early to skip the drawing as well — although the
// drawing is not where the cost is. Submitting and presenting a frame
// is about 0.3 ms of CPU whether it holds sixty runs of text or
// nothing at all.
func (f *Frame) Unchanged() { f.skip = true }
