package render

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/msdf"
)

// Distance-field text: one sheet per font FAMILY, drawn at any size.
//
// The rasterised atlas this sits beside keeps a sheet of coverage masks
// per pixel size, because a mask is only correct at the size it was
// rasterised at. A UI that draws 12, 13 and 34 px text — a body, a label
// and a heading — carries three sheets and rasterises every glyph three
// times, and animating a size rasterises a fresh sheet per frame. The
// distance field is scale-free: one sheet, and the size is a multiply in
// the vertex.
//
// What it costs is small text. A coverage mask is hinted and gamma-
// corrected against the pixel grid; a distance field is not, so at 11 px
// the stems are softer than the subpixel path's. So this is not the
// default — it is what headings, scaled, rotated and animated text want,
// and what an outline or a glow needs, since those are a threshold shift
// in a field and a second atlas in a mask.

// msdfFamily is the sheet shared by every size derived from one face.
// It hangs off the Font that parsed the face and is handed down by
// AtSize, so a family that draws at eight sizes still builds one.
type msdfFamily struct {
	atlas *msdf.Atlas
	tex   *Image
	// One bind group per parameter slot, since the slot's uniform is
	// part of the group. There are as many slots as distinct weight/
	// outline/glow settings a frame uses, which in practice is one or
	// two.
	binds map[int]*gpu.BindGroup
}

// SetMSDFText turns the distance-field text path on. It applies to every
// font, and takes precedence over the grayscale and subpixel paths.
func (r *Renderer2D) SetMSDFText(on bool) { r.msdfText = on && r.msdfPipeline != nil }

// MSDFTextEnabled reports whether distance-field text is in use.
func (r *Renderer2D) MSDFTextEnabled() bool { return r.msdfText && r.msdfPipeline != nil }

// SetMSDFAuto chooses the path per draw instead of for everything: the
// distance field for text drawn scaled, rotated or sheared, or at a size
// other than the one its font was rasterised at, and the hinted raster
// for text drawn as it was rasterised. That is where each measured
// better (sdf/TODO.md): at its own size the raster's coverage mask has
// the edge on small body copy; magnified it stays at the density it was
// rasterised at (contrast 0.48 at 2×, 4× and 8×) while the field
// sharpens (0.72, 0.85, 0.92), and rotated small text holds 0.62 against
// 0.47. On by default wherever the pipeline exists; SetMSDFText(true)
// still forces the field everywhere, and turning this off forces the
// raster unless it is.
func (r *Renderer2D) SetMSDFAuto(on bool) { r.msdfAuto = on && r.msdfPipeline != nil }

// MSDFAuto reports whether the path is chosen per draw.
func (r *Renderer2D) MSDFAuto() bool { return r.msdfAuto }

// fieldText says whether this draw takes the distance-field path.
func (r *Renderer2D) fieldText(f *Font) bool {
	if r.msdfPipeline == nil || f.parsed == nil {
		return false
	}
	if r.msdfText {
		return true
	}
	if !r.msdfAuto {
		return false
	}
	m := r.xform()
	return m.A != 1 || m.B != 0 || m.C != 0 || m.D != 1
}

// SetSubpixelText turns LCD subpixel text on or off. It is on wherever
// the device supports dual-source blending; turning it off falls back to
// grayscale coverage (and is what a test comparing paths wants).
func (r *Renderer2D) SetSubpixelText(on bool) {
	r.subpixelText = on && r.subpixelPipeline != nil
}

// SubpixelTextEnabled reports whether LCD subpixel text is in use.
func (r *Renderer2D) SubpixelTextEnabled() bool { return r.subpixelText }

// SetTextWeight thickens (or, negative, thins) distance-field text, in
// ems — a fraction of the size the text is drawn at, so one setting
// holds from a caption to a headline. 0.03 is a usable synthetic bold,
// −0.015 a light.
//
// A coverage atlas cannot do this at all; it would need re-rasterising
// at a different stem weight. In a field it is an offset on the
// threshold, so it costs nothing.
//
// Screen pixels would be the wrong unit here, and were: a fixed 1.6 px
// is a hairline on a 96 px heading and closes the counters of a 22 px
// word into blobs.
func (r *Renderer2D) SetTextWeight(em float32) {
	r.setMSDFParam(0, em)
}

// SetTextOutline draws an outline around distance-field text, its width
// in ems (0 turns it off). Same trick: the outline is the band between
// two thresholds of the same field.
//
// It reaches as far as the tile padding, msdf.TilePad/msdf.TilePPEM —
// a quarter of an em — and tapers off rather than clipping square past
// that.
func (r *Renderer2D) SetTextOutline(em float32, c RGBA) {
	r.setMSDFParam(1, min(max(em, 0), MaxTextReachEm))
	r.setMSDFParam(4, c.R)
	r.setMSDFParam(5, c.G)
	r.setMSDFParam(6, c.B)
	r.setMSDFParam(7, c.A)
}

// SetTextSoftness spreads the edge over a fraction of an em, which is
// the cheap glow: the field holds distance out to msdf.TilePad texels,
// so the falloff stays smooth for a quarter of an em and then tapers.
func (r *Renderer2D) SetTextSoftness(em float32) {
	r.setMSDFParam(2, min(max(em, 0), MaxTextReachEm))
}

// MaxTextReachEm is how far outside the outline the field goes, so how
// wide an outline or a glow can be before it has nothing left to read:
// the tile padding, in ems. Past it the effect only tapers, so the two
// setters clamp there rather than let a caller ask for a band that
// fades out halfway along.
const MaxTextReachEm = float32(msdf.TilePad) / msdf.TilePPEM

// setMSDFParam changes one parameter and gives up the slot the old
// settings were committed to, so the next glyph takes a fresh one.
//
// These are renderer state, but they cannot be ONE uniform: the whole
// frame is recorded and then submitted at once, so a single buffer would
// hand every batch in it whatever the last setting was — an outlined
// heading would outline the body text under it too. Each distinct
// setting a frame uses gets its own small buffer instead, and the batch
// that used it holds the bind group that names it.
func (r *Renderer2D) setMSDFParam(i int, v float32) {
	if r.msdfParams[i] == v {
		return
	}
	r.msdfParams[i] = v
	r.msdfSlot = -1
}

// msdfParamSlot is the slot the current settings live in this frame,
// taking and filling a new one the first time they are asked for.
func (r *Renderer2D) msdfParamSlot() int {
	if r.msdfSlot >= 0 {
		return r.msdfSlot
	}
	i := r.msdfSlotN
	r.msdfSlotN++
	for len(r.msdfSlots) <= i {
		r.msdfSlots = append(r.msdfSlots,
			r.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, 32))
		r.msdfSlotVals = append(r.msdfSlotVals, [8]float32{})
	}
	// A buffer keeps what the last frame put in it, and most frames use
	// the same settings as the one before — the default ones, usually.
	// Writing anyway cost a buffer write, and so a trip through the FFI
	// and an allocation, on every frame that drew any text at all.
	if r.msdfSlotVals[i] != r.msdfParams {
		var buf [32]byte
		for k, v := range r.msdfParams {
			binary.LittleEndian.PutUint32(buf[4*k:], math.Float32bits(v))
		}
		r.queue.WriteBuffer(r.msdfSlots[i], 0, buf[:])
		r.msdfSlotVals[i] = r.msdfParams
	}
	r.msdfSlot = i
	return i
}

// msdfBind is the font family's sheet bound for the distance-field
// pipeline, building the atlas and its texture on first use.
func (f *Font) msdfBind(slot int) *gpu.BindGroup {
	fam := f.fam
	if fam == nil || f.parsed == nil || f.r.msdfLayout == nil {
		return nil
	}
	if bg := fam.binds[slot]; bg != nil {
		return bg
	}
	if fam.atlas == nil {
		fam.atlas = msdf.NewAtlas(f.parsed)
		fam.tex = f.r.NewImage(msdf.SheetDim, msdf.SheetDim, fam.atlas.Pix)
		fam.atlas.Dirty() // the whole sheet just went up
		fam.binds = map[int]*gpu.BindGroup{}
	}
	bg := f.r.device.CreateBindGroup(f.r.msdfLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: f.r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: fam.tex.view},
		gpu.BindGroupEntry{Binding: 2, Buffer: f.r.msdfSlots[slot]},
	)
	fam.binds[slot] = bg
	return bg
}

// uploadMSDF sends up only the rows packed since the last frame — a
// sheet is a megabyte and a new letter is a few hundred texels.
func (f *Font) uploadMSDF() {
	fam := f.fam
	if fam == nil || fam.atlas == nil || fam.tex == nil {
		return
	}
	y0, y1 := fam.atlas.Dirty()
	if y1 <= y0 {
		return
	}
	row := msdf.SheetDim * 4
	f.r.queue.WriteTextureRows(fam.tex.tex, msdf.SheetDim, y0, y1-y0,
		fam.atlas.Pix[y0*row:y1*row])
}

// release drops a family's GPU objects.
func (fam *msdfFamily) release() {
	if fam == nil {
		return
	}
	for _, bg := range fam.binds {
		bg.Release()
	}
	fam.binds = nil
	if fam.tex != nil {
		fam.tex.Release()
		fam.tex = nil
	}
}

// msdfGlyph is everything drawing a rune needs, cached per font size:
// the tile and the advance. Both were being fetched per glyph per
// frame — the advance from the metrics face, which looks the glyph up
// again on every call, where the raster path has had it cached in its
// atlas entry all along.
type msdfGlyph struct {
	g   msdf.Glyph
	adv float32 // layout advance at this font's size, in pixels
	ok  bool
}

// msdfGlyphOf caches a rune's tile and advance, misses included.
func (f *Font) msdfGlyphOf(r rune) (msdfGlyph, bool) {
	if m, ok := f.mglyphs[r]; ok {
		return m, m.ok
	}
	var m msdfGlyph
	m.g, m.ok = f.fam.atlas.Ensure(r)
	if m.ok {
		// The same fractional advance the rasterised atlas stores, taken
		// from the unhinted metrics face, so measuring and drawing agree
		// whichever path drew.
		if adv, ok := f.advanceOf(r); ok {
			m.adv = adv
		} else {
			m.adv = m.g.Advance * float32(f.size)
		}
	}
	if f.mglyphs == nil {
		f.mglyphs = map[rune]msdfGlyph{}
	}
	f.mglyphs[r] = m
	return m, m.ok
}

// advanceOf is a rune's layout advance without rasterising it — the same
// fractional number ensure() stores, taken straight from the unhinted
// metrics face, so measuring and drawing still agree.
func (f *Font) advanceOf(r rune) (float32, bool) {
	if f.faceM != nil {
		if adv, ok := f.faceM.GlyphAdvance(r); ok {
			return fixedToFloat(adv), true
		}
	}
	if adv, ok := f.face.GlyphAdvance(r); ok {
		return fixedToFloat(adv), true
	}
	return 0, false
}

// DrawTextSize draws a run at an arbitrary pixel size from ONE font, and
// is the thing the distance field is for.
//
// Every other text call needs a Font built for the size it draws at,
// because a rasterised atlas is only correct at the size it was made
// for. An animated size therefore means a Font, a sheet and a
// rasterisation per frame — a megabyte a frame, and a cache that never
// stops growing. A field has no size in it; this one multiplies.
//
// The metrics scale with it: the unhinted face's advances are linear in
// the size, so the run comes out MeasureText's width times
// sizePx/f.Size(). With the field path off it falls back to AtSize, and
// then it does cost a sheet.
func (r *Renderer2D) DrawTextSize(f *Font, x, y float32, s string, c RGBA, sizePx float32) {
	if sizePx <= 0 {
		return
	}
	// A size the font was not rasterised at is the field's case too:
	// the raster would build a sheet for it.
	field := r.msdfPipeline != nil && f.parsed != nil &&
		(r.msdfText || (r.msdfAuto && (sizePx != float32(f.size) || r.fieldText(f))))
	if !field {
		r.DrawText(f.AtSize(float64(sizePx)), x, y, s, c)
		return
	}
	r.drawTextMSDF(f, x, y, s, c, 0, 0, sizePx/float32(f.size))
}

// MeasureTextSize is MeasureText at an arbitrary size — the same run,
// scaled, since the advances are linear in the size.
func (r *Renderer2D) MeasureTextSize(f *Font, s string, sizePx float32) (w, h float32) {
	k := sizePx / float32(f.size)
	w, h = r.MeasureText(f, s)
	return w * k, h * k
}

// drawTextMSDF lays a run out with the face's own metrics — identical to
// the raster path, so MeasureText still describes it — and draws each
// glyph from the family sheet, scaled by the font size and by scale.
func (r *Renderer2D) drawTextMSDF(f *Font, x, y float32, s string, c RGBA, skew, tracking, scale float32) {
	baseline := y + f.layoutAscent(f, 1)*scale
	pivot := baseline - f.layoutAscent(f, 1)*scale*0.45
	// The baseline lands on a whole device pixel: a horizontal stem is
	// the one feature a field cannot keep crisp across a half-pixel, and
	// rounding it costs nothing since the run shares one baseline. The
	// pen stays fractional, which is where the raster path has to round
	// and this one does not.
	rs := r.sc()
	baseline = math32.Round(baseline*rs) / rs

	// One face, and the scale applied per glyph rather than baked into
	// the shaping: a size that slides must not make a cache entry a
	// frame, which is the whole reason DrawTextSize exists.
	run := r.shape(f, f, nil, s, 1, 1)

	// The parameters cannot change inside a run, and the sheet only
	// changes when a rune falls through to a fallback face, so neither
	// the slot nor the bind group is looked up per glyph.
	slot := r.msdfParamSlot()
	var bound *Font
	var bind *gpu.BindGroup
	var used [4]*Font
	nused := 0

	for i := range run.glyphs {
		sg := &run.glyphs[i]
		pen := x + sg.pen*scale + float32(i)*tracking
		// Colour emoji are bitmaps, not outlines: they stay on the
		// sprite path and its per-size atlas.
		if sg.emoji {
			if g, ok := f.ensureEmoji(sg.ch); ok {
				gx := math32.Round(pen + g.bearingX*scale)
				gy := math32.Round(baseline + g.bearingY*scale)
				r.glyphQuad(kindSprite, f.epages[g.page].tex.bind, gx, gy, g.w*scale, g.h*scale,
					g.u0, g.v0, g.u1, g.v1, RGBA{R: 1, G: 1, B: 1, A: c.A}, pivot, skew)
				f.upload()
			}
			continue
		}
		sf := sg.font
		if sf != bound {
			bound, bind = sf, sf.msdfBind(slot)
		}
		if bind == nil {
			// No sheet for this face (no parsed font, or no pipeline):
			// fall back to the raster glyph rather than drop the rune.
			if g, ok := sf.ensure(sg.ch); ok && g.w > 0 {
				r.glyphQuad(kindGlyph, sf.pages[g.page].tex.bind, math32.Round(pen+g.bearingX*scale),
					math32.Round(baseline+g.bearingY*scale), g.w*scale, g.h*scale,
					g.u0, g.v0, g.u1, g.v1, c, pivot, skew)
				sf.upload()
			}
			continue
		}
		m, ok := sf.msdfGlyphOf(sg.ch)
		if !ok {
			continue
		}
		px := float32(sf.size) * scale
		if m.g.W > 0 {
			r.glyphQuad(kindMSDF, bind,
				pen+m.g.OffX*px, baseline+m.g.OffY*px,
				m.g.EmW*px, m.g.EmH*px,
				m.g.U0, m.g.V0, m.g.U1, m.g.V1, c, pivot, skew)
			nused = touchFont(&used, nused, sf)
		}
	}
	for i := 0; i < nused; i++ {
		used[i].uploadMSDF()
	}
}

// msdfShader reconstructs the outline from the median of the three
// channels and antialiases it in screen space.
//
// The median is the whole multi-channel trick: two channels agree on
// where the edge is everywhere except at a corner, where the third
// breaks the tie and the corner comes back as a point instead of a
// blob (render/msdf explains the generator side).
//
// The antialiasing is a screen-space derivative. A sample is a distance
// in texels; fwidth of the texture coordinate says how many texels a
// screen pixel covers right here, which converts it to a distance in
// screen pixels and makes one expression cover 8 px text, a 400 px
// heading and anything rotated between.
var msdfShader = fmt.Sprintf(msdfShaderFmt, 2*msdf.TilePad, msdf.TilePPEM,
	smallSharpPPEM, smallSharpMin)

// Small text is where a field loses to a coverage mask, and the two
// numbers below are the whole of the answer.
//
// A rasterised mask is HINTED: the outline is nudged so a stem lands on
// the pixel grid, and one pixel goes fully dark. A field cannot be —
// the tile is generated once, at one size, with no idea where the pixel
// grid will fall — so an 11 px stem that straddles two pixels is drawn
// straddling two pixels, correctly, and reads as mush.
//
// Measured on `Hamburgefonstiv`, white on black, against the raster
// path at the same size: the two agree on the AMOUNT of ink to within
// 3 % at every size from 9 px up. What differs is how far it is spread.
// Taking contrast as the mean distance of an inked pixel from mid-grey
// (1.0 a hard mask, 0 everything at 50 %), the field sat at 0.42–0.53
// between 10 and 13 px where the raster reached 0.50–0.65.
//
// So the fix is not more ink, it is the same ink in fewer pixels:
// narrow the antialias ramp below a full screen pixel. The ramp stays
// centred on the outline, so the integral is unchanged and the glyph
// neither thickens nor thins — it only gets its edges back. It is a
// trade, and the thing traded away is real: a sub-pixel ramp aliases
// when the text moves. Below 32 px/em that is the better bargain, and
// above it the ramp is a full pixel again and this does nothing.
const (
	smallSharpPPEM = 20.0 // at and above this many px/em, a full-pixel ramp
	smallSharpMin  = 0.55 // the narrowest ramp, in screen pixels
)

// %[1]d is the field's full range in texels (twice the padding, since a
// byte spans the padding either side of the edge) and %[2]d the px/em
// the tiles were generated at. Both come from render/msdf, so the
// shader cannot drift from the generator. %[3]v and %[4]v are the
// small-text ramp (see smallSharpPPEM above).
const msdfShaderFmt = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var tex : texture_2d<f32>;

// weight, outline width, softness — all in screen pixels — and the
// outline colour.
struct MSDFParams {
    params : vec4f,
    outline : vec4f,
}
@group(0) @binding(2) var<uniform> msdfParams : MSDFParams;

struct ClipShape {
    rect : vec4f,
    params : vec4f,
    maskRect : vec4f,
}
@group(1) @binding(0) var<uniform> clipShape : ClipShape;
@group(1) @binding(1) var clipMaskTex : texture_2d<f32>;
@group(1) @binding(2) var clipMaskSamp : sampler;

fn clip_cov(p : vec2f) -> f32 {
    var cov = 1.0;
    let radius = clipShape.params.x;
    if (radius > 0.0) {
        let half_size = clipShape.rect.zw * 0.5;
        let center = clipShape.rect.xy + half_size;
        let ccp = abs(p - center) - half_size + vec2f(radius);
        let d = length(max(vec2f(0.0), ccp)) + min(0.0, max(ccp.x, ccp.y)) - radius;
        cov = clamp(0.5 - d, 0.0, 1.0);
    }
    if (clipShape.params.y > 0.5) {
        let uv = (p - clipShape.maskRect.xy) / max(clipShape.maskRect.zw, vec2f(1.0));
        cov *= textureSample(clipMaskTex, clipMaskSamp, uv).r;
    }
    return cov;
}

struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) uv : vec2f,
    @location(1) color : vec4f,
}

// One INSTANCE per quad: four corners already in clip space (the
// transform happens on the CPU, so a rotated or sheared quad is simply
// four different corners), the uv rect, and the colour. Six vertices
// are still drawn; they read the same record.
@vertex
fn vs(@builtin(vertex_index) vi : u32,
      @location(0) p01 : vec4f, @location(1) p23 : vec4f,
      @location(2) uvr : vec4f, @location(3) color : vec4f) -> VSOut {
    var corners = array<vec2f, 4>(p01.xy, p01.zw, p23.xy, p23.zw);
    var uvs = array<vec2f, 4>(uvr.xy, vec2f(uvr.z, uvr.y), uvr.zw, vec2f(uvr.x, uvr.w));
    var order = array<u32, 6>(0u, 1u, 2u, 0u, 2u, 3u);
    let k = order[vi];
    var out : VSOut;
    out.pos = vec4f(corners[k], 0.0, 1.0);
    out.uv = uvs[k];
    out.color = color;
    return out;
}

fn median3(r : f32, g : f32, b : f32) -> f32 {
    return max(min(r, g), min(max(r, g), b));
}

// The field's full range, in texels, and the px/em the tiles were
// generated at — filled in from render/msdf's own constants.
const PX_RANGE : f32 = %[1]d.0;
const TILE_PPEM : f32 = %[2]d.0;
const SHARP_PPEM : f32 = %[3]v;
const SHARP_MIN : f32 = %[4]v;

fn screen_px_range(uv : vec2f) -> f32 {
    let unitRange = vec2f(PX_RANGE) / vec2f(textureDimensions(tex, 0));
    let screenTexSize = vec2f(1.0) / fwidth(uv);
    return max(0.5 * dot(unitRange, screenTexSize), 1.0);
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let s = textureSample(tex, samp, in.uv).rgb;
    let med = median3(s.r, s.g, s.b);
    let range = screen_px_range(in.uv);
    // The parameters are in ems, so that one setting holds at every
    // size. A texel is an em/TILE_PPEM, and range screen pixels cover
    // PX_RANGE texels, so this is how many screen pixels an em is here.
    let pxPerEm = range / PX_RANGE * TILE_PPEM;
    // Distance in screen pixels, positive inside, plus the weight.
    let d = (med - 0.5) * range + msdfParams.params.x * pxPerEm;
    // The edge is antialiased over a ramp centred on the outline. A
    // full screen pixel is the correct width and what everything at
    // reading size and up gets; below that the ramp narrows, because
    // an unhinted field spreads a small stem over two pixels where a
    // hinted mask darkens one (see smallSharpPPEM). Centred either
    // way, so the amount of ink does not change.
    let ramp = clamp(pxPerEm / SHARP_PPEM, SHARP_MIN, 1.0);
    let cov = clamp(d / ramp + 0.5, 0.0, 1.0);

    var col = in.color;
    var a = cov * in.color.a;
    let ow = msdfParams.params.y * pxPerEm;
    if (ow > 0.0) {
        // The outline is the same field thresholded further out, with
        // the fill composited over it by its own coverage.
        let outer = clamp(d + ow + 0.5, 0.0, 1.0);
        let oc = vec4f(msdfParams.outline.rgb, msdfParams.outline.a * in.color.a);
        col = vec4f(mix(oc.rgb, in.color.rgb, cov), 1.0);
        a = outer * mix(oc.a, in.color.a, cov);
    }
    let soft = max(msdfParams.params.z, 0.0) * pxPerEm;
    if (soft > 0.0) {
        // A halo OUTSIDE the glyph, over a solid core — not a wider
        // edge ramp. The ramp was the first attempt and it is why a
        // glow looked weak: coverage only reaches 1 a ramp-width inside
        // the outline, so a two-pixel stem under a five-pixel ramp came
        // out at half alpha and the word faded instead of glowing.
        //
        // Squared, so the falloff has a shoulder at the glyph and a
        // long tail, which is what a glow looks like. It starts outside
        // the outline when there is one, and takes that colour, since
        // the fill's coverage is zero out there.
        let t = clamp(1.0 + (d + ow) / soft, 0.0, 1.0);
        a = max(a, t * t * in.color.a);
    }
    // The field stops at the edge of the tile, where the distance
    // saturates and med reaches 0. A wide outline or a soft glow is
    // still asking for ink out there, and the answer cuts off square:
    // a faint rectangle appears round the glyph, most visible at small
    // sizes, where the padding is only a pixel or two of screen. Fade
    // to nothing over the last sliver of field instead, so an effect
    // that outgrows the padding tapers off rather than drawing a box.
    let edge = smoothstep(0.0, 0.06, med);
    return vec4f(col.rgb, a * edge * clip_cov(in.pos.xy));
}
`

// AtlasStats reports what each text path has built for this font
// family, which is the difference between them stated as numbers.
//
// sheets is how many rasterised atlases exist — one per pixel size that
// has drawn, plus a colour one per size that drew an emoji — and
// rasterGlyphs the total number of glyphs rasterised into them: the
// same letter at nine sizes counts nine times. fieldTiles is what the
// distance-field path built for the same text, counted once per rune.
// The byte counts are the sheets themselves, on the CPU; each has a
// texture of the same size behind it.
//
// Call it on the font the family was created from; derived sizes hang
// off that one.
func (f *Font) AtlasStats() (sheets, rasterGlyphs, rasterBytes, fieldTiles, fieldBytes int) {
	count := func(g *Font) {
		for _, s := range g.pages {
			sheets++
			rasterBytes += len(s.pix)
		}
		for _, s := range g.epages {
			sheets++
			rasterBytes += len(s.pix)
		}
		rasterGlyphs += len(g.glyphs)
	}
	count(f)
	for _, g := range f.sizes {
		count(g)
	}
	if f.fam != nil && f.fam.atlas != nil {
		fieldTiles = len(f.fam.atlas.Glyphs)
		fieldBytes = len(f.fam.atlas.Pix)
	}
	return
}
