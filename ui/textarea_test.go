//go:build !js

package ui

import (
	"fmt"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// runeWidth measures a rune as one unit, for layout tests without a font.
func runeWidth(s string) float32 { return float32(len([]rune(s))) }

func TestWrapLines(t *testing.T) {
	for _, tc := range []struct {
		in   string
		w    float32
		want string
	}{
		{"six rangers up a short strip", 12, "[six rangers up a short strip]"},
		{"line one\nline two", 40, "[line one line two]"},
		{"ends with a break\n", 40, "[ends with a break ]"},
		{"", 40, "[]"},
		{"abcdefghij", 4, "[abcd efgh ij]"},
	} {
		rs := []rune(tc.in)
		var got []string
		for _, ln := range WrapLines(rs, tc.w, runeWidth) {
			got = append(got, string(rs[ln.Start:ln.End]))
		}
		if fmt.Sprint(got) != tc.want {
			t.Errorf("wrap %q at %.0f: %v, want %s", tc.in, tc.w, got, tc.want)
		}
	}
	lines := WrapLines([]rune("first line\nsecond line"), 40, runeWidth)
	if c := MoveLine(lines, 3, 1); c != 14 {
		t.Errorf("down from column 3: %d, want 14", c)
	}
	if c := MoveLine(lines, 14, -1); c != 3 {
		t.Errorf("up from column 3: %d, want 3", c)
	}
	if c := MoveLine(lines, 3, -1); c != 0 {
		t.Errorf("up from the first line: %d, want 0", c)
	}
}

// A text area takes Enter as a line break and Ctrl+Enter as submit, moves
// by line with Up/Down and Home/End, and pastes line breaks — through the
// same focus and key routing as a field.
func TestTextAreaKeys(t *testing.T) {
	st := NewInput("ab")
	submitted := ""
	st.OnSubmit(func(s string) { submitted = s })
	var area *InputEl
	view := func() Element {
		area = TextArea(st, 200, 80)
		return Div().Col().P(10).Children(area)
	}
	u := New(nil, nil, view)
	u.root = view()
	u.root.layout(&u.ctx, Rect{0, 0, 300, 200})
	ax, ay := area.bounds().X+5, area.bounds().Y+5
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: ax, Y: ay, Button: 1}, {Kind: app.MouseUp, X: ax, Y: ay, Button: 1}})
	if u.ctx.focus != st {
		t.Fatal("press on the area did not focus it")
	}
	type k = app.KeyEvent
	u.Key(k{Down: true, Key: app.KeyEnd})
	u.Key(k{Down: true, Key: app.KeyEnter})
	u.Key(k{Down: true, Rune: 'c'})
	if got := st.Text.Get(); got != "ab\nc" {
		t.Fatalf("enter in a text area: %q, want %q", got, "ab\nc")
	}
	if submitted != "" {
		t.Fatal("plain enter submitted a text area")
	}
	// Up/Down and Home/End by line (the layout a paint would have made).
	st.lines = WrapLines([]rune(st.Text.Get()), 100, runeWidth)
	u.Key(k{Down: true, Key: app.KeyUp})
	if st.caret != 1 {
		t.Errorf("up from \"c|\": caret %d, want 1", st.caret)
	}
	u.Key(k{Down: true, Key: app.KeyEnd})
	if st.caret != 2 {
		t.Errorf("end on the first line: caret %d, want 2", st.caret)
	}
	u.Key(k{Down: true, Key: app.KeyDown, Shift: true})
	if lo, hi := st.selRange(); lo != 2 || hi != 4 {
		t.Errorf("shift+down selected [%d,%d), want [2,4)", lo, hi)
	}
	// Paste keeps its line breaks in an area.
	SetClipboard(func() string { return "x\ny" }, func(string) {})
	defer SetClipboard(nil, nil)
	u.Key(k{Down: true, Ctrl: true, Rune: 'v'})
	if got := st.Text.Get(); got != "abx\ny" {
		t.Errorf("paste over the selection: %q, want %q", got, "abx\ny")
	}
	u.Key(k{Down: true, Key: app.KeyEnter, Ctrl: true})
	if submitted != "abx\ny" {
		t.Errorf("ctrl+enter submitted %q", submitted)
	}
}

// A text area draws its text on several lines: three short lines leave ink
// in three bands down the box.
func TestTextAreaDrawsLines(t *testing.T) {
	const W, H = 220, 120
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatalf("RequestAdapter: %v", err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatalf("RequestDevice: %v", err)
	}
	queue := device.Queue()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	font, err := r.NewFont(goregular.TTF, 16)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer font.Release()
	st := NewInput("one\ntwo\nthree")
	var area *InputEl
	u := New(r, font, func() Element {
		area = TextArea(st, 200, 100).Bg(RGB(0x000000)).TextColor(RGB(0xffffff)).Border(0, RGB(0))
		return Div().P(10).Children(area)
	})
	pix := frame(t, device, queue, target, view, r, u, W, H)
	rect := area.bounds()
	lineH := font.Height()
	for k := 0; k < 3; k++ {
		y0 := int(rect.Y + inputPadY + float32(k)*lineH)
		lit := 0
		for y := y0; y < y0+int(lineH) && y < H; y++ {
			for x := int(rect.X); x < int(rect.X+60); x++ {
				if pix[(y*W+x)*4] > 128 {
					lit++
				}
			}
		}
		if lit == 0 {
			t.Errorf("line %d of the area has no text drawn in it", k+1)
		}
	}
	if len(st.lines) != 3 {
		t.Errorf("laid out %d lines, want 3", len(st.lines))
	}
}
