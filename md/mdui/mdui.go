// Package mdui is Markdown as a ui element: a page laid out to the
// element's width, as tall as it needs.
//
//	ui.Div().Col().Children(
//		mdui.Markdown(src, 600, mdui.Dark(15)),
//	)
package mdui

import (
	"sync"

	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/md/mdrender"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Options are how a Markdown element looks.
type Options struct {
	Style *mdrender.Style
	// OnLink, when set, is called with a link's destination when it is
	// clicked (see Element.Click).
	OnLink func(dest string)
	// Images, when set, supplies the pictures the document names; without
	// it a picture is shown as its alt text.
	Images *mdrender.Images
}

// Dark is light text on a dark page at a body size.
func Dark(size float32) Options { return Options{Style: mdrender.DarkStyle(size)} }

// Light is dark text on a light page at a body size.
func Light(size float32) Options { return Options{Style: mdrender.LightStyle(size)} }

// Element is a Markdown page in a ui tree.
type Element struct {
	*ui.Canvas
	src  string
	w    float32
	opts Options
	page *mdrender.Page
	rect ui.Rect
}

// Markdown renders src, w wide.
//
// The UI tree is rebuilt every frame, so what costs anything is kept:
// the parsed document by its source, the fonts by renderer and size, and
// the laid-out page by source, width and style.
func Markdown(src string, w float32, opts Options) *Element {
	if opts.Style == nil {
		opts.Style = mdrender.DarkStyle(15)
	}
	e := &Element{src: src, w: w, opts: opts}
	e.Canvas = ui.NewCanvasFlow(w, func(r *render.Renderer2D, f *render.Font, w float32) float32 {
		return e.layout(r, f, w).H
	}, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
		e.rect = rect
		p := e.layout(r, f, rect.W)
		p.Draw(r, fontsFor(r, f, e.opts.Style.Size), rect.X, rect.Y)
	})
	return e
}

// Page is the laid-out page as last drawn, or nil before the first frame.
func (e *Element) Page() *mdrender.Page { return e.page }

// Click reports whether a point (in UI coordinates) is on a link, calls
// OnLink with it if so, and returns the destination.
func (e *Element) Click(x, y float32) (string, bool) {
	if e.page == nil {
		return "", false
	}
	dest, ok := e.page.LinkAt(x-e.rect.X, y-e.rect.Y)
	if ok && e.opts.OnLink != nil {
		e.opts.OnLink(dest)
	}
	return dest, ok
}

func (e *Element) layout(r *render.Renderer2D, uiFont *render.Font, w float32) *mdrender.Page {
	key := pageKey{src: e.src, w: w, st: *e.opts.Style, imgs: e.opts.Images}
	if e.opts.Images != nil {
		key.gen = e.opts.Images.Gen() // a picture that arrives relays the page
	}
	cache.Lock()
	p := cache.pages[key]
	cache.Unlock()
	if p == nil {
		fs := fontsFor(r, uiFont, e.opts.Style.Size)
		var imgs mdrender.ImageSource
		if e.opts.Images != nil {
			imgs = e.opts.Images
		}
		p = mdrender.LayoutImages(parsed(e.src), w, e.opts.Style, fs.Metrics(), imgs)
		cache.Lock()
		if len(cache.pages) > 64 {
			clear(cache.pages)
		}
		cache.pages[key] = p
		cache.Unlock()
	}
	e.page = p
	return p
}

type pageKey struct {
	src  string
	w    float32
	st   mdrender.Style
	imgs *mdrender.Images
	gen  int
}

type fontKey struct {
	r    *render.Renderer2D
	size float32
}

var cache = struct {
	sync.Mutex
	docs  map[string]*md.Node
	fonts map[fontKey]*mdrender.Fonts
	pages map[pageKey]*mdrender.Page
}{docs: map[string]*md.Node{}, fonts: map[fontKey]*mdrender.Fonts{}, pages: map[pageKey]*mdrender.Page{}}

func parsed(src string) *md.Node {
	cache.Lock()
	defer cache.Unlock()
	if d := cache.docs[src]; d != nil {
		return d
	}
	if len(cache.docs) > 64 {
		clear(cache.docs)
	}
	d := md.Parse(src)
	cache.docs[src] = d
	return d
}

// fontsFor loads the Go fonts once per renderer and size, or falls back to
// the UI's own font for every face if they will not load.
func fontsFor(r *render.Renderer2D, uiFont *render.Font, size float32) *mdrender.Fonts {
	k := fontKey{r, size}
	cache.Lock()
	defer cache.Unlock()
	if fs := cache.fonts[k]; fs != nil {
		return fs
	}
	fs, err := mdrender.LoadFonts(r, float64(size))
	if err != nil {
		return mdrender.WithFonts(r, uiFont, nil, nil, nil, nil)
	}
	cache.fonts[k] = fs
	return fs
}
