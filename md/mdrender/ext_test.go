package mdrender

import (
	"strings"
	"testing"

	"github.com/sushvibez/gfx/md"
)

func textOf(p *Page) string {
	var b []string
	for _, o := range texts(p) {
		b = append(b, o.Text)
	}
	return strings.Join(b, "|")
}

// Headings and footnotes are anchors; a footnote's number is a raised
// link to its note, and the note links back.
func TestAnchorsAndFootnotes(t *testing.T) {
	p := lay("# Intro\n\nSee[^n].\n\n## Next\n\n[^n]: The note.", 400)
	for _, a := range []string{"intro", "next", "fn-n", "fnref-n"} {
		if _, ok := p.Anchor("#" + a); !ok {
			t.Errorf("no anchor %q in %v", a, p.Anchors)
		}
	}
	if yi, _ := p.Anchor("#intro"); yi != 0 {
		t.Errorf("intro at %v", yi)
	}
	yn, _ := p.Anchor("#fn-n")
	yr, _ := p.Anchor("#fnref-n")
	if yn <= yr {
		t.Errorf("the note (%v) is not below its reference (%v)", yn, yr)
	}
	var ref, back bool
	for _, l := range p.Links {
		ref = ref || l.Dest == "#fn-n"
		back = back || l.Dest == "#fnref-n"
	}
	if !ref || !back {
		t.Errorf("links %+v: want the reference and the way back", p.Links)
	}
	// The number is smaller than the text and sits above its baseline.
	var see, num Op
	for _, o := range texts(p) {
		switch o.Text {
		case "See":
			see = o
		case "1":
			num = o
		}
	}
	if num.Face.Size >= see.Face.Size || num.Y >= see.Y {
		t.Errorf("footnote number %+v not raised and smaller than %+v", num, see)
	}
}

func TestDefinitionList(t *testing.T) {
	p := lay("Apple\n: A fruit.", 400)
	var term, def Op
	for _, o := range texts(p) {
		switch o.Text {
		case "Apple":
			term = o
		case "A":
			def = o
		}
	}
	if !term.Face.Bold || def.X <= term.X || def.Y <= term.Y {
		t.Errorf("term %+v, definition %+v", term, def)
	}
}

// Inline HTML is drawn as what it means; unknown tags are not drawn.
func TestInlineHTML(t *testing.T) {
	p := lay("a <b>bold</b> <kbd>Ctrl</kbd> H<sub>2</sub>O <blink>x</blink><br>next <a href=\"u\">link</a>", 400)
	got := map[string]Op{}
	for _, o := range texts(p) {
		got[o.Text] = o
		if strings.Contains(o.Text, "<") {
			t.Errorf("a tag was drawn: %q", o.Text)
		}
	}
	if !got["bold"].Face.Bold || got["a"].Face.Bold {
		t.Errorf("bold: %+v", got["bold"])
	}
	if !got["Ctrl"].Face.Mono {
		t.Errorf("kbd: %+v", got["Ctrl"])
	}
	if got["2"].Face.Size >= got["H"].Face.Size || got["2"].Y <= got["H"].Y {
		t.Errorf("subscript %+v against %+v", got["2"], got["H"])
	}
	if got["next"].Y <= got["a"].Y {
		t.Error("<br> did not break the line")
	}
	if _, ok := got["x"]; !ok {
		t.Error("an unknown tag's text was dropped with it")
	}
	if dest, ok := p.LinkAt(got["link"].X+1, got["link"].Y+1); !ok || dest != "u" {
		t.Errorf("<a href> link = %q, %v", dest, ok)
	}
}

// A README's centred block of HTML: centred, its text drawn, scripts not.
func TestHTMLBlock(t *testing.T) {
	p := lay("<p align=\"center\">\n  <b>Title</b> &amp; more\n</p>\n<script>alert(1)</script>", 400)
	s := textOf(p)
	if !strings.Contains(s, "Title") || !strings.Contains(s, "&") || strings.Contains(s, "alert") || strings.Contains(s, "<") {
		t.Errorf("drew %q", s)
	}
	for _, o := range texts(p) {
		if o.Text == "Title" && o.X < 100 {
			t.Errorf("not centred: %+v", o)
		}
	}
}

// Selection: a point maps to a character, two positions to the text
// between them, and that text to highlight bands.
func TestSelection(t *testing.T) {
	p := lay("hello world\n\nsecond para", 400) // 5 px a character
	a, ok := p.Hit(2*5+1, 2)                    // inside "hello", after "he"
	if !ok || p.Ops[a.Op].Text != "hello" || a.Off != 2 {
		t.Fatalf("hit %+v (%q)", a, p.Ops[a.Op].Text)
	}
	b, _ := p.Hit(400, 40) // past the end of the second paragraph
	if got := p.Text(a, b); got != "llo world\n\nsecond para" {
		t.Errorf("text %q", got)
	}
	if got := p.Text(b, a); got != "llo world\n\nsecond para" {
		t.Errorf("backwards: %q", got)
	}
	rs := p.Rects(a, b)
	if len(rs) < 2 || rs[0].X != 10 {
		t.Errorf("rects %+v", rs)
	}
	s, _ := p.Start()
	e, _ := p.End()
	if got := p.Text(s, e); got != "hello world\n\nsecond para" {
		t.Errorf("all: %q", got)
	}
	_ = md.Parse
}
