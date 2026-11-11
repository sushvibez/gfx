// Package navcube is the orientation cube (a view cube, as in Shapr3D or
// Fusion): a chamfered cube drawn in its own small offscreen 3D view,
// turned with an orbit camera, whose 6 labelled faces, 12 edge bevels and
// 8 corners are separately pickable. Hovering a facet highlights it;
// clicking one gives the camera angles that look straight at it (a face
// for Front or Top, a corner for a three-quarter view), and Ease moves a
// camera there a step per frame.
//
// The cube knows nothing about the app's camera beyond its angles. The
// convention is the usual orbit one: the eye sits at
// target + d·(cos p·sin y, sin p, cos p·cos y) looking at the target with
// +Y up, so yaw 0, pitch 0 looks along −Z at the Front face.
//
// Use: New once on the GPU device; place Viewport(size, onSnap) in the
// layout (an Overlay over the corner of the main 3D view, or wherever);
// each frame call Render(enc, yaw, pitch) in the 3D pass and HoverAt with
// the pointer; in onSnap start easing the camera toward the angles given.
package navcube

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/chewxy/math32"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Chamfer is how much of each face's half-width the edge bevels take.
const Chamfer = 0.34

// Facet colours.
var (
	FaceColor  = render.RGBA{R: 0.985, G: 0.988, B: 0.992, A: 1}
	BevelColor = render.RGBA{R: 0.925, G: 0.93, B: 0.94, A: 1}
	HoverColor = render.RGBA{R: 0.62, G: 0.75, B: 0.98, A: 1}
	LabelColor = color.RGBA{R: 0x55, G: 0x58, B: 0x5d, A: 0xff}
)

// Options configure New.
type Options struct {
	// Font is the TTF/OTF the face labels are drawn in; nil uses the Go
	// font, which is always available.
	Font []byte
	// Light is the light direction (zero: from above, front left).
	Light core.Vec3
	// Labels names the faces Top, Bottom, Front, Back, Right, Left in
	// that order; empty entries keep the English names.
	Labels [6]string
	// Axes draws the world axes from the cube's (-1, -1, -1) corner along its
	// edges, out past the far corners (X red, Y green, Z blue; AxisColors),
	// depth-tested, so the cube hides the stretches behind it.
	Axes bool
}

// Cube is one orientation cube with its own renderer and offscreen view.
type Cube struct {
	// R3 draws the cube and View holds its image; both are exposed for
	// apps that place the view themselves (a .gml Viewport3D source) or
	// tune the renderer (shadows).
	R3   *render.Renderer3D
	View *render.View3D
	// Hover is the facet under the pointer (-1 none), drawn highlighted.
	Hover int

	facets     []facet
	axes       [3]*render.Mesh // with Options.Axes
	light      core.Vec3
	yaw, pitch float32 // the angles the cube was last rendered at
}

type facet struct {
	mesh   *render.Mesh
	normal core.Vec3 // outward
	label  *render.Image3D
}

// faceDefs are the six faces: direction and the label's texture axes (u
// right, v down).
var faceDefs = []struct {
	name string
	dir  core.Vec3
	u, v core.Vec3
}{
	{"Top", core.V3(0, 1, 0), core.V3(1, 0, 0), core.V3(0, 0, 1)},
	{"Bottom", core.V3(0, -1, 0), core.V3(1, 0, 0), core.V3(0, 0, -1)},
	{"Front", core.V3(0, 0, 1), core.V3(1, 0, 0), core.V3(0, -1, 0)},
	{"Back", core.V3(0, 0, -1), core.V3(-1, 0, 0), core.V3(0, -1, 0)},
	{"Right", core.V3(1, 0, 0), core.V3(0, 0, -1), core.V3(0, -1, 0)},
	{"Left", core.V3(-1, 0, 0), core.V3(0, 0, 1), core.V3(0, -1, 0)},
}

// Geometry is one facet's polygon (unit cube, chamfered), its outward
// normal, texture coordinates for a labelled face and the face index (-1
// for bevels and corners). New builds meshes from it; it is exported for
// tests and for apps that want the facets elsewhere.
type Geometry struct {
	Points []core.Vec3
	Normal core.Vec3
	UV     [][2]float32
	Face   int
}

// Facets returns the cube's 26 facets: the 6 faces first (in faceDefs
// order), then the 12 edge bevels, then the 8 corners.
func Facets() []Geometry {
	e := float32(1 - Chamfer)
	var out []Geometry
	for fi, fd := range faceDefs {
		g := Geometry{Normal: fd.dir, Face: fi}
		for _, s := range [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			g.Points = append(g.Points, fd.dir.Add(fd.u.Scale(s[0]*e)).Add(fd.v.Scale(s[1]*e)))
			g.UV = append(g.UV, [2]float32{(s[0]*e + 1) / 2, (s[1]*e + 1) / 2})
		}
		out = append(out, g)
	}
	axes := [3]core.Vec3{core.V3(1, 0, 0), core.V3(0, 1, 0), core.V3(0, 0, 1)}
	for a := 0; a < 3; a++ {
		for b := a + 1; b < 3; b++ {
			t := 3 - a - b
			for _, sa := range []float32{1, -1} {
				for _, sb := range []float32{1, -1} {
					na, nb := axes[a].Scale(sa), axes[b].Scale(sb)
					out = append(out, Geometry{Face: -1, Normal: na.Add(nb).Normalize(), Points: []core.Vec3{
						na.Add(nb.Scale(e)).Add(axes[t].Scale(-e)),
						na.Add(nb.Scale(e)).Add(axes[t].Scale(e)),
						nb.Add(na.Scale(e)).Add(axes[t].Scale(e)),
						nb.Add(na.Scale(e)).Add(axes[t].Scale(-e)),
					}})
				}
			}
		}
	}
	for _, sx := range []float32{1, -1} {
		for _, sy := range []float32{1, -1} {
			for _, sz := range []float32{1, -1} {
				out = append(out, Geometry{Face: -1, Normal: core.V3(sx, sy, sz).Normalize(), Points: []core.Vec3{
					core.V3(sx, sy*e, sz*e), core.V3(sx*e, sy, sz*e), core.V3(sx*e, sy*e, sz),
				}})
			}
		}
	}
	return out
}

// New builds the cube: its renderer, its offscreen view (cleared to
// transparent, so only the facets show over whatever is under it), the
// facet meshes and the label textures.
func New(dev *gpu.Device, q *gpu.Queue, format gpu.TextureFormat, r2 *render.Renderer2D, opt Options) (*Cube, error) {
	r3, err := render.New3D(dev, q, format)
	if err != nil {
		return nil, err
	}
	c := &Cube{R3: r3, View: render.NewView3D(dev, r2, format), Hover: -1, light: opt.Light}
	if c.light == (core.Vec3{}) {
		c.light = core.V3(-0.55, -1, -0.35)
	}
	c.View.Clear = gpu.Color{}
	fontData := opt.Font
	if fontData == nil {
		fontData = goregular.TTF
	}
	for _, g := range Facets() {
		f := facet{normal: g.Normal, mesh: mesh(r3, g)}
		if g.Face >= 0 {
			name := faceDefs[g.Face].name
			if opt.Labels[g.Face] != "" {
				name = opt.Labels[g.Face]
			}
			if f.label, err = labelTexture(r3, fontData, name); err != nil {
				return nil, err
			}
		}
		c.facets = append(c.facets, f)
	}
	if opt.Axes {
		for a := range c.axes {
			c.axes[a] = axisBar(r3, a)
		}
	}
	return c, nil
}

// mesh makes a facet's mesh, wound to face along its normal.
func mesh(r *render.Renderer3D, g Geometry) *render.Mesh {
	verts := make([]render.Vertex3D, len(g.Points))
	for i, p := range g.Points {
		verts[i] = render.Vertex3D{Pos: p, Normal: g.Normal}
		if g.UV != nil {
			verts[i].U, verts[i].V = g.UV[i][0], g.UV[i][1]
		}
	}
	idx := []uint32{0, 1, 2}
	if len(g.Points) == 4 {
		idx = []uint32{0, 1, 2, 0, 2, 3}
	}
	p := g.Points
	if p[1].Sub(p[0]).Cross(p[2].Sub(p[0])).Dot(g.Normal) < 0 {
		for i := 0; i+2 < len(idx); i += 3 {
			idx[i+1], idx[i+2] = idx[i+2], idx[i+1]
		}
	}
	return r.NewMesh(verts, idx)
}

// labelTexture rasterises a face label, dark on white.
func labelTexture(r *render.Renderer3D, fontData []byte, text string) (*render.Image3D, error) {
	const size = 128
	f, err := opentype.Parse(fontData)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 30, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	d := font.Drawer{Dst: img, Src: image.NewUniform(LabelColor), Face: face}
	w := d.MeasureString(text)
	d.Dot = fixed.Point26_6{X: (fixed.I(size) - w) / 2, Y: fixed.I(size/2) + face.Metrics().Ascent/2 - face.Metrics().Descent/2}
	d.DrawString(text)
	return r.NewImage3D(size, size, img.Pix), nil
}

// SetClear sets the view's background: the colour of whatever the cube
// floats on, at zero alpha, so only the facets are drawn over it.
func (c *Cube) SetClear(bg gpu.Color) {
	bg.A = 0
	c.View.Clear = bg
}

// Viewport is the cube's element, size × size pixels. A tap on a facet
// calls onSnap with the camera angles that look straight at it.
func (c *Cube) Viewport(size float32, onSnap func(yaw, pitch float32)) *ui.Viewport3DEl {
	return ui.Viewport3D(c.View).Size(size, size).OnTap(func(x, y float32) {
		if yaw, pitch, ok := c.Tap(x, y); ok && onSnap != nil {
			onSnap(yaw, pitch)
		}
	})
}

// ViewProj is the cube's camera at the given angles for a w×h view: the
// cube spans [-1, 1]³ in it (hosts draw beside it, an axis triad from a
// corner, say).
func ViewProj(yaw, pitch float32, w, h int) core.Mat4 {
	vp, _ := viewProj(yaw, pitch, w, h)
	return vp
}

// viewProj is the cube's camera at the given angles.
func viewProj(yaw, pitch float32, w, h int) (core.Mat4, core.Vec3) {
	const d = 5.4
	cp := math32.Cos(pitch)
	eye := core.V3(d*cp*math32.Sin(yaw), d*math32.Sin(pitch), d*cp*math32.Cos(yaw))
	view := core.Mat4LookAt(eye, core.Vec3{}, core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math32.Pi/3, float32(w)/float32(max(h, 1)), 0.05, 500)
	return proj.Mul(view), eye
}

// Render draws the cube at the camera's yaw and pitch into its view (call
// it in the 3D pass, before the UI pass shows the view). Picking uses the
// angles of the last Render.
func (c *Cube) Render(enc *gpu.CommandEncoder, yaw, pitch float32) error {
	c.yaw, c.pitch = yaw, pitch
	w, h := c.View.Size()
	if w == 0 || h == 0 {
		return nil
	}
	vp, eye := viewProj(yaw, pitch, w, h)
	c.R3.Begin(vp, c.light, eye)
	model := core.Mat4Scale(core.V3(Scale, Scale, Scale))
	for i, f := range c.facets {
		col := FaceColor
		if f.label == nil {
			col = BevelColor
		}
		if i == c.Hover {
			col = HoverColor
		}
		c.R3.SetPickTag(i)
		c.R3.DrawShaded(f.mesh, model, col, 0.55, f.label)
	}
	c.R3.SetPickTag(-1)
	for a, m := range c.axes {
		if m != nil {
			c.R3.DrawUnlit(m, model, AxisColors[a])
		}
	}
	return c.View.Render(enc, c.R3)
}

// Pick is the facet under a point in the cube view's own pixels (-1
// none).
func (c *Cube) Pick(x, y float32) int {
	w, h := c.View.Size()
	if w == 0 || h == 0 {
		return -1
	}
	vp, _ := viewProj(c.yaw, c.pitch, w, h)
	if o, d, ok := render.ScreenRay(vp, x, y, float32(w), float32(h)); ok {
		if hit, ok := c.R3.Pick(o, d); ok && hit.Tag >= 0 && hit.Tag < len(c.facets) {
			return hit.Tag
		}
	}
	return -1
}

// HoverAt sets Hover from a pointer position in window pixels and the
// view's window rect (Viewport3DEl.Rect); outside the rect it clears it.
func (c *Cube) HoverAt(x, y float32, rect ui.Rect) {
	c.Hover = -1
	if rect.W > 0 && x >= rect.X && x < rect.X+rect.W && y >= rect.Y && y < rect.Y+rect.H {
		c.Hover = c.Pick(x-rect.X, y-rect.Y)
	}
}

// Normal is a facet's outward normal.
func (c *Cube) Normal(i int) core.Vec3 { return c.facets[i].normal }

// Tap is the snap target for a tap at a view-local point: the angles that
// look at the facet there.
func (c *Cube) Tap(x, y float32) (yaw, pitch float32, ok bool) {
	i := c.Pick(x, y)
	if i < 0 {
		return 0, 0, false
	}
	yaw, pitch = SnapAngles(c.facets[i].normal)
	return yaw, pitch, true
}

// Release frees the cube's GPU resources.
func (c *Cube) Release() {
	for a, m := range c.axes {
		if m != nil {
			m.Release()
			c.axes[a] = nil
		}
	}
	if c.View != nil {
		c.View.Release()
		c.View = nil
	}
	if c.R3 != nil {
		c.R3.Release()
		c.R3 = nil
	}
}

// SnapAngles is the orbit yaw and pitch that look straight along −n (the
// camera on n's side), pitch kept within ±1.5 rad so the orbit's up
// vector never flips.
func SnapAngles(n core.Vec3) (yaw, pitch float32) {
	yaw = math32.Atan2(n.X, n.Z)
	pitch = min(max(math32.Asin(min(max(n.Y, -1), 1)), -1.5), 1.5)
	return yaw, pitch
}

// Ease moves yaw and pitch a quarter of the way to the target, yaw the
// short way round; done reports that they have arrived (and are set
// exactly).
func Ease(yaw, pitch, toYaw, toPitch float32) (y, p float32, done bool) {
	dy := toYaw - yaw
	for dy > math32.Pi {
		dy -= 2 * math32.Pi
	}
	for dy < -math32.Pi {
		dy += 2 * math32.Pi
	}
	dp := toPitch - pitch
	if math32.Abs(dy)+math32.Abs(dp) < 0.003 {
		return toYaw, toPitch, true
	}
	return yaw + dy*0.25, pitch + dp*0.25, false
}

// Element is the cube ready to place: Viewport (a tap snaps through
// onSnap) inside its own drag region, so a drag that starts on the cube
// turns the camera through onOrbit (dx, dy: pixels moved since the last
// call) and never reaches what lies under it (a panel's resize handle,
// a pannable pane). A drag that travels does not also snap.
func (c *Cube) Element(size float32, onSnap func(yaw, pitch float32), onOrbit func(dx, dy float32)) ui.Element {
	var lastX, lastY float32
	return ui.Div().W(size).H(size).OnDrag(func(dx, dy float32, phase ui.DragPhase) {
		if phase == ui.DragStart {
			lastX, lastY = 0, 0
			return
		}
		if onOrbit != nil && (dx != lastX || dy != lastY) {
			onOrbit(dx-lastX, dy-lastY)
		}
		lastX, lastY = dx, dy
	}).Children(c.Viewport(size, onSnap))
}

// Scale is the cube's size in its view: it spans [-Scale, Scale]³ in the
// space ViewProj projects.
const Scale = 1.55

// AxisLength is how far (in cube units, the cube being 2 across) the
// axes reach from the corner: past the far corner, so their tips and
// letters clear the cube.
const AxisLength = 2.7

// AxisColors are the axes' colours, X, Y, Z.
var AxisColors = [3]render.RGBA{
	{R: 0.86, G: 0.27, B: 0.27, A: 1},
	{R: 0.25, G: 0.66, B: 0.31, A: 1},
	{R: 0.30, G: 0.47, B: 0.92, A: 1},
}

// AxisTip is where axis a (0 X, 1 Y, 2 Z) ends, in the space ViewProj
// projects (a host puts its letter there).
func AxisTip(a int) core.Vec3 {
	p := core.V3(-1, -1, -1)
	switch a {
	case 0:
		p.X += AxisLength
	case 1:
		p.Y += AxisLength
	default:
		p.Z += AxisLength
	}
	return p.Scale(Scale)
}

// axisBar is axis a as a thin square bar from the (-1, -1, -1) corner,
// lifted a little off the cube's bevelled edge so it shows where the
// edge faces the viewer.
func axisBar(r *render.Renderer3D, a int) *render.Mesh {
	const w = 0.035 // half thickness
	lo := core.V3(-1-2*w, -1-2*w, -1-2*w)
	hi := core.V3(-1, -1, -1)
	switch a {
	case 0:
		hi.X = -1 + AxisLength
	case 1:
		hi.Y = -1 + AxisLength
	default:
		hi.Z = -1 + AxisLength
	}
	c := [8]core.Vec3{}
	for i := range c {
		c[i] = core.V3(pick(i&1 != 0, hi.X, lo.X), pick(i&2 != 0, hi.Y, lo.Y), pick(i&4 != 0, hi.Z, lo.Z))
	}
	quads := [6][4]int{{0, 2, 3, 1}, {4, 5, 7, 6}, {0, 1, 5, 4}, {2, 6, 7, 3}, {0, 4, 6, 2}, {1, 3, 7, 5}}
	var verts []render.Vertex3D
	var idx []uint32
	mid := lo.Add(hi).Scale(0.5)
	for _, q := range quads {
		n := c[q[1]].Sub(c[q[0]]).Cross(c[q[2]].Sub(c[q[0]])).Normalize()
		if n.Dot(c[q[0]].Add(c[q[2]]).Scale(0.5).Sub(mid)) < 0 {
			q[1], q[3] = q[3], q[1] // wind it outward
			n = n.Scale(-1)
		}
		base := uint32(len(verts))
		for _, k := range q {
			verts = append(verts, render.Vertex3D{Pos: c[k], Normal: n})
		}
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return r.NewMesh(verts, idx)
}

func pick(b bool, x, y float32) float32 {
	if b {
		return x
	}
	return y
}
