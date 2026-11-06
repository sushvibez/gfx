//go:build linux || windows || js

// Distance-field text, side by side with the rasterised glyph atlas it
// replaces.
//
// The left column is the atlas the renderer has always used: a sheet of
// coverage masks per pixel size, rasterised by the font face and correct
// only at the size it was made for. The right column is one multi-channel
// distance field per glyph, generated once at 32 px/em and read back at
// whatever size the vertex asks for.
//
// At a fixed size the two are hard to tell apart, which is the point —
// that is what "matches the atlas it replaces" means. The difference is
// in the three panels underneath:
//
//   - a size that slides continuously. Watch the two lines: the field
//     glides and the raster line steps. The field line is DrawTextSize
//     off a single font; the raster line needs a Font, a sheet and a
//     rasterisation per size, so this demo rounds it to two pixels
//     rather than build one every frame. Even rounded, the counter above
//     it climbs into the tens of megabytes; unrounded it does not stop.
//   - a line that rotates. Both manage this — the atlas by sampling a mask
//     made for the upright grid — but the atlas can only do it at a size
//     it already holds a sheet for.
//   - weight, outline and glow. In a coverage mask those need a second
//     rasterisation each; in a field they are a threshold moved, a band
//     between two thresholds, and a softer ramp. They are set in ems, so
//     one setting means the same amount of bold at 12 px and at 120.
//
// Run it:
//
//	go run ./msdftext
//	go run ./msdftext -screenshot out.png
//
// Keys: M switches the whole window between the two paths, SPACE pauses
// the animation, R holds the rotation still, ESC quits. -still starts
// paused — and paused really means paused: the frame declares itself
// unchanged, so the shell stops drawing until a key arrives.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 1180, 896

func rgb(hex uint32) render.RGBA {
	return render.RGBA{
		R: float32(hex>>16&0xff) / 255,
		G: float32(hex>>8&0xff) / 255,
		B: float32(hex&0xff) / 255,
		A: 1,
	}
}

var (
	bg      = rgb(0x0d0d12)
	panelBg = rgb(0x16161f)
	rule    = rgb(0x2a2a38)
	dim     = rgb(0x6f6f85)
	label   = rgb(0x9aa0b4)
	fg      = rgb(0xe8e8f0)
	accent  = rgb(0x74c0fc)
	warm    = rgb(0xffd43b)
	hot     = rgb(0xff6b4a)
)

const sample = "Hamburgefonstiv"

type demo struct {
	t      float64 // animation clock
	spin   float64 // rotation clock
	paused bool
	held   bool
	// bothPaths draws the left column with the raster atlas and the right
	// with the field; M drops that and puts one path in both columns, so
	// the same pixels can be compared against themselves.
	forced  bool
	useMSDF bool
}

// panel draws a titled box and returns its content origin.
func panel(g *render.Renderer2D, f *render.Font, x, y, w, h float32, title string) {
	g.FillRect(x, y, w, h, panelBg)
	g.FillRect(x, y, w, 1, rule)
	g.DrawText(f.AtSize(12), x+12, y+9, title, dim)
}

// column draws the sample at a spread of sizes, which is where the atlas
// pays for every one of them and the field pays for none.
func column(g *render.Renderer2D, f *render.Font, x, y, w float32) float32 {
	for _, s := range []float64{10, 13, 16, 22, 30, 44} {
		sf := f.AtSize(s)
		g.DrawText(sf, x, y, sample, fg)
		g.DrawText(f.AtSize(11), x+w-34, y+float32(s)*0.35, fmt.Sprintf("%gpx", s), dim)
		y += float32(s)*1.3 + 6
	}
	return y
}

func (d *demo) update(fr *shell.Frame) bool {
	g, base := fr.G, fr.Font
	in := fr.In
	if in.Down(app.KeyEscape) {
		return false
	}
	if in.Pressed(app.KeyNone, 'm') {
		d.forced = !d.forced
		d.useMSDF = true
	}
	if d.forced && in.Pressed(app.KeyNone, 'n') {
		d.useMSDF = !d.useMSDF
	}
	if in.Pressed(app.KeyNone, ' ') {
		d.paused = !d.paused
	}
	d.held = in.Rune('r')
	if !d.paused {
		d.t += fr.Dt
	}
	if !d.held && !d.paused {
		d.spin += fr.Dt
	}

	// Everything the window draws before the first DrawText decides
	// nothing; the path is chosen per run of text, so both can appear in
	// one frame.
	field := func(on bool) { g.SetMSDFText(on && (!d.forced || d.useMSDF)) }

	w, h := float32(fr.W), float32(fr.H)
	g.FillRect(0, 0, w, h, bg)

	// Header.
	field(true)
	g.DrawText(base.AtSize(22), 24, 18, "Distance-field text", fg)
	field(false)
	mode := "left: raster atlas   right: distance field"
	if d.forced {
		mode = "both columns: raster atlas"
		if d.useMSDF {
			mode = "both columns: distance field"
		}
	}
	g.DrawText(base.AtSize(13), 24, 50, mode+"    —    M: compare/one path   N: which path   SPACE: pause   R: hold rotation   ESC: quit", label)

	// --- the two columns, same text, same sizes -----------------------
	const colY = 86
	colW := (w - 72) / 2
	panel(g, base, 24, colY, colW, 300, "RASTERISED ATLAS — one sheet per pixel size")
	panel(g, base, 48+colW, colY, colW, 300, "DISTANCE FIELD — one sheet for all of them")

	field(false)
	column(g, base, 40, colY+34, colW-16)
	field(true)
	column(g, base, 64+colW, colY+34, colW-16)

	// What each path has actually built for everything drawn so far.
	// The sliding size below is what makes this move: every step is a
	// pixel size of its own, so the atlas rasterises the whole run again
	// and keeps another megabyte of sheet, while the field reuses the
	// tiles it already has.
	sheets, rasterGlyphs, rasterBytes, tiles, fieldBytes := base.AtlasStats()
	mb := func(b int) float64 { return float64(b) / (1 << 20) }
	field(false)
	g.DrawText(base.AtSize(13), 40, colY+264,
		fmt.Sprintf("%d sheets · %d glyphs rasterised · %.0f MB", sheets, rasterGlyphs, mb(rasterBytes)), hot)
	g.DrawText(base.AtSize(13), 64+colW, colY+264,
		fmt.Sprintf("1 sheet · %d tiles · %.0f MB, whatever the sizes", tiles, mb(fieldBytes)), rgb(0x69db7c))

	// --- a size that slides ------------------------------------------
	// Two columns like the rest, so the same word at the same moment can
	// be read off against itself.
	const animY = 400
	panel(g, base, 24, animY, colW, 190,
		"A SIZE THAT SLIDES — the raster line steps: every size it visits is another sheet")
	panel(g, base, 48+colW, animY, colW, 190,
		"A SIZE THAT SLIDES — the field glides; it is the same tiles, multiplied")
	size := 14 + 30*(1-math.Cos(d.t*0.9))/2
	// The raster line rounds to two pixels. That is not a handicap
	// invented for the demo: a sheet is a megabyte of RGBA and another on
	// the GPU, and a sheet per fractional size runs the card out of
	// memory in under a minute. The stepping is what the limitation looks
	// like; the field line beside it is the same animation unrounded.
	rsize := 2 * math.Round(size/2)
	field(false)
	g.DrawText(base.AtSize(rsize), 40, animY+46+float32(44-rsize)*0.6, "Hamburger", accent)
	g.DrawText(base.AtSize(12), 40, animY+160, fmt.Sprintf("drawn at %.0f px", rsize), dim)
	field(true)
	// DrawTextSize, not AtSize: the field draws any size from the one
	// font, so an animated size costs nothing. AtSize here would build a
	// Font, a sheet and a rasterisation every frame, and keep every one
	// of them — which is what made an earlier version of this example
	// climb towards a whole core the longer it ran.
	g.DrawTextSize(base, 64+colW, animY+46+float32(44-size)*0.6, "Hamburger", warm, float32(size))
	field(false)
	g.DrawText(base.AtSize(12), 64+colW, animY+160, fmt.Sprintf("drawn at %.2f px", size), dim)

	// --- a line that turns -------------------------------------------
	const spinY = 604
	panel(g, base, 24, spinY, colW, 190, "ROTATED — the mask was made for the upright grid, and is sampled off it")
	panel(g, base, 48+colW, spinY, colW, 190, "ROTATED — the field is read at the angle, from the same tiles")
	ang := float32(d.spin * 0.6)
	for i, on := range []bool{false, true} {
		x := 24 + float32(i)*(colW+24)
		cx, cy := x+colW/2, float32(spinY+104)
		g.PushRotate(ang, cx, cy)
		field(on)
		g.DrawText(base.AtSize(26), cx-82, cy-17, "turning text", []render.RGBA{accent, warm}[i])
		g.PopTransform()
	}

	// --- what a field makes cheap ------------------------------------
	const fxY = 808
	panel(g, base, 24, fxY, colW, 78,
		"WEIGHT · OUTLINE · GLOW — the atlas has no parameters: the same glyphs, five times")
	panel(g, base, 48+colW, fxY, colW, 78,
		"WEIGHT · OUTLINE · GLOW — a threshold moved, a band between two, a softer ramp")
	words := []string{"plain", "bold", "thin", "outlined", "glow"}
	at := []float32{0, 72, 140, 205, 330}
	fx := base.AtSize(22)

	// The raster row first, so the comparison is on the screen rather
	// than in the caption. Every one of these would be a second set of
	// glyphs — rasterised at a different stem weight, or dilated by hand
	// — and the renderer has none of that, so all five come out the same.
	field(false)
	for i, word := range words {
		g.DrawText(fx, 40+at[i], fxY+34, word, fg)
	}

	field(true)
	x0 := 64 + colW
	// Each setting is renderer state, but a frame can hold several: the
	// batch that drew under one keeps its own copy.
	g.SetTextWeight(0)
	g.DrawText(fx, x0+at[0], fxY+34, words[0], fg)
	g.SetTextWeight(0.035)
	g.DrawText(fx, x0+at[1], fxY+34, words[1], fg)
	g.SetTextWeight(-0.02)
	g.DrawText(fx, x0+at[2], fxY+34, words[2], fg)
	g.SetTextWeight(0)
	g.SetTextOutline(0.05, hot)
	g.DrawText(fx, x0+at[3], fxY+34, words[3], fg)
	g.SetTextOutline(0, render.RGBA{})
	g.SetTextSoftness(0.22)
	g.DrawText(fx, x0+at[4], fxY+34, words[4], warm)
	g.SetTextSoftness(0)

	field(false)

	// Paused, nothing on this page moves — so say so, and the shell
	// sleeps on the event queue instead of drawing it again. The keys
	// still wake it: input overrides the hint.
	if d.paused {
		fr.Unchanged()
	}
	return true
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	still := flag.Bool("still", false, "start with the animation paused")
	fps := flag.Int("fps", 60, "frame rate cap (0 = the display's own)")
	flag.Parse()

	d := &demo{t: 1.7, spin: 0.35, paused: *still}
	// 60 is plenty for reading text. Without a cap the window redraws at
	// the monitor's refresh rate — 190-odd frames a second on a 240 Hz
	// screen — and each frame costs about a third of a millisecond of
	// CPU inside wgpu's submit and present whether anything changed or
	// not. That is the whole of this example's CPU: 9% of a core
	// uncapped, 3% at 60.
	opts := shell.Options{Title: "graphics: distance-field text", Width: W, Height: H,
		MaxFrames: *frames, MaxFPS: *fps}
	var err error
	if *shot != "" {
		err = shell.ScreenshotGame(opts, 240, 1.0/60, d.update, nil, *shot)
	} else {
		err = shell.RunGame(opts, d.update)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "msdftext:", err)
		os.Exit(1)
	}
}
