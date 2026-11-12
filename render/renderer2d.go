package render

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// Renderer2D is the 2D batcher, GPUI-style: rounded/bordered rectangles are
// instanced SDF quads read from a storage buffer, while text glyphs and
// images are textured sprite vertices. Draws are batched in paint order and
// break only on pipeline or texture change.
type Renderer2D struct {
	// quadCap and shadowCap are how many instances this frame's upload
	// holds (fitStorage); overflowWarned says the limit was reported.
	quadCap, shadowCap, vertCap, glyphCap int
	overflowWarned                        bool
	device                                *gpu.Device
	queue                                 *gpu.Queue

	// shapes memoizes run shaping per (faces, string) — drawing and
	// MeasureText share it; see shape.go.
	shapes map[shapeKey]*shapedRun
	frame  uint32

	// damage clamps every scissor of the next Flush to this framebuffer
	// rectangle (w < 0: none) — see SetDamage.
	damage clipRect

	// SDF quad pipeline (solid fills, rounded corners, borders).
	quadPipeline *gpu.RenderPipeline
	quadLayout   *gpu.BindGroupLayout
	quadBuf      *gpu.Buffer
	quadBind     *gpu.BindGroup
	quads        []float32 // 20 floats per instance (5 vec4s)

	// Gaussian drop-shadow pipeline (GPUI's blur math).
	shadowPipeline *gpu.RenderPipeline
	shadowLayout   *gpu.BindGroupLayout
	shadowBuf      *gpu.Buffer
	shadowBind     *gpu.BindGroup
	shadows        []float32 // 20 floats per instance (5 vec4s)

	// Sprite pipeline (glyph atlas, images, path fills via white).
	spritePipeline *gpu.RenderPipeline
	spriteLayout   *gpu.BindGroupLayout
	sampler        *gpu.Sampler
	spriteBuf      *gpu.Buffer
	glyphInstBuf   *gpu.Buffer
	verts          []float32 // 8 floats per vertex
	glyphInst      []float32 // 16 floats per text quad
	white          *Image    // 1x1 white for solid path triangles

	// Colorized-sprite pipeline: the same vertex data as a sprite, drawn as
	// the source's luminance in the vertex colour (Qt MultiEffect
	// colorization). Structurally identical bind group layouts, so it reuses
	// the sprite bind groups.
	colorizePipeline *gpu.RenderPipeline
	colorizeLayout   *gpu.BindGroupLayout
	// Premultiplied-sprite pipeline: the same vertex data again, for a
	// texture that already carries premultiplied colour (an offscreen
	// layer). See fs_premul.
	premulPipeline *gpu.RenderPipeline
	premulLayout   *gpu.BindGroupLayout
	// stencil* paints a texture's COVERAGE in a flat colour (a drop
	// shadow's silhouette); see fs_stencil.
	stencilPipeline *gpu.RenderPipeline
	stencilLayout   *gpu.BindGroupLayout
	// ... and the same again with an ADDITIVE blend, for the weighted sum
	// a blur pass over premultiplied pixels is (see fs_premul_add).
	premulAddPipeline *gpu.RenderPipeline
	premulAddLayout   *gpu.BindGroupLayout

	// Glyph pipeline: the sprite layout against a one-byte coverage sheet.
	glyphPipeline *gpu.RenderPipeline
	glyphLayout   *gpu.BindGroupLayout

	// Subpixel (LCD) text pipeline: dual-source blend, per-channel coverage.
	subpixelPipeline *gpu.RenderPipeline
	subpixelLayout   *gpu.BindGroupLayout
	subpixelText     bool
	textParamsBuf    *gpu.Buffer // 2xvec4 uniform: (chrome contrast, DW enhanced contrast, _, _) + gamma ratios
	textParams       [8]float32

	// Distance-field text pipeline: one sheet per family, any size
	// (msdftext.go).
	msdfPipeline *gpu.RenderPipeline
	msdfLayout   *gpu.BindGroupLayout
	msdfText     bool
	msdfAuto     bool          // the field for transformed text, the raster for the rest (msdftext.go)
	msdfParams   [8]float32    // 2xvec4: (weight, outline width, softness, _) + outline colour
	msdfSlots    []*gpu.Buffer // one uniform per distinct setting in a frame
	msdfSlotVals [][8]float32  // what each slot currently holds
	msdfSlotN    int           // slots taken this frame
	msdfSlot     int           // the current settings' slot, -1 until committed

	samples int               // MSAA sample count the pipelines were built for
	format  gpu.TextureFormat // target format the pipelines were built for

	namedTTF     map[fontKey][]byte // (family, style) -> TTF/OTF bytes
	namedFonts   map[fontKey]*Font  // (family, style) -> base Font (derived sizes cached on it)
	fallbackTTF  []byte             // CJK glyph fallback inherited by NamedFonts
	fallbackMore [][]byte           // further fallbacks (symbols), tried after it
	emojiTTF     []byte             // color-emoji face inherited by NamedFonts

	// Clip shapes reach the shaders through group(1): one small uniform
	// buffer per distinct clip in a frame, from a pool that is rewritten
	// each flush (a scrolling view changes its clip rect every frame, so
	// caching by shape would grow without bound).
	clipPool  []clipBinds
	clipIndex map[clipRect]int

	// Path clips: masks rendered before the frame's own pass (clipmask.go).
	maskPipeline     *gpu.RenderPipeline
	maskLayout0      *gpu.BindGroupLayout
	maskLayout1      *gpu.BindGroupLayout
	maskWhiteBind    *gpu.BindGroup
	maskIdentityBind *gpu.BindGroup
	maskIdentityBuf  *gpu.Buffer
	maskPool         []maskTarget
	clipMasks        []clipMask
	clipVerts        []float32
	clipVertBuf      *gpu.Buffer

	// Blend groups: offscreen composites with a blend mode (group.go).
	groups     []group
	openGroups []int // stack of indices into groups
	// resolve is every offscreen target the frame needs, in creation
	// order: a clip mask when it is pushed, a blend group when it closes.
	// Walked in that order, each one is drawn after everything it reads.
	resolve        []resolveRef
	groupCut       []int // where each open group cut the batch list
	groupPool      []groupTarget
	groupStage     []stageTarget
	compPipes      [numBlendModes]*gpu.RenderPipeline
	compLayouts    [numBlendModes]*gpu.BindGroupLayout
	extractPipes   [numExtractKinds]*gpu.RenderPipeline
	extractLayouts [numExtractKinds]*gpu.BindGroupLayout
	compVerts      []float32
	compVertBuf    *gpu.Buffer
	// batchFloor is the first batch a new draw may be merged into. A group
	// boundary raises it, so the draws inside a group are never appended to
	// a batch that started outside it (and so cannot be carried off with
	// it when EndGroup cuts the list).
	batchFloor int

	batches []batch
	// mergeScratch is drawBatches' working copy (see mergeBatches).
	mergeScratch []batch
	clips        []clipRect  // intersection stack; top is the active clip
	tmpTris      []core.Vec2 // scratch triangle buffer for path tessellation
	width        float32
	height       float32
	scale        float32 // DPI scale (SetScale); 0 means 1

	// 2D camera: world→screen as (p - cam)*zoom + (camCX,camCY), applied in
	// toNDC. Identity (zoom 0) by default and reset each Begin; SetCamera /
	// SetCameraView enable world-space drawing (pan/zoom) for games and maps.
	camX, camY, camZoom float32
	// xforms is the 2D transform stack (see transform.go): every queued
	// primitive is drawn through the top entry.
	xforms       []Affine2
	camCX, camCY float32 // screen point the camera centers on
}

// resolveRef names one offscreen target: a 1-based index into clipMasks or
// into groups (exactly one is set).
type resolveRef struct {
	mask, group int
}

type batchKind int

const (
	kindQuad batchKind = iota
	kindShadow
	kindSprite
	kindColorized // a sprite drawn as its luminance in one colour
	kindPremul    // a sprite whose texture is premultiplied (a layer)
	kindStencil   // a sprite drawn as a flat-coloured silhouette
	kindPremulAdd // one additive tap of a blur over premultiplied pixels
	kindShaderFX  // a QML ShaderEffect's quad, on its own pipeline
	kindGlyph     // grayscale text: an R8 coverage sheet tinted by the colour
	kindSubpixel  // LCD text: dual-source blend against the subpixel atlas
	kindMSDF      // distance-field text: median of three, derivative AA
	kindComposite // a blend group's offscreen result (group.go)
)

// clipRect is an integer scissor rect; w < 0 means "no clip".
// clipBinds is one pool slot: the uniform holding a clip shape, plus the
// bind group each pipeline needs for it (the layouts are per pipeline, so
// the same buffer is bound four ways).
type clipBinds struct {
	buf                                                                  *gpu.Buffer
	quad, shadow, sprite, colorize, premul, stencil, premulAdd, subpixel *gpu.BindGroup
	msdf, glyph                                                          *gpu.BindGroup
	view                                                                 *gpu.TextureView // the mask these were built for
}

// clipRect is the clip region a batch draws under: a framebuffer-pixel box
// (applied as the scissor) plus a corner radius the shaders round it by, so
// a clip can be a rounded rectangle and not only a box. w < 0 means "no
// clip at all".
type clipRect struct {
	x, y, w, h int
	r          float32 // corner radius, in framebuffer pixels
	// maskID is 1-based into clipMasks when the clip carries a coverage
	// mask — an arbitrary path, or a soft mask (see clipmask.go); 0 means
	// the shape is the box and its radius.
	maskID int
	// mx..mh is the mask texture's own extent in device pixels, which is
	// NOT the box: a box clip pushed inside a mask narrows the scissor
	// while the mask still covers what it always did, and the shader needs
	// the original extent to map a pixel into it.
	mx, my, mw, mh int
}

// clipUniformFloats is the size of the group(1) clip uniform: the box,
// (radius, has-mask, _, _), then the mask's extent.
const clipUniformFloats = 12

// maskFormat is what clip masks are rendered into. RGBA8 rather than a
// single-channel format because it is the one format every backend here
// already builds pipelines for; coverage lands in every channel.
const maskFormat = gpu.FormatRGBA8Unorm

var noClip = clipRect{w: -1}

type batch struct {
	kind     batchKind
	bind     *gpu.BindGroup // sprites only
	clip     clipRect
	clipSlot int // index into the clip-shape pool, resolved at flush
	start    int // first instance (quads) or first vertex (sprites)
	count    int
	// Composite batches only: which blend group they draw, and how.
	mode  BlendMode
	group int // 1-based into groups
	// ShaderFX batches only: the custom-shader quad to draw (its own
	// pipeline, bind group and vertex buffer — see shaderfx.go).
	fx *ShaderFX
	// The NDC box every instance of the batch lies in, for mergeBatches;
	// a kind that draws arbitrary vertices never records one (bounded
	// stays false) and so never moves or is moved across.
	bounded            bool
	bx0, by0, bx1, by1 float32
}

// cover grows the batch's bounds by one instance's corners (NDC, any
// order).
func (b *batch) cover(xs, ys [4]float32) {
	x0, x1 := min(xs[0], xs[1], xs[2], xs[3]), max(xs[0], xs[1], xs[2], xs[3])
	y0, y1 := min(ys[0], ys[1], ys[2], ys[3]), max(ys[0], ys[1], ys[2], ys[3])
	if !b.bounded {
		b.bounded = true
		b.bx0, b.by0, b.bx1, b.by1 = x0, y0, x1, y1
		return
	}
	b.bx0, b.by0 = min(b.bx0, x0), min(b.by0, y0)
	b.bx1, b.by1 = max(b.bx1, x1), max(b.by1, y1)
}

// disjoint reports whether two bounded batches share no pixel, with a
// margin for the anti-aliased edge an SDF quad feathers at its boundary.
func (b *batch) disjoint(o *batch, margin float32) bool {
	return b.bx1+margin <= o.bx0 || o.bx1+margin <= b.bx0 ||
		b.by1+margin <= o.by0 || o.by1+margin <= b.by0
}

// mergeBatches joins a batch back into an earlier compatible one when
// nothing drawn between them touches it: a HUD that draws panel, label,
// panel, label alternates quad and glyph batches, thirty of them a frame,
// each a pipeline switch and a draw — and every label is disjoint from
// every other panel, so the quads collapse into one draw and the glyphs
// into another. A batch can only merge into the nearest earlier batch
// its instances are contiguous with in the buffer (same kind, bind, clip
// and clip shape, adjacent ranges), and only when every batch between is
// bounded and disjoint from it; a batch without bounds blocks every move
// across it. margin is the AA feather in NDC.
func mergeBatches(batches []batch, margin float32) []batch {
	out := batches[:0]
	for i := range batches {
		b := batches[i]
		merged := false
		if b.bounded && b.kind != kindComposite && b.fx == nil {
			for j := len(out) - 1; j >= 0; j-- {
				t := &out[j]
				if t.kind == b.kind && t.bind == b.bind && t.clip == b.clip && t.clipSlot == b.clipSlot &&
					t.start+t.count == b.start {
					if t.bounded {
						t.count += b.count
						t.cover([4]float32{b.bx0, b.bx1, b.bx0, b.bx1}, [4]float32{b.by0, b.by1, b.by0, b.by1})
						merged = true
					}
					break
				}
				if !t.bounded || !t.disjoint(&b, margin) {
					break
				}
			}
		}
		if !merged {
			out = append(out, b)
		}
	}
	return out
}

// Image is a GPU texture ready for DrawImage, created via NewImage.
type Image struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	// borrowed: the texture and view belong to someone else (see
	// ImageFromView) — Release frees only what this Image made.
	borrowed bool
	bind     *gpu.BindGroup
	// cbind is the same texture bound for the COLORIZE pipeline. wgpu
	// treats an auto-derived pipeline layout as exclusive to its pipeline,
	// so a colorized draw needs its own bind group; it is built on first
	// use (most images are never colorized).
	cbind *gpu.BindGroup
	// sbind is the same texture bound for the SILHOUETTE pipeline (a drop
	// shadow's shape); every auto-derived layout is exclusive, so each
	// pipeline needs its own group.
	sbind *gpu.BindGroup
	// pbind is the same texture bound for the PREMULTIPLIED pipeline (an
	// offscreen layer composited over the frame), for the same reason.
	pbind *gpu.BindGroup
	// abind is the same texture bound for the ADDITIVE premultiplied
	// pipeline (a blur pass over a layer).
	abind *gpu.BindGroup
	W, H  int
}

func (i *Image) Release() {
	if i.cbind != nil {
		i.cbind.Release()
		i.cbind = nil
	}
	if i.pbind != nil {
		i.pbind.Release()
		i.pbind = nil
	}
	if i.sbind != nil {
		i.sbind.Release()
		i.sbind = nil
	}
	if i.abind != nil {
		i.abind.Release()
		i.abind = nil
	}
	if i.bind != nil {
		i.bind.Release()
	}
	if i.borrowed {
		return // the view and texture are the caller's
	}
	if i.view != nil {
		i.view.Release()
	}
	if i.tex != nil {
		i.tex.Release()
	}
}

// RGBA is a straight (non-premultiplied) color, components 0..1.
type RGBA struct{ R, G, B, A float32 }

// White is the no-tint color for DrawImage.
var White = RGBA{1, 1, 1, 1}

const (
	floatsPerVertex = 8
	// A text quad is ONE record, not six vertices: four corners already
	// in clip space, the uv rect and the colour. Sixteen floats against
	// the forty-eight six vertices of eight take — glyphs are the most
	// numerous thing on a screen of text, and they were writing three
	// times the bytes they needed to.
	//
	// Paths share the sprite buffer and emit arbitrary triangles, which
	// is why this is a buffer of its own rather than a change to that
	// one.
	floatsPerGlyphInst = 16
	floatsPerQuad      = 44 // bounds_ndc, bounds_px, color0, color1, border, params, extra, color2, color3, stops, xform
	floatsPerShadow    = 20
	quadBufMin         = 24 * 1024
)

// The quad SDF (quadSDF / the border evaluation strategy) is adapted from
// GPUI, the UI framework of the Zed editor — zed-industries/zed,
// crates/gpui_wgpu/src/shaders.wgsl, Apache-2.0. Simplifications vs GPUI:
// one corner radius and one border width per quad instead of per-corner /
// per-edge values, and no gradients or dashed borders yet.
const quadShader = `
struct Quad {
    bounds_ndc : vec4f, // x0, y0, x1, y1 in clip space
    bounds_px  : vec4f, // origin.xy, size.xy in framebuffer pixels
    color0 : vec4f,     // solid fill, or gradient start
    color1 : vec4f,     // gradient end (ignored when solid)
    border_color : vec4f,
    params : vec4f,     // corner_radius, border_width, grad_kind, grad_angle
    extra : vec4f,      // border_style (0 solid, 1 dashed), grad_count, colorspace, rotation
    color2 : vec4f,     // gradient stops 3 and 4 (grad_count > 2)
    color3 : vec4f,
    stops : vec4f,      // ramp positions of color0..color3 (0..1)
    xform : vec4f,      // shear (x per unit y, in the quad's own box), unused...
}

const M_PI : f32 = 3.1415926;

// fmod with the sign of a (for dash phase wrapping).
fn fmod_signed(a : f32, b : f32) -> f32 { return a - b * trunc(a / b); }

// dash_alpha: antialiased coverage of the dash at perimeter position t, with
// dashes of len "length" repeating every "period" (in dash space, one dash
// period = 1). dv converts the signed distance back to pixels for AA. Adapted
// from GPUI's dash_alpha (crates/gpui_wgpu/src/shaders.wgsl, Apache-2.0).
fn dash_alpha(t : f32, period : f32, length : f32, dv : f32, aa : f32) -> f32 {
    let half_period = period * 0.5;
    let half_length = length * 0.5;
    let centered = fmod_signed(t + half_period - half_length, period) - half_period;
    let signed_distance = abs(centered) - half_length;
    return clamp(aa - signed_distance / dv, 0.0, 1.0);
}

// dash_factor returns 0..1 border coverage for a dashed border at point p,
// for our uniform corner_radius / border_width quad: the dash velocity is the
// same on every side and corner, so t is just the clockwise perimeter arc
// length (pixels) times dv. Pattern: 2*width dash, 1*width gap (GPUI's).
fn dash_factor(q : Quad, p : vec2f, radius : f32, border : f32) -> f32 {
    let size = q.bounds_px.zw;
    let pt = p - q.bounds_px.xy;        // point within the quad
    let c2p = pt - size * 0.5;          // center to point
    let corner_to_point = abs(c2p) - size * 0.5;
    let ccp = corner_to_point + radius; // corner-center to point
    let is_corner = ccp.x >= 0.0 && ccp.y >= 0.0;

    let st = max(size.x - 2.0 * radius, 0.0); // top/bottom straight length
    let sr = max(size.y - 2.0 * radius, 0.0); // left/right straight length
    let carc = radius * (M_PI * 0.5);         // one corner arc length
    // Cumulative perimeter (px) clockwise, starting at the top edge (x=radius).
    let upto_r = st + carc;
    let upto_br = upto_r + sr;
    let upto_bl = upto_br + carc + st;
    let upto_l = upto_bl + carc;
    let upto_tl = upto_l + sr;
    let perim = upto_tl + carc;

    var arc = 0.0;
    if (is_corner) {
        let ct = atan2(ccp.y, ccp.x) * radius; // 0..(pi/2)*r along the corner
        if (c2p.x >= 0.0) {
            if (c2p.y < 0.0) { arc = upto_r - ct; }   // top-right
            else { arc = upto_br + ct; }              // bottom-right
        } else {
            if (c2p.y >= 0.0) { arc = upto_l - ct; }  // bottom-left
            else { arc = upto_tl + ct; }              // top-left
        }
    } else {
        if (ccp.x < ccp.y) { // horizontal edge
            if (c2p.y < 0.0) { arc = pt.x - radius; }          // top
            else { arc = upto_bl - (pt.x - radius); }          // bottom
        } else {             // vertical edge
            if (c2p.x < 0.0) { arc = upto_tl - (pt.y - radius); } // left
            else { arc = upto_r + (pt.y - radius); }             // right
        }
    }

    let dv = (1.0 / 3.0) / border; // dash_period_per_width = 2 dash + 1 gap
    let t = arc * dv;
    let max_t = perim * dv;
    let dash_length = 2.0 / 3.0;
    if (max_t >= 1.0) {
        let dash_count = floor(max_t);
        let dash_period = max_t / dash_count; // evenly divide the perimeter
        return dash_alpha(t, dash_period, dash_length, dv, 0.5);
    }
    return 1.0;
}
@group(0) @binding(0) var<storage, read> quads : array<Quad>;

// The clip shape: a box (already applied as the scissor) plus a corner
// radius, so a clip can be a rounded rectangle. The radius is cut here,
// with the same antialiasing as any other rounded edge.
struct ClipShape {
    rect : vec4f,     // x, y, w, h in framebuffer pixels
    params : vec4f,   // radius, has-mask, unused, unused
    maskRect : vec4f, // the rectangle the coverage mask covers
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
    // An arbitrary path clip is a coverage mask over the clip box.
    if (clipShape.params.y > 0.5) {
        let uv = (p - clipShape.maskRect.xy) / max(clipShape.maskRect.zw, vec2f(1.0));
        cov *= textureSample(clipMaskTex, clipMaskSamp, uv).r;
    }
    return cov;
}


struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) @interpolate(flat) id : u32,
}

@vertex
fn vs(@builtin(vertex_index) vid : u32, @builtin(instance_index) iid : u32) -> VSOut {
    var units = array<vec2f, 6>(
        vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0),
        vec2f(0.0, 0.0), vec2f(1.0, 1.0), vec2f(0.0, 1.0),
    );
    let unit = units[vid];
    let q = quads[iid];
    var out : VSOut;
    var p = mix(q.bounds_ndc.xy, q.bounds_ndc.zw, unit);
    let rot = q.extra.w;
    let shear = q.xform.x;
    if (rot != 0.0 || shear != 0.0) {
        // Shear and rotate the corner about the quad's centre. NDC is
        // anisotropic, so both happen in pixel proportions: scale into
        // pixels with the quad's own ndc-per-pixel ratio, transform, scale
        // back.
        let c = (q.bounds_ndc.xy + q.bounds_ndc.zw) * 0.5;
        let ndc_size = q.bounds_ndc.zw - q.bounds_ndc.xy;
        let px_size = max(q.bounds_px.zw, vec2f(1e-6));
        // ndc_size.y is negative (NDC y points up), so dividing by it puts
        // the offset in y-DOWN pixels — the same frame the fragment shader
        // un-transforms its sample point in.
        let per_px = ndc_size / px_size;
        var d = (p - c) / per_px;                             // to pixels
        d = vec2f(d.x + shear * d.y, d.y);                    // shear, then
        let s = sin(rot); let co = cos(rot);                  // rotate
        d = vec2f(d.x * co - d.y * s, d.x * s + d.y * co);
        p = c + d * per_px;
    }
    out.pos = vec4f(p, 0.0, 1.0);
    out.id = iid;
    return out;
}

// --- gradient color spaces (extra.z): 0 sRGB, 1 linear sRGB, 2 Oklab ---

fn srgb_to_lin(c : vec3f) -> vec3f {
    // Exact piecewise sRGB EOTF.
    let lo = c / 12.92;
    let hi = pow((c + 0.055) / 1.055, vec3f(2.4));
    return select(hi, lo, c <= vec3f(0.04045));
}

fn lin_to_srgb(c : vec3f) -> vec3f {
    let lo = c * 12.92;
    let hi = 1.055 * pow(c, vec3f(1.0 / 2.4)) - 0.055;
    return select(hi, lo, c <= vec3f(0.0031308));
}

fn lin_to_oklab(c : vec3f) -> vec3f {
    // Björn Ottosson's Oklab, from linear sRGB.
    let l = pow(0.4122214708*c.r + 0.5363325363*c.g + 0.0514459929*c.b, 1.0/3.0);
    let m = pow(0.2119034982*c.r + 0.6806995451*c.g + 0.1073969566*c.b, 1.0/3.0);
    let s = pow(0.0883024619*c.r + 0.2817188376*c.g + 0.6299787005*c.b, 1.0/3.0);
    return vec3f(
        0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
        1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
        0.0259040371*l + 0.7827717662*m - 0.8086757660*s);
}

fn oklab_to_lin(c : vec3f) -> vec3f {
    let l = c.x + 0.3963377774*c.y + 0.2158037573*c.z;
    let m = c.x - 0.1055613458*c.y - 0.0638541728*c.z;
    let s = c.x - 0.0894841775*c.y - 1.2914855480*c.z;
    let l3 = l*l*l; let m3 = m*m*m; let s3 = s*s*s;
    return vec3f(
        4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3,
        -1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3,
        -0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3);
}

// mix_space blends a→b at f in the quad's gradient color space.
fn mix_space(a : vec4f, b : vec4f, f : f32, space : f32) -> vec4f {
    if (space < 0.5) {
        // sRGB, PREMULTIPLIED (the CSS/QML default). A plain component
        // lerp is only right while both stops are opaque: a stop fading to
        // transparent (rgba 0,0,0,0) drags the colour toward black as
        // well, so the composited result curves instead of running
        // straight down to the background — Qt's own gradient to
        // transparent is linear on screen, and ours dropped away twice as
        // fast at the top of a page-wide glow.
        let pa = vec4f(a.rgb * a.a, a.a);
        let pb = vec4f(b.rgb * b.a, b.a);
        let m = mix(pa, pb, f);
        if (m.a <= 1e-5) {
            return vec4f(0.0, 0.0, 0.0, 0.0);
        }
        return vec4f(m.rgb / m.a, m.a);
    }
    let la = srgb_to_lin(a.rgb);
    let lb = srgb_to_lin(b.rgb);
    let alpha = mix(a.a, b.a, f);
    if (space < 1.5) { // linear sRGB
        return vec4f(lin_to_srgb(mix(la, lb, f)), alpha);
    }
    // Oklab (perceptual; what Zed's gradient example rows use)
    let ok = mix(lin_to_oklab(la), lin_to_oklab(lb), f);
    return vec4f(lin_to_srgb(clamp(oklab_to_lin(ok), vec3f(0.0), vec3f(1.0))), alpha);
}

// seg_mix maps t into the [sa, sb] segment and blends ca→cb there; equal
// positions give a hard split at sa.
fn seg_mix(ca : vec4f, cb : vec4f, sa : f32, sb : f32, t : f32, space : f32) -> vec4f {
    if (sb - sa < 1e-4) {
        return mix_space(ca, cb, step(sa, t), space);
    }
    return mix_space(ca, cb, clamp((t - sa) / (sb - sa), 0.0, 1.0), space);
}

// fill_at resolves a quad's interior color at framebuffer point p: solid
// (grad_kind 0), a linear gradient (grad_kind 1) interpolated along grad_angle,
// or a radial gradient (grad_kind 2) from the center (c0) to the corners (c1).
// The ramp holds grad_count (extra.y) stops — color0..color3 at stops.x..w —
// blended in the colorspace of extra.z; coincident stops give hard splits.
// dither8: sign-preserving triangular-PDF noise in ~±1/255, decorrelated
// per pixel via a hash. Added to gradient output before 8-bit quantization
// to break the visible stair-step banding smooth gradients show on a wide
// dark surface (the theme's card/hero gradients).
fn dither8(p : vec2f) -> f32 {
    let h = fract(sin(dot(floor(p), vec2f(12.9898, 78.233))) * 43758.5453);
    let h2 = fract(sin(dot(floor(p) + vec2f(1.0, 0.0), vec2f(12.9898, 78.233))) * 43758.5453);
    return (h + h2 - 1.0) / 255.0;
}

fn fill_at(q : Quad, p : vec2f) -> vec4f {
    if (q.params.z < 0.5) {
        return q.color0;
    }
    let uv = (p - q.bounds_px.xy) / q.bounds_px.zw; // 0..1 across the quad
    var t : f32;
    if (q.params.z >= 1.5) {
        // Radial: distance from center, normalized so the corner is t=1.
        t = length((uv - vec2f(0.5)) * 2.0) / sqrt(2.0);
    } else {
        let a = q.params.w;
        let dir = vec2f(cos(a), sin(a));
        t = dot(uv - vec2f(0.5), dir) + 0.5;
    }
    let count = q.extra.y;
    let space = q.extra.z;
    if (count >= 3.5) { // 4 stops: three segments
        if (t < q.stops.y) { return seg_mix(q.color0, q.color1, q.stops.x, q.stops.y, t, space); }
        if (t < q.stops.z) { return seg_mix(q.color1, q.color2, q.stops.y, q.stops.z, t, space); }
        return seg_mix(q.color2, q.color3, q.stops.z, q.stops.w, t, space);
    }
    if (count >= 2.5) { // 3 stops
        if (t < q.stops.y) { return seg_mix(q.color0, q.color1, q.stops.x, q.stops.y, t, space); }
        return seg_mix(q.color1, q.color2, q.stops.y, q.stops.z, t, space);
    }
    return seg_mix(q.color0, q.color1, q.stops.x, q.stops.y, t, space);
}

// over composites "above" onto "below" (source-over, straight alpha), like
// GPUI's over(): the result is as opaque as the two inputs combined, so a
// translucent border over an opaque background stays opaque.
fn over(below : vec4f, above : vec4f) -> vec4f {
    let alpha = above.a + below.a * (1.0 - above.a);
    let rgb = (above.rgb * above.a + below.rgb * below.a * (1.0 - above.a)) / max(alpha, 1e-6);
    return vec4f(rgb, alpha);
}

// Signed distance to a rounded rectangle's edge (adapted from GPUI's
// quad_sdf_impl): corner_center_to_point is the vector from the rounded
// corner's circle center to the point, mirrored into the bottom-right
// quadrant.
fn quad_sdf_impl(corner_center_to_point : vec2f, corner_radius : f32) -> f32 {
    if (corner_radius == 0.0) {
        return max(corner_center_to_point.x, corner_center_to_point.y);
    }
    let d = length(max(vec2f(0.0), corner_center_to_point)) +
        min(0.0, max(corner_center_to_point.x, corner_center_to_point.y));
    return d - corner_radius;
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let q = quads[in.id];
    let radius = q.params.x;
    let border = q.params.y;
    // The distance field is computed in the quad's own (untransformed)
    // frame, so a transformed quad samples the point mapped back about its
    // centre — the rotation undone first, then the shear.
    var sample = in.pos.xy;
    let rot = q.extra.w;
    let shear = q.xform.x;
    if (rot != 0.0 || shear != 0.0) {
        let c = q.bounds_px.xy + q.bounds_px.zw * 0.5;
        var d = sample - c;
        let s = sin(-rot); let co = cos(-rot);
        d = vec2f(d.x * co - d.y * s, d.x * s + d.y * co);
        d = vec2f(d.x - shear * d.y, d.y);
        sample = c + d;
    }
    let bg = fill_at(q, sample);

    let cc = clip_cov(in.pos.xy);
    if (cc <= 0.0) { discard; }
    // Fast path: solid, square, borderless quads need no SDF.
    if (radius == 0.0 && border == 0.0 && q.params.z < 0.5) {
        return vec4f(bg.rgb, bg.a * cc);
    }
    var dith = 0.0;
    if (q.params.z >= 0.5) { dith = dither8(sample); }

    let half_size = q.bounds_px.zw * 0.5;
    let center = q.bounds_px.xy + half_size;
    let center_to_point = sample - center;
    let corner_to_point = abs(center_to_point) - half_size;
    let ccp = corner_to_point + radius;

    let aa = 0.5; // antialias threshold, as in GPUI
    let outer = quad_sdf_impl(ccp, radius);

    var color = bg;
    if (border > 0.0) {
        // border_amount is 1 within the border band and 0 in the interior
        // (GPUI's circular-inner-edge fast path for a uniform border width).
        let border_amount = 1.0 - clamp(aa - (outer + border), 0.0, 1.0);
        var bc = q.border_color;
        if (q.extra.x > 0.5) { // dashed
            bc.a *= dash_factor(q, sample, radius, border);
        }
        // Composite the border over the background before the inner-edge
        // lerp (GPUI does the same): lerping straight toward a translucent
        // border color would pass through dark half-opaque fringe values.
        color = mix(bg, over(bg, bc), border_amount);
    }
    color.a *= clamp(aa - outer, 0.0, 1.0) * cc;
    color = vec4f(color.rgb + vec3f(dith), color.a);
    return color;
}
`

// Drop shadows: a Gaussian blur of a rounded rectangle evaluated in closed
// form along x (Evan Wallace's technique) and sampled along y. Ported from
// GPUI's fs_shadow / blur_along_x / gaussian / erf (zed-industries/zed,
// crates/gpui_wgpu/src/shaders.wgsl, Apache-2.0), simplified to a single
// corner radius and drop (non-inset) shadows.
const shadowShader = `
struct Shadow {
    bounds_ndc : vec4f, // expanded by 3*blur for the gaussian tail
    bounds_px  : vec4f, // the shadow rect itself: origin.xy, size.xy
    color : vec4f,
    unused : vec4f,
    params : vec4f,     // corner_radius, blur_radius, unused, unused
}
@group(0) @binding(0) var<storage, read> shadows : array<Shadow>;

// The clip shape: a box (already applied as the scissor) plus a corner
// radius, so a clip can be a rounded rectangle. The radius is cut here,
// with the same antialiasing as any other rounded edge.
struct ClipShape {
    rect : vec4f,     // x, y, w, h in framebuffer pixels
    params : vec4f,   // radius, has-mask, unused, unused
    maskRect : vec4f, // the rectangle the coverage mask covers
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
    // An arbitrary path clip is a coverage mask over the clip box.
    if (clipShape.params.y > 0.5) {
        let uv = (p - clipShape.maskRect.xy) / max(clipShape.maskRect.zw, vec2f(1.0));
        cov *= textureSample(clipMaskTex, clipMaskSamp, uv).r;
    }
    return cov;
}


struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) @interpolate(flat) id : u32,
}

@vertex
fn vs(@builtin(vertex_index) vid : u32, @builtin(instance_index) iid : u32) -> VSOut {
    var units = array<vec2f, 6>(
        vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0),
        vec2f(0.0, 0.0), vec2f(1.0, 1.0), vec2f(0.0, 1.0),
    );
    let unit = units[vid];
    let s = shadows[iid];
    var out : VSOut;
    out.pos = vec4f(mix(s.bounds_ndc.xy, s.bounds_ndc.zw, unit), 0.0, 1.0);
    out.id = iid;
    return out;
}

const M_PI_F : f32 = 3.1415926535;

fn gaussian(x : f32, sigma : f32) -> f32 {
    return exp(-(x * x) / (2.0 * sigma * sigma)) / (sqrt(2.0 * M_PI_F) * sigma);
}

fn erf(v : vec2f) -> vec2f {
    let s = sign(v);
    let a = abs(v);
    let r1 = 1.0 + (0.278393 + (0.230389 + (0.000972 + 0.078108 * a) * a) * a) * a;
    let r2 = r1 * r1;
    return s - s / (r2 * r2);
}

fn blur_along_x(x : f32, y : f32, sigma : f32, corner : f32, half_size : vec2f) -> f32 {
    let delta = min(half_size.y - corner - abs(y), 0.0);
    let curved = half_size.x - corner + sqrt(max(0.0, corner * corner - delta * delta));
    let integral = 0.5 + 0.5 * erf((x + vec2f(-curved, curved)) * (sqrt(0.5) / sigma));
    return integral.y - integral.x;
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let s = shadows[in.id];
    let radius = s.params.x;
    let blur = s.params.y;
    let half_size = s.bounds_px.zw * 0.5;
    let center = s.bounds_px.xy + half_size;
    let center_to_point = in.pos.xy - center;

    var alpha : f32;
    if (blur == 0.0) {
        let corner_to_point = abs(center_to_point) - half_size;
        let ccp = corner_to_point + radius;
        alpha = clamp(0.5 - quad_sdf_impl(ccp, radius), 0.0, 1.0);
    } else {
        // The signal is only non-zero in a limited range, so don't waste
        // samples (GPUI samples 4 points along y).
        let low = center_to_point.y - half_size.y;
        let high = center_to_point.y + half_size.y;
        let start = clamp(-3.0 * blur, low, high);
        let end = clamp(3.0 * blur, low, high);
        let step = (end - start) / 4.0;
        var y = start + step * 0.5;
        alpha = 0.0;
        for (var i = 0; i < 4; i += 1) {
            alpha += blur_along_x(center_to_point.x, center_to_point.y - y,
                blur, radius, half_size) * gaussian(y, blur) * step;
            y += step;
        }
    }
    return vec4f(s.color.rgb, s.color.a * alpha * clip_cov(in.pos.xy));
}

fn quad_sdf_impl(corner_center_to_point : vec2f, corner_radius : f32) -> f32 {
    if (corner_radius == 0.0) {
        return max(corner_center_to_point.x, corner_center_to_point.y);
    }
    let d = length(max(vec2f(0.0), corner_center_to_point)) +
        min(0.0, max(corner_center_to_point.x, corner_center_to_point.y));
    return d - corner_radius;
}
`

const spriteShader = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var tex : texture_2d<f32>;

// The clip shape: a box (already applied as the scissor) plus a corner
// radius, so a clip can be a rounded rectangle. The radius is cut here,
// with the same antialiasing as any other rounded edge.
struct ClipShape {
    rect : vec4f,     // x, y, w, h in framebuffer pixels
    params : vec4f,   // radius, has-mask, unused, unused
    maskRect : vec4f, // the rectangle the coverage mask covers
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
    // An arbitrary path clip is a coverage mask over the clip box.
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

@vertex
fn vs(@location(0) pos : vec2f, @location(1) uv : vec2f, @location(2) color : vec4f) -> VSOut {
    var out : VSOut;
    out.pos = vec4f(pos, 0.0, 1.0);
    out.uv = uv;
    out.color = color;
    return out;
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let c = textureSample(tex, samp, in.uv) * in.color;
    return vec4f(c.rgb, c.a * clip_cov(in.pos.xy));
}

// A COLORIZED sprite: the source's luminance in the given colour, keeping
// its alpha — Qt's MultiEffect colorization, and what tinting an icon
// means. A plain multiply cannot do it: multiplying a purple glyph by teal
// gives mud, where colorizing gives a teal glyph.
@fragment
fn fs_colorize(in : VSOut) -> @location(0) vec4f {
    let src = textureSample(tex, samp, in.uv);
    let lum = dot(src.rgb, vec3f(0.2126, 0.7152, 0.0722));
    let a = src.a * in.color.a * clip_cov(in.pos.xy);
    return vec4f(in.color.rgb * lum, a);
}

// A PREMULTIPLIED sprite: what an offscreen target holds. Rendering into a
// cleared target leaves premultiplied pixels, and blending them again with
// a straight-alpha equation multiplies by the alpha a SECOND time — a
// translucent captured layer then composites at alpha^2 (a blob painted at
// 0.3 came out at 0.084, a third of Qt's). The straight colour is restored
// here so the ordinary blend is right; the vertex colour is a straight
// tint, as everywhere else.
@fragment
fn fs_premul(in : VSOut) -> @location(0) vec4f {
    let src = textureSample(tex, samp, in.uv);
    let rgb = src.rgb / max(src.a, 1.0 / 512.0) * in.color.rgb;
    let a = src.a * in.color.a * clip_cov(in.pos.xy);
    return vec4f(rgb, a);
}

// A SILHOUETTE: the source's coverage in a flat colour. A drop shadow is
// the shape of what casts it, not a darker copy of it — Qt's MultiEffect
// shadow paints the item's alpha in shadowColor, so a BLACK item still
// casts the warm brown shadow the markup asked for (the paper diorama's
// trees are black and their shadows rgba(0.08, 0.02, 0, 0.7); tinting the
// texture instead multiplied that brown by zero and every shadow came out
// black). The texture is premultiplied, so its alpha is the coverage.
@fragment
fn fs_stencil(in : VSOut) -> @location(0) vec4f {
    let src = textureSample(tex, samp, in.uv);
    let a = src.a * in.color.a * clip_cov(in.pos.xy);
    return vec4f(in.color.rgb, a);
}

// Glyphs come in as one record per quad rather than six vertices, so
// they need their own vertex entry in this module — the one above still
// serves images and the path triangles, which are not quads at all.
@vertex
fn vs_glyph(@builtin(vertex_index) vi : u32,
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

// A GLYPH: coverage in the red channel, tinted by the vertex colour.
//
// A glyph sheet is a mask. Stored as RGBA it is three bytes of white per
// texel that every shader here throws away, and a 1024-square sheet is
// four megabytes on the CPU and four more on the GPU rather than one and
// one. R8 is the same mask at a quarter of the size, and the only
// difference on this side is which channel the coverage arrives in.
@fragment
fn fs_glyph(in : VSOut) -> @location(0) vec4f {
    let cov = textureSample(tex, samp, in.uv).r;
    return vec4f(in.color.rgb, cov * in.color.a * clip_cov(in.pos.xy));
}

// One WEIGHTED TAP of a blur over premultiplied pixels, for a pipeline
// whose blend is additive: a convolution is a weighted SUM, and scaling a
// premultiplied colour by a scalar keeps it premultiplied. Compositing the
// taps "over" one another instead only approximates the sum while the
// samples are opaque — for a translucent layer it accumulates alpha that
// is not there (a blob painted at 0.3 came out at 0.55 after one pass and
// 0.75 after two).
@fragment
fn fs_premul_add(in : VSOut) -> @location(0) vec4f {
    return textureSample(tex, samp, in.uv) * in.color.a * clip_cov(in.pos.xy);
}
`

// subpixelShader is the LCD text fragment using dual-source blending (GPUI's
// subpixel approach): output 0 is the foreground color, output 1 is a
// per-channel coverage mask. The pipeline blends result = color*mask +
// dst*(1-mask) per channel, so each RGB subpixel is composited independently.
//
// Coverage then gets GPUI's contrast + gamma correction (their wgpu renderer
// defaults: gamma 1.8, subpixel enhanced contrast 0.5), adapted from
// microsoft/terminal's dwrite.hlsl (MIT) by way of gpui_wgpu/src/shaders.wgsl
// — without it, dark-on-light stems read fatter/darker than GPUI's.
const subpixelShader = `
enable dual_source_blending;

@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var tex : texture_2d<f32>;

// The clip shape: a box (already applied as the scissor) plus a corner
// radius, so a clip can be a rounded rectangle. The radius is cut here,
// with the same antialiasing as any other rounded edge.
struct ClipShape {
    rect : vec4f,     // x, y, w, h in framebuffer pixels
    params : vec4f,   // radius, has-mask, unused, unused
    maskRect : vec4f, // the rectangle the coverage mask covers
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
    // An arbitrary path clip is a coverage mask over the clip box.
    if (clipShape.params.y > 0.5) {
        let uv = (p - clipShape.maskRect.xy) / max(clipShape.maskRect.zw, vec2f(1.0));
        cov *= textureSample(clipMaskTex, clipMaskSamp, uv).r;
    }
    return cov;
}

// params.x: legacy Chrome/Skia contrast strength 0..1 (0 = off);
// params.y: DirectWrite enhanced-contrast factor (GPUI subpixel default 0.5);
// gamma: alpha-correction ratios for the chosen gamma (GPUI default 1.8).
struct TextParams {
    params : vec4f,
    gamma : vec4f,
}
@group(0) @binding(2) var<uniform> textParams : TextParams;

fn color_brightness(color : vec3f) -> f32 {
    // REC. 601 luminance coefficients for perceived brightness.
    return dot(color, vec3f(0.30, 0.59, 0.11));
}

fn light_on_dark_contrast(enhanced_contrast : f32, color : vec3f) -> f32 {
    let brightness = color_brightness(color);
    let multiplier = saturate(4.0 * (0.75 - brightness));
    return enhanced_contrast * multiplier;
}

fn enhance_contrast3(alpha : vec3f, k : f32) -> vec3f {
    return alpha * (k + 1.0) / (alpha * k + 1.0);
}

fn apply_alpha_correction3(a : vec3f, b : vec3f, g : vec4f) -> vec3f {
    let brightness_adjustment = g.x * b + g.y;
    let correction = brightness_adjustment * a + (g.z * b + g.w);
    return a + a * (1.0 - a) * correction;
}

fn apply_contrast_and_gamma_correction3(sample : vec3f, color : vec3f, ec : f32, g : vec4f) -> vec3f {
    let contrasted = enhance_contrast3(sample, light_on_dark_contrast(ec, color));
    return apply_alpha_correction3(contrasted, color, g);
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

struct FragOut {
    @location(0) @blend_src(0) color : vec4f,
    @location(0) @blend_src(1) mask : vec4f,
}

@fragment
fn fs(in : VSOut) -> FragOut {
    // Per-channel coverage from the LCD atlas. Linear coverage matches the
    // stroke weight of GPUI's text (measured against Zed's hello_world); a
    // gamma here would bold/thin the stems.
    let cov0 = textureSample(tex, samp, in.uv).rgb;
    // Pull the per-channel coverage partway toward its luminance to damp LCD
    // colour fringing — the speckle is most visible on dark backgrounds. This
    // is a lighter-weight equivalent of a stronger FreeType LCD filter; it
    // keeps most of the subpixel sharpness while removing the grainy colour.
    let avg = (cov0.r + cov0.g + cov0.b) / 3.0;
    var cov = mix(cov0, vec3f(avg), 0.45);
    // GPUI/DirectWrite contrast + gamma correction: thins dark-on-light stems
    // and fattens light-on-dark ones, matching gpui_wgpu's defaults.
    cov = apply_contrast_and_gamma_correction3(cov, in.color.rgb, textParams.params.y, textParams.gamma);
    // Optional Chrome/Skia-style contrast on top (off by default): browsers
    // gamma-adjust coverage by text polarity more aggressively.
    let strength = textParams.params.x;
    if (strength > 0.0) {
        let lum = dot(in.color.rgb, vec3f(0.299, 0.587, 0.114));
        let gLight = mix(1.0, 0.72, strength); // light text: fatten
        let gDark  = mix(1.0, 1.15, strength); // dark text: thin a touch
        let gamma = mix(gDark, gLight, smoothstep(0.35, 0.75, lum));
        cov = pow(cov, vec3f(gamma));
    }
    let m = cov * in.color.a * clip_cov(in.pos.xy);
    var o : FragOut;
    o.color = vec4f(in.color.rgb, 1.0);
    o.mask = vec4f(m, max(m.r, max(m.g, m.b)));
    return o;
}
`

// New2D builds the renderer for a given target format.
// New2D creates a single-sample 2D renderer.
func New2D(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (*Renderer2D, error) {
	return newRenderer2D(device, queue, format, 1)
}

// New2DMSAA creates a 2D renderer whose pipelines render at the given MSAA
// sample count (4 = anti-aliased path/triangle edges). The caller must give it
// a multisampled color target that resolves into the final texture (see
// gpu.CreateMSAATexture + CommandEncoder.BeginRenderPassResolve).
func New2DMSAA(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat, samples int) (*Renderer2D, error) {
	return newRenderer2D(device, queue, format, samples)
}

func newRenderer2D(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat, samples int) (*Renderer2D, error) {
	qm, err := device.CreateShaderModuleWGSL("quad2d", quadShader)
	if err != nil {
		return nil, err
	}
	defer qm.Release()
	quadPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: qm, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, AlphaBlend: true, Samples: samples,
	})
	if err != nil {
		return nil, err
	}

	shm, err := device.CreateShaderModuleWGSL("shadow2d", shadowShader)
	if err != nil {
		return nil, err
	}
	defer shm.Release()
	shadowPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: shm, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, AlphaBlend: true, Samples: samples,
	})
	if err != nil {
		return nil, err
	}

	sm, err := device.CreateShaderModuleWGSL("sprite2d", spriteShader)
	if err != nil {
		return nil, err
	}
	defer sm.Release()
	spritePipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, AlphaBlend: true, Samples: samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerVertex * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	colorizePipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs_colorize",
		Format: format, AlphaBlend: true, Samples: samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerVertex * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	premulPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs_premul",
		Format: format, AlphaBlend: true, Samples: samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerVertex * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	stencilPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs_stencil",
		Format: format, AlphaBlend: true, Samples: samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerVertex * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	premulAddPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs_premul_add",
		Format: format, Samples: samples,
		// A convolution SUMS its taps: the pass renders into a cleared
		// target and the weights add to one, so nothing can overflow.
		Blend: &gpu.BlendDesc{
			Color: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOne},
			Alpha: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOne},
		},
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerVertex * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	glyphPipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs_glyph", FragmentEntry: "fs_glyph",
		Format: format, AlphaBlend: true, Samples: samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: floatsPerGlyphInst * 4, StepMode: gpu.StepInstance,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x4, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x4, Offset: 16, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 32, Location: 2},
				{Format: gpu.VertexFloat32x4, Offset: 48, Location: 3},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	// Subpixel (LCD) text pipeline: same vertex layout as sprites, dual-source
	// blend. Best-effort — if dual_source_blending isn't available the renderer
	// falls back to grayscale text.
	// (Asking a device without the feature, as every browser is, would raise
	// a validation error for a shader that was never going to be used.)
	var subpixelPipeline *gpu.RenderPipeline
	if !device.DualSourceBlending() {
	} else if spm, err := device.CreateShaderModuleWGSL("subpixel2d", subpixelShader); err == nil {
		defer spm.Release()
		subpixelPipeline, _ = device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: spm, VertexEntry: "vs", FragmentEntry: "fs",
			Format: format, DualSrcBlend: true, Samples: samples,
			VertexLayouts: []gpu.VertexLayout{{
				Stride: floatsPerGlyphInst * 4, StepMode: gpu.StepInstance,
				Attrs: []gpu.VertexAttr{
					{Format: gpu.VertexFloat32x4, Offset: 0, Location: 0},
					{Format: gpu.VertexFloat32x4, Offset: 16, Location: 1},
					{Format: gpu.VertexFloat32x4, Offset: 32, Location: 2},
					{Format: gpu.VertexFloat32x4, Offset: 48, Location: 3},
				},
			}},
		})
	}

	// Distance-field text pipeline: the sprite vertex layout again, with
	// a fragment that reconstructs the outline from the field.
	mm, err := device.CreateShaderModuleWGSL("msdf2d", msdfShader)
	var msdfPipeline *gpu.RenderPipeline
	if err == nil {
		defer mm.Release()
		msdfPipeline, _ = device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: mm, VertexEntry: "vs", FragmentEntry: "fs",
			Format: format, AlphaBlend: true, Samples: samples,
			VertexLayouts: []gpu.VertexLayout{{
				Stride: floatsPerGlyphInst * 4, StepMode: gpu.StepInstance,
				Attrs: []gpu.VertexAttr{
					{Format: gpu.VertexFloat32x4, Offset: 0, Location: 0},
					{Format: gpu.VertexFloat32x4, Offset: 16, Location: 1},
					{Format: gpu.VertexFloat32x4, Offset: 32, Location: 2},
					{Format: gpu.VertexFloat32x4, Offset: 48, Location: 3},
				},
			}},
		})
	}

	r := &Renderer2D{damage: noClip,
		device:            device,
		queue:             queue,
		quadPipeline:      quadPipeline,
		quadLayout:        quadPipeline.BindGroupLayout(0),
		shadowPipeline:    shadowPipeline,
		shadowLayout:      shadowPipeline.BindGroupLayout(0),
		spritePipeline:    spritePipeline,
		colorizePipeline:  colorizePipeline,
		premulPipeline:    premulPipeline,
		stencilPipeline:   stencilPipeline,
		glyphPipeline:     glyphPipeline,
		glyphLayout:       glyphPipeline.BindGroupLayout(0),
		premulAddPipeline: premulAddPipeline,
		spriteLayout:      spritePipeline.BindGroupLayout(0),
		colorizeLayout:    colorizePipeline.BindGroupLayout(0),
		premulLayout:      premulPipeline.BindGroupLayout(0),
		stencilLayout:     stencilPipeline.BindGroupLayout(0),
		premulAddLayout:   premulAddPipeline.BindGroupLayout(0),
		sampler:           device.CreateSampler(true),
		samples:           samples,
		format:            format,
	}
	if subpixelPipeline != nil {
		r.subpixelPipeline = subpixelPipeline
		r.subpixelLayout = subpixelPipeline.BindGroupLayout(0)
		r.subpixelText = true
		r.textParamsBuf = device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, 32)
		// GPUI's wgpu defaults: Chrome-contrast off, DirectWrite enhanced
		// contrast 0.5, gamma-correction ratios for gamma 1.8.
		g := gammaCorrectionRatios(1.8)
		r.textParams = [8]float32{0, 0.5, 0, 0, g[0], g[1], g[2], g[3]}
		r.writeTextParams()
	}
	if msdfPipeline != nil {
		r.msdfPipeline = msdfPipeline
		r.msdfLayout = msdfPipeline.BindGroupLayout(0)
		r.msdfAuto = true
	}
	r.msdfSlot = -1
	r.growQuadBuf(quadBufMin)
	r.growShadowBuf(quadBufMin)
	// 1x1 white sprite: path fills sample it so white*color == color.
	r.white = r.NewImage(1, 1, []byte{255, 255, 255, 255})
	return r, nil
}

// spriteCount is how many of a vertex batch's vertices this frame's
// upload holds, in whole triangles.
func spriteCount(b batch, vertCap int) int {
	n := min(b.count, vertCap-b.start)
	return n - n%3
}

// storageGrow is the size a storage buffer grows to: double, so a
// growing frame does not reallocate every time — but never past what a
// bind group can bind. Doubling past it is how the split screen in Drone
// Swarm asked for 279 MB for 140 MB of quads, got an invalid buffer, and
// aborted inside the driver a hundred frames later.
func storageGrow(cur, need int) int {
	return max(need, min(2*cur, gpu.MaxStorageBindingSize))
}

// fitStorage cuts a frame's instance data to what one storage binding can
// hold, and returns how many instances survive. Past the limit the rest
// are not drawn, and that is said once; the alternative is a buffer the
// driver will not bind, and an abort.
func (r *Renderer2D) fitStorage(data []byte, stride int, what string) ([]byte, int) {
	return r.fitBuffer(data, stride, gpu.MaxStorageBindingSize, what)
}

// fitBuffer is fitStorage against any byte limit: a vertex buffer may be
// as large as a buffer may be.
func (r *Renderer2D) fitBuffer(data []byte, stride, max int, what string) ([]byte, int) {
	limit := max / stride * stride
	if len(data) <= limit {
		return data, len(data) / stride
	}
	if !r.overflowWarned {
		r.overflowWarned = true
		fmt.Fprintf(os.Stderr, "render: %d %s in one frame (%d MB) is past the %d MB one buffer holds; drawing the first %d\n",
			len(data)/stride, what, len(data)>>20, max>>20, limit/stride)
	}
	return data[:limit], limit / stride
}

func (r *Renderer2D) growShadowBuf(size int) {
	if r.shadowBind != nil {
		r.shadowBind.Release()
	}
	if r.shadowBuf != nil {
		r.shadowBuf.Release()
	}
	r.shadowBuf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, size)
	r.shadowBind = r.device.CreateBindGroup(r.shadowLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.shadowBuf})
}

func (r *Renderer2D) growQuadBuf(size int) {
	if r.quadBind != nil {
		r.quadBind.Release()
	}
	if r.quadBuf != nil {
		r.quadBuf.Release()
	}
	r.quadBuf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, size)
	r.quadBind = r.device.CreateBindGroup(r.quadLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.quadBuf})
}

func (r *Renderer2D) Release() {
	if r.quadBind != nil {
		r.quadBind.Release()
	}
	if r.quadBuf != nil {
		r.quadBuf.Release()
	}
	if r.quadLayout != nil {
		r.quadLayout.Release()
	}
	if r.quadPipeline != nil {
		r.quadPipeline.Release()
	}
	if r.shadowBind != nil {
		r.shadowBind.Release()
	}
	if r.shadowBuf != nil {
		r.shadowBuf.Release()
	}
	if r.shadowLayout != nil {
		r.shadowLayout.Release()
	}
	if r.shadowPipeline != nil {
		r.shadowPipeline.Release()
	}
	for i := range r.clipPool {
		r.clipPool[i].releaseGroups()
		if r.clipPool[i].buf != nil {
			r.clipPool[i].buf.Release()
		}
	}
	r.clipPool = nil
	for _, b := range r.msdfSlots {
		b.Release()
	}
	r.msdfSlots, r.msdfSlotVals = nil, nil
	r.releaseMasks()
	r.releaseGroups()
	if r.white != nil {
		r.white.Release()
	}
	if r.sampler != nil {
		r.sampler.Release()
	}
	if r.spriteLayout != nil {
		r.spriteLayout.Release()
	}
	if r.colorizeLayout != nil {
		r.colorizeLayout.Release()
		r.colorizeLayout = nil
	}
	if r.premulLayout != nil {
		r.premulLayout.Release()
		r.premulLayout = nil
	}
	if r.stencilLayout != nil {
		r.stencilLayout.Release()
		r.stencilLayout = nil
	}
	if r.premulAddLayout != nil {
		r.premulAddLayout.Release()
		r.premulAddLayout = nil
	}
	if r.premulAddPipeline != nil {
		r.premulAddPipeline.Release()
		r.premulAddPipeline = nil
	}
	if r.premulPipeline != nil {
		r.premulPipeline.Release()
		r.premulPipeline = nil
	}
	if r.stencilPipeline != nil {
		r.stencilPipeline.Release()
		r.stencilPipeline = nil
	}
	if r.colorizePipeline != nil {
		r.colorizePipeline.Release()
		r.colorizePipeline = nil
	}
	if r.spritePipeline != nil {
		r.spritePipeline.Release()
	}
	if r.glyphInstBuf != nil {
		r.glyphInstBuf.Release()
	}
	if r.spriteBuf != nil {
		r.spriteBuf.Release()
	}
}

// NewImage uploads tightly packed RGBA8 pixels as a drawable image.
// NewAlphaImage uploads a one-byte-a-texel coverage mask, bound for the
// glyph pipeline. Glyph sheets are the only thing that wants it so far.
func (r *Renderer2D) NewAlphaImage(w, h int, cov []byte) *Image {
	tex := r.device.CreateTexture2D(w, h, gpu.FormatR8Unorm, gpu.TextureBinding|gpu.TextureCopyDst)
	r.queue.WriteTexture(tex, w, h, cov)
	view := tex.CreateView()
	bind := r.device.CreateBindGroup(r.glyphLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
	return &Image{tex: tex, view: view, bind: bind, W: w, H: h}
}

func (r *Renderer2D) NewImage(w, h int, rgba []byte) *Image {
	tex := r.device.CreateTexture2D(w, h, gpu.FormatRGBA8Unorm, gpu.TextureBinding|gpu.TextureCopyDst)
	r.queue.WriteTexture(tex, w, h, rgba)
	view := tex.CreateView()
	bind := r.device.CreateBindGroup(r.spriteLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
	return &Image{tex: tex, view: view, bind: bind, W: w, H: h}
}

// ImageFromView wraps an existing texture view (a render-graph texture,
// say) as a drawable image. The caller keeps ownership of the texture and
// view; Release frees only the bind group.
func (r *Renderer2D) ImageFromView(view *gpu.TextureView, w, h int) *Image {
	bind := r.device.CreateBindGroup(r.spriteLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
	// The view is kept so the image can be bound elsewhere too (a custom
	// shader samples it), but it stays the caller's to free.
	return &Image{bind: bind, view: view, borrowed: true, W: w, H: h}
}

// Begin starts a frame for a target of the given pixel size.
// Samples reports the MSAA sample count the renderer's pipelines were built
// for (1 = none). Callers must supply a matching color target.
func (r *Renderer2D) Samples() int { return r.samples }

// Device, Queue and Format expose what the renderer was built with, so a
// caller can allocate a target its pipelines can draw into (an offscreen
// Layer2D, a readback texture).
func (r *Renderer2D) Device() *gpu.Device       { return r.device }
func (r *Renderer2D) Queue() *gpu.Queue         { return r.queue }
func (r *Renderer2D) Format() gpu.TextureFormat { return r.format }

func (r *Renderer2D) Begin(width, height int) {
	r.width, r.height = float32(width), float32(height)
	r.camX, r.camY, r.camZoom = 0, 0, 0 // reset camera to screen space each frame
	r.verts = r.verts[:0]
	r.glyphInst = r.glyphInst[:0]
	r.quads = r.quads[:0]
	r.shadows = r.shadows[:0]
	r.batches = r.batches[:0]
	r.clips = r.clips[:0]
	r.xforms = r.xforms[:0]
	r.clipMasks = r.clipMasks[:0]
	r.clipVerts = r.clipVerts[:0]
	r.groups = r.groups[:0]
	r.openGroups = r.openGroups[:0]
	r.resolve = r.resolve[:0]
	r.batchFloor = 0
	r.groupCut = r.groupCut[:0]
	r.compVerts = r.compVerts[:0]
	r.sweepShapes()
	// The distance-field text parameters start each frame uncommitted,
	// so the first run that draws takes slot zero again.
	r.msdfSlotN, r.msdfSlot = 0, -1
}

// PushClip intersects a pixel rectangle with the current clip; draws until
// the matching PopClip are restricted to it.
func (r *Renderer2D) PushClip(x, y, w, h float32) {
	r.PushClipRound(x, y, w, h, 0)
}

// PushClipRound is PushClip with rounded corners — a card clipping its
// content, an avatar cropped to a circle (radius = half the smaller side).
// The box is a scissor as before; the corners are cut in the shaders, so
// they antialias like any other rounded edge.
//
// Two overlapping ROUNDED clips cannot be expressed as one rounded box, so
// the intersection keeps the corners of whichever clip's box survives it
// and falls back to the plain box when neither does.
func (r *Renderer2D) PushClipRound(x, y, w, h, radius float32) {
	// The box arrives in the caller's own coordinates, so it goes through
	// the transform stack like any drawn geometry: a clip inside a layer's
	// offscreen pass (or under a scale/rotation) has to land where the
	// content it clips actually goes. A rotation or shear has no scissor
	// rectangle, so its bounding box is used — a clip that lets a little
	// too much through beats one in the wrong place.
	s := r.sc() // scissors are framebuffer-pixel rectangles
	x, y, w, h, rs := r.clipBox(x, y, w, h)
	c := clipRect{x: int(x * s), y: int(y * s), w: int(w * s), h: int(h * s),
		r: max(radius, 0) * rs * s}
	// A radius past half the box is a stadium, as everywhere else.
	c.r = min(c.r, float32(min(c.w, c.h))/2)
	if top := r.clip(); top.w >= 0 {
		x0, y0 := max(c.x, top.x), max(c.y, top.y)
		x1, y1 := min(c.x+c.w, top.x+top.w), min(c.y+c.h, top.y+top.h)
		box := clipRect{x: x0, y: y0, w: max(0, x1-x0), h: max(0, y1-y0)}
		// A box clip inside a masked clip keeps that mask, with the mask's
		// own extent: narrowing the box only trims what shows through.
		box.maskID = top.maskID
		box.mx, box.my, box.mw, box.mh = top.mx, top.my, top.mw, top.mh
		switch {
		case box.x == c.x && box.y == c.y && box.w == c.w && box.h == c.h:
			box.r = c.r // the new clip is the whole intersection
		case box.x == top.x && box.y == top.y && box.w == top.w && box.h == top.h:
			box.r = top.r // the old one is
		}
		c = box
	}
	r.clips = append(r.clips, c)
}

// PopClip restores the clip active before the matching PushClip.
func (r *Renderer2D) PopClip() {
	if len(r.clips) > 0 {
		r.clips = r.clips[:len(r.clips)-1]
	}
}

func (r *Renderer2D) clip() clipRect {
	if len(r.clips) == 0 {
		return noClip
	}
	return r.clips[len(r.clips)-1]
}

// prepareClips writes every distinct clip shape this frame uses into the
// pool and records the slot on each batch. Called from Flush, before the
// pass: a uniform buffer cannot be written while a pass that reads it is
// open. Slot 0 always holds the identity shape (radius 0, which makes
// clip_cov return 1), so a batch with a plain box clip still has something
// to bind — group(1) is not optional once a shader declares it.
func (r *Renderer2D) prepareClips() {
	if r.clipIndex == nil {
		r.clipIndex = map[clipRect]int{}
	}
	clear(r.clipIndex)
	r.ensureClipSlot(0, r.white.view)
	var identity [clipUniformFloats]float32
	r.queue.WriteBuffer(r.clipPool[0].buf, 0, floatBytes(identity[:]))
	// A blend group's batches were moved out of the frame's list, so they
	// have to be resolved too: they are drawn (into the group's target)
	// with the same clip shapes.
	lists := [][]batch{r.batches}
	for i := range r.groups {
		if len(r.groups[i].batch) > 0 {
			lists = append(lists, r.groups[i].batch)
		}
	}
	for _, list := range lists {
		r.prepareClipsIn(list)
	}
}

// prepareClipsIn assigns one draw list's clip slots, filling the pool as it
// meets shapes it has not seen this frame.
func (r *Renderer2D) prepareClipsIn(batches []batch) {
	for i := range batches {
		c := batches[i].clip
		if c.r <= 0 && c.maskID == 0 { // a plain box: the scissor does it
			batches[i].clipSlot = 0
			continue
		}
		slot, ok := r.clipIndex[c]
		if !ok {
			slot = len(r.clipIndex) + 1 // 0 is the identity
			r.clipIndex[c] = slot
			view := r.maskViewFor(c)
			r.ensureClipSlot(slot, view)
			hasMask := float32(0)
			if c.maskID > 0 && view != r.white.view {
				hasMask = 1
			}
			// rect (x, y, w, h), params (radius, has-mask, _, _), then the
			// mask's extent — the rectangle the mask texture covers, which
			// is how the shader maps a device position into it.
			data := [clipUniformFloats]float32{
				float32(c.x), float32(c.y), float32(c.w), float32(c.h),
				c.r, hasMask, 0, 0,
				float32(c.mx), float32(c.my), float32(c.mw), float32(c.mh),
			}
			r.queue.WriteBuffer(r.clipPool[slot].buf, 0, floatBytes(data[:]))
		}
		batches[i].clipSlot = slot
	}
}

// ensureClipSlot grows the pool to cover slot i and makes sure its bind
// groups reference the given mask texture (the 1x1 white when the clip has
// no mask). A group cannot be rebound, so a changed mask means new groups.
func (r *Renderer2D) ensureClipSlot(i int, view *gpu.TextureView) {
	for len(r.clipPool) <= i {
		buf := r.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, clipUniformFloats*4)
		r.clipPool = append(r.clipPool, clipBinds{buf: buf})
	}
	cb := &r.clipPool[i]
	if cb.view == view && cb.quad != nil {
		return
	}
	cb.releaseGroups()
	cb.view = view
	bind := func(p *gpu.RenderPipeline) *gpu.BindGroup {
		if p == nil {
			return nil
		}
		layout := p.BindGroupLayout(1)
		defer layout.Release()
		return r.device.CreateBindGroup(layout,
			gpu.BindGroupEntry{Binding: 0, Buffer: cb.buf},
			gpu.BindGroupEntry{Binding: 1, TextureView: view},
			gpu.BindGroupEntry{Binding: 2, Sampler: r.sampler},
		)
	}
	cb.quad = bind(r.quadPipeline)
	cb.shadow = bind(r.shadowPipeline)
	cb.sprite = bind(r.spritePipeline)
	cb.colorize = bind(r.colorizePipeline)
	cb.premul = bind(r.premulPipeline)
	cb.stencil = bind(r.stencilPipeline)
	cb.premulAdd = bind(r.premulAddPipeline)
	cb.subpixel = bind(r.subpixelPipeline)
	cb.msdf = bind(r.msdfPipeline)
	cb.glyph = bind(r.glyphPipeline)
}

// releaseGroups frees a slot's bind groups (its buffer is kept).
func (c *clipBinds) releaseGroups() {
	for _, bg := range []*gpu.BindGroup{c.quad, c.shadow, c.sprite, c.colorize, c.premul, c.stencil, c.premulAdd, c.subpixel, c.msdf, c.glyph} {
		if bg != nil {
			bg.Release()
		}
	}
	c.quad, c.shadow, c.sprite, c.subpixel, c.msdf, c.glyph = nil, nil, nil, nil, nil, nil
}

// clipBindFor is the bind group a pipeline needs for a batch's clip slot.
func (r *Renderer2D) clipBindFor(slot int, kind batchKind) *gpu.BindGroup {
	if slot < 0 || slot >= len(r.clipPool) {
		slot = 0
	}
	cb := r.clipPool[slot]
	switch kind {
	case kindQuad:
		return cb.quad
	case kindShadow:
		return cb.shadow
	case kindSprite:
		return cb.sprite
	case kindColorized:
		return cb.colorize
	case kindPremul:
		return cb.premul
	case kindStencil:
		return cb.stencil
	case kindPremulAdd:
		return cb.premulAdd
	case kindSubpixel:
		return cb.subpixel
	case kindMSDF:
		return cb.msdf
	case kindGlyph:
		return cb.glyph
	}
	return nil
}

// isGlyphKind reports whether a batch draws text quads, which live in
// their own instance buffer.
func isGlyphKind(k batchKind) bool {
	return k == kindGlyph || k == kindSubpixel || k == kindMSDF
}

func (r *Renderer2D) ensureBatch(kind batchKind, bind *gpu.BindGroup, start int) *batch {
	clip := r.clip()
	if n := len(r.batches); n > r.batchFloor && r.batches[n-1].kind == kind &&
		r.batches[n-1].bind == bind && r.batches[n-1].clip == clip {
		return &r.batches[n-1]
	}
	r.batches = append(r.batches, batch{kind: kind, bind: bind, clip: clip, start: start})
	return &r.batches[len(r.batches)-1]
}

// coverRect records a pixel rect's transformed corners on the batch.
func (r *Renderer2D) coverRect(b *batch, x, y, w, h float32) {
	tlx, tly := r.toNDC(x, y)
	trx, try := r.toNDC(x+w, y)
	brx, bry := r.toNDC(x+w, y+h)
	blx, bly := r.toNDC(x, y+h)
	b.cover([4]float32{tlx, trx, brx, blx}, [4]float32{tly, try, bry, bly})
}

// FillRect queues a solid rectangle in pixel coordinates, (0,0) top-left.
func (r *Renderer2D) FillRect(x, y, w, h float32, c RGBA) {
	r.Quad(x, y, w, h, c, 0, 0, RGBA{})
}

// FillRoundRect queues a solid rectangle with rounded corners.
func (r *Renderer2D) FillRoundRect(x, y, w, h, radius float32, c RGBA) {
	r.Quad(x, y, w, h, c, radius, 0, RGBA{})
}

// Quad queues the full GPUI-style primitive: solid fill, corner radius,
// and a border of the given width and color.
func (r *Renderer2D) Quad(x, y, w, h float32, bg RGBA, radius, borderWidth float32, borderColor RGBA) {
	r.pushQuad(x, y, w, h, bg, nil, 0, 0, radius, borderWidth, borderColor, 0)
}

// DashedQuad is Quad with a dashed border (GPUI's border_style: dashed). The
// dash size scales with the border width (2×width dash, 1×width gap).
func (r *Renderer2D) DashedQuad(x, y, w, h float32, bg RGBA, radius, borderWidth float32, borderColor RGBA) {
	r.pushQuad(x, y, w, h, bg, nil, 0, 0, radius, borderWidth, borderColor, 1)
}

// ColorSpace selects how gradient colors interpolate.
type ColorSpace int

const (
	SpaceSRGB   ColorSpace = iota // plain component lerp (CSS/QML default)
	SpaceLinear                   // linear-light sRGB
	SpaceOklab                    // perceptual (GPUI's gradient example rows)
)

// Ramp is a gradient of 2-4 positioned color stops. Stops must be ascending
// (0..1); coincident stops give a hard split.
type Ramp struct {
	Colors [4]RGBA
	Stops  [4]float32
	Count  int
	Space  ColorSpace
}

// Ramp2 builds the common 2-stop ramp spanning 0..1.
func Ramp2(c0, c1 RGBA) Ramp {
	return Ramp{Colors: [4]RGBA{c0, c1}, Stops: [4]float32{0, 1}, Count: 2}
}

// GradientRect queues a rounded rectangle filled with a linear gradient
// from c0 to c1 along angleRad (0 = left→right, π/2 = top→bottom).
func (r *Renderer2D) GradientRect(x, y, w, h, radius float32, c0, c1 RGBA, angleRad float32) {
	rp := Ramp2(c0, c1)
	r.pushQuad(x, y, w, h, RGBA{}, &rp, 1, angleRad, radius, 0, RGBA{}, 0)
}

// GradientQuad is the full gradient primitive: linear fill plus corner
// radius and border.
func (r *Renderer2D) GradientQuad(x, y, w, h float32, c0, c1 RGBA, angleRad, radius, borderWidth float32, borderColor RGBA) {
	rp := Ramp2(c0, c1)
	r.pushQuad(x, y, w, h, RGBA{}, &rp, 1, angleRad, radius, borderWidth, borderColor, 0)
}

// GradientQuadStops is GradientQuad with explicit ramp positions: color0
// holds until stop0, color1 from stop1 on (both 0..1 along the gradient
// axis). Equal stops make a hard color split — CSS/QML positioned stops.
func (r *Renderer2D) GradientQuadStops(x, y, w, h float32, c0, c1 RGBA, angleRad, radius, borderWidth float32, borderColor RGBA, stop0, stop1 float32) {
	rp := Ramp2(c0, c1)
	rp.Stops = [4]float32{stop0, stop1}
	r.pushQuad(x, y, w, h, RGBA{}, &rp, 1, angleRad, radius, borderWidth, borderColor, 0)
}

// GradientQuadStopsN is a linear gradient of ANY number of stops: a quad's
// ramp holds four, and QML puts no limit on a Gradient's stops (the corpus'
// mist layers use five). A QML Gradient is always axis-aligned, so each
// SEGMENT is drawn as its own two-stop band — exact, where dropping a stop
// was not: collapsing five stops to four ran the mist's fade straight from
// 0.65 to the end, leaving it twice as bright through the bottom third.
func (r *Renderer2D) GradientQuadStopsN(x, y, w, h float32, stops []GradientStop,
	angleRad, radius, borderWidth float32, borderColor RGBA) {
	if len(stops) == 0 {
		return
	}
	if len(stops) <= 4 {
		var rp Ramp
		rp.Count = len(stops)
		for i, st := range stops {
			rp.Colors[i], rp.Stops[i] = st.Col, st.Pos
		}
		r.GradientQuadRamp(x, y, w, h, rp, angleRad, radius, borderWidth, borderColor)
		return
	}
	vertical := math.Abs(math.Sin(float64(angleRad))) > 0.5
	if radius > 0 {
		r.PushClipRound(x, y, w, h, radius)
	}
	for i := 0; i+1 < len(stops); i++ {
		a, b := stops[i], stops[i+1]
		p0, p1 := min(max(a.Pos, 0), 1), min(max(b.Pos, 0), 1)
		if p1 <= p0 {
			continue
		}
		bx, by, bw, bh := x, y, w, h
		if vertical {
			by, bh = y+h*p0, h*(p1-p0)
		} else {
			bx, bw = x+w*p0, w*(p1-p0)
		}
		r.GradientRect(bx, by, bw, bh, 0, a.Col, b.Col, angleRad)
	}
	if radius > 0 {
		r.PopClip()
	}
	// The border (and with it the corner shape) is the box's, over the bands.
	if borderWidth > 0 {
		r.Quad(x, y, w, h, RGBA{}, radius, borderWidth, borderColor)
	}
}

// GradientQuadRamp is the full-fidelity gradient: up to 4 positioned stops
// blended in the ramp's color space.
func (r *Renderer2D) GradientQuadRamp(x, y, w, h float32, ramp Ramp, angleRad, radius, borderWidth float32, borderColor RGBA) {
	r.pushQuad(x, y, w, h, RGBA{}, &ramp, 1, angleRad, radius, borderWidth, borderColor, 0)
}

// RadialGradientQuad fills with a radial gradient from c0 (center) to c1
// (corner), plus corner radius and border.
func (r *Renderer2D) RadialGradientQuad(x, y, w, h float32, c0, c1 RGBA, radius, borderWidth float32, borderColor RGBA) {
	rp := Ramp2(c0, c1)
	r.pushQuad(x, y, w, h, RGBA{}, &rp, 2, 0, radius, borderWidth, borderColor, 0)
}

// pushQuad queues one SDF quad; ramp nil (with bg) means a solid fill.
func (r *Renderer2D) pushQuad(x, y, w, h float32, bg RGBA, ramp *Ramp, gradKind, gradAngle, radius, borderWidth float32, borderColor RGBA, borderStyle float32) {
	b := r.ensureBatch(kindQuad, nil, len(r.quads)/floatsPerQuad)
	r.coverRect(b, x, y, w, h)
	// A quad is an axis-aligned rect its shader rebuilds from pixel bounds,
	// so the transform folds into (centre, size, angle) rather than moving
	// the corners: the shader rotates both the geometry and the distance
	// field by the angle.
	rot, shear := float32(0), float32(0)
	if m := r.xform(); !m.IsIdentity() {
		cx, cy := m.Apply(x+w/2, y+h/2)
		// A scale, a SHEAR and a rotation carry any 2x2 exactly — which is
		// what a rotation about a diagonal axis needs (see
		// shearDecompose). A mirrored transform comes back as a negative
		// size, so keep the box positive and mirror the sampling instead.
		sx, sy, sh, a := m.shearDecompose()
		w, h = w*sx, h*sy
		if w < 0 {
			w, sh = -w, -sh
		}
		if h < 0 {
			h = -h
		}
		x, y = cx-w/2, cy-h/2
		if s := min(abs32(sx), abs32(sy)); s != 1 {
			radius, borderWidth = radius*s, borderWidth*s
		}
		rot, shear = a, sh
	}
	radius = clampCornerRadius(radius, w, h)
	x0, y0 := r.rawToNDC(x, y)
	x1, y1 := r.rawToNDC(x+w, y+h)
	r.noteGroupNDC(x0, y0, x1, y1)
	var rp Ramp
	if ramp != nil {
		rp = *ramp
		if rp.Count < 2 {
			rp.Count = 2
		}
	} else {
		rp = Ramp2(bg, bg)
		gradKind = 0
	}
	c := rp.Colors
	// The shader's SDF math runs in framebuffer pixels: scale the pixel-unit
	// bounds and widths (NDC corners already went through toNDC's scale).
	s := r.sc()
	r.quads = append(r.quads,
		x0, y0, x1, y1,
		x*s, y*s, w*s, h*s,
		c[0].R, c[0].G, c[0].B, c[0].A,
		c[1].R, c[1].G, c[1].B, c[1].A,
		borderColor.R, borderColor.G, borderColor.B, borderColor.A,
		radius*s, borderWidth*s, gradKind, gradAngle,
		borderStyle, float32(rp.Count), float32(rp.Space), rot,
		c[2].R, c[2].G, c[2].B, c[2].A,
		c[3].R, c[3].G, c[3].B, c[3].A,
		rp.Stops[0], rp.Stops[1], rp.Stops[2], rp.Stops[3],
		shear, 0, 0, 0,
	)
	b.count++
}

// Shadow queues a Gaussian drop shadow under a rounded rectangle: the rect
// the shadow is cast by, its corner radius, the blur sigma, and the shadow
// color (alpha controls strength).
func (r *Renderer2D) Shadow(x, y, w, h, radius, blur float32, c RGBA) {
	b := r.ensureBatch(kindShadow, nil, len(r.shadows)/floatsPerShadow)
	radius = clampCornerRadius(radius, w, h) // a stadium's shadow, not a lens
	m := 3 * blur                            // leave room for the gaussian tail, as GPUI does
	r.coverRect(b, x-m, y-m, w+2*m, h+2*m)
	x0, y0 := r.toNDC(x-m, y-m)
	x1, y1 := r.toNDC(x+w+m, y+h+m)
	r.noteGroupNDC(x0, y0, x1, y1)
	s := r.sc() // shader gaussian runs in framebuffer pixels (see pushQuad)
	r.shadows = append(r.shadows,
		x0, y0, x1, y1,
		x*s, y*s, w*s, h*s,
		c.R, c.G, c.B, c.A,
		0, 0, 0, 0,
		radius*s, blur*s, 0, 0,
	)
	b.count++
}

// DrawImage queues img stretched to a pixel rectangle, modulated by tint
// (use White for none).
func (r *Renderer2D) DrawImage(img *Image, x, y, w, h float32, tint RGBA) {
	r.quadUV(img.bind, x, y, w, h, 0, 0, 1, 1, tint)
}

// DrawImageColorized queues img stretched to a pixel rectangle, recoloured:
// every pixel takes c's hue at the source's own luminance, keeping the
// source alpha (Qt's `MultiEffect { colorization: 1 }`, how a Controls style
// tints an icon). A tint multiply cannot do this — it can only darken, so a
// coloured icon comes out muddy rather than recoloured.
func (r *Renderer2D) DrawImageColorized(img *Image, x, y, w, h float32, c RGBA) {
	bind := r.colorizeBind(img)
	if bind == nil {
		r.DrawImage(img, x, y, w, h, c) // no colorize pipeline: draw plain
		return
	}
	r.spriteQuad(kindColorized, bind, x, y, w, h, 0, 0, 1, 1, c)
}

// DrawImagePremul queues img stretched to a pixel rectangle, treating its
// texels as PREMULTIPLIED — what an offscreen target holds. Compositing a
// captured layer any other way multiplies by its alpha twice.
func (r *Renderer2D) DrawImagePremul(img *Image, x, y, w, h float32, c RGBA) {
	bind := r.premulBind(img)
	if bind == nil {
		r.DrawImage(img, x, y, w, h, c) // no premul pipeline: draw plain
		return
	}
	r.spriteQuad(kindPremul, bind, x, y, w, h, 0, 0, 1, 1, c)
}

// premulBind is img's texture bound for the premultiplied pipeline, built
// once per image (each auto-derived pipeline layout is exclusive in wgpu,
// so a pipeline needs its own bind group — see Image.cbind).
func (r *Renderer2D) premulBind(img *Image) *gpu.BindGroup {
	if img.pbind != nil {
		return img.pbind
	}
	if r.premulLayout == nil || img.view == nil {
		return nil
	}
	img.pbind = r.device.CreateBindGroup(r.premulLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: img.view},
	)
	return img.pbind
}

// DrawImageStencil queues img's COVERAGE stretched to a pixel rectangle,
// painted in c: the shape of the image rather than the image. That is what
// a drop shadow is — Qt's MultiEffect paints the item's alpha in
// shadowColor — and a tint multiply cannot do it, since a black item
// multiplied by a brown shadow colour is still black.
func (r *Renderer2D) DrawImageStencil(img *Image, x, y, w, h float32, c RGBA) {
	bind := r.stencilBind(img)
	if bind == nil {
		r.DrawImage(img, x, y, w, h, c) // no stencil pipeline: draw plain
		return
	}
	r.spriteQuad(kindStencil, bind, x, y, w, h, 0, 0, 1, 1, c)
}

// stencilBind is img's texture bound for the silhouette pipeline (see
// premulBind on why each pipeline needs its own group).
func (r *Renderer2D) stencilBind(img *Image) *gpu.BindGroup {
	if img.sbind != nil {
		return img.sbind
	}
	if r.stencilLayout == nil || img.view == nil {
		return nil
	}
	img.sbind = r.device.CreateBindGroup(r.stencilLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: img.view},
	)
	return img.sbind
}

// DrawImagePremulAdd queues one additive tap of a blur: img's
// premultiplied texels scaled by c.A. The pass it belongs to must render
// into a cleared target — see fs_premul_add.
func (r *Renderer2D) DrawImagePremulAdd(img *Image, x, y, w, h float32, c RGBA) {
	bind := r.premulAddBind(img)
	if bind == nil {
		r.DrawImage(img, x, y, w, h, c)
		return
	}
	r.spriteQuad(kindPremulAdd, bind, x, y, w, h, 0, 0, 1, 1, c)
}

// premulAddBind is img's texture bound for the additive premultiplied
// pipeline (its own bind group, as every pipeline needs).
func (r *Renderer2D) premulAddBind(img *Image) *gpu.BindGroup {
	if img.abind != nil {
		return img.abind
	}
	if r.premulAddLayout == nil || img.view == nil {
		return nil
	}
	img.abind = r.device.CreateBindGroup(r.premulAddLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: img.view},
	)
	return img.abind
}

// colorizeBind is img's texture bound for the colorize pipeline, built once
// per image (see Image.cbind).
func (r *Renderer2D) colorizeBind(img *Image) *gpu.BindGroup {
	if img.cbind != nil {
		return img.cbind
	}
	if r.colorizeLayout == nil || img.view == nil {
		return nil
	}
	img.cbind = r.device.CreateBindGroup(r.colorizeLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: img.view},
	)
	return img.cbind
}

// quadUV queues a sprite quad sampling the sub-rect (u0,v0)-(u1,v1) of
// bind's texture — the path glyphs and images share.
func (r *Renderer2D) quadUV(bind *gpu.BindGroup, x, y, w, h, u0, v0, u1, v1 float32, c RGBA) {
	r.spriteQuad(kindSprite, bind, x, y, w, h, u0, v0, u1, v1, c)
}

// spriteQuad appends a textured quad (6 verts) into the shared sprite vertex
// buffer under the given batch kind (sprite or subpixel).
func (r *Renderer2D) spriteQuad(kind batchKind, bind *gpu.BindGroup, x, y, w, h, u0, v0, u1, v1 float32, c RGBA) {
	if isGlyphKind(kind) {
		b := r.ensureBatch(kind, bind, len(r.glyphInst)/floatsPerGlyphInst)
		tlx, tly := r.toNDC(x, y)
		trx, try := r.toNDC(x+w, y)
		brx, bry := r.toNDC(x+w, y+h)
		blx, bly := r.toNDC(x, y+h)
		b.cover([4]float32{tlx, trx, brx, blx}, [4]float32{tly, try, bry, bly})
		r.glyphInstance(tlx, tly, trx, try, brx, bry, blx, bly, u0, v0, u1, v1, c)
		b.count++
		return
	}
	b := r.ensureBatch(kind, bind, len(r.verts)/floatsPerVertex)
	// All FOUR corners go through the transform. Taking two and pairing
	// their coordinates works only while the transform keeps the quad
	// axis-aligned: under a rotation it mixes one corner's x with another's
	// y, which tore every glyph of a rotated text into fragments.
	tlx, tly := r.toNDC(x, y)
	trx, try := r.toNDC(x+w, y)
	brx, bry := r.toNDC(x+w, y+h)
	blx, bly := r.toNDC(x, y+h)
	r.vertex(tlx, tly, u0, v0, c)
	r.vertex(trx, try, u1, v0, c)
	r.vertex(brx, bry, u1, v1, c)
	r.vertex(tlx, tly, u0, v0, c)
	r.vertex(brx, bry, u1, v1, c)
	r.vertex(blx, bly, u0, v1, c)
	b.count += 6
}

// subpixelBind returns the bind group for a font's LCD atlas, creating the
// atlas texture on first use and uploading newly-rasterized glyphs.
// SetTextContrast sets the Chrome/Skia-style text gamma-contrast strength
// (0 = off — the default; 1 = full browser-like stem fattening for
// light-on-dark text). Layered on top of the always-on GPUI/DirectWrite
// correction. Applies to subpixel (LCD) text.
func (r *Renderer2D) SetTextContrast(strength float32) {
	if r.textParamsBuf == nil {
		return
	}
	r.textParams[0] = min(max(strength, 0), 1)
	r.writeTextParams()
}

// SetTextGamma tunes the GPUI/DirectWrite text correction: gamma (1.0–2.2,
// GPUI default 1.8; 1.0 disables alpha correction) and the enhanced-contrast
// factor (GPUI subpixel default 0.5; 0 disables). Applies to subpixel text.
func (r *Renderer2D) SetTextGamma(gamma, enhancedContrast float32) {
	if r.textParamsBuf == nil {
		return
	}
	r.textParams[1] = max(enhancedContrast, 0)
	g := gammaCorrectionRatios(gamma)
	copy(r.textParams[4:], g[:])
	r.writeTextParams()
}

func (r *Renderer2D) writeTextParams() {
	var buf [32]byte
	for i, v := range r.textParams {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
	}
	r.queue.WriteBuffer(r.textParamsBuf, 0, buf[:])
}

// gammaCorrectionRatios returns the DirectWrite alpha-correction ratios for a
// display gamma. Adapted from microsoft/terminal src/renderer/atlas/dwrite.cpp
// (MIT) by way of gpui's get_gamma_correction_ratios (gpui/src/platform.rs).
func gammaCorrectionRatios(gamma float32) [4]float32 {
	ratios := [13][4]float32{
		{0.0000, 0.0000, 0.0000, 0.0000},   // gamma = 1.0
		{0.0166, -0.0807, 0.2227, -0.0751}, // gamma = 1.1
		{0.0350, -0.1760, 0.4325, -0.1370}, // gamma = 1.2
		{0.0543, -0.2821, 0.6302, -0.1876}, // gamma = 1.3
		{0.0739, -0.3963, 0.8167, -0.2287}, // gamma = 1.4
		{0.0933, -0.5161, 0.9926, -0.2616}, // gamma = 1.5
		{0.1121, -0.6395, 1.1588, -0.2877}, // gamma = 1.6
		{0.1300, -0.7649, 1.3159, -0.3080}, // gamma = 1.7
		{0.1469, -0.8911, 1.4644, -0.3234}, // gamma = 1.8
		{0.1627, -1.0170, 1.6051, -0.3347}, // gamma = 1.9
		{0.1773, -1.1420, 1.7385, -0.3426}, // gamma = 2.0
		{0.1908, -1.2652, 1.8650, -0.3476}, // gamma = 2.1
		{0.2031, -1.3864, 1.9851, -0.3501}, // gamma = 2.2
	}
	const norm13 = float32(65536.0 / (255.0 * 255.0)) // includes the /4*4
	const norm24 = float32(256.0 / 255.0)
	i := int(math.Round(float64(gamma)*10)) - 10
	i = min(max(i, 0), 12)
	r := ratios[i]
	return [4]float32{r[0] * norm13, r[1] * norm24, r[2] * norm13, r[3] * norm24}
}

// uploadSubpixel sends up whatever rows each of a font's LCD sheets
// gained this frame.
func (r *Renderer2D) uploadSubpixel(f *Font) {
	for _, s := range f.spages {
		s.upload(r)
	}
}

func (r *Renderer2D) toNDC(x, y float32) (float32, float32) {
	if len(r.xforms) > 0 {
		x, y = r.xforms[len(r.xforms)-1].Apply(x, y)
	}
	return r.rawToNDC(x, y)
}

// rawToNDC is toNDC without the transform stack — for primitives that carry
// their own transform (the SDF quad, which rotates in its shader).
func (r *Renderer2D) rawToNDC(x, y float32) (float32, float32) {
	if r.camZoom != 0 {
		x = (x-r.camX)*r.camZoom + r.camCX
		y = (y-r.camY)*r.camZoom + r.camCY
	}
	if s := r.scale; s > 0 && s != 1 {
		x, y = x*s, y*s
	}
	return x/r.width*2 - 1, 1 - y/r.height*2
}

// sc returns the DPI scale, treating the zero value as 1 so renderers
// constructed without SetScale (screenshots, tests) draw 1:1.
func (r *Renderer2D) sc() float32 {
	if r.scale <= 0 {
		return 1
	}
	return r.scale
}

// SetScale sets the DPI scale factor mapping logical pixels (all draw-call
// coordinates and sizes) onto the physical framebuffer given to Begin:
// geometry is scaled in toNDC, SDF pixel parameters (corner radius, border
// and shadow widths) are scaled per quad, and text rasterizes glyph atlases
// at size*scale while layout metrics (MeasureText, Font.Height) stay
// logical. 0 or 1 draws 1:1.
func (r *Renderer2D) SetScale(s float32) {
	if s <= 0 {
		s = 1
	}
	r.scale = s
}

// Scale returns the DPI scale set by SetScale (1 when unset).
func (r *Renderer2D) Scale() float32 { return r.sc() }

// SetCamera positions a world-space camera: world point (camX,camY) maps to the
// screen center and world units are scaled by zoom. Draws after this call are
// in world space until CameraReset (or the next Begin). Screen-space overlays
// (HUD/UI) should CameraReset first.
func (r *Renderer2D) SetCamera(camX, camY, zoom float32) {
	// Camera math runs in logical pixels (toNDC applies the DPI scale after).
	s := r.sc()
	r.SetCameraView(camX, camY, zoom, r.width/(2*s), r.height/(2*s))
}

// SetCameraView is SetCamera with an explicit screen center (cx,cy) the world
// point maps to — e.g. to reserve a top toolbar by centering in the area below
// it.
func (r *Renderer2D) SetCameraView(camX, camY, zoom, cx, cy float32) {
	if zoom <= 0 {
		zoom = 1
	}
	r.camX, r.camY, r.camZoom, r.camCX, r.camCY = camX, camY, zoom, cx, cy
}

// CameraReset restores screen-space (pixel) drawing.
func (r *Renderer2D) CameraReset() { r.camX, r.camY, r.camZoom = 0, 0, 0 }

// WorldToScreen maps a world point to screen pixels under the active camera.
func (r *Renderer2D) WorldToScreen(x, y float32) (float32, float32) {
	if r.camZoom == 0 {
		return x, y
	}
	return (x-r.camX)*r.camZoom + r.camCX, (y-r.camY)*r.camZoom + r.camCY
}

// ScreenToWorld maps screen pixels to a world point under the active camera —
// for picking and click-to-world. With no camera it is the identity.
func (r *Renderer2D) ScreenToWorld(sx, sy float32) (float32, float32) {
	if r.camZoom == 0 {
		return sx, sy
	}
	return (sx-r.camCX)/r.camZoom + r.camX, (sy-r.camCY)/r.camZoom + r.camY
}

// glyphInstance appends one text quad: the four corners in clip space
// (the transform is applied here, so a rotated or sheared quad is just
// four different corners), the uv rect, and the colour.
func (r *Renderer2D) glyphInstance(tlx, tly, trx, try, brx, bry, blx, bly, u0, v0, u1, v1 float32, c RGBA) {
	r.glyphInst = append(r.glyphInst,
		tlx, tly, trx, try,
		brx, bry, blx, bly,
		u0, v0, u1, v1,
		c.R, c.G, c.B, c.A)
	r.noteGroupNDC(tlx, tly, tlx, tly)
	r.noteGroupNDC(trx, try, trx, try)
	r.noteGroupNDC(brx, bry, brx, bry)
	r.noteGroupNDC(blx, bly, blx, bly)
}

func (r *Renderer2D) vertex(x, y, u, v float32, c RGBA) {
	r.verts = append(r.verts, x, y, u, v, c.R, c.G, c.B, c.A)
	r.noteGroupNDC(x, y, x, y)
}

func floatBytes(f []float32) []byte {
	if len(f) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
}

// SetDamage restricts the next Flush's drawing to a framebuffer rectangle:
// every batch's scissor is intersected with it. The caller pairs it with a
// pass that LOADS the previous frame (see ui/damage.go).
func (r *Renderer2D) SetDamage(x, y, w, h int) { r.damage = clipRect{x: x, y: y, w: w, h: h} }

// ClearDamage lifts SetDamage.
func (r *Renderer2D) ClearDamage() { r.damage = noClip }

// Flush uploads the queued primitives and records their draws into pass in
// paint order, switching pipelines only when the batch kind changes.
//
// One Begin/Flush cycle per queue submission: uploads go to fixed buffer
// offsets, so flushing the same renderer twice before a submit would
// overwrite the first flush's data. Render-graph passes that each need 2D
// drawing should use separate Renderer2D instances for now (per-pass
// sub-allocation is planned).
func (r *Renderer2D) Flush(pass *gpu.RenderPass) error {
	if len(r.batches) == 0 {
		return nil
	}
	if data := floatBytes(r.quads); len(data) > 0 {
		data, r.quadCap = r.fitStorage(data, floatsPerQuad*4, "quads")
		if r.quadBuf.Size() < len(data) {
			r.growQuadBuf(storageGrow(r.quadBuf.Size(), len(data)))
		}
		r.queue.WriteBuffer(r.quadBuf, 0, data)
	}
	if data := floatBytes(r.shadows); len(data) > 0 {
		data, r.shadowCap = r.fitStorage(data, floatsPerShadow*4, "shadows")
		if r.shadowBuf.Size() < len(data) {
			r.growShadowBuf(storageGrow(r.shadowBuf.Size(), len(data)))
		}
		r.queue.WriteBuffer(r.shadowBuf, 0, data)
	}
	if data := floatBytes(r.verts); len(data) > 0 {
		data, r.vertCap = r.fitBuffer(data, floatsPerVertex*4, gpu.MaxBufferSize, "vertices")
		r.vertCap -= r.vertCap % 3 // whole triangles
		if r.spriteBuf == nil || r.spriteBuf.Size() < len(data) {
			if r.spriteBuf != nil {
				r.spriteBuf.Release()
			}
			r.spriteBuf = r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, max(len(data), 64*1024))
			if r.spriteBuf == nil {
				return fmt.Errorf("render: vertex buffer allocation failed")
			}
		}
		r.queue.WriteBuffer(r.spriteBuf, 0, data)
	}
	if data := floatBytes(r.glyphInst); len(data) > 0 {
		data, r.glyphCap = r.fitBuffer(data, floatsPerGlyphInst*4, gpu.MaxBufferSize, "glyphs")
		if r.glyphInstBuf == nil || r.glyphInstBuf.Size() < len(data) {
			if r.glyphInstBuf != nil {
				r.glyphInstBuf.Release()
			}
			r.glyphInstBuf = r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, max(len(data), 64*1024))
			if r.glyphInstBuf == nil {
				return fmt.Errorf("render: glyph instance buffer allocation failed")
			}
		}
		r.queue.WriteBuffer(r.glyphInstBuf, 0, data)
	}
	// Clip masks and blend groups render into offscreen targets that the
	// caller's pass then reads, so they go on their own submission first.
	// Targets are allocated before the clip uniforms are written (a clip
	// shape names the texture its bind group points at) and drawn after,
	// in the order the frame created them — which is dependency order,
	// since nothing can be used before it is made.
	r.allocOffscreen()
	r.prepareClips()
	r.renderOffscreen()

	r.drawBatches(pass, r.batches)
	return nil
}

// allocOffscreen makes the targets for this frame's clip masks and blend
// groups, and uploads the geometry their passes draw.
func (r *Renderer2D) allocOffscreen() {
	if len(r.resolve) == 0 {
		return
	}
	r.allocMaskTargets()
	r.allocGroupTargets()
}

// renderOffscreen draws every clip mask and blend group, in creation
// order, on one private command buffer.
func (r *Renderer2D) renderOffscreen() {
	if len(r.resolve) == 0 {
		return
	}
	enc := r.device.CreateCommandEncoder()
	drew := false
	for _, ref := range r.resolve {
		switch {
		case ref.mask > 0:
			drew = r.drawClipMask(enc, ref.mask-1) || drew
		case ref.group > 0:
			drew = r.drawGroup(enc, ref.group-1) || drew
		}
	}
	if !drew {
		return
	}
	r.queue.Submit(enc.Finish())
}

// drawBatches issues one draw list into a pass. The caller's frame and
// every blend-group target go through it, which is what lets a group's
// content be the same batches, drawn somewhere else.
func (r *Renderer2D) drawBatches(pass *gpu.RenderPass, batches []batch) {
	// Two pixels of AA feather, in NDC on the shorter axis.
	margin := float32(4) / max(min(r.width, r.height), 1)
	r.mergeScratch = append(r.mergeScratch[:0], batches...)
	batches = mergeBatches(r.mergeScratch, margin)
	// Scissor state: applied per batch, clamped to the target — and to
	// the frame's damage rect when one is set (see SetDamage).
	full := clipRect{x: 0, y: 0, w: int(r.width), h: int(r.height)}
	if r.damage.w >= 0 {
		x0 := min(max(0, r.damage.x), full.w)
		y0 := min(max(0, r.damage.y), full.h)
		x1 := min(max(x0, r.damage.x+r.damage.w), full.w)
		y1 := min(max(y0, r.damage.y+r.damage.h), full.h)
		full = clipRect{x: x0, y: y0, w: x1 - x0, h: y1 - y0}
	}
	setClip := func(pass *gpu.RenderPass, c clipRect) {
		if c.w < 0 {
			c = full
		}
		// Intersect with the target (or the damage rect): a fully
		// off-screen clip must become an empty in-bounds scissor, not a
		// validation error.
		x0 := min(max(full.x, c.x), full.x+full.w)
		y0 := min(max(full.y, c.y), full.y+full.h)
		x1 := min(max(x0, c.x+c.w), full.x+full.w)
		y1 := min(max(y0, c.y+c.h), full.y+full.h)
		pass.SetScissor(x0, y0, x1-x0, y1-y0)
	}
	curClip := noClip
	setClip(pass, curClip)
	// (Every per-batch clip below is intersected with full too.)

	last := batchKind(-1)
	lastMode := BlendNormal
	curSlot := -1
	for _, b := range batches {
		if b.clip != curClip {
			curClip = b.clip
			setClip(pass, curClip)
		}
		// group(1) is the clip shape. It is re-set whenever the shape or
		// the pipeline changes, since each pipeline has its own layout.
		if b.kind != kindComposite && (b.clipSlot != curSlot || b.kind != last) {
			if bg := r.clipBindFor(b.clipSlot, b.kind); bg != nil {
				pass.SetBindGroup(1, bg)
			}
			curSlot = b.clipSlot
		}
		switch b.kind {
		case kindQuad:
			if last != kindQuad {
				pass.SetPipeline(r.quadPipeline)
				pass.SetBindGroup(0, r.quadBind)
			}
			if n := min(b.count, r.quadCap-b.start); n > 0 {
				pass.Draw(6, n, 0, b.start)
			}
		case kindShadow:
			if last != kindShadow {
				pass.SetPipeline(r.shadowPipeline)
				pass.SetBindGroup(0, r.shadowBind)
			}
			if n := min(b.count, r.shadowCap-b.start); n > 0 {
				pass.Draw(6, n, 0, b.start)
			}
		case kindSprite:
			if last != kindSprite {
				pass.SetPipeline(r.spritePipeline)
				pass.SetVertexBuffer(0, r.spriteBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := spriteCount(b, r.vertCap); n > 0 {
				pass.Draw(n, 1, b.start, 0)
			}
		case kindColorized:
			if last != kindColorized {
				pass.SetPipeline(r.colorizePipeline)
				pass.SetVertexBuffer(0, r.spriteBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := spriteCount(b, r.vertCap); n > 0 {
				pass.Draw(n, 1, b.start, 0)
			}
		case kindPremul:
			if last != kindPremul {
				pass.SetPipeline(r.premulPipeline)
				pass.SetVertexBuffer(0, r.spriteBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := spriteCount(b, r.vertCap); n > 0 {
				pass.Draw(n, 1, b.start, 0)
			}
		case kindStencil:
			if last != kindStencil {
				pass.SetPipeline(r.stencilPipeline)
				pass.SetVertexBuffer(0, r.spriteBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := spriteCount(b, r.vertCap); n > 0 {
				pass.Draw(n, 1, b.start, 0)
			}
		case kindPremulAdd:
			if last != kindPremulAdd {
				pass.SetPipeline(r.premulAddPipeline)
				pass.SetVertexBuffer(0, r.spriteBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := spriteCount(b, r.vertCap); n > 0 {
				pass.Draw(n, 1, b.start, 0)
			}
		case kindSubpixel:
			if last != kindSubpixel {
				pass.SetPipeline(r.subpixelPipeline)
				pass.SetVertexBuffer(0, r.glyphInstBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := min(b.count, r.glyphCap-b.start); n > 0 {
				pass.Draw(6, n, 0, b.start)
			}
		case kindGlyph:
			if last != kindGlyph {
				pass.SetPipeline(r.glyphPipeline)
				pass.SetVertexBuffer(0, r.glyphInstBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := min(b.count, r.glyphCap-b.start); n > 0 {
				pass.Draw(6, n, 0, b.start)
			}
		case kindMSDF:
			if last != kindMSDF {
				pass.SetPipeline(r.msdfPipeline)
				pass.SetVertexBuffer(0, r.glyphInstBuf)
			}
			pass.SetBindGroup(0, b.bind)
			if n := min(b.count, r.glyphCap-b.start); n > 0 {
				pass.Draw(6, n, 0, b.start)
			}
		case kindShaderFX:
			// The effect owns its pipeline and bindings (shaderfx.go);
			// the web build has none and draws nothing.
			b.fx.draw(pass, b.count)
		case kindComposite:
			bind := r.compositeBind(b.group)
			pipe := r.compPipes[b.mode]
			if bind == nil || pipe == nil {
				continue // the group was dropped (empty, or no pipeline)
			}
			if last != kindComposite || b.mode != lastMode {
				pass.SetPipeline(pipe)
				pass.SetVertexBuffer(0, r.compVertBuf)
				lastMode = b.mode
			}
			pass.SetBindGroup(0, bind)
			pass.Draw(b.count, 1, b.start, 0)
		}
		last = b.kind
	}
}

// clampCornerRadius holds a corner radius to half the shorter side. Past
// that a rounded rectangle IS a stadium — the corner circles meet — and the
// shader's corner centre would fall outside the box, carving the quad into
// a lens instead. QML states pill radii that way routinely (`radius: 30` on
// a 36px-tall button), so the clamp belongs here rather than at each call.
func clampCornerRadius(radius, w, h float32) float32 {
	if radius <= 0 {
		return 0
	}
	return min(radius, min(w, h)/2)
}
