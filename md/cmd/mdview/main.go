//go:build !js

// mdview renders a Markdown file headlessly to a PNG.
//
//	cd md && go run ./cmd/mdview -out page.png            (the built-in sample)
//	go run ./cmd/mdview -in README.md -out page.png -w 900 -light
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/md/mdrender"
	"github.com/sushvibez/gfx/render"
)

//go:embed sample.md
var sample string

//go:embed sample.png
var samplePNG []byte

//go:embed icon.png
var iconPNG []byte

func main() {
	in := flag.String("in", "", "Markdown file (default: the built-in sample)")
	out := flag.String("out", "mdview.png", "PNG to write")
	width := flag.Int("w", 820, "page width in pixels")
	size := flag.Float64("size", 15, "body text size in pixels")
	light := flag.Bool("light", false, "dark text on a light page")
	base := flag.String("base", "", "directory pictures are relative to (default: the input's)")
	flag.Parse()
	src := sample
	imgs := mdrender.NewImages(*base)
	for name, data := range map[string][]byte{"sample.png": samplePNG, "icon.png": iconPNG} {
		if err := imgs.Add(name, data); err != nil {
			fail(err)
		}
	}
	if *in != "" {
		if *base == "" {
			imgs.Base = filepath.Dir(*in)
		}
		b, err := os.ReadFile(*in)
		if err != nil {
			fail(err)
		}
		src = string(b)
	}
	if err := draw(src, imgs, *out, *width, float32(*size), *light); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "mdview:", err)
	os.Exit(1)
}

func draw(src string, imgs *mdrender.Images, out string, width int, size float32, light bool) error {
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	r, err := renderer(device, queue)
	if err != nil {
		return err
	}
	fonts, err := mdrender.LoadFonts(r, float64(size))
	if err != nil {
		return err
	}
	defer fonts.Release()
	st, bg := mdrender.DarkStyle(size), gpu.Color{R: 0.07, G: 0.08, B: 0.1, A: 1}
	if light {
		st, bg = mdrender.LightStyle(size), gpu.Color{R: 1, G: 1, B: 1, A: 1}
	}
	pad := float32(32)
	doc := md.Parse(src)
	// Ask for every picture once, then wait for the remote ones: a PNG
	// is drawn once, so it has to be drawn with them.
	md.Walk(doc, func(n *md.Node) bool {
		if n.Kind == md.Image {
			imgs.Size(n.Dest)
		}
		return true
	})
	imgs.Wait()
	md.Walk(doc, func(n *md.Node) bool {
		if n.Kind == md.Image {
			if _, _, ok := imgs.Size(n.Dest); !ok {
				fmt.Fprintf(os.Stderr, "mdview: picture %s: %v\n", n.Dest, imgs.Err(n.Dest))
			}
		}
		return true
	})
	defer imgs.Release()
	page := mdrender.LayoutImages(doc, float32(width)-2*pad, st, fonts.Metrics(), imgs)
	h := int(page.H + 2*pad)
	target := device.CreateRenderTexture(width, h, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r.Begin(width, h)
	page.Draw(r, fonts, pad, pad)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, bg)
	if err := r.Flush(pass); err != nil {
		return err
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, width, h)
	if err != nil {
		return err
	}
	img := &image.NRGBA{Pix: pix, Stride: width * 4, Rect: image.Rect(0, 0, width, h)}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "mdview: wrote %s (%dx%d)\n", out, width, h)
	return nil
}

func renderer(d *gpu.Device, q *gpu.Queue) (*render.Renderer2D, error) {
	return render.New2D(d, q, gpu.FormatRGBA8Unorm)
}
