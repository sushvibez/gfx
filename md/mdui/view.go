package mdui

import (
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/md/mdrender"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// View is a Markdown page that keeps its state between frames, for a page
// somebody reads rather than glances at: text can be selected with the
// mouse and copied (Ctrl+C, Ctrl+A for all of it), links are followed,
// and a link to a heading or a footnote ("#…") asks the host to scroll
// there.
//
//	v := mdui.NewView(src, mdui.Dark(15))
//	v.OnScroll = func(y float32) { scroller.ScrollTo(y) }
//	... each frame: ui.Div().Children(v.Element(600))
type View struct {
	opts Options
	src  string
	// OnScroll, when set, is asked to bring a height on the page (from
	// its top) into view: a "#heading" or footnote link was clicked.
	OnScroll func(y float32)

	page *mdrender.Page
	rect ui.Rect

	// The selection: anchor where the press was, caret where the pointer
	// is; dragging is the press under way.
	anchor, caret mdrender.Pos
	hasSel        bool
	dragging      bool
	pressX        float32
	pressY        float32
}

// NewView makes a page of src.
func NewView(src string, opts Options) *View {
	if opts.Style == nil {
		opts.Style = mdrender.DarkStyle(15)
	}
	return &View{src: src, opts: opts}
}

// SetSource replaces the text, dropping the selection if it changed.
func (v *View) SetSource(src string) {
	if src != v.src {
		v.src, v.hasSel = src, false
	}
}

// Page is the page as last laid out, or nil before the first frame.
func (v *View) Page() *mdrender.Page { return v.page }

// selColor is the selection's highlight.
var selColor = render.RGBA{R: 0.35, G: 0.55, B: 0.95, A: 0.35}

// Element is the page for this frame's tree, w wide.
func (v *View) Element(w float32) ui.Element {
	el := Markdown(v.src, w, v.opts)
	el.Canvas = ui.NewCanvasFlow(w, func(r *render.Renderer2D, f *render.Font, w float32) float32 {
		return el.layout(r, f, w).H
	}, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
		v.rect = rect
		p := el.layout(r, f, rect.W)
		v.page = p
		if v.hasSel {
			for _, s := range p.Rects(v.anchor, v.caret) {
				r.FillRect(rect.X+s.X, rect.Y+s.Y, s.W, s.H, selColor)
			}
		}
		p.Draw(r, fontsFor(r, f, v.opts.Style.Size), rect.X, rect.Y)
	})
	return ui.Div().Children(el.Canvas).OnPointer(v.pointer).OnKey(v.key).Cursor(app.CursorText)
}

// clickSlopPx is how far a press may travel and still be a click.
const clickSlopPx = 4

func (v *View) pointer(x, y float32, phase ui.DragPhase) {
	p := v.page
	if p == nil {
		return
	}
	px, py := x-v.rect.X, y-v.rect.Y
	switch phase {
	case ui.DragStart:
		v.pressX, v.pressY = x, y
		if pos, ok := p.Hit(px, py); ok {
			v.anchor, v.caret, v.dragging = pos, pos, true
		}
		v.hasSel = false
	case ui.DragMove:
		if !v.dragging {
			return
		}
		if pos, ok := p.Hit(px, py); ok {
			v.caret = pos
			v.hasSel = v.anchor != v.caret
		}
	case ui.DragEnd:
		v.dragging = false
		dx, dy := x-v.pressX, y-v.pressY
		if dx*dx+dy*dy > clickSlopPx*clickSlopPx {
			return
		}
		// A click: no selection, and a link under it is followed.
		v.hasSel = false
		if dest, ok := p.LinkAt(px, py); ok {
			v.follow(dest)
		}
	}
}

// follow goes where a link points: a place on this page, or the host's
// business.
func (v *View) follow(dest string) {
	if strings.HasPrefix(dest, "#") && v.page != nil {
		if y, ok := v.page.Anchor(dest); ok {
			if v.OnScroll != nil {
				v.OnScroll(y)
			}
			return
		}
	}
	if v.opts.OnLink != nil {
		v.opts.OnLink(dest)
	}
}

func (v *View) key(ev app.KeyEvent) {
	if !ev.Down || !ev.Ctrl {
		return
	}
	switch ev.Rune {
	case 'a', 'A':
		v.SelectAll()
	case 'c', 'C':
		if s := v.Selection(); s != "" {
			app.SetClipboardText(s)
		}
	}
}

// SelectAll selects the whole page.
func (v *View) SelectAll() {
	if v.page == nil {
		return
	}
	a, ok1 := v.page.Start()
	b, ok2 := v.page.End()
	if ok1 && ok2 {
		v.anchor, v.caret, v.hasSel = a, b, true
	}
}

// Selection is the selected text, or "".
func (v *View) Selection() string {
	if !v.hasSel || v.page == nil {
		return ""
	}
	return v.page.Text(v.anchor, v.caret)
}

// PlainText is the whole document as text, for a host that wants it
// without laying it out.
func (v *View) PlainText() string { return md.PlainText(parsed(v.src)) }
