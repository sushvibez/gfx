//go:build linux || windows

// fps is the first-person walkthrough: the physics/p3 kinematic character
// (MoveAndSlide) walking a small level — corridor walls, a staircase, a 30°
// ramp, a moving platform to ride, and a pressure-plate trigger that slides
// a door open. The seed of a
// first-person game's movement.
//
// Controls: WASD move, drag mouse to look, Space jump, Shift walk slow.
// Gamepad: left stick move, right stick look, A jump.
//
//	cd examples && go run ./fps
//	go run ./fps -screenshot out.png   # scripted walk, then a frame
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/chewxy/math32"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/audio"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/physics/p3"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/script/gml"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

var lightDir = core.V3(-0.45, -1, -0.3)

// ---- level ------------------------------------------------------------------

// level wraps the gml scene document: solids + physics come from level.gml;
// the door and platform are driven through bound host properties.
type level struct {
	scene    *gml.SceneView
	doorOpen core.Property[float32]
	platX    core.Property[float32]
	plate    p3.BodyID
	doorUp   float32
	t        float32
}

func levelPath() (string, error) {
	for _, p := range []string{"fps/level.gml", "level.gml", "examples/fps/level.gml"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("level.gml not found")
}

func buildLevel() (*level, error) {
	path, err := levelPath()
	if err != nil {
		return nil, err
	}
	l := &level{}
	l.platX.Set(2.2)
	env := gml.NewEnv()
	// Host properties the document binds against (declared BEFORE load).
	env.Float("doorOpen", &l.doorOpen)
	env.Float("platX", &l.platX)
	l.scene, err = gml.LoadSceneFile(path, env)
	if err != nil {
		return nil, err
	}
	plate, ok := l.scene.Body("plate")
	if !ok {
		return nil, fmt.Errorf("level.gml has no plate trigger")
	}
	l.plate = plate
	return l, nil
}

func (l *level) world() *p3.World { return l.scene.World() }

// step animates the movers by setting the bound properties — the scene
// rebuild re-poses the physics bodies and the draws together.
func (l *level) step(dt float32, doorOpen bool) {
	l.t += dt
	l.scene.Advance(dt) // animated MeshInstances
	l.platX.Set(2.2 + 1.2*math32.Sin(l.t*0.8))
	target := float32(0)
	if doorOpen {
		target = 1
	}
	speed := dt * 1.5
	if l.doorUp < target {
		l.doorUp = math32.Min(l.doorUp+speed, 1)
	} else if l.doorUp > target {
		l.doorUp = math32.Max(l.doorUp-speed, 0)
	}
	l.doorOpen.Set(l.doorUp)
}

func (l *level) draw(r3 *render.Renderer3D) error {
	f, err := l.scene.Frame()
	if err != nil {
		return err
	}
	f.Draw(r3)
	return nil
}

// ---- player -----------------------------------------------------------------

type player struct {
	char       *p3.Character
	yaw, pitch float32
	vel        core.Vec3
	triggers   p3.TriggerEvents
	inZone     bool
}

func newPlayer(w *p3.World) *player {
	p := &player{char: p3.NewCharacter(w, core.V3(0, 0.9, 0), 0.35, 0.55)}
	p.yaw = 0 // facing +Z into the level
	return p
}

// update applies one frame of input → movement.
func (p *player) update(dt float32, move core.Vec3, look [2]float32, jump bool) {
	const (
		speed    = 4.0
		jumpVel  = 5.0
		gravity  = -12.0
		lookRate = 2.2
	)
	p.yaw += look[0] * lookRate
	p.pitch = clamp(p.pitch+look[1]*lookRate, -1.4, 1.4)

	// Move vector in facing space (yaw only).
	sin, cos := math32.Sincos(p.yaw)
	fwd := core.V3(sin, 0, cos)
	right := core.V3(cos, 0, -sin)
	vel := fwd.Scale(move.Z * speed).Add(right.Scale(move.X * speed))
	vel.Y = p.vel.Y + gravity*dt
	if p.char.OnGround {
		vel.Y = gravity * dt
		if jump {
			vel.Y = jumpVel
		}
	}
	p.vel = p.char.MoveAndSlide(vel, dt)

	p.triggers.Update(p.char, p3.LayerAll, func(p3.BodyID) { p.inZone = true }, func(p3.BodyID) { p.inZone = false })
}

// viewProj builds the first-person camera at eye height (capsule centre
// sits at 0.9, so +0.30 puts the eye at 1.2m — deliberately below a real
// 1.6m eye line; the level reads better slightly low).
func (p *player) viewProj(w, h int) (core.Mat4, core.Vec3) {
	eye := p.char.Pos.Add(core.V3(0, 0.30, 0))
	sy, cy := math32.Sincos(p.yaw)
	sp, cp := math32.Sincos(p.pitch)
	fwd := core.V3(sy*cp, sp, cy*cp)
	view := core.Mat4LookAt(eye, eye.Add(fwd), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(1.1, float32(w)/float32(h), 0.05, 200)
	return proj.Mul(view), eye
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- audio ------------------------------------------------------------------

// sounds are synthesized feedback clips (no assets): a plate ping and a
// door rumble, mixed by the audio module (silent null device when no
// audio hardware is present).
type sounds struct {
	dev    *audio.Device
	ping   *audio.Sound
	whoosh *audio.Sound
}

func newSounds() *sounds {
	dev, err := audio.Open()
	if err != nil {
		return &sounds{}
	}
	s := &sounds{dev: dev}
	s.ping = synth(dev, 0.12, func(t float32) float32 {
		return 0.4 * math32.Sin(2*math32.Pi*880*t) * (1 - t/0.12)
	})
	s.whoosh = synth(dev, 0.5, func(t float32) float32 {
		env := math32.Sin(math32.Pi * t / 0.5)
		return 0.3 * env * math32.Sin(2*math32.Pi*(90+60*t)*t)
	})
	return s
}

// synth builds a mono clip from a sample function over [0, dur) seconds.
func synth(dev *audio.Device, dur float32, f func(t float32) float32) *audio.Sound {
	n := int(dur * audio.SampleRate)
	pcm := make([]byte, 44+n*2)
	copy(pcm[0:], "RIFF")
	binary.LittleEndian.PutUint32(pcm[4:], uint32(36+n*2))
	copy(pcm[8:], "WAVE")
	copy(pcm[12:], "fmt ")
	binary.LittleEndian.PutUint32(pcm[16:], 16)
	binary.LittleEndian.PutUint16(pcm[20:], 1)
	binary.LittleEndian.PutUint16(pcm[22:], 1)
	binary.LittleEndian.PutUint32(pcm[24:], audio.SampleRate)
	binary.LittleEndian.PutUint32(pcm[28:], audio.SampleRate*2)
	binary.LittleEndian.PutUint16(pcm[32:], 2)
	binary.LittleEndian.PutUint16(pcm[34:], 16)
	copy(pcm[36:], "data")
	binary.LittleEndian.PutUint32(pcm[40:], uint32(n*2))
	for i := 0; i < n; i++ {
		v := int16(f(float32(i)/audio.SampleRate) * 32767)
		binary.LittleEndian.PutUint16(pcm[44+i*2:], uint16(v))
	}
	snd, err := dev.LoadWAV(pcm)
	if err != nil {
		return nil
	}
	return snd
}

func (s *sounds) play(snd *audio.Sound) {
	if snd != nil {
		snd.Play()
	}
}

// ---- app --------------------------------------------------------------------

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
	shot := flag.String("screenshot", "", "scripted walk, then one frame to a PNG")
	flag.Parse()
	var err error
	if *shot != "" {
		err = headless(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fps:", err)
		os.Exit(1)
	}
}

func frame(dev *gpu.Device, q *gpu.Queue, r3 *render.Renderer3D, r2 *render.Renderer2D,
	u *ui.UI, l *level, p *player,
	view, depthView *gpu.TextureView, w, h int) error {
	vp, eye := p.viewProj(w, h)
	r3.Begin(vp, lightDir, eye)
	if err := l.draw(r3); err != nil {
		return err
	}

	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, depthView, gpu.Color{R: 0.10, G: 0.11, B: 0.14, A: 1}); err != nil {
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

// hud builds the crosshair + status overlay.
func hud(p *player, w, h float32) ui.Element {
	dim := ui.RGB(0x9aa0ac)
	cross := ui.Div().Size(4, 4).Rounded(2).Bg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.8})
	status := "walk to the green plate to open the door"
	col := dim
	if p.inZone {
		status = "plate active — door opening"
		col = ui.RGBA{R: 0.25, G: 0.85, B: 0.45, A: 1}
	}
	ground := "airborne"
	if p.char.OnGround {
		ground = "grounded"
	}
	bar := ui.Div().Row().Gap(14).P(8).Bg(ui.RGBA{A: 0.5}).Rounded(8).Children(
		ui.Text(status, col).SizePx(13),
		ui.Text(ground, dim).SizePx(13),
		ui.Text("WASD move · drag look · Space jump", dim).SizePx(13),
	)
	return ui.Div().Children(
		ui.Overlay(cross, w/2-2, h/2-2),
		ui.Overlay(bar, 14, h-46),
	)
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: fps walkthrough", Width: 1024, Height: 640})
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
	r3.SetShadowArea(core.V3(0, 0, 10), 16)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 15)
	if err != nil {
		return err
	}

	l, err := buildLevel()
	if err != nil {
		return err
	}
	p := newPlayer(l.world())
	u := ui.New(r2, font, func() ui.Element { return hud(p, float32(w), float32(h)) })
	in := shell.NewInput()
	snd := newSounds()
	wasInZone, wasOpening := false, false

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()
	last := time.Now()
	var lookX, lookY float32
	var haveLast bool
	var mouseFrame []app.MouseEvent
	rendered := 0
	win.EachFrame(func() bool {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now
		if dt > 0.05 {
			dt = 0.05
		}

		mouseFrame = win.MouseEvents()
		in.DrainEvents(win.KeyEvents(), mouseFrame)
		// Mouse look: pointer capture (click to grab, Esc to release) gives
		// relative deltas; uncaptured falls back to drag-look.
		var look [2]float32
		if win.PointerCaptured() {
			for _, m := range mouseFrame {
				look[0] += m.DX * 0.0022
				look[1] += -m.DY * 0.0022
			}
			if in.Pressed(app.KeyEscape, 0) {
				win.SetPointerCapture(false)
			}
		} else {
			if in.MouseClicked() {
				win.SetPointerCapture(true)
			}
			if in.MouseDown() && haveLast {
				look[0] = (in.MouseX - lookX) * 0.005
				look[1] = -(in.MouseY - lookY) * 0.005
			}
		}
		lookX, lookY, haveLast = in.MouseX, in.MouseY, true

		// Keyboard movement.
		var move core.Vec3
		if in.Rune('w') {
			move.Z++
		}
		if in.Rune('s') {
			move.Z--
		}
		if in.Rune('d') {
			move.X++
		}
		if in.Rune('a') {
			move.X--
		}
		jump := in.Pressed(app.KeyNone, ' ')

		// Gamepad: first connected pad.
		pads := app.Gamepads()
		for i := range pads {
			g := &pads[i]
			if !g.Connected {
				continue
			}
			move.X += dead(g.Axes[0])
			move.Z -= dead(g.Axes[1]) // stick up = forward
			look[0] += dead(g.Axes[2]) * dt * 1.5
			look[1] -= dead(g.Axes[3]) * dt * 1.5
			if g.Pressed(app.PadA) {
				jump = true
			}
			break
		}
		if m := move.Length(); m > 1 {
			move = move.Scale(1 / m)
		}

		l.step(dt, p.inZone)
		p.update(dt, move, look, jump)
		// Audio feedback: plate entry pings, the door rumbles as it starts.
		if p.inZone && !wasInZone {
			snd.play(snd.ping)
		}
		wasInZone = p.inZone
		opening := l.doorUp > 0 && l.doorUp < 1
		if opening && !wasOpening {
			snd.play(snd.whoosh)
		}
		wasOpening = opening

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
		err := frame(device, queue, r3, r2, u, l, p, view, depthView, w, h)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fps:", err)
			return false
		}
		// Present BEFORE releasing the swapchain texture — releasing
		// first drops the reference presentation needs and the
		// swapchain slot leaks until acquire blocks (frozen window).
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	return nil
}

func dead(v float32) float32 {
	if v > -0.15 && v < 0.15 {
		return 0
	}
	return v
}

// headless scripts a short walk (forward to the plate, door opens) and
// renders the resulting first-person frame.
func headless(path string) error {
	const W, H = 1024, 640
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
	r3.SetShadowArea(core.V3(0, 0, 10), 16)
	r2, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 15)
	if err != nil {
		return err
	}

	l, err := buildLevel()
	if err != nil {
		return err
	}
	p := newPlayer(l.world())
	u := ui.New(r2, font, func() ui.Element { return hud(p, W, H) })

	// Scripted walk: forward onto the pressure plate (z≈13), then hold
	// while the door slides open — the frame shows the level from the plate.
	const dt = 1.0 / 60
	for i := 0; i < 200; i++ {
		l.step(dt, p.inZone)
		p.update(dt, core.V3(0, 0, 1), [2]float32{}, false)
	}
	for i := 0; i < 90; i++ { // stand still; door animates open
		l.step(dt, p.inZone)
		p.update(dt, core.Vec3{}, [2]float32{}, false)
	}
	// Turn around for the shot: the landing with the animated character.
	p.yaw = math32.Pi
	p.pitch = -0.05
	if !p.inZone && l.doorUp < 0.9 {
		return fmt.Errorf("scripted walk never opened the door (pos %+v, doorUp %v)", p.char.Pos, l.doorUp)
	}

	if err := frame(device, queue, r3, r2, u, l, p, view, depthView, W, H); err != nil {
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
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}
