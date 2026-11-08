package gml

// QtQuick.Pdf's object model: PdfDocument plus the viewer items
// (PdfMultiPageView / PdfPageView / PdfScrollablePageView).
//
// The pages are really rendered: render/pdf reads the file and draws each
// page's content stream through the 2D renderer, so a PdfDocument reports
// Ready with its true page count and metadata, and a viewer scrolls the
// pages at its own renderScale.

import (
	"time"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/pdf"
	"github.com/sushvibez/gfx/ui"
)

// Qt's QPdfDocument::Status.
const (
	pdfNull    = float64(0)
	pdfLoading = float64(1)
	pdfReady   = float64(2)
	pdfUnload  = float64(3)
	pdfError   = float64(4)
)

// pdfDocumentNS is the PdfDocument enum namespace (PdfDocument.Ready, …).
func pdfDocumentNS() map[string]any {
	return map[string]any{
		"Null": pdfNull, "Loading": pdfLoading, "Ready": pdfReady,
		"Unloading": pdfUnload, "Error": pdfError,
		// QPdfDocument::Error
		"NoError": float64(0), "UnknownError": float64(1),
		"DataNotYetAvailable": float64(2), "FileNotFound": float64(3),
		"InvalidFileFormat": float64(4), "IncorrectPassword": float64(5),
		"UnsupportedSecurityScheme": float64(6),
		// QPdfDocument::ModifiedFlags / page selection helpers markup reads
		"SinglePage": float64(0), "MultiPage": float64(1),
	}
}

// pdfState is a PdfDocument's retained state. source and status are engine
// properties, not plain values: a write from markup (a file dialog
// assigning `source`) has to invalidate the frame like any other property,
// and a binding that reads `status` has to be tracked. The parsed document
// is retained beside them — reading a 40MB book on every frame is not a
// thing to do.
type pdfState struct {
	source Ref
	status Ref
	doc    *pdf.Document
	loaded string // the source the document was parsed from
	err    string
}

func (doc *docState) pdfFor(key string) *pdfState {
	if doc.pdfs == nil {
		doc.pdfs = map[string]*pdfState{}
	}
	if p, ok := doc.pdfs[key]; ok {
		return p
	}
	p := &pdfState{
		source: doc.strProp(key+".source", ""),
		status: doc.floatProp(key+".status", float32(pdfNull)),
	}
	doc.pdfs[key] = p
	return p
}

// pdfStatusOf reads the status as a number.
func pdfStatusOf(p *pdfState) float64 {
	f, _ := toFloat64(p.status.get())
	return f
}

// refreshPdfDocument services a PdfDocument declaration and exposes the QML
// contract on its id.
func (doc *docState) refreshPdfDocument(n *node, env *Env) {
	p := doc.pdfFor(doc.key(n))
	for _, a := range n.attrs {
		if a.name != "source" || a.expr == nil {
			continue
		}
		if v, err := a.expr.eval(env); err == nil {
			_ = pdfSourceRef{p}.set(str(v))
		}
	}
	doc.loadPdf(p)
	doc.exposeIDProp(n, "source", pdfSourceRef{p})
	doc.exposeIDProp(n, "status", p.status)
	doc.exposeIDProp(n, "pageCount", float64(p.pageCount()))
	doc.exposeIDProp(n, "title", p.info("Title"))
	doc.exposeIDProp(n, "author", p.info("Author"))
	doc.exposeIDProp(n, "subject", p.info("Subject"))
	doc.exposeIDProp(n, "keywords", p.info("Keywords"))
	doc.exposeIDProp(n, "creator", p.info("Creator"))
	doc.exposeIDProp(n, "producer", p.info("Producer"))
	doc.exposeIDProp(n, "password", "")
	doc.exposeIDProp(n, "error", pdfErrorOf(p))
	doc.exposeIDProp(n, "errorString", pdfErrorString(p))
	// The document object is what a viewer item resolves `document:` to.
	if id := nodeID(n); id != "" {
		if obj := doc.elemObjs.get(doc.here(id)); obj != nil {
			obj.pdf = p
		}
	}
	doc.exposeIDMethods(n, map[string]any{
		"pagePointSize": jsFunc(func(args []any) (any, error) {
			w, h := p.pageSize(argIndex(args))
			return map[string]any{"width": float64(w), "height": float64(h)}, nil
		}),
		"pageLabel": jsFunc(func(args []any) (any, error) {
			return str(float64(argIndex(args) + 1)), nil
		}),
		"loadPage": jsFunc(func([]any) (any, error) { return nil, nil }),
		"close": jsFunc(func([]any) (any, error) {
			return nil, pdfSourceRef{p}.set("")
		}),
	})
}

// loadPdf parses the document when its source changes, once.
func (doc *docState) loadPdf(p *pdfState) {
	src := str(p.source.get())
	if src == p.loaded {
		return
	}
	p.loaded, p.doc, p.err = src, nil, ""
	if src == "" {
		_ = p.status.set(pdfNull)
		return
	}
	path := resolveImagePath(doc.baseDir, src)
	d, err := pdf.Open(path)
	if err != nil {
		p.err = err.Error()
		_ = p.status.set(pdfError)
		return
	}
	p.doc = d
	_ = p.status.set(pdfReady)
}

func (p *pdfState) pageCount() int {
	if p.doc == nil {
		return 0
	}
	return p.doc.PageCount()
}

func (p *pdfState) info(key string) string {
	if p.doc == nil {
		return ""
	}
	return p.doc.Info(pdf.Name(key))
}

// pageSize is a page's size in points.
func (p *pdfState) pageSize(i int) (float32, float32) {
	if p.doc == nil {
		return 0, 0
	}
	pg := p.doc.Page(i)
	if pg == nil {
		return 0, 0
	}
	return pg.Size()
}

// argIndex reads a page index argument.
func argIndex(args []any) int {
	if len(args) == 0 {
		return 0
	}
	f, err := toFloat64(args[0])
	if err != nil || f < 0 {
		return 0
	}
	return int(f)
}

func pdfErrorOf(p *pdfState) float64 {
	if pdfStatusOf(p) == pdfError {
		return 4 // InvalidFileFormat
	}
	return 0
}

func pdfErrorString(p *pdfState) string { return p.err }

// pdfSourceRef makes `doc.source` assignable (a file dialog or a drop
// handler writes it) and readable back as its URL string.
type pdfSourceRef struct{ p *pdfState }

func (r pdfSourceRef) get() any { return r.p.source.get() }
func (r pdfSourceRef) set(v any) error {
	src := str(v)
	if src == str(r.p.source.get()) {
		return nil
	}
	if err := r.p.source.set(src); err != nil {
		return err
	}
	// The document is parsed on the next build (which the write triggers),
	// so the status only says "not yet" here.
	if src == "" {
		return r.p.status.set(pdfNull)
	}
	return r.p.status.set(pdfLoading)
}
func (r pdfSourceRef) animate(any, time.Duration) error { return nil }
func (r pdfSourceRef) onChange(func())                  {}

// buildPdfView renders a PDF viewer item: the pages of its document,
// scrolled, at its own renderScale — plus the view state its toolbar binds
// to (currentPage, zoom, page navigation).
func buildPdfView(n *node, env *Env, doc *docState, kind string) (ui.Element, error) {
	var w, h float32
	grow := false
	var st *pdfState
	var curAttr *attr
	for i := range n.attrs {
		a := &n.attrs[i]
		switch a.name {
		case "currentPage":
			curAttr = a
		case "width":
			if f, err := attrFloat(*a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(*a, env); err == nil {
				h = f
			}
		case "anchors.fill":
			grow = true
		case "renderScale":
			if f, err := attrFloat(*a, env); err == nil && f > 0 {
				seen := doc.floatProp(doc.key(n)+"|scaleAttr", f+1)
				if floatRef(seen) != float64(f) {
					_ = seen.set(f)
					_ = doc.floatProp(doc.key(n)+".renderScale", 1).set(f)
				}
			}
		case "document":
			st = pdfStateOf(a.expr, env)
		default:
			// searchString, rotation and the rest of the contract: accepted.
		}
	}
	key := doc.key(n)
	pageRef := doc.floatProp(key+".currentPage", 0)
	zoomRef := doc.floatProp(key+".renderScale", 1)
	// The page the markup asks for: a literal seeds the view, a binding
	// turns it. Told apart from a scroll or a Next click by remembering the
	// value last taken from the markup.
	if curAttr != nil {
		if f, err := attrFloat(*curAttr, env); err == nil {
			seen := doc.floatProp(key+"|pageAttr", f+1)
			if floatRef(seen) != float64(f) {
				_ = seen.set(f)
				_ = pageRef.set(f)
			}
		}
	}
	pages := 0
	if st != nil {
		pages = st.pageCount()
	}
	doc.exposeIDProp(n, "currentPage", pageRef)
	doc.exposeIDProp(n, "renderScale", zoomRef)
	doc.exposeIDProp(n, "pageCount", float64(pages))
	doc.exposeIDProp(n, "rotation", float64(0))
	doc.exposeIDProp(n, "searchString", "")
	doc.exposeIDProp(n, "currentPageRenderingStatus", float64(0))
	clampPage := func(v float64) float64 {
		if pages > 0 {
			return min(max(v, 0), float64(pages-1))
		}
		return max(v, 0)
	}
	step := func(d float64) jsFunc {
		return func([]any) (any, error) {
			cur, _ := toFloat64(pageRef.get())
			return nil, pageRef.set(clampPage(cur + d))
		}
	}
	doc.exposeIDMethods(n, map[string]any{
		"goToPage": jsFunc(func(args []any) (any, error) {
			if len(args) > 0 {
				if f, err := toFloat64(args[0]); err == nil {
					return nil, pageRef.set(clampPage(f))
				}
			}
			return nil, nil
		}),
		"goToNextPage":     step(1),
		"goToPreviousPage": step(-1),
		"goToLocation":     jsFunc(func([]any) (any, error) { return nil, nil }),
		"scaleToWidth":     jsFunc(func([]any) (any, error) { return nil, zoomRef.set(0) }),
		"scaleToPage":      jsFunc(func([]any) (any, error) { return nil, zoomRef.set(0) }),
		"resetScale":       jsFunc(func([]any) (any, error) { return nil, zoomRef.set(1) }),
		"searchBack":       jsFunc(func([]any) (any, error) { return nil, nil }),
		"searchForward":    jsFunc(func([]any) (any, error) { return nil, nil }),
	})
	if st == nil || st.doc == nil {
		box := ui.Div()
		if w > 0 || h > 0 {
			box.Size(w, h)
		}
		if grow {
			box.Grow()
		}
		return box, nil
	}
	// A multi-page view scrolls its pages; a single-page view draws the
	// current one to fit. Either way the drawing itself is a canvas: the
	// page interpreter emits renderer calls, like any other 2D content.
	single := kind != "PdfMultiPageView"
	scale := float32(1)
	if f, err := toFloat32(zoomRef.get()); err == nil && f > 0 {
		scale = f
	}
	cur := int(clampPage(floatRef(pageRef)))
	if single {
		return pdfSinglePage(st, cur, scale, w, h, grow), nil
	}
	return pdfScrolledPages(doc, key, st, cur, scale, w, h, grow), nil
}

// pdfSinglePage draws one page, fitted to the item when no scale is set.
func pdfSinglePage(st *pdfState, page int, scale, w, h float32, grow bool) ui.Element {
	canvas := ui.NewCanvasFont(w, h, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
		rd := pdf.NewRenderer(st.doc, r)
		rd.SetFallbackFont(f)
		rd.DrawPage(page, rect.X, rect.Y, rect.W, rect.H)
	})
	if grow {
		return ui.Div().Grow().AnchorMode().Children(
			ui.Anchored(canvas, "", ui.Anchors{Fill: "parent"}))
	}
	return canvas
}

// pdfScrolledPages lays the pages out in a scroll area, each at its own
// size, and draws the ones the viewport can see.
func pdfScrolledPages(doc *docState, key string, st *pdfState, page int, scale, w, h float32, grow bool) ui.Element {
	const gap = 12
	state := doc.scrolls[key+"|pdf"]
	if state == nil {
		state = &ui.ScrollState{}
		doc.scrolls[key+"|pdf"] = state
	}
	s := ui.Scroll(state).Flickable().AutoHideBar()
	if w > 0 {
		s.W(w)
	}
	if h > 0 {
		s.H(h)
	}
	if grow {
		s.Grow()
	}
	// Every page gets a box of its own size, but only the pages near the
	// viewport draw: a 700-page book must not interpret 700 content
	// streams to show one of them.
	count := st.pageCount()
	first, last := max(page-1, 0), min(page+2, count)
	// currentPage drives the scroll: a toolbar's Next button, or a page
	// number typed into a field, has to move the view. The offset is
	// tracked here rather than read back, so a user's own scrolling is not
	// fought over (only a CHANGE of currentPage scrolls).
	synced := doc.floatProp(key+"|pdfPage", -1)
	// The scroll area clamps its offset to the content height, which is
	// only known once it has been laid out — so a page change is applied
	// for a few builds rather than once, and then left alone so the
	// reader's own scrolling is not fought over.
	tries := doc.floatProp(key+"|pdfSettle", 0)
	var offset float32
	for i := 0; i < count; i++ {
		pw, ph := st.pageSize(i)
		bw, bh := pw*scale, ph*scale
		if bw <= 0 || bh <= 0 {
			continue
		}
		box := ui.Div().Size(bw, bh).Bg(ui.RGB(0xffffff))
		if i >= first && i < last {
			idx := i
			box.Children(ui.NewCanvasFont(bw, bh,
				func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
					rd := pdf.NewRenderer(st.doc, r)
					rd.SetFallbackFont(f)
					rd.DrawPageAt(idx, rect.X, rect.Y, scale)
				}))
		}
		wrap := ui.Div().Col().ItemsCenter().P(gap / 2).Children(box)
		s.Children(wrap)
		if i < page {
			offset += bh + gap
		}
	}
	if int(floatRef(synced)) != page {
		_ = synced.set(float32(page))
		_ = tries.set(3)
	}
	if n := floatRef(tries); n > 0 {
		_ = tries.set(float32(n - 1))
		state.Y = offset
	}
	return s
}

// pdfStateOf resolves a viewer's `document:` expression to the document
// object it names.
func pdfStateOf(e expr, env *Env) *pdfState {
	if e == nil {
		return nil
	}
	v, err := e.eval(env)
	if err != nil {
		return nil
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if obj, ok := v.(*elemObject); ok {
		return obj.pdf
	}
	return nil
}

// floatRef reads a numeric ref.
func floatRef(r Ref) float64 {
	f, _ := toFloat64(r.get())
	return f
}
