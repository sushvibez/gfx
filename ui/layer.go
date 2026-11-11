package ui

// LayerEl renders its child into an offscreen texture and paints that
// texture instead of the child — QML's `layer.enabled`, and what makes a
// real blur possible: you cannot average pixels that were never drawn.
//
//	ui.Layer(state, card).Blur(24)   // card, blurred
//
// Blur blurs the layer's OWN content — the child subtree it renders — not
// the pixels behind it: there is no backdrop sampling (CSS backdrop-filter,
// a frosted panel over other widgets). For that look, put the content to be
// frosted inside the layer and draw the panel over the blurred result.
//
// The offscreen pass runs between layout and the frame's own paint
// (UI.RenderLayers), so the texture is one build old at most — the same
// content, laid out identically.
//
// The target lives in a LayerState, not in the element: element trees are
// rebuilt every frame, GPU textures must not be.

import (
	"github.com/sushvibez/gfx/render"
	"unsafe"
)

// LayerRenders counts offscreen layer renders (for GFX_FPS: a layer that
// re-renders every frame is a layer whose skip test is failing).
var LayerRenders int

// LayerRenderWhy counts the reason a layer re-rendered: not ready, rect
// changed, content changed, blur changed.
var LayerRenderWhy [4]int

// LayerState retains a layer's offscreen target across frames. Create one
// per layered item and keep it alive (the same way ScrollState or
// InputState are kept).
type LayerState struct {
	layer *render.Layer2D
	ds    int  // downscale factor the target was rendered at
	ready bool // the target holds this frame's content
	// What the target was rendered from: an unchanged box and an unchanged
	// property revision mean the content cannot have changed either, so the
	// offscreen passes are skipped entirely (a static page costs nothing).
	rect Rect
	sig  uint64 // SubtreeSig of the content the target holds
	blur float32
	// pad is how far the target reaches PAST the item's box on every side,
	// so a blur has somewhere to spread: Qt's MultiEffect pads the effect
	// item by its blur (autoPaddingEnabled, on by default) and the blurred
	// blob bleeds outside the rectangle that cast it. Without it our blur
	// was confined to the item's own box — a 200px circle blurred by 60
	// reached exactly the circle's edge and stopped, where Qt's fades on
	// for another 60px.
	pad float32
	// rendered: the target was redrawn this frame, so the composite on
	// screen is out of date (see LayerEl.dirty).
	rendered bool
}

// NewLayerState returns an empty layer state; the target is allocated on
// the first frame that paints it.
func NewLayerState() *LayerState { return &LayerState{} }

// Release frees the layer's texture now. Calling it is optional: the UI
// that paints a layer frees the texture itself once the layer has gone
// unpainted for layerIdleFrames frames, so states that are simply dropped
// (a closed page, a reloaded document) do not keep GPU memory. A released
// layer that is painted again gets a new target.
func (s *LayerState) Release() {
	if s.layer != nil {
		s.layer.Release()
		s.layer = nil
	}
	s.ready = false // the content went with the texture
}

// layerIdleFrames is how many rendered frames a layer may go unpainted
// before its UI frees the target: about five seconds of animation at 60
// frames a second, long enough that a tab switched away and back keeps it.
const layerIdleFrames = 300

type LayerEl struct {
	st       *LayerState
	child    Element
	blur     float32
	opacity  float32
	tint     RGBA
	hasTint  bool
	colorize bool // recolour by luminance instead of multiplying
	shadow   *layerShadow
	// offscreen: render into the texture and paint nothing, because
	// something else samples it (see Offscreen).
	offscreen bool
	rect      Rect
}

// blurPassFactor is each in-texture pass's radius as a fraction of the
// stated blur, for the NARROW blurs that do not go through the chain (see
// chainMinBlur). Two applications of the kernel at half the radius read as
// a smooth gaussian where one shows its taps as a faint ring.
const blurPassFactor = 0.5

// chainMinBlur is the radius, in the layer's own texels, at which the
// downsample chain takes over from the two-pass blur. A chain needs
// levels to build a falloff out of, and below three or four it is a worse
// model of Qt than a plain convolution is. Probed against Qt with a 200px
// circle (RMS of the profile across it, sampled every 10px): at blurMax
// 60 — 30 texels here — the chain scores 2.1 against two passes' 11.8; at
// blurMax 32 (16 texels) two passes still win, 8.5 against 9.8; at
// blurMax 16 they win outright, 3.2 against 10.2.
const chainMinBlur = 20

// layerShadow is a drop shadow cast by the whole layer: the same texture,
// drawn offset, blurred and tinted, under the layer itself.
type layerShadow struct {
	dx, dy, blur float32
	color        RGBA
}

// Layer wraps child in an offscreen layer backed by st.
func Layer(st *LayerState, child Element) *LayerEl {
	return &LayerEl{st: st, child: child, opacity: 1}
}

// Blur sets the gaussian radius (in pixels) applied to the layer's own
// rendered content (its child, not the backdrop behind it) before it is
// composited; 0 draws the layer as-is.
func (e *LayerEl) Blur(px float32) *LayerEl { e.blur = px; return e }

// Opacity scales the whole layer's alpha as one object — which is what
// `layer.enabled` buys in QML: overlapping children stop showing through
// each other.
func (e *LayerEl) Opacity(a float32) *LayerEl { e.opacity = a; return e }

// Offscreen renders the child into the layer's texture and paints NOTHING:
// something else consumes the texture. That is QML's ShaderEffectSource
// with `hideSource: true` — the capture feeds a shader, and drawing it
// again would double it.
func (e *LayerEl) Offscreen() *LayerEl { e.offscreen = true; return e }

// Tint multiplies the layer's colour (Qt's MultiEffect brightness,
// approximated as a multiply).
func (e *LayerEl) Tint(c RGBA) *LayerEl { e.tint, e.hasTint = c, true; return e }

// Colorize recolours the whole captured group to c at its own luminance —
// QML's `MultiEffect { colorization: 1; colorizationColor: c }` over an
// arbitrary item, which is what makes a neon glow one colour instead of
// leaving the source's white highlights white.
func (e *LayerEl) Colorize(c RGBA) *LayerEl {
	e.tint, e.hasTint, e.colorize = c, true, true
	return e
}

// Colorizing reports whether the layer recolours its capture (see
// Colorize) — what a tool or a test asks a built tree.
func (e *LayerEl) Colorizing() bool { return e.colorize }

// Shadow casts the layer's own silhouette as a drop shadow — the whole
// group's shape, not a rounded box, which is what an item made of arbitrary
// drawing (a canvas, a shape) needs (QML's MultiEffect shadowEnabled).
func (e *LayerEl) Shadow(dx, dy, blur float32, c RGBA) *LayerEl {
	e.shadow = &layerShadow{dx: dx, dy: dy, blur: blur, color: c}
	return e
}

func (e *LayerEl) measure(ctx *Context) (float32, float32) { return e.child.measure(ctx) }

func (e *LayerEl) layout(ctx *Context, r Rect) {
	e.rect = r
	e.child.layout(ctx, r)
	if r.W >= 1 && r.H >= 1 {
		ctx.layers = append(ctx.layers, e)
	}
}

// render draws the child into the layer's own target. Called by
// UI.RenderLayers after layout, before the frame's paint.
func (e *LayerEl) render(ctx *Context) {
	if ctx.R == nil || e.rect.W < 1 || e.rect.H < 1 {
		return
	}
	if e.st.layer == nil {
		e.st.layer = render.NewLayer2D(ctx.R)
	}
	// Nothing to redo when the box, the blur and every property the content
	// could read are as they were.
	// Nothing to redo when the box, the blur and the CONTENT are as they
	// were. The content is judged by its paint signature (see SubtreeSig),
	// not by the global property revision: that moves for any write
	// anywhere, and re-rendered every layer on the page every frame an
	// unrelated label changed.
	sig, dirty := SubtreeSig(e.child)
	if e.st.ready && e.st.rect == e.rect && e.st.sig == sig && !dirty && e.st.blur == e.blur {
		e.st.rendered = false
		return
	}
	switch {
	case !e.st.ready:
		LayerRenderWhy[0]++
	case e.st.rect != e.rect:
		LayerRenderWhy[1]++
	case e.st.sig != sig || dirty:
		LayerRenderWhy[2]++
	default:
		LayerRenderWhy[3]++
	}
	e.st.rendered = true // the texture changed: the composite is damaged (see dirty)
	LayerRenders++
	e.st.rect, e.st.sig, e.st.blur = e.rect, sig, e.blur
	// Room for the blur to spread past the item (see LayerState.pad). A
	// plain layer — a shader source, a transparency group — is unpadded, so
	// its texture still maps 1:1 onto the item's rect.
	e.st.pad = 0
	if e.blur > 0 {
		e.st.pad = e.blur
	}
	pad := e.st.pad
	// Render small when it is going to be blurred anyway: the linear
	// sampler does the first half of the blur on the way back up. Never
	// smaller than half, though — the chain does its OWN halving and at
	// 1/8 has nothing left to halve, and even the two-pass blur loses the
	// shape of its falloff there (probed at blurMax 16, where a fifth of
	// the size scores 11.3 against half-size's 5.4, and at blurMax 32,
	// 5.7 against 3.9).
	ds := min(render.BlurDownscale(e.blur), 2)
	s := ctx.R.Scale()
	w, h := int((e.rect.W+2*pad)*s)/ds, int((e.rect.H+2*pad)*s)/ds
	e.st.layer.Resize(w, h)
	e.st.ds = ds
	scale := 1 / float32(ds)
	prevScale := s
	err := e.st.layer.Render(func() {
		// The child paints in window coordinates; map those onto the
		// target: shift the layer's origin to (0,0), then shrink.
		ctx.R.SetScale(prevScale)
		ctx.R.PushTransform(render.Scale2(scale, scale, 0, 0).
			Mul(render.Translate2(-e.rect.X+pad, -e.rect.Y+pad)))
		e.child.paint(ctx)
		ctx.R.PopTransform()
	})
	ctx.R.SetScale(prevScale)
	// The whole blur happens here, in the layer's own (small) texture. It
	// cannot be split with the composite — a convolution is a weighted
	// sum, and only a pass into a cleared target can add its taps up.
	if err == nil && e.blur > 0 {
		// A wide blur goes through a DOWNSAMPLE CHAIN, which is how Qt's
		// MultiEffect makes one and what the frosted pages are made of; a
		// narrow one is two applications of the kernel, which read as a
		// smooth gaussian where one shows its taps as a faint ring.
		if r := e.blur * scale; r < chainMinBlur {
			err = e.st.layer.BlurTexture(r * blurPassFactor)
			if err == nil {
				err = e.st.layer.BlurTexture(r * blurPassFactor)
			}
		} else {
			err = e.st.layer.BlurChain(r)
		}
	}
	ctx.R.SetScale(prevScale)
	e.st.ready = err == nil
}

func (e *LayerEl) childElems() []Element { return []Element{e.child} }

// paintBounds: the composite is padded by the blur, and a shadow reaches
// further still.
func (e *LayerEl) paintBounds() Rect {
	r := inflate(e.rect, e.st.pad)
	if sh := e.shadow; sh != nil {
		r = unionRect(r, inflate(Rect{r.X + sh.dx, r.Y + sh.dy, r.W, r.H}, 3*sh.blur))
	}
	return r
}

// dirty: the offscreen target was re-rendered this frame.
func (e *LayerEl) dirty(*Context) bool { return e.st.rendered || !e.st.ready }

// paintSig hashes what the composite reads (see damage.go).
func (e *LayerEl) paintSig() uint64 {
	h := newSig(0x1e1)
	h.ptr(unsafe.Pointer(e.st))
	h.f32(e.blur)
	h.f32(e.opacity)
	h.b(e.hasTint)
	h.col(e.tint)
	h.b(e.colorize)
	h.b(e.offscreen)
	if e.shadow != nil {
		h.f32(e.shadow.dx)
		h.f32(e.shadow.dy)
		h.f32(e.shadow.blur)
		h.col(e.shadow.color)
	}
	return uint64(h)
}

func (e *LayerEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	if e.offscreen {
		return // the texture is the point; see Offscreen
	}
	img := e.st.image()
	if img == nil || !e.st.ready {
		e.child.paint(ctx) // no target yet: draw straight, unblurred
		return
	}
	c := RGBA{R: 1, G: 1, B: 1, A: 1}
	if e.hasTint {
		c = e.tint
	}
	c.A *= e.opacity
	r := e.rect
	if p := e.st.pad; p > 0 {
		r = Rect{r.X - p, r.Y - p, r.W + 2*p, r.H + 2*p}
	}
	if sh := e.shadow; sh != nil && sh.color.A > 0 {
		// The shadow is the same texture's SILHOUETTE, offset and blurred:
		// the group's real outline in the shadow colour. Tinting the
		// texture instead multiplies the shadow colour by the content, so
		// anything dark cast a black shadow however warm the colour was
		// (the paper diorama's black trees, shadowed rgba(.08,.02,0,.7)).
		ctx.R.DrawImageBlurredStencil(img, r.X+sh.dx, r.Y+sh.dy,
			r.W, r.H, sh.blur, ctx.color(sh.color))
	}
	// The blur happened in the texture (render), where it could be a
	// proper weighted sum of premultiplied samples; the composite is one
	// ordinary draw of the result. It has to say the texels are
	// PREMULTIPLIED — anything rendered into a transparent target is — or
	// the alpha is applied a second time and a translucent captured item
	// comes out at alpha squared (a blob painted at 0.3 composited at
	// 0.084, a third of Qt's).
	if e.colorize {
		ctx.R.DrawImageBlurredColorized(img, r.X, r.Y, r.W, r.H, 0, ctx.color(c))
		return
	}
	ctx.R.DrawImagePremul(img, r.X, r.Y, r.W, r.H, ctx.color(c))
}

// Image is the state's rendered texture: what an offscreen layer was
// captured into, for a caller that samples it (a ShaderEffect's source).
// nil until the first frame has rendered it.
func (s *LayerState) Image() *render.Image {
	if !s.ready {
		return nil
	}
	return s.image()
}

// Rect is the box the layer was captured over (for tooling and tests).
func (s *LayerState) Rect() Rect { return s.rect }

// Ready reports whether the texture holds this frame's content.
func (s *LayerState) Ready() bool { return s.ready }

// image is the state's rendered texture, if any.
func (s *LayerState) image() *render.Image {
	if s.layer == nil {
		return nil
	}
	return s.layer.Image()
}

func (e *LayerEl) hit(x, y float32) Element { return e.child.hit(x, y) }
func (e *LayerEl) bounds() Rect             { return e.rect }
func (e *LayerEl) onClick() func()          { return nil }

// growFactor/fixedWH/minWH forward the child's layout hints, so wrapping an
// item in a layer does not change where it lands.
func (e *LayerEl) growFactor() float32 {
	if g, ok := e.child.(interface{ growFactor() float32 }); ok {
		return g.growFactor()
	}
	return 0
}

func (e *LayerEl) fixedWH() (float32, float32) {
	if f, ok := e.child.(interface{ fixedWH() (float32, float32) }); ok {
		return f.fixedWH()
	}
	return 0, 0
}

func (e *LayerEl) minWH() (float32, float32) {
	if m, ok := e.child.(interface{ minWH() (float32, float32) }); ok {
		return m.minWH()
	}
	return 0, 0
}

// RenderLayers draws every layered item registered by the last layout into
// its own offscreen target. Call after Layout and before the frame's main
// pass (the shells do); it submits its own command buffers, so the
// renderer's vertex buffers are free again by the time the frame flushes.
func (u *UI) RenderLayers() {
	// Deepest first: a layer nested inside another must be ready before the
	// outer one paints it.
	for i := len(u.ctx.layers) - 1; i >= 0; i-- {
		u.ctx.layers[i].render(&u.ctx)
	}
	u.sweepLayers()
}

// sweepLayers frees the targets of layers this UI has not painted for
// layerIdleFrames frames (see LayerState.Release).
func (u *UI) sweepLayers() {
	u.layerTick++
	if u.layerSeen == nil {
		u.layerSeen = map[*LayerState]uint64{}
	}
	for _, e := range u.ctx.layers {
		u.layerSeen[e.st] = u.layerTick
	}
	for st, seen := range u.layerSeen {
		if u.layerTick-seen > layerIdleFrames {
			st.Release()
			delete(u.layerSeen, st)
		}
	}
}

// ReleaseLayers frees every layer target this UI has painted, for a
// window that is closing while its device lives on.
func (u *UI) ReleaseLayers() {
	for st := range u.layerSeen {
		st.Release()
	}
	clear(u.layerSeen)
}
