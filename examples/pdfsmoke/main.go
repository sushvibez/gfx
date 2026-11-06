//go:build linux || windows

// Command pdfsmoke renders one PDF page to a PNG through the framework's
// own renderer — the quickest way to see what the PDF reader draws.
//
//	go run ./pdfsmoke -page 3 -scale 1.5 -out /tmp/p.png some.pdf
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/pdf"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

func main() {
	page := flag.Int("page", 0, "page index (0-based)")
	scale := flag.Float64("scale", 1, "render scale (1 = 72dpi points to pixels)")
	out := flag.String("out", "/tmp/pdfpage.png", "output PNG")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pdfsmoke [-page n] [-scale f] [-out png] file.pdf")
		os.Exit(2)
	}
	doc, err := pdf.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	p := doc.Page(*page)
	if p == nil {
		fmt.Fprintf(os.Stderr, "page %d does not exist (%d pages)\n", *page, doc.PageCount())
		os.Exit(1)
	}
	pw, ph := p.Size()
	s := float32(*scale)
	w, h := int(pw*s+0.5), int(ph*s+0.5)
	fmt.Printf("%s: page %d of %d, %.0fx%.0f pt -> %dx%d px\n",
		flag.Arg(0), *page, doc.PageCount(), pw, ph, w, h)

	view := func(a *shell.App) (func() ui.Element, error) {
		rd := pdf.NewRenderer(doc, a.R2)
		canvas := ui.NewCanvasFont(float32(w), float32(h),
			func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
				rd.SetFallbackFont(f)
				rd.DrawPageAt(*page, rect.X, rect.Y, s)
			})
		return func() ui.Element { return canvas }, nil
	}
	if err := shell.Screenshot(shell.Options{
		Width: w, Height: h, ClearHex: 0xffffff,
	}, shell.Hooks{Init: view}, *out); err != nil {
		fmt.Fprintln(os.Stderr, "screenshot:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
