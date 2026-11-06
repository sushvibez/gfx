package render

// Layer2D is an offscreen 2D target: part of a UI draws into it once, and
// the frame that follows samples the result as an ordinary image. That is
// QML's `layer.enabled` — and the only honest way to blur an item, since
// blurring means averaging pixels that have to exist first.
//
//	l := render.NewLayer2D(r2)
//	l.Resize(w, h)          // in TARGET pixels (see Downscale)
//	l.Render(func() { paintTheSubtree() })
//	r2.DrawImageBlurred(l.Image(), x, y, w, h, radius, ui.White)
//
// Render submits its own command buffer. The renderer keeps one set of
// vertex buffers and refills them per flush, so a layer pass and the main
// pass cannot share a submission — separating them is what makes reusing
// the SAME renderer (and therefore its font atlases) safe.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/gpu"
)

type Layer2D struct {
	r2     *Renderer2D
	device *gpu.Device

	color     *gpu.Texture
	colorView *gpu.TextureView
	img       *Image
	// scratch is the other half of the ping-pong a blur pass needs: it
	// reads the whole texture, so it cannot write into it.
	scratch     *gpu.Texture
	scratchView *gpu.TextureView
	scratchImg  *Image
	msaa        *gpu.Texture // MSAA attachment when the renderer is multisampled
	msaaView    *gpu.TextureView
	w, h        int
	// mips is the downsample chain BlurChain works through: level i is
	// half the size of level i-1. Allocated lazily and kept, since a
	// blurred layer re-blurs every time its content changes.
	mips []mipLevel
}

// mipLevel is one rung of the downsample chain: a half-size render target
// plus the two things needed to draw INTO it and to sample FROM it.
type mipLevel struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	img  *Image
	// A multisampled renderer's pipelines can only draw into a matching
	// attachment, so each level carries its own when the renderer is one.
	msaa     *gpu.Texture
	msaaView *gpu.TextureView
	w, h     int
}

// NewLayer2D creates an (initially empty) offscreen target for r2's format
// and sample count.
func NewLayer2D(r2 *Renderer2D) *Layer2D {
	return &Layer2D{r2: r2, device: r2.Device()}
}

// Resize (re)allocates the target; idempotent for an unchanged size.
func (l *Layer2D) Resize(w, h int) {
	w, h = max(w, 1), max(h, 1)
	if w == l.w && h == l.h {
		return
	}
	l.release()
	l.w, l.h = w, h
	format := l.r2.Format()
	l.color = l.device.CreateTexture2D(w, h, format, gpu.TextureRenderAttachment|gpu.TextureBinding)
	l.colorView = l.color.CreateView()
	if l.r2.Samples() > 1 {
		l.msaa = l.device.CreateMSAATexture(w, h, format, l.r2.Samples())
		l.msaaView = l.msaa.CreateView()
	}
	l.img = l.r2.ImageFromView(l.colorView, w, h)
}

// ensureScratch allocates the ping-pong half lazily: only blurred layers
// need it.
func (l *Layer2D) ensureScratch() {
	if l.scratch != nil || l.w == 0 {
		return
	}
	format := l.r2.Format()
	l.scratch = l.device.CreateTexture2D(l.w, l.h, format, gpu.TextureRenderAttachment|gpu.TextureBinding)
	l.scratchView = l.scratch.CreateView()
	l.scratchImg = l.r2.ImageFromView(l.scratchView, l.w, l.h)
}

// Size is the target's pixel size (0,0 before the first Resize).
func (l *Layer2D) Size() (int, int) { return l.w, l.h }

// Image is the sampleable result (nil before the first Resize).
func (l *Layer2D) Image() *Image { return l.img }

// Render draws into the layer: it begins a frame sized to the target, runs
// draw (which queues primitives on the renderer), then flushes into the
// layer's own pass and submits it. The target starts fully transparent, so
// whatever the caller does not paint shows the page behind it.
func (l *Layer2D) Render(draw func()) error {
	if l.colorView == nil {
		return nil // not yet sized
	}
	l.r2.Begin(l.w, l.h)
	draw()
	enc := l.device.CreateCommandEncoder()
	var pass *gpu.RenderPass
	if l.msaaView != nil {
		pass = enc.BeginRenderPassResolve(l.msaaView, l.colorView, gpu.Color{})
	} else {
		pass = enc.BeginRenderPass(l.colorView, gpu.Color{})
	}
	err := l.r2.Flush(pass)
	pass.End()
	l.r2.Queue().Submit(enc.Finish())
	return err
}

// BlurTexture blurs the layer's own contents in place: it draws the
// texture through the tap kernel into the ping-pong half and swaps. Doing
// one pass here and one while compositing squares the kernel — 13 taps
// become 169 — which is what turns a visible ring of copies into a smooth
// falloff, and it is cheap because the layer is small.
func (l *Layer2D) BlurTexture(radius float32) error {
	if l.colorView == nil || radius <= 0 {
		return nil
	}
	l.ensureScratch()
	l.r2.Begin(l.w, l.h)
	// The layer's pixels are PREMULTIPLIED (they were composited over a
	// transparent target) and a blur is a weighted SUM of them, so the
	// taps are drawn additively into the cleared scratch half with weights
	// that add to one. Compositing them over one another instead only
	// approximates the sum while the samples are opaque: a translucent
	// captured item gained alpha it never had (a blob painted at 0.3 came
	// out at 0.55 after one pass).
	alphas := blurTapWeights()
	for i, t := range blurTaps {
		l.r2.DrawImagePremulAdd(l.img, t[0]*radius, t[1]*radius,
			float32(l.w), float32(l.h), RGBA{A: alphas[i]})
	}
	enc := l.device.CreateCommandEncoder()
	var pass *gpu.RenderPass
	if l.msaaView != nil {
		pass = enc.BeginRenderPassResolve(l.msaaView, l.scratchView, gpu.Color{})
	} else {
		pass = enc.BeginRenderPass(l.scratchView, gpu.Color{})
	}
	err := l.r2.Flush(pass)
	pass.End()
	l.r2.Queue().Submit(enc.Finish())
	// The blurred copy becomes the layer's image.
	l.color, l.scratch = l.scratch, l.color
	l.colorView, l.scratchView = l.scratchView, l.colorView
	l.img, l.scratchImg = l.scratchImg, l.img
	return err
}

// ensureMips allocates (or reuses) a downsample chain of n levels, each
// half the size of the one above it.
func (l *Layer2D) ensureMips(n int) {
	if len(l.mips) >= n && (n == 0 || l.mips[0].w == max(l.w/2, 1)) {
		return
	}
	l.releaseMips()
	format := l.r2.Format()
	w, h := l.w, l.h
	for i := 0; i < n; i++ {
		w, h = max(w/2, 1), max(h/2, 1)
		tex := l.device.CreateTexture2D(w, h, format, gpu.TextureRenderAttachment|gpu.TextureBinding)
		view := tex.CreateView()
		lv := mipLevel{tex: tex, view: view, img: l.r2.ImageFromView(view, w, h), w: w, h: h}
		if l.r2.Samples() > 1 {
			lv.msaa = l.device.CreateMSAATexture(w, h, format, l.r2.Samples())
			lv.msaaView = lv.msaa.CreateView()
		}
		l.mips = append(l.mips, lv)
	}
}

func (l *Layer2D) releaseMips() {
	for i := range l.mips {
		if l.mips[i].img != nil {
			l.mips[i].img.Release()
		}
		if l.mips[i].view != nil {
			l.mips[i].view.Release()
		}
		if l.mips[i].msaaView != nil {
			l.mips[i].msaaView.Release()
		}
		if l.mips[i].msaa != nil {
			l.mips[i].msaa.Release()
		}
		if l.mips[i].tex != nil {
			l.mips[i].tex.Release()
		}
	}
	l.mips = nil
}

// pass renders draw into view, clearing it first. The layer's own MSAA
// attachment is only sized for the full-size target, so the chain's
// levels resolve nothing — they are single-sampled copies.
func (l *Layer2D) pass(view, msaa *gpu.TextureView, w, h int, draw func()) error {
	l.r2.Begin(w, h)
	draw()
	enc := l.device.CreateCommandEncoder()
	var rp *gpu.RenderPass
	if msaa != nil {
		rp = enc.BeginRenderPassResolve(msaa, view, gpu.Color{})
	} else {
		rp = enc.BeginRenderPass(view, gpu.Color{})
	}
	err := l.r2.Flush(rp)
	rp.End()
	l.r2.Queue().Submit(enc.Finish())
	return err
}

// BlurChain blurs the layer's contents the way Qt's MultiEffect does: by
// DOWNSAMPLING through a chain of half-size targets and summing the levels
// back together, instead of convolving once at the full radius.
//
// The difference is the shape of the falloff, and it is the whole reason
// the frosted pages never quite matched. Two gaussian passes put most of
// their energy near the source and reach about 0.6 of the nominal radius;
// a sum of successively coarser levels carries much further with a flatter
// shoulder — probed against Qt with a 200px circle at blurMax 60, its
// falloff runs 50px past the circle where two passes reach 35, and the
// profile through the middle differs by more than the extent does.
//
// Level i is blurred with the ordinary tap kernel at a radius of about
// one of ITS OWN texels, which is 2^i in the layer's pixels: the chain
// covers the whole range geometrically, which is what makes the tail long
// without smearing the core. It only works from a few levels up, though —
// see ui.chainMinBlur for where the two-pass blur is still the better
// model.
func (l *Layer2D) BlurChain(radius float32) error {
	if l.colorView == nil || radius <= 0 {
		return nil
	}
	// How many halvings it takes for one texel of the coarsest level to
	// span the radius. Two levels is the floor (below that the chain is
	// just a blur), five the ceiling Qt itself uses.
	levels := int(math32.Ceil(math32.Log2(max(radius, 2)))) - 1
	levels = min(max(levels, 2), 5)
	l.ensureMips(levels)
	l.ensureScratch()

	// Down the chain: each level is the one above it, drawn at half size
	// through the linear sampler (which is itself a 2x2 box filter) and
	// then blurred by a texel of its own.
	src := l.img
	// The tap ring's radius in each level's own texels, chosen so the
	// chain's total reach (the geometric sum 1+2+4+... of the levels'
	// radii, in layer pixels) is the radius asked for. Fixing the ring at
	// a constant instead quantized the blur to powers of two: at blurMax
	// 32 our falloff ran 20px wider than Qt's while matching it at 60.
	tap := radius / float32(int(1)<<uint(levels)-1)
	alphas := blurTapWeights()
	for i := range alphas {
		alphas[i] *= addGain
	}
	for i := range l.mips[:levels] {
		m := l.mips[i]
		w, h := m.w, m.h
		// Downsample AND convolve in the same pass: the tap ring is drawn
		// at the destination's scale, so each level is a blur of the one
		// above it at twice the reach. Halving alone would only give the
		// sampler's 2x2 box, which upscales back to a soft EDGE and not to
		// a blur (probed: a 32px square through a plain chain came back
		// square).
		if err := l.pass(m.view, m.msaaView, w, h, func() {
			for j, t := range blurTaps {
				l.r2.DrawImagePremulAdd(src, t[0]*tap, t[1]*tap,
					float32(w), float32(h), RGBA{A: alphas[j]})
			}
		}); err != nil {
			return err
		}
		src = m.img
	}

	// Back up: every level is summed into the scratch half at full size,
	// weighted so the whole sums to one. The coarse levels carry the tail,
	// the fine ones keep the core from turning to mush.
	weights := chainWeights(levels)
	for i := range weights {
		weights[i] *= addGain
	}
	if err := l.pass(l.scratchView, l.msaaView, l.w, l.h, func() {
		for i := range l.mips[:levels] {
			l.r2.DrawImagePremulAdd(l.mips[i].img, 0, 0,
				float32(l.w), float32(l.h), RGBA{A: weights[i]})
		}
	}); err != nil {
		return err
	}
	l.color, l.scratch = l.scratch, l.color
	l.colorView, l.scratchView = l.scratchView, l.colorView
	l.img, l.scratchImg = l.scratchImg, l.img
	return nil
}

// addGain compensates the rounding of an 8-bit target: every additive tap
// is written back a shade low, and the chain's five passes turned a solid
// white layer into 250/255 — a 2% dimming across every frosted panel.
// Measured: 1.004 restores 255 for two to four levels, and the extra
// clamps away where the sum is already saturated.
const addGain = 1.005

// chainWeights are the level weights BlurChain sums with, normalised to
// one. Qt weights the coarse end heavily — that is what carries the tail
// — so the weights grow with the level.
func chainWeights(n int) []float32 {
	out := make([]float32, n)
	var total float32
	for i := range out {
		out[i] = float32(i + 1)
		total += out[i]
	}
	for i := range out {
		out[i] /= total
	}
	return out
}

// runningAlphas turns a normalized weight list into the OVER alphas that
// reproduce it when the images are drawn one atop the last: each entry is
// that image's share of the weight drawn so far (see blurTapAlphas).
func runningAlphas(w []float32) []float32 {
	out := make([]float32, len(w))
	var acc float32
	for i, x := range w {
		acc += x
		out[i] = x / acc
	}
	return out
}

// Release frees the target textures.
func (l *Layer2D) Release() { l.release(); l.releaseMips(); l.w, l.h = 0, 0 }

func (l *Layer2D) release() {
	l.releaseMips()
	if l.img != nil {
		l.img.Release()
		l.img = nil
	}
	if l.scratchImg != nil {
		l.scratchImg.Release()
		l.scratchImg = nil
	}
	if l.scratchView != nil {
		l.scratchView.Release()
		l.scratchView = nil
	}
	if l.scratch != nil {
		l.scratch.Release()
		l.scratch = nil
	}
	if l.msaaView != nil {
		l.msaaView.Release()
		l.msaaView = nil
	}
	if l.msaa != nil {
		l.msaa.Release()
		l.msaa = nil
	}
	if l.colorView != nil {
		l.colorView.Release()
		l.colorView = nil
	}
	if l.color != nil {
		l.color.Release()
		l.color = nil
	}
}

// BlurDownscale is the factor a layer should render at for a given blur
// radius: half the work of the blur is done by rendering small and letting
// the linear sampler smooth the way back up, which is what keeps a 32px
// blur affordable. 1 means full resolution.
func BlurDownscale(radius float32) int {
	switch {
	case radius <= 2:
		return 1
	case radius >= 32:
		return 8
	default:
		return int(radius/4) + 1
	}
}

// blurTaps is the offset/weight kernel DrawImageBlurred sums, in units of
// the blur radius: a ring at half the radius and one at the radius, around
// a centre tap. Two rings rather than one keep the falloff smooth — a
// single ring reads as a handful of offset copies.
var blurTaps = [13][3]float32{
	{0, 0, 6},
	{-0.5, 0, 3}, {0.5, 0, 3}, {0, -0.5, 3}, {0, 0.5, 3},
	{-0.4, -0.4, 2}, {0.4, -0.4, 2}, {-0.4, 0.4, 2}, {0.4, 0.4, 2},
	{-1, 0, 1}, {1, 0, 1}, {0, -1, 1}, {0, 1, 1},
}

// blurTapAlphas is each tap's alpha when the taps are drawn in order, one
// over the last: a tap's share of the weight drawn SO FAR. That leaves the
// running result the weighted average of every tap — draw them at their
// bare weights instead and the last tap dominates, which reads as a pile of
// ghosts rather than a blur.
func blurTapAlphas() []float32 {
	out := make([]float32, len(blurTaps))
	var acc float32
	for i, t := range blurTaps {
		acc += t[2]
		out[i] = t[2] / acc
	}
	return out
}

// blurTapWeights are the taps' weights NORMALIZED to sum to one — the
// kernel of a proper convolution, for the additive premultiplied pass.
func blurTapWeights() []float32 {
	out := make([]float32, len(blurTaps))
	var total float32
	for _, t := range blurTaps {
		total += t[2]
	}
	for i, t := range blurTaps {
		out[i] = t[2] / total
	}
	return out
}

// DrawImageBlurred draws img into the rect blurred by an approximate
// gaussian of the given radius (in destination pixels). The taps are drawn
// back to front with alpha weights that make the result their weighted
// average, so it reads as a blur rather than a pile of ghosts. radius <= 0
// draws the image plainly.
func (r *Renderer2D) DrawImageBlurred(img *Image, x, y, w, h, radius float32, tint RGBA) {
	r.drawImageBlurred(img, x, y, w, h, radius, tint, drawPlain)
}

// DrawImageBlurredPremul is DrawImageBlurred for a PREMULTIPLIED texture —
// an offscreen layer's own target. See DrawImagePremul.
func (r *Renderer2D) DrawImageBlurredPremul(img *Image, x, y, w, h, radius float32, tint RGBA) {
	r.drawImageBlurred(img, x, y, w, h, radius, tint, drawPremul)
}

// DrawImageBlurredStencil is DrawImageBlurred through the silhouette path:
// the image's shape, blurred, in one flat colour. See DrawImageStencil.
func (r *Renderer2D) DrawImageBlurredStencil(img *Image, x, y, w, h, radius float32, c RGBA) {
	r.drawImageBlurred(img, x, y, w, h, radius, c, drawStencil)
}

// DrawImageBlurredColorized is DrawImageBlurred through the colorize path:
// every texel takes the colour's hue at the source's own luminance. That is
// what a `MultiEffect { blurEnabled: true; colorization: 1 }` draws — a
// neon glow whose source colours have all become the glow colour — where a
// tint multiply would leave a white highlight white.
func (r *Renderer2D) DrawImageBlurredColorized(img *Image, x, y, w, h, radius float32, c RGBA) {
	r.drawImageBlurred(img, x, y, w, h, radius, c, drawColorized)
}

// drawMode selects which sprite pipeline a blurred draw goes through.
type drawMode int

const (
	drawPlain drawMode = iota
	drawColorized
	drawPremul
	drawStencil
)

func (r *Renderer2D) drawImageBlurred(img *Image, x, y, w, h, radius float32, tint RGBA, mode drawMode) {
	if img == nil {
		return
	}
	draw := r.DrawImage
	switch mode {
	case drawColorized:
		draw = r.DrawImageColorized
	case drawPremul:
		draw = r.DrawImagePremul
	case drawStencil:
		draw = r.DrawImageStencil
	}
	if radius <= 0 {
		draw(img, x, y, w, h, tint)
		return
	}
	alphas := blurTapAlphas()
	for i, t := range blurTaps {
		c := tint
		c.A *= alphas[i]
		if c.A <= 0 {
			continue
		}
		draw(img, x+t[0]*radius, y+t[1]*radius, w, h, c)
	}
}
