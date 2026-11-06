package render

// Shaping a run — deciding which glyph comes from which face and where
// the pen stands before each — is the same work every frame for text
// that has not changed, and a retained-mode page redraws the same
// strings sixty times a second.
//
// Per glyph it costs a UTF-8 decode, a zero-width test, an emoji-range
// test, a fallback-coverage lookup, a kern-pair lookup and an advance
// lookup, all before anything is drawn. None of it depends on where the
// run lands, so it is cached on (face, string) and reused until a frame
// goes by without asking for it.
//
// What is NOT cached is the atlas entry or the quad. A glyph's position
// is rounded to the device pixel grid, which is not linear in the run's
// origin, and the subpixel path picks a different rasterisation
// depending on the pen's fractional phase. Both have to happen at draw
// time; they are a map lookup and some arithmetic, against the walk
// above.

// shapedGlyph is one glyph of a run: the rune, the faces that carry it,
// and the pen offset from the run's origin, in destination pixels.
type shapedGlyph struct {
	ch rune
	// font is the metrics face the fallback chain resolved to, raster
	// the matching face on the device-pixel grid (subpixel text only).
	font   *Font
	raster *Font
	pen    float32
	emoji  bool
}

// shapedRun is a cached run plus the frame it was last drawn on.
type shapedRun struct {
	glyphs []shapedGlyph
	width  float32
	used   uint32
}

// shapeKey identifies a run. The three text paths measure with
// different faces — the grayscale path lays out on the raster face and
// divides, the others on the logical one — so the faces and the
// multipliers are part of the key rather than assumed.
type shapeKey struct {
	logical, metrics, raster *Font
	s                        string
	textMul, emojiMul        float32
}

// shapeSweep is how many frames a run survives unused. A sweep walks
// the whole map, so it is done rarely rather than every frame; the
// point is only that a page that stops drawing a string eventually
// stops paying to remember it.
const shapeSweep = 240

// shape returns the run's shaping, from the cache when it is there.
//
// logical is the face the caller asked for (emoji come from it),
// metrics the face advances and kerning are taken from, and raster the
// device-grid face the subpixel path rasterises from (nil elsewhere).
// The multipliers convert each face's pixels into the destination's.
// Neither the tracking nor the distance-field path's size multiplier is
// in the key, and that is not a saving but a correctness matter: a size
// that slides would otherwise make a fresh entry every frame and the
// cache would grow without bound — the same trap DrawTextSize exists to
// avoid. The pen offsets are stored unscaled and unspaced, and the
// caller applies both: glyph i carries i lots of tracking, because
// every glyph before it advanced once.
func (r *Renderer2D) shape(logical, metrics, raster *Font, s string, textMul, emojiMul float32) *shapedRun {
	key := shapeKey{logical, metrics, raster, s, textMul, emojiMul}
	if run, ok := r.shapes[key]; ok {
		run.used = r.frame
		return run
	}
	run := &shapedRun{used: r.frame}
	pen := float32(0)
	prev := rune(-1)
	for _, ch := range s {
		if zeroWidth(ch) {
			continue
		}
		if logical.emojiPick(ch) {
			if g, ok := logical.ensureEmoji(ch); ok {
				run.glyphs = append(run.glyphs, shapedGlyph{ch: ch, font: logical, pen: pen, emoji: true})
				pen += g.advance * emojiMul
			}
			prev = -1
			continue
		}
		mf := metrics.pick(ch)
		if prev >= 0 {
			pen += mf.kern(prev, ch) * textMul
		}
		g := shapedGlyph{ch: ch, font: mf, pen: pen}
		if raster != nil {
			g.raster = raster.pick(ch)
		}
		run.glyphs = append(run.glyphs, g)
		if adv, ok := mf.advanceOf(ch); ok {
			pen += adv * textMul
		}
		prev = ch
	}
	run.width = pen
	if r.shapes == nil {
		r.shapes = map[shapeKey]*shapedRun{}
	}
	r.shapes[key] = run
	return run
}

// sweepShapes drops runs no frame has asked for in a while. Called from
// Begin, and only every shapeSweep frames: the walk is over every run
// the page has ever drawn, which is the wrong thing to do sixty times a
// second to save a few kilobytes.
func (r *Renderer2D) sweepShapes() {
	r.frame++
	if r.frame%shapeSweep != 0 || len(r.shapes) == 0 {
		return
	}
	for k, run := range r.shapes {
		if r.frame-run.used >= shapeSweep {
			delete(r.shapes, k)
		}
	}
}
