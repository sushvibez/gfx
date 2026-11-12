package render

// Post is the camera after the 3D renderer: the scene is rendered in high
// dynamic range (linear radiance in an rgba16float target) and Post turns
// it into the displayed image the way a film camera would —
//
//	scene (HDR) → bloom (a 13-tap downsample chain with firefly
//	suppression, tent upsampled back) → auto exposure (the scene's
//	log-average luminance, eased over time) → tone curve (ACES pulled
//	toward AgX) → grade (split tone, S-curve, lifted blacks, saturation)
//	→ vignette and chromatic aberration → FXAA, sharpen and film grain.
//
// It is opt-in: a Renderer3D built for FormatRGBA16Float with
// FrameSettings.Tonemap = TonemapHDR writes radiance, Post displays it.
// Everything is a fullscreen WGSL pass, the same on wgpu-native and in the
// browser.
//
//	r3, _ := render.New3D(dev, queue, gpu.FormatRGBA16Float)
//	post, _ := render.NewPost(dev, queue, surfaceFormat)
//	post.Resize(w, h)
//	r3.Render(enc, post.SceneView(), post.DepthView(), clear) // HDR
//	post.Render(enc, surfaceView)                             // display
//
// A View3D does both steps itself when its Post field is set.

import (
	"fmt"
	"math"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// TonemapHDR is the FrameSettings.Tonemap mode for rendering into Post:
// no tone curve, no clamp, no gamma — linear radiance out.
const TonemapHDR = 3

// PostSettings is the look. DefaultPostSettings is a neutral film look;
// the zero value is not meant to be used.
type PostSettings struct {
	// Exposure scales radiance before the tone curve (1 = as rendered).
	Exposure float32
	// AutoExposure (0…1) blends in eye adaptation: exposure follows the
	// scene's average luminance toward Key, within MinExposure…MaxExposure
	// (multipliers of Exposure), eased at AdaptRate per frame (0…1).
	AutoExposure, Key        float32
	MinExposure, MaxExposure float32
	AdaptRate                float32
	// Bloom (0…1) is how much of the blurred image replaces the sharp one
	// (an energy-conserving mix, so it glows without brightening).
	// BloomSpread (0…1) weights the wide mips against the tight ones.
	Bloom, BloomSpread float32
	// AgX (0…1) pulls the ACES tone curve toward AgX: 0 is punchy with
	// burning saturated highlights, 1 rolls highlights off to white.
	AgX float32
	// The grade, in display-linear: ShadowTint and HighlightTint multiply
	// the dark and bright ends (split toning), Contrast (0…1) mixes in an
	// S-curve, Lift is added to black, Saturation scales chroma (1 = as is).
	ShadowTint, HighlightTint [3]float32
	Contrast                  float32
	Lift                      [3]float32
	Saturation                float32
	// AO (0…1) is ambient occlusion's strength: corners, crevices and the
	// ground under things darken (0 skips the passes). AORadius is how far,
	// in world units, a surface looks for occluders. It needs the camera:
	// set Post.Projection each frame.
	AO, AORadius float32
	// Vignette (0…1) darkens the corners; ChromaticAberration is the
	// red/blue split at the corner, in UV units (0.002 is subtle).
	Vignette, ChromaticAberration float32
	// FXAA smooths edges after the tone curve; Sharpen (0…1) restores
	// some crispness; Grain is the film grain's amplitude (0.02 is fine).
	FXAA    bool
	Sharpen float32
	Grain   float32
}

// DefaultPostSettings is a gentle filmic look: mild bloom, eye
// adaptation, teal shadows against warm highlights, a soft vignette.
func DefaultPostSettings() PostSettings {
	return PostSettings{
		Exposure: 1, AutoExposure: 1, Key: 0.16, MinExposure: 0.35, MaxExposure: 4, AdaptRate: 0.04,
		Bloom: 0.07, BloomSpread: 0.6,
		AgX:        0.35,
		ShadowTint: [3]float32{0.86, 1.0, 1.10}, HighlightTint: [3]float32{1.06, 1.0, 0.92},
		Contrast: 0.2, Lift: [3]float32{0.004, 0.006, 0.008}, Saturation: 1.05,
		AO: 0.75, AORadius: 1.4,
		Vignette: 0.25, ChromaticAberration: 0.0012,
		FXAA: true, Sharpen: 0.15, Grain: 0.018,
	}
}

// postBloomMips caps the bloom chain; the smallest mip is at least 4 px.
const postBloomMips = 6

// Post owns the HDR scene target and the passes that display it.
type Post struct {
	// Settings is the look, read each Render.
	Settings PostSettings
	// Projection is the camera's projection matrix (perspective, depth 0…1)
	// for the frame in the scene target: ambient occlusion rebuilds
	// positions from depth with it. Zero disables the depth-based passes.
	Projection core.Mat4
	// View is the camera's view matrix for that frame; with Projection it
	// gives the atmosphere its view rays.
	View core.Mat4
	// Atmosphere is the air and the sky (post_atmos.go); zero is off.
	Atmosphere Atmosphere

	device *gpu.Device
	queue  *gpu.Queue
	format gpu.TextureFormat

	w, h int

	scene, depth         *gpu.Texture
	sceneView, depthView *gpu.TextureView
	down, up             []postTex // down[i] is 1/2^(i+1) size; up[i] matches down[i]
	adapt                [2]postTex
	ldr                  postTex
	ao                   [2]postTex // half resolution: the estimate, and blur scratch
	frame                int        // which adapt texture is current

	linear *gpu.Sampler
	uni    *gpu.Buffer
	time   float32

	plDownFirst, plDown, plUp, plAdapt, plComposite, plFinal *gpu.RenderPipeline
	plAO, plAOBlurH, plAOBlurV                               *gpu.RenderPipeline

	bgDown      []*gpu.BindGroup // into down[i]
	bgUp        []*gpu.BindGroup // into up[i]
	bgAdapt     [2]*gpu.BindGroup
	bgComposite [2]*gpu.BindGroup
	bgFinal     *gpu.BindGroup
	bgAO        *gpu.BindGroup

	// The atmosphere (post_atmos.go).
	shadowView                             *gpu.TextureView // the sun's shadow map (not owned)
	lightBuf                               *gpu.Buffer      // the renderer's light list (not owned)
	lightCount                             int
	reflect                                bool                             // trace scene reflections (the renderer asked for them)
	skyView                                *gpu.TextureView                 // the renderer's sky map (not owned)
	spotViews                              [MaxSpotShadows]*gpu.TextureView // the renderer's spot shadow maps (not owned)
	rainVP                                 core.Mat4
	noLights                               *gpu.Buffer
	lightVP                                core.Mat4
	atmosDirty                             bool
	vol                                    [2]postTex // half resolution: the shafts, and blur scratch
	lit                                    postTex    // the scene with the air and the sky: what the camera sees
	noShadow                               postTex
	atmosUni                               *gpu.Buffer
	plVol, plVolBlurH, plVolBlurV, plAtmos *gpu.RenderPipeline
	bgVol, bgAtmos                         *gpu.BindGroup
	bgVolBlur                              [2]*gpu.BindGroup
	bgAOBlur                               [2]*gpu.BindGroup // H: ao[0]→ao[1], V: ao[1]→ao[0]
}

type postTex struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	w, h int
}

func (t *postTex) release() {
	if t.view != nil {
		t.view.Release()
	}
	if t.tex != nil {
		t.tex.Release()
	}
	*t = postTex{}
}

// NewPost builds the passes for a display target of the given format (the
// surface's, or a View3D's).
func NewPost(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (*Post, error) {
	p := &Post{Settings: DefaultPostSettings(), device: device, queue: queue, format: format}
	p.linear = device.CreateSampler(true)
	p.uni = device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, postUniformSize)
	hdr := gpu.FormatRGBA16Float
	var err error
	mk := func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline {
		if err != nil {
			return nil
		}
		var m *gpu.ShaderModule
		if m, err = device.CreateShaderModuleWGSL(label, postCommonWGSL+body); err != nil {
			err = fmt.Errorf("render: post %s: %w", label, err)
			return nil
		}
		defer m.Release()
		var pl *gpu.RenderPipeline
		if pl, err = device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: m, VertexEntry: "vs", FragmentEntry: entry, Format: format,
		}); err != nil {
			err = fmt.Errorf("render: post %s: %w", label, err)
		}
		return pl
	}
	p.plDownFirst = mk("post-down-first", postDownWGSL, "fs_first", hdr)
	p.plDown = mk("post-down", postDownWGSL, "fs", hdr)
	p.plUp = mk("post-up", postUpWGSL, "fs", hdr)
	p.plAdapt = mk("post-adapt", postAdaptWGSL, "fs", hdr)
	p.plComposite = mk("post-composite", postCompositeWGSL, "fs", gpu.FormatRGBA8Unorm)
	p.plFinal = mk("post-final", postFinalWGSL, "fs", format)
	p.plAO = mk("post-ao", postAOWGSL, "fs", gpu.FormatRGBA8Unorm)
	p.plAOBlurH = mk("post-ao-blur-h", postAOBlurWGSL, "fs_h", gpu.FormatRGBA8Unorm)
	p.plAOBlurV = mk("post-ao-blur-v", postAOBlurWGSL, "fs_v", gpu.FormatRGBA8Unorm)
	p.atmosPipelines(mk)
	if err != nil {
		p.Release()
		return nil, err
	}
	return p, nil
}

// Size is the scene target's size in pixels (0, 0 before Resize).
func (p *Post) Size() (int, int) { return p.w, p.h }

// SceneView is the HDR colour target the Renderer3D draws into.
func (p *Post) SceneView() *gpu.TextureView { return p.sceneView }

// DepthView is the depth target that goes with SceneView.
func (p *Post) DepthView() *gpu.TextureView { return p.depthView }

// Resize (re)allocates the targets for a w×h pixel image; a no-op at the
// same size.
func (p *Post) Resize(w, h int) {
	w, h = max(w, 1), max(h, 1)
	if w == p.w && h == p.h && p.scene != nil {
		return
	}
	p.releaseTargets()
	p.w, p.h = w, h
	d := p.device
	hdr := gpu.FormatRGBA16Float
	tex := func(w, h int, f gpu.TextureFormat) postTex {
		t := d.CreateTexture2D(w, h, f, gpu.TextureRenderAttachment|gpu.TextureBinding)
		return postTex{tex: t, view: t.CreateView(), w: w, h: h}
	}
	p.scene = d.CreateTexture2D(w, h, hdr, gpu.TextureRenderAttachment|gpu.TextureBinding)
	p.sceneView = p.scene.CreateView()
	p.depth = d.CreateTexture2D(w, h, gpu.FormatDepth32Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
	p.depthView = p.depth.CreateView()
	mw, mh := w, h
	for i := 0; i < postBloomMips; i++ {
		mw, mh = max(mw/2, 1), max(mh/2, 1)
		p.down = append(p.down, tex(mw, mh, hdr))
		if min(mw, mh) <= 4 {
			break
		}
	}
	for i := 0; i < len(p.down)-1; i++ {
		p.up = append(p.up, tex(p.down[i].w, p.down[i].h, hdr))
	}
	p.adapt[0], p.adapt[1] = tex(1, 1, hdr), tex(1, 1, hdr)
	p.ldr = tex(w, h, gpu.FormatRGBA8Unorm)
	hw, hh := max(w/2, 1), max(h/2, 1)
	p.ao[0], p.ao[1] = tex(hw, hh, gpu.FormatRGBA8Unorm), tex(hw, hh, gpu.FormatRGBA8Unorm)
	p.atmosTargets(tex, w, h)

	// Bind groups. Each pipeline's layout is taken, used and released (a
	// kept layout keeps its pipeline alive).
	group := func(pl *gpu.RenderPipeline, entries ...gpu.BindGroupEntry) *gpu.BindGroup {
		l := pl.BindGroupLayout(0)
		defer l.Release()
		return d.CreateBindGroup(l, entries...)
	}
	texEntry := func(b int, v *gpu.TextureView) gpu.BindGroupEntry {
		return gpu.BindGroupEntry{Binding: b, TextureView: v}
	}
	samp := gpu.BindGroupEntry{Binding: 0, Sampler: p.linear}
	uni := func(b int) gpu.BindGroupEntry { return gpu.BindGroupEntry{Binding: b, Buffer: p.uni} }
	for i := range p.down {
		if i == 0 {
			p.bgDown = append(p.bgDown, group(p.plDownFirst, samp, texEntry(1, p.lit.view)))
		} else {
			p.bgDown = append(p.bgDown, group(p.plDown, samp, texEntry(1, p.down[i-1].view)))
		}
	}
	for i := range p.up {
		low := p.down[len(p.down)-1].view
		if i+1 < len(p.up) {
			low = p.up[i+1].view
		}
		p.bgUp = append(p.bgUp, group(p.plUp, samp, texEntry(1, low), texEntry(2, p.down[i].view), uni(3)))
	}
	bloom := p.down[0].view
	if len(p.up) > 0 {
		bloom = p.up[0].view
	}
	small := p.down[len(p.down)-1].view
	for k := 0; k < 2; k++ {
		// Frame k reads adapt[1-k] and writes adapt[k].
		p.bgAdapt[k] = group(p.plAdapt, samp, texEntry(1, small), texEntry(2, p.adapt[1-k].view), uni(3))
		p.bgComposite[k] = group(p.plComposite, samp, texEntry(1, p.lit.view), texEntry(2, bloom), texEntry(3, p.adapt[k].view), uni(4), texEntry(5, p.ao[0].view))
	}
	p.bgFinal = group(p.plFinal, samp, texEntry(1, p.ldr.view), uni(2))
	p.bgAO = group(p.plAO, texEntry(0, p.depthView), uni(1))
	p.bgAOBlur[0] = group(p.plAOBlurH, texEntry(0, p.depthView), texEntry(1, p.ao[0].view), uni(2))
	p.bgAOBlur[1] = group(p.plAOBlurV, texEntry(0, p.depthView), texEntry(1, p.ao[1].view), uni(2))
}

// Render displays the scene target into dst (the surface, or a view's
// colour target). Call after the Renderer3D rendered into SceneView.
func (p *Post) Render(enc *gpu.CommandEncoder, dst *gpu.TextureView) {
	if p.scene == nil {
		return
	}
	p.time += 1.0 / 60
	p.queue.WriteBuffer(p.uni, 0, floatBytes(p.uniforms()))
	black := gpu.Color{}
	pass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup) {
		rp := enc.BeginRenderPass(view, black)
		rp.SetPipeline(pl)
		rp.SetBindGroup(0, bg)
		rp.Draw(3, 1, 0, 0)
		rp.End()
	}
	p.atmosRender(enc) // the air and the sky: p.lit is what the camera sees
	for i := range p.down {
		pl := p.plDown
		if i == 0 {
			pl = p.plDownFirst
		}
		pass(p.down[i].view, pl, p.bgDown[i])
	}
	for i := len(p.up) - 1; i >= 0; i-- {
		pass(p.up[i].view, p.plUp, p.bgUp[i])
	}
	if p.aoOn() {
		white := gpu.Color{R: 1, G: 1, B: 1, A: 1}
		aoPass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup) {
			rp := enc.BeginRenderPass(view, white)
			rp.SetPipeline(pl)
			rp.SetBindGroup(0, bg)
			rp.Draw(3, 1, 0, 0)
			rp.End()
		}
		aoPass(p.ao[0].view, p.plAO, p.bgAO)
		aoPass(p.ao[1].view, p.plAOBlurH, p.bgAOBlur[0])
		aoPass(p.ao[0].view, p.plAOBlurV, p.bgAOBlur[1])
	}
	k := p.frame & 1
	pass(p.adapt[k].view, p.plAdapt, p.bgAdapt[k])
	pass(p.ldr.view, p.plComposite, p.bgComposite[k])
	pass(dst, p.plFinal, p.bgFinal)
	p.frame++
}

// postUniformSize is the uniform block: nine vec4f.
const postUniformSize = 9 * 16

// aoOn reports whether ambient occlusion runs: asked for, and the camera
// known.
func (p *Post) aoOn() bool { return p.Settings.AO > 0 && p.Projection[0] != 0 }

func (p *Post) uniforms() []float32 {
	s := p.Settings
	b := func(v bool) float32 {
		if v {
			return 1
		}
		return 0
	}
	ao := s.AO
	if !p.aoOn() {
		ao = 0
	}
	rate := s.AdaptRate
	if p.frame < 2 {
		rate = 1 // the first frames have no history: adapt at once
	}
	return []float32{
		s.Exposure, s.AutoExposure, s.Key, s.Bloom,
		s.MinExposure, s.MaxExposure, rate, s.AgX,
		s.Saturation, s.Vignette, s.ChromaticAberration, s.Grain,
		float32(math.Mod(float64(p.time), 64)), b(s.FXAA), s.Sharpen, s.BloomSpread,
		s.ShadowTint[0], s.ShadowTint[1], s.ShadowTint[2], s.Contrast,
		s.HighlightTint[0], s.HighlightTint[1], s.HighlightTint[2], 0,
		s.Lift[0], s.Lift[1], s.Lift[2], 0,
		ao, max(s.AORadius, 0.05), 0, 0,
		// What rebuilds a view position from depth: the projection's x and y
		// scales and its two depth terms (column-major: [0], [5], [10], [14]).
		p.Projection[0], p.Projection[5], p.Projection[10], p.Projection[14],
	}
}

func (p *Post) releaseTargets() {
	rel := func(gs ...*gpu.BindGroup) {
		for _, g := range gs {
			if g != nil {
				g.Release()
			}
		}
	}
	rel(p.bgDown...)
	rel(p.bgUp...)
	rel(p.bgAdapt[0], p.bgAdapt[1], p.bgComposite[0], p.bgComposite[1], p.bgFinal, p.bgAO, p.bgAOBlur[0], p.bgAOBlur[1])
	p.bgAO, p.bgAOBlur = nil, [2]*gpu.BindGroup{}
	p.bgDown, p.bgUp, p.bgFinal = nil, nil, nil
	p.bgAdapt, p.bgComposite = [2]*gpu.BindGroup{}, [2]*gpu.BindGroup{}
	for i := range p.down {
		p.down[i].release()
	}
	for i := range p.up {
		p.up[i].release()
	}
	p.down, p.up = nil, nil
	p.adapt[0].release()
	p.adapt[1].release()
	p.ldr.release()
	p.ao[0].release()
	p.ao[1].release()
	p.atmosReleaseTargets()
	if p.sceneView != nil {
		p.sceneView.Release()
		p.sceneView = nil
	}
	if p.scene != nil {
		p.scene.Release()
		p.scene = nil
	}
	if p.depthView != nil {
		p.depthView.Release()
		p.depthView = nil
	}
	if p.depth != nil {
		p.depth.Release()
		p.depth = nil
	}
	p.w, p.h = 0, 0
}

// Release frees everything Post made.
func (p *Post) Release() {
	p.releaseTargets()
	p.atmosRelease()
	for _, pl := range []**gpu.RenderPipeline{&p.plDownFirst, &p.plDown, &p.plUp, &p.plAdapt, &p.plComposite, &p.plFinal, &p.plAO, &p.plAOBlurH, &p.plAOBlurV} {
		if *pl != nil {
			(*pl).Release()
			*pl = nil
		}
	}
	if p.uni != nil {
		p.uni.Release()
		p.uni = nil
	}
	if p.linear != nil {
		p.linear.Release()
		p.linear = nil
	}
}

// ---- shaders ---------------------------------------------------------------

// postCommonWGSL: the fullscreen triangle and the shared uniform block.
const postCommonWGSL = `
struct VOut { @builtin(position) pos : vec4f, @location(0) uv : vec2f };
@vertex fn vs(@builtin(vertex_index) i : u32) -> VOut {
    var o : VOut;
    let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
    o.pos = vec4f(p * 2.0 - 1.0, 0.0, 1.0);
    o.uv = vec2f(p.x, 1.0 - p.y);
    return o;
}
struct PostU {
    p0 : vec4f,     // exposure, auto exposure, key, bloom
    p1 : vec4f,     // min exposure, max exposure, adapt rate, agx mix
    p2 : vec4f,     // saturation, vignette, chromatic aberration, grain
    p3 : vec4f,     // time, fxaa, sharpen, bloom spread
    shadow : vec4f, // rgb shadow tint, w contrast
    high : vec4f,   // rgb highlight tint
    lift : vec4f,   // rgb lift
    p4 : vec4f,     // ambient occlusion strength, radius (world)
    proj : vec4f,   // projection: x scale, y scale, depth terms [10] and [14]
};
fn luma(c : vec3f) -> f32 { return dot(c, vec3f(0.2126, 0.7152, 0.0722)); }
`

// postDownWGSL: the 13-tap downsample (Jimenez, "Next Generation Post
// Processing in Call of Duty: Advanced Warfare"); the first step weights
// its five 2×2 groups by 1/(1+luma) (a partial Karis average) so a single
// hot pixel cannot become a firefly blob.
const postDownWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var src : texture_2d<f32>;
fn s(uv : vec2f, o : vec2f, t : vec2f) -> vec3f {
    return min(max(textureSampleLevel(src, samp, uv + o * t, 0.0).rgb, vec3f(0.0)), vec3f(400.0));
}
fn kw(c : vec3f) -> f32 { return 1.0 / (1.0 + luma(c) * 0.08); }
fn down(uv : vec2f, first : bool) -> vec3f {
    let t = 1.0 / vec2f(textureDimensions(src));
    let a = s(uv, vec2f(-2.0, 2.0), t); let b = s(uv, vec2f(0.0, 2.0), t); let c = s(uv, vec2f(2.0, 2.0), t);
    let d = s(uv, vec2f(-2.0, 0.0), t); let e = s(uv, vec2f(0.0, 0.0), t); let f = s(uv, vec2f(2.0, 0.0), t);
    let g = s(uv, vec2f(-2.0, -2.0), t); let h = s(uv, vec2f(0.0, -2.0), t); let i = s(uv, vec2f(2.0, -2.0), t);
    let j = s(uv, vec2f(-1.0, 1.0), t); let k = s(uv, vec2f(1.0, 1.0), t);
    let l = s(uv, vec2f(-1.0, -1.0), t); let m = s(uv, vec2f(1.0, -1.0), t);
    if (first) {
        let g0 = (a + b + d + e) * 0.25; let g1 = (b + c + e + f) * 0.25;
        let g2 = (d + e + g + h) * 0.25; let g3 = (e + f + h + i) * 0.25;
        let g4 = (j + k + l + m) * 0.25;
        let w0 = kw(g0) * 0.125; let w1 = kw(g1) * 0.125; let w2 = kw(g2) * 0.125;
        let w3 = kw(g3) * 0.125; let w4 = kw(g4) * 0.5;
        return (g0 * w0 + g1 * w1 + g2 * w2 + g3 * w3 + g4 * w4) / (w0 + w1 + w2 + w3 + w4);
    }
    return e * 0.125 + (a + c + g + i) * 0.03125 + (b + d + f + h) * 0.0625 + (j + k + l + m) * 0.125;
}
@fragment fn fs_first(in : VOut) -> @location(0) vec4f { return vec4f(down(in.uv, true), 1.0); }
@fragment fn fs(in : VOut) -> @location(0) vec4f { return vec4f(down(in.uv, false), 1.0); }
`

// postUpWGSL: a 3×3 tent of the lower mip mixed into this one.
const postUpWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var low : texture_2d<f32>;
@group(0) @binding(2) var high : texture_2d<f32>;
@group(0) @binding(3) var<uniform> U : PostU;
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let o = 1.0 / vec2f(textureDimensions(low));
    let uv = in.uv;
    var t = textureSampleLevel(low, samp, uv, 0.0).rgb * 4.0;
    t += (textureSampleLevel(low, samp, uv + vec2f(o.x, 0.0), 0.0).rgb + textureSampleLevel(low, samp, uv - vec2f(o.x, 0.0), 0.0).rgb
        + textureSampleLevel(low, samp, uv + vec2f(0.0, o.y), 0.0).rgb + textureSampleLevel(low, samp, uv - vec2f(0.0, o.y), 0.0).rgb) * 2.0;
    t += textureSampleLevel(low, samp, uv + o, 0.0).rgb + textureSampleLevel(low, samp, uv - o, 0.0).rgb
        + textureSampleLevel(low, samp, uv + vec2f(o.x, -o.y), 0.0).rgb + textureSampleLevel(low, samp, uv + vec2f(-o.x, o.y), 0.0).rgb;
    t *= 1.0 / 16.0;
    return vec4f(mix(textureSampleLevel(high, samp, uv, 0.0).rgb, t, U.p3.w), 1.0);
}
`

// postAdaptWGSL: the eye. The smallest bloom mip is metered (log average,
// centre weighted) and eased toward over time; the 1×1 result is the
// adapted luminance.
const postAdaptWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var small : texture_2d<f32>;
@group(0) @binding(2) var prev : texture_2d<f32>;
@group(0) @binding(3) var<uniform> U : PostU;
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    var ls = 0.0; var ws = 0.0;
    for (var y = 0; y < 5; y++) {
        for (var x = 0; x < 7; x++) {
            let uv = (vec2f(f32(x), f32(y)) + 0.5) / vec2f(7.0, 5.0);
            let w = 1.0 - 0.75 * smoothstep(0.1, 0.55, length(uv - 0.5));
            let l = luma(textureSampleLevel(small, samp, uv, 0.0).rgb);
            ls += log(max(l, 1e-4)) * w; ws += w;
        }
    }
    let avg = clamp(exp(ls / ws), 0.002, 40.0);
    let p = textureLoad(prev, vec2i(0, 0), 0).r;
    var a = mix(p, avg, U.p1.z);
    if (!(a > 0.0) || a > 1e4) { a = avg; }   // NaN and first-frame guard
    return vec4f(a, 0.0, 0.0, 1.0);
}
`

// postCompositeWGSL: bloom, exposure, the tone curve and the grade, into
// display space with luma in alpha for FXAA.
const postCompositeWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var scene : texture_2d<f32>;
@group(0) @binding(2) var bloom : texture_2d<f32>;
@group(0) @binding(3) var adapt : texture_2d<f32>;
@group(0) @binding(4) var<uniform> U : PostU;
@group(0) @binding(5) var aotex : texture_2d<f32>;

// ACES, Stephen Hill's fit (RRT and ODT).
fn rrt_odt(v : vec3f) -> vec3f {
    let a = v * (v + 0.0245786) - 0.000090537;
    let b = v * (0.983729 * v + 0.4329510) + 0.238081;
    return a / b;
}
fn aces(c : vec3f) -> vec3f {
    let m_in = mat3x3f(vec3f(0.59719, 0.07600, 0.02840), vec3f(0.35458, 0.90834, 0.13383), vec3f(0.04823, 0.01566, 0.83777));
    let m_out = mat3x3f(vec3f(1.60475, -0.10208, -0.00327), vec3f(-0.53108, 1.10813, -0.07276), vec3f(-0.07367, -0.00605, 1.07602));
    return clamp(m_out * rrt_odt(m_in * (c * 1.3)), vec3f(0.0), vec3f(1.0));
}
// AgX (Troy Sobotka), the Filament/Blender fit.
fn agx_contrast(x : vec3f) -> vec3f {
    let x2 = x * x; let x4 = x2 * x2;
    return 15.5 * x4 * x2 - 40.14 * x4 * x + 31.96 * x4 - 6.868 * x2 * x + 0.4298 * x2 + 0.1191 * x - 0.00232;
}
fn agx(c : vec3f) -> vec3f {
    let to2020 = mat3x3f(vec3f(0.6274, 0.0691, 0.0164), vec3f(0.3293, 0.9195, 0.0880), vec3f(0.0433, 0.0113, 0.8956));
    let from2020 = mat3x3f(vec3f(1.6605, -0.1246, -0.0182), vec3f(-0.5876, 1.1329, -0.1006), vec3f(-0.0728, -0.0083, 1.1187));
    let inset = mat3x3f(vec3f(0.856627153315983, 0.137318972929847, 0.11189821299995), vec3f(0.0951212405381588, 0.761241990602591, 0.0767994186031903), vec3f(0.0482516061458583, 0.101439036467562, 0.811302368396859));
    let outset = mat3x3f(vec3f(1.1271005818144368, -0.1413297634984383, -0.14132976349843826), vec3f(-0.11060664309660323, 1.157823702216272, -0.11060664309660294), vec3f(-0.016493938717834573, -0.016493938717834257, 1.2519364065950405));
    let min_ev = -12.47393; let max_ev = 4.026069;
    var v = inset * (to2020 * c);
    v = clamp((log2(max(v, vec3f(1e-10))) - min_ev) / (max_ev - min_ev), vec3f(0.0), vec3f(1.0));
    v = agx_contrast(v);
    v = outset * v;
    v = pow(max(v, vec3f(0.0)), vec3f(2.2));
    return clamp(from2020 * v, vec3f(0.0), vec3f(1.0));
}
fn to_srgb(c : vec3f) -> vec3f {
    return mix(c * 12.92, 1.055 * pow(max(c, vec3f(0.0)), vec3f(1.0 / 2.4)) - 0.055, step(vec3f(0.0031308), c));
}
fn exposure() -> f32 {
    let a = textureLoad(adapt, vec2i(0, 0), 0).r;
    let e = clamp(pow(U.p0.z / max(a, 1e-4), 0.85), U.p1.x, U.p1.y);
    return U.p0.x * mix(1.0, e, U.p0.y);
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let uv = in.uv;
    let c = uv - 0.5;
    let dims = vec2f(textureDimensions(scene));
    let asp = dims.x / dims.y;
    let ca = c * vec2f(asp, 1.0);
    let r2 = dot(ca, ca) / (0.25 * (asp * asp + 1.0));   // 0 centre … 1 corner
    // radial chromatic aberration
    let off = c * r2 * U.p2.z;
    var col = vec3f(textureSampleLevel(scene, samp, uv + off, 0.0).r,
                    textureSampleLevel(scene, samp, uv, 0.0).g,
                    textureSampleLevel(scene, samp, uv - off, 0.0).b);
    col = max(col, vec3f(0.0));
    // Ambient occlusion belongs to ambient light: it fades on brightly lit
    // pixels, so sunlit surfaces never get dirty halos; faintly cool.
    let e = exposure();
    let occ = textureSampleLevel(aotex, samp, uv, 0.0).r;
    let lit = smoothstep(0.16, 0.85, luma(col) * e);
    let a = mix(1.0, occ, U.p4.x * (1.0 - 0.8 * lit));
    col *= mix(vec3f(a), vec3f(1.0), vec3f(0.0, 0.03, 0.07));
    // bloom: an energy-conserving mix
    col = mix(col, textureSampleLevel(bloom, samp, uv, 0.0).rgb, U.p0.w);
    col *= e;
    // the film curve: ACES for punch, pulled toward AgX so highlights roll off to white
    col = mix(aces(col), agx(col), U.p1.w);
    // ---- grade (display-linear) ----
    var l = luma(col);
    col *= mix(U.shadow.rgb, U.high.rgb, smoothstep(0.03, 0.55, l));   // split tone
    col = mix(col, col * col * (3.0 - 2.0 * col), U.shadow.w);          // S-curve
    col = col * (1.0 - max(U.lift.r, max(U.lift.g, U.lift.b))) + U.lift.rgb;
    l = luma(col);
    col = mix(vec3f(l), col, U.p2.x);                                    // saturation
    let vr = smoothstep(0.15, 1.05, r2);
    col *= 1.0 - U.p2.y * vr * vr * (3.0 - 2.0 * vr);                    // vignette
    col = to_srgb(clamp(col, vec3f(0.0), vec3f(1.0)));
    return vec4f(col, dot(col, vec3f(0.299, 0.587, 0.114)));
}
`

// postFinalWGSL: FXAA (the luma is in alpha), a little sharpening, and
// film grain, into the display target.
const postFinalWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var<uniform> U : PostU;
fn hash12(p : vec2f) -> f32 {
    var p3 = fract(vec3f(p.xyx) * 0.1031);
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.x + p3.y) * p3.z);
}
fn t(uv : vec2f) -> vec4f { return textureSampleLevel(src, samp, uv, 0.0); }
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let uv = in.uv;
    let px = 1.0 / vec2f(textureDimensions(src));
    let M = t(uv);
    var col = M.rgb;
    let NW = t(uv + vec2f(-1.0, -1.0) * px); let NE = t(uv + vec2f(1.0, -1.0) * px);
    let SW = t(uv + vec2f(-1.0, 1.0) * px);  let SE = t(uv + vec2f(1.0, 1.0) * px);
    let l_min = min(M.a, min(min(NW.a, NE.a), min(SW.a, SE.a)));
    let l_max = max(M.a, max(max(NW.a, NE.a), max(SW.a, SE.a)));
    if (U.p3.y > 0.5 && (l_max - l_min) > max(0.0312, l_max * 0.10)) {
        var dir = vec2f(-((NW.a + NE.a) - (SW.a + SE.a)), (NW.a + SW.a) - (NE.a + SE.a));
        let reduce = max((NW.a + NE.a + SW.a + SE.a) * 0.03125, 0.0078125);
        let rcp = 1.0 / (min(abs(dir.x), abs(dir.y)) + reduce);
        dir = clamp(dir * rcp, vec2f(-8.0), vec2f(8.0)) * px;
        let A = 0.5 * (t(uv + dir * (1.0 / 3.0 - 0.5)) + t(uv + dir * (2.0 / 3.0 - 0.5)));
        let B = A * 0.5 + 0.25 * (t(uv - dir * 0.5) + t(uv + dir * 0.5));
        if (B.a < l_min || B.a > l_max) { col = A.rgb; } else { col = B.rgb; }
    } else {
        // sharpen only where FXAA left the pixel alone
        let blur = (NW.rgb + NE.rgb + SW.rgb + SE.rgb) * 0.25;
        col = col + (col - blur) * U.p3.z;
    }
    // film grain: stronger in the mid tones, moving with time
    let n = hash12(in.pos.xy + vec2f(U.p3.x * 61.0, U.p3.x * 37.0)) - 0.5;
    let lum = dot(col, vec3f(0.299, 0.587, 0.114));
    col += n * U.p2.w * (1.0 - 0.6 * abs(lum * 2.0 - 1.0));
    return vec4f(clamp(col, vec3f(0.0), vec3f(1.0)), 1.0);
}
`

// postDepthWGSL: view-space position from the depth buffer and the
// projection (depth 0…1, right-handed: the camera looks down -z).
const postDepthWGSL = `
fn view_dist(d : f32) -> f32 { return U.proj.w / (d + U.proj.z); }
fn view_pos(uv : vec2f, z : f32) -> vec3f {
    let ndc = vec2f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    return vec3f(ndc.x / U.proj.x * z, ndc.y / U.proj.y * z, -z);
}
fn ign(p : vec2f) -> f32 { return fract(52.9829189 * fract(dot(p, vec2f(0.06711056, 0.00583715)))); }
`

// postAOWGSL: ambient occlusion at half resolution (the Alchemy / Scalable
// Ambient Obscurance estimator, McGuire et al.): the normal comes from the
// depth buffer's nearer neighbours, the taps spiral out over the projected
// radius with a per-pixel rotation, and each contributes by how far it
// rises above the surface's tangent plane.
const postAOWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var<uniform> U : PostU;
` + postDepthWGSL + `
const AO_TAPS = 10;
// The view position of the depth texel under uv: the position is rebuilt
// at that texel's centre, not at uv, or a slanted surface read through
// nearest texels would look stepped and occlude itself.
fn pos_texel(c : vec2i, dims : vec2f) -> vec3f {
    let t = clamp(c, vec2i(0), vec2i(dims) - 1);
    return view_pos((vec2f(t) + 0.5) / dims, view_dist(textureLoad(depth, t, 0)));
}
fn pos_at(uv : vec2f, dims : vec2f) -> vec3f { return pos_texel(vec2i(floor(uv * dims)), dims); }
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(depth));
    // This half-resolution pixel's depth texel, by integer: its centre sits
    // exactly on a corner between four depth texels, where a UV lookup
    // would pick rows unpredictably and band the result.
    let c0 = min(vec2i(in.pos.xy) * 2, vec2i(dims) - 1);
    let uv = (vec2f(c0) + 0.5) / dims;
    let d0 = textureLoad(depth, c0, 0);
    if (d0 >= 0.99999) { return vec4f(1.0); }   // the sky
    let z0 = view_dist(d0);
    if (z0 > 140.0) { return vec4f(1.0); }
    let P = pos_texel(c0, dims);
    let pl = pos_texel(c0 - vec2i(1, 0), dims); let pr = pos_texel(c0 + vec2i(1, 0), dims);
    let pu = pos_texel(c0 - vec2i(0, 1), dims); let pd = pos_texel(c0 + vec2i(0, 1), dims);
    // The nearer neighbour on each axis: no normals bent across an edge.
    var dx = pr - P;
    if (abs(pl.z - P.z) < abs(pr.z - P.z)) { dx = P - pl; }
    var dy = pd - P;
    if (abs(pu.z - P.z) < abs(pd.z - P.z)) { dy = P - pu; }
    var n = normalize(cross(dx, dy));
    if (dot(n, P) > 0.0) { n = -n; }             // face the camera
    let R = U.p4.y * (0.6 + 0.4 * clamp(z0 / 12.0, 0.0, 1.0));
    let r_uv = clamp(R * U.proj.y / (2.0 * z0), 3.0 / dims.y, 0.14);
    let R2 = R * R;
    let aspect = dims.x / dims.y;
    let rot = ign(in.pos.xy) * 6.2831853;
    var sum = 0.0;
    for (var i = 0; i < AO_TAPS; i++) {
        let a = (f32(i) + 0.5) / f32(AO_TAPS);
        let th = a * 6.2831853 * 3.0 + rot;
        var o = vec2f(cos(th), sin(th)) * a * r_uv;
        o.x /= aspect;
        let uv2 = uv + o;
        let v = pos_at(uv2, dims) - P;
        let vv = dot(v, v);
        let vn = dot(v, n);
        let f = max(R2 - vv, 0.0);
        sum += f * f * f * max((vn - (0.015 + 0.004 * z0)) / (vv + 0.004), 0.0);
    }
    var ao = max(0.0, 1.0 - sum * 5.0 / (R2 * R2 * R2 * f32(AO_TAPS)));
    ao = mix(ao, 1.0, smoothstep(70.0, 140.0, z0));
    return vec4f(ao, ao, ao, 1.0);
}
`

// postAOBlurWGSL: a separable blur that does not cross depth edges.
const postAOBlurWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var<uniform> U : PostU;
` + postDepthWGSL + `
fn blur(frag : vec2f, dir : vec2i) -> vec4f {
    // Integer texels throughout (see the AO pass): this pixel is p in the
    // half-resolution source and 2p in the depth buffer.
    let dmax = vec2i(textureDimensions(depth)) - 1;
    let smax = vec2i(textureDimensions(src)) - 1;
    let p = vec2i(frag);
    let z0 = min(view_dist(textureLoad(depth, min(p * 2, dmax), 0)), 300.0);
    var acc = 0.0; var ws = 0.0;
    for (var i = -3; i <= 3; i++) {
        let q = clamp(p + dir * i, vec2i(0), smax);
        let z = min(view_dist(textureLoad(depth, min(q * 2, dmax), 0)), 300.0);
        let w = (1.0 - abs(f32(i)) * 0.22) * max(0.0, 1.0 - abs(z - z0) / (0.04 * z0 + 0.02));
        acc += textureLoad(src, q, 0).r * w; ws += w;
    }
    var v = textureLoad(src, min(p, smax), 0).r;
    if (ws > 1e-4) { v = acc / ws; }
    return vec4f(v, v, v, 1.0);
}
@fragment fn fs_h(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(1, 0)); }
@fragment fn fs_v(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(0, 1)); }
`
