package render

// Blend groups. Everything queued between BeginGroup and EndGroup is drawn
// into an offscreen target of its own and composited back as ONE image,
// with a blend mode and a group alpha. That is PDF's transparency group,
// QML's `layer.enabled` with a blend mode, and SVG's `isolation: isolate`
// — and it is also how a single object gets a blend mode, by wrapping it in
// a group of one.
//
// Compositing is the only place a blend mode can live here. WebGPU has no
// programmable blending: a fragment shader cannot read the pixel it is
// about to blend with, so the modes have to be expressed as fixed-function
// blend equations on the composite draw. Multiply and Screen come out
// exact at any alpha; Darken and Lighten are min()/max(), which have no
// notion of coverage, so they are exact where the group is opaque and fade
// toward the backdrop where it is not.
//
// The group's own pass runs before the caller's, on a private command
// buffer — the same reentrancy trick clip masks use (clipmask.go). Nesting
// works because a group closes before its parent does: the inner target is
// already filled when the outer pass replays the composite that samples it.

import "github.com/sushvibez/gfx/gpu"

// BlendMode is how a group's pixels combine with the backdrop under them.
// The four beyond Normal are PDF's separable modes of the same names.
type BlendMode int

const (
	BlendNormal BlendMode = iota
	BlendMultiply
	BlendScreen
	BlendDarken
	BlendLighten
	numBlendModes
)

func (m BlendMode) String() string {
	switch m {
	case BlendMultiply:
		return "Multiply"
	case BlendScreen:
		return "Screen"
	case BlendDarken:
		return "Darken"
	case BlendLighten:
		return "Lighten"
	}
	return "Normal"
}

// blendState is the fixed-function equation for a mode, applied to the
// composite draw. The group's texture is PREMULTIPLIED — it was composited
// over a transparent target — which is what makes these exact:
//
//	Normal    a*Cs + (1-a)*Cb
//	Multiply  a*Cs*Cb + (1-a)*Cb
//	Screen    a*Cs + (1 - a*Cs)*Cb
//	Darken    min(mix(1, Cs, a), Cb)   (the shader fades toward white)
//	Lighten   max(a*Cs, Cb)
func blendStateFor(m BlendMode) *gpu.BlendDesc {
	over := gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrcAlpha}
	keepMax := gpu.BlendComponent{Op: gpu.BlendMax, Src: gpu.BlendOne, Dst: gpu.BlendOne}
	switch m {
	case BlendMultiply:
		return &gpu.BlendDesc{
			Color: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendDst, Dst: gpu.BlendOneMinusSrcAlpha},
			Alpha: over,
		}
	case BlendScreen:
		return &gpu.BlendDesc{
			Color: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrc},
			Alpha: over,
		}
	case BlendDarken:
		return &gpu.BlendDesc{
			Color: gpu.BlendComponent{Op: gpu.BlendMin, Src: gpu.BlendOne, Dst: gpu.BlendOne},
			Alpha: keepMax,
		}
	case BlendLighten:
		return &gpu.BlendDesc{Color: keepMax, Alpha: keepMax}
	}
	return &gpu.BlendDesc{Color: over, Alpha: over}
}

// extractKind is which fragment the copy out of the stage runs: a plain
// copy for a blend group, or coverage for a soft mask.
type extractKind int

const (
	extractCopy extractKind = iota
	extractLuma
	extractAlpha
	numExtractKinds
)

// group is one BeginGroup/EndGroup pair: the batches it captured, where
// they land, and how the result is composited.
type group struct {
	mode    BlendMode
	alpha   float32
	depth   int      // nesting level, which stage target it renders through
	box     clipRect // device-pixel bounds of everything queued inside
	clip    clipRect // the clip in force at BeginGroup: the composite's scissor
	batch   []batch  // the captured draw list, replayed into the target
	tex     int      // index into groupPool
	vert    int      // first vertex of the composite quad in compVerts
	extract int      // first vertex of the copy-out-of-the-stage quad
	// A soft mask draws into a group like any other, but its result is a
	// COVERAGE MASK for what follows rather than something composited:
	// kind says whether the coverage is the drawing's luminosity or its
	// alpha, and backdrop is what a luminosity mask is measured against
	// (PDF's /BC, black unless the file says otherwise).
	isMask   bool
	kind     SoftMaskKind
	backdrop RGBA
	// ndc bounds, accumulated as primitives are queued.
	x0, y0, x1, y1 float32
	empty          bool
}

// groupTarget is a pooled offscreen texture one group's content ends up in,
// sized to that group's bounds.
type groupTarget struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	bind *gpu.BindGroup // group 0 of the composite pipeline for mode
	mask *gpu.BindGroup // group 0 of the mask pipeline, for a soft mask
	mode BlendMode      // which pipeline's layout bind was built for
	w, h int
}

// stageTarget is the full-size scratch a group is rendered through, one per
// nesting depth. Full size because the queued geometry is already in the
// frame's clip space: rendering it anywhere else would mean re-projecting
// every vertex, so instead the content is drawn at frame scale and the
// group's bounds are copied out of it.
type stageTarget struct {
	color, msaa         *gpu.Texture
	colorView, msaaView *gpu.TextureView
	// One bind group per extract pipeline: the layouts come from
	// `layout: "auto"` pipelines, which wgpu makes exclusive to their own.
	extract [numExtractKinds]*gpu.BindGroup
	w, h    int
}

// BeginGroup starts a blend group: the draws that follow go to an offscreen
// target instead of the frame, until EndGroup composites them.
func (r *Renderer2D) BeginGroup() {
	g := group{
		depth: len(r.openGroups),
		clip:  r.clip(),
		empty: true,
		tex:   -1,
	}
	r.groups = append(r.groups, g)
	r.openGroups = append(r.openGroups, len(r.groups)-1)
	// The batches queued from here belong to the group, so the main list
	// is cut at this point and rejoined by EndGroup.
	r.groupCut = append(r.groupCut, len(r.batches))
	r.batchFloor = len(r.batches)
}

// EndGroup composites the open group onto whatever it was drawn over, with
// the given blend mode and group alpha (1 for none). The alpha applies to
// the group as a WHOLE, which is the visible difference from setting it on
// each draw: overlapping shapes inside a half-transparent group do not show
// through each other.
func (r *Renderer2D) EndGroup(mode BlendMode, alpha float32) {
	if len(r.openGroups) == 0 {
		return
	}
	gi := r.openGroups[len(r.openGroups)-1]
	r.openGroups = r.openGroups[:len(r.openGroups)-1]
	cut := r.groupCut[len(r.groupCut)-1]
	r.groupCut = r.groupCut[:len(r.groupCut)-1]

	g := &r.groups[gi]
	g.mode, g.alpha = mode, max(0, min(1, alpha))
	g.batch = append([]batch(nil), r.batches[cut:]...)
	r.batches = r.batches[:cut]

	box, ok := r.groupBox(g)
	r.batchFloor = len(r.batches)
	if !ok || len(g.batch) == 0 {
		// Nothing visible inside: no target, no composite.
		g.batch = nil
		return
	}
	g.box = box
	if g.isMask {
		// A mask is never composited: it is only read as coverage.
		r.resolve = append(r.resolve, resolveRef{group: gi + 1})
		r.batchFloor = len(r.batches)
		return
	}
	g.vert = len(r.compVerts) / floatsPerVertex
	// The composite quad: the group's box, sampling its texture whole.
	x0, y0 := r.devToNDC(float32(box.x), float32(box.y))
	x1, y1 := r.devToNDC(float32(box.x+box.w), float32(box.y+box.h))
	a := g.alpha
	r.compQuad(x0, y0, x1, y1, 0, 0, 1, 1, RGBA{a, a, a, a})
	r.batches = append(r.batches, batch{
		kind: kindComposite, clip: g.clip, mode: mode,
		start: g.vert, count: 6, group: gi + 1,
	})
	// The frame resolves offscreen targets in creation order, and a group
	// registers when it CLOSES — which is what makes a nested group ready
	// before the group that composites it.
	r.resolve = append(r.resolve, resolveRef{group: gi + 1})
	r.batchFloor = len(r.batches)
	// A composite draw is a draw: a group nested in another group has to
	// count toward its parent's bounds.
	r.noteGroupNDC(x0, y0, x1, y1)
}

// devToNDC maps a framebuffer pixel to clip space, without the camera or
// the DPI scale a drawing coordinate goes through: a group's box is already
// in device pixels.
func (r *Renderer2D) devToNDC(x, y float32) (float32, float32) {
	return x/r.width*2 - 1, 1 - y/r.height*2
}

// SoftMaskKind says what a soft mask's drawing contributes as coverage.
type SoftMaskKind int

const (
	// SoftMaskLuminosity takes the mask drawing's brightness: white lets
	// everything through, black nothing. PDF's /S /Luminosity, and what a
	// gradient fade is made of.
	SoftMaskLuminosity SoftMaskKind = iota
	// SoftMaskAlpha takes the mask drawing's alpha instead, ignoring what
	// colour it was drawn in. PDF's /S /Alpha.
	SoftMaskAlpha
)

// BeginSoftMask starts drawing a soft mask: the draws that follow do not
// reach the frame at all, they become the coverage for the drawing AFTER
// EndSoftMask.
func (r *Renderer2D) BeginSoftMask() {
	r.BeginGroup()
	g := &r.groups[r.openGroups[len(r.openGroups)-1]]
	g.isMask = true
}

// EndSoftMask closes the mask and pushes it as a clip: until the matching
// PopClip, every draw is multiplied by the mask's luminosity (or alpha).
// backdrop is what a luminosity mask is measured against outside its own
// drawing — black (fully masked) unless a file says otherwise.
//
// The mask is a clip like any other, so it intersects the clip it is
// pushed inside and nests with path clips both ways.
func (r *Renderer2D) EndSoftMask(kind SoftMaskKind, backdrop RGBA) {
	if len(r.openGroups) == 0 {
		return
	}
	gi := r.openGroups[len(r.openGroups)-1]
	g := &r.groups[gi]
	g.kind, g.backdrop = kind, backdrop
	r.EndGroup(BlendNormal, 1)
	if len(g.batch) == 0 {
		// Nothing in the mask: a luminosity mask over a black backdrop
		// masks everything away, an alpha mask likewise.
		r.clips = append(r.clips, clipRect{})
		return
	}
	// The mask becomes a clip: the group's box is the scissor AND the
	// extent the mask texture covers, narrowed by whatever clip was
	// already in force (the group's own bounds are already inside it).
	box := g.box
	top := r.clip()
	parent := 0
	if top.w >= 0 {
		x0, y0 := max(box.x, top.x), max(box.y, top.y)
		x1, y1 := min(box.x+box.w, top.x+top.w), min(box.y+box.h, top.y+top.h)
		box = clipRect{x: x0, y: y0, w: max(0, x1-x0), h: max(0, y1-y0)}
		parent = top.maskID
	}
	if box.w <= 0 || box.h <= 0 {
		r.clips = append(r.clips, clipRect{})
		return
	}
	r.clipMasks = append(r.clipMasks, clipMask{box: g.box, group: gi + 1, parent: parent})
	box.maskID = len(r.clipMasks)
	box.mx, box.my, box.mw, box.mh = g.box.x, g.box.y, g.box.w, g.box.h
	r.clips = append(r.clips, box)
}

// groupBox turns a group's accumulated clip-space bounds into a device
// rectangle, clamped to the frame and to the clip it was drawn under.
func (r *Renderer2D) groupBox(g *group) (clipRect, bool) {
	if g.empty {
		return clipRect{}, false
	}
	toDev := func(nx, ny float32) (float32, float32) {
		return (nx + 1) * 0.5 * r.width, (1 - ny) * 0.5 * r.height
	}
	ax, ay := toDev(g.x0, g.y1)
	bx, by := toDev(g.x1, g.y0)
	box := clipRect{
		x: int(ax) - 1, y: int(ay) - 1,
		w: int(bx-ax) + 3, h: int(by-ay) + 3,
	}
	// Everything outside the frame or the active clip is invisible, and a
	// target only has to cover what is left.
	lim := clipRect{w: int(r.width), h: int(r.height)}
	if c := g.clip; c.w >= 0 {
		x0, y0 := max(lim.x, c.x), max(lim.y, c.y)
		x1, y1 := min(lim.w, c.x+c.w), min(lim.h, c.y+c.h)
		lim = clipRect{x: x0, y: y0, w: x1 - x0, h: y1 - y0}
	}
	x0, y0 := max(box.x, lim.x), max(box.y, lim.y)
	x1, y1 := min(box.x+box.w, lim.x+lim.w), min(box.y+box.h, lim.y+lim.h)
	if x1 <= x0 || y1 <= y0 {
		return clipRect{}, false
	}
	return clipRect{x: x0, y: y0, w: x1 - x0, h: y1 - y0}, true
}

// noteGroupNDC grows the open group's bounds by a primitive's clip-space
// extent. Called from the three funnels every draw goes through (quads,
// shadows, sprite vertices), so a group's target is only as big as what it
// actually contains.
func (r *Renderer2D) noteGroupNDC(x0, y0, x1, y1 float32) {
	if len(r.openGroups) == 0 {
		return
	}
	g := &r.groups[r.openGroups[len(r.openGroups)-1]]
	lo, hi := min(x0, x1), max(x0, x1)
	blo, bhi := min(y0, y1), max(y0, y1)
	if g.empty {
		g.x0, g.x1, g.y0, g.y1 = lo, hi, blo, bhi
		g.empty = false
		return
	}
	g.x0, g.x1 = min(g.x0, lo), max(g.x1, hi)
	g.y0, g.y1 = min(g.y0, blo), max(g.y1, bhi)
}

// compQuad appends one composite/extract quad (two triangles) to the
// composite vertex buffer.
func (r *Renderer2D) compQuad(x0, y0, x1, y1, u0, v0, u1, v1 float32, c RGBA) {
	push := func(x, y, u, v float32) {
		r.compVerts = append(r.compVerts, x, y, u, v, c.R, c.G, c.B, c.A)
	}
	push(x0, y0, u0, v0)
	push(x1, y0, u1, v0)
	push(x1, y1, u1, v1)
	push(x0, y0, u0, v0)
	push(x1, y1, u1, v1)
	push(x0, y1, u0, v1)
}

// allocGroupTargets makes a target for every group this frame needs and
// uploads the quads its passes draw. Runs before the clip uniforms are
// written, since a soft mask's clip points at its group's texture.
func (r *Renderer2D) allocGroupTargets() {
	live := false
	for i := range r.groups {
		if len(r.groups[i].batch) > 0 {
			live = true
			break
		}
	}
	if !live {
		return
	}
	if err := r.ensureCompositePipelines(); err != nil {
		// No composite pipelines: drop the groups rather than the frame.
		// Their content is lost, which is visible, but a failed pipeline
		// build means the device is in trouble anyway.
		for i := range r.groups {
			r.groups[i].batch = nil
		}
		return
	}
	// The extract quads (one per group, copying its box out of the stage)
	// go into the same buffer, so they are appended BEFORE it is written.
	for i := range r.groups {
		g := &r.groups[i]
		if len(g.batch) == 0 {
			continue
		}
		g.tex = r.ensureGroupTarget(i, g.box.w, g.box.h, g.mode)
		if r.groupPool[g.tex].view == nil {
			g.batch = nil
			continue
		}
		g.extract = len(r.compVerts) / floatsPerVertex
		u0 := float32(g.box.x) / r.width
		v0 := float32(g.box.y) / r.height
		u1 := float32(g.box.x+g.box.w) / r.width
		v1 := float32(g.box.y+g.box.h) / r.height
		// A luminosity mask is measured against its backdrop, which rides
		// along in the extract quad's colour; everything else copies as it
		// is.
		tint := White
		if g.isMask && g.kind == SoftMaskLuminosity {
			tint = g.backdrop
		}
		r.compQuad(-1, 1, 1, -1, u0, v0, u1, v1, tint)
	}
	if data := floatBytes(r.compVerts); len(data) > 0 {
		if r.compVertBuf == nil || r.compVertBuf.Size() < len(data) {
			if r.compVertBuf != nil {
				r.compVertBuf.Release()
			}
			r.compVertBuf = r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, max(len(data), 4096))
		}
		r.queue.WriteBuffer(r.compVertBuf, 0, data)
	}
}

// drawGroup renders one group's content and copies its box out of the
// stage into the group's own target. Reports whether it queued any work.
func (r *Renderer2D) drawGroup(enc *gpu.CommandEncoder, gi int) bool {
	if gi >= len(r.groups) {
		return false
	}
	g := &r.groups[gi]
	if len(g.batch) == 0 || r.extractPipes[extractCopy] == nil {
		return false
	}
	st := r.ensureStage(g.depth)
	if st == nil {
		g.batch = nil
		return false
	}
	gt := r.groupPool[g.tex]
	if gt.view == nil {
		g.batch = nil
		return false
	}
	// The content, at frame scale, into the shared stage.
	var pass *gpu.RenderPass
	if st.msaaView != nil {
		pass = enc.BeginRenderPassResolve(st.msaaView, st.colorView, gpu.Color{})
	} else {
		pass = enc.BeginRenderPass(st.colorView, gpu.Color{})
	}
	r.drawBatches(pass, g.batch)
	pass.End()
	// Then the group's own box out of it, 1:1 and unblended — turned into
	// coverage on the way if the group is a soft mask.
	kind := extractCopy
	if g.isMask {
		kind = extractLuma
		if g.kind == SoftMaskAlpha {
			kind = extractAlpha
		}
	}
	pass = enc.BeginRenderPass(gt.view, gpu.Color{})
	pass.SetPipeline(r.extractPipes[kind])
	pass.SetBindGroup(0, st.bind(r, kind))
	pass.SetVertexBuffer(0, r.compVertBuf)
	pass.Draw(6, 1, g.extract, 0)
	pass.End()
	return true
}

// groupView is a group's target texture (nil if it has none).
func (r *Renderer2D) groupView(gi int) *gpu.TextureView {
	if gi < 0 || gi >= len(r.groups) {
		return nil
	}
	if t := r.groups[gi].tex; t >= 0 && t < len(r.groupPool) {
		return r.groupPool[t].view
	}
	return nil
}

// groupMaskBind is group 0 of the MASK pipeline over a group's target —
// what a path clip nested inside a soft mask is intersected with.
func (r *Renderer2D) groupMaskBind(gi int) *gpu.BindGroup {
	if gi < 0 || gi >= len(r.groups) || r.maskLayout0 == nil {
		return nil
	}
	t := r.groups[gi].tex
	if t < 0 || t >= len(r.groupPool) {
		return nil
	}
	gt := &r.groupPool[t]
	if gt.view == nil {
		return nil
	}
	if gt.mask == nil {
		gt.mask = r.device.CreateBindGroup(r.maskLayout0,
			gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
			gpu.BindGroupEntry{Binding: 1, TextureView: gt.view},
		)
	}
	return gt.mask
}

// bind is the stage's group 0 for one extract pipeline, built on first use.
func (s *stageTarget) bind(r *Renderer2D, kind extractKind) *gpu.BindGroup {
	if s.extract[kind] == nil {
		s.extract[kind] = r.device.CreateBindGroup(r.extractLayouts[kind],
			gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
			gpu.BindGroupEntry{Binding: 1, TextureView: s.colorView},
		)
	}
	return s.extract[kind]
}

// ensureStage returns the full-size scratch for a nesting depth.
func (r *Renderer2D) ensureStage(depth int) *stageTarget {
	w, h := max(int(r.width), 1), max(int(r.height), 1)
	for len(r.groupStage) <= depth {
		r.groupStage = append(r.groupStage, stageTarget{})
	}
	st := &r.groupStage[depth]
	if st.color != nil && st.w == w && st.h == h {
		return st
	}
	st.release()
	tex := r.device.CreateTexture2D(w, h, r.format,
		gpu.TextureRenderAttachment|gpu.TextureBinding)
	if tex == nil {
		return nil
	}
	*st = stageTarget{color: tex, colorView: tex.CreateView(), w: w, h: h}
	if r.samples > 1 {
		st.msaa = r.device.CreateMSAATexture(w, h, r.format, r.samples)
		st.msaaView = st.msaa.CreateView()
	}
	return st
}

// ensureGroupTarget returns a pooled target of the group's size, bound for
// the pipeline that will composite it.
//
// The bind group has to be built from THAT pipeline's layout: wgpu marks a
// layout derived from `layout: "auto"` as exclusive to its pipeline, so one
// bind group cannot be shared across the blend modes even though the five
// pipelines declare an identical group 0.
func (r *Renderer2D) ensureGroupTarget(i, w, h int, mode BlendMode) int {
	w, h = max(w, 1), max(h, 1)
	for len(r.groupPool) <= i {
		r.groupPool = append(r.groupPool, groupTarget{})
	}
	gt := &r.groupPool[i]
	if gt.tex != nil && gt.w == w && gt.h == h && gt.mode == mode {
		return i
	}
	if gt.tex != nil && gt.w == w && gt.h == h {
		// Same texture, different mode: only the bind group changes.
		gt.bind.Release()
		gt.mode = mode
		gt.bind = r.compBind(mode, gt.view)
		return i
	}
	gt.release()
	tex := r.device.CreateTexture2D(w, h, r.format,
		gpu.TextureRenderAttachment|gpu.TextureBinding)
	if tex == nil {
		return i
	}
	view := tex.CreateView()
	*gt = groupTarget{tex: tex, view: view, w: w, h: h, mode: mode,
		bind: r.compBind(mode, view)}
	return i
}

// compBind is group 0 (sampler + texture) for one blend mode's pipeline.
func (r *Renderer2D) compBind(mode BlendMode, view *gpu.TextureView) *gpu.BindGroup {
	return r.device.CreateBindGroup(r.compLayouts[mode],
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
}

// compositeBind is the texture a composite batch samples.
func (r *Renderer2D) compositeBind(groupID int) *gpu.BindGroup {
	if groupID <= 0 || groupID > len(r.groups) {
		return nil
	}
	g := r.groups[groupID-1]
	if g.tex < 0 || g.tex >= len(r.groupPool) {
		return nil
	}
	return r.groupPool[g.tex].bind
}

// ensureCompositePipelines builds the composite family on first use: one
// pipeline per blend mode at the frame's format and sample count, plus the
// unblended single-sample copy the extract step uses.
func (r *Renderer2D) ensureCompositePipelines() error {
	if r.extractPipes[extractCopy] != nil {
		return nil
	}
	sm, err := r.device.CreateShaderModuleWGSL("composite2d", compositeShader)
	if err != nil {
		return err
	}
	defer sm.Release()
	layout := []gpu.VertexLayout{{
		Stride: floatsPerVertex * 4,
		Attrs: []gpu.VertexAttr{
			{Format: gpu.VertexFloat32x2, Offset: 0, Location: 0},
			{Format: gpu.VertexFloat32x2, Offset: 8, Location: 1},
			{Format: gpu.VertexFloat32x4, Offset: 16, Location: 2},
		},
	}}
	// The copies out of the stage: no blending at all, so the pixels land
	// exactly as the fragment computed them. One per kind, since a soft
	// mask turns its drawing into coverage on the way out.
	var extract [numExtractKinds]*gpu.RenderPipeline
	for k, entry := range [numExtractKinds]string{"fs", "fs_luminosity", "fs_alpha"} {
		pipe, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: sm, VertexEntry: "vs", FragmentEntry: entry,
			Format: r.format, Samples: 1, VertexLayouts: layout,
		})
		if err != nil {
			for _, p := range extract {
				if p != nil {
					p.Release()
				}
			}
			return err
		}
		extract[k] = pipe
	}
	for m := BlendNormal; m < numBlendModes; m++ {
		entry := "fs"
		if m == BlendDarken {
			entry = "fs_toward_white"
		}
		pipe, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: sm, VertexEntry: "vs", FragmentEntry: entry,
			Format: r.format, Samples: r.samples, VertexLayouts: layout,
			Blend: blendStateFor(m),
		})
		if err != nil {
			for _, p := range extract {
				p.Release()
			}
			for i := range r.compPipes {
				if r.compPipes[i] != nil {
					r.compPipes[i].Release()
					r.compPipes[i] = nil
				}
			}
			return err
		}
		r.compPipes[m] = pipe
		r.compLayouts[m] = pipe.BindGroupLayout(0)
	}
	for k, p := range extract {
		r.extractPipes[k] = p
		r.extractLayouts[k] = p.BindGroupLayout(0)
	}
	return nil
}

func (g *groupTarget) release() {
	if g.bind != nil {
		g.bind.Release()
	}
	if g.mask != nil {
		g.mask.Release()
	}
	if g.view != nil {
		g.view.Release()
	}
	if g.tex != nil {
		g.tex.Release()
	}
	*g = groupTarget{}
}

func (s *stageTarget) release() {
	for i := range s.extract {
		if s.extract[i] != nil {
			s.extract[i].Release()
			s.extract[i] = nil
		}
	}
	if s.colorView != nil {
		s.colorView.Release()
	}
	if s.color != nil {
		s.color.Release()
	}
	if s.msaaView != nil {
		s.msaaView.Release()
	}
	if s.msaa != nil {
		s.msaa.Release()
	}
	*s = stageTarget{}
}

// releaseGroups frees the group pools and the composite pipelines.
func (r *Renderer2D) releaseGroups() {
	for i := range r.groupPool {
		r.groupPool[i].release()
	}
	r.groupPool = nil
	for i := range r.groupStage {
		r.groupStage[i].release()
	}
	r.groupStage = nil
	for i := range r.compPipes {
		if r.compPipes[i] != nil {
			r.compPipes[i].Release()
			r.compPipes[i] = nil
		}
		if r.compLayouts[i] != nil {
			r.compLayouts[i].Release()
			r.compLayouts[i] = nil
		}
	}
	for i := range r.extractPipes {
		if r.extractPipes[i] != nil {
			r.extractPipes[i].Release()
			r.extractPipes[i] = nil
		}
		if r.extractLayouts[i] != nil {
			r.extractLayouts[i].Release()
			r.extractLayouts[i] = nil
		}
	}
	if r.compVertBuf != nil {
		r.compVertBuf.Release()
		r.compVertBuf = nil
	}
}

// compositeShader draws one group's texture over the backdrop. The blend
// equation does the mode; the fragment only has to hand the pipeline the
// right kind of colour.
const compositeShader = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var tex : texture_2d<f32>;

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

// A group's pixels are premultiplied — they were composited over a
// transparent target — which is what the blend equations expect. The vertex
// colour carries the group alpha, and scaling a premultiplied colour by it
// keeps it premultiplied.
@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    return textureSample(tex, samp, in.uv) * in.color;
}

// Darken is a min() in fixed-function blending, and min() knows nothing
// about coverage: an uncovered pixel would win as black. Fading toward
// WHITE instead makes uncovered pixels the identity of min — and for a
// premultiplied colour, rgb + (1 - a) IS mix(white, colour, a).
@fragment
fn fs_toward_white(in : VSOut) -> @location(0) vec4f {
    let c = textureSample(tex, samp, in.uv) * in.color;
    return vec4f(c.rgb + vec3f(1.0 - c.a), c.a);
}

// A luminosity soft mask: the mask drawing's brightness IS the coverage.
// The group's pixels are premultiplied, so compositing them over the
// backdrop (which rides in the vertex colour) is one mix, and the weights
// are the Rec.709 luma the PDF spec names for DeviceRGB.
@fragment
fn fs_luminosity(in : VSOut) -> @location(0) vec4f {
    let c = textureSample(tex, samp, in.uv);
    let over = c.rgb + in.color.rgb * (1.0 - c.a);
    let l = dot(over, vec3f(0.2126, 0.7152, 0.0722));
    return vec4f(l, l, l, 1.0);
}

// An alpha soft mask: the coverage is what the mask drawing covered,
// whatever colour it was drawn in.
@fragment
fn fs_alpha(in : VSOut) -> @location(0) vec4f {
    let a = textureSample(tex, samp, in.uv).a;
    return vec4f(a, a, a, 1.0);
}
`
