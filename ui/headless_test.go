//go:build !js

package ui

import (
	"fmt"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// TestHeadlessHostDrivesApp is DESIGN.md §8.1 end to end: an app.Headless
// host + offscreen gpu rendering, driven by injected input and stepped one
// deterministic frame at a time — click a button, type into a field, and
// verify both state and pixels. This is the loop CI and AI agents use.
func TestHeadlessHostDrivesApp(t *testing.T) {
	const W, H = 320, 200
	host := app.NewHeadless(app.Options{Width: W, Height: H})
	defer host.Destroy()

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

	// The app under test: a counter button and a name field.
	var count core.Property[int]
	name := NewInput("")
	var btn *DivEl
	var field *InputEl
	u := New(r, font, func() Element {
		btn = Div().Size(90, 36).Bg(RGB(0x3b5bdb)).Rounded(8).
			JustifyCenter().ItemsCenter().
			OnClick(func() { count.Set(count.Get() + 1) }).
			Children(Text("count++", RGB(0xffffff)))
		field = Input(name, 160)
		return Div().Col().P(16).Gap(12).Bg(RGB(0x16161e)).Children(
			Text(fmt.Sprintf("count: %d", count.Get()), RGB(0xe8e8f0)),
			btn,
			field,
		)
	})

	// step = the standard frame: drain host input into the UI, render,
	// return the pixels.
	step := func() []byte {
		var pix []byte
		host.Step(func() bool {
			u.Mouse(host.MouseEvents())
			for _, k := range host.KeyEvents() {
				u.Key(k)
			}
			w, h := host.Size()
			r.Begin(w, h)
			u.Frame(float32(w), float32(h))
			enc := device.CreateCommandEncoder()
			pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
			if err := r.Flush(pass); err != nil {
				t.Fatal(err)
			}
			pass.End()
			queue.Submit(enc.Finish())
			var err error
			pix, err = device.ReadTexture(queue, target, W, H)
			if err != nil {
				t.Fatal(err)
			}
			return true
		})
		return pix
	}

	step() // initial frame lays out the tree

	// Click the button via injected input.
	b := btn.Rect()
	host.Click(b.X+b.W/2, b.Y+b.H/2)
	step()
	if count.Get() != 1 {
		t.Fatalf("count = %d after injected click, want 1", count.Get())
	}

	// Focus the field and type.
	f := field.bounds()
	host.Click(f.X+5, f.Y+5)
	step()
	host.Type("hi")
	pix := step()
	if got := name.Text.Get(); got != "hi" {
		t.Fatalf("typed text = %q, want %q", got, "hi")
	}

	// The button's blue pixels are where layout says they are. Sample at
	// the vertical center near the left edge — flat fill, no label text.
	i := (int(b.Y+b.H/2)*W + int(b.X+12)) * 4
	if pix[i+2] < 200 {
		t.Fatalf("button pixel not blue: %v", pix[i:i+4])
	}

	// Closing the host ends the loop.
	host.Close()
	if host.Poll() {
		t.Fatal("Poll should be false after Close")
	}
	if host.Step(func() bool { return true }) {
		t.Fatal("Step should refuse after Close")
	}
}
