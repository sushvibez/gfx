//go:build !js

package gml

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// onLinkActivated runs when a link in Markdown text is clicked, with the
// link's destination as `link`; a click off the link runs nothing.
func TestMarkdownLinkActivated(t *testing.T) {
	lib := os.Getenv("GFX_WGPU_LIB")
	if lib == "" {
		lib, _ = filepath.Abs(filepath.Join("..", "..", "build", "wgpu", gpu.DefaultLibNames()[0]))
		if _, err := os.Stat(lib); err != nil {
			t.Skip("wgpu-native not found")
		}
	}
	inst, err := gpu.NewInstance(lib)
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
	src := `Item {
    property string got: "none"
    Text { width: 300; textFormat: Text.MarkdownText; text: "[go there](https://example.com) and plain text"
        onLinkActivated: got = link }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	view := d.View()
	u := ui.New(r, font, func() ui.Element { return view() })
	frame := func() { r.Begin(400, 200); u.Frame(400, 200) }
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: x, Y: y}, {Kind: app.MouseUp, Button: 1, X: x, Y: y}})
		frame()
	}
	frame()
	click(250, 8) // on "plain text"
	if got := d.Property("got"); got != "none" {
		t.Errorf("a click off the link ran the handler: got = %v", got)
	}
	click(6, 8) // on "go"
	if got := d.Property("got"); got != "https://example.com" {
		t.Errorf("got = %v after clicking the link", got)
	}
}
