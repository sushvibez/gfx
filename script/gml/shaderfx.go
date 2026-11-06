package gml

// A ShaderEffect that actually runs its shaders.
//
// A QML ShaderEffect names its shaders as URLs to .qsb files — what Qt's
// build compiles from GLSL and what a Qt app ships. We read the SPIR-V out
// of them (render.ParseQSB) and hand it to wgpu, which runs it through
// naga's frontend like any other shader, so nothing here needs a GLSL
// compiler. What the markup has to supply is the uniform block Qt's
// convention defines: qt_Matrix and qt_Opacity first, then the effect's own
// properties BY NAME, which is exactly what the .qsb's reflection lists.
//
// A shader we cannot load (no file, no SPIR-V variant, or a GPU that
// refuses it) falls back to what this engine has always drawn: the source,
// unshaded — see shadereffect.go.

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// shaderFXState is one ShaderEffect's retained GPU state: the parsed
// shaders and the pipeline built from them. GPU objects must outlive the
// per-frame element tree, so they live on the document by element key.
type shaderFXState struct {
	vert, frag *render.QSB
	fx         *render.ShaderFX
	// source is the offscreen capture of the item the shader samples, when
	// it samples one; cell is the image's own texture when the source is
	// an Image (see shaderSourceImage).
	source *ui.LayerState
	cell   *ui.ImageCell
	failed bool // parsed or built once and did not work; do not retry
}

// shaderFXFor returns the retained state for an element, parsing the two
// .qsb files on first use.
func (doc *docState) shaderFXFor(key, vertPath, fragPath string) *shaderFXState {
	if doc.shaderFX == nil {
		doc.shaderFX = map[string]*shaderFXState{}
	}
	if st, ok := doc.shaderFX[key]; ok {
		return st
	}
	st := &shaderFXState{}
	doc.shaderFX[key] = st
	frag := parseQSBFile(fragPath)
	if frag == nil {
		st.failed = true
		return st
	}
	vert := parseQSBFile(vertPath)
	if vert == nil {
		// A ShaderEffect may state only a fragment shader: Qt supplies the
		// default vertex shader, which passes the texture coordinate
		// through. Ours is built in WGSL by the renderer, so the effect
		// still needs a vertex .qsb — without one it degrades.
		st.failed = true
		return st
	}
	st.vert, st.frag = vert, frag
	return st
}

// parseQSBFile reads and parses a .qsb, or nil.
func parseQSBFile(path string) *render.QSB {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	q, err := render.ParseQSB(b)
	if err != nil {
		logf("gml: %s: %v", filepath.Base(path), err)
		return nil
	}
	return q
}

// resolveShaderPath maps a ShaderEffect's shader URL to a file. Qt states
// them as resource paths ("qrc:/qt/qml/<module>/shaders/x.frag.qsb")
// because that is where its build puts them; on disk the same files sit
// beside the page, so the module prefix is stripped and the rest is
// resolved against the document's directory.
func resolveShaderPath(baseDir, url string) string {
	p := strings.TrimSpace(url)
	if p == "" {
		return ""
	}
	p = strings.TrimPrefix(p, "file://")
	if strings.HasPrefix(p, "qrc:") {
		p = strings.TrimPrefix(p, "qrc:")
	}
	if rest, ok := strings.CutPrefix(p, "/qt/qml/"); ok {
		// Drop the module segment: /qt/qml/portal/shaders/x → shaders/x.
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			p = rest[i+1:]
		} else {
			p = rest
		}
	}
	p = strings.TrimPrefix(p, "/")
	cands := []string{p}
	if base := filepath.Base(p); base != p {
		cands = append(cands, base, filepath.Join("shaders", base))
	}
	// Where to look: beside the document, then the directories GFX_QSB_PATH
	// names. A Qt app SHIPS its .qsb files (its build compiles them from
	// GLSL into the resource tree), so a page's sources on their own are
	// not enough for us either — the path is how a build hands us the
	// compiled ones.
	dirs := []string{baseDir}
	if extra := os.Getenv("GFX_QSB_PATH"); extra != "" {
		dirs = append(dirs, filepath.SplitList(extra)...)
	}
	for _, dir := range dirs {
		for _, c := range cands {
			full := c
			if !filepath.IsAbs(full) && dir != "" {
				full = filepath.Join(dir, c)
			}
			if st, err := os.Stat(full); err == nil && !st.IsDir() {
				return full
			}
		}
	}
	return ""
}

// shaderEffectElement builds the element that RUNS a ShaderEffect, or nil
// when its shaders are not usable (the caller then degrades). w/h are the
// size the markup stated; 0 means "whatever the layout gives it".
func shaderEffectElement(n *node, env *Env, doc *docState, w, h float32) ui.Element {
	var vertURL, fragURL string
	for _, a := range n.attrs {
		switch a.name {
		case "vertexShader":
			vertURL, _ = attrString(a, env)
		case "fragmentShader":
			fragURL, _ = attrString(a, env)
		}
	}
	if fragURL == "" {
		return nil
	}
	st := doc.shaderFXFor(doc.key(n), resolveShaderPath(doc.baseDir, vertURL),
		resolveShaderPath(doc.baseDir, fragURL))
	if st.failed || st.frag == nil {
		return nil
	}
	// A shader that samples something needs that something as a TEXTURE:
	// the item its `source` names is captured into an offscreen layer (the
	// same machinery a blur uses), which the shader then reads. QML calls
	// this a ShaderEffectSource with hideSource; here the capture is a
	// sibling that paints nothing.
	var capture ui.Element
	if len(st.frag.Textures) > 0 {
		texName := st.frag.Textures[0].Name
		// An IMAGE source hands over its OWN texture, not a snapshot of
		// the item: in Qt an Image is a texture provider, so
		// `ShaderEffect { source: someImage }` samples the decoded image
		// whole and its fillMode never enters into it. Measured on the
		// corpus' lens page: Qt's shader sees the entire photo stretched
		// across the quad, where a rendered snapshot of the item would
		// show it cover-cropped — a 10% difference in scale down the
		// middle of the picture.
		if cell := doc.shaderSourceImage(n, env, texName); cell != nil {
			st.cell = cell
		} else if src := shaderSourceElement(n, env, doc, texName); src != nil {
			layer := doc.layerFor(doc.key(n) + "|fxsource")
			st.source = layer
			capture = ui.Layer(layer, src).Offscreen()
		}
	}
	// The uniform block, filled from the markup NOW (the values are
	// bindings, re-read on every build like any other attribute).
	data := shaderUniformBytes(st.frag, n, env, doc, w, h)
	// The pipeline needs a device, which only the paint has: build it on
	// the first paint and keep it.
	quad := ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
		if st.fx == nil || !st.fx.Valid(r) {
			if st.fx != nil {
				st.fx.Release()
				st.fx = nil
			}
			fx, err := r.NewShaderFX(doc.key(n), st.vert, st.frag, nil)
			if err != nil {
				logf("gml: ShaderEffect: %v", err)
				st.failed = true
				return
			}
			st.fx = fx
		}
		switch {
		case st.cell != nil:
			// The image's own texture, decoded for the quad's size.
			maxDim := int(2 * max(rect.W, rect.H))
			st.fx.SetTextures(r, []*render.Image{st.cell.EnsureSized(r, maxDim)})
		case st.source != nil:
			st.fx.SetTextures(r, []*render.Image{st.source.Image()})
		}
		st.fx.SetUniforms(r, data)
		r.DrawShaderFX(st.fx, rect.X, rect.Y, rect.W, rect.H)
	})
	if capture == nil {
		return quad
	}
	// The capture is laid out over the same box (it has to be the size the
	// shader samples) and paints nothing; the quad draws on top. The
	// wrapper carries the size the markup stated, or takes the one its
	// parent anchors it to.
	box := ui.Div().AnchorMode()
	if w > 0 || h > 0 {
		box.Size(w, h)
	}
	return box.Children(
		ui.Anchored(capture, "", ui.Anchors{Fill: "parent"}),
		ui.Anchored(quad, "", ui.Anchors{Fill: "parent"}),
	)
}

// shaderSourceImage returns the image cell a shader's source names, when
// that source is an Image item. Qt treats an Image as a TEXTURE PROVIDER:
// its texture goes to the shader as it is, so the item's fillMode, its
// clipping and its size play no part.
func (doc *docState) shaderSourceImage(n *node, env *Env, texName string) *ui.ImageCell {
	cellOf := func(src *node) *ui.ImageCell {
		if src == nil || (src.typeName != "Image" && src.typeName != "AnimatedImage") {
			return nil
		}
		for _, sa := range src.attrs {
			if sa.name != "source" || sa.expr == nil {
				continue
			}
			if v, err := sa.expr.eval(env); err == nil {
				if url := str(v); url != "" && !isVectorSource(url) {
					return doc.imageCell(url)
				}
			}
		}
		return nil
	}
	// The source named on THIS element.
	for _, name := range []string{texName, "source", "sourceItem", "src"} {
		if name == "" {
			continue
		}
		for _, a := range n.attrs {
			if a.name != name {
				continue
			}
			if id := doc.sourceRef(a.expr, env); id != "" && doc.nodesByID != nil {
				if c := cellOf(doc.nodesByID[id]); c != nil {
					return c
				}
			}
		}
	}
	// ... or the one a WRAPPING COMPONENT was handed: a reusable effect
	// declares `property var source` and its use site assigns the item
	// (`LensEffect { source: photoImage }`), so the value is in scope
	// rather than on this node.
	for _, name := range []string{"source", "sourceItem"} {
		v, ok := env.lookup(name)
		if !ok {
			continue
		}
		if r, isRef := v.(Ref); isRef {
			v = r.get()
		}
		if obj, isObj := v.(*elemObject); isObj {
			if c := cellOf(obj.node); c != nil {
				return c
			}
		}
	}
	return nil
}

// shaderSourceElement builds the item a shader samples: the element the
// named property (or `source`/`sourceItem`) points at.
func shaderSourceElement(n *node, env *Env, doc *docState, texName string) ui.Element {
	for _, name := range []string{texName, "source", "sourceItem", "src"} {
		if name == "" {
			continue
		}
		for _, a := range n.attrs {
			if a.name != name {
				continue
			}
			if id := doc.sourceRef(a.expr, env); id != "" {
				if el := doc.buildByID(id, env); el != nil {
					return el
				}
			}
		}
	}
	return doc.buildBySourceValue(n, env)
}

// shaderUniformBytes lays the uniform block out the way the shader
// declares it: qt_Matrix is the identity (the vertices are already in clip
// space, see render/shaderfx.go), qt_Opacity comes from the item, and every
// other member is looked up BY NAME among the element's attributes — which
// is how a ShaderEffect's properties reach its shader in QML.
func shaderUniformBytes(q *render.QSB, n *node, env *Env, doc *docState, w, h float32) []byte {
	size := q.Size
	if size < 16 {
		size = 16
	}
	buf := make([]byte, size)
	put := func(off int, vals ...float32) {
		for i, v := range vals {
			if off+4*i+4 <= len(buf) {
				putFloat32(buf[off+4*i:], v)
			}
		}
	}
	for _, u := range q.Uniforms {
		switch u.Name {
		case "qt_Matrix":
			put(u.Offset, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1)
			continue
		case "qt_Opacity":
			put(u.Offset, attrFloatOr(n, "opacity", env, 1))
			continue
		}
		v, ok := shaderUniformValue(n, env, doc, u.Name)
		if !ok {
			// Two names a page routinely derives from its own geometry
			// rather than stating: fill them in so a shader that reads
			// them is not left with zero.
			switch u.Name {
			case "aspectRatio":
				if h > 0 {
					put(u.Offset, w/h)
				}
			case "width", "itemWidth":
				put(u.Offset, w)
			case "height", "itemHeight":
				put(u.Offset, h)
			}
			continue
		}
		switch u.Kind {
		case render.UniformFloat, render.UniformInt, render.UniformBool:
			f, err := toFloat32(v)
			if err == nil {
				put(u.Offset, f)
			} else if b, err := toBool(v); err == nil {
				if b {
					put(u.Offset, 1)
				}
			}
		case render.UniformVec2, render.UniformVec3, render.UniformVec4:
			put(u.Offset, shaderVector(v, u.Kind)...)
		}
	}
	return buf
}

// shaderUniformValue reads the value of a ShaderEffect property by name: a
// declared property's live value, else a plain attribute's expression.
func shaderUniformValue(n *node, env *Env, doc *docState, name string) (any, bool) {
	// A declared property is what a handler WRITES (`portalEffect.time =
	// ...` from a FrameAnimation), so the element's own object holds the
	// live value — not the env, where the name may not exist at all.
	if r := doc.idPropRef(n, name); r != nil {
		return r.get(), true
	}
	if ov, ok := doc.attrOv.get(doc.keyID(n))[name]; ok {
		return ov, true
	}
	for _, a := range n.attrs {
		if a.name == name && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				return v, true
			}
			return nil, false
		}
	}
	for _, d := range n.props {
		if d.name == name {
			if r, ok := env.lookup(name); ok {
				if ref, isRef := r.(Ref); isRef {
					return ref.get(), true
				}
				return r, true
			}
			if d.def != nil {
				if v, err := d.def.eval(env); err == nil {
					return v, true
				}
			}
		}
	}
	return nil, false
}

// shaderVector turns a value into the components a vec2/3/4 uniform wants:
// a colour (QML passes one as a vec4), a point/size object, or a number
// broadcast across the components.
func shaderVector(v any, kind render.UniformKind) []float32 {
	n := 2
	switch kind {
	case render.UniformVec3:
		n = 3
	case render.UniformVec4:
		n = 4
	}
	out := make([]float32, n)
	switch t := v.(type) {
	case map[string]any:
		keys := [][]string{{"x", "width", "r"}, {"y", "height", "g"}, {"z", "b"}, {"w", "a"}}
		for i := 0; i < n; i++ {
			for _, k := range keys[i] {
				if f, err := toFloat32(t[k]); err == nil && t[k] != nil {
					out[i] = f
					break
				}
			}
		}
		return out
	case colorVal:
		return shaderVector(string(t), kind) // a colour is its text here
	case string:
		if c, err := parseColor(t); err == nil {
			comps := []float32{c.R, c.G, c.B, c.A}
			// A colour reaches a shader PREMULTIPLIED, as Qt's scene graph
			// keeps them.
			if n == 4 {
				return []float32{c.R * c.A, c.G * c.A, c.B * c.A, c.A}
			}
			copy(out, comps[:n])
			return out
		}
	case []any:
		for i := 0; i < n && i < len(t); i++ {
			if f, err := toFloat32(t[i]); err == nil {
				out[i] = f
			}
		}
		return out
	}
	if f, err := toFloat32(v); err == nil {
		for i := range out {
			out[i] = f
		}
	}
	return out
}

// putFloat32 writes a little-endian float32 (the layout every GPU buffer
// here uses).
func putFloat32(b []byte, f float32) {
	binary.LittleEndian.PutUint32(b, math.Float32bits(f))
}
