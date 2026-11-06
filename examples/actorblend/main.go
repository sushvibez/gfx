//go:build linux || windows

// actorblend is the skeletal-animation showcase: one Mixamo-style soldier, five
// retargeted clips crossfaded into each other, and a rifle attached to the
// right-hand joint.
//
// Keys 1-5 switch clip with a 0.15 s crossfade — the AnimationPlayer
// samples the outgoing and incoming clips into separate poses and blends
// them per joint, so the actor leans out of one stance into the next
// instead of snapping. The weapon is drawn at
//
//	place · model.JointBySuffix("RightHand") · attachment
//
// re-read every frame from the pose the last Advance produced, so it tracks
// the hand through the whole clip.
//
//	cd examples && go run ./actorblend
//	go run ./actorblend -screenshot out.png          # headless, mid-clip
//	go run ./actorblend -clip 2 -screenshot out.png  # a specific stance
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chewxy/math32"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/fbx"
	"github.com/sushvibez/gfx/render/gltf"
	"github.com/sushvibez/gfx/ui"
)

var (
	assetDir = flag.String("assets", "", "asset root holding models/ and animations/ (default: build/actors)")
	meshName = flag.String("mesh", "models/hostile.fbx", "character FBX, relative to the asset root")
	weapon   = flag.String("weapon", "weapons/ak47_with_scope.glb", "weapon glTF, relative to the asset root")
	boneName = flag.String("bone", "RightHand", "joint the weapon attaches to (suffix match)")
	startIdx = flag.Int("clip", 0, "clip index to start on (0-4)")
	fadeSecs = flag.Float64("fade", 0.15, "crossfade length in seconds")
	corpse   = flag.Float64("alpha", 1, "draw the actor faded (corpse dissolve), 1 = solid")
	tintHex  = flag.String("tint", "", "faction tint as RRGGBB, e.g. 8899ff")
)

// The five clips bound to keys 1-5.
var clipFiles = []string{
	"animations/Idle.fbx",
	"animations/Stepping Backward.fbx",
	"animations/Firing Rifle.fbx",
	"animations/Grab Rifle And Put Back.fbx",
	"animations/Dying.fbx",
}

// Weapon attachment, a Godot BoneAttachment3D equivalent:
// metres in the hand's frame, and a 0.001 scale because the glb is modelled
// in millimetres (Bounds() on the loaded weapon confirms it — see banner()).
var (
	attachOffset = core.V3(-0.11, 0.292, 0.015)
	attachEuler  = core.V3(0, 0, 270) // degrees, Z only
	attachScale  = float32(0.001)
)

var lightDir = core.V3(-0.5, -1, -0.3)

// hexTint parses "RRGGBB" into a multiplicative material tint.
func hexTint(h string) render.RGBA {
	var r, g, b int
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return render.RGBA{R: 1, G: 1, B: 1, A: 1}
	}
	return render.RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: 1}
}

// assetRoot finds the character assets: -assets, else build/actors.
func assetRoot() (string, error) {
	roots := []string{*assetDir}
	if *assetDir == "" {
		roots = []string{
			"../build/actors", "build/actors",
		}
	}
	for _, r := range roots {
		if _, err := os.Stat(filepath.Join(r, *meshName)); err == nil {
			return r, nil
		}
	}
	return "", fmt.Errorf("character assets not found (tried %s); pass -assets", strings.Join(roots, ", "))
}

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

// loadDoc reads an FBX or glTF/glb file into a document, resolving external
// textures next to it.
func loadDoc(path string) (*gltf.Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	ext := func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(uri)))
	}
	if strings.EqualFold(filepath.Ext(path), ".fbx") {
		d, err := fbx.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		doc, err := fbx.ToGLTF(d, ext)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return doc, nil
	}
	doc, err := gltf.Parse(b, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// scene holds everything both the windowed and headless paths need.
type scene struct {
	r3     *render.Renderer3D
	r2     *render.Renderer2D
	font   *render.Font
	actor  *render.Model
	player *render.AnimationPlayer
	rifle  *render.Model
	place  core.Mat4 // actor: raw rig units → metres, feet on the ground
	attach core.Mat4 // hand joint → weapon, in metres
	names  []string  // clip labels, in key order
	note   string    // one-off diagnostics (weapon size, missing bone)
}

func setup(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (*scene, error) {
	root, err := assetRoot()
	if err != nil {
		return nil, err
	}
	meshDoc, err := loadDoc(filepath.Join(root, *meshName))
	if err != nil {
		return nil, err
	}

	// Every clip ships as its own FBX on a differently-namespaced rig, so
	// each is retargeted onto the character by bone name and renamed after
	// its file (Mixamo calls them all "mixamo.com").
	var clips []gltf.Animation
	var names []string
	for _, rel := range clipFiles {
		clipDoc, err := loadDoc(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		fbx.Retarget(meshDoc, clipDoc)
		label := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		for i := range clipDoc.Animations {
			a := clipDoc.Animations[i]
			a.Name = label
			clips = append(clips, a)
			names = append(names, label)
			break // one clip per file
		}
	}
	if len(clips) == 0 {
		return nil, fmt.Errorf("no clips loaded from %s", root)
	}

	r3, err := render.New3D(device, queue, format)
	if err != nil {
		return nil, err
	}
	r3.SetShadowArea(core.V3(0, 0.9, 0), 4)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return nil, err
	}
	font, err := r2.NewFont(fontBytes(), 15)
	if err != nil {
		return nil, err
	}
	actor, err := r3.BuildModel(meshDoc)
	if err != nil {
		return nil, err
	}

	// Normalize the rig (Mixamo FBX is centimetres) to a 1.8 m actor with
	// its feet on the ground.
	s := float32(1)
	if h := actor.Height(); h > 0 {
		s = 1.8 / h
	}
	lo, hi := actor.Bounds()
	c := lo.Add(hi).Scale(0.5)
	place := core.Mat4Translate(core.V3(-c.X*s, -lo.Y*s, -c.Z*s)).Mul(core.Mat4Scale(core.V3(s, s, s)))

	sc := &scene{r3: r3, r2: r2, font: font, actor: actor, place: place, names: names}

	// The attachment is authored in metres in the bone's frame, while the
	// joint world that carries it is still in rig units: undo the rig scale
	// first (place·joint·1/s is a metre frame), then offset/rotate/scale.
	sc.attach = core.Mat4Scale(core.V3(1/s, 1/s, 1/s)).
		Mul(core.Mat4Translate(attachOffset)).
		Mul(core.Mat4RotateZ(attachEuler.Z * math32.Pi / 180)).
		Mul(core.Mat4RotateY(attachEuler.Y * math32.Pi / 180)).
		Mul(core.Mat4RotateX(attachEuler.X * math32.Pi / 180)).
		Mul(core.Mat4Scale(core.V3(attachScale, attachScale, attachScale)))

	if wdoc, err := loadDoc(filepath.Join(root, *weapon)); err == nil {
		if rifle, err := r3.BuildModel(wdoc); err == nil {
			sc.rifle = rifle
			wlo, whi := rifle.Bounds()
			d := whi.Sub(wlo)
			long := math32.Max(d.X, math32.Max(d.Y, d.Z))
			sc.note = fmt.Sprintf("rifle %.0f×%.0f×%.0f source units → %.2f m long at ×%g",
				d.X, d.Y, d.Z, long*attachScale, attachScale)
		}
	}
	if _, ok := actor.JointBySuffix(*boneName); !ok {
		sc.note = fmt.Sprintf("joint %q not found in the rig", *boneName)
	}

	sc.player = render.NewAnimationPlayer(actor, clips)
	idx := *startIdx
	if idx < 0 || idx >= len(clips) {
		idx = 0
	}
	sc.player.PlayIndex(idx, render.PlayOpts{Loop: true})
	return sc, nil
}

// switchTo crossfades to clip i. Dying plays once and freezes on its last
// pose (Stop), the way a downed actor should stay down.
func (s *scene) switchTo(i int) {
	if i < 0 || i >= len(s.names) {
		return
	}
	loop := s.names[i] != "Dying"
	s.player.PlayIndex(i, render.PlayOpts{Loop: loop, Fade: float32(*fadeSecs)})
	if !loop {
		s.player.OnFinished = func(string) { s.player.Stop() }
	} else {
		s.player.OnFinished = nil
	}
}

func (s *scene) caption() string {
	fading, w := s.player.Fading()
	state := "playing"
	switch {
	case fading:
		state = fmt.Sprintf("fading %.0f%%", w*100)
	case s.player.Finished():
		state = "finished (pose held)"
	}
	line := fmt.Sprintf("%s — %.2fs / %.2fs — %s", s.player.Current(),
		s.player.Position(), s.player.Duration(), state)
	keys := "1 Idle  2 Back  3 Fire  4 Grab  5 Dying   (drag: orbit, wheel: zoom)"
	if s.note != "" {
		return line + "\n" + keys + "\n" + s.note
	}
	return line + "\n" + keys
}

// draw queues the ground, the actor and the hand-attached weapon.
func (s *scene) draw() {
	s.r3.Draw(s.r3.CubeMesh(),
		core.Mat4Translate(core.V3(0, -0.03, 0)).Mul(core.Mat4Scale(core.V3(6, 0.06, 6))),
		render.RGBA{R: 0.20, G: 0.21, B: 0.24, A: 1})
	switch {
	case *corpse < 1:
		// Corpse fade: a dissolve, not alpha blending (see DrawFaded).
		s.actor.DrawFaded(s.r3, s.place, float32(*corpse))
	case *tintHex != "":
		s.actor.DrawTinted(s.r3, s.place, hexTint(*tintHex))
	default:
		s.actor.Draw(s.r3, s.place)
	}
	if s.rifle == nil {
		return
	}
	// The joint world comes from the pose the last Advance produced.
	if hand, ok := s.actor.JointBySuffix(*boneName); ok {
		s.rifle.Draw(s.r3, s.place.Mul(hand).Mul(s.attach))
	}
}

func (s *scene) frame(dev *gpu.Device, q *gpu.Queue, u *ui.UI, cam *orbit.Camera,
	view, depthView *gpu.TextureView, w, h int) error {
	vp, eye := cam.ViewProj(w, h)
	s.r3.Begin(vp, lightDir, eye)
	s.draw()

	enc := dev.CreateCommandEncoder()
	if err := s.r3.Render(enc, view, depthView, gpu.Color{R: 0.09, G: 0.10, B: 0.12, A: 1}); err != nil {
		return err
	}
	s.r2.Begin(w, h)
	u.Frame(float32(w), float32(h))
	pass := enc.BeginRenderPassLoad(view)
	if err := s.r2.Flush(pass); err != nil {
		return err
	}
	pass.End()
	q.Submit(enc.Finish())
	return nil
}

func (s *scene) overlay() *ui.UI {
	return ui.New(s.r2, s.font, func() ui.Element {
		lines := strings.Split(s.caption(), "\n")
		kids := make([]ui.Element, len(lines))
		for i, l := range lines {
			kids[i] = ui.Text(l, ui.RGB(0xe8eaf0))
		}
		return ui.Div().P(12).Children(
			ui.Div().P(10).Gap(2).Bg(ui.RGBA{R: 0, G: 0, B: 0, A: 0.55}).Rounded(8).Children(kids...))
	})
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	flag.Parse()
	var err error
	if *shot != "" {
		err = headless(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "actorblend:", err)
		os.Exit(1)
	}
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: blended actor", Width: 900, Height: 640})
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

	sc, err := setup(device, queue, format)
	if err != nil {
		return err
	}
	u := sc.overlay()
	cam := &orbit.Camera{Target: core.V3(0, 1.0, 0), Distance: 3.0, Yaw: 0.5, Pitch: 0.06}

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()
	last := time.Now()
	rendered := 0
	win.EachFrame(func() bool {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now
		if dt > 0.1 {
			dt = 0.1
		}
		for _, ev := range win.KeyEvents() {
			if ev.Down && ev.Rune >= '1' && ev.Rune <= '5' {
				sc.switchTo(int(ev.Rune - '1'))
			}
		}
		sc.player.Advance(dt)

		u.Mouse(win.MouseEvents())
		cam.Handle(win.MouseEvents())
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
		if err := sc.frame(device, queue, u, cam, view, depthView, w, h); err != nil {
			fmt.Fprintln(os.Stderr, "actorblend:", err)
			return false
		}
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	return nil
}

// headless renders one frame mid-crossfade: the player starts on the chosen
// clip, then fades to the next one and is advanced halfway through the
// fade, so the screenshot shows a genuinely blended pose.
func headless(path string) error {
	const W, H = 900, 640
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

	sc, err := setup(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	sc.player.Seek(sc.player.Duration() * 0.35)
	sc.switchTo((*startIdx + 1) % len(sc.names))
	sc.player.Advance(float32(*fadeSecs) * 0.5)

	u := sc.overlay()
	cam := &orbit.Camera{Target: core.V3(0, 1.0, 0), Distance: 3.0, Yaw: 0.5, Pitch: 0.06}
	if err := sc.frame(device, queue, u, cam, view, depthView, W, H); err != nil {
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
	fmt.Println("wrote", path, "—", sc.note)
	return nil
}
