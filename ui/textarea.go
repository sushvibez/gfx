package ui

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/render"
)

// TextArea: a multi-line text field — QML's TextArea and TextEdit.
//
// It is an InputEl in area mode over an InputState in multi-line mode, so
// focus, Tab order, the caret, selection by keyboard and by drag, the
// clipboard, validators and length caps are the single-line field's own,
// and everything that routes events to a field (ui.go) routes them here
// unchanged. What the area adds is the layout: the text wrapped to the
// box at word boundaries and at its own line breaks, Enter for a new
// line (Ctrl+Enter submits), Up/Down and Home/End by wrapped line, a box
// that scrolls to keep the caret's line in view, and clicks that land on
// the line under the pointer.

// TextLine is one wrapped line: the runes [Start, End) of the text, and
// where each rune boundary in it falls from the line's left edge (len
// End-Start+1).
type TextLine struct {
	Start, End int
	Xs         []float32
}

// WrapLines lays runes out in lines no wider than w, measured by m: at the
// spaces where it can, mid-word where one word is wider than the line, and
// at every line break in the text. A text ending in a line break has an
// empty last line, for the caret to stand on.
func WrapLines(rs []rune, w float32, m func(string) float32) []TextLine {
	line := func(a, b int) TextLine {
		ln := TextLine{Start: a, End: b, Xs: make([]float32, b-a+1)}
		for k := a; k <= b; k++ {
			ln.Xs[k-a] = m(string(rs[a:k]))
		}
		return ln
	}
	var out []TextLine
	start := 0
	for {
		end, lastSpace := start, -1
		for end < len(rs) && rs[end] != '\n' {
			if end > start && m(string(rs[start:end+1])) > w {
				break
			}
			if rs[end] == ' ' {
				lastSpace = end
			}
			end++
		}
		next := end
		if end < len(rs) {
			switch {
			case rs[end] == '\n':
				next = end + 1
			case lastSpace >= start:
				end, next = lastSpace, lastSpace+1
			}
		}
		out = append(out, line(start, end))
		if end >= len(rs) {
			return out
		}
		if next >= len(rs) {
			return append(out, line(len(rs), len(rs)))
		}
		start = next
	}
}

// LineAt is the line a caret position is on. At a wrap point the caret
// belongs to the start of the next line.
func LineAt(lines []TextLine, caret int) int {
	for i, ln := range lines {
		if caret >= ln.Start && caret <= ln.End {
			if caret == ln.End && i+1 < len(lines) && lines[i+1].Start == caret {
				continue
			}
			return i
		}
	}
	return len(lines) - 1
}

// MoveLine is where Up (dir -1) or Down (+1) takes a caret: the same
// distance across, on the line above or below; the start or the end of
// the text past the first or last line.
func MoveLine(lines []TextLine, caret, dir int) int {
	if len(lines) == 0 {
		return caret
	}
	i := LineAt(lines, caret)
	j := i + dir
	if j < 0 {
		return 0
	}
	if j >= len(lines) {
		return lines[len(lines)-1].End
	}
	x := lines[i].Xs[min(max(caret-lines[i].Start, 0), len(lines[i].Xs)-1)]
	best, bestD := lines[j].Start, float32(-1)
	for k, kx := range lines[j].Xs {
		if d := absF(kx - x); bestD < 0 || d < bestD {
			best, bestD = lines[j].Start+k, d
		}
	}
	return best
}

// Multiline makes the state a text area's: Enter inserts a line break
// (Ctrl+Enter submits), pasted text keeps its line breaks, and Up/Down
// and Home/End move by the lines the area last laid out.
func (s *InputState) Multiline() *InputState { s.multiline = true; return s }

// IsMultiline reports whether the state is a text area's.
func (s *InputState) IsMultiline() bool { return s.multiline }

// multiKey handles the keys a text area treats differently from a field,
// and reports whether it did.
func (s *InputState) multiKey(ev app.KeyEvent) bool {
	if !s.multiline || !ev.Down {
		return false
	}
	runes := []rune(s.Text.Get())
	s.caret = min(max(s.caret, 0), len(runes))
	s.anchor = min(max(s.anchor, 0), len(runes))
	switch {
	case ev.Key == app.KeyEnter && ev.Ctrl:
		if s.submit != nil {
			s.submit(s.Text.Get())
		}
		return true
	case ev.Key == app.KeyEnter:
		runes, _ = s.deleteSelection(runes)
		typed := append(runes[:s.caret:s.caret], append([]rune{'\n'}, runes[s.caret:]...)...)
		s.commit(string(typed), s.caret+1)
		return true
	case (ev.Key == app.KeyUp || ev.Key == app.KeyDown) && s.lines != nil:
		dir := 1
		if ev.Key == app.KeyUp {
			dir = -1
		}
		s.moveCaret(MoveLine(s.lines, s.caret, dir), ev.Shift)
		return true
	case (ev.Key == app.KeyHome || ev.Key == app.KeyEnd) && s.lines != nil && !ev.Ctrl:
		ln := s.lines[LineAt(s.lines, s.caret)]
		to := ln.Start
		if ev.Key == app.KeyEnd {
			to = ln.End
		}
		s.moveCaret(to, ev.Shift)
		return true
	case ev.Ctrl && ev.Rune == 'v':
		runes, _ = s.deleteSelection(runes)
		ins := []rune(clipboardGet())
		pasted := append(runes[:s.caret:s.caret], append(ins, runes[s.caret:]...)...)
		s.commit(string(pasted), s.caret+len(ins))
		return true
	}
	return false
}

// TextArea renders a multi-line text field bound to state, w wide and h
// tall (QML TextArea). The state is made multi-line. Same skin and setters
// as Input.
func TextArea(state *InputState, w, h float32) *InputEl {
	state.Multiline()
	e := Input(state, w)
	e.area, e.areaH = true, h
	return e
}

// areaWrapW is the width text wraps to inside the box.
func (e *InputEl) areaWrapW() float32 { return max(e.rect.W-2*inputPadX, 10) }

// areaLayout wraps the shown text to the box, and remembers it on the
// state so Up/Down/Home/End move by these lines.
func (e *InputEl) areaLayout(ctx *Context, font *render.Font) []TextLine {
	lines := WrapLines([]rune(e.shown()), e.areaWrapW(), func(s string) float32 {
		w, _ := ctx.R.MeasureText(font, s)
		return w
	})
	e.state.lines = lines
	return lines
}

// areaFirst is the first line drawn. When the area has focus and the caret
// has moved since the last paint (typing, arrows, a click) it scrolls to keep the
// caret's line in the box; otherwise it stays where the wheel or the
// scrollbar left it.
func (e *InputEl) areaFirst(lines []TextLine, lineH float32) int {
	a := &e.state.area
	shown := max(int((e.rect.H-2*inputPadY)/lineH), 1)
	a.shown, a.count, a.lineH = shown, len(lines), lineH
	if caret := e.state.caret; e.state.focused && caret != a.caretSeen {
		a.caretSeen = caret
		cl := LineAt(lines, caret)
		switch {
		case cl < a.top:
			a.top = cl
		case cl >= a.top+shown:
			a.top = cl - shown + 1
		}
	}
	a.top = min(max(a.top, 0), a.maxTop())
	return a.top
}

// maxTop is the last first line: the text's end at the bottom of the box.
func (a *areaScroll) maxTop() int { return max(a.count-a.shown, 0) }

// A text area is a scroll target: the wheel moves it by lines, and its
// scrollbar thumb drags.
func (e *InputEl) contains(x, y float32) bool { return e.rect.contains(x, y) }
func (e *InputEl) viewRect() Rect             { return e.rect }

func (e *InputEl) scroll(dy float32) bool {
	a := &e.state.area
	if !e.area || a.lineH <= 0 || a.maxTop() == 0 {
		return false
	}
	a.wheel += dy / a.lineH
	n := int(a.wheel)
	if n == 0 {
		return true // a part-line step: held, and the wheel is consumed
	}
	a.wheel -= float32(n)
	top := min(max(a.top+n, 0), a.maxTop())
	if top == a.top {
		a.wheel = 0
		return false // at the end: let the page scroll instead
	}
	a.top = top
	return true
}

// thumbRect is the scrollbar thumb, zero-wide when everything fits.
func (e *InputEl) thumbRect() Rect {
	a := &e.state.area
	if !e.area || a.maxTop() == 0 {
		return Rect{}
	}
	h := max(24, e.rect.H*float32(a.shown)/float32(a.count))
	y := e.rect.Y + (e.rect.H-h)*float32(a.top)/float32(a.maxTop())
	return Rect{e.rect.X + e.rect.W - 7, y, 7, h}
}

// scrollToY drags the thumb's centre to pixel y.
func (e *InputEl) scrollToY(y float32) {
	a := &e.state.area
	th := e.thumbRect()
	if th.W == 0 {
		return
	}
	span := e.rect.H - th.H
	if span <= 0 {
		return
	}
	f := min(max((y-th.H/2-e.rect.Y)/span, 0), 1)
	a.top = int(f*float32(a.maxTop()) + 0.5)
}

// paintArea draws the area: box, wrapped text, selection and caret.
func (e *InputEl) paintArea(ctx *Context, font *render.Font, focused bool) {
	lines := e.areaLayout(ctx, font)
	lineH := font.Height()
	first := e.areaFirst(lines, lineH)
	runes := []rune(e.shown())
	tx, ty := e.rect.X+inputPadX, e.rect.Y+inputPadY
	ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
	defer ctx.R.PopClip()
	if e.state.Text.Get() == "" && e.hasPlaceholdr {
		ctx.R.DrawText(font, tx, ty, e.placeholder, e.placeholderC)
	}
	lo, hi := e.state.selRange()
	cl := LineAt(lines, e.state.caret)
	for k := first; k < len(lines); k++ {
		y := ty + float32(k-first)*lineH
		if y > e.rect.Y+e.rect.H {
			break
		}
		ln := lines[k]
		if focused && e.state.hasSelection() {
			a, b := max(lo, ln.Start), min(hi, ln.End)
			if a < b {
				ctx.R.FillRect(tx+ln.Xs[a-ln.Start], y, ln.Xs[b-ln.Start]-ln.Xs[a-ln.Start], lineH, e.selC)
			}
		}
		ctx.R.DrawText(font, tx, y, string(runes[ln.Start:ln.End]), e.textC)
		if focused && k == cl {
			cx := ln.Xs[min(max(e.state.caret-ln.Start, 0), len(ln.Xs)-1)]
			ctx.R.FillRect(tx+cx, y, 1.5, lineH, e.textC)
		}
	}
	// The scrollbar, when the text is taller than the box: the same thin
	// bar a ScrollEl draws, in the text colour.
	if th := e.thumbRect(); th.W > 0 {
		bc := e.textC
		bc.A *= 0.3
		ctx.R.FillRoundRect(th.X+2, th.Y, 3, th.H, 1.5, bc)
	}
}

// caretAt maps a point to the nearest caret position: in a text area the
// line under it first, then the place along that line.
func (e *InputEl) caretAt(ctx *Context, x, y float32) int {
	if !e.area {
		return e.caretAtX(ctx, x)
	}
	font := e.font(ctx)
	if font == nil {
		return 0
	}
	lines := e.state.lines
	if lines == nil {
		lines = e.areaLayout(ctx, font)
	}
	k := e.state.area.top + int((y-e.rect.Y-inputPadY)/font.Height())
	k = min(max(k, 0), len(lines)-1)
	ln := lines[k]
	rel := x - e.rect.X - inputPadX
	for i := 1; i < len(ln.Xs); i++ {
		if rel < (ln.Xs[i-1]+ln.Xs[i])/2 {
			return ln.Start + i - 1
		}
	}
	return ln.End
}
