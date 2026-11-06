//go:build !js

package mdui

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/md/mdrender"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

func wgpuLib(t *testing.T) string {
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return p
	}
	p, _ := filepath.Abs(filepath.Join("..", "..", "build", "wgpu", gpu.DefaultLibNames()[0]))
	if _, err := os.Stat(p); err != nil {
		t.Skip("wgpu-native not found")
	}
	return p
}

// A Markdown element in a UI is as tall as its page, draws its text, and
// finds its links.
func TestMarkdownElement(t *testing.T) {
	const W, H = 400, 300
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatal(err)
	}
	queue := device.Queue()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	font, err := r.NewFont(goregular.TTF, 15)
	if err != nil {
		t.Fatal(err)
	}
	var el *Element
	clicked := ""
	u := ui.New(r, font, func() ui.Element {
		el = Markdown("# Title\n\nSome **bold** text and [a link](https://example.com).\n\n- one\n- two",
			360, Options{Style: Dark(15).Style, OnLink: func(d string) { clicked = d }})
		return ui.Div().P(20).Children(el)
	})
	r.Begin(W, H)
	u.Frame(W, H)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatal(err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatal(err)
	}
	p := el.Page()
	if p == nil || p.H < 80 {
		t.Fatalf("page %v: want a heading, a paragraph and two items", p)
	}
	lit := 0
	for y := 20; y < 20+int(p.H); y++ {
		for x := 20; x < 380; x++ {
			if pix[(y*W+x)*4] > 128 {
				lit++
			}
		}
	}
	if lit < 200 {
		t.Errorf("only %d bright pixels where the page was drawn", lit)
	}
	l := p.Links[0]
	if dest, ok := el.Click(el.rect.X+l.X+1, el.rect.Y+l.Y+1); !ok || dest != "https://example.com" || clicked != dest {
		t.Errorf("click on the link: %q %v, OnLink got %q", dest, ok, clicked)
	}
}

// A View is selected by dragging across it, copied with Ctrl+C, selected
// whole with Ctrl+A, and a "#heading" link asks the host to scroll there.
func TestViewSelectAndFollow(t *testing.T) {
	const W, H = 400, 400
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatal(err)
	}
	r, err := render.New2D(device, device.Queue(), gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	font, err := r.NewFont(goregular.TTF, 15)
	if err != nil {
		t.Fatal(err)
	}
	v := NewView("[Go down](#far-below)\n\nhello world\n\n# Far below\n\ntext", Dark(15))
	scrolled := float32(-1)
	v.OnScroll = func(y float32) { scrolled = y }
	u := ui.New(r, font, func() ui.Element { return ui.Div().P(20).Children(v.Element(360)) })
	frame := func() {
		r.Begin(W, H)
		u.Frame(W, H)
	}
	frame()
	p := v.Page()
	if p == nil {
		t.Fatal("no page")
	}
	op := func(text string) mdrender.Op {
		for _, o := range p.Ops {
			if o.Kind == mdrender.OpText && o.Text == text {
				return o
			}
		}
		t.Fatalf("no %q on the page", text)
		return mdrender.Op{}
	}
	// Drag from the start of "hello" to the end of "world".
	h, w := op("hello"), op("world")
	x0, y0 := v.rect.X+h.X+0.5, v.rect.Y+h.Y+5
	x1, y1 := v.rect.X+w.X+w.W-0.5, v.rect.Y+w.Y+5
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: x0, Y: y0}})
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x1, Y: y1}})
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: 1, X: x1, Y: y1}})
	frame()
	if got := v.Selection(); got != "hello world" {
		t.Errorf("dragged selection %q", got)
	}
	u.Key(app.KeyEvent{Down: true, Ctrl: true, Rune: 'c'})
	if got := app.ClipboardText(); got != "hello world" {
		t.Errorf("clipboard %q after Ctrl+C", got)
	}
	u.Key(app.KeyEvent{Down: true, Ctrl: true, Rune: 'a'})
	if got := v.Selection(); got != "Go down\n\nhello world\n\nFar below\n\ntext" {
		t.Errorf("Ctrl+A selected %q", got)
	}
	// A click on the link scrolls to the heading, and clears the selection.
	l := op("Go")
	cx, cy := v.rect.X+l.X+2, v.rect.Y+l.Y+5
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: cx, Y: cy}, {Kind: app.MouseUp, Button: 1, X: cx, Y: cy}})
	frame()
	want, _ := p.Anchor("#far-below")
	if scrolled != want || want <= 0 {
		t.Errorf("scrolled to %v, want the heading at %v", scrolled, want)
	}
	if v.Selection() != "" {
		t.Error("a click left the selection")
	}
}
