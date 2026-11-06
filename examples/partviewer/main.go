//go:build linux || windows || js

// Partviewer is the part-studio style demo: a full-window 3D viewport (parametric
// machined part, orbit camera) with the 2D UI floating over it — a left tool
// rail, a right properties panel whose sliders REGENERATE the part live, a
// CAM section that overlays a serpentine pocket toolpath, and a status bar.
// It is the first example compositing Renderer3D + Renderer2D/ui in one
// frame (3D clear pass, then a UI pass that loads instead of clearing).
//
//	go run ./partviewer
//	go run ./partviewer -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime/pprof"
	"runtime/trace"

	"github.com/chewxy/math32"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// ---- parametric part -------------------------------------------------------

// part holds the model parameters the properties panel edits. Meshes are unit
// primitives; regeneration is pure matrix math, so edits are instant.
type part struct {
	Width    core.Property[float32] // plate X extent
	Depth    core.Property[float32] // plate Z extent
	Boss     core.Property[float32] // corner boss radius
	Pocket   core.Property[float32] // center pocket inset 0..1
	Path     core.Property[bool]    // show CAM toolpath
	Selected core.Property[int]     // picked component tag; -1 = none
}

// Pick tags (SetPickTag) — what a viewport click resolves to.
const (
	pickPlate = iota
	pickPocket
	pickBoss
	pickBolt
)

var pickNames = []string{"base plate", "pocket", "boss", "bolt head"}

func newPart() *part {
	p := &part{}
	p.Selected.Set(-1)
	p.Width.Set(6)
	p.Depth.Set(4)
	p.Boss.Set(0.45)
	p.Pocket.Set(0.55)
	return p
}

var (
	alu    = render.RGBA{R: 0.72, G: 0.75, B: 0.80, A: 1}
	aluCut = render.RGBA{R: 0.52, G: 0.56, B: 0.63, A: 1}
	brass  = render.RGBA{R: 0.85, G: 0.68, B: 0.30, A: 1}
	toolC  = render.RGBA{R: 0.20, G: 0.85, B: 0.55, A: 1}
)

// draw renders the part: base plate, center pocket (an inset darker slab),
// four corner bosses, and — when enabled — the CAM serpentine over the pocket.
func (p *part) draw(r *render.Renderer3D, cube, cyl *render.Mesh) {
	w, d := p.Width.Get(), p.Depth.Get()
	const plateH = 0.6
	sel := p.Selected.Get()
	hl := func(tag int, c render.RGBA) render.RGBA {
		if tag != sel {
			return c
		}
		// Selection tint: pull toward the accent blue.
		return render.RGBA{R: c.R*0.45 + 0.18, G: c.G*0.45 + 0.35, B: c.B*0.45 + 0.55, A: c.A}
	}

	// Ground reference grid slab (untagged: clicking it clears selection).
	r.Draw(cube, mat(core.V3(0, -1.3, 0), core.V3(24, 0.05, 24)), render.RGBA{R: 0.16, G: 0.17, B: 0.20, A: 1})

	// Base plate.
	r.SetPickTag(pickPlate)
	r.DrawPBR(cube, mat(core.V3(0, 0, 0), core.V3(w, plateH, d)), hl(pickPlate, alu), 0.85, 0.35, nil)

	// Center pocket: an inset, darker slab sunk into the top face.
	pw, pd := w*p.Pocket.Get(), d*p.Pocket.Get()
	r.SetPickTag(pickPocket)
	r.DrawPBR(cube, mat(core.V3(0, plateH*0.20, 0), core.V3(pw, plateH*0.7, pd)), hl(pickPocket, aluCut), 0.8, 0.5, nil)

	// Corner bosses (cylinders) with brass bolt heads.
	br := p.Boss.Get()
	bx, bz := w/2-br-0.25, d/2-br-0.25
	for _, c := range [][2]float32{{-bx, -bz}, {bx, -bz}, {-bx, bz}, {bx, bz}} {
		r.SetPickTag(pickBoss)
		r.DrawPBR(cyl, mat(core.V3(c[0], plateH/2+0.25, c[1]), core.V3(br, 0.5, br)), hl(pickBoss, alu), 0.9, 0.3, nil)
		r.SetPickTag(pickBolt)
		r.DrawPBR(cyl, mat(core.V3(c[0], plateH/2+0.56, c[1]), core.V3(br*0.55, 0.12, br*0.55)), hl(pickBolt, brass), 1, 0.4, nil)
	}
	r.SetPickTag(-1)

	// CAM: serpentine pocket toolpath as thin segments just above the floor.
	if p.Path.Get() {
		y := float32(plateH*0.20 + plateH*0.35 + 0.03)
		step := float32(0.28)
		hw, hd := pw/2-0.15, pd/2-0.15
		n := int(2 * hd / step)
		for i := 0; i <= n; i++ {
			z := -hd + float32(i)*step
			if z > hd {
				z = hd
			}
			// Horizontal stroke.
			r.Draw(cube, mat(core.V3(0, y, z), core.V3(2*hw, 0.03, 0.05)), toolC)
			// Link to the next lane, alternating ends.
			if i < n {
				x := hw
				if i%2 == 1 {
					x = -hw
				}
				zn := math32.Min(z+step, hd)
				r.Draw(cube, mat(core.V3(x, y, (z+zn)/2), core.V3(0.05, 0.03, zn-z)), toolC)
			}
		}
	}
}

func mat(pos, scale core.Vec3) core.Mat4 {
	return core.Mat4Translate(pos).Mul(core.Mat4Scale(scale))
}

// cylinderMesh builds a unit cylinder (radius 1, height 1, centered) — the
// primitive the part's bosses scale.
func cylinderMesh(r *render.Renderer3D) *render.Mesh {
	const segs = 32
	var verts []render.Vertex3D
	var idx []uint32
	// Side wall.
	for i := 0; i <= segs; i++ {
		a := float32(i) / segs * 2 * math32.Pi
		s, c := math32.Sincos(a)
		verts = append(verts,
			render.Vertex3D{Pos: core.V3(c, -0.5, s), Normal: core.V3(c, 0, s)},
			render.Vertex3D{Pos: core.V3(c, 0.5, s), Normal: core.V3(c, 0, s)})
	}
	for i := 0; i < segs; i++ {
		b := uint32(i * 2)
		idx = append(idx, b, b+1, b+2, b+2, b+1, b+3)
	}
	// Caps.
	topC := uint32(len(verts))
	verts = append(verts,
		render.Vertex3D{Pos: core.V3(0, 0.5, 0), Normal: core.V3(0, 1, 0)},
		render.Vertex3D{Pos: core.V3(0, -0.5, 0), Normal: core.V3(0, -1, 0)})
	ring := uint32(len(verts))
	for i := 0; i <= segs; i++ {
		a := float32(i) / segs * 2 * math32.Pi
		s, c := math32.Sincos(a)
		verts = append(verts,
			render.Vertex3D{Pos: core.V3(c, 0.5, s), Normal: core.V3(0, 1, 0)},
			render.Vertex3D{Pos: core.V3(c, -0.5, s), Normal: core.V3(0, -1, 0)})
	}
	for i := 0; i < segs; i++ {
		t := ring + uint32(i*2)
		idx = append(idx, topC, t+2, t)     // top cap
		idx = append(idx, topC+1, t+1, t+3) // bottom cap
	}
	return r.NewMesh(verts, idx)
}

// ---- Shapr3D-style chrome ---------------------------------------------------

// chrome builds the floating UI over the viewport and reports the panel
// rects so the orbit camera ignores pointer activity above them.
type chrome struct {
	p        *part
	tool     core.Property[int] // selected tool on the rail
	winW     float32
	winH     float32
	deadRect []ui.Rect // panel areas that swallow camera input
}

var toolNames = []string{"Select", "Sketch", "Extrude", "Revolve", "Fillet", "CAM"}
var toolGlyphs = []string{"Sel", "Skt", "Ext", "Rev", "Fil", "CAM"}

func (c *chrome) view() ui.Element {
	c.deadRect = c.deadRect[:0]
	dark := ui.RGBA{R: 0.10, G: 0.11, B: 0.13, A: 0.92}
	line := ui.RGBA{R: 1, G: 1, B: 1, A: 0.08}
	fg := ui.RGB(0xe8eaf0)
	dim := ui.RGB(0x9aa0ac)
	accent := ui.RGB(0x4c8dff)

	// Title chip.
	title := ui.Div().Row().ItemsCenter().Gap(8).P(10).Bg(dark).Rounded(10).
		Border(1, line).Children(
		ui.Text("◆", accent).SizePx(14),
		ui.Text("Part Studio — bracket_v2", fg).SizePx(14),
	)

	// Left tool rail.
	rail := ui.Div().Col().Gap(4).P(6).Bg(dark).Rounded(12).Border(1, line)
	for i := range toolNames {
		i := i
		cell := ui.Div().Size(44, 44).JustifyCenter().ItemsCenter().Rounded(9).
			HoverBg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.08}).
			Children(ui.Text(toolGlyphs[i], fg).SizePx(12))
		if c.tool.Get() == i {
			cell.Bg(ui.RGBA{R: 0.30, G: 0.55, B: 1, A: 0.30}).Border(1, accent)
		}
		cell.OnClick(func() { c.tool.Set(i) })
		rail.Children(cell)
	}

	// Right properties panel: parametric dimensions + CAM.
	row := func(label string, p *core.Property[float32], width float32) ui.Element {
		return ui.Div().Col().Gap(4).Children(
			ui.Div().Row().Children(
				ui.Text(label, dim).SizePx(12),
				ui.Div().Grow(),
				ui.Text(fmt.Sprintf("%.2f", p.Get()), fg).SizePx(12)),
			ui.Slider(p, width),
		)
	}
	sep := func() ui.Element { return ui.Div().H(1).Bg(line) }
	props := ui.Div().Col().Gap(12).P(14).W(260).Bg(dark).Rounded(12).Border(1, line).Children(
		ui.Text(toolNames[c.tool.Get()]+" — Properties", fg).SizePx(14).Bold(),
		sep(),
		row("Width", &c.p.Width, 220),
		row("Depth", &c.p.Depth, 220),
		row("Boss radius", &c.p.Boss, 220),
		row("Pocket", &c.p.Pocket, 220),
		sep(),
		ui.Text("CAM — Pocket 2.5D", fg).SizePx(13).Bold(),
		ui.Div().Row().ItemsCenter().Gap(10).Children(
			ui.Checkbox(&c.p.Path, "Show toolpath"),
		),
		ui.Div().Row().Gap(10).Children(
			ui.Div().Col().Children(ui.Text("Feed", dim).SizePx(11), ui.Text("1200 mm/min", fg).SizePx(12)),
			ui.Div().Col().Children(ui.Text("Speed", dim).SizePx(11), ui.Text("9000 rpm", fg).SizePx(12)),
			ui.Div().Col().Children(ui.Text("Tool", dim).SizePx(11), ui.Text("D6 flat", fg).SizePx(12)),
		),
	)

	// Status bar.
	status := ui.Div().Row().ItemsCenter().Gap(14).P(8).Bg(dark).Rounded(10).Border(1, line).Children(
		ui.Text("● solid", toolC2()).SizePx(12),
		ui.Text(fmt.Sprintf("plate %.1f × %.1f", c.p.Width.Get(), c.p.Depth.Get()), dim).SizePx(12),
		ui.Text(selLabel(c.p.Selected.Get()), accent).SizePx(12),
		ui.Text("drag: orbit   wheel: zoom   click: pick", dim).SizePx(12),
	)

	// Fixed placements (tracked as camera dead-zones).
	railR := ui.Rect{X: 16, Y: 76, W: 56, H: float32(len(toolNames))*48 + 12}
	titleR := ui.Rect{X: 16, Y: 16, W: 300, H: 40}
	propsR := ui.Rect{X: c.winW - 276, Y: 16, W: 260, H: 430}
	statusR := ui.Rect{X: 16, Y: c.winH - 52, W: 420, H: 36}
	c.deadRect = append(c.deadRect, railR, titleR, propsR, statusR)

	return ui.Div().Children(
		ui.Overlay(title, titleR.X, titleR.Y),
		ui.Overlay(rail, railR.X, railR.Y),
		ui.Overlay(props, propsR.X, propsR.Y),
		ui.Overlay(status, statusR.X, statusR.Y),
	)
}

func toolC2() ui.RGBA { return ui.RGBA{R: 0.2, G: 0.85, B: 0.55, A: 1} }

func selLabel(tag int) string {
	if tag < 0 || tag >= len(pickNames) {
		return "nothing selected"
	}
	return "selected: " + pickNames[tag]
}

// overPanel reports whether a point is above any floating panel.
func (c *chrome) overPanel(x, y float32) bool {
	for _, r := range c.deadRect {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return true
		}
	}
	return false
}

// ---- frame loop --------------------------------------------------------------

var lightDir = core.V3(-0.55, -1, -0.35)

func fontBytes() []byte {
	for _, p := range []string{
		"/usr/share/fonts/truetype/ubuntu/Ubuntu-R.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return goregular.TTF
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile to this file")
	traceFile := flag.String("trace", "", "write a runtime/trace to this file")
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "partviewer:", err)
			os.Exit(1)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if *traceFile != "" {
		f, err := os.Create(*traceFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "partviewer:", err)
			os.Exit(1)
		}
		defer f.Close()
		trace.Start(f)
		defer trace.Stop()
	}
	var err error
	if *shot != "" {
		err = screenshot(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "partviewer:", err)
		os.Exit(1)
	}
}

func newCamera() *orbit.Camera {
	return &orbit.Camera{Target: core.V3(0, 0, 0), Distance: 11, Yaw: 0.7, Pitch: 0.5}
}

// frame renders one composited frame: 3D pass (clears), then UI pass (loads).
func frame(dev *gpu.Device, q *gpu.Queue, r3 *render.Renderer3D, r2 *render.Renderer2D,
	u *ui.UI, p *part, cube, cyl *render.Mesh, cam *orbit.Camera,
	view, depthView *gpu.TextureView, w, h int) error {
	vp, eye := cam.ViewProj(w, h)
	r3.Begin(vp, lightDir, eye)
	p.draw(r3, cube, cyl)

	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, depthView, gpu.Color{R: 0.075, G: 0.08, B: 0.10, A: 1}); err != nil {
		return err
	}
	r2.Begin(w, h)
	u.Frame(float32(w), float32(h))
	pass := enc.BeginRenderPassLoad(view)
	if err := r2.Flush(pass); err != nil {
		return err
	}
	pass.End()
	q.Submit(enc.Finish())
	return nil
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "gfx: part viewer", Width: 1100, Height: 720})
	if err != nil {
		return err
	}
	defer win.Destroy()
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	format := surface.PreferredFormat(adapter)
	w, h := win.Size()
	surface.Configure(device, w, h, format)

	r3, err := render.New3D(device, queue, format)
	if err != nil {
		return err
	}
	r3.SetShadowArea(core.V3(0, 0, 0), 8)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}

	p := newPart()
	c := &chrome{p: p}
	u := ui.New(r2, font, c.view)
	cube, cyl := r3.CubeMesh(), cylinderMesh(r3)
	cam := newCamera()

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()
	rendered := 0
	var pickDown bool
	var pickX, pickY float32
	win.EachFrame(func() bool {
		c.winW, c.winH = float32(w), float32(h)
		// UI gets every event; the camera only gets those not over a panel
		// (and no drags that STARTED over a panel).
		events := win.MouseEvents()
		u.Mouse(events)
		// Click-to-pick: a press+release with little travel, not over a
		// panel, ray-picks the LAST rendered frame's draw list (still valid
		// until the next r3.Begin) with the camera as it was displayed.
		for _, m := range events {
			switch m.Kind {
			case app.MouseDown:
				if m.Button == 1 && !c.overPanel(m.X, m.Y) {
					pickDown, pickX, pickY = true, m.X, m.Y
				}
			case app.MouseUp:
				dx, dy := m.X-pickX, m.Y-pickY
				if pickDown && m.Button == 1 && dx*dx+dy*dy < 36 && !c.overPanel(m.X, m.Y) {
					vp, _ := cam.ViewProj(w, h)
					if o, d, ok := render.ScreenRay(vp, m.X, m.Y, float32(w), float32(h)); ok {
						if hit, ok := r3.Pick(o, d); ok {
							p.Selected.Set(hit.Tag) // -1 (ground) clears
						} else {
							p.Selected.Set(-1)
						}
					}
				}
				pickDown = false
			}
		}
		var camEvents []app.MouseEvent
		for _, m := range events {
			if m.Kind == app.MouseDown && c.overPanel(m.X, m.Y) {
				continue
			}
			if c.overPanel(m.X, m.Y) && m.Kind == app.MouseScroll {
				continue
			}
			camEvents = append(camEvents, m)
		}
		cam.Handle(camEvents)

		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
			depthView.Release()
			depth.Release()
			depth = device.CreateDepthTexture(w, h)
			depthView = depth.CreateView()
		}
		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		err := frame(device, queue, r3, r2, u, p, cube, cyl, cam, view, depthView, w, h)
		view.Release()
		if err != nil {
			tex.Release()
			fmt.Fprintln(os.Stderr, "partviewer:", err)
			return false
		}
		// Present BEFORE releasing the swapchain texture — releasing
		// first discards the acquired frame and the present silently fails
		// (on DX12 every subsequent acquire then blocks out wgpu's 1s
		// frame timeout: ~1fps and a ghosted "Not Responding" window).
		surface.Present()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	return nil
}

// screenshot renders one composited frame fully headlessly.
func screenshot(path string) error {
	const W, H = 1100, 720
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

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := device.CreateDepthTexture(W, H)
	depthView := depth.CreateView()

	r3, err := render.New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	r3.SetShadowArea(core.V3(0, 0, 0), 8)
	r2, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}
	p := newPart()
	p.Path.Set(true) // show the CAM toolpath in the capture
	c := &chrome{p: p, winW: W, winH: H}
	u := ui.New(r2, font, c.view)
	cube, cyl := r3.CubeMesh(), cylinderMesh(r3)

	if err := frame(device, queue, r3, r2, u, p, cube, cyl, newCamera(), view, depthView, W, H); err != nil {
		return err
	}
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		return err
	}
	img := &image.RGBA{Pix: pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
