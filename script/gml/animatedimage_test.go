package gml

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

func TestAnimatedImageAttrs(t *testing.T) {
	dir := t.TempDir()
	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{}
	for i := 0; i < 3; i++ {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 4, 4), pal))
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spin.gif"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
import QtQuick 2.15
Item {
    width: 100; height: 100
    property bool hold: true
    AnimatedImage { id: spin; source: "spin.gif"; paused: hold; currentFrame: 2 }
    BorderImage { id: frame; source: "spin.gif"; width: 50; height: 50
        border.left: 1; border.top: 1; border.right: 1; border.bottom: 1
        horizontalTileMode: BorderImage.Round; verticalTileMode: BorderImage.Repeat }
    Image { source: "spin.gif"; width: 20; height: 20; fillMode: Image.Tile }
}`
	env := NewEnv()
	d, err := LoadStringAt(src, dir, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100)
	for expr, want := range map[string]string{
		"spin.frameCount":   "3",
		"spin.currentFrame": "2",
		"spin.paused":       "true",
	} {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if str(v) != want {
			t.Errorf("%s = %v, want %s", expr, v, want)
		}
	}
}

// QML's grouped form, border { left: 3; top: 4 }, reaches buildImage as
// the border.* attributes.
func TestBorderImageGroupedBorder(t *testing.T) {
	n, err := Parse(`BorderImage { border { left: 3; top: 4 } }`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, a := range n.attrs {
		got[a.name] = true
	}
	if !got["border.left"] || !got["border.top"] {
		t.Errorf("attrs = %v, want border.left and border.top", got)
	}
}
