package render

// Path clips. A clip that is not a box (PDF's `W n` around vector art, an
// SVG clipPath, a QML shape used as a mask) becomes a COVERAGE MASK: the
// path is filled into a small offscreen texture the size of its own
// bounding box, and every pipeline multiplies its alpha by that texture
// through the clip's group(1) binding.
//
// The mask is rendered in its own pass, submitted from inside Flush before
// the caller's pass runs — the same trick Layer2D uses, so no caller has
// to learn about a new step. It uses its own vertex buffer and pipeline,
// which is what makes that reentrancy safe: the main queues are untouched.
//
// Masks are supersampled 2x and sampled linearly, since a mask target is
// single-sample: without it a clipped edge would be a hard staircase while
// everything else the renderer draws is antialiased.

import (
	"github.com/sushvibez/gfx/gpu"
)

// maskScale is how much finer than the target a mask is rendered.
const maskScale = 2

// clipMask is one clip's coverage mask: where it lands on screen, what
// fills it, and the mask it is nested inside.
//
// A PATH clip fills triangles into a mask target of its own. A SOFT mask
// (PDF's /SMask: a whole drawing whose luminosity or alpha is the coverage)
// is a blend group instead — group is 1-based into groups, and the group's
// own target is the mask.
type clipMask struct {
	box    clipRect // device pixels; also the mask's own extent
	start  int      // first vertex in clipVerts
	count  int
	parent int // 1-based id of the enclosing mask, 0 for none
	target int // index into maskPool
	group  int // 1-based into groups: a soft mask, drawn as a group
}

// maskTarget is a pooled offscreen texture masks are rendered into.
type maskTarget struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	bind *gpu.BindGroup // group 0 of the mask pipeline: sampler + this texture
	w, h int
}

// PushClipPath intersects an arbitrary path with the current clip. The
// path is in device pixels (the same space the drawing calls use), and its
// fill rule decides what is inside.
//
// Until the matching PopClip, every draw is multiplied by the path's
// coverage. Nested path clips intersect: a mask is rendered through the
// mask it sits inside.
func (r *Renderer2D) PushClipPath(p *Path, rule FillRule) {
	// An open subpath counts, as it does for a fill: a caller that built
	// the path and never closed it (a canvas doing beginPath/ellipse/fill)
	// would otherwise hand us NO subpaths, and an empty clip hides
	// everything.
	p.flushSub()
	tris := triangulateRule(p.subs, rule, nil)
	if len(tris) < 3 {
		// An empty path clips everything away, which is what the spec says
		// a degenerate clip does.
		r.clips = append(r.clips, clipRect{})
		return
	}
	// The clip box: the path's bounds, narrowed by whatever clip is active.
	s := r.sc()
	minX, minY := tris[0].X, tris[0].Y
	maxX, maxY := minX, minY
	for _, pt := range tris[1:] {
		minX, minY = min(minX, pt.X), min(minY, pt.Y)
		maxX, maxY = max(maxX, pt.X), max(maxY, pt.Y)
	}
	box := clipRect{
		x: int(minX * s), y: int(minY * s),
		w: int((maxX-minX)*s) + 1, h: int((maxY-minY)*s) + 1,
	}
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
	// Record the mask and its geometry, mapped into the mask's own clip
	// space (and into the parent mask's uv, so the two intersect).
	id := len(r.clipMasks) + 1
	m := clipMask{box: box, start: len(r.clipVerts) / floatsPerVertex, count: len(tris), parent: parent}
	var pbox clipRect
	if parent > 0 {
		pbox = r.clipMasks[parent-1].box
	}
	fx, fy := float32(box.x), float32(box.y)
	fw, fh := float32(box.w), float32(box.h)
	for _, pt := range tris {
		dx, dy := pt.X*s, pt.Y*s // device pixels
		// The mask's own clip space: the box maps to -1..1, y down.
		nx := (dx-fx)/fw*2 - 1
		ny := 1 - (dy-fy)/fh*2
		// The parent mask's uv, or (0,0) with the white texture bound.
		var u, v float32
		if parent > 0 {
			u = (dx - float32(pbox.x)) / float32(pbox.w)
			v = (dy - float32(pbox.y)) / float32(pbox.h)
		}
		r.clipVerts = append(r.clipVerts, nx, ny, u, v, 1, 1, 1, 1)
	}
	r.clipMasks = append(r.clipMasks, m)
	r.resolve = append(r.resolve, resolveRef{mask: id})
	box.maskID = id
	box.mx, box.my, box.mw, box.mh = box.x, box.y, box.w, box.h
	r.clips = append(r.clips, box)
}

// allocMaskTargets makes a target for every path mask this frame needs and
// uploads the triangles that fill them. Runs before the clip uniforms are
// written, since a clip's bind group points at its mask texture.
func (r *Renderer2D) allocMaskTargets() {
	paths := false
	for i := range r.clipMasks {
		if r.clipMasks[i].group == 0 {
			paths = true
			break
		}
	}
	if !paths {
		return
	}
	if err := r.ensureMaskPipeline(); err != nil {
		// No mask pipeline: the clip degrades to its bounding box, which
		// is what a rectangular clip already was.
		for i := range r.clipMasks {
			r.clipMasks[i].count = 0
		}
		r.clipVerts = r.clipVerts[:0]
		return
	}
	if data := floatBytes(r.clipVerts); len(data) > 0 {
		if r.clipVertBuf == nil || r.clipVertBuf.Size() < len(data) {
			if r.clipVertBuf != nil {
				r.clipVertBuf.Release()
			}
			r.clipVertBuf = r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, max(len(data), 4096))
		}
		r.queue.WriteBuffer(r.clipVertBuf, 0, data)
	}
	for i := range r.clipMasks {
		m := &r.clipMasks[i]
		if m.group > 0 || m.count == 0 {
			continue // a soft mask: its group allocates the target
		}
		m.target = r.ensureMaskTarget(i, m.box.w*maskScale, m.box.h*maskScale)
	}
}

// drawClipMask fills one path mask, through the mask it is nested inside.
// Reports whether it queued any work.
func (r *Renderer2D) drawClipMask(enc *gpu.CommandEncoder, i int) bool {
	if i >= len(r.clipMasks) {
		return false
	}
	m := &r.clipMasks[i]
	if m.group > 0 || m.count == 0 || r.maskPipeline == nil {
		return false
	}
	mt := r.maskPool[m.target]
	if mt.view == nil {
		return false
	}
	pass := enc.BeginRenderPass(mt.view, gpu.Color{})
	pass.SetPipeline(r.maskPipeline)
	// The parent mask (or white) is what this one is intersected with.
	src := r.maskWhiteBind
	if m.parent > 0 {
		if pb := r.parentMaskBind(m.parent); pb != nil {
			src = pb
		}
	}
	pass.SetBindGroup(0, src)
	pass.SetBindGroup(1, r.maskIdentityBind)
	pass.SetVertexBuffer(0, r.clipVertBuf)
	pass.Draw(m.count, 1, m.start, 0)
	pass.End()
	return true
}

// parentMaskBind is group 0 of the mask pipeline over an enclosing mask —
// a path mask's own target, or a soft mask's group target.
func (r *Renderer2D) parentMaskBind(parent int) *gpu.BindGroup {
	pm := r.clipMasks[parent-1]
	if pm.group > 0 {
		return r.groupMaskBind(pm.group - 1)
	}
	if pt := r.maskPool[pm.target]; pt.bind != nil {
		return pt.bind
	}
	return nil
}

// ensureMaskPipeline builds the mask pass's pipeline on first use. It is
// the sprite shader at the mask format: the fragment samples the parent
// mask by uv and multiplies by the vertex colour, which is exactly "fill
// this path, inside whatever contains it".
func (r *Renderer2D) ensureMaskPipeline() error {
	if r.maskPipeline != nil {
		return nil
	}
	sm, err := r.device.CreateShaderModuleWGSL("clipmask2d", spriteShader)
	if err != nil {
		return err
	}
	defer sm.Release()
	pipe, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: sm, VertexEntry: "vs", FragmentEntry: "fs",
		Format: maskFormat, AlphaBlend: true, Samples: 1,
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
		return err
	}
	r.maskPipeline = pipe
	r.maskLayout0 = pipe.BindGroupLayout(0)
	r.maskLayout1 = pipe.BindGroupLayout(1)
	// A mask draw is itself unclipped: bind the identity clip shape.
	buf := r.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, clipUniformFloats*4)
	var identity [clipUniformFloats]float32
	r.queue.WriteBuffer(buf, 0, floatBytes(identity[:]))
	r.maskIdentityBuf = buf
	r.maskIdentityBind = r.device.CreateBindGroup(r.maskLayout1,
		gpu.BindGroupEntry{Binding: 0, Buffer: buf},
		gpu.BindGroupEntry{Binding: 1, TextureView: r.white.view},
		gpu.BindGroupEntry{Binding: 2, Sampler: r.sampler},
	)
	r.maskWhiteBind = r.device.CreateBindGroup(r.maskLayout0,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: r.white.view},
	)
	return nil
}

// ensureMaskTarget returns a pooled target of at least the given size,
// growing the pool as a frame needs it.
func (r *Renderer2D) ensureMaskTarget(i, w, h int) int {
	w, h = max(w, 1), max(h, 1)
	for len(r.maskPool) <= i {
		r.maskPool = append(r.maskPool, maskTarget{})
	}
	mt := &r.maskPool[i]
	if mt.tex != nil && mt.w == w && mt.h == h {
		return i
	}
	mt.release()
	// The mask is both drawn into and sampled from.
	tex := r.device.CreateTexture2D(w, h, maskFormat,
		gpu.TextureRenderAttachment|gpu.TextureBinding)
	view := tex.CreateView()
	*mt = maskTarget{
		tex: tex, view: view, w: w, h: h,
		bind: r.device.CreateBindGroup(r.maskLayout0,
			gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
			gpu.BindGroupEntry{Binding: 1, TextureView: view},
		),
	}
	return i
}

func (m *maskTarget) release() {
	if m.bind != nil {
		m.bind.Release()
	}
	if m.view != nil {
		m.view.Release()
	}
	if m.tex != nil {
		m.tex.Release()
	}
	*m = maskTarget{}
}

// maskViewFor is the texture a clip's shader samples: its mask, or the 1x1
// white when the clip is a plain box (group(1) always binds something).
func (r *Renderer2D) maskViewFor(c clipRect) *gpu.TextureView {
	if c.maskID > 0 && c.maskID <= len(r.clipMasks) {
		m := r.clipMasks[c.maskID-1]
		if m.group > 0 {
			if v := r.groupView(m.group - 1); v != nil {
				return v
			}
			return r.white.view
		}
		if mt := r.maskPool[m.target]; mt.view != nil {
			return mt.view
		}
	}
	return r.white.view
}

// releaseMasks frees the mask pool and pipeline.
func (r *Renderer2D) releaseMasks() {
	for i := range r.maskPool {
		r.maskPool[i].release()
	}
	r.maskPool = nil
	if r.maskWhiteBind != nil {
		r.maskWhiteBind.Release()
		r.maskWhiteBind = nil
	}
	if r.maskIdentityBind != nil {
		r.maskIdentityBind.Release()
		r.maskIdentityBind = nil
	}
	if r.maskIdentityBuf != nil {
		r.maskIdentityBuf.Release()
		r.maskIdentityBuf = nil
	}
	if r.maskLayout0 != nil {
		r.maskLayout0.Release()
		r.maskLayout0 = nil
	}
	if r.maskLayout1 != nil {
		r.maskLayout1.Release()
		r.maskLayout1 = nil
	}
	if r.maskPipeline != nil {
		r.maskPipeline.Release()
		r.maskPipeline = nil
	}
	if r.clipVertBuf != nil {
		r.clipVertBuf.Release()
		r.clipVertBuf = nil
	}
}
