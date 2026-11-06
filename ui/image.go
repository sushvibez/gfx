package ui

import (
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/render"
)

// ImageEl draws a GPU image (load it once with Renderer2D.LoadImage or
// NewImage, outside the per-frame view) at a fixed box, scaled to fit with
// preserved aspect ratio.
type ImageEl struct {
	img      *render.Image
	w, h     float32
	tint     RGBA
	colorize bool // recolour by luminance instead of multiplying (see Colorize)
	rect     Rect
}

// Img builds an image element of the given box size. A zero w or h takes
// the source's own size for that axis, which is what an unsized QML Image
// does (implicitWidth/Height are the source's).
func Img(img *render.Image, w, h float32) *ImageEl {
	if img != nil {
		if w == 0 {
			w = float32(img.W)
		}
		if h == 0 {
			h = float32(img.H)
		}
	}
	return &ImageEl{img: img, w: w, h: h, tint: render.White}
}

// Tint multiplies the image by a color (e.g. to fade or recolor).
func (e *ImageEl) Tint(c RGBA) *ImageEl { e.tint = c; return e }

// Colorize recolours the image to c at its own LUMINANCE instead of
// multiplying by it — a monochrome icon takes the colour whatever its own
// hue was. QML's `icon.color` on a control, which the corpus' ToolButton
// style spells as a MultiEffect colorization over the icon.
func (e *ImageEl) Colorize(c RGBA) *ImageEl { e.tint, e.colorize = c, true; return e }

func (e *ImageEl) measure(*Context) (float32, float32) { return e.w, e.h }
func (e *ImageEl) layout(_ *Context, r Rect)           { e.rect = r }

// paintSig: the image, its box and its tint (see damage.go).
func (e *ImageEl) paintSig() uint64 {
	h := newSig(0x1a1)
	h.ptr(unsafe.Pointer(e.img))
	h.f32(e.w)
	h.f32(e.h)
	h.col(e.tint)
	h.b(e.colorize)
	return uint64(h)
}

func (e *ImageEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	switch {
	case e.img == nil:
	case e.colorize:
		ctx.R.DrawImageFitColorized(e.img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
	default:
		ctx.R.DrawImageFit(e.img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
	}
}
func (e *ImageEl) hit(float32, float32) Element { return nil }
func (e *ImageEl) onClick() func()              { return nil }
func (e *ImageEl) bounds() Rect                 { return e.rect }
func (e *ImageEl) fixedWH() (float32, float32)  { return e.w, e.h }

// ImageCell is a retained, lazily-decoded image: source bytes are set once
// (off the per-frame path), and the GPU image is decoded+uploaded on first
// paint when a Renderer2D is available. Hold one across frames (e.g. in a
// document's state) so an image loads only once.
type ImageCell struct {
	Data []byte // source PNG/JPEG/GIF bytes (nil/empty = no image)
	img  *render.Image
	done bool
	// decodedAt is the box size the bytes were decoded for, and atSource
	// says the decode was already the source's own resolution. A cell that
	// first paints small (a layout still settling, a thumbnail that grows
	// into a full-size view) would otherwise keep that first small decode
	// for ever, upscaled — the corpus' lens photo stayed visibly soft
	// against Qt's.
	decodedAt int
	atSource  bool

	// Animated GIF state: one image per frame plus its delay (centiseconds).
	frames  []*render.Image
	delays  []int
	totalCs int // sum of delays, centiseconds (0 = not animated)
}

// needsRedecode reports whether a bigger box wants a bigger decode: only
// while the source has more pixels to give, and only in real steps, so a
// box that grows a little every frame does not re-decode every frame.
func (c *ImageCell) needsRedecode(maxDim int) bool {
	return c.done && !c.atSource && maxDim > c.decodedAt*5/4
}

func (c *ImageCell) ensure(r *render.Renderer2D) *render.Image {
	return c.ensureAt(r, 0, time.Now())
}

// ensureSized decodes on first use, box-downscaling sources much larger than
// maxDim (the display box, with headroom) so minification doesn't alias.
func (c *ImageCell) ensureSized(r *render.Renderer2D, maxDim int) *render.Image {
	return c.ensureAt(r, maxDim, time.Now())
}

// EnsureSized is ensureSized for a caller outside the widget set — a
// ShaderEffect whose source is an Image samples the image's OWN texture, as
// Qt does (an Image is a texture provider there).
func (c *ImageCell) EnsureSized(r *render.Renderer2D, maxDim int) *render.Image {
	return c.ensureSized(r, maxDim)
}

// ensureAt is ensureSized rendering animated frames at an explicit time —
// the shell freezes it to pause GIFs in unfocused windows.
func (c *ImageCell) ensureAt(r *render.Renderer2D, maxDim int, now time.Time) *render.Image {
	if c.needsRedecode(maxDim) {
		c.Release()
	}
	if !c.done {
		c.done = true
		c.decodedAt = maxDim
		if len(c.Data) > 0 {
			if frames, delays, err := r.LoadGIFFramesScaled(c.Data, maxDim); err == nil && len(frames) > 1 {
				c.frames, c.delays = frames, delays
				for _, d := range delays {
					if d <= 0 {
						d = 10 // GIFs with 0 delay default to ~100ms
					}
					c.totalCs += d
				}
				c.img = frames[0]
			} else if img, err := r.LoadImageScaled(c.Data, maxDim); err == nil {
				c.img = img
				// The decode came back smaller than the box asked for:
				// that IS the source's resolution, so there is nothing to
				// gain from decoding again.
				c.atSource = maxDim <= 0 || img.W < maxDim && img.H < maxDim
			}
		}
	}
	if c.totalCs > 0 {
		return c.frameAt(now)
	}
	return c.img
}

// SetData replaces the cell's encoded bytes and re-decodes on next paint —
// for images that arrive asynchronously (network fetches) after the cell
// already painted its placeholder. Call from the UI goroutine.
func (c *ImageCell) SetData(b []byte) {
	c.Release()
	c.Data = b
}

// Release frees the cell's GPU textures and resets it to undecoded. Call it
// (UI goroutine) when discarding a cell — e.g. swapping a gallery to a new
// batch — or its texture memory lives until process exit; wgpu keeps the
// textures alive under any still in-flight frame, so releasing between
// frames is safe.
func (c *ImageCell) Release() {
	for _, f := range c.frames {
		f.Release()
	}
	if c.img != nil && len(c.frames) == 0 {
		c.img.Release()
	}
	c.done = false
	c.img = nil
	c.frames = nil
	c.delays = nil
	c.totalCs = 0
}

// Animated reports whether this cell is a multi-frame GIF (so the host can
// keep the render loop ticking).
func (c *ImageCell) Animated() bool { return c.totalCs > 0 }

// frameAt selects the frame to show at wall-clock time now, looping over the
// total GIF duration.
func (c *ImageCell) frameAt(now time.Time) *render.Image {
	if c.totalCs <= 0 {
		return c.img
	}
	ms := now.UnixMilli() % int64(c.totalCs*10) // centiseconds -> ms
	var acc int64
	for i, d := range c.delays {
		if d <= 0 {
			d = 10
		}
		acc += int64(d) * 10
		if ms < acc {
			return c.frames[i]
		}
	}
	return c.frames[len(c.frames)-1]
}

// LazyImageEl draws an ImageCell fit to its box, decoding on first paint.
// When the cell has no usable image it paints a muted placeholder box.
type LazyImageEl struct {
	cell     *ImageCell
	w, h     float32
	tint     RGBA
	colorize bool // tint recolours by luminance instead of multiplying
	stretch  bool // fillMode: Stretch (default fits with preserved aspect)
	natW     float32
	natH     float32 // the SOURCE's own pixel size (see Natural)
	crop     bool    // fillMode: PreserveAspectCrop — cover the box and clip
	grow     bool    // expand to fill the parent flex box (both axes)
	growWt   float32 // Grow WEIGHT (0 = the plain 1); see DivEl.GrowBy
	rect     Rect
}

// LazyImage builds an image element backed by a retained cell.
func LazyImage(cell *ImageCell, w, h float32) *LazyImageEl {
	return &LazyImageEl{cell: cell, w: w, h: h, tint: render.White}
}

func (e *LazyImageEl) Tint(c RGBA) *LazyImageEl { e.tint = c; return e }

// Colorize recolours the image to c at the source's own luminance, keeping
// its alpha — QML's `MultiEffect { colorization: 1; colorizationColor: c }`,
// which is how a Controls style tints an icon. Unlike Tint (a multiply) it
// works on a coloured source.
func (e *LazyImageEl) Colorize(c RGBA) *LazyImageEl { e.tint, e.colorize = c, true; return e }

// Stretch fills the box ignoring aspect ratio (QML Image.Stretch); the default
// preserves aspect (PreserveAspectFit).
func (e *LazyImageEl) Stretch() *LazyImageEl { e.stretch = true; return e }

// Crop covers the box, keeping the aspect ratio and clipping the overflow
// — QML's Image.PreserveAspectCrop. A photo behind a card fills its frame
// that way instead of sitting letterboxed in it.
func (e *LazyImageEl) Crop() *LazyImageEl { e.crop = true; return e }

// Natural records the source image's own pixel size. QML reports that as an
// Image's implicitWidth/implicitHeight — independent of the box the item
// ends up in, which matters when the LAYOUT depends on it: the corpus'
// blueprint viewer sizes its pan area from `blueprintImage.implicitWidth`
// and would otherwise divide by the zero it read back from a box that was
// waiting on the same number.
func (e *LazyImageEl) Natural(w, h float32) *LazyImageEl {
	e.natW, e.natH = w, h
	return e
}

// natural is ui.Implicit's hook: an Image's implicit size is its source's.
func (e *LazyImageEl) natural() (float32, float32) { return e.natW, e.natH }

// Grow expands the image to fill its parent flex box on both axes (the box
// then fits/stretches the frame within it). Use it to fill a window.
func (e *LazyImageEl) Grow() *LazyImageEl { e.grow = true; return e }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (e *LazyImageEl) GrowBy(f float32) *LazyImageEl {
	e.grow, e.growWt = true, f
	return e
}

func (e *LazyImageEl) growFactor() float32 {
	if e.grow {
		if e.growWt > 0 {
			return e.growWt
		}
		return 1
	}
	return 0
}

func (e *LazyImageEl) measure(*Context) (float32, float32) {
	if e.grow {
		return 0, 0
	}
	// With no size of its own an image measures as its SOURCE — QML's
	// implicit size, and the floor a layout may not squeeze it below. The
	// images page's 9-patch tiles are 90x90 sources in a two-column grid
	// whose columns are narrower than that: Qt hands them their 90 and
	// lets the grid overflow, where measuring 0 let ours shrink them to
	// the 79 the cell had.
	w, h := e.w, e.h
	if w == 0 {
		w = e.natW
	}
	if h == 0 {
		h = e.natH
	}
	return w, h
}
func (e *LazyImageEl) layout(_ *Context, r Rect) { e.rect = r }

// paintSig: the cell (its decode is retained state, so the pointer is the
// identity), the box and the tint; a playing GIF is dirty every frame.
func (e *LazyImageEl) paintSig() uint64 {
	h := newSig(0x1a2)
	h.ptr(unsafe.Pointer(e.cell))
	h.f32(e.w)
	h.f32(e.h)
	h.col(e.tint)
	h.b(e.colorize)
	return uint64(h)
}

func (e *LazyImageEl) dirty(*Context) bool { return e.cell != nil && e.cell.Animated() }

func (e *LazyImageEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	// Decode sized to the display box (2x headroom keeps rescaling crisp);
	// huge sources are box-downscaled instead of aliasing in the sampler.
	maxDim := int(2 * max(e.rect.W, e.rect.H))
	now := ctx.animT
	if now.IsZero() {
		now = time.Now()
	}
	if img := e.cell.ensureAt(ctx.R, maxDim, now); img != nil {
		if e.cell.Animated() {
			// A playing GIF needs the frame loop running even when nothing
			// else changes — surface that to the shell's damage check.
			ctx.wantsFrame = true
		}
		switch {
		case e.crop:
			ctx.R.DrawImageCropped(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
		case e.colorize && e.stretch:
			ctx.R.DrawImageColorized(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
		case e.colorize:
			ctx.R.DrawImageFitColorized(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
		case e.stretch:
			ctx.R.DrawImage(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
		default:
			ctx.R.DrawImageFit(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(e.tint))
		}
		return
	}
	// No source bytes yet (an async fetch still in flight): reserve the box
	// but paint nothing, like gpui's img() while loading.
	if len(e.cell.Data) == 0 {
		return
	}
	// Decode failed: a muted, bordered box so a broken source stays visible.
	ctx.R.Quad(e.rect.X, e.rect.Y, e.rect.W, e.rect.H, ctx.color(RGB(0x24242f)), 6, 1, ctx.color(RGB(0x44445a)))
}
func (e *LazyImageEl) hit(float32, float32) Element { return nil }
func (e *LazyImageEl) onClick() func()              { return nil }
func (e *LazyImageEl) bounds() Rect                 { return e.rect }
func (e *LazyImageEl) fixedWH() (float32, float32) {
	if e.grow {
		return 0, 0 // let the flex box stretch us on the cross axis too
	}
	return e.w, e.h
}
