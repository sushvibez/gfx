package mdrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/render"
)

// sizes is an image source with fixed sizes and no textures.
type sizes map[string][2]float32

func (s sizes) Size(dest string) (float32, float32, bool) {
	v, ok := s[dest]
	return v[0], v[1], ok
}

func (sizes) Image(*render.Renderer2D, string) *render.Image { return nil }

func images(p *Page) []Op {
	var out []Op
	for _, o := range p.Ops {
		if o.Kind == OpImage {
			out = append(out, o)
		}
	}
	return out
}

// A picture on its own line keeps its size, and the text after it starts
// below it.
func TestBlockImage(t *testing.T) {
	src := "![a cat](cat.png)\n\nafter"
	p := LayoutImages(md.Parse(src), 400, DarkStyle(10), cm, sizes{"cat.png": {200, 100}})
	im := images(p)
	if len(im) != 1 || im[0].W != 200 || im[0].H != 100 || im[0].Text != "cat.png" {
		t.Fatalf("images = %+v", im)
	}
	for _, o := range texts(p) {
		if o.Text == "[a cat]" {
			t.Error("alt text drawn as well as the picture")
		}
		if o.Text == "after" && o.Y < im[0].Y+im[0].H {
			t.Errorf("text at %v overlaps the picture ending at %v", o.Y, im[0].Y+im[0].H)
		}
	}
}

// A picture wider than the column shrinks to it, keeping its shape.
func TestWideImageFits(t *testing.T) {
	p := LayoutImages(md.Parse("![](big.png)"), 300, DarkStyle(10), cm, sizes{"big.png": {1200, 600}})
	im := images(p)
	if len(im) != 1 || im[0].W != 300 || im[0].H != 150 {
		t.Fatalf("images = %+v", im)
	}
	if p.H < 150 {
		t.Errorf("page height %v shorter than the picture", p.H)
	}
}

// An unknown picture falls back to its alt text; a linked picture is a
// link box.
func TestImageFallbackAndLink(t *testing.T) {
	p := LayoutImages(md.Parse("![gone](x.png) [![icon](i.png)](https://example.com)"), 400, DarkStyle(10), cm, sizes{"i.png": {16, 16}})
	var alt bool
	for _, o := range texts(p) {
		alt = alt || o.Text == "[gone]"
	}
	if !alt {
		t.Error("missing picture's alt text not shown")
	}
	im := images(p)
	if len(im) != 1 {
		t.Fatalf("images = %+v", im)
	}
	if dest, ok := p.LinkAt(im[0].X+8, im[0].Y+8); !ok || dest != "https://example.com" {
		t.Errorf("LinkAt on the picture = %q, %v", dest, ok)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Images reads sizes from files, data URIs, added bytes and URLs.
func TestImagesSources(t *testing.T) {
	data := pngBytes(t, 30, 20)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my pic.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer srv.Close()
	im := NewImages(dir)
	loads := 0
	im.OnLoad = func() { loads++ }
	if err := im.Add("mem", data); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"my pic.png", "my%20pic.png", "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), "mem"} {
		if w, h, ok := im.Size(dest); !ok || w != 30 || h != 20 {
			t.Errorf("Size(%.40q) = %v, %v, %v (%v)", dest, w, h, ok, im.Err(dest))
		}
	}
	if _, _, ok := im.Size("missing.png"); ok || im.Err("missing.png") == nil {
		t.Error("a missing file has a size")
	}
	g := im.Gen()
	if _, _, ok := im.Size(srv.URL + "/p.png"); ok {
		t.Error("a URL has a size before it is fetched")
	}
	im.Wait()
	if w, _, ok := im.Size(srv.URL + "/p.png"); !ok || w != 30 {
		t.Errorf("fetched picture: %v %v (%v)", w, ok, im.Err(srv.URL+"/p.png"))
	}
	if im.Gen() == g || loads != 1 {
		t.Errorf("gen %d→%d, %d loads: a fetch must announce itself", g, im.Gen(), loads)
	}
}
