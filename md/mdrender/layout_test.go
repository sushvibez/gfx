package mdrender

import (
	"strings"
	"testing"

	"github.com/sushvibez/gfx/md"
)

// charMetrics measures every character as half the face's size wide and a
// line as the size tall: layout without fonts.
type charMetrics struct{}

func (charMetrics) Width(f Face, s string) float32 { return float32(len([]rune(s))) * f.Size * 0.5 }
func (charMetrics) LineHeight(f Face) float32      { return f.Size }

var cm charMetrics

func lay(src string, w float32) *Page {
	return Layout(md.Parse(src), w, DarkStyle(10), charMetrics{})
}

func texts(p *Page) []Op {
	var out []Op
	for _, o := range p.Ops {
		if o.Kind == OpText {
			out = append(out, o)
		}
	}
	return out
}

// A paragraph fills lines to the width and no further, and breaks between
// words.
func TestParagraphWraps(t *testing.T) {
	p := lay("one two three four five six seven eight nine ten", 100) // 20 characters a line
	var lines []float32
	for _, o := range texts(p) {
		if o.X+o.W > 100 {
			t.Errorf("%q runs past the width", o.Text)
		}
		if strings.Contains(o.Text, " ") {
			t.Errorf("word %q has a space in it", o.Text)
		}
		if len(lines) == 0 || lines[len(lines)-1] != o.Y {
			lines = append(lines, o.Y)
		}
	}
	for _, o := range texts(p) {
		if o.X+cm.Width(o.Face, o.Text) > 100.01 {
			t.Errorf("%q at %.0f runs past 100", o.Text, o.X)
		}
	}
	if len(lines) != 3 {
		t.Errorf("%d lines, want 3 (49 characters at 20 a line)", len(lines))
	}
	if p.H != 3*12.5 {
		t.Errorf("height %.1f, want three lines of 12.5", p.H)
	}
}

// A word wider than the column is broken where it must be.
func TestLongWordBreaks(t *testing.T) {
	p := lay(strings.Repeat("x", 50), 100)
	if n := len(texts(p)); n != 3 {
		t.Errorf("a 50-character word in 20-character lines made %d pieces, want 3", n)
	}
}

// Links know where they are.
func TestLinkBoxes(t *testing.T) {
	p := lay("go to [the docs](https://example.com) now", 400)
	if len(p.Links) != 3 {
		t.Fatalf("%d link boxes, want 3: both words and the space between, so the underline is unbroken", len(p.Links))
	}
	l := p.Links[0]
	if dest, ok := p.LinkAt(l.X+1, l.Y+1); !ok || dest != "https://example.com" {
		t.Errorf("LinkAt on the link: %q %v", dest, ok)
	}
	if _, ok := p.LinkAt(0, 0); ok {
		t.Error("LinkAt found a link over plain text")
	}
}

// Headings are larger than body text, and emphasis picks its face.
func TestFaces(t *testing.T) {
	p := lay("# Title\n\nplain *it* **bold** `code`", 400)
	ts := texts(p)
	want := map[string]Face{
		"Title": {Bold: true, Size: 20},
		"plain": {Size: 10},
		"it":    {Italic: true, Size: 10},
		"bold":  {Bold: true, Size: 10},
		"code":  {Mono: true, Size: 9},
	}
	for _, o := range ts {
		if f, ok := want[o.Text]; ok && o.Face != f {
			t.Errorf("%q in %+v, want %+v", o.Text, o.Face, f)
		}
	}
}

// A table fits its width, and every cell's text sits inside its column.
func TestTableFits(t *testing.T) {
	p := lay("| name | description |\n| --- | --- |\n| a | "+strings.Repeat("long ", 30)+"|", 200)
	for _, o := range p.Ops {
		if o.X+o.W > 200.5 {
			t.Errorf("%v op at %.0f+%.0f past the 200 width", o.Kind, o.X, o.W)
		}
	}
	for _, o := range texts(p) {
		if o.X+cm.Width(o.Face, o.Text) > 200.5 {
			t.Errorf("cell text %q runs past the table", o.Text)
		}
	}
}

// A done task gets a filled box; an open one an outline.
func TestTaskBoxes(t *testing.T) {
	st := DarkStyle(10)
	p := lay("- [x] done\n- [ ] open", 200)
	var filled, framed bool
	for _, o := range p.Ops {
		if o.Kind == OpRect && o.Color == st.Check {
			filled = true
		}
		if o.Kind == OpFrame && o.Color == st.Muted {
			framed = true
		}
	}
	if !filled || !framed {
		t.Errorf("done box %v, open box %v", filled, framed)
	}
}

// Code blocks keep their lines and wrap the long ones by character.
func TestCodeBlock(t *testing.T) {
	p := lay("```\nshort\n"+strings.Repeat("y", 60)+"\n```", 200)
	var ys []float32
	for _, o := range texts(p) {
		if !o.Face.Mono {
			t.Errorf("code text %q not in the mono face", o.Text)
		}
		ys = append(ys, o.Y)
	}
	if len(ys) < 3 {
		t.Errorf("%d code lines, want the short one and the long one wrapped", len(ys))
	}
}
