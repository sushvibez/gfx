package mdrender

import (
	"strings"
	"unicode/utf8"
)

// Selection on a laid-out page. A position is a place in one of the page's
// text runs; positions are ordered by the order the runs were laid out in,
// which is the document's reading order. The page answers three things:
// where a point is in the text, what text lies between two positions, and
// which rectangles to light up to show it.

// Pos is a place in the page's text: an index into Ops (always an OpText)
// and a byte offset into its text.
type Pos struct{ Op, Off int }

// Before reports whether p comes before q in reading order.
func (p Pos) Before(q Pos) bool { return p.Op < q.Op || p.Op == q.Op && p.Off < q.Off }

// Rect is an area of the page.
type Rect struct{ X, Y, W, H float32 }

// Start and End are the first and last positions of the page's text, for
// selecting all of it; ok is false for a page with no text.
func (p *Page) Start() (Pos, bool) {
	for i, o := range p.Ops {
		if o.Kind == OpText {
			return Pos{Op: i}, true
		}
	}
	return Pos{}, false
}

func (p *Page) End() (Pos, bool) {
	for i := len(p.Ops) - 1; i >= 0; i-- {
		if p.Ops[i].Kind == OpText {
			return Pos{Op: i, Off: len(p.Ops[i].Text)}, true
		}
	}
	return Pos{}, false
}

// lineH is a text run's height.
func (p *Page) lineH(o Op) float32 {
	if p.m != nil {
		return p.m.LineHeight(o.Face)
	}
	return o.Face.Size * 1.2
}

// Hit is the position nearest a point on the page: in the line the point
// is on (or the nearest line to it), at the character boundary nearest
// its x. ok is false for a page with no text.
func (p *Page) Hit(x, y float32) (Pos, bool) {
	best, bestDY, bestDX := -1, float32(1e30), float32(1e30)
	for i, o := range p.Ops {
		if o.Kind != OpText {
			continue
		}
		h := p.lineH(o)
		var dy float32
		switch {
		case y < o.Y:
			dy = o.Y - y
		case y > o.Y+h:
			dy = y - (o.Y + h)
		}
		var dx float32
		switch {
		case x < o.X:
			dx = o.X - x
		case x > o.X+o.W:
			dx = x - (o.X + o.W)
		}
		// The line first, then the run nearest along it.
		if dy < bestDY-0.5 || (dy <= bestDY+0.5 && dx < bestDX) {
			best, bestDY, bestDX = i, dy, dx
		}
	}
	if best < 0 {
		return Pos{}, false
	}
	o := p.Ops[best]
	return Pos{Op: best, Off: p.offsetAt(o, x-o.X)}, true
}

// offsetAt is the character boundary in a run nearest dx from its start.
func (p *Page) offsetAt(o Op, dx float32) int {
	if dx <= 0 || p.m == nil {
		return 0
	}
	prev := float32(0)
	for i := range o.Text {
		if i == 0 {
			continue
		}
		w := p.m.Width(o.Face, o.Text[:i])
		if w >= dx {
			if dx-prev < w-dx {
				_, n := utf8.DecodeLastRuneInString(o.Text[:i])
				return i - n
			}
			return i
		}
		prev = w
	}
	if w := p.m.Width(o.Face, o.Text); dx-prev < w-dx {
		_, n := utf8.DecodeLastRuneInString(o.Text)
		return len(o.Text) - n
	}
	return len(o.Text)
}

// order puts two positions in reading order.
func order(a, b Pos) (Pos, Pos) {
	if b.Before(a) {
		return b, a
	}
	return a, b
}

// Text is the text between two positions: runs on one line joined by a
// space where there was one, lines by a newline, blocks by a blank line.
func (p *Page) Text(a, b Pos) string {
	a, b = order(a, b)
	var sb strings.Builder
	prev := -1
	for i := a.Op; i <= b.Op && i < len(p.Ops); i++ {
		o := p.Ops[i]
		if o.Kind != OpText {
			continue
		}
		from, to := 0, len(o.Text)
		if i == a.Op {
			from = min(a.Off, to)
		}
		if i == b.Op {
			to = min(b.Off, to)
		}
		if prev >= 0 {
			q := p.Ops[prev]
			h := p.lineH(q)
			switch {
			case o.Y > q.Y+h*1.6 || o.Y < q.Y-h*0.5:
				sb.WriteString("\n\n") // a gap: another block
			case o.Y > q.Y+h*0.5:
				sb.WriteString("\n")
			case o.X > q.X+q.W+0.5:
				sb.WriteString(" ")
			}
		}
		sb.WriteString(o.Text[from:to])
		prev = i
	}
	return sb.String()
}

// Rects are the areas to highlight to show the text between two
// positions: one a run, stretched over the space to the next run on the
// same line so a selected line reads as one band.
func (p *Page) Rects(a, b Pos) []Rect {
	a, b = order(a, b)
	var out []Rect
	for i := a.Op; i <= b.Op && i < len(p.Ops); i++ {
		o := p.Ops[i]
		if o.Kind != OpText {
			continue
		}
		x0, x1 := o.X, o.X+o.W
		if i == a.Op && p.m != nil {
			x0 = o.X + p.m.Width(o.Face, o.Text[:min(a.Off, len(o.Text))])
		}
		if i == b.Op && p.m != nil {
			x1 = o.X + p.m.Width(o.Face, o.Text[:min(b.Off, len(o.Text))])
		} else if n := p.nextText(i); n >= 0 && n <= b.Op {
			if q := p.Ops[n]; q.Y == o.Y && q.X > x1 {
				x1 = q.X // over the space between
			}
		}
		if x1 > x0 {
			out = append(out, Rect{X: x0, Y: o.Y, W: x1 - x0, H: p.lineH(o)})
		}
	}
	return out
}

func (p *Page) nextText(i int) int {
	for j := i + 1; j < len(p.Ops); j++ {
		if p.Ops[j].Kind == OpText {
			return j
		}
	}
	return -1
}
