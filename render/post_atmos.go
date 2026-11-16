package render

// The air and the sky, for Post. Between the 3D pass and the camera:
//
//	shafts  — at half resolution, each pixel's view ray is marched from
//	          the eye to the surface it ends on; at every step the sun's
//	          shadow map says whether that bit of air is lit, and lit air
//	          scatters sunlight toward the eye (single scattering, a
//	          two-lobe Henyey-Greenstein phase, density thinning with
//	          height). Unlit air still scatters the sky's ambient. The
//	          result is in-scattered light and what is left of the
//	          surface's own (transmittance).
//	sky     — where the ray ends on nothing: a zenith-to-horizon gradient,
//	          a warm band toward the sun, the sun's glow and its disc.
//	combine — scene × transmittance + in-scatter, or the sky, into the HDR
//	          image the bloom and the tone curve then see.
//
// The sun's shadow map and matrix come from the Renderer3D (UseShadow;
// View3D does it), the camera from Post.View and Post.Projection.

import (
	"strings"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/wgsl"
)

// Atmosphere is the air and the sky. The zero value is off: the scene
// passes through untouched and empty pixels keep the clear colour.
type Atmosphere struct {
	// Sky draws the sky where nothing was rendered. Zenith and Horizon are
	// its colours (linear radiance) overhead and at the horizon.
	Sky             bool
	Zenith, Horizon [3]float32
	// Shafts is the air's density: the scattering coefficient per world
	// unit at Height 0 (0 = no volumetric light; 0.01 is a clear evening
	// haze, 0.03 thick). Falloff is the height over which the air thins
	// to about a third (0 = 6). MaxDistance is how far the march goes
	// (0 = 140); beyond it the haze continues analytically into the
	// horizon colour.
	Shafts, Falloff, MaxDistance float32
	// Lamps is how strongly the Renderer3D's point and spot lights light
	// the air (0 = not at all; 1 = as they light surfaces): a spot lamp
	// then shows its cone as a beam in the haze. It needs Shafts > 0 (the
	// air must have something in it) and a renderer (UseShadow). Lamps
	// cast no shadows, so a beam passes through what stands in it.
	Lamps float32
	// Rain (0…1) draws rain in the air: streaks falling past the camera in
	// three layers of depth, hidden behind whatever is nearer than they
	// are, lit by the ambient light and by what shines behind them.
	Rain float32
	// SunDir is the direction the sunlight travels, Sun its radiance and
	// Ambient the sky's fill light: normally the Renderer3D's own
	// (FrameSettings.Sun and Ambient, and Begin's light direction).
	SunDir  core.Vec3
	Sun     [3]float32
	Ambient [3]float32
	// NoFarHaze stops the haze at MaxDistance: past it the scene is left as
	// it is instead of fading into the horizon colour. For a renderer that
	// draws its own aerial perspective (a planet's), so the far field is
	// not fogged twice; the shafts and the near haze are still added.
	NoFarHaze bool
}

// atmosUniforms mirrors the WGSL AtmU struct.
type atmosUniforms struct {
	InvView core.Mat4 // the camera's world matrix
	LightVP core.Mat4
	Cam     core.Vec4 // xyz eye
	SunDir  core.Vec4 // xyz toward the sun, w 1 when there is a shadow map
	Sun     core.Vec4 // rgb radiance, w density (0 = no shafts)
	Amb     core.Vec4 // rgb, w height falloff
	Zenith  core.Vec4 // rgb, w 1 when the sky is drawn
	Horizon core.Vec4 // rgb, w max march distance
	Proj    core.Vec4 // projection x and y scales, depth terms
	Lamps   core.Vec4 // x light count, y how strongly lamps light the air, z 1 when scene reflections are traced
	Rain    core.Vec4 // x how hard it rains, y time in seconds, z 1 when reflections were also traced, w the march's noise frame (0: still)
	RainVP  core.Mat4 // the renderer's sky map (all zero: none), so rain stops under roofs
	Depth   core.Vec4 // x log depth scale (0 = perspective), y 1 = no haze past the march, zw the projection's jitter (NDC)
	Refl    core.Vec4 // a foreign renderer's reflections (UseReflection): x 1 = level mirrors, y 1 = a sky map, z 1 = the sky is in the scene, w blur
	Origin  core.Vec4 // xyz the eye in a frame fixed to the world, for the ripples' phase
}

// Reflection asks Post to trace screen-space reflections of the scene for
// a renderer that is not a Renderer3D (UseReflection; a Renderer3D asks
// through FrameSettings.SceneReflection and UseShadow). The renderer's
// materials leave their mirror strength m in the scene target's alpha as
// 1 − m (negated for moving water, whose mirror Post ripples), having
// already added the sky's reflection, m × sky; where a traced ray meets
// the scene, Post swaps that sky for what the ray meets.
type Reflection struct {
	// Sky is the sky the materials put in their mirrors, as an
	// equirectangular map in Post's world (u = ½ + atan2(x, −z)/2π,
	// v = acos(y)/π; linear radiance on the scene's scale). Nil: the
	// Atmosphere's analytic sky.
	Sky *gpu.TextureView
	// Level says every mirror lies level (water, puddles, wet paving): the
	// normal is Post's world up rather than the depth buffer's, which on a
	// terrain mesh carries the relief under the water.
	Level bool
	// SkyInScene says the renderer drew its sky into the scene target: a
	// ray that meets nothing takes the sky from where its direction shows
	// on screen (the clouds in a lake) before falling back to Sky.
	SkyInScene bool
	// Origin is the eye's position in a frame fixed to the world (Post's
	// axes), for the ripples' phase when the renderer keeps the eye at
	// Post's origin; zero otherwise.
	Origin core.Vec3
	// Blur smears the reflection in a weak mirror (wet ground rather than
	// standing water) down the screen, as wet tarmac draws a lamp out into
	// a streak: the spread is Blur × (1 − m)² of the ray's length (0 =
	// sharp everywhere).
	Blur float32
}

// UseReflection turns screen-space reflections on for a renderer that is
// not a Renderer3D (see Reflection), or off with nil. Call it again when
// a field changes; UseShadow replaces it.
func (p *Post) UseReflection(r *Reflection) {
	if r == nil {
		if p.foreignRefl != nil {
			p.atmosDirty = true
		}
		p.foreignRefl, p.reflect = nil, false
		return
	}
	if p.foreignRefl == nil || p.foreignRefl.Sky != r.Sky {
		p.atmosDirty = true
	}
	c := *r
	p.foreignRefl, p.reflect = &c, true
}

// UseShadow gives Post the sun's shadow map and matrix and the frame's
// lights from a Renderer3D (as of its last Begin), for the light shafts
// and the lamps' beams. View3D calls it.
func (p *Post) UseShadow(r *Renderer3D) {
	if r == nil {
		if p.shadowView != nil || p.lightBuf != nil || p.skyView != nil {
			p.atmosDirty = true
		}
		p.shadowView, p.lightBuf, p.lightCount, p.reflect = nil, nil, 0, false
		p.skyView, p.rainVP, p.foreignRefl = nil, core.Mat4{}, nil
		p.traceNodes, p.traceInsts, p.traceTris, p.bounceOn, p.reflectTraced = nil, nil, nil, false, false
		p.hazeTraced = false
		p.spotViews = [MaxSpotShadows]*gpu.TextureView{}
		return
	}
	if p.shadowView != r.shadowView || p.lightBuf != r.lightBuf || p.skyView != r.skyView || p.spotViews != r.spotViews {
		p.shadowView, p.lightBuf, p.skyView, p.spotViews = r.shadowView, r.lightBuf, r.skyView, r.spotViews
		p.atmosDirty = true
	}
	p.rainVP = r.lastRainVP
	if p.traceNodes != r.traceNodes || p.traceInsts != r.traceInsts || p.traceTris != r.traceTris {
		p.traceNodes, p.traceInsts, p.traceTris = r.traceNodes, r.traceInsts, r.traceTris
		p.traceMeshes = r.traceMeshes
		p.bounceDirty, p.reflDirty = true, true
		p.atmosDirty = true // the traced shafts bind the trace scene
	}
	p.hazeTraced = r.settings.TracedHaze && r.traceShapes > 0
	p.bounceOn = r.settings.TracedBounce && r.traceShapes > 0
	p.reflectTraced = r.settings.TracedReflections && r.traceShapes > 0 &&
		r.settings.SceneReflection > 0 && r.settings.SkyReflection > 0
	p.lightVP = r.lastLightVP
	p.lightCount = r.lastLightCount
	p.reflect = r.settings.SceneReflection > 0 && r.settings.SkyReflection > 0
	if p.foreignRefl != nil {
		p.foreignRefl = nil
		p.atmosDirty = true
	}
}

// UseSunShadow gives Post a sun shadow map drawn by a renderer that is
// not a Renderer3D, for the light shafts: a depth map (0 nearest the sun)
// and the matrix taking a point in Post's world (the frame View is the
// camera of) to its clip space, x and y −1…1, z 0…1. The renderer must
// have drawn it before Render, in the same encoder or earlier. Call it
// every frame the matrix moves; nil turns the map off again (the air is
// then lit everywhere). It replaces what UseShadow gave.
func (p *Post) UseSunShadow(shadow *gpu.TextureView, lightVP core.Mat4) {
	if p.shadowView != shadow {
		p.shadowView = shadow
		p.atmosDirty = true
	}
	p.lightVP = lightVP
}

// atmosInit builds the atmosphere's pipelines (NewPost calls it through mk).
func (p *Post) atmosPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	hdr := gpu.FormatRGBA16Float
	p.plVol = mk("post-shafts", postVolSource(false), "fs", hdr)
	p.plVolTraced = mk("post-shafts-traced", traceWGSL+postVolSource(true), "fs", hdr)
	p.plVolBlurH = mk("post-shafts-blur-h", postVolBlurWGSL, "fs_h", hdr)
	p.plVolBlurV = mk("post-shafts-blur-v", postVolBlurWGSL, "fs_v", hdr)
	p.plAtmos = mk("post-atmosphere", postAtmosWGSL, "fs", hdr)
}

// atmosTargets allocates the atmosphere's targets (Resize calls it).
func (p *Post) atmosTargets(tex func(w, h int, f gpu.TextureFormat) postTex, w, h int) {
	hdr := gpu.FormatRGBA16Float
	hw, hh := max(w/2, 1), max(h/2, 1)
	p.vol[0], p.vol[1] = tex(hw, hh, hdr), tex(hw, hh, hdr)
	p.lit = tex(w, h, hdr)
	if p.noShadow.tex == nil {
		// A stand-in for the shadow map when no renderer supplied one.
		t := p.device.CreateTexture2D(1, 1, gpu.FormatDepth32Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
		p.noShadow = postTex{tex: t, view: t.CreateView(), w: 1, h: 1}
	}
	if p.noSky.tex == nil {
		// A stand-in for a foreign renderer's sky map.
		t := p.device.CreateTexture2D(1, 1, hdr, gpu.TextureBinding|gpu.TextureCopyDst)
		p.noSky = postTex{tex: t, view: t.CreateView(), w: 1, h: 1}
	}
	if p.noLights == nil {
		// A stand-in for the renderer's light list.
		p.noLights = p.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(lightBlock{})))
	}
	if p.atmosUni == nil {
		p.atmosUni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(atmosUniforms{})))
	}
	p.atmosDirty = true
}

// atmosBind (re)builds the atmosphere's bind groups: after a resize, or
// when the shadow map changed.
func (p *Post) atmosBind() {
	if !p.atmosDirty || p.lit.view == nil {
		return
	}
	p.atmosDirty = false
	p.atmosReleaseBinds()
	d := p.device
	group := func(pl *gpu.RenderPipeline, entries ...gpu.BindGroupEntry) *gpu.BindGroup {
		l := pl.BindGroupLayout(0)
		defer l.Release()
		return d.CreateBindGroup(l, entries...)
	}
	tex := func(b int, v *gpu.TextureView) gpu.BindGroupEntry {
		return gpu.BindGroupEntry{Binding: b, TextureView: v}
	}
	uni := func(b int) gpu.BindGroupEntry { return gpu.BindGroupEntry{Binding: b, Buffer: p.atmosUni} }
	shadow := p.shadowView
	if shadow == nil {
		shadow = p.noShadow.view
	}
	lights := p.lightBuf
	if lights == nil {
		lights = p.noLights
	}
	vol := []gpu.BindGroupEntry{tex(0, p.depthView), tex(1, shadow), uni(2), {Binding: 3, Buffer: lights}}
	for i, v := range p.spotViews {
		if v == nil {
			v = p.noShadow.view
		}
		vol = append(vol, tex(4+i, v))
	}
	p.bgVol = group(p.plVol, vol...)
	if p.traceNodes != nil && p.plVolTraced != nil {
		// The traced shafts read no shadow map: the trace scene instead.
		p.bgVolTraced = group(p.plVolTraced, tex(0, p.depthView), uni(2), gpu.BindGroupEntry{Binding: 3, Buffer: lights},
			gpu.BindGroupEntry{Binding: 10, Buffer: p.traceNodes},
			gpu.BindGroupEntry{Binding: 11, Buffer: p.traceInsts},
			gpu.BindGroupEntry{Binding: 13, Buffer: p.traceTris})
	}
	p.bgVolBlur[0] = group(p.plVolBlurH, tex(0, p.depthView), tex(1, p.vol[0].view), uni(2))
	p.bgVolBlur[1] = group(p.plVolBlurV, tex(0, p.depthView), tex(1, p.vol[1].view), uni(2))
	skyMap := p.skyView
	if skyMap == nil {
		skyMap = p.noShadow.view
	}
	reflSky := p.noSky.view
	if p.foreignRefl != nil && p.foreignRefl.Sky != nil {
		reflSky = p.foreignRefl.Sky
	}
	p.bgAtmos = group(p.plAtmos, tex(0, p.depthView), tex(1, p.sceneView), tex(2, p.vol[0].view), uni(3), tex(4, skyMap), tex(5, p.refl.view), tex(6, reflSky))
	p.reflDirty = true // its own group reads the same views
}

// atmosRender records the shafts and the combine pass into dst (p.lit,
// which everything after reads, or motion blur's input).
func (p *Post) atmosRender(enc *gpu.CommandEncoder, dst *gpu.TextureView) {
	p.atmosBind()
	a := p.Atmosphere
	inv, ok := p.View.Inverse()
	shafts := a.Shafts > 0 && ok && p.Projection[0] != 0
	sky := a.Sky && ok && p.Projection[0] != 0
	u := atmosUniforms{InvView: inv, LightVP: p.lightVP}
	u.Cam = core.V4(inv[12], inv[13], inv[14], 1)
	sd := a.SunDir.Normalize().Scale(-1)
	hasShadow := float32(0)
	if p.shadowView != nil {
		hasShadow = 1
	}
	u.SunDir = core.V4(sd.X, sd.Y, sd.Z, hasShadow)
	density := float32(0)
	if shafts {
		density = a.Shafts
	}
	u.Sun = core.V4(a.Sun[0], a.Sun[1], a.Sun[2], density)
	falloff := a.Falloff
	if falloff <= 0 {
		falloff = 6
	}
	u.Amb = core.V4(a.Ambient[0], a.Ambient[1], a.Ambient[2], falloff)
	skyOn := float32(0)
	if sky {
		skyOn = 1
	}
	u.Zenith = core.V4(a.Zenith[0], a.Zenith[1], a.Zenith[2], skyOn)
	maxD := a.MaxDistance
	if maxD <= 0 {
		maxD = 140
	}
	u.Horizon = core.V4(a.Horizon[0], a.Horizon[1], a.Horizon[2], maxD)
	u.Proj = core.V4(p.Projection[0], p.Projection[5], p.Projection[10], p.Projection[14])
	if p.lightBuf != nil && a.Lamps > 0 {
		u.Lamps = core.V4(float32(p.lightCount), a.Lamps, 0, 0)
	}
	if p.reflect && ok && p.Projection[0] != 0 {
		u.Lamps.Z = 1
	}
	u.Rain = core.V4(0, p.time, 0, 0)
	if p.reflectTraced && ok && p.Projection[0] != 0 {
		u.Rain.Z = 1 // the combine reads the traced reflections
	}
	if p.skyView != nil {
		u.RainVP = p.rainVP
	}
	if ok && p.Projection[0] != 0 {
		u.Rain.X = a.Rain
	}
	plVol, bgVol := p.plVol, p.bgVol
	if shafts && p.hazeTraced && p.bgVolTraced != nil {
		plVol, bgVol = p.plVolTraced, p.bgVolTraced
		if p.Settings.Temporal > 0 {
			// The march's jitter changes every frame, for the temporal
			// filter to average: the traced march takes fewer looks.
			p.hazeFrame = p.hazeFrame%64 + 1
			u.Rain.W = float32(p.hazeFrame)
		}
	}
	if f := p.foreignRefl; f != nil {
		b := func(v bool) float32 {
			if v {
				return 1
			}
			return 0
		}
		u.Refl = core.V4(b(f.Level), b(f.Sky != nil), b(f.SkyInScene), max(f.Blur, 0))
		u.Origin = core.V4(f.Origin.X, f.Origin.Y, f.Origin.Z, 0)
	}
	u.Depth.X = max(p.LogDepth, 0)
	if a.NoFarHaze {
		u.Depth.Y = 1
	}
	u.Depth.Z, u.Depth.W = p.taa.jit[0], p.taa.jit[1]
	p.queue.WriteBuffer(p.atmosUni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))

	pass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup, clear gpu.Color) {
		rp := enc.BeginRenderPass(view, clear)
		rp.SetPipeline(pl)
		rp.SetBindGroup(0, bg)
		rp.Draw(3, 1, 0, 0)
		rp.End()
	}
	clear := gpu.Color{A: 1} // no in-scatter, full transmittance
	if shafts {
		pass(p.vol[0].view, plVol, bgVol, clear)
		pass(p.vol[1].view, p.plVolBlurH, p.bgVolBlur[0], clear)
		pass(p.vol[0].view, p.plVolBlurV, p.bgVolBlur[1], clear)
	}
	p.reflectRender(enc) // the mirrors' traced reflections, for the combine
	pass(dst, p.plAtmos, p.bgAtmos, gpu.Color{})
}

func (p *Post) atmosReleaseBinds() {
	for _, g := range []**gpu.BindGroup{&p.bgVol, &p.bgVolTraced, &p.bgVolBlur[0], &p.bgVolBlur[1], &p.bgAtmos} {
		if *g != nil {
			(*g).Release()
			*g = nil
		}
	}
}

func (p *Post) atmosReleaseTargets() {
	p.atmosReleaseBinds()
	p.vol[0].release()
	p.vol[1].release()
	p.lit.release()
}

func (p *Post) atmosRelease() {
	p.atmosReleaseTargets()
	p.noShadow.release()
	p.noSky.release()
	for _, pl := range []**gpu.RenderPipeline{&p.plVol, &p.plVolTraced, &p.plVolBlurH, &p.plVolBlurV, &p.plAtmos} {
		if *pl != nil {
			(*pl).Release()
			*pl = nil
		}
	}
	if p.atmosUni != nil {
		p.atmosUni.Release()
		p.atmosUni = nil
	}
	if p.noLights != nil {
		p.noLights.Release()
		p.noLights = nil
	}
}

// ---- shaders ---------------------------------------------------------------

// postAtmosCommonWGSL: the atmosphere's uniforms, view rays from depth,
// and the sky.
const postAtmosCommonWGSL = wgsl.Hash + wgsl.Depth + wgsl.Light + `
struct AtmU {
    inv_view : mat4x4f,
    light_vp : mat4x4f,
    cam : vec4f,
    sun_dir : vec4f,   // xyz toward the sun, w 1 with a shadow map
    sun : vec4f,       // rgb radiance, w density (0 = no shafts)
    amb : vec4f,       // rgb ambient, w height falloff
    zenith : vec4f,    // rgb, w 1 when the sky is drawn
    horizon : vec4f,   // rgb, w max march distance
    proj : vec4f,      // projection x, y scales; depth terms [10], [14]
    lamps : vec4f,     // x light count, y how strongly lamps light the air, z 1 = trace reflections
    rain : vec4f,      // x how hard it rains, y time, z 1 when reflections were also traced, w the march's noise frame
    rain_vp : mat4x4f, // the sky map's view-projection (zero: none)
    depth : vec4f,     // x log depth scale (0 = perspective), y 1 = no haze past the march, zw the projection's jitter (NDC)
    refl : vec4f,      // a foreign renderer's reflections: x 1 = level mirrors, y 1 = sky map, z 1 = sky in the scene, w blur
    origin : vec4f,    // xyz the eye in a world-fixed frame (the ripples' phase)
};
// The view distance (along -z) of a depth-buffer value.
fn view_dist(d : f32) -> f32 {
    return depth_dist(d, A.depth.x, A.proj.z, A.proj.w); // A.depth.x > 0: logarithmic depth (Post.LogDepth)
}
// The view-space ray through a uv (not normalised: z is -1).
// The scene was drawn with its projection offset by depth.zw (temporal
// anti-aliasing's jitter, in NDC), so the pixel at uv saw the ray through
// uv less that offset, and a view-space point shows at its uv plus it.
fn view_ray(uv : vec2f) -> vec3f {
    let ndc = vec2f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0) - A.depth.zw;
    return vec3f(ndc.x / A.proj.x, ndc.y / A.proj.y, -1.0);
}
fn screen_uv(q : vec3f) -> vec2f {
    let ndc = vec2f(q.x * A.proj.x / -q.z, q.y * A.proj.y / -q.z) + A.depth.zw;
    return ndc * vec2f(0.5, -0.5) + 0.5;
}
fn world_dir(v : vec3f) -> vec3f {
    return normalize(mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz) * v);
}
// Henyey-Greenstein phase function (the probability density: wgsl.Light's
// over 4π).
fn hg(mu : f32, g : f32) -> f32 { return hg_phase(mu, g) / 12.566371; }
// The horizon's colour in a direction: warmer and brighter toward the sun.
fn horizon_color(rd : vec3f) -> vec3f {
    let toward = max(dot(normalize(rd.xz + vec2f(1e-5)), normalize(A.sun_dir.xz + vec2f(1e-5))), 0.0);
    let warm = A.horizon.rgb + A.sun.rgb * 0.10 * pow(toward, 6.0) * smoothstep(-0.1, 0.3, A.sun_dir.y + 0.1);
    return mix(A.horizon.rgb, warm, 0.85);
}
// The sky in a direction: the gradient, the sun's glow and its disc.
fn sky_color(rd : vec3f) -> vec3f {
    let up = max(rd.y, 0.0);
    var c = mix(horizon_color(rd), A.zenith.rgb, pow(up, 0.42));
    // Below the horizon: the haze over the ground, a little darker.
    c = mix(c, horizon_color(rd) * 0.55, smoothstep(0.0, -0.25, rd.y));
    let mu = dot(rd, A.sun_dir.xyz);
    // Forward-scattered glow round the sun, then the disc itself.
    c += A.sun.rgb * (hg(mu, 0.80) * 0.55 + hg(mu, 0.965) * 0.035);
    c += A.sun.rgb * 60.0 * smoothstep(0.99975, 0.99991, mu);
    return c;
}
`

// postVolSource is the light shafts' shader: by the shadow maps, or (traced,
// after traceWGSL) by rays through the trace scene.
func postVolSource(traced bool) string {
	sun, lamp := postVolSunMap, postVolLampMap
	if traced {
		sun, lamp = postVolSunTraced, postVolLampTraced
	}
	src := strings.Replace(strings.Replace(postVolWGSL, "/*SUN*/", sun, 1), "/*LAMP*/", lamp, 1)
	step := "(i & 1) == 0"
	if traced {
		step = "(i + fr) % VOL_LAMP_EVERY == 0"
	}
	return strings.Replace(src, "/*LAMPSTEP*/", step, 1)
}

// What a point of air asks about the sun and about a lamp, in the two ways.
// The traced march asks the sun on every other step and holds the answer
// between (as the lamps are read): the blur after the pass, and the
// temporal filter where it is on, hide the difference.
const (
	postVolSunMap    = `if (A.sun_dir.w > 0.5) { lit = sun_vis(p); }`
	postVolSunTraced = `if ((i + fr) % VOL_SUN_EVERY == 0 && A.sun.r + A.sun.g + A.sun.b > 0.001) {
            held = select(1.0, 0.0, trace_any(p, A.sun_dir.xyz, 0.02, 200.0));
        }
        lit = held;`
	postVolLampMap = `if (att > 0.0 && li.params.y > 0.5) {
            att = att * spot_vis(i32(li.params.y) - 1, p); // what stands under the lamp cuts its beam
        }`
	postVolLampTraced = `if (att > 0.002 && trace_any(p, dv / dist, 0.02, dist - 0.3)) {
            att = 0.0; // something stands between this air and the lamp
        }`
)

// postVolWGSL: the light shafts, at half resolution. Output: rgb the light
// scattered toward the eye, a what is left of the light behind it.
const postVolWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var shadow : texture_depth_2d;
@group(0) @binding(2) var<uniform> A : AtmU;
@group(0) @binding(3) var<storage, read> lights : Lights;
@group(0) @binding(4) var spot0 : texture_depth_2d;
@group(0) @binding(5) var spot1 : texture_depth_2d;
@group(0) @binding(6) var spot2 : texture_depth_2d;
@group(0) @binding(7) var spot3 : texture_depth_2d;
` + postAtmosCommonWGSL + `
// The Renderer3D's light list (matshader.go's layout).
struct Light {
    pos_range : vec4f,  // xyz position, w range
    color : vec4f,      // rgb colour * energy, w kind (0 point, 1 spot)
    dir_cos : vec4f,    // xyz cone axis, w cos(outer half angle)
    params : vec4f,     // x cos(inner half angle), y shadow slot + 1
}
struct Lights { items : array<Light, 16>, spot_vp : array<mat4x4f, 4>, rain_vp : mat4x4f }
// Whether a shadow-casting spot's light reaches a point of air: one read
// of its shadow map (the march's own jitter softens the edge).
fn spot_vis(slot : i32, p : vec3f) -> f32 {
    let sc = lights.spot_vp[slot] * vec4f(p, 1.0);
    if (sc.w <= 0.0) { return 1.0; }
    let q = sc.xyz / sc.w;
    let uv = vec2f(q.x * 0.5 + 0.5, -q.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) { return 1.0; }
    var d = 1.0;
    switch slot {
        case 0 { let n = vec2f(textureDimensions(spot0)); d = textureLoad(spot0, min(vec2i(uv * n), vec2i(n) - 1), 0); }
        case 1 { let n = vec2f(textureDimensions(spot1)); d = textureLoad(spot1, min(vec2i(uv * n), vec2i(n) - 1), 0); }
        case 2 { let n = vec2f(textureDimensions(spot2)); d = textureLoad(spot2, min(vec2i(uv * n), vec2i(n) - 1), 0); }
        default { let n = vec2f(textureDimensions(spot3)); d = textureLoad(spot3, min(vec2i(uv * n), vec2i(n) - 1), 0); }
    }
    return step(q.z - 0.00003, d);
}
const VOL_STEPS = 20;
const VOL_SUN_EVERY = 5;  // the traced march asks the sun on one step in this many
const VOL_LAMP_EVERY = 4; // and the lamps on one in this many
// The lamps' light reaching a point of air: the surface shader's falloff
// and cone, scattered evenly in all directions.
fn lamp_light(p : vec3f) -> vec3f {
    var sum = vec3f(0.0);
    let count = u32(A.lamps.x);
    for (var i = 0u; i < count; i = i + 1u) {
        let li = lights.items[i];
        let dv = li.pos_range.xyz - p;
        let dist = length(dv);
        if (dist >= li.pos_range.w) { continue; }
        // Outside a spot's cone first: the cheapest way out.
        let cd = dot(-dv / max(dist, 1e-4), li.dir_cos.xyz);
        if (li.color.w > 0.5 && cd <= li.dir_cos.w) { continue; }
        let nd = dist / li.pos_range.w;
        var win = max(1.0 - nd * nd * nd * nd, 0.0);
        win = win * win;
        var att = win / (1.0 + dist * dist);
        if (li.color.w > 0.5) {
            att = att * smoothstep(li.dir_cos.w, li.params.x, cd);
        }
        /*LAMP*/
        sum += li.color.rgb * att;
    }
    return sum * (A.lamps.y / 12.566371);
}
// The air's density relative to the ground's, thinning with height.
fn shape(h : f32) -> f32 { return 0.28 + 0.72 * exp(-max(h, 0.0) / A.amb.w); }
// Whether a point of air is in sunlight.
fn sun_vis(p : vec3f) -> f32 {
    let sc = A.light_vp * vec4f(p, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || sc.z < 0.0 || sc.z > 1.0) { return 1.0; }
    let dims = vec2f(textureDimensions(shadow));
    let d = textureLoad(shadow, vec2i(min(uv * dims, dims - 1.0)), 0);
    return step(sc.z - 0.0008, d);
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(depth));
    let c0 = min(vec2i(in.pos.xy) * 2, vec2i(dims) - 1);
    let uv = (vec2f(c0) + 0.5) / dims;
    let d = textureLoad(depth, c0, 0);
    let is_sky = d >= 0.99999;
    let vr = view_ray(uv);
    let rd = world_dir(vr);
    let max_d = A.horizon.w;
    var dist = max_d * 0.6;                      // the sky: march a way into it
    if (!is_sky) { dist = view_dist(d) * length(vr); }
    let march = min(dist, max_d);
    let sigma = A.sun.w;
    let mu = dot(rd, A.sun_dir.xyz);
    // Two lobes: a tight forward one (the glow looking toward the sun) and
    // a broad one, so shafts seen from the side still read.
    let ph = min(hg(mu, 0.76) * 0.6 + hg(mu, 0.2) * 0.4, 1.2);
    let jitter = fract(ign(in.pos.xy) + A.rain.w * 0.61803399);
    var acc = vec3f(0.0);
    var T = 1.0;
    var t0 = 0.0;
    var lamp = vec3f(0.0);
    var held = 1.0; // the last answer about the sun (the traced march)
    let fr = i32(A.rain.w);
    for (var i = 0; i < VOL_STEPS; i++) {
        let f = (f32(i) + 1.0) / f32(VOL_STEPS);
        let t1 = march * f * (0.3 + 0.7 * f);    // denser steps near the eye
        let ds = t1 - t0;
        let p = A.cam.xyz + rd * (t0 + ds * jitter);
        var lit = 1.0;
        /*SUN*/
        let ext = sigma * shape(p.y);
        let Ti = exp(-ext * ds);
        var src = A.sun.rgb * (ph * lit) + A.amb.rgb * (0.35 + 0.65 * lit);
        // The lamps are the march's dearest term (every light, every step):
        // they are read on every other step and held for the one between,
        // which the blur after this pass hides.
        if (A.lamps.x > 0.5 && /*LAMPSTEP*/) { lamp = lamp_light(p); }
        src += lamp;
        acc += T * src * (1.0 - Ti);
        T *= Ti;
        t0 = t1;
    }
    // Past the march: the haze goes on, fading the far field into the
    // horizon's colour in that direction.
    if (!is_sky && dist > max_d && A.depth.y < 0.5) {
        let h1 = A.cam.y + rd.y * max_d;
        let h2 = A.cam.y + rd.y * dist;
        let tau = sigma * 0.5 * (dist - max_d) * 0.5 * (shape(h1) + shape(h2));
        let Tf = exp(-tau);
        acc += T * (horizon_color(rd) * 0.5 + A.amb.rgb * 0.5) * (1.0 - Tf);
        T *= Tf;
    }
    return vec4f(acc, T);
}
`

// postVolBlurWGSL: the depth-aware separable blur, over all four channels.
const postVolBlurWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var<uniform> A : AtmU;
` + postAtmosCommonWGSL + `
fn blur(frag : vec2f, dir : vec2i) -> vec4f {
    let dmax = vec2i(textureDimensions(depth)) - 1;
    let smax = vec2i(textureDimensions(src)) - 1;
    let p = vec2i(frag);
    let z0 = min(view_dist(textureLoad(depth, min(p * 2, dmax), 0)), 400.0);
    var acc = vec4f(0.0); var ws = 0.0;
    for (var i = -3; i <= 3; i++) {
        let q = clamp(p + dir * i, vec2i(0), smax);
        let z = min(view_dist(textureLoad(depth, min(q * 2, dmax), 0)), 400.0);
        let w = (1.0 - abs(f32(i)) * 0.2) * max(0.0, 1.0 - abs(z - z0) / (0.08 * z0 + 0.05));
        acc += textureLoad(src, q, 0) * w; ws += w;
    }
    if (ws > 1e-4) { return acc / ws; }
    return textureLoad(src, min(p, smax), 0);
}
@fragment fn fs_h(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(1, 0)); }
@fragment fn fs_v(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(0, 1)); }
`

// postAtmosWGSL: the combine. The half-resolution shafts are upsampled
// with depth as the guide (so haze does not bleed across silhouettes),
// then scene × transmittance + in-scatter; empty pixels take the sky.
const postAtmosWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var scene : texture_2d<f32>;
@group(0) @binding(2) var vol : texture_2d<f32>;
@group(0) @binding(3) var<uniform> A : AtmU;
@group(0) @binding(4) var sky_map : texture_depth_2d;
@group(0) @binding(5) var refl : texture_2d<f32>;
@group(0) @binding(6) var refl_sky_map : texture_2d<f32>;
` + postAtmosCommonWGSL + `
// Whether the air at a world point is open to the sky (1) or has a roof
// over it (0), by the renderer's sky map; open where there is no map.
fn rain_open(w : vec3f) -> f32 {
    if (A.rain_vp[3][3] == 0.0) {
        return 1.0;
    }
    let sc = A.rain_vp * vec4f(w, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) {
        return 1.0;
    }
    let dims = vec2f(textureDimensions(sky_map));
    let top = textureLoad(sky_map, min(vec2i(uv * dims), vec2i(dims) - 1), 0);
    return step(sc.z - 0.0006, top);
}
// One layer of rain: a grid of tall cells, each with a streak at its own
// place, length and phase, falling at speed (screens per second); cells is
// how many columns span the screen's height, so nearer layers are coarser
// and faster.
fn rain_layer(uv : vec2f, aspect : f32, turn : vec2f, cells : f32, speed : f32, seed : f32) -> f32 {
    var q = vec2f((uv.x - 0.5) * aspect + uv.y * 0.06 + turn.x * 0.9, uv.y - turn.y * 0.9) * vec2f(cells, cells * 0.11);
    q.y -= A.rain.y * speed * cells * 0.11;
    let cell = floor(q);
    let f = fract(q);
    let h = hash12s(cell + seed * 17.0);
    let x = 0.15 + 0.7 * h;
    let len = 0.35 + 0.5 * fract(h * 17.3);
    let line = smoothstep(0.045, 0.0, abs(f.x - x));
    let seg = smoothstep(0.0, 0.15, f.y) * smoothstep(len, len - 0.2, f.y);
    return line * seg * step(0.3, fract(h * 7.7));
}
// The view-space position of a texel.
fn view_pos(t : vec2i, dims : vec2f) -> vec3f {
    return view_ray((vec2f(t) + 0.5) / dims) * view_dist(textureLoad(depth, t, 0));
}
// The sky the materials put in their mirrors: a foreign renderer's map
// (Reflection.Sky, equirectangular, filtered by hand), else the analytic one.
fn refl_sky(rd : vec3f) -> vec3f {
    if (A.refl.y < 0.5) { return sky_color(rd); }
    let sz = vec2i(textureDimensions(refl_sky_map));
    let uv = vec2f(0.5 + atan2(rd.x, -rd.z) / 6.2831853, acos(clamp(rd.y, -1.0, 1.0)) / 3.14159265);
    let p = uv * vec2f(sz) - 0.5;
    let i = vec2i(floor(p));
    let f = fract(p);
    let x0 = (i.x % sz.x + sz.x) % sz.x;
    let x1 = (x0 + 1) % sz.x;
    let y0 = clamp(i.y, 0, sz.y - 1);
    let y1 = clamp(i.y + 1, 0, sz.y - 1);
    let a = mix(textureLoad(refl_sky_map, vec2i(x0, y0), 0).rgb, textureLoad(refl_sky_map, vec2i(x1, y0), 0).rgb, f.x);
    let b = mix(textureLoad(refl_sky_map, vec2i(x0, y1), 0).rgb, textureLoad(refl_sky_map, vec2i(x1, y1), 0).rgb, f.x);
    return mix(a, b, f.y);
}
// A ray that met nothing, when the renderer drew its sky into the scene
// (Reflection.SkyInScene): the sky where the ray's direction shows on
// screen, if it shows there and nothing stands in front of it.
fn sky_in_scene(r : vec3f, dims : vec2f) -> vec4f {
    if (A.refl.z < 0.5 || r.z > -0.02) { return vec4f(0.0); }
    let uv = vec2f(r.x * A.proj.x / -r.z, r.y * A.proj.y / -r.z) * vec2f(0.5, -0.5) + 0.5;
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) { return vec4f(0.0); }
    let t = clamp(vec2i(uv * dims), vec2i(0), vec2i(dims) - 1);
    if (textureLoad(depth, t, 0) < 0.99999) { return vec4f(0.0); }
    let e = min(min(uv.x, 1.0 - uv.x), min(uv.y, 1.0 - uv.y));
    return vec4f(max(textureLoad(scene, t, 0).rgb, vec3f(0.0)), smoothstep(0.0, 0.08, e));
}
// A rough mirror's reflection (Reflection.Blur): taps down the screen
// round the hit, spread by radius (in uv), skipping the sky.
fn refl_blur(uv : vec2f, dims : vec2f, hit : vec3f, radius : f32) -> vec3f {
    let dmax = vec2i(dims) - 1;
    var acc = hit;
    var ws = 1.0;
    for (var i = 1; i <= 3; i++) {
        let o = vec2f(0.12 * radius, radius) * (f32(i) / 3.0);
        let w = exp(-f32(i * i) * 0.25);
        for (var s = -1.0; s <= 1.0; s += 2.0) {
            let t = clamp(vec2i((uv + o * vec2f(-s, s)) * dims), vec2i(0), dmax);
            if (textureLoad(depth, t, 0) < 0.99999) {
                acc += max(textureLoad(scene, t, 0).rgb, vec3f(0.0)) * w;
                ws += w;
            }
        }
    }
    return acc / ws;
}
// Whether a ray off a level mirror (Reflection.Level) at p, now at q,
// meets the surface at q's pixel (q scaled by k): only if that surface
// stands up into the ray's rise. A ray grazing off water toward the
// horizon otherwise "meets" the far water it passes over (the depth
// buffer is one layer, and a planet's water curves away below the ray).
fn level_hit(p : vec3f, q : vec3f, k : f32) -> bool {
    if (A.refl.x < 0.5) { return true; }
    let up = transpose(mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz)) * vec3f(0.0, 1.0, 0.0);
    return dot(q * k - p, up) > 0.3 * dot(q - p, up);
}
// Screen-space reflection: from a mirror-like pixel, march the reflected
// ray through the depth buffer (strides growing with distance, then a
// bisection to the surface) and return what it meets with a confidence in
// w: zero when it leaves the screen, goes behind something, or comes back
// toward the camera.
struct ReflHit { c : vec4f, sky : vec3f }   // rgb what the ray met and w how surely; the sky that way
fn trace_reflection(c : vec2i, dims : vec2f, ripples : bool, strength : f32) -> ReflHit {
    let dmax = vec2i(dims) - 1;
    let p = view_pos(c, dims);
    // The surface normal from the depth buffer: on each axis, the neighbour
    // whose depth is nearer ours (so an edge does not bend the normal).
    let px0 = view_pos(clamp(c - vec2i(1, 0), vec2i(0), dmax), dims);
    let px1 = view_pos(clamp(c + vec2i(1, 0), vec2i(0), dmax), dims);
    let py0 = view_pos(clamp(c - vec2i(0, 1), vec2i(0), dmax), dims);
    let py1 = view_pos(clamp(c + vec2i(0, 1), vec2i(0), dmax), dims);
    var dx = px1 - p;
    if (abs(px0.z - p.z) < abs(px1.z - p.z)) { dx = p - px0; }
    var dy = py1 - p;
    if (abs(py0.z - p.z) < abs(py1.z - p.z)) { dy = p - py0; }
    var n = normalize(cross(dy, dx));
    let v = normalize(p);
    if (dot(n, v) > 0.0) { n = -n; }
    if (A.refl.x > 0.5) {
        // Level mirrors (Reflection.Level): the world's up.
        n = normalize(transpose(mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz)) * vec3f(0.0, 1.0, 0.0));
    }
    if (ripples) {
        // Moving water: a few trains of waves crossing in the horizontal
        // plane tilt the mirror, less with distance (where they would only
        // shimmer).
        let rot = mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz);
        let w = (A.cam.xyz + A.origin.xyz + rot * p).xz;
        let t = A.rain.y;
        var g = vec2f(0.0);
        g += vec2f(0.8, 0.6) * 0.045 * cos(dot(w, vec2f(0.8, 0.6)) * 1.7 + t * 1.3);
        g += vec2f(-0.5, 0.87) * 0.040 * cos(dot(w, vec2f(-0.5, 0.87)) * 3.9 - t * 1.9);
        g += vec2f(0.3, -0.95) * 0.030 * cos(dot(w, vec2f(0.3, -0.95)) * 8.3 + t * 2.7);
        g += vec2f(-0.9, -0.44) * 0.022 * cos(dot(w, vec2f(-0.9, -0.44)) * 17.0 - t * 3.6);
        g = g / (1.0 + 0.015 * length(p));
        n = normalize(n + transpose(rot) * vec3f(g.x, 0.0, g.y));
    }
    let r = reflect(v, n);
    let sky = refl_sky(world_dir(r)); // what the material mirrored there
    // Off a level mirror only a ripple can send the ray down, into the
    // water behind it: the material's own sky stands for that.
    let lift = select(1.0, smoothstep(0.0, 0.04, world_dir(r).y), A.refl.x > 0.5);
    if (lift <= 0.0) {
        return ReflHit(vec4f(0.0), sky);
    }
    if (r.z > 0.15) {
        return ReflHit(vec4f(0.0), sky); // back toward the camera: nothing on screen to meet
    }
    let dist = -p.z;
    var stride = max(0.08, dist * 0.02) * (0.6 + 0.8 * ign(vec2f(c)));
    var t0 = max(0.05, dist * 0.004);
    var t1 = t0;
    var found = false;
    for (var i = 0; i < 40; i++) {
        t1 = t0 + stride;
        let q = p + r * t1;
        if (q.z > -0.05) { break; }
        let uv = screen_uv(q);
        if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) { break; }
        let dq = textureLoad(depth, min(vec2i(uv * dims), dmax), 0);
        let zs = view_dist(dq);
        let zr = -q.z;
        // Behind the surface there, but not by more than a plausible thickness.
        if (dq < 0.99999 && zr > zs && zr - zs < max(1.5 * stride, 0.3 + 0.05 * zr) && level_hit(p, q, zs / zr)) {
            found = true;
            break;
        }
        t0 = t1;
        stride *= 1.14;
    }
    if (!found) {
        return ReflHit(sky_in_scene(r, dims) * vec4f(1.0, 1.0, 1.0, lift), sky);
    }
    for (var i = 0; i < 5; i++) {
        let tm = 0.5 * (t0 + t1);
        let q = p + r * tm;
        let uv = screen_uv(q);
        let zs = view_dist(textureLoad(depth, clamp(vec2i(uv * dims), vec2i(0), dmax), 0));
        if (-q.z > zs) { t1 = tm; } else { t0 = tm; }
    }
    let q = p + r * t1;
    let uv = screen_uv(q);
    var hit = max(textureLoad(scene, clamp(vec2i(uv * dims), vec2i(0), dmax), 0).rgb, vec3f(0.0));
    if (A.refl.w > 0.0 && strength < 0.98) {
        let k = 1.0 - strength;
        hit = refl_blur(uv, dims, hit, A.refl.w * k * k * t1 * A.proj.y * 0.5 / max(-q.z, 0.1));
    }
    // Fade toward the screen's edges and for rays turning back at the camera.
    let e = min(min(uv.x, 1.0 - uv.x), min(uv.y, 1.0 - uv.y));
    let conf = smoothstep(0.0, 0.08, e) * (1.0 - smoothstep(-0.1, 0.15, r.z)) * lift;
    return ReflHit(vec4f(hit, conf), sky);
}

@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let c = vec2i(in.pos.xy);
    let d = textureLoad(depth, c, 0);
    let sc = textureLoad(scene, c, 0);
    var col = max(sc.rgb, vec3f(0.0));
    let is_sky = d >= 0.99999;
    // Reflections of the scene: the material left its mirror strength in
    // alpha and already added the sky's reflection; where the ray meets the
    // scene, swap the sky for what it meets.
    if (A.lamps.z > 0.5 && !is_sky && abs(sc.a) < 0.995) {
        let hit = trace_reflection(c, vec2f(textureDimensions(depth)), sc.a < 0.0, 1.0 - abs(sc.a));
        var seen = hit.c.rgb * hit.c.w;
        var sure = hit.c.w;
        if (A.rain.z > 0.5) {
            // Where the picture has no answer (off screen, or hidden), the
            // trace scene's: half resolution, the nearest texel.
            let rmax = vec2i(textureDimensions(refl)) - 1;
            let tr = textureLoad(refl, min(c / 2, rmax), 0);
            seen += tr.rgb * tr.a * (1.0 - hit.c.w);
            sure += tr.a * (1.0 - hit.c.w);
        }
        // Take the sky's reflection out (never below nothing: this sky is an
        // estimate of what the material put in) and put what was met in.
        let m = 1.0 - abs(sc.a);
        col = max(col - hit.sky * sure * m, vec3f(0.0)) + seen * m;
    }
    if (is_sky && A.zenith.w > 0.5) {
        let dims = vec2f(textureDimensions(depth));
        col = sky_color(world_dir(view_ray((vec2f(c) + 0.5) / dims)));
    }
    if (A.sun.w > 0.0) {
        // Joint bilateral upsample: the four half-resolution texels round
        // this pixel, weighted by how close their depth is to ours.
        let vmax = vec2i(textureDimensions(vol)) - 1;
        let dmax = vec2i(textureDimensions(depth)) - 1;
        let hp = (vec2f(c) + 0.5) * 0.5 - 0.5;
        let base = vec2i(floor(hp));
        let f = fract(hp);
        let z0 = min(view_dist(d), 400.0);
        var acc = vec4f(0.0); var ws = 0.0;
        for (var k = 0; k < 4; k++) {
            let o = vec2i(k & 1, k >> 1);
            let q = clamp(base + o, vec2i(0), vmax);
            let z = min(view_dist(textureLoad(depth, min(q * 2, dmax), 0)), 400.0);
            var w = select(1.0 - f.x, f.x, o.x == 1) * select(1.0 - f.y, f.y, o.y == 1);
            w *= 1.0 / (0.02 + abs(z - z0) / z0);
            w = max(w, 1e-5);
            acc += textureLoad(vol, q, 0) * w; ws += w;
        }
        let v = acc / ws;
        col = col * v.a + v.rgb;
    }
    if (A.rain.x > 0.0) {
        let dims = vec2f(textureDimensions(depth));
        let uv = (vec2f(c) + 0.5) / dims;
        // The view's heading and tilt slide the layers, so the rain stays in
        // the world as the camera turns instead of sticking to the glass.
        let fwd = -A.inv_view[2].xyz;
        let turn = vec2f(atan2(fwd.x, -fwd.z), -asin(clamp(fwd.y, -1.0, 1.0)));
        let zd = select(view_dist(d), 1e6, is_sky);
        let glow = A.amb.rgb * 1.4 + A.sun.rgb * 0.12 + col * 0.8;
        // Each layer stands at its own distance: hidden behind what is
        // nearer, and absent where the air there has a roof over it.
        let ray = world_dir(view_ray(uv));
        var streaks = 0.0;
        streaks += rain_layer(uv, dims.x / dims.y, turn, 9.0, 1.9, 1.0) * step(2.0, zd) * rain_open(A.cam.xyz + ray * 2.0) * 1.0;
        streaks += rain_layer(uv, dims.x / dims.y, turn, 17.0, 1.5, 2.0) * step(6.0, zd) * rain_open(A.cam.xyz + ray * 6.0) * 0.7;
        streaks += rain_layer(uv, dims.x / dims.y, turn, 31.0, 1.1, 3.0) * step(15.0, zd) * rain_open(A.cam.xyz + ray * 15.0) * 0.45;
        col = col + glow * streaks * A.rain.x * 0.5;
    }
    return vec4f(col, 1.0);
}
`
