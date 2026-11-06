//go:build !js

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

func wgpuLib(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return p
	}
	p, err := filepath.Abs(filepath.Join("..", "build", "wgpu", gpu.DefaultLibNames()[0]))
	if err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			return p
		}
	}
	t.Skip("wgpu-native not found; run scripts/fetch-wgpu.sh or set GFX_WGPU_LIB")
	return ""
}

// frame renders one UI frame headlessly and returns the pixels.
func frame(t *testing.T, device *gpu.Device, queue *gpu.Queue, target *gpu.Texture,
	view *gpu.TextureView, r *render.Renderer2D, u *UI, w, h int) []byte {
	t.Helper()
	r.Begin(w, h)
	u.Frame(float32(w), float32(h))
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, w, h)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	return pix
}

// TestCounterInteraction drives a complete UI loop with no window: render,
// synthetic click, property update, re-render, assert the pixels moved —
// DESIGN.md §8.1's "drive the app, assert on pixels/state" as a plain test.
func TestCounterInteraction(t *testing.T) {
	const W, H = 240, 140
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

	var count core.Property[int]
	var button *DivEl
	viewFn := func() Element {
		button = Div().Size(80, 32).Bg(RGB(0x3355ff)).JustifyCenter().ItemsCenter().
			OnClick(func() { count.Set(count.Get() + 1) }).
			Children(Text("+1", RGB(0xffffff)))
		return Div().Col().P(10).Gap(10).Bg(RGB(0x202028)).Children(
			Text(fmt.Sprintf("count: %d", count.Get()), RGB(0xffffff)),
			button,
		)
	}
	u := New(r, font, viewFn)

	before := frame(t, device, queue, target, view, r, u, W, H)

	// The button's layout rect must contain its visual position.
	br := button.Rect()
	if br.W != 80 || br.H != 32 {
		t.Fatalf("button rect = %+v, want 80x32", br)
	}
	bx, by := int(br.X+br.W/2), int(br.Y+br.H/2)
	i := (by*W + bx) * 4
	if before[i+2] < 200 {
		t.Fatalf("button pixel at (%d,%d) not blue: %v", bx, by, before[i:i+4])
	}

	// Click outside: nothing consumed, state unchanged.
	if u.Click(W-2, H-2) {
		t.Fatal("click on empty space should not be consumed")
	}
	if count.Get() != 0 {
		t.Fatal("count changed without a hit")
	}

	// Click the button: consumed, property increments, pixels change.
	if !u.Click(br.X+br.W/2, br.Y+br.H/2) {
		t.Fatal("click on button not consumed")
	}
	if count.Get() != 1 {
		t.Fatalf("count = %d, want 1", count.Get())
	}
	after := frame(t, device, queue, target, view, r, u, W, H)

	diff := 0
	for j := 0; j < len(before); j += 4 {
		if before[j] != after[j] || before[j+1] != after[j+1] || before[j+2] != after[j+2] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("frame identical after state change — label did not repaint")
	}
}

// TestInputTypingFlow drives focus and typing entirely through the public
// event API: press on the field to focus, type, edit, and watch a binding
// downstream of the Text property update.
func TestInputTypingFlow(t *testing.T) {
	in := ui_NewInputForTest("go")
	var greeting = struct{ p core.Property[string] }{}
	greeting.p.Bind(func() string { return "hi " + in.Text.Get() })

	var field *InputEl
	view := func() Element {
		field = Input(in, 200)
		return Div().Col().P(10).Children(field)
	}
	u := New(nil, nil, view)
	// Layout without painting (no GPU needed): build the tree directly.
	u.root = view()
	u.root.layout(&u.ctx, Rect{0, 0, 300, 100})

	fx, fy := field.bounds().X+5, field.bounds().Y+5
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: fx, Y: fy, Button: 1},
		{Kind: app.MouseUp, X: fx, Y: fy, Button: 1},
	})
	if u.ctx.focus != in {
		t.Fatal("press on field did not focus it")
	}

	type k = app.KeyEvent
	// Click positions the caret (fontless test → caret 0); normalize to the
	// end so the edit sequence below is deterministic.
	u.Key(k{Down: true, Key: app.KeyEnd})
	for _, ev := range []k{
		{Down: true, Rune: 'p'},
		{Down: true, Rune: 'h'},
		{Down: true, Key: app.KeyLeft},
		{Down: true, Key: app.KeyLeft},
		{Down: true, Key: app.KeyBackspace},
		{Down: true, Rune: 'o'},
		{Down: true, Key: app.KeyEnd},
		{Down: true, Rune: '!'},
	} {
		if !u.Key(ev) {
			t.Fatalf("key %+v not consumed while focused", ev)
		}
	}
	if got := in.Text.Get(); got != "goph!" {
		t.Fatalf("text = %q, want %q", got, "goph!")
	}
	core.FlushBindings()
	if got := greeting.p.Get(); got != "hi goph!" {
		t.Fatalf("binding = %q", got)
	}

	// Clicking empty space blurs; keys stop being consumed.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 290, Y: 95, Button: 1}})
	if u.ctx.focus != nil {
		t.Fatal("click outside did not blur")
	}
	if u.Key(k{Down: true, Rune: 'x'}) {
		t.Fatal("key consumed with nothing focused")
	}
	if in.Text.Get() != "goph!" {
		t.Fatal("text changed while blurred")
	}
}

func ui_NewInputForTest(s string) *InputState { return NewInput(s) }

// TestClickOnReleaseSemantics: press on a button then release elsewhere
// must not fire; press+release on it must.
func TestClickOnReleaseSemantics(t *testing.T) {
	fired := 0
	var btn *DivEl
	view := func() Element {
		btn = Div().Size(80, 30).Bg(RGB(0x123456)).OnClick(func() { fired++ })
		return Div().P(10).Children(btn)
	}
	u := New(nil, nil, view)
	u.root = view()
	u.root.layout(&u.ctx, Rect{0, 0, 200, 100})

	bx, by := btn.bounds().X+5, btn.bounds().Y+5
	// Press on button, drag off, release: no click.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: bx, Y: by, Button: 1},
		{Kind: app.MouseUp, X: 190, Y: 95, Button: 1},
	})
	if fired != 0 {
		t.Fatal("click fired on release outside the press target")
	}
	// Press and release on the button: click.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: bx, Y: by, Button: 1},
		{Kind: app.MouseUp, X: bx + 2, Y: by + 2, Button: 1},
	})
	if fired != 1 {
		t.Fatalf("fired = %d, want 1", fired)
	}
}

// TestListVirtualization: a million-row list builds only the visible
// window, scrolling shifts that window, and clicking a visible row fires
// that row's handler — proving O(visible), not O(count).
func TestListVirtualization(t *testing.T) {
	const rows = 1_000_000
	var st ListState
	built := 0
	clicked := -1
	view := func() Element {
		built = 0
		return Div().P(0).Children(
			List(&st, 20, rows, func(i int) Element {
				built++
				idx := i
				return Div().H(20).OnClick(func() { clicked = idx })
			}).W(200).H(100),
		)
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.scrolls = u.ctx.scrolls[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 200, 100})
	}

	relayout()
	// 100px viewport / 20px rows = 5 visible, plus overscan — far fewer
	// than a million.
	if built > 12 {
		t.Fatalf("built %d rows for a 100px viewport — not virtualized", built)
	}
	if built < 5 {
		t.Fatalf("built only %d rows, expected the visible window", built)
	}

	// Scroll deep into the list; the built window must move there, not
	// grow.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 20 * 5000}})
	relayout()
	first, n := u.ctx.scrolls[0].(*ListEl).VisibleRange()
	if first < 4000 || n > 12 {
		t.Fatalf("after scroll: first=%d built=%d, want a small window deep in the list", first, n)
	}

	// Click a visible row → its handler (with the right index) fires.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 50, Y: 50, Button: 1},
		{Kind: app.MouseUp, X: 50, Y: 50, Button: 1},
	})
	if clicked < 0 || clicked < first || clicked >= first+n {
		t.Fatalf("clicked row = %d, want one of the visible rows [%d,%d)", clicked, first, first+n)
	}

	// Clamp at the bottom.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 1e9}})
	relayout()
	first, n = u.ctx.scrolls[0].(*ListEl).VisibleRange()
	if first+n != rows {
		t.Fatalf("bottom: last visible row %d, want %d", first+n, rows)
	}
}

// TestOpacityPixels renders a half-opacity white box over black and checks
// the result is ~50% gray, and that opacity multiplies down the subtree.
func TestOpacityPixels(t *testing.T) {
	const W, H = 80, 80
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, _ := inst.RequestAdapter(nil)
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

	// Outer 0.5 opacity, inner white box → 0.5 effective; a nested 0.5
	// box → 0.25.
	u := New(r, nil, func() Element {
		return Div().Opacity(0.5).Children(
			Div().Size(40, 80).Bg(RGBA{R: 1, G: 1, B: 1, A: 1}),
			Div().Opacity(0.5).Children(
				Div().Size(40, 80).Bg(RGBA{R: 1, G: 1, B: 1, A: 1}),
			),
		).Row()
	})

	r.Begin(W, H)
	u.Frame(float32(W), float32(H))
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1}) // black bg
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	red := func(x, y int) int { return int(pix[(y*W+x)*4]) }
	left := red(15, 40)  // 0.5 white over black ≈ 128
	right := red(60, 40) // 0.25 white over black ≈ 64
	if left < 118 || left > 138 {
		t.Errorf("0.5 opacity = %d, want ~128", left)
	}
	if right < 54 || right > 74 {
		t.Errorf("nested 0.25 opacity = %d, want ~64", right)
	}
}

// TestScrollbarDrag: pressing and dragging the scrollbar thumb scrolls,
// independent of the wheel.
func TestScrollbarDrag(t *testing.T) {
	var st ScrollState
	view := func() Element {
		s := Scroll(&st).W(120).H(100)
		s.Children(Div().Size(100, 1000)) // 10x overflow
		return Div().Children(s)
	}
	u := New(nil, nil, view)
	u.ctx.scrolls = u.ctx.scrolls[:0]
	u.root = view()
	u.root.layout(&u.ctx, Rect{0, 0, 200, 100})

	tr := u.ctx.scrolls[0].thumbRect()
	if tr.W == 0 {
		t.Fatal("no thumb for overflowing content")
	}
	// Grab the thumb and drag to the bottom of the track.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: tr.X + 2, Y: tr.Y + 2, Button: 1},
		{Kind: app.MouseMove, X: tr.X + 2, Y: 100},
	})
	if st.Y < st.maxY()-1 {
		t.Fatalf("drag to bottom: Y=%v, want ~maxY %v", st.Y, st.maxY())
	}
	// Release; further moves don't scroll.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: tr.X + 2, Y: 100, Button: 1}})
	atRelease := st.Y
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: tr.X + 2, Y: 0}})
	if st.Y != atRelease {
		t.Fatal("scroll moved after releasing the thumb")
	}
}

// TestTextSelection exercises selection editing on InputState directly:
// shift-extend, replace-selection, select-all, and clipboard cut/paste.
func TestTextSelection(t *testing.T) {
	type k = app.KeyEvent
	s := NewInput("hello world")

	// Caret at end; shift+Home selects the whole line backwards.
	s.Key(k{Down: true, Key: app.KeyHome, Shift: true})
	if lo, hi := s.selRange(); lo != 0 || hi != 11 {
		t.Fatalf("shift+Home selection = [%d,%d), want [0,11)", lo, hi)
	}
	// Typing replaces the selection.
	s.Key(k{Down: true, Rune: 'h'})
	s.Key(k{Down: true, Rune: 'i'})
	if s.Text.Get() != "hi" {
		t.Fatalf("replace-selection = %q, want \"hi\"", s.Text.Get())
	}

	// Select-all then cut to the clipboard.
	s.Key(k{Down: true, Rune: 'a', Ctrl: true})
	if !s.hasSelection() {
		t.Fatal("Ctrl+A did not select")
	}
	s.Key(k{Down: true, Rune: 'x', Ctrl: true})
	if s.Text.Get() != "" || clipboard != "hi" {
		t.Fatalf("cut: text=%q clip=%q", s.Text.Get(), clipboard)
	}
	// Paste twice.
	s.Key(k{Down: true, Rune: 'v', Ctrl: true})
	s.Key(k{Down: true, Rune: 'v', Ctrl: true})
	if s.Text.Get() != "hihi" {
		t.Fatalf("paste = %q, want \"hihi\"", s.Text.Get())
	}

	// Shift+Left extends, then Backspace deletes the selection.
	s.Key(k{Down: true, Key: app.KeyLeft, Shift: true})
	s.Key(k{Down: true, Key: app.KeyLeft, Shift: true})
	s.Key(k{Down: true, Key: app.KeyBackspace})
	if s.Text.Get() != "hi" {
		t.Fatalf("shift-extend + delete = %q, want \"hi\"", s.Text.Get())
	}

	// A plain Left collapses a selection to its left edge.
	s.SelectAll()
	s.Key(k{Down: true, Key: app.KeyLeft})
	if s.hasSelection() || s.caret != 0 {
		t.Fatalf("Left didn't collapse to left edge: sel=%v caret=%d", s.hasSelection(), s.caret)
	}
}

// TestDragAndDrop: pressing a Draggable, moving past the threshold, and
// releasing over a DropZone fires OnDrop with the payload; releasing
// elsewhere, or a payload the zone rejects, does not.
func TestDragAndDrop(t *testing.T) {
	var dropped any
	view := func() Element {
		src := Draggable(Div().Size(40, 40), "apple")
		zone := DropZone(Div().Size(80, 80), func(p any) { dropped = p })
		return Div().Row().Gap(100).Children(src, zone)
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.dropZones = u.ctx.dropZones[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 300, 100})
	}
	relayout()

	// Source is at x≈0..40; the gap-100 puts the zone at x≈140..220.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 20, Y: 20, Button: 1},
		{Kind: app.MouseMove, X: 40, Y: 20}, // past threshold → dragging
		{Kind: app.MouseMove, X: 170, Y: 40},
		{Kind: app.MouseUp, X: 170, Y: 40, Button: 1},
	})
	if dropped != "apple" {
		t.Fatalf("dropped = %v, want \"apple\"", dropped)
	}

	// Drop outside any zone: no callback.
	dropped = nil
	relayout()
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 20, Y: 20, Button: 1},
		{Kind: app.MouseMove, X: 40, Y: 20},
		{Kind: app.MouseUp, X: 280, Y: 90, Button: 1},
	})
	if dropped != nil {
		t.Fatalf("drop outside any zone fired: %v", dropped)
	}

	// A mere click (no movement past threshold) is not a drag.
	dropped = nil
	relayout()
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 20, Y: 20, Button: 1},
		{Kind: app.MouseUp, X: 20, Y: 20, Button: 1},
	})
	if u.dragging {
		t.Fatal("a click without movement should not start a drag")
	}
}

// TestFocusTabAndActivate: Tab cycles focus through focusables in order,
// Enter activates the focused button, and click focuses directly.
func TestFocusTabAndActivate(t *testing.T) {
	fired := [3]int{}
	field := NewInput("")
	view := func() Element {
		return Div().Col().Children(
			Div().Size(80, 30).Focusable().OnClick(func() { fired[0]++ }),
			Input(field, 120),
			Div().Size(80, 30).Focusable().OnClick(func() { fired[2]++ }),
		)
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.focusables = u.ctx.focusables[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 200, 200})
	}
	relayout()

	if u.focusedEl() != nil {
		t.Fatal("nothing should be focused initially")
	}
	// Tab → first button, Tab → input, Tab → second button, Tab → wrap.
	type k = app.KeyEvent
	u.Key(k{Down: true, Key: app.KeyTab})
	if u.focusIdx != 0 {
		t.Fatalf("after 1 tab focusIdx=%d, want 0", u.focusIdx)
	}
	// Enter activates the focused first button.
	u.Key(k{Down: true, Key: app.KeyEnter})
	if fired[0] != 1 {
		t.Fatalf("Enter didn't activate button 0 (fired %v)", fired)
	}
	u.Key(k{Down: true, Key: app.KeyTab}) // → input
	if _, ok := u.focusedEl().(*InputEl); !ok {
		t.Fatal("second tab should focus the input")
	}
	// Typing goes to the focused input, not button activation.
	u.Key(k{Down: true, Rune: 'h'})
	u.Key(k{Down: true, Rune: 'i'})
	if field.Text.Get() != "hi" {
		t.Fatalf("typing to focused input = %q", field.Text.Get())
	}
	u.Key(k{Down: true, Key: app.KeyTab}) // → button 2
	u.Key(k{Down: true, Rune: ' '})       // Space activates
	if fired[2] != 1 {
		t.Fatalf("Space didn't activate button 2 (fired %v)", fired)
	}
	u.Key(k{Down: true, Key: app.KeyTab}) // wrap → button 0
	if u.focusIdx != 0 {
		t.Fatalf("tab didn't wrap: focusIdx=%d", u.focusIdx)
	}
}

// TestOverlayZOrderAndHit: an overlay paints on top and is hit-tested
// before the main tree, even when a main-tree button sits under it.
func TestOverlayZOrderAndHit(t *testing.T) {
	mainClicked, overlayClicked := 0, 0
	view := func() Element {
		base := Div().Size(200, 200).OnClick(func() { mainClicked++ })
		pop := Div().Size(80, 40).OnClick(func() { overlayClicked++ })
		return Div().Children(base, Overlay(pop, 50, 50))
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.overlays = u.ctx.overlays[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 200, 200})
	}
	relayout()

	// The overlay registered itself.
	if len(u.ctx.overlays) != 1 {
		t.Fatalf("overlays = %d, want 1", len(u.ctx.overlays))
	}
	// A click at (70,70) lands on the overlay (which covers 50..130), not
	// the base behind it.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 70, Y: 70, Button: 1},
		{Kind: app.MouseUp, X: 70, Y: 70, Button: 1},
	})
	if overlayClicked != 1 || mainClicked != 0 {
		t.Fatalf("overlay=%d main=%d, want overlay on top", overlayClicked, mainClicked)
	}
	// A click outside the overlay hits the base.
	relayout()
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 160, Y: 160, Button: 1},
		{Kind: app.MouseUp, X: 160, Y: 160, Button: 1},
	})
	if mainClicked != 1 {
		t.Fatalf("base click main=%d, want 1", mainClicked)
	}
}

// TestScrollWheelClampAndOffset drives the wheel through UI.Mouse and
// checks offset clamping plus content displacement — no GPU needed.
func TestScrollWheelClampAndOffset(t *testing.T) {
	var st ScrollState
	var inner *DivEl
	view := func() Element {
		inner = Div().Size(100, 500) // taller than the 100px viewport
		return Div().P(10).Children(Scroll(&st).W(120).H(100).Children(inner))
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.scrolls = u.ctx.scrolls[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 200, 200})
	}
	relayout()

	y0 := inner.Rect().Y
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 120}})
	if st.Y != 120 {
		t.Fatalf("offset = %v, want 120", st.Y)
	}
	relayout()
	if got := inner.Rect().Y; got != y0-120 {
		t.Fatalf("content y = %v, want %v", got, y0-120)
	}

	// Clamp at the bottom: content 500, viewport 100 -> max 400.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 10000}})
	if st.Y != 400 {
		t.Fatalf("offset = %v, want clamped 400", st.Y)
	}
	// And at the top.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: -10000}})
	if st.Y != 0 {
		t.Fatalf("offset = %v, want 0", st.Y)
	}
	// Wheel outside the scroll area does nothing.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 190, Y: 190, ScrollY: 50}})
	if st.Y != 0 {
		t.Fatal("wheel outside viewport scrolled it")
	}
}

// TestScrollHorizontal verifies 2-way scroll: content wider than the viewport
// pans with ScrollX (clamped), shifting the content rect left.
func TestScrollHorizontal(t *testing.T) {
	var st ScrollState
	var inner *DivEl
	view := func() Element {
		inner = Div().Size(600, 50) // wider than the 120px viewport
		return Div().P(10).Children(Scroll(&st).W(120).H(100).Children(inner))
	}
	u := New(nil, nil, view)
	relayout := func() {
		u.ctx.scrolls = u.ctx.scrolls[:0]
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 200, 200})
	}
	relayout()

	x0 := inner.Rect().X
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollX: 80}})
	if st.X != 80 {
		t.Fatalf("offset = %v, want 80", st.X)
	}
	relayout()
	if got := inner.Rect().X; got != x0-80 {
		t.Fatalf("content x = %v, want %v", got, x0-80)
	}
	// Clamp at the right edge: content 600, viewport 120 -> max 480.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollX: 10000}})
	if st.X != 480 {
		t.Fatalf("offset = %v, want clamped 480", st.X)
	}
	// A diagonal event moves both axes in one dispatch.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollX: -10000, ScrollY: 30}})
	if st.X != 0 || st.Y != 0 {
		t.Fatalf("offsets = (%v,%v), want (0,0) — X clamped, Y content fits", st.X, st.Y)
	}
}

// TestSliderDrag presses, drags and releases through UI.Mouse.
func TestSliderDrag(t *testing.T) {
	var v core.Property[float32]
	var sl *SliderEl
	view := func() Element {
		sl = Slider(&v, 200+2*sliderKnob) // track is exactly 200px
		return Div().Children(sl)
	}
	u := New(nil, nil, view)
	u.root = view()
	u.root.layout(&u.ctx, Rect{0, 0, 300, 50})

	r := sl.bounds()
	trackX := r.X + sliderKnob
	cy := r.Y + r.H/2
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: trackX + 50, Y: cy, Button: 1}})
	if got := v.Get(); got != 0.25 {
		t.Fatalf("press value = %v, want 0.25", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: trackX + 150, Y: cy}})
	if got := v.Get(); got != 0.75 {
		t.Fatalf("drag value = %v, want 0.75", got)
	}
	// Dragging past the end clamps.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: trackX + 999, Y: cy}})
	if got := v.Get(); got != 1 {
		t.Fatalf("clamped value = %v, want 1", got)
	}
	// After release, moves stop affecting the value.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseUp, X: trackX + 999, Y: cy, Button: 1},
		{Kind: app.MouseMove, X: trackX, Y: cy},
	})
	if got := v.Get(); got != 1 {
		t.Fatalf("value moved after release: %v", got)
	}
}

// TestCheckboxToggle clicks the row through the public event path.
func TestCheckboxToggle(t *testing.T) {
	var checked core.Property[bool]
	view := func() Element {
		return Div().P(10).Children(Checkbox(&checked, "Dark mode"))
	}
	u := New(nil, nil, view)
	click := func() {
		u.root = view()
		u.root.layout(&u.ctx, Rect{0, 0, 300, 100})
		u.Mouse([]app.MouseEvent{
			{Kind: app.MouseDown, X: 20, Y: 20, Button: 1},
			{Kind: app.MouseUp, X: 20, Y: 20, Button: 1},
		})
	}
	click()
	if !checked.Get() {
		t.Fatal("first click did not check")
	}
	click()
	if checked.Get() {
		t.Fatal("second click did not uncheck")
	}
}

// TestLayoutRowGrowJustify checks the flexbox-lite math without a GPU.
func TestLayoutRowGrowJustify(t *testing.T) {
	ctx := &Context{}

	var left, right, growing *DivEl
	left = Div().Size(50, 40)
	growing = Div().Grow().H(40)
	right = Div().Size(30, 40)
	root := Div().Row().P(10).Gap(5).Children(left, growing, right)
	root.layout(ctx, Rect{0, 0, 300, 100})

	if got := left.Rect(); got.X != 10 || got.W != 50 {
		t.Errorf("left = %+v", got)
	}
	// inner width 280, fixed 80, gaps 10 -> grow gets 190.
	if got := growing.Rect(); got.X != 65 || got.W != 190 {
		t.Errorf("growing = %+v", got)
	}
	if got := right.Rect(); got.X != 260 || got.W != 30 {
		t.Errorf("right = %+v", got)
	}

	// Centering without grow.
	a := Div().Size(40, 20)
	c := Div().Row().JustifyCenter().ItemsCenter().Children(a)
	c.layout(ctx, Rect{0, 0, 200, 100})
	if got := a.Rect(); got.X != 80 || got.Y != 40 {
		t.Errorf("centered = %+v, want X=80 Y=40", got)
	}
}
