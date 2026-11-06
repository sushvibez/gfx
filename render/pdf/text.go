package pdf

// Text: the text matrix, the pen, and drawing a string. PDF positions
// every glyph itself — the string's bytes are codes, each code has a width
// from the font dictionary, and the pen advances by that width scaled by
// the font size. So text is drawn a glyph at a time at computed positions
// rather than handed to the renderer as a run, which is also what keeps
// justified and kerned lines landing where the document says.

import "github.com/sushvibez/gfx/render"

// textState is the text object state (Tm and Tlm in the spec).
type textState struct {
	tm   render.Affine2 // text matrix
	tlm  render.Affine2 // line matrix
	open bool
}

func (t *textState) begin() {
	t.tm, t.tlm, t.open = render.Identity2(), render.Identity2(), true
}

func (t *textState) end() { t.open = false }

func (t *textState) setMatrix(m render.Affine2) { t.tm, t.tlm = m, m }

// nextLine moves to a new line offset (tx, ty) from the current line start.
func (t *textState) nextLine(tx, ty float32) {
	t.tlm = t.tlm.Mul(render.Translate2(tx, ty))
	t.tm = t.tlm
}

// advance moves the pen along the text matrix by tx (already scaled).
func (t *textState) advance(tx, ty float32) {
	t.tm = t.tm.Mul(render.Translate2(tx, ty))
}

// showText draws one string and advances the pen.
func (rd *Renderer) showText(s String, st *gstate, t *textState) {
	if st.font == nil || st.fontSize == 0 {
		// A string with no font selected still has to move the pen, or the
		// rest of the line piles up at one point.
		return
	}
	if st.render == 3 || st.render == 7 { // invisible (the OCR text layer)
		rd.advanceString(s, st, t)
		return
	}
	col := st.fill
	if st.render == 1 || st.render == 5 { // stroke-only text
		col = st.stroke
	}
	if col.A <= 0 {
		rd.advanceString(s, st, t)
		return
	}
	for _, code := range st.font.codes(s) {
		// The glyph's device transform: text space scaled by the font size
		// and horizontal scale, through the text and current matrices.
		trm := st.ctm.Mul(t.tm).Mul(render.Affine2{
			A: st.fontSize * st.hscale, D: st.fontSize, F: st.rise,
		})
		w0 := st.font.widthOf(code) / 1000
		if r := st.font.runeOf(code); r != 0 && r != ' ' {
			rd.drawGlyph(r, trm, col, st)
		}
		// The pen advance, in text space: the glyph width plus the
		// character spacing, plus word spacing for a single-byte code 32.
		adv := w0*st.fontSize + st.charSp
		if code == 32 && !st.font.twoByte {
			adv += st.wordSp
		}
		t.advance(adv*st.hscale, 0)
	}
}

// advanceString moves the pen across a string without drawing it.
func (rd *Renderer) advanceString(s String, st *gstate, t *textState) {
	for _, code := range st.font.codes(s) {
		adv := st.font.widthOf(code)/1000*st.fontSize + st.charSp
		if code == 32 && !st.font.twoByte {
			adv += st.wordSp
		}
		t.advance(adv*st.hscale, 0)
	}
}

// drawGlyph draws one character with the glyph transform trm, whose origin
// is the glyph's own origin (its baseline at x=0).
func (rd *Renderer) drawGlyph(r rune, trm render.Affine2, col render.RGBA, st *gstate) {
	// The device size of an em, from the transform's vertical scale.
	sx, sy, _ := decompose(trm)
	if sy < 0.6 || sy > 4000 { // too small to see, or a broken matrix
		return
	}
	face := rd.faceFor(st.font, sy, r)
	if face == nil {
		return
	}
	x, y := trm.E, trm.F
	// The renderer places text by its top-left corner; PDF places it on the
	// baseline.
	y -= face.Ascent()
	// A rotated or mirrored text matrix needs the renderer's transform;
	// upright text (the overwhelming majority) draws directly, which keeps
	// the glyph atlas pixel-aligned.
	if isUpright(trm) {
		rd.clippedBy(*st, func() { rd.r.DrawText(face, x, y, string(r), col) })
		return
	}
	rot := render.Affine2{A: trm.A / sx, B: trm.B / sx, C: trm.C / sy, D: trm.D / sy, E: trm.E, F: trm.F}
	rd.clippedBy(*st, func() {
		rd.r.PushTransform(rot)
		rd.r.DrawText(face, 0, -face.Ascent(), string(r), col)
		rd.r.PopTransform()
	})
}

// isUpright reports a transform that only scales and translates — no
// rotation, no skew, and the y axis pointing the way the renderer's does.
func isUpright(m render.Affine2) bool {
	return absf(m.B) < 1e-4 && absf(m.C) < 1e-4 && m.A > 0 && m.D < 0
}
